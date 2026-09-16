package kubeenv

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const projectKubeconfigEnv = "KUBERNETES_KUBECONFIG"

func NewClientset(explicitKubeconfig string) (kubernetes.Interface, error) {
	config, err := RESTConfig(explicitKubeconfig)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return client, nil
}

func RESTConfig(explicitKubeconfig string) (*rest.Config, error) {
	path := strings.TrimSpace(explicitKubeconfig)
	if path == "" {
		path = strings.TrimSpace(os.Getenv(projectKubeconfigEnv))
	}
	if path == "" {
		path = strings.TrimSpace(os.Getenv("KUBECONFIG"))
	}
	if path != "" {
		config, err := clientcmd.BuildConfigFromFlags("", path)
		if err != nil {
			return nil, fmt.Errorf("load explicit Kubernetes configuration: %w", err)
		}
		return config, nil
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		if errors.Is(err, rest.ErrNotInCluster) {
			return nil, errors.New("Kubernetes configuration is required: set KUBERNETES_KUBECONFIG or KUBECONFIG, or run in cluster")
		}
		return nil, fmt.Errorf("load in-cluster Kubernetes configuration: %w", err)
	}
	return config, nil
}
