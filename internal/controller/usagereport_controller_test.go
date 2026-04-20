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
	"github.com/defilantech/infercost/internal/scraper"
)

var _ = Describe("UsageReport Controller", func() {
	Context("When reconciling with a valid CostProfile", func() {
		const (
			reportName      = "test-usagereport"
			costProfileName = "test-costprofile-for-report"
		)

		ctx := context.Background()

		reportKey := types.NamespacedName{
			Name:      reportName,
			Namespace: "default",
		}
		profileKey := types.NamespacedName{
			Name:      costProfileName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the CostProfile that the UsageReport references")
			profile := &finopsv1alpha1.CostProfile{}
			err := k8sClient.Get(ctx, profileKey, profile)
			if err != nil && errors.IsNotFound(err) {
				tdp := int32(150)
				profile = &finopsv1alpha1.CostProfile{
					ObjectMeta: metav1.ObjectMeta{
						Name:      costProfileName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.CostProfileSpec{
						Hardware: finopsv1alpha1.HardwareSpec{
							GPUModel:          "NVIDIA GeForce RTX 5060 Ti",
							GPUCount:          2,
							PurchasePriceUSD:  960,
							AmortizationYears: 3,
							TDPWatts:          &tdp,
						},
						Electricity: finopsv1alpha1.ElectricitySpec{
							RatePerKWh: 0.08,
							PUEFactor:  1.0,
						},
					},
				}
				Expect(k8sClient.Create(ctx, profile)).To(Succeed())
			}

			By("setting hourlyCostUSD on the CostProfile status")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, profileKey, profile); err != nil {
					return err
				}
				now := metav1.Now()
				profile.Status.HourlyCostUSD = 0.06
				profile.Status.LastUpdated = &now
				return k8sClient.Status().Update(ctx, profile)
			}, 5*time.Second, 250*time.Millisecond).Should(Succeed())

			By("creating the UsageReport")
			report := &finopsv1alpha1.UsageReport{}
			err = k8sClient.Get(ctx, reportKey, report)
			if err != nil && errors.IsNotFound(err) {
				report = &finopsv1alpha1.UsageReport{
					ObjectMeta: metav1.ObjectMeta{
						Name:      reportName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.UsageReportSpec{
						CostProfileRef: costProfileName,
						Schedule:       finopsv1alpha1.ReportScheduleDaily,
					},
				}
				Expect(k8sClient.Create(ctx, report)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("cleaning up the UsageReport")
			report := &finopsv1alpha1.UsageReport{}
			if err := k8sClient.Get(ctx, reportKey, report); err == nil {
				Expect(k8sClient.Delete(ctx, report)).To(Succeed())
			}

			By("cleaning up the CostProfile")
			profile := &finopsv1alpha1.CostProfile{}
			if err := k8sClient.Get(ctx, profileKey, profile); err == nil {
				Expect(k8sClient.Delete(ctx, profile)).To(Succeed())
			}
		})

		It("should reconcile and populate status fields", func() {
			apiStore := internalapi.NewStore()
			reconciler := &UsageReportReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				ScrapeClient: scraper.NewClient(5 * time.Second),
				APIStore:     apiStore,
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: reportKey,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))

			By("verifying the status was updated")
			updated := &finopsv1alpha1.UsageReport{}
			Expect(k8sClient.Get(ctx, reportKey, updated)).To(Succeed())
			Expect(updated.Status.Period).NotTo(BeEmpty())
			Expect(updated.Status.PeriodStart).NotTo(BeNil())
			Expect(updated.Status.PeriodEnd).NotTo(BeNil())
			Expect(updated.Status.LastUpdated).NotTo(BeNil())
			// Cost should be > 0 since hourlyCostUSD is 0.06 and hoursInPeriod > 0
			Expect(updated.Status.EstimatedCostUSD).To(BeNumerically(">", 0))

			By("verifying the Ready condition is set")
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
			Expect(readyCondition.Reason).To(Equal("ReportComputed"))
		})
	})

	Context("When the referenced CostProfile does not exist", func() {
		const reportName = "test-usagereport-missing-profile"

		ctx := context.Background()

		reportKey := types.NamespacedName{
			Name:      reportName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating a UsageReport referencing a nonexistent CostProfile")
			report := &finopsv1alpha1.UsageReport{}
			err := k8sClient.Get(ctx, reportKey, report)
			if err != nil && errors.IsNotFound(err) {
				report = &finopsv1alpha1.UsageReport{
					ObjectMeta: metav1.ObjectMeta{
						Name:      reportName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.UsageReportSpec{
						CostProfileRef: "nonexistent-costprofile",
						Schedule:       finopsv1alpha1.ReportScheduleDaily,
					},
				}
				Expect(k8sClient.Create(ctx, report)).To(Succeed())
			}
		})

		AfterEach(func() {
			report := &finopsv1alpha1.UsageReport{}
			if err := k8sClient.Get(ctx, reportKey, report); err == nil {
				Expect(k8sClient.Delete(ctx, report)).To(Succeed())
			}
		})

		It("should set an error condition on the UsageReport", func() {
			reconciler := &UsageReportReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				ScrapeClient: scraper.NewClient(5 * time.Second),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: reportKey,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))

			By("verifying the error condition is set")
			updated := &finopsv1alpha1.UsageReport{}
			Expect(k8sClient.Get(ctx, reportKey, updated)).To(Succeed())
			Expect(updated.Status.Conditions).NotTo(BeEmpty())

			var readyCondition *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == "Ready" {
					readyCondition = &updated.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("CostProfileNotFound"))
		})
	})

	Context("When namespace filtering is configured", func() {
		const (
			reportName      = "test-usagereport-ns-filter"
			costProfileName = "test-costprofile-ns-filter"
		)

		ctx := context.Background()

		reportKey := types.NamespacedName{
			Name:      reportName,
			Namespace: "default",
		}
		profileKey := types.NamespacedName{
			Name:      costProfileName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the CostProfile")
			profile := &finopsv1alpha1.CostProfile{}
			err := k8sClient.Get(ctx, profileKey, profile)
			if err != nil && errors.IsNotFound(err) {
				tdp := int32(150)
				profile = &finopsv1alpha1.CostProfile{
					ObjectMeta: metav1.ObjectMeta{
						Name:      costProfileName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.CostProfileSpec{
						Hardware: finopsv1alpha1.HardwareSpec{
							GPUModel:          "NVIDIA GeForce RTX 5060 Ti",
							GPUCount:          1,
							PurchasePriceUSD:  480,
							AmortizationYears: 3,
							TDPWatts:          &tdp,
						},
						Electricity: finopsv1alpha1.ElectricitySpec{
							RatePerKWh: 0.08,
							PUEFactor:  1.0,
						},
					},
				}
				Expect(k8sClient.Create(ctx, profile)).To(Succeed())
			}

			By("setting hourlyCostUSD on the CostProfile status")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, profileKey, profile); err != nil {
					return err
				}
				now := metav1.Now()
				profile.Status.HourlyCostUSD = 0.03
				profile.Status.LastUpdated = &now
				return k8sClient.Status().Update(ctx, profile)
			}, 5*time.Second, 250*time.Millisecond).Should(Succeed())

			By("creating the UsageReport with namespace filter")
			report := &finopsv1alpha1.UsageReport{}
			err = k8sClient.Get(ctx, reportKey, report)
			if err != nil && errors.IsNotFound(err) {
				report = &finopsv1alpha1.UsageReport{
					ObjectMeta: metav1.ObjectMeta{
						Name:      reportName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.UsageReportSpec{
						CostProfileRef: costProfileName,
						Schedule:       finopsv1alpha1.ReportScheduleWeekly,
						Namespaces:     []string{"team-a", "team-b"},
					},
				}
				Expect(k8sClient.Create(ctx, report)).To(Succeed())
			}
		})

		AfterEach(func() {
			report := &finopsv1alpha1.UsageReport{}
			if err := k8sClient.Get(ctx, reportKey, report); err == nil {
				Expect(k8sClient.Delete(ctx, report)).To(Succeed())
			}
			profile := &finopsv1alpha1.CostProfile{}
			if err := k8sClient.Get(ctx, profileKey, profile); err == nil {
				Expect(k8sClient.Delete(ctx, profile)).To(Succeed())
			}
		})

		It("should reconcile successfully with namespace filtering", func() {
			reconciler := &UsageReportReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				ScrapeClient: scraper.NewClient(5 * time.Second),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: reportKey,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))

			By("verifying the status was updated with weekly period")
			updated := &finopsv1alpha1.UsageReport{}
			Expect(k8sClient.Get(ctx, reportKey, updated)).To(Succeed())
			Expect(updated.Status.Period).To(MatchRegexp(`^\d{4}-W\d{2}$`))
			Expect(updated.Status.PeriodStart).NotTo(BeNil())

			By("verifying no namespace breakdowns since no pods exist in team-a or team-b")
			Expect(updated.Status.ByNamespace).To(BeEmpty())
			Expect(updated.Status.ByModel).To(BeEmpty())
		})
	})

	Context("When the UsageReport does not exist", func() {
		It("should handle not-found gracefully", func() {
			reconciler := &UsageReportReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				ScrapeClient: scraper.NewClient(5 * time.Second),
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

	// Regression test for #19: consecutive reconciles on an idle cluster must
	// not keep writing status. The first reconcile transitions the condition and
	// persists content; the second reconcile with no token change must be a
	// no-op on the status subresource so we don't re-trigger watch events.
	Context("When reconciling twice in a row with no underlying change", func() {
		const (
			reportName      = "test-usagereport-idempotent"
			costProfileName = "test-costprofile-idempotent"
		)

		ctx := context.Background()

		reportKey := types.NamespacedName{
			Name:      reportName,
			Namespace: "default",
		}
		profileKey := types.NamespacedName{
			Name:      costProfileName,
			Namespace: "default",
		}

		BeforeEach(func() {
			profile := &finopsv1alpha1.CostProfile{}
			err := k8sClient.Get(ctx, profileKey, profile)
			if err != nil && errors.IsNotFound(err) {
				tdp := int32(150)
				profile = &finopsv1alpha1.CostProfile{
					ObjectMeta: metav1.ObjectMeta{
						Name:      costProfileName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.CostProfileSpec{
						Hardware: finopsv1alpha1.HardwareSpec{
							GPUModel:          "NVIDIA GeForce RTX 5060 Ti",
							GPUCount:          1,
							PurchasePriceUSD:  480,
							AmortizationYears: 3,
							TDPWatts:          &tdp,
						},
						Electricity: finopsv1alpha1.ElectricitySpec{
							RatePerKWh: 0.08,
							PUEFactor:  1.0,
						},
					},
				}
				Expect(k8sClient.Create(ctx, profile)).To(Succeed())
			}

			Eventually(func() error {
				if err := k8sClient.Get(ctx, profileKey, profile); err != nil {
					return err
				}
				now := metav1.Now()
				profile.Status.HourlyCostUSD = 0.03
				profile.Status.LastUpdated = &now
				return k8sClient.Status().Update(ctx, profile)
			}, 5*time.Second, 250*time.Millisecond).Should(Succeed())

			report := &finopsv1alpha1.UsageReport{}
			err = k8sClient.Get(ctx, reportKey, report)
			if err != nil && errors.IsNotFound(err) {
				report = &finopsv1alpha1.UsageReport{
					ObjectMeta: metav1.ObjectMeta{
						Name:      reportName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.UsageReportSpec{
						CostProfileRef: costProfileName,
						Schedule:       finopsv1alpha1.ReportScheduleDaily,
					},
				}
				Expect(k8sClient.Create(ctx, report)).To(Succeed())
			}
		})

		AfterEach(func() {
			report := &finopsv1alpha1.UsageReport{}
			if err := k8sClient.Get(ctx, reportKey, report); err == nil {
				Expect(k8sClient.Delete(ctx, report)).To(Succeed())
			}
			profile := &finopsv1alpha1.CostProfile{}
			if err := k8sClient.Get(ctx, profileKey, profile); err == nil {
				Expect(k8sClient.Delete(ctx, profile)).To(Succeed())
			}
		})

		It("second reconcile must not write status when nothing changed", func() {
			reconciler := &UsageReportReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				ScrapeClient: scraper.NewClient(5 * time.Second),
			}

			By("first reconcile writes initial status")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: reportKey})
			Expect(err).NotTo(HaveOccurred())

			afterFirst := &finopsv1alpha1.UsageReport{}
			Expect(k8sClient.Get(ctx, reportKey, afterFirst)).To(Succeed())
			Expect(afterFirst.Status.Conditions).NotTo(BeEmpty())
			rvAfterFirst := afterFirst.ResourceVersion

			By("second reconcile must be a no-op on the status subresource")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: reportKey})
			Expect(err).NotTo(HaveOccurred())

			afterSecond := &finopsv1alpha1.UsageReport{}
			Expect(k8sClient.Get(ctx, reportKey, afterSecond)).To(Succeed())
			Expect(afterSecond.ResourceVersion).To(Equal(rvAfterFirst),
				"resourceVersion changed between idempotent reconciles — status was rewritten, which triggers hot-loop")
		})
	})

	// Regression test for #19: back-to-back reconciles when the referenced
	// CostProfile is missing must not keep re-writing the same error condition.
	Context("When CostProfile is missing, repeat reconciles must be no-ops", func() {
		const reportName = "test-usagereport-missing-idempotent"

		ctx := context.Background()
		reportKey := types.NamespacedName{Name: reportName, Namespace: "default"}

		BeforeEach(func() {
			report := &finopsv1alpha1.UsageReport{}
			err := k8sClient.Get(ctx, reportKey, report)
			if err != nil && errors.IsNotFound(err) {
				report = &finopsv1alpha1.UsageReport{
					ObjectMeta: metav1.ObjectMeta{
						Name:      reportName,
						Namespace: "default",
					},
					Spec: finopsv1alpha1.UsageReportSpec{
						CostProfileRef: "nonexistent-costprofile-xyz",
						Schedule:       finopsv1alpha1.ReportScheduleDaily,
					},
				}
				Expect(k8sClient.Create(ctx, report)).To(Succeed())
			}
		})

		AfterEach(func() {
			report := &finopsv1alpha1.UsageReport{}
			if err := k8sClient.Get(ctx, reportKey, report); err == nil {
				Expect(k8sClient.Delete(ctx, report)).To(Succeed())
			}
		})

		It("does not rewrite status when error condition is already set", func() {
			reconciler := &UsageReportReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				ScrapeClient: scraper.NewClient(5 * time.Second),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: reportKey})
			Expect(err).NotTo(HaveOccurred())

			afterFirst := &finopsv1alpha1.UsageReport{}
			Expect(k8sClient.Get(ctx, reportKey, afterFirst)).To(Succeed())
			rvAfterFirst := afterFirst.ResourceVersion

			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: reportKey})
			Expect(err).NotTo(HaveOccurred())

			afterSecond := &finopsv1alpha1.UsageReport{}
			Expect(k8sClient.Get(ctx, reportKey, afterSecond)).To(Succeed())
			Expect(afterSecond.ResourceVersion).To(Equal(rvAfterFirst),
				"resourceVersion changed between idempotent reconciles with missing CostProfile")
		})
	})
})
