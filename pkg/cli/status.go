package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
	"github.com/defilantech/infercost/internal/calculator"
	"github.com/defilantech/infercost/internal/scraper"
)

type statusOptions struct {
	namespace     string
	allNamespaces bool
}

func NewStatusCommand() *cobra.Command {
	opts := &statusOptions{}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current inference cost status",
		Long: `Display real-time cost data for all CostProfiles and inference workloads.

Shows hardware costs, GPU power draw, active models, token throughput,
and cost-per-token computed from live metrics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "", "Kubernetes namespace (default: all)")
	cmd.Flags().BoolVarP(&opts.allNamespaces, "all-namespaces", "A", false, "Show costs across all namespaces")

	return cmd
}

func runStatus(opts *statusOptions) error {
	ctx := context.Background()

	k8sClient, err := newK8sClient()
	if err != nil {
		return err
	}

	// Fetch all CostProfiles.
	var profiles finopsv1alpha1.CostProfileList
	if err := k8sClient.List(ctx, &profiles); err != nil {
		return fmt.Errorf("failed to list CostProfiles: %w", err)
	}

	if len(profiles.Items) == 0 {
		fmt.Println("No CostProfiles found. Create one to start tracking costs.")
		fmt.Println("  kubectl apply -f config/samples/finops_v1alpha1_costprofile.yaml")
		return nil
	}

	// Print infrastructure costs.
	fmt.Println("INFRASTRUCTURE COSTS")
	fmt.Println("====================")
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "PROFILE\tGPU MODEL\tGPUs\t$/HOUR\tAMORT\tELEC\tPOWER\tAGE\n")
	for _, p := range profiles.Items {
		age := formatAge(time.Since(p.CreationTimestamp.Time))
		fmt.Fprintf(w, "%s\t%s\t%d\t$%.4f\t$%.4f\t$%.4f\t%.1fW\t%s\n",
			p.Name,
			p.Spec.Hardware.GPUModel,
			p.Spec.Hardware.GPUCount,
			p.Status.HourlyCostUSD,
			p.Status.AmortizationRatePerHour,
			p.Status.ElectricityCostPerHour,
			p.Status.CurrentPowerDrawWatts,
			age,
		)
	}
	w.Flush()

	// Calculate monthly/yearly projections.
	for _, p := range profiles.Items {
		if p.Status.HourlyCostUSD > 0 {
			monthly := p.Status.HourlyCostUSD * 24 * 30
			yearly := p.Status.HourlyCostUSD * 8760
			fmt.Printf("\n  %s projected: $%.2f/month, $%.2f/year\n", p.Name, monthly, yearly)
		}
	}

	// Discover inference pods and show model status.
	fmt.Println("\nINFERENCE MODELS")
	fmt.Println("================")

	listOpts := []client.ListOption{}
	if opts.namespace != "" {
		listOpts = append(listOpts, client.InNamespace(opts.namespace))
	}

	var podList corev1.PodList
	if err := k8sClient.List(ctx, &podList, listOpts...); err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	scrapeClient := scraper.NewClient(5 * time.Second)

	w = tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "MODEL\tNAMESPACE\tPOD\tINPUT TOKENS\tOUTPUT TOKENS\tTOKENS/SEC\tSTATUS\n")

	modelCount := 0
	for i := range podList.Items {
		pod := &podList.Items[i]
		modelName := pod.Labels["inference.llmkube.dev/model"]
		if modelName == "" {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning || pod.Status.PodIP == "" {
			fmt.Fprintf(w, "%s\t%s\t%s\t-\t-\t-\t%s\n",
				modelName, pod.Namespace, pod.Name, string(pod.Status.Phase))
			modelCount++
			continue
		}

		// Scrape live metrics from the pod.
		endpoint := fmt.Sprintf("http://%s:8080/metrics", pod.Status.PodIP)
		im, err := scraper.ScrapeLlamaCPP(ctx, scrapeClient, endpoint)
		if err != nil {
			fmt.Fprintf(w, "%s\t%s\t%s\t-\t-\t-\tScrape Error\n",
				modelName, pod.Namespace, pod.Name)
			modelCount++
			continue
		}

		status := "Idle"
		if im.RequestsProcessing > 0 {
			status = fmt.Sprintf("Active (%d req)", int(im.RequestsProcessing))
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%.1f\t%s\n",
			modelName,
			pod.Namespace,
			pod.Name,
			formatTokenCount(im.PromptTokensTotal),
			formatTokenCount(im.PredictedTokensTotal),
			im.PredictedTokensPerSec,
			status,
		)
		modelCount++
	}
	w.Flush()

	if modelCount == 0 {
		fmt.Println("  No inference pods found with LLMKube labels.")
	}

	// Quick cloud comparison summary using cumulative totals.
	if len(profiles.Items) > 0 && modelCount > 0 {
		fmt.Println("\nQUICK COMPARISON")
		fmt.Println("================")
		profile := profiles.Items[0]
		hoursRunning := time.Since(profile.CreationTimestamp.Time).Hours()
		onPremTotal := profile.Status.HourlyCostUSD * hoursRunning

		// Sum all tokens across pods.
		var totalInput, totalOutput float64
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.Labels["inference.llmkube.dev/model"] == "" || pod.Status.PodIP == "" {
				continue
			}
			endpoint := fmt.Sprintf("http://%s:8080/metrics", pod.Status.PodIP)
			im, err := scraper.ScrapeLlamaCPP(ctx, scrapeClient, endpoint)
			if err != nil {
				continue
			}
			totalInput += im.PromptTokensTotal
			totalOutput += im.PredictedTokensTotal
		}

		if totalInput+totalOutput > 0 {
			fmt.Printf("  Total tokens processed: %s input + %s output\n",
				formatTokenCount(totalInput), formatTokenCount(totalOutput))
			fmt.Printf("  On-prem cost (%.1f hours): $%.4f\n", hoursRunning, onPremTotal)
			fmt.Println()

			pricing := calculator.DefaultCloudPricing()
			comparisons := calculator.CompareToCloud(int64(totalInput), int64(totalOutput), onPremTotal, pricing)

			w = tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "  PROVIDER\tMODEL\tCLOUD COST\tSAVINGS\t\n")
			for _, c := range comparisons {
				savingsStr := fmt.Sprintf("$%.2f (%.0f%%)", c.SavingsUSD, c.SavingsPercent)
				if c.SavingsPercent < 0 {
					savingsStr = fmt.Sprintf("-$%.2f (cloud %.0f%% cheaper)", -c.SavingsUSD, -c.SavingsPercent)
				}
				fmt.Fprintf(w, "  %s\t%s\t$%.2f\t%s\t\n",
					c.Provider, c.Model, c.CloudCostUSD, savingsStr)
			}
			w.Flush()
		}
	}

	return nil
}

func formatTokenCount(tokens float64) string {
	switch {
	case tokens >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", tokens/1_000_000_000)
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", tokens/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fK", tokens/1_000)
	default:
		return fmt.Sprintf("%.0f", tokens)
	}
}

func formatAge(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
