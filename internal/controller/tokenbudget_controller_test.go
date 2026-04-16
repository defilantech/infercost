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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
	internalapi "github.com/defilantech/infercost/internal/api"
)

const conditionTypeBudgetOK = "BudgetOK"

var _ = Describe("TokenBudget Controller", func() {
	Context("When reconciling with no matching namespace spend data", func() {
		const budgetName = "test-budget-no-spend"

		ctx := context.Background()

		budgetKey := types.NamespacedName{
			Name:      budgetName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the TokenBudget resource")
			budget := &finopsv1alpha1.TokenBudget{}
			err := k8sClient.Get(ctx, budgetKey, budget)
			if err != nil && errors.IsNotFound(err) {
				budget = &finopsv1alpha1.TokenBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      budgetName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.TokenBudgetSpec{
						Scope: finopsv1alpha1.TokenBudgetScope{
							Namespace: "nonexistent-namespace",
						},
						MonthlyLimitUSD: 500,
						AlertThresholds: []finopsv1alpha1.AlertThreshold{
							{Percent: 80, Severity: "warning"},
							{Percent: 100, Severity: "critical"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, budget)).To(Succeed())
			}
		})

		AfterEach(func() {
			budget := &finopsv1alpha1.TokenBudget{}
			if err := k8sClient.Get(ctx, budgetKey, budget); err == nil {
				Expect(k8sClient.Delete(ctx, budget)).To(Succeed())
			}
		})

		It("should show zero spend and BudgetOK condition", func() {
			apiStore := internalapi.NewStore()
			reconciler := &TokenBudgetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				APIStore: apiStore,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: budgetKey,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

			By("verifying the status shows zero spend")
			updated := &finopsv1alpha1.TokenBudget{}
			Expect(k8sClient.Get(ctx, budgetKey, updated)).To(Succeed())
			Expect(updated.Status.CurrentSpendUSD).To(BeNumerically("==", 0))
			Expect(updated.Status.UtilizationPercent).To(BeNumerically("==", 0))
			Expect(updated.Status.PeriodStart).NotTo(BeNil())
			Expect(updated.Status.PeriodEnd).NotTo(BeNil())
			Expect(updated.Status.LastUpdated).NotTo(BeNil())

			By("verifying the BudgetOK condition is True")
			var budgetOK *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == conditionTypeBudgetOK {
					budgetOK = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(budgetOK).NotTo(BeNil())
			Expect(budgetOK.Status).To(Equal(metav1.ConditionTrue))
			Expect(budgetOK.Reason).To(Equal("WithinBudget"))

			By("verifying the API store was updated")
			budgets := apiStore.GetBudgets()
			Expect(budgets).To(HaveLen(1))
			Expect(budgets[0].Status).To(Equal("ok"))
		})
	})

	Context("When reconciling with spend below warning threshold", func() {
		const budgetName = "test-budget-below-threshold"

		ctx := context.Background()

		budgetKey := types.NamespacedName{
			Name:      budgetName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the TokenBudget resource")
			budget := &finopsv1alpha1.TokenBudget{}
			err := k8sClient.Get(ctx, budgetKey, budget)
			if err != nil && errors.IsNotFound(err) {
				budget = &finopsv1alpha1.TokenBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      budgetName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.TokenBudgetSpec{
						Scope: finopsv1alpha1.TokenBudgetScope{
							Namespace: "team-a",
						},
						MonthlyLimitUSD: 100,
						AlertThresholds: []finopsv1alpha1.AlertThreshold{
							{Percent: 80, Severity: "warning"},
							{Percent: 100, Severity: "critical"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, budget)).To(Succeed())
			}
		})

		AfterEach(func() {
			budget := &finopsv1alpha1.TokenBudget{}
			if err := k8sClient.Get(ctx, budgetKey, budget); err == nil {
				Expect(k8sClient.Delete(ctx, budget)).To(Succeed())
			}
		})

		It("should show correct spend and BudgetOK condition", func() {
			apiStore := internalapi.NewStore()
			// Pre-populate the API store with namespace cost data (50% utilization).
			apiStore.SetNamespaceCosts([]internalapi.NamespaceCostData{
				{Namespace: "team-a", EstimatedCostUSD: 50.0, TokenCount: 100000},
			})

			reconciler := &TokenBudgetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				APIStore: apiStore,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: budgetKey,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

			By("verifying the status shows correct spend and utilization")
			updated := &finopsv1alpha1.TokenBudget{}
			Expect(k8sClient.Get(ctx, budgetKey, updated)).To(Succeed())
			Expect(updated.Status.CurrentSpendUSD).To(BeNumerically("==", 50.0))
			Expect(updated.Status.UtilizationPercent).To(BeNumerically("==", 50.0))

			By("verifying the BudgetOK condition is True")
			var budgetOK *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == conditionTypeBudgetOK {
					budgetOK = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(budgetOK).NotTo(BeNil())
			Expect(budgetOK.Status).To(Equal(metav1.ConditionTrue))

			By("verifying the API store budget status is ok")
			budgets := apiStore.GetBudgets()
			Expect(budgets).To(HaveLen(1))
			Expect(budgets[0].Status).To(Equal("ok"))
			Expect(budgets[0].CurrentSpendUSD).To(BeNumerically("==", 50.0))
		})
	})

	Context("When reconciling with spend above warning threshold", func() {
		const budgetName = "test-budget-warning"

		ctx := context.Background()

		budgetKey := types.NamespacedName{
			Name:      budgetName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the TokenBudget resource")
			budget := &finopsv1alpha1.TokenBudget{}
			err := k8sClient.Get(ctx, budgetKey, budget)
			if err != nil && errors.IsNotFound(err) {
				budget = &finopsv1alpha1.TokenBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      budgetName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.TokenBudgetSpec{
						Scope: finopsv1alpha1.TokenBudgetScope{
							Namespace: "team-b",
						},
						MonthlyLimitUSD: 100,
						AlertThresholds: []finopsv1alpha1.AlertThreshold{
							{Percent: 80, Severity: "warning"},
							{Percent: 100, Severity: "critical"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, budget)).To(Succeed())
			}
		})

		AfterEach(func() {
			budget := &finopsv1alpha1.TokenBudget{}
			if err := k8sClient.Get(ctx, budgetKey, budget); err == nil {
				Expect(k8sClient.Delete(ctx, budget)).To(Succeed())
			}
		})

		It("should set BudgetWarning condition", func() {
			apiStore := internalapi.NewStore()
			// Pre-populate with 90% utilization (above warning, below critical).
			apiStore.SetNamespaceCosts([]internalapi.NamespaceCostData{
				{Namespace: "team-b", EstimatedCostUSD: 90.0, TokenCount: 200000},
			})

			reconciler := &TokenBudgetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				APIStore: apiStore,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: budgetKey,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

			By("verifying the status shows 90% utilization")
			updated := &finopsv1alpha1.TokenBudget{}
			Expect(k8sClient.Get(ctx, budgetKey, updated)).To(Succeed())
			Expect(updated.Status.CurrentSpendUSD).To(BeNumerically("==", 90.0))
			Expect(updated.Status.UtilizationPercent).To(BeNumerically("==", 90.0))

			By("verifying the BudgetWarning condition is True")
			var budgetWarning *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == "BudgetWarning" {
					budgetWarning = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(budgetWarning).NotTo(BeNil())
			Expect(budgetWarning.Status).To(Equal(metav1.ConditionTrue))
			Expect(budgetWarning.Reason).To(Equal("ThresholdCrossed"))

			By("verifying BudgetOK is False")
			var budgetOK *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == conditionTypeBudgetOK {
					budgetOK = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(budgetOK).NotTo(BeNil())
			Expect(budgetOK.Status).To(Equal(metav1.ConditionFalse))

			By("verifying BudgetExceeded is False")
			var budgetExceeded *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == "BudgetExceeded" {
					budgetExceeded = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(budgetExceeded).NotTo(BeNil())
			Expect(budgetExceeded.Status).To(Equal(metav1.ConditionFalse))

			By("verifying the API store budget status is warning")
			budgets := apiStore.GetBudgets()
			Expect(budgets).To(HaveLen(1))
			Expect(budgets[0].Status).To(Equal("warning"))
		})
	})

	Context("When reconciling with spend above critical threshold", func() {
		const budgetName = "test-budget-critical"

		ctx := context.Background()

		budgetKey := types.NamespacedName{
			Name:      budgetName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the TokenBudget resource")
			budget := &finopsv1alpha1.TokenBudget{}
			err := k8sClient.Get(ctx, budgetKey, budget)
			if err != nil && errors.IsNotFound(err) {
				budget = &finopsv1alpha1.TokenBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      budgetName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.TokenBudgetSpec{
						Scope: finopsv1alpha1.TokenBudgetScope{
							Namespace: "team-c",
						},
						MonthlyLimitUSD: 100,
						AlertThresholds: []finopsv1alpha1.AlertThreshold{
							{Percent: 80, Severity: "warning"},
							{Percent: 100, Severity: "critical"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, budget)).To(Succeed())
			}
		})

		AfterEach(func() {
			budget := &finopsv1alpha1.TokenBudget{}
			if err := k8sClient.Get(ctx, budgetKey, budget); err == nil {
				Expect(k8sClient.Delete(ctx, budget)).To(Succeed())
			}
		})

		It("should set BudgetExceeded condition", func() {
			apiStore := internalapi.NewStore()
			// Pre-populate with 120% utilization.
			apiStore.SetNamespaceCosts([]internalapi.NamespaceCostData{
				{Namespace: "team-c", EstimatedCostUSD: 120.0, TokenCount: 300000},
			})

			reconciler := &TokenBudgetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				APIStore: apiStore,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: budgetKey,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

			By("verifying the status shows 120% utilization")
			updated := &finopsv1alpha1.TokenBudget{}
			Expect(k8sClient.Get(ctx, budgetKey, updated)).To(Succeed())
			Expect(updated.Status.CurrentSpendUSD).To(BeNumerically("==", 120.0))
			Expect(updated.Status.UtilizationPercent).To(BeNumerically("==", 120.0))

			By("verifying the BudgetExceeded condition is True")
			var budgetExceeded *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == "BudgetExceeded" {
					budgetExceeded = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(budgetExceeded).NotTo(BeNil())
			Expect(budgetExceeded.Status).To(Equal(metav1.ConditionTrue))
			Expect(budgetExceeded.Reason).To(Equal("CriticalThresholdCrossed"))

			By("verifying BudgetWarning is also True (warning is also crossed)")
			var budgetWarning *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == "BudgetWarning" {
					budgetWarning = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(budgetWarning).NotTo(BeNil())
			Expect(budgetWarning.Status).To(Equal(metav1.ConditionTrue))

			By("verifying BudgetOK is False")
			var budgetOK *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == conditionTypeBudgetOK {
					budgetOK = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(budgetOK).NotTo(BeNil())
			Expect(budgetOK.Status).To(Equal(metav1.ConditionFalse))

			By("verifying the API store budget status is exceeded")
			budgets := apiStore.GetBudgets()
			Expect(budgets).To(HaveLen(1))
			Expect(budgets[0].Status).To(Equal("exceeded"))
		})
	})

	Context("When the TokenBudget does not exist", func() {
		It("should handle not-found gracefully", func() {
			reconciler := &TokenBudgetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				APIStore: internalapi.NewStore(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "nonexistent",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
