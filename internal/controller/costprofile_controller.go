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
	"sync"
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
	"github.com/defilantech/infercost/internal/calculator"
	infercostmetrics "github.com/defilantech/infercost/internal/metrics"
	"github.com/defilantech/infercost/internal/scraper"
	"github.com/defilantech/infercost/internal/utilization"
)

const (
	// How often to re-scrape metrics and recompute costs.
	reconcileInterval = 30 * time.Second

	// Label on inference pods that identifies the model name (LLMKube convention).
	modelLabel = "inference.llmkube.dev/model"
)

// DCGM status reporting on CostProfile.status.conditions.
const (
	// ConditionDCGMReachable reports whether InferCost can read real-time GPU
	// power metrics from a DCGM exporter. When False, the cost engine falls
	// back to the TDP declared on the CostProfile spec — accurate enough for
	// order-of-magnitude numbers, but it cannot track dynamic load.
	ConditionDCGMReachable = "DCGMReachable"

	ReasonDCGMHealthy       = "DCGMHealthy"
	ReasonDCGMNotConfigured = "DCGMNotConfigured"
	ReasonDCGMScrapeError   = "DCGMScrapeError"
	ReasonDCGMNoReadings    = "DCGMNoReadings"
)

// CostProfileReconciler reconciles a CostProfile object.
type CostProfileReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ScrapeClient *scraper.Client
	DCGMEndpoint string // DCGM exporter service URL
	APIStore     *internalapi.Store

	// Sampler records per-tick GPU power draw so UsageReports can answer
	// "what fraction of this period was the GPU actually working?". Shared
	// with the UsageReport reconciler; both are constructed with the same
	// *Sampler instance in cmd/main.go.
	Sampler *utilization.Sampler

	// tokenSnapshots tracks previous token readings for rate computation.
	tokenSnapshots map[string]calculator.TokenSnapshot
	snapshotMu     sync.Mutex
}

// +kubebuilder:rbac:groups=finops.infercost.ai,resources=costprofiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=costprofiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=costprofiles/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile computes the hourly infrastructure cost for a CostProfile by combining
// hardware economics (amortization, electricity) with real-time GPU power draw from
// DCGM or TDP fallback. It scrapes inference pod token metrics, updates Prometheus
// metrics and the API store, and writes computed costs to the CostProfile status.
func (r *CostProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the CostProfile.
	var profile finopsv1alpha1.CostProfile
	if err := r.Get(ctx, req.NamespacedName, &profile); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("reconciling CostProfile", "name", profile.Name)

	// 1. Compute static hourly costs from the CostProfile spec.
	hw := calculator.HardwareCosts{
		PurchasePriceUSD:          profile.Spec.Hardware.PurchasePriceUSD,
		AmortizationYears:         profile.Spec.Hardware.AmortizationYears,
		MaintenancePercentPerYear: profile.Spec.Hardware.MaintenancePercentPerYear,
		RatePerKWh:                profile.Spec.Electricity.RatePerKWh,
		PUEFactor:                 profile.Spec.Electricity.PUEFactor,
	}

	// 2. Scrape DCGM for real-time GPU power draw, and surface any fallback
	// path as a DCGMReachable condition so operators can tell at a glance
	// whether their dashboards will show real-time load or a flat TDP estimate.
	totalPowerW, dcgmCondition := r.readDCGMPower(ctx, profile)

	// 2a. Record the sample for utilization accounting. Computes the active
	// threshold from spec with a sensible default when the operator hasn't
	// declared one.
	if r.Sampler != nil {
		threshold := resolveIdleThreshold(&profile)
		sampleKey := sampleKeyFor(&profile)
		r.Sampler.Record(sampleKey, totalPowerW, threshold)
	}

	// 3. Compute hourly costs.
	amort, elec, total := calculator.ComputeHourlyCost(hw, totalPowerW)

	// 4. Update Prometheus metrics for infrastructure costs.
	node := profile.Spec.NodeSelector["kubernetes.io/hostname"]
	if node == "" {
		node = "unknown"
	}
	infercostmetrics.HourlyCostUSD.WithLabelValues(
		profile.Name, node, profile.Spec.Hardware.GPUModel,
	).Set(total)
	infercostmetrics.AmortizationPerHour.WithLabelValues(profile.Name, node).Set(amort)
	infercostmetrics.ElectricityPerHour.WithLabelValues(profile.Name, node).Set(elec)

	// 5. Find inference pods and scrape token metrics.
	if err := r.scrapeInferencePods(ctx, profile, total); err != nil {
		log.Error(err, "failed to scrape inference pods")
	}

	// 6. Update CostProfile status.
	now := metav1.Now()
	profile.Status.HourlyCostUSD = total
	profile.Status.AmortizationRatePerHour = amort
	profile.Status.ElectricityCostPerHour = elec
	profile.Status.CurrentPowerDrawWatts = totalPowerW
	profile.Status.LastUpdated = &now

	meta.SetStatusCondition(&profile.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "CostComputed",
		Message:            fmt.Sprintf("Hourly cost: $%.4f (amort: $%.4f, elec: $%.4f)", total, amort, elec),
		LastTransitionTime: now,
	})
	dcgmCondition.LastTransitionTime = now
	meta.SetStatusCondition(&profile.Status.Conditions, dcgmCondition)

	if err := r.Status().Update(ctx, &profile); err != nil {
		log.Error(err, "failed to update CostProfile status")
		return ctrl.Result{}, err
	}

	// 7. Update API store.
	if r.APIStore != nil {
		hoursRunning := time.Since(profile.CreationTimestamp.Time).Hours()
		r.APIStore.SetCostData(internalapi.CostData{
			ProfileName:       profile.Name,
			GPUModel:          profile.Spec.Hardware.GPUModel,
			GPUCount:          profile.Spec.Hardware.GPUCount,
			HourlyCostUSD:     total,
			AmortizationPerHr: amort,
			ElectricityPerHr:  elec,
			PowerDrawWatts:    totalPowerW,
			MonthlyProjected:  total * 24 * 30,
			YearlyProjected:   total * 8760,
			PurchasePriceUSD:  profile.Spec.Hardware.PurchasePriceUSD,
			AmortizationYears: profile.Spec.Hardware.AmortizationYears,
			RatePerKWh:        profile.Spec.Electricity.RatePerKWh,
			PUEFactor:         profile.Spec.Electricity.PUEFactor,
			UptimeHours:       hoursRunning,
			LastUpdated:       time.Now(),
		})
	}

	log.Info("cost computed",
		"hourlyCost", fmt.Sprintf("$%.4f", total),
		"powerDraw", fmt.Sprintf("%.1fW", totalPowerW),
		"amortization", fmt.Sprintf("$%.4f/hr", amort),
		"electricity", fmt.Sprintf("$%.4f/hr", elec),
	)

	return ctrl.Result{RequeueAfter: reconcileInterval}, nil
}

// scrapeInferencePods discovers inference pods and scrapes their token metrics.
func (r *CostProfileReconciler) scrapeInferencePods(ctx context.Context, profile finopsv1alpha1.CostProfile, hourlyCost float64) error {
	log := logf.FromContext(ctx)

	// Find pods with the LLMKube model label.
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.MatchingLabels{
		// Any pod with this label is an LLMKube inference pod.
	}); err != nil {
		return fmt.Errorf("listing pods: %w", err)
	}

	// Aggregate tokens across all pods for cloud comparison (computed once, not per-pod).
	var totalInputTokens, totalOutputTokens int64
	var modelDataList []internalapi.ModelData

	// Filter to pods that have inference labels and are running.
	for i := range podList.Items {
		pod := &podList.Items[i]
		modelName := pod.Labels[modelLabel]
		if modelName == "" {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning || pod.Status.PodIP == "" {
			continue
		}

		// Check if this pod is on a node matching the CostProfile's nodeSelector.
		if hostname, ok := profile.Spec.NodeSelector["kubernetes.io/hostname"]; ok {
			if pod.Spec.NodeName != hostname {
				continue
			}
		}

		// Dispatch to llama.cpp or vLLM based on pod annotations/labels.
		backend := scraper.ResolveBackend(pod.Annotations, pod.Labels)
		port := scraper.ResolveMetricsPort(backend, pod.Annotations, pod.Labels)
		endpoint := fmt.Sprintf("http://%s:%d/metrics", pod.Status.PodIP, port)
		im, err := scraper.Scrape(ctx, r.ScrapeClient, backend, endpoint)
		if err != nil {
			log.Error(err, "failed to scrape inference pod", "pod", pod.Name, "backend", backend)
			continue
		}
		im.Pod = pod.Name
		im.Namespace = pod.Namespace
		im.Model = modelName

		// Update per-model token counters.
		infercostmetrics.TokensTotal.WithLabelValues(modelName, pod.Namespace, "input").Set(im.PromptTokensTotal)
		infercostmetrics.TokensTotal.WithLabelValues(modelName, pod.Namespace, "output").Set(im.PredictedTokensTotal)

		// Accumulate for cloud comparison.
		totalInputTokens += int64(im.PromptTokensTotal)
		totalOutputTokens += int64(im.PredictedTokensTotal)

		// Collect model data for API store.
		modelDataList = append(modelDataList, internalapi.ModelData{
			Model:        modelName,
			Namespace:    pod.Namespace,
			Pod:          pod.Name,
			InputTokens:  im.PromptTokensTotal,
			OutputTokens: im.PredictedTokensTotal,
			TokensPerSec: im.PredictedTokensPerSec,
			IsActive:     im.RequestsProcessing > 0,
		})

		// Token rate computation: only update snapshot if enough time has elapsed.
		// Status updates trigger re-reconciles within milliseconds — debounce to avoid
		// dividing tiny token deltas by tiny time intervals.
		const minSnapshotInterval = 10 * time.Second

		snapshotKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		r.snapshotMu.Lock()
		if r.tokenSnapshots == nil {
			r.tokenSnapshots = make(map[string]calculator.TokenSnapshot)
		}
		prev, hasPrev := r.tokenSnapshots[snapshotKey]
		now := time.Now()

		if hasPrev && now.Sub(prev.Timestamp) < minSnapshotInterval {
			r.snapshotMu.Unlock()
			continue
		}

		curr := calculator.TokenSnapshot{
			Timestamp:    now,
			InputTokens:  im.PromptTokensTotal,
			OutputTokens: im.PredictedTokensTotal,
		}
		r.tokenSnapshots[snapshotKey] = curr
		r.snapshotMu.Unlock()

		if !hasPrev {
			log.Info("first token snapshot recorded", "pod", pod.Name, "model", modelName,
				"inputTokens", im.PromptTokensTotal, "outputTokens", im.PredictedTokensTotal)
			continue
		}

		_, _, totalTokensPerHour := calculator.ComputeTokenRate(prev, curr)
		costPerToken := calculator.ComputeCostPerToken(hourlyCost, totalTokensPerHour)

		infercostmetrics.TokensPerHour.WithLabelValues(modelName, pod.Namespace).Set(totalTokensPerHour)
		infercostmetrics.CostPerTokenUSD.WithLabelValues(modelName, pod.Namespace).Set(costPerToken)
		infercostmetrics.CostPerMillionTokensUSD.WithLabelValues(modelName, pod.Namespace).Set(costPerToken * 1_000_000)

		log.Info("inference pod metrics",
			"pod", pod.Name,
			"model", modelName,
			"tokensPerHour", fmt.Sprintf("%.0f", totalTokensPerHour),
			"costPerMTok", fmt.Sprintf("$%.4f", costPerToken*1_000_000),
		)
	}

	// Cloud comparison: aggregate across ALL pods, compute once.
	// On-prem cost = total infrastructure hours × hourly rate (GPUs cost money even when idle).
	if totalInputTokens+totalOutputTokens > 0 {
		hoursRunning := time.Since(profile.CreationTimestamp.Time).Hours()
		if hoursRunning < 0.001 {
			hoursRunning = 0.001
		}
		onPremCost := hourlyCost * hoursRunning

		comparisons := calculator.CompareToCloud(totalInputTokens, totalOutputTokens, onPremCost, calculator.DefaultCloudPricing())
		for _, c := range comparisons {
			infercostmetrics.CloudEquivalentCostUSD.WithLabelValues(c.Provider, c.Model).Set(c.CloudCostUSD)
			infercostmetrics.SavingsVsCloudUSD.WithLabelValues(c.Provider, c.Model).Set(c.SavingsUSD)
			infercostmetrics.SavingsVsCloudPercent.WithLabelValues(c.Provider, c.Model).Set(c.SavingsPercent)
		}

		// Update API store with comparison data.
		if r.APIStore != nil {
			r.APIStore.SetComparisons(internalapi.BuildComparisons(totalInputTokens, totalOutputTokens, onPremCost))
		}
	}

	// Update API store with model data.
	if r.APIStore != nil {
		r.APIStore.SetModels(modelDataList)
	}

	return nil
}

// fallbackPowerDraw computes total power from TDP when DCGM is unavailable.
func (r *CostProfileReconciler) fallbackPowerDraw(profile finopsv1alpha1.CostProfile) float64 {
	if profile.Spec.Hardware.TDPWatts != nil {
		return float64(*profile.Spec.Hardware.TDPWatts) * float64(profile.Spec.Hardware.GPUCount)
	}
	return 0
}

// readDCGMPower returns the current total GPU power draw for the profile's
// node selector along with a DCGMReachable condition that describes how the
// number was obtained. Silent fallbacks are the main failure mode users have
// reported ("why is my dashboard flat?"), so every path here produces an
// explicit condition rather than hiding state.
func (r *CostProfileReconciler) readDCGMPower(ctx context.Context, profile finopsv1alpha1.CostProfile) (float64, metav1.Condition) {
	log := logf.FromContext(ctx)

	if r.DCGMEndpoint == "" {
		power := r.fallbackPowerDraw(profile)
		return power, metav1.Condition{
			Type:    ConditionDCGMReachable,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonDCGMNotConfigured,
			Message: fmt.Sprintf("DCGM endpoint not set; using TDP fallback (%.0fW). Install the DCGM exporter for real-time power — see https://infercost.ai/docs/dcgm.", power),
		}
	}

	readings, err := scraper.ScrapeDCGM(ctx, r.ScrapeClient, r.DCGMEndpoint)
	if err != nil {
		log.Error(err, "failed to scrape DCGM, falling back to TDP", "endpoint", r.DCGMEndpoint)
		power := r.fallbackPowerDraw(profile)
		return power, metav1.Condition{
			Type:    ConditionDCGMReachable,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonDCGMScrapeError,
			Message: fmt.Sprintf("DCGM scrape failed at %s: %v. Using TDP fallback (%.0fW).", r.DCGMEndpoint, err, power),
		}
	}

	node := profile.Spec.NodeSelector["kubernetes.io/hostname"]
	var totalPowerW float64
	for _, reading := range readings {
		if node == "" || reading.Node == node {
			totalPowerW += reading.PowerW
			infercostmetrics.GPUPowerWatts.WithLabelValues(
				profile.Name, reading.Node, reading.GPUID,
			).Set(reading.PowerW)
		}
	}

	if totalPowerW == 0 {
		power := r.fallbackPowerDraw(profile)
		nodeDesc := node
		if nodeDesc == "" {
			nodeDesc = "(any node)"
		}
		return power, metav1.Condition{
			Type:    ConditionDCGMReachable,
			Status:  metav1.ConditionUnknown,
			Reason:  ReasonDCGMNoReadings,
			Message: fmt.Sprintf("DCGM reachable at %s but returned no readings for node %q. Check the nodeSelector matches a GPU host. Using TDP fallback (%.0fW).", r.DCGMEndpoint, nodeDesc, power),
		}
	}

	return totalPowerW, metav1.Condition{
		Type:    ConditionDCGMReachable,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonDCGMHealthy,
		Message: fmt.Sprintf("DCGM healthy at %s (%.0fW current draw).", r.DCGMEndpoint, totalPowerW),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *CostProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1alpha1.CostProfile{}).
		Named("costprofile").
		Complete(r)
}

// sampleKeyFor returns the string key used by the Sampler for this profile.
// Namespaced so two CostProfiles with the same name in different namespaces
// don't pool samples.
func sampleKeyFor(profile *finopsv1alpha1.CostProfile) string {
	return profile.Namespace + "/" + profile.Name
}

// resolveIdleThreshold picks the active-threshold wattage for a profile: the
// operator's explicit setting if present, otherwise the sampler's default
// derived from TDP and GPU count.
func resolveIdleThreshold(profile *finopsv1alpha1.CostProfile) float64 {
	if profile.Spec.Electricity.IdleWattsThreshold != nil {
		return *profile.Spec.Electricity.IdleWattsThreshold
	}
	return utilization.DefaultIdleThresholdWatts(
		profile.Spec.Hardware.TDPWatts,
		profile.Spec.Hardware.GPUCount,
	)
}
