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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
	"github.com/defilantech/infercost/internal/scraper"
)

var _ = Describe("CostProfile Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-costprofile"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		costprofile := &finopsv1alpha1.CostProfile{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind CostProfile")
			err := k8sClient.Get(ctx, typeNamespacedName, costprofile)
			if err != nil && errors.IsNotFound(err) {
				tdp := int32(150)
				resource := &finopsv1alpha1.CostProfile{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.CostProfileSpec{
						Hardware: finopsv1alpha1.HardwareSpec{
							GPUModel:                  "NVIDIA GeForce RTX 5060 Ti",
							GPUCount:                  2,
							PurchasePriceUSD:          960,
							AmortizationYears:         3,
							MaintenancePercentPerYear: 0.05,
							TDPWatts:                  &tdp,
						},
						Electricity: finopsv1alpha1.ElectricitySpec{
							RatePerKWh: 0.08,
							PUEFactor:  1.0,
						},
						NodeSelector: map[string]string{
							"kubernetes.io/hostname": "test-node",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &finopsv1alpha1.CostProfile{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance CostProfile")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile and compute costs using TDP fallback", func() {
			By("Reconciling the created resource")
			controllerReconciler := &CostProfileReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				ScrapeClient: scraper.NewClient(5 * time.Second),
				DCGMEndpoint: "", // No DCGM — should fall back to TDP
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			By("Verifying the status was updated")
			updated := &finopsv1alpha1.CostProfile{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.HourlyCostUSD).To(BeNumerically(">", 0))
			Expect(updated.Status.AmortizationRatePerHour).To(BeNumerically(">", 0))
			Expect(updated.Status.ElectricityCostPerHour).To(BeNumerically(">", 0))
			Expect(updated.Status.CurrentPowerDrawWatts).To(BeNumerically("==", 300)) // 2 GPUs * 150W TDP
			Expect(updated.Status.LastUpdated).NotTo(BeNil())

			By("Verifying the Ready condition is set")
			Expect(updated.Status.Conditions).NotTo(BeEmpty())
			var readyCondition *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == "Ready" {
					readyCondition = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal("CostComputed"))
		})

		It("should handle not-found gracefully", func() {
			controllerReconciler := &CostProfileReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				ScrapeClient: scraper.NewClient(5 * time.Second),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "nonexistent",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
