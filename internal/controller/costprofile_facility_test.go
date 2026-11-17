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

package controller

import (
	"context"
	"math"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
	"github.com/defilantech/infercost/internal/scraper"
)

// TestFacilityMonthlyUSD pins how a facility block becomes one monthly
// figure: the recurring charges summed, times share, with no block or no share
// meaning none and the whole contract respectively.
func TestFacilityMonthlyUSD(t *testing.T) {
	charges := []finopsv1alpha1.RecurringCost{
		{Name: "space", MonthlyUSD: 600},
		{Name: "power", MonthlyUSD: 300},
		{Name: "network", MonthlyUSD: 100},
	}
	tests := []struct {
		name     string
		facility *finopsv1alpha1.FacilitySpec
		want     float64
	}{
		{"no facility block", nil, 0},
		{"share omitted carries the whole contract", &finopsv1alpha1.FacilitySpec{Recurring: charges}, 1000},
		{"share applies to the sum", &finopsv1alpha1.FacilitySpec{Recurring: charges, Share: new(float64(0.25))}, 250},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.facility.MonthlyUSD(); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("MonthlyUSD() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestComputeBreakEvenCountsFacility: facility charges are a fixed daily cost
// like amortization, so they raise the tokens per day needed to beat a cloud
// API.
func TestComputeBreakEvenCountsFacility(t *testing.T) {
	profile := func(facilityPerHour float64) *finopsv1alpha1.CostProfile {
		return &finopsv1alpha1.CostProfile{
			Spec: finopsv1alpha1.CostProfileSpec{
				Hardware:    finopsv1alpha1.HardwareSpec{GPUModel: "test", GPUCount: 1, AmortizationYears: 1},
				Electricity: finopsv1alpha1.ElectricitySpec{RatePerKWh: 0, PUEFactor: 1.0},
			},
			Status: finopsv1alpha1.CostProfileStatus{AmortizationRatePerHour: 1.0, FacilityCostPerHour: facilityPerHour},
		}
	}
	without, _ := computeBreakEven(profile(0), 1_000_000, 1_000_000, 24)
	with, _ := computeBreakEven(profile(2.0), 1_000_000, 1_000_000, 24)
	if len(without) == 0 || len(without) != len(with) {
		t.Fatalf("break-even entries: %d without facility, %d with", len(without), len(with))
	}
	for i := range without {
		// Daily fixed cost goes from $24 to $72, so break-even triples.
		ratio := float64(with[i].BreakEvenTokensPerDay) / float64(without[i].BreakEvenTokensPerDay)
		if math.Abs(ratio-3.0) > 0.01 {
			t.Errorf("%s/%s: break-even %d with facility vs %d without, ratio %.3f, want 3",
				with[i].Provider, with[i].Model, with[i].BreakEvenTokensPerDay, without[i].BreakEvenTokensPerDay, ratio)
		}
	}
}

var _ = Describe("CostProfile facility costs", func() {
	ctx := context.Background()

	newProfile := func(name string, facility *finopsv1alpha1.FacilitySpec) *finopsv1alpha1.CostProfile {
		tdp := int32(500)
		return &finopsv1alpha1.CostProfile{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: finopsv1alpha1.CostProfileSpec{
				Hardware: finopsv1alpha1.HardwareSpec{
					GPUModel:          "NVIDIA B200",
					GPUCount:          2,
					PurchasePriceUSD:  8760, // $1/hour over one year
					AmortizationYears: 1,
					TDPWatts:          &tdp,
				},
				Electricity:  finopsv1alpha1.ElectricitySpec{RatePerKWh: 0, PUEFactor: 1.0},
				NodeSelector: map[string]string{"kubernetes.io/hostname": "facility-node"},
				Facility:     facility,
			},
		}
	}

	It("adds the facility charges to the hourly cost, with no electricity under flat power", func() {
		profile := newProfile("facility-flat-power", &finopsv1alpha1.FacilitySpec{
			Recurring: []finopsv1alpha1.RecurringCost{
				{Name: "space", MonthlyUSD: 1000},
				{Name: "power", MonthlyUSD: 400},
				{Name: "network", MonthlyUSD: 60},
			},
			Share: new(float64(0.5)), // $730/month carried: $1/hour
		})
		Expect(k8sClient.Create(ctx, profile)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, profile)).To(Succeed()) })

		r := &CostProfileReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ScrapeClient: scraper.NewClient(5 * time.Second)}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: profile.Name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())

		updated := &finopsv1alpha1.CostProfile{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: profile.Name, Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Status.FacilityCostPerHour).To(BeNumerically("~", 1.0, 1e-9))
		Expect(updated.Status.ElectricityCostPerHour).To(BeZero())
		Expect(updated.Status.AmortizationRatePerHour).To(BeNumerically("~", 1.0, 1e-9))
		Expect(updated.Status.HourlyCostUSD).To(BeNumerically("~", 2.0, 1e-9))
	})

	It("rejects a facility block the API cannot use", func() {
		invalid := map[string]*finopsv1alpha1.FacilitySpec{
			"facility-share-zero":       {Recurring: []finopsv1alpha1.RecurringCost{{Name: "space", MonthlyUSD: 1}}, Share: new(float64(0))},
			"facility-share-above-one":  {Recurring: []finopsv1alpha1.RecurringCost{{Name: "space", MonthlyUSD: 1}}, Share: new(float64(1.5))},
			"facility-negative-charge":  {Recurring: []finopsv1alpha1.RecurringCost{{Name: "space", MonthlyUSD: -1}}},
			"facility-no-charges":       {Recurring: []finopsv1alpha1.RecurringCost{}},
			"facility-duplicate-charge": {Recurring: []finopsv1alpha1.RecurringCost{{Name: "power", MonthlyUSD: 1}, {Name: "power", MonthlyUSD: 2}}},
		}
		for name, facility := range invalid {
			Expect(k8sClient.Create(ctx, newProfile(name, facility))).NotTo(Succeed(), name)
		}
	})
})
