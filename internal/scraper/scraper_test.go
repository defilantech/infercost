package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const dcgmMetricsText = `# HELP DCGM_FI_DEV_POWER_USAGE Power draw (in W).
# TYPE DCGM_FI_DEV_POWER_USAGE gauge
DCGM_FI_DEV_POWER_USAGE{gpu="0",UUID="GPU-1c3313b6-13f7-fa06-5651-680f8235330b",pci_bus_id="00000000:01:00.0",device="nvidia0",modelName="NVIDIA GeForce RTX 5060 Ti",Hostname="shadowstack",DCGM_FI_DRIVER_VERSION="580.126.09",container="llama-server",namespace="default",pod="openclaw-llm-6d8485c99f-vfdkm"} 3.471000
DCGM_FI_DEV_POWER_USAGE{gpu="1",UUID="GPU-66a08344-9a71-c851-38fa-2149e9381230",pci_bus_id="00000000:04:00.0",device="nvidia1",modelName="NVIDIA GeForce RTX 5060 Ti",Hostname="shadowstack",DCGM_FI_DRIVER_VERSION="580.126.09",container="llama-server",namespace="default",pod="openclaw-llm-6d8485c99f-vfdkm"} 3.281000
`

const llamacppMetricsText = `# HELP llamacpp:prompt_tokens_total Number of prompt tokens processed.
# TYPE llamacpp:prompt_tokens_total counter
llamacpp:prompt_tokens_total 107537
# HELP llamacpp:prompt_seconds_total Prompt process time
# TYPE llamacpp:prompt_seconds_total counter
llamacpp:prompt_seconds_total 164.579
# HELP llamacpp:tokens_predicted_total Number of generation tokens processed.
# TYPE llamacpp:tokens_predicted_total counter
llamacpp:tokens_predicted_total 297842
# HELP llamacpp:tokens_predicted_seconds_total Predict process time
# TYPE llamacpp:tokens_predicted_seconds_total counter
llamacpp:tokens_predicted_seconds_total 16116.7
# HELP llamacpp:n_decode_total Total number of llama_decode() calls
# TYPE llamacpp:n_decode_total counter
llamacpp:n_decode_total 281089
# HELP llamacpp:prompt_tokens_seconds Average prompt throughput in tokens/s.
# TYPE llamacpp:prompt_tokens_seconds gauge
llamacpp:prompt_tokens_seconds 653.407
# HELP llamacpp:predicted_tokens_seconds Average predicted throughput in tokens/s.
# TYPE llamacpp:predicted_tokens_seconds gauge
llamacpp:predicted_tokens_seconds 18.4803
# HELP llamacpp:requests_processing Number of requests processing.
# TYPE llamacpp:requests_processing gauge
llamacpp:requests_processing 0
# HELP llamacpp:requests_deferred Number of requests deferred.
# TYPE llamacpp:requests_deferred gauge
llamacpp:requests_deferred 0
`

func TestParseMetrics_DCGMFormat(t *testing.T) {
	samples, err := parseMetrics(strings.NewReader(dcgmMetricsText))
	if err != nil {
		t.Fatalf("parseMetrics returned error: %v", err)
	}

	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}

	gpu0Found := false
	gpu1Found := false
	for _, s := range samples {
		if s.Name != "DCGM_FI_DEV_POWER_USAGE" {
			t.Errorf("unexpected metric name: %q", s.Name)
		}
		switch s.Labels["gpu"] {
		case "0":
			gpu0Found = true
			if s.Value != 3.471 {
				t.Errorf("gpu 0 power = %v, want 3.471", s.Value)
			}
			if s.Labels["UUID"] != "GPU-1c3313b6-13f7-fa06-5651-680f8235330b" {
				t.Errorf("gpu 0 UUID = %q, want GPU-1c3313b6-13f7-fa06-5651-680f8235330b", s.Labels["UUID"])
			}
			if s.Labels["modelName"] != "NVIDIA GeForce RTX 5060 Ti" {
				t.Errorf("gpu 0 modelName = %q", s.Labels["modelName"])
			}
			if s.Labels["Hostname"] != "shadowstack" {
				t.Errorf("gpu 0 Hostname = %q", s.Labels["Hostname"])
			}
			if s.Labels["pod"] != "openclaw-llm-6d8485c99f-vfdkm" {
				t.Errorf("gpu 0 pod = %q", s.Labels["pod"])
			}
			if s.Labels["namespace"] != "default" {
				t.Errorf("gpu 0 namespace = %q", s.Labels["namespace"])
			}
		case "1":
			gpu1Found = true
			if s.Value != 3.281 {
				t.Errorf("gpu 1 power = %v, want 3.281", s.Value)
			}
			if s.Labels["UUID"] != "GPU-66a08344-9a71-c851-38fa-2149e9381230" {
				t.Errorf("gpu 1 UUID = %q", s.Labels["UUID"])
			}
		}
	}
	if !gpu0Found {
		t.Error("gpu 0 sample not found")
	}
	if !gpu1Found {
		t.Error("gpu 1 sample not found")
	}
}

func TestParseMetrics_LlamaCPPFormat(t *testing.T) {
	samples, err := parseMetrics(strings.NewReader(llamacppMetricsText))
	if err != nil {
		t.Fatalf("parseMetrics returned error: %v", err)
	}

	sampleMap := make(map[string]float64)
	for _, s := range samples {
		sampleMap[s.Name] = s.Value
	}

	expectedMetrics := map[string]float64{
		"llamacpp:prompt_tokens_total":            107537,
		"llamacpp:prompt_seconds_total":           164.579,
		"llamacpp:tokens_predicted_total":         297842,
		"llamacpp:tokens_predicted_seconds_total": 16116.7,
		"llamacpp:n_decode_total":                 281089,
		"llamacpp:prompt_tokens_seconds":          653.407,
		"llamacpp:predicted_tokens_seconds":       18.4803,
		"llamacpp:requests_processing":            0,
		"llamacpp:requests_deferred":              0,
	}

	for name, expectedVal := range expectedMetrics {
		val, ok := sampleMap[name]
		if !ok {
			t.Errorf("metric %q not found in parsed samples", name)
			continue
		}
		if val != expectedVal {
			t.Errorf("metric %q = %v, want %v", name, val, expectedVal)
		}
	}
}

func TestParseMetrics_EmptyInput(t *testing.T) {
	samples, err := parseMetrics(strings.NewReader(""))
	if err != nil {
		t.Fatalf("parseMetrics returned error for empty input: %v", err)
	}
	if len(samples) != 0 {
		t.Errorf("expected 0 samples for empty input, got %d", len(samples))
	}
}

func TestParseMetrics_CommentsOnly(t *testing.T) {
	input := `# HELP some_metric A help string
# TYPE some_metric gauge
`
	samples, err := parseMetrics(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseMetrics returned error: %v", err)
	}
	if len(samples) != 0 {
		t.Errorf("expected 0 samples for comments-only input, got %d", len(samples))
	}
}

func TestParseMetrics_UntypedMetric(t *testing.T) {
	input := `# HELP my_metric Some metric
# TYPE my_metric untyped
my_metric 42.5
`
	samples, err := parseMetrics(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseMetrics returned error: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}
	if samples[0].Value != 42.5 {
		t.Errorf("value = %v, want 42.5", samples[0].Value)
	}
}

func TestParseMetrics_HistogramSkipped(t *testing.T) {
	input := `# HELP http_duration_seconds Request duration
# TYPE http_duration_seconds histogram
http_duration_seconds_bucket{le="0.1"} 10
http_duration_seconds_bucket{le="0.5"} 20
http_duration_seconds_bucket{le="+Inf"} 30
http_duration_seconds_sum 15.5
http_duration_seconds_count 30
`
	samples, err := parseMetrics(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseMetrics returned error: %v", err)
	}
	if len(samples) != 0 {
		t.Errorf("expected 0 samples (histogram should be skipped), got %d", len(samples))
	}
}

func TestFilterByName(t *testing.T) {
	samples := []MetricSample{
		{Name: "metric_a", Value: 1.0},
		{Name: "metric_b", Value: 2.0},
		{Name: "metric_a", Value: 3.0},
		{Name: "metric_c", Value: 4.0},
	}

	tests := []struct {
		name      string
		filterBy  string
		wantCount int
	}{
		{name: "filter existing metric", filterBy: "metric_a", wantCount: 2},
		{name: "filter single match", filterBy: "metric_b", wantCount: 1},
		{name: "filter no match", filterBy: "metric_z", wantCount: 0},
		{name: "filter empty name", filterBy: "", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := FilterByName(samples, tt.filterBy)
			if len(filtered) != tt.wantCount {
				t.Errorf("FilterByName(%q) returned %d results, want %d", tt.filterBy, len(filtered), tt.wantCount)
			}
			for _, s := range filtered {
				if s.Name != tt.filterBy {
					t.Errorf("filtered sample has name %q, want %q", s.Name, tt.filterBy)
				}
			}
		})
	}
}

func TestFilterByName_NilSlice(t *testing.T) {
	filtered := FilterByName(nil, "anything")
	if len(filtered) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(filtered))
	}
}

func TestFilterByName_EmptySlice(t *testing.T) {
	filtered := FilterByName([]MetricSample{}, "anything")
	if len(filtered) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(filtered))
	}
}

func TestScrape_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(dcgmMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	samples, err := client.Scrape(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Scrape returned error: %v", err)
	}

	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}
}

func TestScrape_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	_, err := client.Scrape(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error message %q should contain 'status 500'", err.Error())
	}
}

func TestScrape_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	_, err := client.Scrape(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 404 response")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Errorf("error message %q should contain 'status 404'", err.Error())
	}
}

func TestScrape_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	samples, err := client.Scrape(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Scrape returned error for empty response: %v", err)
	}
	if len(samples) != 0 {
		t.Errorf("expected 0 samples for empty response, got %d", len(samples))
	}
}

func TestScrape_ConnectionRefused(t *testing.T) {
	client := NewClient(1 * time.Second)
	_, err := client.Scrape(context.Background(), "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestScrape_InvalidURL(t *testing.T) {
	client := NewClient(1 * time.Second)
	_, err := client.Scrape(context.Background(), "://not-a-url")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestScrape_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(10 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Scrape(ctx, server.URL)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestScrape_LlamaCPPMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(llamacppMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	samples, err := client.Scrape(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Scrape returned error: %v", err)
	}

	if len(samples) == 0 {
		t.Fatal("expected non-empty samples from llama.cpp metrics")
	}

	promptTokens := FilterByName(samples, "llamacpp:prompt_tokens_total")
	if len(promptTokens) != 1 {
		t.Fatalf("expected 1 prompt_tokens_total sample, got %d", len(promptTokens))
	}
	if promptTokens[0].Value != 107537 {
		t.Errorf("prompt_tokens_total = %v, want 107537", promptTokens[0].Value)
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient(30 * time.Second)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.httpClient == nil {
		t.Fatal("NewClient returned client with nil httpClient")
	}
	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("client timeout = %v, want 30s", client.httpClient.Timeout)
	}
}
