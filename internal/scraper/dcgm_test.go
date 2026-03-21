package scraper

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScrapeDCGM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(dcgmMetricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	readings, err := ScrapeDCGM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeDCGM returned error: %v", err)
	}

	if len(readings) != 2 {
		t.Fatalf("expected 2 GPU readings, got %d", len(readings))
	}

	readingsByGPU := make(map[string]GPUPowerReading)
	for _, r := range readings {
		readingsByGPU[r.GPUID] = r
	}

	gpu0, ok := readingsByGPU["0"]
	if !ok {
		t.Fatal("GPU 0 reading not found")
	}
	if gpu0.UUID != "GPU-1c3313b6-13f7-fa06-5651-680f8235330b" {
		t.Errorf("GPU 0 UUID = %q", gpu0.UUID)
	}
	if gpu0.ModelName != "NVIDIA GeForce RTX 5060 Ti" {
		t.Errorf("GPU 0 ModelName = %q", gpu0.ModelName)
	}
	if gpu0.Node != "shadowstack" {
		t.Errorf("GPU 0 Node = %q", gpu0.Node)
	}
	if gpu0.Pod != "openclaw-llm-6d8485c99f-vfdkm" {
		t.Errorf("GPU 0 Pod = %q", gpu0.Pod)
	}
	if gpu0.Namespace != "default" {
		t.Errorf("GPU 0 Namespace = %q", gpu0.Namespace)
	}
	if gpu0.PowerW != 3.471 {
		t.Errorf("GPU 0 PowerW = %v, want 3.471", gpu0.PowerW)
	}

	gpu1, ok := readingsByGPU["1"]
	if !ok {
		t.Fatal("GPU 1 reading not found")
	}
	if gpu1.UUID != "GPU-66a08344-9a71-c851-38fa-2149e9381230" {
		t.Errorf("GPU 1 UUID = %q", gpu1.UUID)
	}
	if gpu1.PowerW != 3.281 {
		t.Errorf("GPU 1 PowerW = %v, want 3.281", gpu1.PowerW)
	}
}

func TestScrapeDCGM_NoMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	readings, err := ScrapeDCGM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeDCGM returned error: %v", err)
	}
	if len(readings) != 0 {
		t.Errorf("expected 0 readings for empty metrics, got %d", len(readings))
	}
}

func TestScrapeDCGM_NonDCGMMetrics(t *testing.T) {
	metricsText := `# HELP node_cpu_seconds_total Seconds the CPUs spent in each mode.
# TYPE node_cpu_seconds_total counter
node_cpu_seconds_total{cpu="0",mode="idle"} 12345.67
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	readings, err := ScrapeDCGM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeDCGM returned error: %v", err)
	}
	if len(readings) != 0 {
		t.Errorf("expected 0 readings for non-DCGM metrics, got %d", len(readings))
	}
}

func TestScrapeDCGM_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	_, err := ScrapeDCGM(context.Background(), client, server.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 503 response")
	}
}

func TestScrapeDCGM_SingleGPU(t *testing.T) {
	metricsText := `# HELP DCGM_FI_DEV_POWER_USAGE Power draw (in W).
# TYPE DCGM_FI_DEV_POWER_USAGE gauge
DCGM_FI_DEV_POWER_USAGE{gpu="0",UUID="GPU-abc123",modelName="NVIDIA A100",Hostname="node1",namespace="ml",pod="train-pod-1"} 250.5
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	readings, err := ScrapeDCGM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeDCGM returned error: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(readings))
	}

	r := readings[0]
	if r.GPUID != "0" {
		t.Errorf("GPUID = %q, want '0'", r.GPUID)
	}
	if r.UUID != "GPU-abc123" {
		t.Errorf("UUID = %q", r.UUID)
	}
	if r.ModelName != "NVIDIA A100" {
		t.Errorf("ModelName = %q", r.ModelName)
	}
	if r.Node != "node1" {
		t.Errorf("Node = %q", r.Node)
	}
	if r.Pod != "train-pod-1" {
		t.Errorf("Pod = %q", r.Pod)
	}
	if r.Namespace != "ml" {
		t.Errorf("Namespace = %q", r.Namespace)
	}
	if r.PowerW != 250.5 {
		t.Errorf("PowerW = %v, want 250.5", r.PowerW)
	}
}

func TestScrapeDCGM_MixedMetrics(t *testing.T) {
	metricsText := `# HELP DCGM_FI_DEV_POWER_USAGE Power draw (in W).
# TYPE DCGM_FI_DEV_POWER_USAGE gauge
DCGM_FI_DEV_POWER_USAGE{gpu="0",UUID="GPU-abc",modelName="RTX 5060 Ti",Hostname="host1",namespace="default",pod="pod1"} 150.0
# HELP DCGM_FI_DEV_GPU_TEMP GPU temperature.
# TYPE DCGM_FI_DEV_GPU_TEMP gauge
DCGM_FI_DEV_GPU_TEMP{gpu="0",UUID="GPU-abc"} 65.0
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	readings, err := ScrapeDCGM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeDCGM returned error: %v", err)
	}
	if len(readings) != 1 {
		t.Errorf("expected 1 power reading (filtering out GPU_TEMP), got %d", len(readings))
	}
}

func TestTotalPowerWatts(t *testing.T) {
	tests := []struct {
		name     string
		readings []GPUPowerReading
		want     float64
	}{
		{
			name: "two GPUs",
			readings: []GPUPowerReading{
				{GPUID: "0", PowerW: 3.471},
				{GPUID: "1", PowerW: 3.281},
			},
			want: 6.752,
		},
		{
			name: "single GPU",
			readings: []GPUPowerReading{
				{GPUID: "0", PowerW: 250.0},
			},
			want: 250.0,
		},
		{
			name:     "empty readings",
			readings: []GPUPowerReading{},
			want:     0.0,
		},
		{
			name:     "nil readings",
			readings: nil,
			want:     0.0,
		},
		{
			name: "four GPUs high power",
			readings: []GPUPowerReading{
				{GPUID: "0", PowerW: 300.0},
				{GPUID: "1", PowerW: 310.5},
				{GPUID: "2", PowerW: 295.3},
				{GPUID: "3", PowerW: 305.2},
			},
			want: 1211.0,
		},
		{
			name: "zero power readings",
			readings: []GPUPowerReading{
				{GPUID: "0", PowerW: 0.0},
				{GPUID: "1", PowerW: 0.0},
			},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TotalPowerWatts(tt.readings)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("TotalPowerWatts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScrapeDCGM_MissingLabels(t *testing.T) {
	metricsText := `# HELP DCGM_FI_DEV_POWER_USAGE Power draw (in W).
# TYPE DCGM_FI_DEV_POWER_USAGE gauge
DCGM_FI_DEV_POWER_USAGE{gpu="0"} 100.0
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metricsText))
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	readings, err := ScrapeDCGM(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("ScrapeDCGM returned error: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(readings))
	}

	r := readings[0]
	if r.GPUID != "0" {
		t.Errorf("GPUID = %q, want '0'", r.GPUID)
	}
	if r.UUID != "" {
		t.Errorf("UUID = %q, want empty string for missing label", r.UUID)
	}
	if r.ModelName != "" {
		t.Errorf("ModelName = %q, want empty string for missing label", r.ModelName)
	}
	if r.Node != "" {
		t.Errorf("Node = %q, want empty string for missing label", r.Node)
	}
	if r.Pod != "" {
		t.Errorf("Pod = %q, want empty string for missing label", r.Pod)
	}
	if r.Namespace != "" {
		t.Errorf("Namespace = %q, want empty string for missing label", r.Namespace)
	}
	if r.PowerW != 100.0 {
		t.Errorf("PowerW = %v, want 100.0", r.PowerW)
	}
}
