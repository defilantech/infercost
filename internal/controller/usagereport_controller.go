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
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

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
		existing := meta.FindStatusCondition(report.Status.Conditions, "Ready")
		// Only write status if the error condition isn't already set with the same reason.
		// Without this, every reconcile on a missing-profile report re-writes status,
		// which re-enqueues events on top of the RequeueAfter schedule.
		if existing == nil || existing.Status != metav1.ConditionFalse || existing.Reason != "CostProfileNotFound" {
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

	// 8. Build the new status, then only write if content has actually changed.
	metaNow := metav1.Now()
	metaPeriodStart := metav1.NewTime(periodStart)
	metaPeriodEnd := metav1.NewTime(periodEnd)

	previous := report.Status.DeepCopy()

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

	// Skip the write entirely when the underlying tokens and breakdowns haven't
	// changed. The Ready condition is preserved because SetStatusCondition is a
	// no-op when the status/reason/message are identical. This keeps apiserver
	// writes proportional to actual workload activity rather than reconcile rate.
	readyTransitioned := conditionTransitioned(previous.Conditions, report.Status.Conditions, "Ready")
	if !readyTransitioned && usageReportStatusContentEqual(previous, &report.Status) {
		log.V(1).Info("usage report unchanged, skipping status write",
			"period", periodStr,
			"totalTokens", totalTokens,
		)
		if r.APIStore != nil {
			r.APIStore.SetNamespaceCosts(namespaceCostData)
		}
		return ctrl.Result{RequeueAfter: usageReportReconcileInterval}, nil
	}

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
//
// The builder uses GenerationChangedPredicate so watch events triggered by our
// own Status().Update calls do not re-enqueue the resource. Without this, the
// reconciler hot-loops: every reconcile writes status, every status write fires
// an Update event, every event re-enqueues the resource. Periodic recomputation
// is handled via the RequeueAfter return value, not via watch events.
func (r *UsageReportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1alpha1.UsageReport{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("usagereport").
		Complete(r)
}

// conditionTransitioned reports whether the named condition changed Status or
// Reason between two condition slices. LastTransitionTime is ignored because
// controller-runtime/meta updates it on every SetStatusCondition call when the
// status or reason flips, which is exactly what we want to detect.
func conditionTransitioned(previous, current []metav1.Condition, conditionType string) bool {
	a := meta.FindStatusCondition(previous, conditionType)
	b := meta.FindStatusCondition(current, conditionType)
	if a == nil && b == nil {
		return false
	}
	if a == nil || b == nil {
		return true
	}
	return a.Status != b.Status || a.Reason != b.Reason
}

// usageReportStatusContentEqual reports whether two UsageReport statuses are
// equivalent for the purposes of skipping a Status().Update. It deliberately
// ignores time-moving fields (PeriodEnd, LastUpdated, EstimatedCostUSD,
// CostPerMillionTokens, and per-entity cost breakdowns) because those drift on
// every reconcile even on an idle cluster — they track how long the current
// period has been open, not the underlying work. Skipping a write on idle
// eliminates apiserver churn even if a stray event slips past the predicate.
func usageReportStatusContentEqual(a, b *finopsv1alpha1.UsageReportStatus) bool {
	if a.Period != b.Period {
		return false
	}
	if a.InputTokens != b.InputTokens || a.OutputTokens != b.OutputTokens {
		return false
	}
	if len(a.ByModel) != len(b.ByModel) || len(a.ByNamespace) != len(b.ByNamespace) {
		return false
	}
	aModels := make([]string, 0, len(a.ByModel))
	bModels := make([]string, 0, len(b.ByModel))
	for _, m := range a.ByModel {
		aModels = append(aModels, fmt.Sprintf("%s/%s:%d+%d", m.Namespace, m.Model, m.InputTokens, m.OutputTokens))
	}
	for _, m := range b.ByModel {
		bModels = append(bModels, fmt.Sprintf("%s/%s:%d+%d", m.Namespace, m.Model, m.InputTokens, m.OutputTokens))
	}
	sort.Strings(aModels)
	sort.Strings(bModels)
	for i := range aModels {
		if aModels[i] != bModels[i] {
			return false
		}
	}
	aNS := make([]string, 0, len(a.ByNamespace))
	bNS := make([]string, 0, len(b.ByNamespace))
	for _, n := range a.ByNamespace {
		aNS = append(aNS, fmt.Sprintf("%s:%d", n.Namespace, n.TokenCount))
	}
	for _, n := range b.ByNamespace {
		bNS = append(bNS, fmt.Sprintf("%s:%d", n.Namespace, n.TokenCount))
	}
	sort.Strings(aNS)
	sort.Strings(bNS)
	for i := range aNS {
		if aNS[i] != bNS[i] {
			return false
		}
	}
	return true
}
