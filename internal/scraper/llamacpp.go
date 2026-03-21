package scraper

import (
	"context"
	"fmt"
)

const (
	// LlamaCPPPromptTokensMetric is the cumulative prompt (input) token counter.
	LlamaCPPPromptTokensMetric = "llamacpp:prompt_tokens_total"
	// LlamaCPPPredictedTokensMetric is the cumulative predicted (output) token counter.
	LlamaCPPPredictedTokensMetric = "llamacpp:tokens_predicted_total"
	// LlamaCPPPredictedTokensSecondsMetric is the tokens/sec generation speed.
	LlamaCPPPredictedTokensSecondsMetric = "llamacpp:predicted_tokens_seconds"
	// LlamaCPPPromptTokensSecondsMetric is the tokens/sec prompt processing speed.
	LlamaCPPPromptTokensSecondsMetric = "llamacpp:prompt_tokens_seconds"
	// LlamaCPPRequestsProcessingMetric is the number of in-flight requests.
	LlamaCPPRequestsProcessingMetric = "llamacpp:requests_processing"
)

// InferenceMetrics represents token counters and throughput for an inference pod.
type InferenceMetrics struct {
	Pod       string
	Namespace string
	Model     string // Set externally from LLMKube labels or user config

	PromptTokensTotal     float64 // Cumulative input tokens
	PredictedTokensTotal  float64 // Cumulative output tokens
	PromptTokensPerSec    float64 // Current prompt processing speed
	PredictedTokensPerSec float64 // Current generation speed
	RequestsProcessing    float64 // In-flight requests (activity indicator)
}

// ScrapeLlamaCPP fetches inference metrics from a llama.cpp /metrics endpoint.
func ScrapeLlamaCPP(ctx context.Context, client *Client, endpoint string) (*InferenceMetrics, error) {
	samples, err := client.Scrape(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("scraping llama.cpp: %w", err)
	}

	m := &InferenceMetrics{}
	for _, s := range samples {
		switch s.Name {
		case LlamaCPPPromptTokensMetric:
			m.PromptTokensTotal = s.Value
		case LlamaCPPPredictedTokensMetric:
			m.PredictedTokensTotal = s.Value
		case LlamaCPPPromptTokensSecondsMetric:
			m.PromptTokensPerSec = s.Value
		case LlamaCPPPredictedTokensSecondsMetric:
			m.PredictedTokensPerSec = s.Value
		case LlamaCPPRequestsProcessingMetric:
			m.RequestsProcessing = s.Value
		}
	}
	return m, nil
}
