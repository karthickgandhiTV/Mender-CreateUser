package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

// GetKubernetesClientConfig returns the Kubernetes client configuration.
func GetKubernetesClientConfig(kubeconfigPath string) (*rest.Config, error) {
	// If kubeconfigPath is empty, use the default path
	if kubeconfigPath == "" {
		kubeconfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	// Load kubeconfig from file
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func main() {
	// Parse command-line flags
	kubeconfigPath := flag.String("kubeconfig", "", "Path to the kubeconfig file")
	flag.Parse()

	// Get the Kubernetes client configuration
	config, err := GetKubernetesClientConfig(*kubeconfigPath)
	if err != nil {
		panic(err)
	}

	// Use the configuration to create the Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	// Register HTTP handler for user creation
	http.HandleFunc("/create-user", func(w http.ResponseWriter, r *http.Request) {
		CreateUserHandler(w, r, clientset)
	})

	// Start HTTP server
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func ExecuteCommandInPod(clientset *kubernetes.Clientset, config *rest.Config, namespace, podName, containerName string, command []string) (string, error) {
	// Construct the command to execute in the pod
	cmd := []string{
		"sh",
		"-c",
		fmt.Sprintf("%s", command), // Convert command to string for execution
	}

	// Create a request to execute the command on the specified pod
	req := clientset.CoreV1().RESTClient().Post().Resource("pods").Name(podName).
		Namespace(namespace).SubResource("exec")

	// Configure options for executing the command
	option := &v1.PodExecOptions{
		Command:   cmd,
		Container: containerName,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}

	// Set versioned params and codec
	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)

	// Create an SPDY executor for executing the command
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor: %v", err)
	}

	// Stream the command's input/output/error streams using the executor
	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", fmt.Errorf("failed to execute command in pod: %v", err)
	}

	// Collect the output
	var output bytes.Buffer
	output.WriteString(stdout.String())
	output.WriteString(stderr.String())

	return output.String(), nil
}

// GetPodNameByLabelSelector retrieves the name of a pod based on a label selector.
func GetPodNameByLabelSelector(clientset *kubernetes.Clientset, namespace, labelSelector string) (string, error) {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %v", err)
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pod found with label selector: %s", labelSelector)
	}

	podName := pods.Items[0].Name
	return podName, nil
}

// ExecuteCommandOnPod executes a command on a specific pod.
func ExecuteCommandOnPod(ctx context.Context, podName, namespace, command string) (string, error) {
	// Create the exec command
	cmd := exec.CommandContext(ctx, "kubectl", "exec", "-n", namespace, podName, "--", "/bin/sh", "-c", command)

	// Execute the command and capture output
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to execute command on pod: %v", err)
	}

	return out.String(), nil
}

func CreateUserHandler(w http.ResponseWriter, r *http.Request, clientset *kubernetes.Clientset) {
	var requestBody struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "failed to parse request body", http.StatusBadRequest)
		return
	}

	namespace := "default"
	podName, err := GetPodNameByLabelSelector(clientset, namespace, "app.kubernetes.io/component=useradm")
	if err != nil {
		log.Printf("Failed to get user admin pod name: %v", err)
		http.Error(w, "failed to get user admin pod name", http.StatusInternalServerError)
		return
	}

	cmd := fmt.Sprintf("useradm create-user --username %s --password %s", requestBody.Username, requestBody.Password)
	_, err = ExecuteCommandOnPod(r.Context(), podName, namespace, cmd)
	if err != nil {
		log.Printf("Failed to create user: %v", err)
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		return
	}

	response := struct {
		Message string `json:"message"`
	}{
		Message: "User created successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
