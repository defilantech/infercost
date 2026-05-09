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
	// LLMKubeRuntimeLabel is the pod label LLMKube applies to inference
	// pods identifying the runtime backend. ResolveBackend reads this as
	// a fallback when neither the InferCost annotation nor label is set,
	// so users running InferCost alongside LLMKube get correct backend
	// detection out of the box without an extra annotation.
	//
	// Reference: github.com/defilantech/LLMKube PR #410.
	LLMKubeRuntimeLabel = "inference.llmkube.dev/runtime"
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

// BackendSource records which lookup rule produced a backend so callers can
// log appropriately when a non-explicit fallback fired. The order also
// reflects precedence: lower-numbered sources beat higher-numbered ones.
type BackendSource int

const (
	// BackendSourceAnnotation: explicit infercost.ai/backend annotation.
	BackendSourceAnnotation BackendSource = iota
	// BackendSourceLabel: explicit infercost.ai/backend label.
	BackendSourceLabel
	// BackendSourceLLMKubeRuntime: inferred from inference.llmkube.dev/runtime
	// label that LLMKube emits on its inference pods. Fires when neither
	// InferCost annotation nor label is present.
	BackendSourceLLMKubeRuntime
	// BackendSourceDefault: nothing matched, fell back to llama.cpp.
	BackendSourceDefault
)

func (s BackendSource) String() string {
	switch s {
	case BackendSourceAnnotation:
		return "infercost.ai/backend annotation"
	case BackendSourceLabel:
		return "infercost.ai/backend label"
	case BackendSourceLLMKubeRuntime:
		return LLMKubeRuntimeLabel + " label (LLMKube)"
	default:
		return "default (no backend hint found)"
	}
}

// ResolveBackend picks a backend from pod annotations or labels, falling back
// to llama.cpp. Precedence:
//
//  1. infercost.ai/backend annotation (explicit override)
//  2. infercost.ai/backend label (explicit override at label level)
//  3. inference.llmkube.dev/runtime label (LLMKube emits this; we read it as a
//     fallback so InferCost works out of the box alongside LLMKube)
//  4. default to llama.cpp
//
// Returns just the resolved backend; callers that want to log which rule
// fired should use ResolveBackendWithSource.
func ResolveBackend(annotations, labels map[string]string) Backend {
	b, _ := ResolveBackendWithSource(annotations, labels)
	return b
}

// ResolveBackendWithSource is the explicit-source variant of ResolveBackend.
// Used by the controller scrape loops so they can log "inferred backend from
// LLMKube runtime label" at info level when rule 3 fires, making the
// behavior visible to operators without surprising them.
func ResolveBackendWithSource(annotations, labels map[string]string) (Backend, BackendSource) {
	if v, ok := annotations[BackendAnnotation]; ok {
		return normalizeBackend(v), BackendSourceAnnotation
	}
	if v, ok := labels[BackendAnnotation]; ok {
		return normalizeBackend(v), BackendSourceLabel
	}
	if v, ok := labels[LLMKubeRuntimeLabel]; ok && v != "" {
		return normalizeBackend(v), BackendSourceLLMKubeRuntime
	}
	return BackendLlamaCPP, BackendSourceDefault
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
