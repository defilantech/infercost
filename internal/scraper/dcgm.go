package scraper

import (
	"context"
	"fmt"
)

const (
	// DCGMPowerUsageMetric is the DCGM metric for GPU power draw in watts.
	DCGMPowerUsageMetric = "DCGM_FI_DEV_POWER_USAGE"
)

// GPUPowerReading represents power draw for a single GPU.
type GPUPowerReading struct {
	GPUID     string  // GPU index (e.g. "0", "1")
	UUID      string  // GPU UUID
	ModelName string  // e.g. "NVIDIA GeForce RTX 5060 Ti"
	Node      string  // Hostname
	Pod       string  // Pod using this GPU
	Namespace string  // Pod namespace
	PowerW    float64 // Current power draw in watts
}

// ScrapeDCGM fetches GPU power metrics from a DCGM exporter endpoint.
func ScrapeDCGM(ctx context.Context, client *Client, endpoint string) ([]GPUPowerReading, error) {
	samples, err := client.Scrape(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("scraping DCGM: %w", err)
	}

	powerSamples := FilterByName(samples, DCGMPowerUsageMetric)
	readings := make([]GPUPowerReading, 0, len(powerSamples))
	for _, s := range powerSamples {
		readings = append(readings, GPUPowerReading{
			GPUID:     s.Labels["gpu"],
			UUID:      s.Labels["UUID"],
			ModelName: s.Labels["modelName"],
			Node:      s.Labels["Hostname"],
			Pod:       s.Labels["pod"],
			Namespace: s.Labels["namespace"],
			PowerW:    s.Value,
		})
	}
	return readings, nil
}

// TotalPowerWatts returns the sum of power draw across all GPUs.
func TotalPowerWatts(readings []GPUPowerReading) float64 {
	var total float64
	for _, r := range readings {
		total += r.PowerW
	}
	return total
}
