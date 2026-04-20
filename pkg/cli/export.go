package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
	"github.com/defilantech/infercost/internal/focus"
)

type exportFocusOptions struct {
	namespace string
	output    string
	period    string
	region    string
}

// NewExportCommand returns the top-level `infercost export` command. Today it
// has one subcommand (focus) but export is its own namespace so future
// formats (e.g. per-provider-native schemas) don't crowd the root command
// surface.
func NewExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export InferCost data in interchange formats",
		Long:  `Export InferCost cost data in formats that drop into external FinOps tooling.`,
	}
	cmd.AddCommand(newExportFocusCommand())
	return cmd
}

func newExportFocusCommand() *cobra.Command {
	opts := &exportFocusOptions{}

	cmd := &cobra.Command{
		Use:   "focus",
		Short: "Export usage reports as FOCUS-compatible CSV",
		Long: `Export UsageReport data as a FOCUS (FinOps Open Cost and Usage Specification)
compatible CSV. Standard FOCUS v1 columns are populated for billing/usage
integration; on-prem inference specifics (GPU model, token counts, power,
cloud-equivalent cost) live in x-Infer* extension columns.

Importable into Kubecost, Cloudability, or any FOCUS-aware BI pipeline.
See docs/focus-export.md for the full schema.`,
		Example: `  # Write a CSV for all UsageReports in the cluster
  infercost export focus --out report.csv

  # Scope to one namespace and tag with region for multi-cluster rollups
  infercost export focus --namespace engineering --region us-east-1 --out eng.csv

  # Stream to stdout for piping into analysis
  infercost export focus | awk -F, '$1>0'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExportFocus(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "",
		"Kubernetes namespace (default: all)")
	cmd.Flags().StringVarP(&opts.output, "out", "o", "",
		"Output file path (default: stdout)")
	cmd.Flags().StringVar(&opts.period, "period", "",
		"Filter to reports whose status.period matches this value (e.g. 2026-04)")
	cmd.Flags().StringVar(&opts.region, "region", "",
		"Value for the FOCUS Region column (e.g. data center or cluster name)")

	return cmd
}

func runExportFocus(ctx context.Context, opts *exportFocusOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	clients, err := newK8sClient()
	if err != nil {
		return err
	}

	reports, profiles, err := fetchReportsAndProfiles(ctx, clients.client, opts.namespace, opts.period)
	if err != nil {
		return err
	}
	if len(reports) == 0 {
		return fmt.Errorf(
			"no UsageReports matched (namespace=%q, period=%q) — nothing to export",
			opts.namespace, opts.period,
		)
	}

	out := os.Stdout
	if opts.output != "" {
		f, err := os.Create(opts.output)
		if err != nil {
			return fmt.Errorf("creating output file: %w", err)
		}
		defer func() { _ = f.Close() }()
		out = f
	}

	if err := focus.Write(out, focus.ExportInput{
		Reports:  reports,
		Profiles: profiles,
		Region:   opts.region,
	}); err != nil {
		return fmt.Errorf("writing FOCUS CSV: %w", err)
	}

	if opts.output != "" {
		fmt.Fprintf(os.Stderr, "Wrote %d UsageReports to %s\n", len(reports), opts.output)
	}
	return nil
}

// fetchReportsAndProfiles loads the UsageReports (optionally scoped by
// namespace and period) and every CostProfile they reference. The profile
// lookup is a map for O(1) access during row emission; missing profiles are
// tolerated so the export doesn't fail on stale references.
func fetchReportsAndProfiles(
	ctx context.Context,
	c client.Client,
	namespace, period string,
) ([]finopsv1alpha1.UsageReport, map[string]finopsv1alpha1.CostProfile, error) {
	var reportList finopsv1alpha1.UsageReportList
	listOpts := []client.ListOption{}
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}
	if err := c.List(ctx, &reportList, listOpts...); err != nil {
		return nil, nil, fmt.Errorf("listing UsageReports: %w", err)
	}

	reports := reportList.Items
	if period != "" {
		filtered := reports[:0]
		for _, r := range reports {
			if r.Status.Period == period {
				filtered = append(filtered, r)
			}
		}
		reports = filtered
	}

	profiles := make(map[string]finopsv1alpha1.CostProfile)
	for _, r := range reports {
		if _, seen := profiles[r.Spec.CostProfileRef]; seen {
			continue
		}
		var p finopsv1alpha1.CostProfile
		err := c.Get(ctx, client.ObjectKey{Name: r.Spec.CostProfileRef, Namespace: r.Namespace}, &p)
		if err == nil {
			profiles[r.Spec.CostProfileRef] = p
		}
		// Missing profile is tolerated — focus.Write produces rows with empty
		// x-Infer* fields rather than failing the whole export.
	}
	return reports, profiles, nil
}
