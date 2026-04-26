package scraper

import (
	"context"
	"fmt"
)

// Apple Silicon power-gauge metric names exposed by the LLMKube Metal Agent
// when started with --apple-power-enabled. Source: defilantech/llmkube
// pkg/agent/agentmetrics.go. The gauges are unlabeled — one float per
// metric — because powermetrics reports a single SoC-wide reading.
const (
	ApplePowerCombinedMetric = "llmkube_metal_agent_apple_power_combined_watts"
	ApplePowerGPUMetric      = "llmkube_metal_agent_apple_power_gpu_watts"
	ApplePowerCPUMetric      = "llmkube_metal_agent_apple_power_cpu_watts"
	ApplePowerANEMetric      = "llmkube_metal_agent_apple_power_ane_watts"
)

// ApplePowerReading is one snapshot of Apple Silicon SoC power, in watts.
// Combined is what `Sampler.Record` should consume because it includes the
// full CPU + GPU + ANE package draw — the same scope DCGM reports for an
// NVIDIA card. The component fields are populated for visibility (dashboards,
// debugging) but cost math should use Combined.
type ApplePowerReading struct {
	CombinedW float64
	GPUW      float64
	CPUW      float64
	ANEW      float64
}

// ScrapeApplePower fetches the LLMKube Metal Agent's apple_power_*_watts
// gauges from a Prometheus-format /metrics endpoint and returns the most
// recent sample. Returns a zero-valued reading (not an error) when the
// endpoint is reachable but the agent isn't publishing power data — that
// happens when the operator forgot to set --apple-power-enabled or
// installed the sudoers entry incorrectly. The caller distinguishes
// "endpoint down" (error) from "sampler off" (zero combined value) so the
// status condition can carry an actionable reason.
func ScrapeApplePower(ctx context.Context, client *Client, endpoint string) (ApplePowerReading, error) {
	samples, err := client.Scrape(ctx, endpoint)
	if err != nil {
		return ApplePowerReading{}, fmt.Errorf("scraping Metal agent: %w", err)
	}

	var r ApplePowerReading
	for _, s := range FilterByName(samples, ApplePowerCombinedMetric) {
		r.CombinedW = s.Value
	}
	for _, s := range FilterByName(samples, ApplePowerGPUMetric) {
		r.GPUW = s.Value
	}
	for _, s := range FilterByName(samples, ApplePowerCPUMetric) {
		r.CPUW = s.Value
	}
	for _, s := range FilterByName(samples, ApplePowerANEMetric) {
		r.ANEW = s.Value
	}
	return r, nil
}
