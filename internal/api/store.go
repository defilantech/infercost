package api

import (
	"sync"
	"time"

	"github.com/defilantech/infercost/internal/calculator"
)

// CostData holds the latest computed cost state, populated by the controller.
type CostData struct {
	// Infrastructure
	ProfileName       string  `json:"profileName"`
	GPUModel          string  `json:"gpuModel"`
	GPUCount          int32   `json:"gpuCount"`
	HourlyCostUSD     float64 `json:"hourlyCostUSD"`
	AmortizationPerHr float64 `json:"amortizationPerHourUSD"`
	ElectricityPerHr  float64 `json:"electricityPerHourUSD"`
	PowerDrawWatts    float64 `json:"powerDrawWatts"`
	MonthlyProjected  float64 `json:"monthlyProjectedUSD"`
	YearlyProjected   float64 `json:"yearlyProjectedUSD"`

	// Hardware economics from spec
	PurchasePriceUSD  float64 `json:"purchasePriceUSD"`
	AmortizationYears int32   `json:"amortizationYears"`
	RatePerKWh        float64 `json:"ratePerKWh"`
	PUEFactor         float64 `json:"pueFactor"`

	// Timing
	UptimeHours float64   `json:"uptimeHours"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// ModelData holds per-model inference metrics.
type ModelData struct {
	Model         string  `json:"model"`
	Namespace     string  `json:"namespace"`
	Pod           string  `json:"pod"`
	InputTokens   float64 `json:"inputTokens"`
	OutputTokens  float64 `json:"outputTokens"`
	TokensPerHour float64 `json:"tokensPerHour"`
	CostPerMTok   float64 `json:"costPerMillionTokensUSD"`
	TokensPerSec  float64 `json:"tokensPerSec"`
	IsActive      bool    `json:"isActive"`
}

// ComparisonData holds cloud comparison results.
type ComparisonData struct {
	Provider       string  `json:"provider"`
	Model          string  `json:"model"`
	CloudCostUSD   float64 `json:"cloudCostUSD"`
	OnPremCostUSD  float64 `json:"onPremCostUSD"`
	SavingsUSD     float64 `json:"savingsUSD"`
	SavingsPercent float64 `json:"savingsPercent"`
	InputPerMTok   float64 `json:"inputPerMillionTokensUSD"`
	OutputPerMTok  float64 `json:"outputPerMillionTokensUSD"`
}

// Store is a thread-safe data store populated by the controller and read by the API.
type Store struct {
	mu          sync.RWMutex
	cost        *CostData
	models      []ModelData
	comparisons []ComparisonData
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{}
}

// SetCostData updates the infrastructure cost data.
func (s *Store) SetCostData(data CostData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cost = &data
}

// SetModels updates the per-model data.
func (s *Store) SetModels(models []ModelData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models = models
}

// SetComparisons updates the cloud comparison data.
func (s *Store) SetComparisons(comparisons []ComparisonData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comparisons = comparisons
}

// GetCostData returns the latest cost data.
func (s *Store) GetCostData() *CostData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cost
}

// GetModels returns the latest model data.
func (s *Store) GetModels() []ModelData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ModelData, len(s.models))
	copy(result, s.models)
	return result
}

// GetComparisons returns the latest cloud comparison data.
func (s *Store) GetComparisons() []ComparisonData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ComparisonData, len(s.comparisons))
	copy(result, s.comparisons)
	return result
}

// BuildComparisons creates comparison data from tokens and cost info.
func BuildComparisons(inputTokens, outputTokens int64, onPremCost float64) []ComparisonData {
	pricing := calculator.DefaultCloudPricing()
	comparisons := calculator.CompareToCloud(inputTokens, outputTokens, onPremCost, pricing)

	results := make([]ComparisonData, 0, len(comparisons))
	for i, c := range comparisons {
		results = append(results, ComparisonData{
			Provider:       c.Provider,
			Model:          c.Model,
			CloudCostUSD:   c.CloudCostUSD,
			OnPremCostUSD:  c.OnPremCostUSD,
			SavingsUSD:     c.SavingsUSD,
			SavingsPercent: c.SavingsPercent,
			InputPerMTok:   pricing[i].InputPerMillion,
			OutputPerMTok:  pricing[i].OutputPerMillion,
		})
	}
	return results
}
