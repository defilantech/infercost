package cli

import (
	"fmt"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
)

type k8sClients struct {
	client     client.Client
	dynamic    dynamic.Interface
	restConfig *rest.Config
}

func newK8sClient() (*k8sClients, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	if err := finopsv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		return nil, fmt.Errorf("failed to add scheme: %w", err)
	}

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &k8sClients{client: k8sClient, dynamic: dynClient, restConfig: cfg}, nil
}

// podProxyURL returns a URL that routes through the K8s API server to reach
// a pod's HTTP endpoint, avoiding the need for direct pod IP connectivity.
func podProxyURL(cfg *rest.Config, namespace, podName string, port int, path string) string {
	return fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s:%d/proxy/%s",
		cfg.Host, namespace, podName, port, path)
}
