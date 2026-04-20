package scraper

import (
	"context"
	"fmt"
)

const (
	// VLLMPromptTokensMetric is the cumulative prompt (input) token counter.
	VLLMPromptTokensMetric = "vllm:prompt_tokens_total"
	// VLLMGenerationTokensMetric is the cumulative generated (output) token counter.
	VLLMGenerationTokensMetric = "vllm:generation_tokens_total"
	// VLLMRequestsRunningMetric is the number of in-flight requests.
	VLLMRequestsRunningMetric = "vllm:num_requests_running"
	// VLLMRequestsWaitingMetric is the number of queued requests waiting for a slot.
	VLLMRequestsWaitingMetric = "vllm:num_requests_waiting"
	// VLLMAvgGenerationThroughputMetric is the average generation throughput
	// in tokens/sec. This metric is exposed directly by older vLLM versions;
	// newer versions derive it from per-request histograms. The scraper reads
	// it if present and leaves PredictedTokensPerSec at zero otherwise.
	VLLMAvgGenerationThroughputMetric = "vllm:avg_generation_throughput_toks_per_s"
	// VLLMAvgPromptThroughputMetric is the average prompt throughput in tokens/sec,
	// exposed by older vLLM versions.
	VLLMAvgPromptThroughputMetric = "vllm:avg_prompt_throughput_toks_per_s"
)

// ScrapeVLLM fetches inference metrics from a vLLM /metrics endpoint.
//
// vLLM labels its metrics with model_name so a single pod may serve multiple
// models; the scraper aggregates counters across all label sets it observes
// so the returned InferenceMetrics reflects the whole pod. Gauges (requests
// running/waiting, throughput) are summed for the same reason — a pod serving
// two models has the combined in-flight request count.
func ScrapeVLLM(ctx context.Context, client *Client, endpoint string) (*InferenceMetrics, error) {
	samples, err := client.Scrape(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("scraping vllm: %w", err)
	}

	m := &InferenceMetrics{}
	for _, s := range samples {
		switch s.Name {
		case VLLMPromptTokensMetric:
			m.PromptTokensTotal += s.Value
		case VLLMGenerationTokensMetric:
			m.PredictedTokensTotal += s.Value
		case VLLMRequestsRunningMetric:
			m.RequestsProcessing += s.Value
		case VLLMAvgPromptThroughputMetric:
			m.PromptTokensPerSec += s.Value
		case VLLMAvgGenerationThroughputMetric:
			m.PredictedTokensPerSec += s.Value
		}
	}
	return m, nil
}
