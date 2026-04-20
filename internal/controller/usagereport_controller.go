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

// modelKey identifies a model by its name and namespace for aggregation.
type modelKey struct {
	model     string
	namespace string
}

// tokenCounts is a running sum of input/output tokens for one aggregation key.
type tokenCounts struct {
	input  int64
	output int64
}

// +kubebuilder:rbac:groups=finops.infercost.ai,resources=usagereports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=usagereports/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=usagereports/finalizers,verbs=update
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=costprofiles,verbs=get;list;watch

// Reconcile computes usage costs for the reporting period by scraping inference pod
// token metrics and attributing costs using the referenced CostProfile's hourly rate.
func (r *UsageReportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var report finopsv1alpha1.UsageReport
	if err := r.Get(ctx, req.NamespacedName, &report); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.Info("reconciling UsageReport", "name", report.Name)

	var profile finopsv1alpha1.CostProfile
	profileKey := client.ObjectKey{Name: report.Spec.CostProfileRef, Namespace: req.Namespace}
	if err := r.Get(ctx, profileKey, &profile); err != nil {
		return r.handleMissingCostProfile(ctx, &report, err)
	}

	periodStart, periodEnd, hoursInPeriod := periodBounds(report.Spec.Schedule)

	modelTokens, nsTokens, err := r.scrapeTokens(ctx, &report)
	if err != nil {
		log.Error(err, "failed to list pods")
		return ctrl.Result{RequeueAfter: usageReportReconcileInterval}, nil
	}

	totalCost := profile.Status.HourlyCostUSD * hoursInPeriod
	byModel, byNamespace, nsCostData, totalIn, totalOut := buildBreakdowns(modelTokens, nsTokens, totalCost)
	totalTokens := totalIn + totalOut

	var costPerMillion float64
	if totalTokens > 0 {
		costPerMillion = totalCost / (float64(totalTokens) / 1_000_000)
	}

	periodStr := formatPeriod(report.Spec.Schedule, periodStart)
	computed := computedStatus{
		period:               periodStr,
		periodStart:          periodStart,
		periodEnd:            periodEnd,
		inputTokens:          totalIn,
		outputTokens:         totalOut,
		totalCost:            totalCost,
		costPerMillionTokens: costPerMillion,
		byModel:              byModel,
		byNamespace:          byNamespace,
	}

	if err := r.applyStatusIfChanged(ctx, &report, computed, totalTokens); err != nil {
		return ctrl.Result{}, err
	}

	if r.APIStore != nil {
		r.APIStore.SetNamespaceCosts(nsCostData)
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

// handleMissingCostProfile writes a CostProfileNotFound condition to the report
// only when the condition is not already in that state. Without the guard, every
// reconcile on a misconfigured report would re-trigger an Update event.
func (r *UsageReportReconciler) handleMissingCostProfile(ctx context.Context, report *finopsv1alpha1.UsageReport, getErr error) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	existing := meta.FindStatusCondition(report.Status.Conditions, "Ready")
	if existing != nil && existing.Status == metav1.ConditionFalse && existing.Reason == "CostProfileNotFound" {
		return ctrl.Result{RequeueAfter: usageReportReconcileInterval}, nil
	}
	now := metav1.Now()
	meta.SetStatusCondition(&report.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "CostProfileNotFound",
		Message:            fmt.Sprintf("CostProfile %q not found: %v", report.Spec.CostProfileRef, getErr),
		LastTransitionTime: now,
	})
	if statusErr := r.Status().Update(ctx, report); statusErr != nil {
		log.Error(statusErr, "failed to update UsageReport status")
	}
	return ctrl.Result{RequeueAfter: usageReportReconcileInterval}, nil
}

// periodBounds returns the reporting period start, end, and length in hours.
// Guards against a zero-length window so downstream proration never divides by zero.
func periodBounds(schedule finopsv1alpha1.ReportSchedule) (time.Time, time.Time, float64) {
	now := time.Now().UTC()
	start := computePeriodStart(schedule, now)
	end := now
	hours := end.Sub(start).Hours()
	if hours < 0.001 {
		hours = 0.001
	}
	return start, end, hours
}

// scrapeTokens lists pods that carry the LLMKube model label, filters by the
// report's namespace selector, scrapes token counters from each, and returns
// aggregated maps keyed by model+namespace and by namespace alone.
func (r *UsageReportReconciler) scrapeTokens(ctx context.Context, report *finopsv1alpha1.UsageReport) (map[modelKey]tokenCounts, map[string]tokenCounts, error) {
	log := logf.FromContext(ctx)

	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.MatchingLabels{}); err != nil {
		return nil, nil, err
	}

	nsFilter := make(map[string]bool, len(report.Spec.Namespaces))
	for _, ns := range report.Spec.Namespaces {
		nsFilter[ns] = true
	}

	modelTokens := make(map[modelKey]tokenCounts)
	nsTokens := make(map[string]tokenCounts)

	for i := range podList.Items {
		pod := &podList.Items[i]
		if !podIsScrapeable(pod, nsFilter) {
			continue
		}
		backend := scraper.ResolveBackend(pod.Annotations, pod.Labels)
		port := scraper.ResolveMetricsPort(backend, pod.Annotations, pod.Labels)
		endpoint := fmt.Sprintf("http://%s:%d/metrics", pod.Status.PodIP, port)
		im, err := scraper.Scrape(ctx, r.ScrapeClient, backend, endpoint)
		if err != nil {
			log.Error(err, "failed to scrape inference pod", "pod", pod.Name, "namespace", pod.Namespace, "backend", backend)
			continue
		}
		modelName := pod.Labels[modelLabel]
		input := int64(im.PromptTokensTotal)
		output := int64(im.PredictedTokensTotal)

		mk := modelKey{model: modelName, namespace: pod.Namespace}
		mExisting := modelTokens[mk]
		mExisting.input += input
		mExisting.output += output
		modelTokens[mk] = mExisting

		nsExisting := nsTokens[pod.Namespace]
		nsExisting.input += input
		nsExisting.output += output
		nsTokens[pod.Namespace] = nsExisting
	}
	return modelTokens, nsTokens, nil
}

// podIsScrapeable returns true when the pod has a model label, is Running with
// an IP, and either matches the namespace filter or the filter is empty.
func podIsScrapeable(pod *corev1.Pod, nsFilter map[string]bool) bool {
	if pod.Labels[modelLabel] == "" {
		return false
	}
	if pod.Status.Phase != corev1.PodRunning || pod.Status.PodIP == "" {
		return false
	}
	if len(nsFilter) > 0 && !nsFilter[pod.Namespace] {
		return false
	}
	return true
}

// buildBreakdowns turns the aggregated token maps into the CRD-shaped slices
// plus the API-store view, and returns the total input/output tokens for
// subsequent blended-cost computation.
func buildBreakdowns(
	modelTokens map[modelKey]tokenCounts,
	nsTokens map[string]tokenCounts,
	totalCost float64,
) (
	[]finopsv1alpha1.ModelCostBreakdown,
	[]finopsv1alpha1.NamespaceCostBreakdown,
	[]internalapi.NamespaceCostData,
	int64,
	int64,
) {
	var totalIn, totalOut int64
	for _, t := range modelTokens {
		totalIn += t.input
		totalOut += t.output
	}
	totalTokens := totalIn + totalOut

	byModel := make([]finopsv1alpha1.ModelCostBreakdown, 0, len(modelTokens))
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

	byNamespace := make([]finopsv1alpha1.NamespaceCostBreakdown, 0, len(nsTokens))
	nsCostData := make([]internalapi.NamespaceCostData, 0, len(nsTokens))
	for ns, t := range nsTokens {
		nsTotal := t.input + t.output
		var nsCost float64
		if totalTokens > 0 {
			nsCost = totalCost * (float64(nsTotal) / float64(totalTokens))
		}
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
		nsCostData = append(nsCostData, internalapi.NamespaceCostData{
			Namespace:        ns,
			EstimatedCostUSD: nsCost,
			TokenCount:       nsTotal,
			Models:           models,
		})
	}
	return byModel, byNamespace, nsCostData, totalIn, totalOut
}

// computedStatus is the intermediate view of a single reconcile's output,
// passed from the computation phase to the write phase so the Reconcile entry
// point stays short enough to read top-to-bottom.
type computedStatus struct {
	period               string
	periodStart          time.Time
	periodEnd            time.Time
	inputTokens          int64
	outputTokens         int64
	totalCost            float64
	costPerMillionTokens float64
	byModel              []finopsv1alpha1.ModelCostBreakdown
	byNamespace          []finopsv1alpha1.NamespaceCostBreakdown
}

// applyStatusIfChanged mutates report.Status to the computed view and writes
// to the apiserver only when the content has actually changed. This is the
// primary guard against the status-update → watch-event → requeue hot-loop
// that controller-runtime setups fall into when a reconcile always writes.
func (r *UsageReportReconciler) applyStatusIfChanged(ctx context.Context, report *finopsv1alpha1.UsageReport, c computedStatus, totalTokens int64) error {
	log := logf.FromContext(ctx)
	previous := report.Status.DeepCopy()

	metaNow := metav1.Now()
	metaStart := metav1.NewTime(c.periodStart)
	metaEnd := metav1.NewTime(c.periodEnd)

	report.Status.Period = c.period
	report.Status.PeriodStart = &metaStart
	report.Status.PeriodEnd = &metaEnd
	report.Status.InputTokens = c.inputTokens
	report.Status.OutputTokens = c.outputTokens
	report.Status.EstimatedCostUSD = c.totalCost
	report.Status.CostPerMillionTokens = c.costPerMillionTokens
	report.Status.ByModel = c.byModel
	report.Status.ByNamespace = c.byNamespace
	report.Status.LastUpdated = &metaNow

	meta.SetStatusCondition(&report.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "ReportComputed",
		Message:            fmt.Sprintf("Period %s: $%.4f across %d tokens", c.period, c.totalCost, totalTokens),
		LastTransitionTime: metaNow,
	})

	readyTransitioned := conditionTransitioned(previous.Conditions, report.Status.Conditions, "Ready")
	if !readyTransitioned && usageReportStatusContentEqual(previous, &report.Status) {
		log.V(1).Info("usage report unchanged, skipping status write",
			"period", c.period,
			"totalTokens", totalTokens,
		)
		return nil
	}
	if err := r.Status().Update(ctx, report); err != nil {
		log.Error(err, "failed to update UsageReport status")
		return err
	}
	return nil
}

// computePeriodStart returns the start of the reporting period based on the schedule.
func computePeriodStart(schedule finopsv1alpha1.ReportSchedule, now time.Time) time.Time {
	switch schedule {
	case finopsv1alpha1.ReportScheduleWeekly:
		weekday := now.Weekday()
		if weekday == time.Sunday {
			weekday = 7
		}
		daysSinceMonday := int(weekday) - int(time.Monday)
		return time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, time.UTC)
	case finopsv1alpha1.ReportScheduleMonthly:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
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
	default:
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
	if !modelBreakdownsEqual(a.ByModel, b.ByModel) {
		return false
	}
	return namespaceBreakdownsEqual(a.ByNamespace, b.ByNamespace)
}

func modelBreakdownsEqual(a, b []finopsv1alpha1.ModelCostBreakdown) bool {
	as := make([]string, 0, len(a))
	bs := make([]string, 0, len(b))
	for _, m := range a {
		as = append(as, fmt.Sprintf("%s/%s:%d+%d", m.Namespace, m.Model, m.InputTokens, m.OutputTokens))
	}
	for _, m := range b {
		bs = append(bs, fmt.Sprintf("%s/%s:%d+%d", m.Namespace, m.Model, m.InputTokens, m.OutputTokens))
	}
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func namespaceBreakdownsEqual(a, b []finopsv1alpha1.NamespaceCostBreakdown) bool {
	as := make([]string, 0, len(a))
	bs := make([]string, 0, len(b))
	for _, n := range a {
		as = append(as, fmt.Sprintf("%s:%d", n.Namespace, n.TokenCount))
	}
	for _, n := range b {
		bs = append(bs, fmt.Sprintf("%s:%d", n.Namespace, n.TokenCount))
	}
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
