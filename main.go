package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/homedir"
)

func main() {
	// Initialize the Kubernetes client
	clientset, config, err := initializeClient()
	if err != nil {
		log.Fatalf("Error initializing Kubernetes client: %v", err)
	}

	// Setup HTTP server and register handler
	http.HandleFunc("/exec-command", handleExecCommand(clientset, config))

	// Start the server
	log.Println("Starting server on port 8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

// initializeClient creates a Kubernetes clientset based on the kubeconfig file.
func initializeClient() (*kubernetes.Clientset, *rest.Config, error) {
	var kubeconfig string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	} else {
		kubeconfig = os.Getenv("KUBECONFIG") // Assume environment variable if not found in home
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create clientset: %v", err)
	}

	return clientset, config, nil
}

// fetchPodByName retrieves the first pod based on label selector in the specified namespace.
func fetchPodByName(clientset *kubernetes.Clientset, namespace, labelSelector string) (string, error) {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to fetch pods: %v", err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found with the label selector '%s' in the namespace '%s'", labelSelector, namespace)
	}

	return pods.Items[0].Name, nil // Assumes the first pod is the target
}

// execCommandInPod executes a command in a specific pod and returns the output.
func execCommandInPod(clientset *kubernetes.Clientset, config *rest.Config, namespace, podName string, command []string) (string, error) {
	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: command,
			Stdin:   false,
			Stdout:  true,
			Stderr:  true,
			TTY:     false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to initialize SPDY executor: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})
	if err != nil {
		return "", fmt.Errorf("failed to execute command: %v", err)
	}

	if stderr.Len() > 0 {
		return "", fmt.Errorf("command error: %s", stderr.String())
	}

	return stdout.String(), nil
}

// handleExecCommand provides an HTTP endpoint for executing commands in a pod.
func handleExecCommand(clientset *kubernetes.Clientset, config *rest.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Define namespace and labels
		namespace := "default"
		labelSelector := "app.kubernetes.io/component=useradm"

		// Fetch the pod where the command will be executed
		podName, err := fetchPodByName(clientset, namespace, labelSelector)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Define the command to be executed inside the pod
		command := []string{"/bin/sh", "-c", "useradm create-user --username 'demo@mender.io' --password 'demodemo'"}

		// Execute the command in the fetched pod
		output, execErr := execCommandInPod(clientset, config, namespace, podName, command)
		if execErr != nil {
			http.Error(w, execErr.Error(), http.StatusInternalServerError)
			return
		}

		// Send the output back to the client
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(output))
	}
}
