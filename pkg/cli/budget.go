package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var tokenBudgetGVR = schema.GroupVersionResource{
	Group:    "finops.infercost.ai",
	Version:  "v1alpha1",
	Resource: "tokenbudgets",
}

type budgetOptions struct {
	namespace     string
	allNamespaces bool
}

// NewBudgetCommand creates the "budget" CLI subcommand that lists TokenBudget
// resources and their monthly spend utilization.
func NewBudgetCommand() *cobra.Command {
	opts := &budgetOptions{}

	cmd := &cobra.Command{
		Use:   "budget",
		Short: "List TokenBudget resources and spend status",
		Long: `Display all TokenBudget resources showing monthly spend limits,
current utilization, and alert status.

TokenBudgets track inference spend per namespace and generate
Prometheus alerts when configured thresholds are exceeded.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBudget(opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "", "Kubernetes namespace (default: all)")
	cmd.Flags().BoolVarP(&opts.allNamespaces, "all-namespaces", "A", false, "Show budgets across all namespaces")

	return cmd
}

func runBudget(opts *budgetOptions) error {
	ctx := context.Background()

	clients, err := newK8sClient()
	if err != nil {
		return err
	}

	var res = clients.dynamic.Resource(tokenBudgetGVR)
	var list *unstructured.UnstructuredList
	if opts.namespace != "" {
		list, err = res.Namespace(opts.namespace).List(ctx, metav1.ListOptions{})
	} else {
		list, err = res.List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return fmt.Errorf("failed to list TokenBudgets: %w", err)
	}

	if len(list.Items) == 0 {
		fmt.Println("No TokenBudgets found. Create one to start tracking spend.")
		fmt.Println("  kubectl apply -f config/samples/finops_v1alpha1_tokenbudget.yaml")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "NAMESPACE\tBUDGET\tLIMIT ($)\tSPEND ($)\tUTILIZATION\tSTATUS\n")

	for i := range list.Items {
		obj := &list.Items[i]

		ns := budgetScopeNamespace(obj)
		name := obj.GetName()
		limit := budgetFieldFloat64(obj, "spec", "monthlyLimitUSD")
		spend := budgetFieldFloat64(obj, "status", "currentSpendUSD")
		utilization := budgetFieldFloat64(obj, "status", "utilizationPercent")
		status := budgetStatus(obj)

		_, _ = fmt.Fprintf(w, "%s\t%s\t$%.2f\t$%.4f\t%.1f%%\t%s\n",
			ns, name, limit, spend, utilization, status)
	}
	_ = w.Flush()

	return nil
}

// budgetScopeNamespace extracts spec.scope.namespace from an unstructured TokenBudget.
func budgetScopeNamespace(obj *unstructured.Unstructured) string {
	val, _, _ := unstructured.NestedString(obj.Object, "spec", "scope", "namespace")
	if val == "" {
		return obj.GetNamespace()
	}
	return val
}

// budgetFieldFloat64 extracts a nested float64 field from an unstructured object.
func budgetFieldFloat64(obj *unstructured.Unstructured, fields ...string) float64 {
	val, found, err := unstructured.NestedFloat64(obj.Object, fields...)
	if err != nil || !found {
		return 0
	}
	return val
}

// budgetStatus derives a human-readable status from the TokenBudget conditions.
// Returns "OK", "Warning", or "Exceeded" based on the condition types present.
func budgetStatus(obj *unstructured.Unstructured) string {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found || len(conditions) == 0 {
		return "OK"
	}

	// Check conditions in priority order: Exceeded > Warning > OK.
	hasWarning := false
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		condStatus, _ := cond["status"].(string)

		if condStatus != "True" {
			continue
		}

		if condType == "BudgetExceeded" {
			return "Exceeded"
		}
		if condType == "BudgetWarning" {
			hasWarning = true
		}
	}

	if hasWarning {
		return "Warning"
	}
	return "OK"
}
