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

// TokenBudgetScope defines what the budget covers.
type TokenBudgetScope struct {
	// namespace is the Kubernetes namespace this budget applies to.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// AlertThreshold defines a budget alert trigger point.
type AlertThreshold struct {
	// percent is the budget utilization percentage that triggers this alert (e.g. 80, 100).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=200
	Percent int32 `json:"percent"`

	// severity is the alert severity level.
	// +kubebuilder:validation:Enum=warning;critical
	Severity string `json:"severity"`
}

// TokenBudgetSpec defines the desired budget configuration.
type TokenBudgetSpec struct {
	// scope defines what this budget covers.
	// +required
	Scope TokenBudgetScope `json:"scope"`

	// monthlyLimitUSD is the maximum spend allowed per calendar month.
	// +kubebuilder:validation:Minimum=0
	MonthlyLimitUSD float64 `json:"monthlyLimitUSD"`

	// alertThresholds defines percentage-based alert trigger points.
	// +optional
	AlertThresholds []AlertThreshold `json:"alertThresholds,omitempty"`
}

// TokenBudgetStatus defines the observed state of a TokenBudget.
type TokenBudgetStatus struct {
	// currentSpendUSD is the accumulated spend in the current billing period.
	// +optional
	CurrentSpendUSD float64 `json:"currentSpendUSD,omitempty"`

	// utilizationPercent is (currentSpend / monthlyLimit) * 100.
	// +optional
	UtilizationPercent float64 `json:"utilizationPercent,omitempty"`

	// periodStart is the start of the current billing period.
	// +optional
	PeriodStart *metav1.Time `json:"periodStart,omitempty"`

	// periodEnd is the end of the current billing period.
	// +optional
	PeriodEnd *metav1.Time `json:"periodEnd,omitempty"`

	// lastUpdated is when the budget was last evaluated.
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// conditions represent the current state of the TokenBudget.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.scope.namespace`
// +kubebuilder:printcolumn:name="Limit ($)",type=number,JSONPath=`.spec.monthlyLimitUSD`
// +kubebuilder:printcolumn:name="Spend ($)",type=number,JSONPath=`.status.currentSpendUSD`,format=float
// +kubebuilder:printcolumn:name="Utilization",type=number,JSONPath=`.status.utilizationPercent`,format=float
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TokenBudget defines a monthly spend budget for inference workloads in a namespace.
// The controller tracks spend against the budget and generates PrometheusRule alerts
// when configured thresholds are crossed.
type TokenBudget struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec TokenBudgetSpec `json:"spec"`

	// +optional
	Status TokenBudgetStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TokenBudgetList contains a list of TokenBudget
type TokenBudgetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TokenBudget `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TokenBudget{}, &TokenBudgetList{})
}
