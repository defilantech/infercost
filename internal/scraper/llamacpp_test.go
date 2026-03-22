package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScrapeLlamaCPP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(llamacppMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeLlamaCPP(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeLlamaCPP returned error: %v", err)
	}

	if m.PromptTokensTotal != 107537 {
		t.Errorf("PromptTokensTotal = %v, want 107537", m.PromptTokensTotal)
	}
	if m.PredictedTokensTotal != 297842 {
		t.Errorf("PredictedTokensTotal = %v, want 297842", m.PredictedTokensTotal)
	}
	if m.PromptTokensPerSec != 653.407 {
		t.Errorf("PromptTokensPerSec = %v, want 653.407", m.PromptTokensPerSec)
	}
	if m.PredictedTokensPerSec != 18.4803 {
		t.Errorf("PredictedTokensPerSec = %v, want 18.4803", m.PredictedTokensPerSec)
	}
	if m.RequestsProcessing != 0 {
		t.Errorf("RequestsProcessing = %v, want 0", m.RequestsProcessing)
	}
}

func TestScrapeLlamaCPP_EmptyMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeLlamaCPP(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeLlamaCPP returned error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil InferenceMetrics for empty response")
	}
	if m.PromptTokensTotal != 0 {
		t.Errorf("PromptTokensTotal = %v, want 0 for empty metrics", m.PromptTokensTotal)
	}
	if m.PredictedTokensTotal != 0 {
		t.Errorf("PredictedTokensTotal = %v, want 0 for empty metrics", m.PredictedTokensTotal)
	}
	if m.PromptTokensPerSec != 0 {
		t.Errorf("PromptTokensPerSec = %v, want 0 for empty metrics", m.PromptTokensPerSec)
	}
	if m.PredictedTokensPerSec != 0 {
		t.Errorf("PredictedTokensPerSec = %v, want 0 for empty metrics", m.PredictedTokensPerSec)
	}
	if m.RequestsProcessing != 0 {
		t.Errorf("RequestsProcessing = %v, want 0 for empty metrics", m.RequestsProcessing)
	}
}

func TestScrapeLlamaCPP_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	_, err := ScrapeLlamaCPP(context.Background(), client, server.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}
}

func TestScrapeLlamaCPP_PartialMetrics(t *testing.T) {
	partialMetrics := `# HELP llamacpp:prompt_tokens_total Number of prompt tokens processed.
# TYPE llamacpp:prompt_tokens_total counter
llamacpp:prompt_tokens_total 5000
# HELP llamacpp:predicted_tokens_seconds Average predicted throughput in tokens/s.
# TYPE llamacpp:predicted_tokens_seconds gauge
llamacpp:predicted_tokens_seconds 22.5
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(partialMetrics))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeLlamaCPP(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeLlamaCPP returned error: %v", err)
	}

	if m.PromptTokensTotal != 5000 {
		t.Errorf("PromptTokensTotal = %v, want 5000", m.PromptTokensTotal)
	}
	if m.PredictedTokensPerSec != 22.5 {
		t.Errorf("PredictedTokensPerSec = %v, want 22.5", m.PredictedTokensPerSec)
	}
	if m.PredictedTokensTotal != 0 {
		t.Errorf("PredictedTokensTotal = %v, want 0 for missing metric", m.PredictedTokensTotal)
	}
	if m.PromptTokensPerSec != 0 {
		t.Errorf("PromptTokensPerSec = %v, want 0 for missing metric", m.PromptTokensPerSec)
	}
	if m.RequestsProcessing != 0 {
		t.Errorf("RequestsProcessing = %v, want 0 for missing metric", m.RequestsProcessing)
	}
}

func TestScrapeLlamaCPP_NonLlamaCPPMetrics(t *testing.T) {
	otherMetrics := `# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 42
# HELP process_cpu_seconds_total Total user and system CPU time spent in seconds.
# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total 123.45
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(otherMetrics))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeLlamaCPP(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeLlamaCPP returned error: %v", err)
	}
	if m.PromptTokensTotal != 0 || m.PredictedTokensTotal != 0 {
		t.Errorf("expected zero token counters for non-llama.cpp metrics, got prompt=%v predicted=%v",
			m.PromptTokensTotal, m.PredictedTokensTotal)
	}
}

func TestScrapeLlamaCPP_ActiveRequests(t *testing.T) {
	metricsText := `# HELP llamacpp:prompt_tokens_total Number of prompt tokens processed.
# TYPE llamacpp:prompt_tokens_total counter
llamacpp:prompt_tokens_total 50000
# HELP llamacpp:tokens_predicted_total Number of generation tokens processed.
# TYPE llamacpp:tokens_predicted_total counter
llamacpp:tokens_predicted_total 120000
# HELP llamacpp:prompt_tokens_seconds Average prompt throughput in tokens/s.
# TYPE llamacpp:prompt_tokens_seconds gauge
llamacpp:prompt_tokens_seconds 800.0
# HELP llamacpp:predicted_tokens_seconds Average predicted throughput in tokens/s.
# TYPE llamacpp:predicted_tokens_seconds gauge
llamacpp:predicted_tokens_seconds 25.0
# HELP llamacpp:requests_processing Number of requests processing.
# TYPE llamacpp:requests_processing gauge
llamacpp:requests_processing 3
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeLlamaCPP(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeLlamaCPP returned error: %v", err)
	}

	if m.RequestsProcessing != 3 {
		t.Errorf("RequestsProcessing = %v, want 3", m.RequestsProcessing)
	}
	if m.PromptTokensTotal != 50000 {
		t.Errorf("PromptTokensTotal = %v, want 50000", m.PromptTokensTotal)
	}
	if m.PredictedTokensTotal != 120000 {
		t.Errorf("PredictedTokensTotal = %v, want 120000", m.PredictedTokensTotal)
	}
	if m.PromptTokensPerSec != 800.0 {
		t.Errorf("PromptTokensPerSec = %v, want 800.0", m.PromptTokensPerSec)
	}
	if m.PredictedTokensPerSec != 25.0 {
		t.Errorf("PredictedTokensPerSec = %v, want 25.0", m.PredictedTokensPerSec)
	}
}

func TestScrapeLlamaCPP_PodAndNamespaceNotSetByScraper(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(llamacppMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	m, err := ScrapeLlamaCPP(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeLlamaCPP returned error: %v", err)
	}

	if m.Pod != "" {
		t.Errorf("Pod = %q, expected empty (set externally, not by scraper)", m.Pod)
	}
	if m.Namespace != "" {
		t.Errorf("Namespace = %q, expected empty (set externally, not by scraper)", m.Namespace)
	}
	if m.Model != "" {
		t.Errorf("Model = %q, expected empty (set externally, not by scraper)", m.Model)
	}
}

func TestLlamaCPPMetricConstants(t *testing.T) {
	if LlamaCPPPromptTokensMetric != "llamacpp:prompt_tokens_total" {
		t.Errorf("LlamaCPPPromptTokensMetric = %q", LlamaCPPPromptTokensMetric)
	}
	if LlamaCPPPredictedTokensMetric != "llamacpp:tokens_predicted_total" {
		t.Errorf("LlamaCPPPredictedTokensMetric = %q", LlamaCPPPredictedTokensMetric)
	}
	if LlamaCPPPredictedTokensSecondsMetric != "llamacpp:predicted_tokens_seconds" {
		t.Errorf("LlamaCPPPredictedTokensSecondsMetric = %q", LlamaCPPPredictedTokensSecondsMetric)
	}
	if LlamaCPPPromptTokensSecondsMetric != "llamacpp:prompt_tokens_seconds" {
		t.Errorf("LlamaCPPPromptTokensSecondsMetric = %q", LlamaCPPPromptTokensSecondsMetric)
	}
	if LlamaCPPRequestsProcessingMetric != "llamacpp:requests_processing" {
		t.Errorf("LlamaCPPRequestsProcessingMetric = %q", LlamaCPPRequestsProcessingMetric)
	}
}
