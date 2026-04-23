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

// HardwareSpec declares the GPU hardware economics for a node or pool.
type HardwareSpec struct {
	// gpuModel is the GPU model name (e.g. "NVIDIA GeForce RTX 5060 Ti", "NVIDIA H100 SXM5").
	// +kubebuilder:validation:MinLength=1
	GPUModel string `json:"gpuModel"`

	// gpuCount is the number of GPUs covered by this cost profile.
	// +kubebuilder:validation:Minimum=1
	GPUCount int32 `json:"gpuCount"`

	// purchasePriceUSD is the total purchase price of the GPU hardware in USD.
	PurchasePriceUSD float64 `json:"purchasePriceUSD"`

	// amortizationYears is the useful life over which the hardware cost is amortized.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	AmortizationYears int32 `json:"amortizationYears"`

	// maintenancePercentPerYear is the annual maintenance cost as a percentage of purchase price (0.0-1.0).
	// +optional
	MaintenancePercentPerYear float64 `json:"maintenancePercentPerYear,omitempty"`

	// tdpWatts is the thermal design power per GPU in watts. Used as fallback when DCGM
	// power metrics are unavailable. If omitted, real-time DCGM metrics are required.
	// +optional
	TDPWatts *int32 `json:"tdpWatts,omitempty"`
}

// ElectricitySpec declares electricity cost parameters.
type ElectricitySpec struct {
	// ratePerKWh is the electricity cost in USD per kilowatt-hour.
	RatePerKWh float64 `json:"ratePerKWh"`

	// pueFactor is the Power Usage Effectiveness factor. 1.0 means no overhead (typical for
	// homelabs), 1.2-1.6 for data centers. Defaults to 1.0.
	// +optional
	PUEFactor float64 `json:"pueFactor,omitempty"`

	// idleWattsThreshold is the total GPU power (in watts across all GPUs
	// covered by this profile) above which a sample is considered "active"
	// for utilization accounting. Samples at or below this threshold are
	// treated as idle: the hardware still costs money (amortization + idle
	// electricity), but those intervals don't inflate $/token dashboards.
	// When omitted, InferCost defaults to 20% of (TDPWatts × GPUCount),
	// or 30 W × GPUCount when TDP isn't declared.
	// +kubebuilder:validation:Minimum=0
	// +optional
	IdleWattsThreshold *float64 `json:"idleWattsThreshold,omitempty"`
}

// CostProfileSpec defines the hardware economics for computing inference costs.
type CostProfileSpec struct {
	// hardware declares GPU hardware cost parameters.
	// +required
	Hardware HardwareSpec `json:"hardware"`

	// electricity declares electricity cost parameters.
	// +required
	Electricity ElectricitySpec `json:"electricity"`

	// nodeSelector matches nodes whose inference workloads are covered by this cost profile.
	// Uses standard Kubernetes label selectors.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// namespaceSelector limits cost attribution to pods in matching namespaces.
	// If omitted, all namespaces on matching nodes are included.
	// +optional
	NamespaceSelector map[string]string `json:"namespaceSelector,omitempty"`
}

// CostProfileStatus defines the computed state of a CostProfile.
type CostProfileStatus struct {
	// hourlyCostUSD is the total computed hourly cost (amortization + electricity).
	// +optional
	HourlyCostUSD float64 `json:"hourlyCostUSD,omitempty"`

	// amortizationRatePerHour is the hardware amortization component of hourly cost.
	// +optional
	AmortizationRatePerHour float64 `json:"amortizationRatePerHour,omitempty"`

	// electricityCostPerHour is the electricity component of hourly cost at current power draw.
	// +optional
	ElectricityCostPerHour float64 `json:"electricityCostPerHour,omitempty"`

	// currentPowerDrawWatts is the real-time total GPU power draw from DCGM.
	// +optional
	CurrentPowerDrawWatts float64 `json:"currentPowerDrawWatts,omitempty"`

	// lastUpdated is the timestamp of the last cost computation.
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// conditions represent the current state of the CostProfile.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="GPU Model",type=string,JSONPath=`.spec.hardware.gpuModel`
// +kubebuilder:printcolumn:name="GPUs",type=integer,JSONPath=`.spec.hardware.gpuCount`
// +kubebuilder:printcolumn:name="$/hr",type=number,JSONPath=`.status.hourlyCostUSD`,format=float
// +kubebuilder:printcolumn:name="Power (W)",type=number,JSONPath=`.status.currentPowerDrawWatts`,format=float
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// CostProfile declares the hardware economics for a node or pool of GPUs.
// InferCost uses this to compute the true cost of inference workloads running
// on the matched nodes.
type CostProfile struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec CostProfileSpec `json:"spec"`

	// +optional
	Status CostProfileStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CostProfileList contains a list of CostProfile
type CostProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CostProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CostProfile{}, &CostProfileList{})
}
