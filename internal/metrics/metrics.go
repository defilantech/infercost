package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// HourlyCostUSD is the total hourly infrastructure cost for a cost profile.
	HourlyCostUSD = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "hourly_cost_usd",
			Help:      "Total hourly infrastructure cost (amortization + electricity) in USD.",
		},
		[]string{"cost_profile", "node", "gpu_model"},
	)

	// AmortizationPerHour is the hardware amortization component of hourly cost.
	AmortizationPerHour = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "amortization_per_hour_usd",
			Help:      "Hourly hardware amortization cost in USD.",
		},
		[]string{"cost_profile", "node"},
	)

	// ElectricityPerHour is the electricity component of hourly cost.
	ElectricityPerHour = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "electricity_per_hour_usd",
			Help:      "Hourly electricity cost in USD based on real-time GPU power draw.",
		},
		[]string{"cost_profile", "node"},
	)

	// GPUPowerWatts is the current GPU power draw.
	GPUPowerWatts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "gpu_power_watts",
			Help:      "Current GPU power draw in watts (from DCGM).",
		},
		[]string{"cost_profile", "node", "gpu"},
	)

	// CostPerTokenUSD is the computed cost per token.
	CostPerTokenUSD = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "cost_per_token_usd",
			Help:      "Computed cost per token in USD.",
		},
		[]string{"model", "namespace"},
	)

	// CostPerMillionTokensUSD is the cost per million tokens (more readable).
	CostPerMillionTokensUSD = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "cost_per_million_tokens_usd",
			Help:      "Computed cost per million tokens in USD.",
		},
		[]string{"model", "namespace"},
	)

	// CloudEquivalentCostUSD is what the same tokens would cost on a cloud provider.
	CloudEquivalentCostUSD = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "cloud_equivalent_cost_usd",
			Help:      "Estimated cost if the same tokens were processed by a cloud provider.",
		},
		[]string{"provider", "cloud_model"},
	)

	// SavingsVsCloudUSD is the savings amount compared to cloud.
	SavingsVsCloudUSD = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "savings_vs_cloud_usd",
			Help:      "USD saved compared to cloud provider pricing. Positive means on-prem is cheaper.",
		},
		[]string{"provider", "cloud_model"},
	)

	// SavingsVsCloudPercent is the savings percentage.
	SavingsVsCloudPercent = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "savings_vs_cloud_percent",
			Help:      "Percentage saved compared to cloud provider pricing.",
		},
		[]string{"provider", "cloud_model"},
	)

	// TokensTotal tracks cumulative token counts per model.
	TokensTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "tokens_total",
			Help:      "Total tokens processed (passthrough from inference engine).",
		},
		[]string{"model", "namespace", "direction"}, // direction: input, output
	)

	// GPUEfficiencyRatio is the fraction of GPU time spent on active inference.
	GPUEfficiencyRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "gpu_efficiency_ratio",
			Help:      "Fraction of GPU time spent on active inference (0.0-1.0).",
		},
		[]string{"cost_profile", "node"},
	)

	// TokensPerHour is the current token throughput rate.
	TokensPerHour = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "infercost",
			Name:      "tokens_per_hour",
			Help:      "Current token throughput rate (tokens per hour).",
		},
		[]string{"model", "namespace"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		HourlyCostUSD,
		AmortizationPerHour,
		ElectricityPerHour,
		GPUPowerWatts,
		CostPerTokenUSD,
		CostPerMillionTokensUSD,
		CloudEquivalentCostUSD,
		SavingsVsCloudUSD,
		SavingsVsCloudPercent,
		TokensTotal,
		GPUEfficiencyRatio,
		TokensPerHour,
	)
}
