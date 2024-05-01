package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type UserCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserCreationResponse struct {
	Message string `json:"UUID"`
	Error   string `json:"error"`
}

func main() {
	clientset, config, err := initializeClient()
	if err != nil {
		log.Fatalf("Error initializing Kubernetes client: %v", err)
	}

	nc, err := nats.Connect("connect.ngs.global", nats.UserCredentials("NGS-Karthick-karthick.creds"), nats.Name("Mender Consumer"))
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	stream, err := js.Stream(ctx, "MenderUser")
	if err != nil {
		log.Fatal(err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:    "mender_users",
		Durable: "mender_users",
	})
	if err != nil {
		log.Fatal(err)
	}
	cctx, err := consumer.Consume(func(msgs jetstream.Msg) {
		var creds UserCredentials
		if err := json.Unmarshal(msgs.Data(), &creds); err != nil {
			log.Printf("Error decoding user credentials: %v", err)
			msgs.Nak()
			return
		}

		handleUserSignup(clientset, config, creds)
		msgs.Ack()
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cctx.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
}

func initializeClient() (*kubernetes.Clientset, *rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create clientset: %v", err)
	}

	return clientset, config, nil
}

func fetchPodByName(clientset *kubernetes.Clientset, namespace, labelSelector string) (*corev1.Pod, error) {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pods: %v", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found with the label selector '%s' in the namespace '%s'", labelSelector, namespace)
	}

	return &pods.Items[0], nil
}

func execCommandInPod(clientset *kubernetes.Clientset, config *rest.Config, namespace, podName, containerName string, command []string) (string, error) {
	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   command,
			Container: containerName,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
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

	return stdout.String(), fmt.Errorf(stderr.String())
}

func handleUserSignup(clientset *kubernetes.Clientset, config *rest.Config, creds UserCredentials) {
	namespace := "default"
	labelSelector := "app.kubernetes.io/component=useradm"

	pod, err := fetchPodByName(clientset, namespace, labelSelector)
	if err != nil {
		log.Printf("Error fetching pod: %v", err)
		return
	}

	containerName := ""
	for _, container := range pod.Spec.Containers {
		if container.Name != "linkerd-proxy" {
			containerName = container.Name
			break
		}
	}

	if containerName == "" {
		log.Println("No appropriate container found")
		return
	}

	log.Printf("Selected container: %s", containerName)
	command := []string{"/usr/bin/useradm", "create-user", "--username", creds.Email, "--password", creds.Password}
	output, execErr := execCommandInPod(clientset, config, namespace, pod.Name, containerName, command)

	var response UserCreationResponse
	if execErr != nil {
		response.Error = execErr.Error()
	}
	response.Message = output

	// Log the response or handle it further if needed
	log.Printf("User creation response: %+v", response)
}
