package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// vllmMetricsText is a representative vLLM /metrics response with model_name
// labels. Counters include tokens and request histograms; gauges include the
// running/waiting request counts and (older-vllm) average throughputs.
const vllmMetricsText = `# HELP vllm:prompt_tokens_total Number of prefill tokens processed.
# TYPE vllm:prompt_tokens_total counter
vllm:prompt_tokens_total{model_name="qwen3-coder-30b"} 250000
# HELP vllm:generation_tokens_total Number of generation tokens processed.
# TYPE vllm:generation_tokens_total counter
vllm:generation_tokens_total{model_name="qwen3-coder-30b"} 870000
# HELP vllm:num_requests_running Number of requests currently running on GPU.
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{model_name="qwen3-coder-30b"} 4
# HELP vllm:num_requests_waiting Number of requests waiting to be processed.
# TYPE vllm:num_requests_waiting gauge
vllm:num_requests_waiting{model_name="qwen3-coder-30b"} 1
# HELP vllm:avg_prompt_throughput_toks_per_s Average prompt throughput in tokens/s.
# TYPE vllm:avg_prompt_throughput_toks_per_s gauge
vllm:avg_prompt_throughput_toks_per_s{model_name="qwen3-coder-30b"} 1200.5
# HELP vllm:avg_generation_throughput_toks_per_s Average generation throughput in tokens/s.
# TYPE vllm:avg_generation_throughput_toks_per_s gauge
vllm:avg_generation_throughput_toks_per_s{model_name="qwen3-coder-30b"} 42.8
# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 37
`

func TestScrapeVLLM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(vllmMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeVLLM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeVLLM returned error: %v", err)
	}

	if m.PromptTokensTotal != 250000 {
		t.Errorf("PromptTokensTotal = %v, want 250000", m.PromptTokensTotal)
	}
	if m.PredictedTokensTotal != 870000 {
		t.Errorf("PredictedTokensTotal = %v, want 870000", m.PredictedTokensTotal)
	}
	if m.RequestsProcessing != 4 {
		t.Errorf("RequestsProcessing = %v, want 4", m.RequestsProcessing)
	}
	if m.PromptTokensPerSec != 1200.5 {
		t.Errorf("PromptTokensPerSec = %v, want 1200.5", m.PromptTokensPerSec)
	}
	if m.PredictedTokensPerSec != 42.8 {
		t.Errorf("PredictedTokensPerSec = %v, want 42.8", m.PredictedTokensPerSec)
	}
}

// A vLLM pod serving multiple models emits the same metric under different
// model_name label sets. The scraper sums them because downstream attribution
// is per-pod (not per-model-name-label), and the pod-level total is the figure
// that matches what the controller records in UsageReport byModel breakdowns.
func TestScrapeVLLM_AggregatesAcrossModelNameLabels(t *testing.T) {
	multiModelMetrics := `# HELP vllm:prompt_tokens_total Number of prefill tokens processed.
# TYPE vllm:prompt_tokens_total counter
vllm:prompt_tokens_total{model_name="qwen3-coder-30b"} 100000
vllm:prompt_tokens_total{model_name="qwen3-8b"} 50000
# HELP vllm:generation_tokens_total Number of generation tokens processed.
# TYPE vllm:generation_tokens_total counter
vllm:generation_tokens_total{model_name="qwen3-coder-30b"} 400000
vllm:generation_tokens_total{model_name="qwen3-8b"} 200000
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(multiModelMetrics))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeVLLM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeVLLM returned error: %v", err)
	}
	if m.PromptTokensTotal != 150000 {
		t.Errorf("PromptTokensTotal = %v, want 150000 (100k+50k)", m.PromptTokensTotal)
	}
	if m.PredictedTokensTotal != 600000 {
		t.Errorf("PredictedTokensTotal = %v, want 600000 (400k+200k)", m.PredictedTokensTotal)
	}
}

func TestScrapeVLLM_EmptyMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeVLLM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeVLLM returned error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil InferenceMetrics for empty response")
	}
	if m.PromptTokensTotal != 0 || m.PredictedTokensTotal != 0 {
		t.Errorf("expected zero token counters for empty metrics, got prompt=%v predicted=%v",
			m.PromptTokensTotal, m.PredictedTokensTotal)
	}
}

func TestScrapeVLLM_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	_, err := ScrapeVLLM(context.Background(), client, server.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}
}

func TestScrapeVLLM_IgnoresLlamaCPPMetrics(t *testing.T) {
	// Pod mislabeled as vllm but actually serving llama.cpp returns zeros,
	// which is the correct fail-safe (no silent mis-attribution).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(llamacppMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeVLLM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeVLLM returned error: %v", err)
	}
	if m.PromptTokensTotal != 0 || m.PredictedTokensTotal != 0 {
		t.Errorf("expected zero for llama.cpp metrics parsed as vllm, got prompt=%v predicted=%v",
			m.PromptTokensTotal, m.PredictedTokensTotal)
	}
}
