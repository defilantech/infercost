package calculator

import (
	"math"
	"testing"
	"time"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9
}

func TestComputeHourlyCost(t *testing.T) {
	tests := []struct {
		name           string
		hw             HardwareCosts
		powerDrawWatts float64
		wantAmort      float64
		wantElec       float64
		wantTotal      float64
	}{
		{
			name: "RTX 5060 Ti real-world params",
			hw: HardwareCosts{
				PurchasePriceUSD:          960.0,
				AmortizationYears:         3,
				MaintenancePercentPerYear: 0.0,
				RatePerKWh:                0.08,
				PUEFactor:                 1.0,
			},
			powerDrawWatts: 150.0,
			// amort = (960 * (1 + 0)) / (3 * 8760) = 960 / 26280 = 0.036529680...
			wantAmort: 960.0 / 26280.0,
			// elec = (150/1000) * 0.08 * 1.0 = 0.012
			wantElec:  0.012,
			wantTotal: 960.0/26280.0 + 0.012,
		},
		{
			name: "zero power draw",
			hw: HardwareCosts{
				PurchasePriceUSD:          960.0,
				AmortizationYears:         3,
				MaintenancePercentPerYear: 0.0,
				RatePerKWh:                0.08,
				PUEFactor:                 1.0,
			},
			powerDrawWatts: 0.0,
			wantAmort:      960.0 / 26280.0,
			wantElec:       0.0,
			wantTotal:      960.0 / 26280.0,
		},
		{
			name: "high PUE factor",
			hw: HardwareCosts{
				PurchasePriceUSD:          2000.0,
				AmortizationYears:         5,
				MaintenancePercentPerYear: 0.10,
				RatePerKWh:                0.12,
				PUEFactor:                 1.6,
			},
			powerDrawWatts: 300.0,
			// amort = (2000 * 1.10) / (5 * 8760) = 2200 / 43800 = 0.050228...
			wantAmort: 2200.0 / 43800.0,
			// elec = (300/1000) * 0.12 * 1.6 = 0.0576
			wantElec:  0.0576,
			wantTotal: 2200.0/43800.0 + 0.0576,
		},
		{
			name: "zero PUE defaults to 1.0",
			hw: HardwareCosts{
				PurchasePriceUSD:          1000.0,
				AmortizationYears:         1,
				MaintenancePercentPerYear: 0.0,
				RatePerKWh:                0.10,
				PUEFactor:                 0.0,
			},
			powerDrawWatts: 100.0,
			// amort = 1000 / (1 * 8760) = 0.11415525...
			wantAmort: 1000.0 / 8760.0,
			// elec = (100/1000) * 0.10 * 1.0 (defaulted) = 0.01
			wantElec:  0.01,
			wantTotal: 1000.0/8760.0 + 0.01,
		},
		{
			name: "maintenance percentage increases amortization",
			hw: HardwareCosts{
				PurchasePriceUSD:          960.0,
				AmortizationYears:         3,
				MaintenancePercentPerYear: 0.05,
				RatePerKWh:                0.08,
				PUEFactor:                 1.0,
			},
			powerDrawWatts: 150.0,
			// amort = (960 * 1.05) / (3 * 8760) = 1008 / 26280 = 0.038356...
			wantAmort: 1008.0 / 26280.0,
			wantElec:  0.012,
			wantTotal: 1008.0/26280.0 + 0.012,
		},
		{
			name: "very expensive hardware with low utilization",
			hw: HardwareCosts{
				PurchasePriceUSD:          40000.0,
				AmortizationYears:         5,
				MaintenancePercentPerYear: 0.15,
				RatePerKWh:                0.15,
				PUEFactor:                 1.4,
			},
			powerDrawWatts: 700.0,
			// amort = (40000 * 1.15) / (5 * 8760) = 46000 / 43800 = 1.050228...
			wantAmort: 46000.0 / 43800.0,
			// elec = (700/1000) * 0.15 * 1.4 = 0.147
			wantElec:  0.147,
			wantTotal: 46000.0/43800.0 + 0.147,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAmort, gotElec, gotTotal := ComputeHourlyCost(tt.hw, tt.powerDrawWatts)

			if !almostEqual(gotAmort, tt.wantAmort) {
				t.Errorf("amortization = %v, want %v", gotAmort, tt.wantAmort)
			}
			if !almostEqual(gotElec, tt.wantElec) {
				t.Errorf("electricity = %v, want %v", gotElec, tt.wantElec)
			}
			if !almostEqual(gotTotal, tt.wantTotal) {
				t.Errorf("total = %v, want %v", gotTotal, tt.wantTotal)
			}
		})
	}
}

func TestComputeHourlyCost_ComponentRelationship(t *testing.T) {
	hw := HardwareCosts{
		PurchasePriceUSD:  960.0,
		AmortizationYears: 3,
		RatePerKWh:        0.08,
		PUEFactor:         1.0,
	}
	amort, elec, total := ComputeHourlyCost(hw, 150.0)
	if !almostEqual(total, amort+elec) {
		t.Errorf("total (%v) should equal amortization (%v) + electricity (%v)", total, amort, elec)
	}
}

func TestComputeTokenRate(t *testing.T) {
	baseTime := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		prev           TokenSnapshot
		curr           TokenSnapshot
		wantInputRate  float64
		wantOutputRate float64
		wantTotalRate  float64
	}{
		{
			name: "normal one-hour delta",
			prev: TokenSnapshot{
				Timestamp:    baseTime,
				InputTokens:  1000,
				OutputTokens: 5000,
			},
			curr: TokenSnapshot{
				Timestamp:    baseTime.Add(1 * time.Hour),
				InputTokens:  2000,
				OutputTokens: 8000,
			},
			wantInputRate:  1000.0,
			wantOutputRate: 3000.0,
			wantTotalRate:  4000.0,
		},
		{
			name: "30-minute interval",
			prev: TokenSnapshot{
				Timestamp:    baseTime,
				InputTokens:  100,
				OutputTokens: 200,
			},
			curr: TokenSnapshot{
				Timestamp:    baseTime.Add(30 * time.Minute),
				InputTokens:  200,
				OutputTokens: 400,
			},
			// 100 tokens in 0.5 hours = 200/hr, 200 in 0.5h = 400/hr
			wantInputRate:  200.0,
			wantOutputRate: 400.0,
			wantTotalRate:  600.0,
		},
		{
			name: "input counter reset",
			prev: TokenSnapshot{
				Timestamp:    baseTime,
				InputTokens:  50000,
				OutputTokens: 100000,
			},
			curr: TokenSnapshot{
				Timestamp:    baseTime.Add(1 * time.Hour),
				InputTokens:  500,
				OutputTokens: 150000,
			},
			// Input delta is negative (-49500), so uses curr value (500) as delta
			wantInputRate:  500.0,
			wantOutputRate: 50000.0,
			wantTotalRate:  50500.0,
		},
		{
			name: "output counter reset",
			prev: TokenSnapshot{
				Timestamp:    baseTime,
				InputTokens:  1000,
				OutputTokens: 90000,
			},
			curr: TokenSnapshot{
				Timestamp:    baseTime.Add(1 * time.Hour),
				InputTokens:  2000,
				OutputTokens: 1000,
			},
			wantInputRate:  1000.0,
			wantOutputRate: 1000.0,
			wantTotalRate:  2000.0,
		},
		{
			name: "both counters reset",
			prev: TokenSnapshot{
				Timestamp:    baseTime,
				InputTokens:  50000,
				OutputTokens: 90000,
			},
			curr: TokenSnapshot{
				Timestamp:    baseTime.Add(1 * time.Hour),
				InputTokens:  100,
				OutputTokens: 200,
			},
			wantInputRate:  100.0,
			wantOutputRate: 200.0,
			wantTotalRate:  300.0,
		},
		{
			name: "zero elapsed time returns zeros",
			prev: TokenSnapshot{
				Timestamp:    baseTime,
				InputTokens:  1000,
				OutputTokens: 5000,
			},
			curr: TokenSnapshot{
				Timestamp:    baseTime,
				InputTokens:  2000,
				OutputTokens: 8000,
			},
			wantInputRate:  0,
			wantOutputRate: 0,
			wantTotalRate:  0,
		},
		{
			name: "negative elapsed time returns zeros",
			prev: TokenSnapshot{
				Timestamp:    baseTime.Add(1 * time.Hour),
				InputTokens:  1000,
				OutputTokens: 5000,
			},
			curr: TokenSnapshot{
				Timestamp:    baseTime,
				InputTokens:  2000,
				OutputTokens: 8000,
			},
			wantInputRate:  0,
			wantOutputRate: 0,
			wantTotalRate:  0,
		},
		{
			name: "no token change",
			prev: TokenSnapshot{
				Timestamp:    baseTime,
				InputTokens:  5000,
				OutputTokens: 10000,
			},
			curr: TokenSnapshot{
				Timestamp:    baseTime.Add(1 * time.Hour),
				InputTokens:  5000,
				OutputTokens: 10000,
			},
			wantInputRate:  0,
			wantOutputRate: 0,
			wantTotalRate:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInput, gotOutput, gotTotal := ComputeTokenRate(tt.prev, tt.curr)

			if !almostEqual(gotInput, tt.wantInputRate) {
				t.Errorf("inputPerHour = %v, want %v", gotInput, tt.wantInputRate)
			}
			if !almostEqual(gotOutput, tt.wantOutputRate) {
				t.Errorf("outputPerHour = %v, want %v", gotOutput, tt.wantOutputRate)
			}
			if !almostEqual(gotTotal, tt.wantTotalRate) {
				t.Errorf("totalPerHour = %v, want %v", gotTotal, tt.wantTotalRate)
			}
		})
	}
}

func TestComputeCostPerToken(t *testing.T) {
	tests := []struct {
		name        string
		hourlyCost  float64
		tokensPerHr float64
		wantCPT     float64
	}{
		{
			name:        "normal throughput",
			hourlyCost:  0.05,
			tokensPerHr: 100000.0,
			wantCPT:     0.05 / 100000.0,
		},
		{
			name:        "zero tokens returns zero",
			hourlyCost:  0.05,
			tokensPerHr: 0,
			wantCPT:     0,
		},
		{
			name:        "negative tokens returns zero",
			hourlyCost:  0.05,
			tokensPerHr: -100,
			wantCPT:     0,
		},
		{
			name:        "high throughput yields low cost per token",
			hourlyCost:  1.0,
			tokensPerHr: 10_000_000,
			wantCPT:     1.0 / 10_000_000,
		},
		{
			name:        "very low throughput yields high cost per token",
			hourlyCost:  1.0,
			tokensPerHr: 1,
			wantCPT:     1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeCostPerToken(tt.hourlyCost, tt.tokensPerHr)
			if !almostEqual(got, tt.wantCPT) {
				t.Errorf("ComputeCostPerToken(%v, %v) = %v, want %v",
					tt.hourlyCost, tt.tokensPerHr, got, tt.wantCPT)
			}
		})
	}
}

func TestComputeFull(t *testing.T) {
	baseTime := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)

	hw := HardwareCosts{
		PurchasePriceUSD:  960.0,
		AmortizationYears: 3,
		RatePerKWh:        0.08,
		PUEFactor:         1.0,
	}
	prev := TokenSnapshot{
		Timestamp:    baseTime,
		InputTokens:  100000,
		OutputTokens: 200000,
	}
	curr := TokenSnapshot{
		Timestamp:    baseTime.Add(1 * time.Hour),
		InputTokens:  110000,
		OutputTokens: 250000,
	}
	powerW := 150.0

	result := ComputeFull(hw, powerW, prev, curr)

	expectedAmort := 960.0 / 26280.0
	expectedElec := 0.012
	expectedTotal := expectedAmort + expectedElec
	// 10000 input/hr + 50000 output/hr = 60000 tokens/hr
	expectedCPT := expectedTotal / 60000.0
	expectedCPM := expectedCPT * 1_000_000

	if !almostEqual(result.AmortizationPerHour, expectedAmort) {
		t.Errorf("AmortizationPerHour = %v, want %v", result.AmortizationPerHour, expectedAmort)
	}
	if !almostEqual(result.ElectricityPerHour, expectedElec) {
		t.Errorf("ElectricityPerHour = %v, want %v", result.ElectricityPerHour, expectedElec)
	}
	if !almostEqual(result.TotalPerHour, expectedTotal) {
		t.Errorf("TotalPerHour = %v, want %v", result.TotalPerHour, expectedTotal)
	}
	if !almostEqual(result.CostPerToken, expectedCPT) {
		t.Errorf("CostPerToken = %v, want %v", result.CostPerToken, expectedCPT)
	}
	if !almostEqual(result.CostPerMillionTokens, expectedCPM) {
		t.Errorf("CostPerMillionTokens = %v, want %v", result.CostPerMillionTokens, expectedCPM)
	}
	if result.GPUEfficiencyRatio != 1.0 {
		t.Errorf("GPUEfficiencyRatio = %v, want 1.0 (GPU active with tokens flowing)", result.GPUEfficiencyRatio)
	}
}

func TestComputeFull_GPUIdleNoTokens(t *testing.T) {
	baseTime := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)

	hw := HardwareCosts{
		PurchasePriceUSD:  960.0,
		AmortizationYears: 3,
		RatePerKWh:        0.08,
		PUEFactor:         1.0,
	}
	prev := TokenSnapshot{
		Timestamp:    baseTime,
		InputTokens:  100000,
		OutputTokens: 200000,
	}
	curr := TokenSnapshot{
		Timestamp:    baseTime.Add(1 * time.Hour),
		InputTokens:  100000,
		OutputTokens: 200000,
	}

	result := ComputeFull(hw, 150.0, prev, curr)
	if result.GPUEfficiencyRatio != 0.0 {
		t.Errorf("GPUEfficiencyRatio = %v, want 0.0 (GPU on but idle)", result.GPUEfficiencyRatio)
	}
	if result.CostPerToken != 0 {
		t.Errorf("CostPerToken = %v, want 0 (no tokens flowing)", result.CostPerToken)
	}
}

func TestComputeFull_GPUOff(t *testing.T) {
	baseTime := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)

	hw := HardwareCosts{
		PurchasePriceUSD:  960.0,
		AmortizationYears: 3,
		RatePerKWh:        0.08,
		PUEFactor:         1.0,
	}
	prev := TokenSnapshot{
		Timestamp:    baseTime,
		InputTokens:  0,
		OutputTokens: 0,
	}
	curr := TokenSnapshot{
		Timestamp:    baseTime.Add(1 * time.Hour),
		InputTokens:  0,
		OutputTokens: 0,
	}

	result := ComputeFull(hw, 5.0, prev, curr)
	if result.GPUEfficiencyRatio != 0.0 {
		t.Errorf("GPUEfficiencyRatio = %v, want 0.0 (GPU power below threshold)", result.GPUEfficiencyRatio)
	}
}

func TestDefaultCloudPricing(t *testing.T) {
	pricing := DefaultCloudPricing()

	if len(pricing) == 0 {
		t.Fatal("DefaultCloudPricing returned empty slice")
	}

	providers := make(map[string]bool)
	for _, p := range pricing {
		providers[p.Provider] = true
		if p.Model == "" {
			t.Error("found CloudPricing entry with empty model name")
		}
		if p.InputPerMillion <= 0 {
			t.Errorf("provider %s model %s has non-positive input pricing: %v",
				p.Provider, p.Model, p.InputPerMillion)
		}
		if p.OutputPerMillion <= 0 {
			t.Errorf("provider %s model %s has non-positive output pricing: %v",
				p.Provider, p.Model, p.OutputPerMillion)
		}
	}

	expectedProviders := []string{"OpenAI", "Anthropic", "Google"}
	for _, ep := range expectedProviders {
		if !providers[ep] {
			t.Errorf("expected provider %q not found in DefaultCloudPricing", ep)
		}
	}
}

func TestDefaultCloudPricing_SpecificModels(t *testing.T) {
	pricing := DefaultCloudPricing()
	modelPricing := make(map[string]CloudPricing)
	for _, p := range pricing {
		modelPricing[p.Model] = p
	}

	// Verified pricing as of 2026-03-21
	gpt54, ok := modelPricing["gpt-5.4"]
	if !ok {
		t.Fatal("gpt-5.4 not found in DefaultCloudPricing")
	}
	if gpt54.InputPerMillion != 2.50 {
		t.Errorf("gpt-5.4 InputPerMillion = %v, want 2.50", gpt54.InputPerMillion)
	}
	if gpt54.OutputPerMillion != 15.00 {
		t.Errorf("gpt-5.4 OutputPerMillion = %v, want 15.00", gpt54.OutputPerMillion)
	}

	haiku, ok := modelPricing["claude-haiku-4-5"]
	if !ok {
		t.Fatal("claude-haiku-4-5 not found in DefaultCloudPricing")
	}
	if haiku.InputPerMillion != 1.00 {
		t.Errorf("claude-haiku-4-5 InputPerMillion = %v, want 1.00", haiku.InputPerMillion)
	}
	if haiku.OutputPerMillion != 5.00 {
		t.Errorf("claude-haiku-4-5 OutputPerMillion = %v, want 5.00", haiku.OutputPerMillion)
	}
}

func TestCompareToCloud(t *testing.T) {
	pricing := []CloudPricing{
		{Provider: "TestProvider", Model: "test-model", InputPerMillion: 2.00, OutputPerMillion: 8.00},
	}

	inputTokens := int64(1_000_000)
	outputTokens := int64(500_000)
	onPremCost := 1.50

	results := CompareToCloud(inputTokens, outputTokens, onPremCost, pricing)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	// Cloud cost: (1M / 1M * 2.00) + (500K / 1M * 8.00) = 2.00 + 4.00 = 6.00
	expectedCloudCost := 6.0
	expectedSavings := 6.0 - 1.50
	expectedSavingsPct := (4.50 / 6.0) * 100

	if r.Provider != "TestProvider" {
		t.Errorf("Provider = %q, want %q", r.Provider, "TestProvider")
	}
	if r.Model != "test-model" {
		t.Errorf("Model = %q, want %q", r.Model, "test-model")
	}
	if !almostEqual(r.CloudCostUSD, expectedCloudCost) {
		t.Errorf("CloudCostUSD = %v, want %v", r.CloudCostUSD, expectedCloudCost)
	}
	if !almostEqual(r.OnPremCostUSD, onPremCost) {
		t.Errorf("OnPremCostUSD = %v, want %v", r.OnPremCostUSD, onPremCost)
	}
	if !almostEqual(r.SavingsUSD, expectedSavings) {
		t.Errorf("SavingsUSD = %v, want %v", r.SavingsUSD, expectedSavings)
	}
	if !almostEqual(r.SavingsPercent, expectedSavingsPct) {
		t.Errorf("SavingsPercent = %v, want %v", r.SavingsPercent, expectedSavingsPct)
	}
}

func TestCompareToCloud_OnPremMoreExpensive(t *testing.T) {
	pricing := []CloudPricing{
		{Provider: "Cheap", Model: "cheap-model", InputPerMillion: 0.10, OutputPerMillion: 0.30},
	}

	results := CompareToCloud(100_000, 50_000, 5.00, pricing)

	r := results[0]
	// Cloud cost: (100K/1M * 0.10) + (50K/1M * 0.30) = 0.01 + 0.015 = 0.025
	expectedCloudCost := 0.025
	if !almostEqual(r.CloudCostUSD, expectedCloudCost) {
		t.Errorf("CloudCostUSD = %v, want %v", r.CloudCostUSD, expectedCloudCost)
	}
	if r.SavingsUSD >= 0 {
		t.Errorf("SavingsUSD = %v, expected negative (on-prem more expensive)", r.SavingsUSD)
	}
	if r.SavingsPercent >= 0 {
		t.Errorf("SavingsPercent = %v, expected negative", r.SavingsPercent)
	}
}

func TestCompareToCloud_ZeroTokens(t *testing.T) {
	pricing := []CloudPricing{
		{Provider: "Test", Model: "test", InputPerMillion: 2.00, OutputPerMillion: 8.00},
	}

	results := CompareToCloud(0, 0, 0.05, pricing)
	r := results[0]

	if !almostEqual(r.CloudCostUSD, 0) {
		t.Errorf("CloudCostUSD = %v, want 0 for zero tokens", r.CloudCostUSD)
	}
	// savings = 0 - 0.05 = -0.05 (on-prem still has fixed costs)
	if !almostEqual(r.SavingsUSD, -0.05) {
		t.Errorf("SavingsUSD = %v, want -0.05", r.SavingsUSD)
	}
	// savingsPct should be 0 because cloudCost is 0
	if !almostEqual(r.SavingsPercent, 0) {
		t.Errorf("SavingsPercent = %v, want 0 (cloud cost is zero)", r.SavingsPercent)
	}
}

func TestCompareToCloud_MultipleProviders(t *testing.T) {
	pricing := DefaultCloudPricing()
	results := CompareToCloud(1_000_000, 500_000, 0.50, pricing)

	if len(results) != len(pricing) {
		t.Fatalf("expected %d results, got %d", len(pricing), len(results))
	}

	for i, r := range results {
		if r.Provider != pricing[i].Provider {
			t.Errorf("result[%d].Provider = %q, want %q", i, r.Provider, pricing[i].Provider)
		}
		if r.Model != pricing[i].Model {
			t.Errorf("result[%d].Model = %q, want %q", i, r.Model, pricing[i].Model)
		}
		if r.OnPremCostUSD != 0.50 {
			t.Errorf("result[%d].OnPremCostUSD = %v, want 0.50", i, r.OnPremCostUSD)
		}
		if !almostEqual(r.SavingsUSD, r.CloudCostUSD-r.OnPremCostUSD) {
			t.Errorf("result[%d].SavingsUSD = %v, want CloudCostUSD - OnPremCostUSD = %v",
				i, r.SavingsUSD, r.CloudCostUSD-r.OnPremCostUSD)
		}
	}
}

func TestCompareToCloud_EmptyPricing(t *testing.T) {
	results := CompareToCloud(1_000_000, 500_000, 0.50, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil pricing, got %d", len(results))
	}

	results = CompareToCloud(1_000_000, 500_000, 0.50, []CloudPricing{})
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty pricing, got %d", len(results))
	}
}
