package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// metalMetricsText is what the LLMKube Metal Agent's /metrics endpoint emits
// when --apple-power-enabled is set and powermetrics is running. Captured
// verbatim from a real M5 Max session so the test fails loudly if the agent
// renames or reshapes a gauge in a future release.
const metalMetricsText = `# HELP llmkube_metal_agent_apple_power_combined_watts Combined CPU + GPU + ANE package power in watts from macOS powermetrics. Zero unless the agent is run with --apple-power-enabled and a NOPASSWD sudoers entry for /usr/bin/powermetrics is installed.
# TYPE llmkube_metal_agent_apple_power_combined_watts gauge
llmkube_metal_agent_apple_power_combined_watts 42.318
# HELP llmkube_metal_agent_apple_power_gpu_watts GPU subsystem power in watts from macOS powermetrics. Zero unless --apple-power-enabled.
# TYPE llmkube_metal_agent_apple_power_gpu_watts gauge
llmkube_metal_agent_apple_power_gpu_watts 31.205
# HELP llmkube_metal_agent_apple_power_cpu_watts CPU subsystem power in watts from macOS powermetrics. Zero unless --apple-power-enabled.
# TYPE llmkube_metal_agent_apple_power_cpu_watts gauge
llmkube_metal_agent_apple_power_cpu_watts 11.113
# HELP llmkube_metal_agent_apple_power_ane_watts Apple Neural Engine power in watts from macOS powermetrics. Zero unless --apple-power-enabled.
# TYPE llmkube_metal_agent_apple_power_ane_watts gauge
llmkube_metal_agent_apple_power_ane_watts 0
`

// metalMetricsTextSamplerDisabled is what the agent emits when
// --apple-power-enabled is false: the gauges are still registered but read
// as zero. InferCost must distinguish this from an unreachable endpoint so
// the status condition can recommend "enable the sampler" rather than
// "check the network".
const metalMetricsTextSamplerDisabled = `# HELP llmkube_metal_agent_apple_power_combined_watts Combined CPU + GPU + ANE package power in watts from macOS powermetrics.
# TYPE llmkube_metal_agent_apple_power_combined_watts gauge
llmkube_metal_agent_apple_power_combined_watts 0
# HELP llmkube_metal_agent_apple_power_gpu_watts GPU subsystem power in watts from macOS powermetrics.
# TYPE llmkube_metal_agent_apple_power_gpu_watts gauge
llmkube_metal_agent_apple_power_gpu_watts 0
# HELP llmkube_metal_agent_apple_power_cpu_watts CPU subsystem power in watts from macOS powermetrics.
# TYPE llmkube_metal_agent_apple_power_cpu_watts gauge
llmkube_metal_agent_apple_power_cpu_watts 0
# HELP llmkube_metal_agent_apple_power_ane_watts Apple Neural Engine power in watts from macOS powermetrics.
# TYPE llmkube_metal_agent_apple_power_ane_watts gauge
llmkube_metal_agent_apple_power_ane_watts 0
`

func TestScrapeApplePower(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metalMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	r, err := ScrapeApplePower(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeApplePower returned error: %v", err)
	}

	if r.CombinedW != 42.318 {
		t.Errorf("CombinedW = %v, want 42.318", r.CombinedW)
	}
	if r.GPUW != 31.205 {
		t.Errorf("GPUW = %v, want 31.205", r.GPUW)
	}
	if r.CPUW != 11.113 {
		t.Errorf("CPUW = %v, want 11.113", r.CPUW)
	}
	if r.ANEW != 0 {
		t.Errorf("ANEW = %v, want 0", r.ANEW)
	}
}

func TestScrapeApplePower_SamplerDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metalMetricsTextSamplerDisabled))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	r, err := ScrapeApplePower(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeApplePower returned error: %v", err)
	}
	// Sampler-off must be a *successful* scrape returning zeros, not an error.
	// The reconciler uses CombinedW == 0 as the "agent reachable but sampler
	// off" signal to emit a different condition reason than a network failure.
	if r.CombinedW != 0 {
		t.Errorf("expected CombinedW=0 when sampler disabled, got %v", r.CombinedW)
	}
}

func TestScrapeApplePower_NoMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	r, err := ScrapeApplePower(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeApplePower returned error: %v", err)
	}
	if r != (ApplePowerReading{}) {
		t.Errorf("expected zero reading for empty metrics, got %+v", r)
	}
}

func TestScrapeApplePower_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	_, err := ScrapeApplePower(context.Background(), client, server.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 503 response")
	}
}

// TestScrapeApplePower_OnlyCombinedPresent guards the case where a future
// LLMKube release stops emitting one of the component gauges. The scraper
// must keep working from whatever it can find — Combined is the only field
// the cost math needs.
func TestScrapeApplePower_OnlyCombinedPresent(t *testing.T) {
	metricsText := `# HELP llmkube_metal_agent_apple_power_combined_watts Combined CPU + GPU + ANE package power in watts from macOS powermetrics.
# TYPE llmkube_metal_agent_apple_power_combined_watts gauge
llmkube_metal_agent_apple_power_combined_watts 17.5
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	r, err := ScrapeApplePower(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeApplePower returned error: %v", err)
	}
	if r.CombinedW != 17.5 {
		t.Errorf("CombinedW = %v, want 17.5", r.CombinedW)
	}
	if r.GPUW != 0 || r.CPUW != 0 || r.ANEW != 0 {
		t.Errorf("expected zero component readings when only combined is published, got %+v", r)
	}
}
