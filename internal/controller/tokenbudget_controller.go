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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
	internalapi "github.com/defilantech/infercost/internal/api"
	infercostmetrics "github.com/defilantech/infercost/internal/metrics"
)

const (
	// How often to re-evaluate budgets.
	budgetReconcileInterval = 1 * time.Minute

	severityCritical = "critical"
	severityWarning  = "warning"
)

// TokenBudgetReconciler reconciles a TokenBudget object.
type TokenBudgetReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	APIStore *internalapi.Store
}

// +kubebuilder:rbac:groups=finops.infercost.ai,resources=tokenbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=tokenbudgets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=finops.infercost.ai,resources=tokenbudgets/finalizers,verbs=update
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch;create;update;patch;delete

// Reconcile evaluates a TokenBudget against current namespace spend and updates
// status, metrics, and PrometheusRule alerts accordingly.
func (r *TokenBudgetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch the TokenBudget CR.
	var budget finopsv1alpha1.TokenBudget
	if err := r.Get(ctx, req.NamespacedName, &budget); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("reconciling TokenBudget", "name", budget.Name, "namespace", budget.Namespace)

	// 2. Compute the current billing period (1st of month to end of month).
	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0).Add(-time.Nanosecond)

	// 3. Read namespace spend from the API Store.
	var currentSpend float64
	if r.APIStore != nil {
		nsCosts := r.APIStore.GetNamespaceCosts()
		for _, ns := range nsCosts {
			if ns.Namespace == budget.Spec.Scope.Namespace {
				currentSpend = ns.EstimatedCostUSD
				break
			}
		}
	}

	// 4. Compute utilization.
	var utilization float64
	if budget.Spec.MonthlyLimitUSD > 0 {
		utilization = (currentSpend / budget.Spec.MonthlyLimitUSD) * 100
	}

	// 5. Update status fields.
	metaPeriodStart := metav1.NewTime(periodStart)
	metaPeriodEnd := metav1.NewTime(periodEnd)
	metaNow := metav1.Now()

	budget.Status.CurrentSpendUSD = currentSpend
	budget.Status.UtilizationPercent = utilization
	budget.Status.PeriodStart = &metaPeriodStart
	budget.Status.PeriodEnd = &metaPeriodEnd
	budget.Status.LastUpdated = &metaNow

	// 6. Set conditions based on threshold evaluation.
	budgetStatus := r.evaluateConditions(&budget, utilization, metaNow)

	// 7. Update Prometheus budget metrics.
	ns := budget.Spec.Scope.Namespace
	infercostmetrics.BudgetLimitUSD.WithLabelValues(ns, budget.Name).Set(budget.Spec.MonthlyLimitUSD)
	infercostmetrics.BudgetCurrentSpendUSD.WithLabelValues(ns, budget.Name).Set(currentSpend)
	infercostmetrics.BudgetUtilizationPercent.WithLabelValues(ns, budget.Name).Set(utilization)

	// 8. Generate/update PrometheusRule CR.
	if err := r.reconcilePrometheusRule(ctx, &budget); err != nil {
		log.Error(err, "failed to reconcile PrometheusRule")
		// Don't fail the reconcile — the PrometheusRule CRD may not exist.
	}

	// 9. Update CRD status.
	if err := r.Status().Update(ctx, &budget); err != nil {
		log.Error(err, "failed to update TokenBudget status")
		return ctrl.Result{}, err
	}

	// 10. Update API Store with budget data.
	if r.APIStore != nil {
		r.updateAPIStore(&budget, budgetStatus)
	}

	log.Info("budget evaluated",
		"namespace", ns,
		"spend", fmt.Sprintf("$%.2f", currentSpend),
		"limit", fmt.Sprintf("$%.2f", budget.Spec.MonthlyLimitUSD),
		"utilization", fmt.Sprintf("%.1f%%", utilization),
		"status", budgetStatus,
	)

	return ctrl.Result{RequeueAfter: budgetReconcileInterval}, nil
}

// evaluateConditions sets status conditions based on utilization vs thresholds
// and returns the overall budget status string.
func (r *TokenBudgetReconciler) evaluateConditions(budget *finopsv1alpha1.TokenBudget, utilization float64, now metav1.Time) string {
	// Determine the highest triggered severity.
	var highestSeverity string
	for _, threshold := range budget.Spec.AlertThresholds {
		if utilization >= float64(threshold.Percent) {
			if threshold.Severity == severityCritical {
				highestSeverity = severityCritical
			} else if highestSeverity != severityCritical {
				highestSeverity = severityWarning
			}
		}
	}

	// Clear previous conditions by setting all to False, then set the active one to True.
	switch highestSeverity {
	case severityCritical:
		meta.SetStatusCondition(&budget.Status.Conditions, metav1.Condition{
			Type:               "BudgetOK",
			Status:             metav1.ConditionFalse,
			Reason:             "BudgetExceeded",
			Message:            fmt.Sprintf("Budget utilization at %.1f%% exceeds critical threshold", utilization),
			LastTransitionTime: now,
		})
		meta.SetStatusCondition(&budget.Status.Conditions, metav1.Condition{
			Type:               "BudgetWarning",
			Status:             metav1.ConditionTrue,
			Reason:             "ThresholdCrossed",
			Message:            fmt.Sprintf("Budget utilization at %.1f%%", utilization),
			LastTransitionTime: now,
		})
		meta.SetStatusCondition(&budget.Status.Conditions, metav1.Condition{
			Type:               "BudgetExceeded",
			Status:             metav1.ConditionTrue,
			Reason:             "CriticalThresholdCrossed",
			Message:            fmt.Sprintf("Budget utilization at %.1f%% exceeds critical threshold", utilization),
			LastTransitionTime: now,
		})
		return "exceeded"

	case severityWarning:
		meta.SetStatusCondition(&budget.Status.Conditions, metav1.Condition{
			Type:               "BudgetOK",
			Status:             metav1.ConditionFalse,
			Reason:             "BudgetWarning",
			Message:            fmt.Sprintf("Budget utilization at %.1f%% exceeds warning threshold", utilization),
			LastTransitionTime: now,
		})
		meta.SetStatusCondition(&budget.Status.Conditions, metav1.Condition{
			Type:               "BudgetWarning",
			Status:             metav1.ConditionTrue,
			Reason:             "ThresholdCrossed",
			Message:            fmt.Sprintf("Budget utilization at %.1f%%", utilization),
			LastTransitionTime: now,
		})
		meta.SetStatusCondition(&budget.Status.Conditions, metav1.Condition{
			Type:               "BudgetExceeded",
			Status:             metav1.ConditionFalse,
			Reason:             "WithinBudget",
			Message:            "Budget utilization is below critical threshold",
			LastTransitionTime: now,
		})
		return "warning"

	default:
		meta.SetStatusCondition(&budget.Status.Conditions, metav1.Condition{
			Type:               "BudgetOK",
			Status:             metav1.ConditionTrue,
			Reason:             "WithinBudget",
			Message:            fmt.Sprintf("Budget utilization at %.1f%% is within limits", utilization),
			LastTransitionTime: now,
		})
		meta.SetStatusCondition(&budget.Status.Conditions, metav1.Condition{
			Type:               "BudgetWarning",
			Status:             metav1.ConditionFalse,
			Reason:             "WithinBudget",
			Message:            "Budget utilization is below warning threshold",
			LastTransitionTime: now,
		})
		meta.SetStatusCondition(&budget.Status.Conditions, metav1.Condition{
			Type:               "BudgetExceeded",
			Status:             metav1.ConditionFalse,
			Reason:             "WithinBudget",
			Message:            "Budget utilization is below critical threshold",
			LastTransitionTime: now,
		})
		return "ok"
	}
}

// reconcilePrometheusRule creates or updates a PrometheusRule CR for budget alerts.
func (r *TokenBudgetReconciler) reconcilePrometheusRule(ctx context.Context, budget *finopsv1alpha1.TokenBudget) error {
	log := logf.FromContext(ctx)

	if len(budget.Spec.AlertThresholds) == 0 {
		return nil
	}

	ruleName := fmt.Sprintf("infercost-budget-%s", budget.Name)
	ns := budget.Spec.Scope.Namespace

	// Build alert rules from thresholds.
	var rules []any
	for _, threshold := range budget.Spec.AlertThresholds {
		var forDuration string
		if threshold.Severity == severityCritical {
			forDuration = "1m"
		} else {
			forDuration = "5m"
		}

		// Capitalize first letter of severity for alert name.
		severityTitle := string(threshold.Severity[0]-32) + threshold.Severity[1:]

		rule := map[string]any{
			"alert": fmt.Sprintf("InferCostBudget%s", severityTitle),
			"expr": fmt.Sprintf(
				`infercost_budget_utilization_percent{namespace="%s",budget_name="%s"} >= %d`,
				ns, budget.Name, threshold.Percent,
			),
			"for": forDuration,
			"labels": map[string]any{
				"severity": threshold.Severity,
			},
			"annotations": map[string]any{
				"summary": fmt.Sprintf(
					"Inference budget at {{ $value }}%% for namespace %s",
					ns,
				),
			},
		}
		rules = append(rules, rule)
	}

	// Build the PrometheusRule as unstructured to avoid importing prometheus-operator types.
	promRule := &unstructured.Unstructured{}
	promRule.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "PrometheusRule",
	})
	promRule.SetName(ruleName)
	promRule.SetNamespace(budget.Namespace)
	promRule.SetLabels(map[string]string{
		"app.kubernetes.io/name":       "infercost",
		"app.kubernetes.io/managed-by": "infercost",
		"infercost.ai/budget":          budget.Name,
	})

	// Set owner reference so the PrometheusRule is garbage collected with the budget.
	if err := controllerutil.SetControllerReference(budget, promRule, r.Scheme); err != nil {
		return fmt.Errorf("setting owner reference: %w", err)
	}

	// Set the spec.
	if err := unstructured.SetNestedSlice(promRule.Object, []any{
		map[string]any{
			"name":  fmt.Sprintf("infercost-budget-%s.rules", budget.Name),
			"rules": rules,
		},
	}, "spec", "groups"); err != nil {
		return fmt.Errorf("setting spec.groups: %w", err)
	}

	// Create or update the PrometheusRule.
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "PrometheusRule",
	})

	err := r.Get(ctx, client.ObjectKey{Name: ruleName, Namespace: budget.Namespace}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the PrometheusRule.
			if createErr := r.Create(ctx, promRule); createErr != nil {
				// Check if the CRD doesn't exist (PrometheusRule not installed).
				if meta.IsNoMatchError(createErr) {
					log.Info("PrometheusRule CRD not found in cluster, skipping alert rule creation")
					return nil
				}
				return fmt.Errorf("creating PrometheusRule: %w", createErr)
			}
			log.Info("created PrometheusRule", "name", ruleName)
			return nil
		}
		// Check if the CRD doesn't exist.
		if meta.IsNoMatchError(err) {
			log.Info("PrometheusRule CRD not found in cluster, skipping alert rule creation")
			return nil
		}
		return fmt.Errorf("getting PrometheusRule: %w", err)
	}

	// Update existing PrometheusRule.
	existing.Object["spec"] = promRule.Object["spec"]
	existing.SetLabels(promRule.GetLabels())
	if updateErr := r.Update(ctx, existing); updateErr != nil {
		return fmt.Errorf("updating PrometheusRule: %w", updateErr)
	}
	log.Info("updated PrometheusRule", "name", ruleName)

	return nil
}

// updateAPIStore updates the API store with the current budget state.
func (r *TokenBudgetReconciler) updateAPIStore(budget *finopsv1alpha1.TokenBudget, status string) {
	// Read existing budgets, update or append this one.
	existing := r.APIStore.GetBudgets()
	found := false
	for i, b := range existing {
		if b.Name == budget.Name && b.Namespace == budget.Namespace {
			existing[i] = internalapi.BudgetData{
				Name:               budget.Name,
				Namespace:          budget.Spec.Scope.Namespace,
				MonthlyLimitUSD:    budget.Spec.MonthlyLimitUSD,
				CurrentSpendUSD:    budget.Status.CurrentSpendUSD,
				UtilizationPercent: budget.Status.UtilizationPercent,
				Status:             status,
			}
			found = true
			break
		}
	}
	if !found {
		existing = append(existing, internalapi.BudgetData{
			Name:               budget.Name,
			Namespace:          budget.Spec.Scope.Namespace,
			MonthlyLimitUSD:    budget.Spec.MonthlyLimitUSD,
			CurrentSpendUSD:    budget.Status.CurrentSpendUSD,
			UtilizationPercent: budget.Status.UtilizationPercent,
			Status:             status,
		})
	}
	r.APIStore.SetBudgets(existing)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TokenBudgetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1alpha1.TokenBudget{}).
		Named("tokenbudget").
		Complete(r)
}
