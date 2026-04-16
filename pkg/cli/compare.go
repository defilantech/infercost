package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
	"github.com/defilantech/infercost/internal/calculator"
	"github.com/defilantech/infercost/internal/scraper"
)

type compareOptions struct {
	namespace string
	monthly   bool
}

func NewCompareCommand() *cobra.Command {
	opts := &compareOptions{}

	cmd := &cobra.Command{
		Use:   "compare",
		Short: "Compare on-prem costs to cloud API pricing",
		Long: `Show a detailed comparison of your on-premises inference costs against
major cloud API providers (OpenAI, Anthropic, Google).

Costs are computed from actual hardware economics and real GPU power draw,
compared against verified list prices for cloud APIs.

Cloud pricing last verified: 2026-03-21
Sources: openai.com/pricing, platform.claude.com/pricing, ai.google.dev/pricing`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompare(opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "", "Kubernetes namespace (default: all)")
	cmd.Flags().BoolVar(&opts.monthly, "monthly", false, "Project costs to monthly estimates")

	return cmd
}

func runCompare(opts *compareOptions) error {
	ctx := context.Background()

	clients, err := newK8sClient()
	if err != nil {
		return err
	}
	k8sClient := clients.client

	// Build a scraper that authenticates through the K8s API server.
	transport, err := rest.TransportFor(clients.restConfig)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}
	scrapeClient := scraper.NewClientFromTransport(5*time.Second, transport)

	// Fetch CostProfiles.
	var profiles finopsv1alpha1.CostProfileList
	if err := k8sClient.List(ctx, &profiles); err != nil {
		return fmt.Errorf("failed to list CostProfiles: %w", err)
	}

	if len(profiles.Items) == 0 {
		fmt.Println("No CostProfiles found. Create one to start tracking costs.")
		return nil
	}

	// Compute on-prem cost.
	profile := profiles.Items[0]
	hoursRunning := time.Since(profile.CreationTimestamp.Time).Hours()
	if hoursRunning < 0.001 {
		hoursRunning = 0.001
	}
	onPremTotal := profile.Status.HourlyCostUSD * hoursRunning

	// Gather tokens from inference models (pods + InferenceService CRDs).
	listOpts := []client.ListOption{}
	if opts.namespace != "" {
		listOpts = append(listOpts, client.InNamespace(opts.namespace))
	}

	var podList corev1.PodList
	if err := k8sClient.List(ctx, &podList, listOpts...); err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	podModels, knownModels := discoverModelsFromPods(clients.restConfig, podList.Items)

	isvcModels, err := discoverModelsFromInferenceServices(ctx, clients.dynamic, k8sClient, opts.namespace, knownModels)
	if err != nil {
		return fmt.Errorf("failed to discover InferenceServices: %w", err)
	}

	allModels := append(podModels, isvcModels...)
	directClient := scraper.NewClient(5 * time.Second)

	var totalInput, totalOutput float64
	for i := range allModels {
		m := &allModels[i]
		if !m.IsScrapable {
			continue
		}
		sc := scrapeClient
		if m.Source == "inferenceservice" {
			sc = directClient
		}
		im, err := scraper.ScrapeLlamaCPP(ctx, sc, m.MetricsURL)
		if err != nil {
			continue
		}
		totalInput += im.PromptTokensTotal
		totalOutput += im.PredictedTokensTotal
	}

	if totalInput+totalOutput == 0 {
		fmt.Println("No token data available. Run some inference requests first.")
		return nil
	}

	// Header.
	fmt.Println("CLOUD vs ON-PREM COST COMPARISON")
	fmt.Println("================================")
	fmt.Printf("GPU Hardware:    %s (%d GPUs)\n", profile.Spec.Hardware.GPUModel, profile.Spec.Hardware.GPUCount)
	fmt.Printf("Purchase Price:  $%.0f (amortized over %d years)\n",
		profile.Spec.Hardware.PurchasePriceUSD, profile.Spec.Hardware.AmortizationYears)
	fmt.Printf("Electricity:     $%.2f/kWh, PUE %.1f\n",
		profile.Spec.Electricity.RatePerKWh, profile.Spec.Electricity.PUEFactor)
	fmt.Printf("Current Power:   %.1fW\n", profile.Status.CurrentPowerDrawWatts)
	fmt.Printf("Uptime:          %.1f hours\n", hoursRunning)
	fmt.Println()
	fmt.Printf("Tokens Processed: %s input + %s output = %s total\n",
		formatTokenCount(totalInput), formatTokenCount(totalOutput),
		formatTokenCount(totalInput+totalOutput))
	fmt.Printf("On-Prem Cost:     $%.4f (infrastructure total for %.1f hours)\n", onPremTotal, hoursRunning)
	fmt.Println()

	// Comparison table.
	pricing := calculator.DefaultCloudPricing()
	comparisons := calculator.CompareToCloud(int64(totalInput), int64(totalOutput), onPremTotal, pricing)

	fmt.Println("PROVIDER        MODEL                  INPUT/MTok  OUTPUT/MTok  CLOUD COST  SAVINGS")
	fmt.Println("--------        -----                  ----------  -----------  ----------  -------")

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, c := range comparisons {
		// Find the pricing entry for this comparison.
		var inputRate, outputRate float64
		for _, p := range pricing {
			if p.Provider == c.Provider && p.Model == c.Model {
				inputRate = p.InputPerMillion
				outputRate = p.OutputPerMillion
				break
			}
		}

		savingsStr := fmt.Sprintf("$%.2f (%.0f%%)", c.SavingsUSD, c.SavingsPercent)
		if c.SavingsPercent < 0 {
			savingsStr = fmt.Sprintf("-$%.2f (%.0f%% more)", -c.SavingsUSD, -c.SavingsPercent)
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t$%.2f\t$%.2f\t$%.2f\t%s\n",
			c.Provider, c.Model, inputRate, outputRate, c.CloudCostUSD, savingsStr)
	}
	_ = w.Flush()

	// Monthly projection if requested.
	if opts.monthly && hoursRunning > 0 {
		tokensPerHour := (totalInput + totalOutput) / hoursRunning
		monthlyTokens := tokensPerHour * 24 * 30
		monthlyOnPrem := profile.Status.HourlyCostUSD * 24 * 30

		fmt.Println()
		fmt.Println("MONTHLY PROJECTION (based on current usage rate)")
		fmt.Println("================================================")
		fmt.Printf("Projected tokens/month: %s\n", formatTokenCount(monthlyTokens))
		fmt.Printf("Projected on-prem cost: $%.2f/month\n", monthlyOnPrem)
		fmt.Println()

		// Assume same input/output ratio.
		ratio := totalInput / (totalInput + totalOutput)
		monthlyInput := monthlyTokens * ratio
		monthlyOutput := monthlyTokens * (1 - ratio)

		monthlyComparisons := calculator.CompareToCloud(
			int64(monthlyInput), int64(monthlyOutput), monthlyOnPrem, pricing)

		w = tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintf(w, "PROVIDER\tMODEL\tCLOUD/MONTH\tON-PREM/MONTH\tSAVINGS/MONTH\n")
		for _, c := range monthlyComparisons {
			savingsStr := fmt.Sprintf("$%.0f (%.0f%%)", c.SavingsUSD, c.SavingsPercent)
			if c.SavingsPercent < 0 {
				savingsStr = fmt.Sprintf("-$%.0f (%.0f%% more)", -c.SavingsUSD, -c.SavingsPercent)
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t$%.0f\t$%.0f\t%s\n",
				c.Provider, c.Model, c.CloudCostUSD, c.OnPremCostUSD, savingsStr)
		}
		_ = w.Flush()
	}

	fmt.Println()
	fmt.Println("Cloud pricing: verified list prices as of 2026-03-21.")
	fmt.Println("Sources: openai.com/pricing, platform.claude.com/pricing, ai.google.dev/pricing")
	fmt.Println("Note: Does not reflect negotiated enterprise rates or batch discounts.")

	return nil
}
