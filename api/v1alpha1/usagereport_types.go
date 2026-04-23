/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReportSchedule defines how often reports are generated.
// +kubebuilder:validation:Enum=daily;weekly;monthly
type ReportSchedule string

const (
	// ReportScheduleDaily generates a report once per calendar day.
	ReportScheduleDaily ReportSchedule = "daily"
	// ReportScheduleWeekly generates a report once per calendar week.
	ReportScheduleWeekly ReportSchedule = "weekly"
	// ReportScheduleMonthly generates a report once per calendar month.
	ReportScheduleMonthly ReportSchedule = "monthly"
)

// UsageReportSpec defines the desired configuration for a UsageReport.
type UsageReportSpec struct {
	// costProfileRef is the name of the CostProfile to use for cost computation.
	// +kubebuilder:validation:MinLength=1
	CostProfileRef string `json:"costProfileRef"`

	// schedule defines how often the report is regenerated.
	// +kubebuilder:default=daily
	// +optional
	Schedule ReportSchedule `json:"schedule,omitempty"`

	// namespaces limits the report to specific namespaces. If omitted, all namespaces are included.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
}

// ModelCostBreakdown contains cost attribution for a single model.
type ModelCostBreakdown struct {
	// model is the model name or identifier.
	Model string `json:"model"`

	// namespace is the namespace where the model runs.
	Namespace string `json:"namespace"`

	// inputTokens is the total input/prompt tokens processed.
	InputTokens int64 `json:"inputTokens"`

	// outputTokens is the total output/completion tokens generated.
	OutputTokens int64 `json:"outputTokens"`

	// estimatedCostUSD is the computed cost for this model.
	EstimatedCostUSD float64 `json:"estimatedCostUSD"`

	// costPerMillionTokens is the effective cost per million tokens for this model.
	CostPerMillionTokens float64 `json:"costPerMillionTokens"`
}

// NamespaceCostBreakdown contains cost attribution for a single namespace (team).
type NamespaceCostBreakdown struct {
	// namespace is the Kubernetes namespace.
	Namespace string `json:"namespace"`

	// estimatedCostUSD is the computed cost for this namespace.
	EstimatedCostUSD float64 `json:"estimatedCostUSD"`

	// tokenCount is the total tokens (input + output) for this namespace.
	TokenCount int64 `json:"tokenCount"`
}

// CloudComparisonEntry compares on-prem cost to a specific cloud provider.
type CloudComparisonEntry struct {
	// provider is the cloud provider name (e.g. "OpenAI", "Anthropic", "Google").
	Provider string `json:"provider"`

	// model is the comparable cloud model (e.g. "gpt-4o", "claude-sonnet-4-5").
	Model string `json:"model"`

	// estimatedCloudCostUSD is what the same tokens would have cost on this provider.
	EstimatedCloudCostUSD float64 `json:"estimatedCloudCostUSD"`

	// savingsUSD is the difference (cloud cost - on-prem cost). Positive means on-prem is cheaper.
	SavingsUSD float64 `json:"savingsUSD"`

	// savingsPercent is the percentage saved vs. cloud (0-100).
	SavingsPercent float64 `json:"savingsPercent"`
}

// UsageReportStatus contains the computed cost report data. Populated by the controller.
type UsageReportStatus struct {
	// period is the time period covered by this report (e.g. "2026-03", "2026-03-20").
	// +optional
	Period string `json:"period,omitempty"`

	// periodStart is the start time of the reporting period.
	// +optional
	PeriodStart *metav1.Time `json:"periodStart,omitempty"`

	// periodEnd is the end time of the reporting period.
	// +optional
	PeriodEnd *metav1.Time `json:"periodEnd,omitempty"`

	// inputTokens is the total input/prompt tokens across all models.
	// +optional
	InputTokens int64 `json:"inputTokens,omitempty"`

	// outputTokens is the total output/completion tokens across all models.
	// +optional
	OutputTokens int64 `json:"outputTokens,omitempty"`

	// estimatedCostUSD is the total computed on-prem cost for the period.
	// +optional
	EstimatedCostUSD float64 `json:"estimatedCostUSD,omitempty"`

	// costPerMillionTokens is the effective blended cost per million tokens.
	// Includes hardware amortization across the full reporting period —
	// sensitive to utilization. Use in tandem with marginalCostPerMillionTokens
	// when comparing against cloud API pricing.
	// +optional
	CostPerMillionTokens float64 `json:"costPerMillionTokens,omitempty"`

	// marginalCostPerMillionTokens is the electricity-only $/MTok computed
	// across active (above-threshold) samples in the reporting period. It
	// excludes hardware amortization, so it answers "what did the marginal
	// token actually cost in electricity?" — the right comparison for
	// cloud APIs that also bill marginally. At saturated utilization this
	// converges with costPerMillionTokens; at low utilization it diverges
	// (amortized goes up, marginal stays flat).
	// +optional
	MarginalCostPerMillionTokens float64 `json:"marginalCostPerMillionTokens,omitempty"`

	// activeEnergyKWh is the integrated energy drawn during active-threshold
	// time across the reporting period, derived from DCGM power samples.
	// Surfaced directly so operators can sanity-check the marginal cost math.
	// +optional
	ActiveEnergyKWh float64 `json:"activeEnergyKWh,omitempty"`

	// gpuEfficiencyRatio is the fraction of GPU time spent on active inference (0.0-1.0).
	// Derived from the same samples as utilizationPercent — the two fields move
	// together; gpuEfficiencyRatio is kept for compatibility with the Grafana
	// dashboard, utilizationPercent is the human-readable view.
	// +optional
	GPUEfficiencyRatio float64 `json:"gpuEfficiencyRatio,omitempty"`

	// utilizationPercent is the fraction of the reporting period the GPUs were
	// drawing meaningful power (above CostProfile.spec.electricity.idleWattsThreshold),
	// expressed as a percentage (0.0-100.0). A value of 4 alongside $16/MTok
	// amortized means idle hours are inflating the amortized rate; at 100 %
	// the amortized and marginal rates converge.
	// +optional
	UtilizationPercent float64 `json:"utilizationPercent,omitempty"`

	// activeHoursInPeriod is how many hours of the reporting period had GPU
	// power draw above the idle threshold. Paired with totalHoursInPeriod so
	// operators can see the ratio without recomputing.
	// +optional
	ActiveHoursInPeriod float64 `json:"activeHoursInPeriod,omitempty"`

	// totalHoursInPeriod is the wall-clock length of the reporting period in
	// hours. For a daily report this grows from 0 to 24 as the day elapses.
	// +optional
	TotalHoursInPeriod float64 `json:"totalHoursInPeriod,omitempty"`

	// byModel contains per-model cost breakdowns.
	// +optional
	ByModel []ModelCostBreakdown `json:"byModel,omitempty"`

	// byNamespace contains per-namespace (team) cost breakdowns.
	// +optional
	ByNamespace []NamespaceCostBreakdown `json:"byNamespace,omitempty"`

	// cloudComparison contains cost comparisons against cloud providers.
	// +optional
	CloudComparison []CloudComparisonEntry `json:"cloudComparison,omitempty"`

	// lastUpdated is the timestamp of the last report computation.
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// conditions represent the current state of the UsageReport.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Period",type=string,JSONPath=`.status.period`
// +kubebuilder:printcolumn:name="Cost ($)",type=number,JSONPath=`.status.estimatedCostUSD`,format=float
// +kubebuilder:printcolumn:name="$/MTok",type=number,JSONPath=`.status.costPerMillionTokens`,format=float
// +kubebuilder:printcolumn:name="$/MTok (marginal)",type=number,JSONPath=`.status.marginalCostPerMillionTokens`,format=float,priority=1
// +kubebuilder:printcolumn:name="Input Tokens",type=integer,JSONPath=`.status.inputTokens`
// +kubebuilder:printcolumn:name="Output Tokens",type=integer,JSONPath=`.status.outputTokens`
// +kubebuilder:printcolumn:name="Util %",type=number,JSONPath=`.status.utilizationPercent`,format=float,priority=1
// +kubebuilder:printcolumn:name="GPU Efficiency",type=number,JSONPath=`.status.gpuEfficiencyRatio`,format=float,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UsageReport contains computed cost data for a time period. The controller populates
// the status with cost attribution, cloud comparison, and efficiency metrics.
type UsageReport struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec UsageReportSpec `json:"spec"`

	// +optional
	Status UsageReportStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// UsageReportList contains a list of UsageReport
type UsageReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []UsageReport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UsageReport{}, &UsageReportList{})
}
