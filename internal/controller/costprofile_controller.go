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
	"github.com/defilantech/infercost/internal/calculator"
	infercostmetrics "github.com/defilantech/infercost/internal/metrics"
	"github.com/defilantech/infercost/internal/scraper"
)

const (
	// How often to re-scrape metrics and recompute costs.
	reconcileInterval = 30 * time.Second

	// Label on inference pods that identifies the model name (LLMKube convention).
	modelLabel = "inference.llmkube.dev/model"
)

// CostProfileReconciler reconciles a CostProfile object.
type CostProfileReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ScrapeClient *scraper.Client
	DCGMEndpoint string // DCGM exporter service URL

	// tokenSnapshots tracks previous token readings for rate computation.
	tokenSnapshots map[string]calculator.TokenSnapshot
	snapshotMu     sync.Mutex
}

// +kubebuilder:rbac:groups=finops.infercost.ai,resources=costprofiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=costprofiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=costprofiles/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

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

	// 2. Scrape DCGM for real-time GPU power draw.
	var totalPowerW float64
	if r.DCGMEndpoint != "" {
		readings, err := scraper.ScrapeDCGM(ctx, r.ScrapeClient, r.DCGMEndpoint)
		if err != nil {
			log.Error(err, "failed to scrape DCGM, falling back to TDP")
			totalPowerW = r.fallbackPowerDraw(profile)
		} else {
			// Filter readings to GPUs matching this profile's node selector.
			node := profile.Spec.NodeSelector["kubernetes.io/hostname"]
			for _, reading := range readings {
				if node == "" || reading.Node == node {
					totalPowerW += reading.PowerW

					infercostmetrics.GPUPowerWatts.WithLabelValues(
						profile.Name, reading.Node, reading.GPUID,
					).Set(reading.PowerW)
				}
			}
			if totalPowerW == 0 {
				totalPowerW = r.fallbackPowerDraw(profile)
			}
		}
	} else {
		totalPowerW = r.fallbackPowerDraw(profile)
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

	if err := r.Status().Update(ctx, &profile); err != nil {
		log.Error(err, "failed to update CostProfile status")
		return ctrl.Result{}, err
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

		// Scrape llama.cpp metrics from the pod.
		endpoint := fmt.Sprintf("http://%s:8080/metrics", pod.Status.PodIP)
		im, err := scraper.ScrapeLlamaCPP(ctx, r.ScrapeClient, endpoint)
		if err != nil {
			log.Error(err, "failed to scrape inference pod", "pod", pod.Name)
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

// SetupWithManager sets up the controller with the Manager.
func (r *CostProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1alpha1.CostProfile{}).
		Named("costprofile").
		Complete(r)
}
