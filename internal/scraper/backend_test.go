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
