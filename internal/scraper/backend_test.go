package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolveBackend_DefaultsToLlamaCPP(t *testing.T) {
	b := ResolveBackend(nil, nil)
	if b != BackendLlamaCPP {
		t.Errorf("expected default llamacpp, got %q", b)
	}
}

func TestResolveBackend_AnnotationBeatsLabel(t *testing.T) {
	annotations := map[string]string{BackendAnnotation: "vllm"}
	labels := map[string]string{BackendAnnotation: "llamacpp"}
	if got := ResolveBackend(annotations, labels); got != BackendVLLM {
		t.Errorf("annotation should win, got %q", got)
	}
}

func TestResolveBackend_LabelFallback(t *testing.T) {
	labels := map[string]string{BackendAnnotation: "vllm"}
	if got := ResolveBackend(nil, labels); got != BackendVLLM {
		t.Errorf("label fallback failed, got %q", got)
	}
}

func TestResolveBackend_UnknownFallsBackToLlamaCPP(t *testing.T) {
	annotations := map[string]string{BackendAnnotation: "sglang"}
	if got := ResolveBackend(annotations, nil); got != BackendLlamaCPP {
		t.Errorf("unknown backend should fall back to llamacpp, got %q", got)
	}
}

// LLMKube emits inference.llmkube.dev/runtime on its inference pods. When
// neither InferCost annotation nor label is present, ResolveBackend uses
// that label as a fallback so InferCost works out of the box alongside
// LLMKube without an extra annotation step. See InferCost issue #45.
func TestResolveBackend_LLMKubeRuntimeLabelFallback_VLLM(t *testing.T) {
	labels := map[string]string{LLMKubeRuntimeLabel: "vllm"}
	if got := ResolveBackend(nil, labels); got != BackendVLLM {
		t.Errorf("LLMKube runtime=vllm should resolve to BackendVLLM, got %q", got)
	}
}

func TestResolveBackend_LLMKubeRuntimeLabelFallback_LlamaCPP(t *testing.T) {
	labels := map[string]string{LLMKubeRuntimeLabel: "llamacpp"}
	if got := ResolveBackend(nil, labels); got != BackendLlamaCPP {
		t.Errorf("LLMKube runtime=llamacpp should resolve to BackendLlamaCPP, got %q", got)
	}
}

// Explicit override (rule 1) still wins over the LLMKube label (rule 3).
func TestResolveBackend_AnnotationBeatsLLMKubeLabel(t *testing.T) {
	annotations := map[string]string{BackendAnnotation: "llamacpp"}
	labels := map[string]string{LLMKubeRuntimeLabel: "vllm"}
	if got := ResolveBackend(annotations, labels); got != BackendLlamaCPP {
		t.Errorf("InferCost annotation must beat LLMKube label, got %q", got)
	}
}

// InferCost label override (rule 2) still wins over the LLMKube label (rule 3).
func TestResolveBackend_InferCostLabelBeatsLLMKubeLabel(t *testing.T) {
	labels := map[string]string{
		BackendAnnotation:   "llamacpp",
		LLMKubeRuntimeLabel: "vllm",
	}
	if got := ResolveBackend(nil, labels); got != BackendLlamaCPP {
		t.Errorf("InferCost label must beat LLMKube label, got %q", got)
	}
}

// Unknown LLMKube runtime values (e.g. "tgi", "personaplex") fall through to
// llamacpp default rather than erroring; we treat unknowns as non-fatal so
// new LLMKube runtimes don't break InferCost installs that haven't bumped.
func TestResolveBackend_UnknownLLMKubeRuntimeFallsBackToLlamaCPP(t *testing.T) {
	labels := map[string]string{LLMKubeRuntimeLabel: "tgi"}
	if got := ResolveBackend(nil, labels); got != BackendLlamaCPP {
		t.Errorf("unknown LLMKube runtime should fall back to llamacpp, got %q", got)
	}
}

// Empty LLMKube label value (someone applied it but cleared the value)
// falls through to the default rather than treating "" as a runtime hint.
func TestResolveBackend_EmptyLLMKubeRuntimeFallsBackToDefault(t *testing.T) {
	labels := map[string]string{LLMKubeRuntimeLabel: ""}
	got, source := ResolveBackendWithSource(nil, labels)
	if got != BackendLlamaCPP {
		t.Errorf("empty LLMKube runtime should fall back to llamacpp, got %q", got)
	}
	if source != BackendSourceDefault {
		t.Errorf("empty LLMKube runtime should report BackendSourceDefault, got %v", source)
	}
}

func TestResolveBackendWithSource_ReportsSource(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		labels      map[string]string
		wantBackend Backend
		wantSource  BackendSource
	}{
		{
			name:        "annotation source",
			annotations: map[string]string{BackendAnnotation: "vllm"},
			wantBackend: BackendVLLM,
			wantSource:  BackendSourceAnnotation,
		},
		{
			name:        "label source",
			labels:      map[string]string{BackendAnnotation: "vllm"},
			wantBackend: BackendVLLM,
			wantSource:  BackendSourceLabel,
		},
		{
			name:        "llmkube runtime source",
			labels:      map[string]string{LLMKubeRuntimeLabel: "vllm"},
			wantBackend: BackendVLLM,
			wantSource:  BackendSourceLLMKubeRuntime,
		},
		{
			name:        "default source",
			wantBackend: BackendLlamaCPP,
			wantSource:  BackendSourceDefault,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, s := ResolveBackendWithSource(tc.annotations, tc.labels)
			if b != tc.wantBackend {
				t.Errorf("backend = %q, want %q", b, tc.wantBackend)
			}
			if s != tc.wantSource {
				t.Errorf("source = %v, want %v", s, tc.wantSource)
			}
		})
	}
}

func TestDefaultPort(t *testing.T) {
	if BackendLlamaCPP.DefaultPort() != 8080 {
		t.Errorf("llamacpp default port = %d, want 8080", BackendLlamaCPP.DefaultPort())
	}
	if BackendVLLM.DefaultPort() != 8000 {
		t.Errorf("vllm default port = %d, want 8000", BackendVLLM.DefaultPort())
	}
}

func TestResolveMetricsPort_Override(t *testing.T) {
	annotations := map[string]string{MetricsPortAnnotation: "9090"}
	if got := ResolveMetricsPort(BackendVLLM, annotations, nil); got != 9090 {
		t.Errorf("annotation override = %d, want 9090", got)
	}
}

func TestResolveMetricsPort_InvalidOverrideFallsBack(t *testing.T) {
	annotations := map[string]string{MetricsPortAnnotation: "not-a-number"}
	if got := ResolveMetricsPort(BackendVLLM, annotations, nil); got != 8000 {
		t.Errorf("invalid override should fall back to default 8000, got %d", got)
	}
	annotations = map[string]string{MetricsPortAnnotation: "99999"}
	if got := ResolveMetricsPort(BackendLlamaCPP, annotations, nil); got != 8080 {
		t.Errorf("out-of-range override should fall back to default 8080, got %d", got)
	}
}

func TestScrape_DispatchesToVLLM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(vllmMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := Scrape(context.Background(), client, BackendVLLM, server.URL)
	if err != nil {
		t.Fatalf("Scrape returned error: %v", err)
	}
	if m.PromptTokensTotal != 250000 {
		t.Errorf("PromptTokensTotal = %v, want 250000 (vllm dispatch)", m.PromptTokensTotal)
	}
}

func TestScrape_DispatchesToLlamaCPP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(llamacppMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := Scrape(context.Background(), client, BackendLlamaCPP, server.URL)
	if err != nil {
		t.Fatalf("Scrape returned error: %v", err)
	}
	if m.PromptTokensTotal != 107537 {
		t.Errorf("PromptTokensTotal = %v, want 107537 (llamacpp dispatch)", m.PromptTokensTotal)
	}
}

func TestScrape_UnknownBackendErrors(t *testing.T) {
	client := NewClient(5 * time.Second)
	_, err := Scrape(context.Background(), client, Backend("unknown"), "http://example.invalid")
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}
