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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
	internalapi "github.com/defilantech/infercost/internal/api"
	"github.com/defilantech/infercost/internal/scraper"
)

const (
	// How often to re-scrape and recompute usage reports.
	usageReportReconcileInterval = 5 * time.Minute
)

// UsageReportReconciler reconciles a UsageReport object
type UsageReportReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ScrapeClient *scraper.Client
	DCGMEndpoint string
	APIStore     *internalapi.Store
}

// +kubebuilder:rbac:groups=finops.infercost.ai,resources=usagereports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=usagereports/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=usagereports/finalizers,verbs=update
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=costprofiles,verbs=get;list;watch

// Reconcile computes usage costs for the reporting period by scraping inference pod
// token metrics and attributing costs using the referenced CostProfile's hourly rate.
func (r *UsageReportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch the UsageReport.
	var report finopsv1alpha1.UsageReport
	if err := r.Get(ctx, req.NamespacedName, &report); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("reconciling UsageReport", "name", report.Name)

	// 2. Fetch the referenced CostProfile to get hourlyCostUSD.
	//    CostProfile is looked up in the same namespace as the UsageReport.
	var profile finopsv1alpha1.CostProfile
	if err := r.Get(ctx, client.ObjectKey{Name: report.Spec.CostProfileRef, Namespace: req.Namespace}, &profile); err != nil {
		now := metav1.Now()
		meta.SetStatusCondition(&report.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "CostProfileNotFound",
			Message:            fmt.Sprintf("CostProfile %q not found: %v", report.Spec.CostProfileRef, err),
			LastTransitionTime: now,
		})
		if statusErr := r.Status().Update(ctx, &report); statusErr != nil {
			log.Error(statusErr, "failed to update UsageReport status")
		}
		return ctrl.Result{RequeueAfter: usageReportReconcileInterval}, nil
	}

	hourlyCostUSD := profile.Status.HourlyCostUSD

	// 3. Determine the reporting period.
	now := time.Now().UTC()
	periodStart := computePeriodStart(report.Spec.Schedule, now)
	periodEnd := now
	hoursInPeriod := periodEnd.Sub(periodStart).Hours()
	if hoursInPeriod < 0.001 {
		hoursInPeriod = 0.001
	}

	// 4. List inference pods with the model label.
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.MatchingLabels{}); err != nil {
		log.Error(err, "failed to list pods")
		return ctrl.Result{RequeueAfter: usageReportReconcileInterval}, nil
	}

	// Build namespace filter set for fast lookup.
	nsFilter := make(map[string]bool, len(report.Spec.Namespaces))
	for _, ns := range report.Spec.Namespaces {
		nsFilter[ns] = true
	}

	// 5. Scrape tokens from each qualifying pod.
	type modelKey struct {
		model     string
		namespace string
	}
	modelTokens := make(map[modelKey]struct{ input, output int64 })
	nsTokens := make(map[string]struct{ input, output int64 })

	for i := range podList.Items {
		pod := &podList.Items[i]
		modelName := pod.Labels[modelLabel]
		if modelName == "" {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning || pod.Status.PodIP == "" {
			continue
		}

		// Filter by namespaces if spec.namespaces is set.
		if len(nsFilter) > 0 && !nsFilter[pod.Namespace] {
			continue
		}

		endpoint := fmt.Sprintf("http://%s:8080/metrics", pod.Status.PodIP)
		im, err := scraper.ScrapeLlamaCPP(ctx, r.ScrapeClient, endpoint)
		if err != nil {
			log.Error(err, "failed to scrape inference pod", "pod", pod.Name, "namespace", pod.Namespace)
			continue
		}

		inputTokens := int64(im.PromptTokensTotal)
		outputTokens := int64(im.PredictedTokensTotal)

		// Aggregate by model+namespace.
		mk := modelKey{model: modelName, namespace: pod.Namespace}
		existing := modelTokens[mk]
		existing.input += inputTokens
		existing.output += outputTokens
		modelTokens[mk] = existing

		// Aggregate by namespace.
		nsExisting := nsTokens[pod.Namespace]
		nsExisting.input += inputTokens
		nsExisting.output += outputTokens
		nsTokens[pod.Namespace] = nsExisting
	}

	// 6. Compute costs prorated by each entity's token share.
	var totalInputTokens, totalOutputTokens int64
	for _, t := range modelTokens {
		totalInputTokens += t.input
		totalOutputTokens += t.output
	}
	totalTokens := totalInputTokens + totalOutputTokens
	totalCost := hourlyCostUSD * hoursInPeriod

	// Build by-model breakdown.
	var byModel []finopsv1alpha1.ModelCostBreakdown
	for mk, t := range modelTokens {
		modelTotal := t.input + t.output
		var modelCost, costPerMillion float64
		if totalTokens > 0 {
			modelCost = totalCost * (float64(modelTotal) / float64(totalTokens))
		}
		if modelTotal > 0 {
			costPerMillion = modelCost / (float64(modelTotal) / 1_000_000)
		}
		byModel = append(byModel, finopsv1alpha1.ModelCostBreakdown{
			Model:                mk.model,
			Namespace:            mk.namespace,
			InputTokens:          t.input,
			OutputTokens:         t.output,
			EstimatedCostUSD:     modelCost,
			CostPerMillionTokens: costPerMillion,
		})
	}

	// Build by-namespace breakdown.
	var byNamespace []finopsv1alpha1.NamespaceCostBreakdown
	var namespaceCostData []internalapi.NamespaceCostData
	for ns, t := range nsTokens {
		nsTotal := t.input + t.output
		var nsCost float64
		if totalTokens > 0 {
			nsCost = totalCost * (float64(nsTotal) / float64(totalTokens))
		}

		// Collect unique models for this namespace.
		var models []string
		for mk := range modelTokens {
			if mk.namespace == ns {
				models = append(models, mk.model)
			}
		}

		byNamespace = append(byNamespace, finopsv1alpha1.NamespaceCostBreakdown{
			Namespace:        ns,
			EstimatedCostUSD: nsCost,
			TokenCount:       nsTotal,
		})

		namespaceCostData = append(namespaceCostData, internalapi.NamespaceCostData{
			Namespace:        ns,
			EstimatedCostUSD: nsCost,
			TokenCount:       nsTotal,
			Models:           models,
		})
	}

	// 7. Compute blended cost per million tokens.
	var costPerMillionTokens float64
	if totalTokens > 0 {
		costPerMillionTokens = totalCost / (float64(totalTokens) / 1_000_000)
	}

	// Format period string based on schedule.
	periodStr := formatPeriod(report.Spec.Schedule, periodStart)

	// 8. Update status.
	metaNow := metav1.Now()
	metaPeriodStart := metav1.NewTime(periodStart)
	metaPeriodEnd := metav1.NewTime(periodEnd)

	report.Status.Period = periodStr
	report.Status.PeriodStart = &metaPeriodStart
	report.Status.PeriodEnd = &metaPeriodEnd
	report.Status.InputTokens = totalInputTokens
	report.Status.OutputTokens = totalOutputTokens
	report.Status.EstimatedCostUSD = totalCost
	report.Status.CostPerMillionTokens = costPerMillionTokens
	report.Status.ByModel = byModel
	report.Status.ByNamespace = byNamespace
	report.Status.LastUpdated = &metaNow

	meta.SetStatusCondition(&report.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "ReportComputed",
		Message:            fmt.Sprintf("Period %s: $%.4f across %d tokens", periodStr, totalCost, totalTokens),
		LastTransitionTime: metaNow,
	})

	if err := r.Status().Update(ctx, &report); err != nil {
		log.Error(err, "failed to update UsageReport status")
		return ctrl.Result{}, err
	}

	// 9. Update the API store with namespace cost data.
	if r.APIStore != nil {
		r.APIStore.SetNamespaceCosts(namespaceCostData)
	}

	log.Info("usage report computed",
		"period", periodStr,
		"totalCost", fmt.Sprintf("$%.4f", totalCost),
		"totalTokens", totalTokens,
		"models", len(byModel),
		"namespaces", len(byNamespace),
	)

	return ctrl.Result{RequeueAfter: usageReportReconcileInterval}, nil
}

// computePeriodStart returns the start of the reporting period based on the schedule.
func computePeriodStart(schedule finopsv1alpha1.ReportSchedule, now time.Time) time.Time {
	switch schedule {
	case finopsv1alpha1.ReportScheduleWeekly:
		// Monday 00:00 UTC of the current week.
		weekday := now.Weekday()
		if weekday == time.Sunday {
			weekday = 7
		}
		daysSinceMonday := int(weekday) - int(time.Monday)
		return time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, time.UTC)
	case finopsv1alpha1.ReportScheduleMonthly:
		// 1st of the current month 00:00 UTC.
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	default: // daily
		// Midnight today UTC.
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}
}

// formatPeriod returns a human-readable period string.
func formatPeriod(schedule finopsv1alpha1.ReportSchedule, start time.Time) string {
	switch schedule {
	case finopsv1alpha1.ReportScheduleWeekly:
		year, week := start.ISOWeek()
		return fmt.Sprintf("%d-W%02d", year, week)
	case finopsv1alpha1.ReportScheduleMonthly:
		return start.Format("2006-01")
	default: // daily
		return start.Format("2006-01-02")
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *UsageReportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1alpha1.UsageReport{}).
		Named("usagereport").
		Complete(r)
}
