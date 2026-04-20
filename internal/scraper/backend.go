package scraper

import (
	"context"
	"fmt"
)

// Backend identifies an inference runtime whose /metrics endpoint the scraper
// knows how to parse.
type Backend string

const (
	// BackendLlamaCPP scrapes metrics named llamacpp:*. Default when no
	// backend annotation or label is present on the pod.
	BackendLlamaCPP Backend = "llamacpp"
	// BackendVLLM scrapes metrics named vllm:*.
	BackendVLLM Backend = "vllm"
)

const (
	// BackendAnnotation is the pod annotation or label key that selects the
	// metrics backend. Values: "llamacpp" (default), "vllm".
	BackendAnnotation = "infercost.ai/backend"
	// MetricsPortAnnotation is the pod annotation or label key that overrides
	// the default /metrics port. Defaults per backend:
	//   llamacpp: 8080
	//   vllm:     8000
	MetricsPortAnnotation = "infercost.ai/metrics-port"
)

// DefaultPort returns the conventional /metrics port for a backend.
func (b Backend) DefaultPort() int {
	switch b {
	case BackendVLLM:
		return 8000
	default:
		return 8080
	}
}

// ResolveBackend picks a backend from pod annotations or labels, falling back
// to llama.cpp. Annotations take precedence over labels so users can override
// the backend without relabeling pods that might be owned by a controller.
func ResolveBackend(annotations, labels map[string]string) Backend {
	if v, ok := annotations[BackendAnnotation]; ok {
		return normalizeBackend(v)
	}
	if v, ok := labels[BackendAnnotation]; ok {
		return normalizeBackend(v)
	}
	return BackendLlamaCPP
}

func normalizeBackend(v string) Backend {
	switch Backend(v) {
	case BackendVLLM:
		return BackendVLLM
	default:
		return BackendLlamaCPP
	}
}

// ResolveMetricsPort returns the /metrics port for a backend, honoring an
// explicit infercost.ai/metrics-port annotation/label override when present.
// Invalid or non-numeric overrides are ignored.
func ResolveMetricsPort(backend Backend, annotations, labels map[string]string) int {
	if v, ok := annotations[MetricsPortAnnotation]; ok {
		if p, ok := parsePort(v); ok {
			return p
		}
	}
	if v, ok := labels[MetricsPortAnnotation]; ok {
		if p, ok := parsePort(v); ok {
			return p
		}
	}
	return backend.DefaultPort()
}

func parsePort(s string) (int, bool) {
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil {
		return 0, false
	}
	if p <= 0 || p > 65535 {
		return 0, false
	}
	return p, true
}

// Scrape dispatches to the correct backend parser for an endpoint. Returns
// a backend-agnostic InferenceMetrics so downstream code does not need to
// know which runtime served the data.
func Scrape(ctx context.Context, client *Client, backend Backend, endpoint string) (*InferenceMetrics, error) {
	switch backend {
	case BackendVLLM:
		return ScrapeVLLM(ctx, client, endpoint)
	case BackendLlamaCPP:
		return ScrapeLlamaCPP(ctx, client, endpoint)
	default:
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
}
