package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var inferenceServiceGVR = schema.GroupVersionResource{
	Group:    "inference.llmkube.dev",
	Version:  "v1alpha1",
	Resource: "inferenceservices",
}

// DiscoveredModel represents a model found through any discovery mechanism.
type DiscoveredModel struct {
	Name        string // Model name (from pod label or CRD modelRef)
	Namespace   string
	Source      string // "pod" or "inferenceservice"
	SourceName  string // Pod name or "isvc/{name}"
	MetricsURL  string // URL to scrape llama.cpp /metrics
	Phase       string // "Running", "Ready", etc.
	IsScrapable bool   // Whether the model has a valid metrics endpoint
}

// discoverModelsFromPods scans a list of pods for inference model labels.
// Returns discovered models and a set of namespace/model keys for deduplication.
func discoverModelsFromPods(cfg *rest.Config, pods []corev1.Pod) ([]DiscoveredModel, map[string]bool) {
	var models []DiscoveredModel
	known := make(map[string]bool)

	for i := range pods {
		pod := &pods[i]
		modelName := pod.Labels["inference.llmkube.dev/model"]
		if modelName == "" {
			continue
		}

		key := deduplicationKey(pod.Namespace, modelName)
		known[key] = true

		m := DiscoveredModel{
			Name:       modelName,
			Namespace:  pod.Namespace,
			Source:     "pod",
			SourceName: pod.Name,
			Phase:      string(pod.Status.Phase),
		}

		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			m.MetricsURL = podProxyURL(cfg, pod.Namespace, pod.Name, 8080, "metrics")
			m.IsScrapable = true
		}

		models = append(models, m)
	}

	return models, known
}

// discoverModelsFromInferenceServices lists InferenceService CRDs and returns
// models not already present in knownModels. Returns nil error if the CRD is
// not installed or RBAC denies access.
func discoverModelsFromInferenceServices(
	ctx context.Context,
	dynClient dynamic.Interface,
	k8sClient client.Client,
	namespace string,
	knownModels map[string]bool,
) ([]DiscoveredModel, error) {
	var res dynamic.ResourceInterface
	if namespace != "" {
		res = dynClient.Resource(inferenceServiceGVR).Namespace(namespace)
	} else {
		res = dynClient.Resource(inferenceServiceGVR)
	}

	list, err := res.List(ctx, metav1.ListOptions{})
	if err != nil {
		// CRD not installed or RBAC denied — degrade gracefully.
		if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
			return nil, nil
		}
		// Also handle discovery errors (CRD not registered at all).
		if apierrors.IsServiceUnavailable(err) {
			return nil, nil
		}
		// Dynamic client returns a generic error for missing CRDs that may not
		// match the above checks. If it's a 404-class error, swallow it.
		if statusErr, ok := err.(*apierrors.StatusError); ok {
			code := statusErr.Status().Code
			if code == 404 || code == 403 {
				return nil, nil
			}
		}
		return nil, fmt.Errorf("listing InferenceServices: %w", err)
	}

	var models []DiscoveredModel
	for i := range list.Items {
		obj := &list.Items[i]
		m, ok := inferenceServiceToModel(obj, knownModels)
		if !ok {
			continue
		}

		// The status.endpoint uses K8s service DNS which isn't resolvable
		// from outside the cluster. Resolve the actual backing IP via the
		// K8s Endpoints object.
		if k8sClient != nil {
			if resolved, err := resolveEndpointIP(ctx, k8sClient, obj.GetName(), obj.GetNamespace()); err == nil && resolved != "" {
				m.MetricsURL = resolved
			}
		}

		models = append(models, m)
	}

	return models, nil
}

// inferenceServiceToModel extracts a DiscoveredModel from an unstructured
// InferenceService object. Returns false if the model should be skipped.
func inferenceServiceToModel(obj *unstructured.Unstructured, knownModels map[string]bool) (DiscoveredModel, bool) {
	modelRef, _, _ := unstructured.NestedString(obj.Object, "spec", "modelRef")
	if modelRef == "" {
		return DiscoveredModel{}, false
	}

	ns := obj.GetNamespace()
	key := deduplicationKey(ns, modelRef)
	if knownModels[key] {
		return DiscoveredModel{}, false
	}

	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if phase != "Ready" {
		return DiscoveredModel{}, false
	}

	endpoint, _, _ := unstructured.NestedString(obj.Object, "status", "endpoint")
	metricsURL, err := metricsURLFromEndpoint(endpoint)
	if err != nil {
		return DiscoveredModel{}, false
	}

	return DiscoveredModel{
		Name:        modelRef,
		Namespace:   ns,
		Source:      "inferenceservice",
		SourceName:  fmt.Sprintf("isvc/%s", obj.GetName()),
		MetricsURL:  metricsURL,
		Phase:       phase,
		IsScrapable: true,
	}, true
}

// metricsURLFromEndpoint derives a /metrics URL from an InferenceService
// status.endpoint URL by replacing the path component.
// e.g. "http://host:8080/v1/chat/completions" → "http://host:8080/metrics"
func metricsURLFromEndpoint(endpoint string) (string, error) {
	if endpoint == "" {
		return "", fmt.Errorf("empty endpoint")
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parsing endpoint URL: %w", err)
	}

	if u.Host == "" {
		return "", fmt.Errorf("endpoint has no host: %s", endpoint)
	}

	u.Path = "/metrics"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// resolveEndpointIP looks up the K8s Endpoints object for a service and returns
// a metrics URL using the actual backing IP address. This is needed because
// status.endpoint uses K8s service DNS names (e.g., svc.ns.svc.cluster.local)
// which aren't resolvable from outside the cluster.
func resolveEndpointIP(ctx context.Context, k8sClient client.Client, svcName, namespace string) (string, error) {
	var ep corev1.Endpoints
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: svcName, Namespace: namespace}, &ep); err != nil {
		return "", err
	}

	for _, subset := range ep.Subsets {
		if len(subset.Addresses) == 0 {
			continue
		}
		ip := subset.Addresses[0].IP
		port := 8080
		for _, p := range subset.Ports {
			if p.Name == "http" || strings.EqualFold(p.Name, "http") {
				port = int(p.Port)
				break
			}
			// Use the first port if no "http" named port.
			port = int(p.Port)
		}
		return fmt.Sprintf("http://%s:%d/metrics", ip, port), nil
	}

	return "", fmt.Errorf("no ready addresses in endpoints for %s/%s", namespace, svcName)
}

// deduplicationKey returns a consistent key for model deduplication.
func deduplicationKey(namespace, modelName string) string {
	return namespace + "/" + modelName
}
