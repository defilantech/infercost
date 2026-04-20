package calculator

import "time"

// HardwareCosts holds the static cost parameters from a CostProfile.
type HardwareCosts struct {
	PurchasePriceUSD          float64
	AmortizationYears         int32
	MaintenancePercentPerYear float64
	RatePerKWh                float64
	PUEFactor                 float64
}

// CostResult contains the computed cost metrics.
type CostResult struct {
	// Static costs (don't change with power draw)
	AmortizationPerHour float64

	// Dynamic costs (depend on real-time power draw)
	ElectricityPerHour float64
	TotalPerHour       float64

	// Token economics (depend on throughput)
	CostPerToken         float64
	CostPerMillionTokens float64

	// Efficiency
	GPUEfficiencyRatio float64 // 0-1, fraction of time GPUs are active
}

// ComputeHourlyCost calculates the hourly infrastructure cost from hardware economics
// and current power draw.
func ComputeHourlyCost(hw HardwareCosts, powerDrawWatts float64) (amortization, electricity, total float64) {
	hoursPerYear := float64(8760)
	totalYears := float64(hw.AmortizationYears)

	// Amortization: (purchase_price * (1 + maintenance%)) / total_hours
	amortization = (hw.PurchasePriceUSD * (1 + hw.MaintenancePercentPerYear)) / (totalYears * hoursPerYear)

	// Electricity: power_kw * rate * PUE
	powerKW := powerDrawWatts / 1000.0
	pue := hw.PUEFactor
	if pue == 0 {
		pue = 1.0
	}
	electricity = powerKW * hw.RatePerKWh * pue

	total = amortization + electricity
	return
}

// TokenSnapshot tracks token counters at a point in time for delta computation.
type TokenSnapshot struct {
	Timestamp    time.Time
	InputTokens  float64
	OutputTokens float64
}

// ComputeTokenRate calculates tokens per hour from two snapshots.
func ComputeTokenRate(prev, curr TokenSnapshot) (inputPerHour, outputPerHour, totalPerHour float64) {
	elapsed := curr.Timestamp.Sub(prev.Timestamp)
	if elapsed <= 0 {
		return 0, 0, 0
	}

	hours := elapsed.Hours()
	inputDelta := curr.InputTokens - prev.InputTokens
	outputDelta := curr.OutputTokens - prev.OutputTokens

	if inputDelta < 0 {
		inputDelta = curr.InputTokens // Counter reset
	}
	if outputDelta < 0 {
		outputDelta = curr.OutputTokens // Counter reset
	}

	inputPerHour = inputDelta / hours
	outputPerHour = outputDelta / hours
	totalPerHour = inputPerHour + outputPerHour
	return
}

// ComputeCostPerToken derives per-token cost from hourly cost and token throughput.
func ComputeCostPerToken(hourlyCost, tokensPerHour float64) float64 {
	if tokensPerHour <= 0 {
		return 0
	}
	return hourlyCost / tokensPerHour
}

// ComputeFull runs the complete cost calculation pipeline.
func ComputeFull(hw HardwareCosts, powerDrawWatts float64, prev, curr TokenSnapshot) CostResult {
	amort, elec, total := ComputeHourlyCost(hw, powerDrawWatts)
	inputRate, outputRate, totalRate := ComputeTokenRate(prev, curr)

	cpt := ComputeCostPerToken(total, totalRate)

	// Efficiency: if GPUs are drawing meaningful power (>10W), they're "on".
	// If tokens are flowing, they're "active".
	var efficiency float64
	if powerDrawWatts > 10 && totalRate > 0 {
		// Simple heuristic: ratio of actual throughput to theoretical max.
		// For now, just indicate whether the GPU is producing tokens.
		efficiency = 1.0
	} else if powerDrawWatts > 10 {
		// GPU is on but idle (no tokens flowing).
		efficiency = 0.0
	}

	_ = inputRate
	_ = outputRate

	return CostResult{
		AmortizationPerHour:  amort,
		ElectricityPerHour:   elec,
		TotalPerHour:         total,
		CostPerToken:         cpt,
		CostPerMillionTokens: cpt * 1_000_000,
		GPUEfficiencyRatio:   efficiency,
	}
}

// CloudPricing holds the per-token pricing for a cloud provider model.
type CloudPricing struct {
	Provider         string  `json:"provider" yaml:"provider"`
	Model            string  `json:"model" yaml:"model"`
	InputPerMillion  float64 `json:"inputPerMillion" yaml:"inputPerMillion"`   // USD per 1M input tokens
	OutputPerMillion float64 `json:"outputPerMillion" yaml:"outputPerMillion"` // USD per 1M output tokens
}

// DefaultCloudPricing is defined in pricing.go. It loads from the canonical
// config/pricing/cloud-pricing.yaml (embedded at build time, drift-guarded by
// TestEmbeddedPricingMatchesCanonical) and honors a runtime override set via
// SetOverrideCloudPricing from --pricing-file or a ConfigMap mount.

// CloudComparison computes what the same tokens would have cost on a cloud provider.
type CloudComparison struct {
	Provider       string
	Model          string
	CloudCostUSD   float64
	OnPremCostUSD  float64
	SavingsUSD     float64
	SavingsPercent float64
}

// CompareToCloud computes savings for a given number of tokens against cloud pricing.
func CompareToCloud(inputTokens, outputTokens int64, onPremCostUSD float64, pricing []CloudPricing) []CloudComparison {
	results := make([]CloudComparison, 0, len(pricing))
	for _, p := range pricing {
		cloudCost := (float64(inputTokens) / 1_000_000 * p.InputPerMillion) +
			(float64(outputTokens) / 1_000_000 * p.OutputPerMillion)

		savings := cloudCost - onPremCostUSD
		var savingsPct float64
		if cloudCost > 0 {
			savingsPct = (savings / cloudCost) * 100
		}

		results = append(results, CloudComparison{
			Provider:       p.Provider,
			Model:          p.Model,
			CloudCostUSD:   cloudCost,
			OnPremCostUSD:  onPremCostUSD,
			SavingsUSD:     savings,
			SavingsPercent: savingsPct,
		})
	}
	return results
}
