/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
*/

// Package focus emits FOCUS-compatible CSV from InferCost UsageReport CRs.
//
// FOCUS (FinOps Open Cost and Usage Specification) is the emerging standard
// the FinOps Foundation publishes for billing/usage data exchange. Its v1
// schema explicitly scopes out on-premises inference economics — there is no
// standard column for "GPU amortization" or "tokens consumed." InferCost
// fills the gap with a set of x-Infer* extension columns while keeping the
// standard columns populated so the CSV drops into any FOCUS-aware consumer
// (Kubecost, Cloudability, internal BI) without a custom importer.
//
// Every row represents one charge — one (period, namespace, model) cell in
// the cost grid. A single UsageReport typically expands to N rows, one per
// ModelCostBreakdown. Reports with no model breakdown emit a single rollup
// row so a misconfigured namespace still reports zero rather than
// disappearing silently.
package focus

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
)

// Columns is the full ordered column header emitted in every CSV. Order is
// significant — downstream importers key off column position when they do
// not fully parse the header.
var Columns = []string{
	// Standard FOCUS v1 columns
	"BilledCost",
	"EffectiveCost",
	"ListCost",
	"ContractedCost",
	"ChargePeriodStart",
	"ChargePeriodEnd",
	"BillingPeriodStart",
	"BillingPeriodEnd",
	"Currency",
	"ServiceName",
	"ServiceCategory",
	"ProviderName",
	"PublisherName",
	"InvoiceIssuerName",
	"ResourceId",
	"ResourceName",
	"ResourceType",
	"Region",
	"UsageQuantity",
	"UsageUnit",
	"PricingUnit",
	"PricingCategory",
	"ChargeCategory",
	"ChargeClass",
	"ChargeDescription",
	"ChargeFrequency",
	"SkuId",
	"SkuPriceId",
	"SubAccountId",
	"SubAccountName",
	"Tags",
	// InferCost extensions for on-prem inference (prefix per FOCUS spec)
	"x-InferCostProfile",
	"x-InferGpuModel",
	"x-InferGpuCount",
	"x-InferTokensInput",
	"x-InferTokensOutput",
	"x-InferAmortizationYears",
	"x-InferElectricityRatePerKWh",
	"x-InferPUEFactor",
	"x-InferCloudEquivalentProvider",
	"x-InferCloudEquivalentModel",
	"x-InferCloudEquivalentCostUSD",
	"x-InferSavingsUSD",
	"x-InferSavingsPercent",
}

// ExportInput is everything needed to emit a FOCUS CSV from an InferCost
// deployment. Reports and Profiles are fetched by the caller (CLI or API) so
// this package has no k8s-client dependency and is trivial to unit-test.
type ExportInput struct {
	Reports  []finopsv1alpha1.UsageReport
	Profiles map[string]finopsv1alpha1.CostProfile // keyed by name
	Region   string                                // optional, maps to the FOCUS Region column
}

// Write emits FOCUS-compatible CSV for all reports in the input, one row per
// model breakdown (or one rollup row when a report has no model data).
func Write(w io.Writer, input ExportInput) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(Columns); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}

	// Deterministic ordering so goldens don't flake and finance can diff runs.
	reports := make([]finopsv1alpha1.UsageReport, len(input.Reports))
	copy(reports, input.Reports)
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Namespace != reports[j].Namespace {
			return reports[i].Namespace < reports[j].Namespace
		}
		return reports[i].Name < reports[j].Name
	})

	for _, r := range reports {
		rows := rowsForReport(r, input)
		for _, row := range rows {
			if err := cw.Write(row); err != nil {
				return fmt.Errorf("writing row: %w", err)
			}
		}
	}

	cw.Flush()
	return cw.Error()
}

// rowsForReport expands a single UsageReport into FOCUS rows. If the report
// has model-level breakdowns it emits one row per model+namespace; otherwise
// one rollup row so the namespace still appears in finance exports.
func rowsForReport(r finopsv1alpha1.UsageReport, input ExportInput) [][]string {
	profile := input.Profiles[r.Spec.CostProfileRef]
	best := bestCloudComparison(r.Status.CloudComparison)

	if len(r.Status.ByModel) == 0 {
		return [][]string{rowFromTotals(r, profile, input.Region, best)}
	}

	models := make([]finopsv1alpha1.ModelCostBreakdown, len(r.Status.ByModel))
	copy(models, r.Status.ByModel)
	sort.Slice(models, func(i, j int) bool {
		if models[i].Namespace != models[j].Namespace {
			return models[i].Namespace < models[j].Namespace
		}
		return models[i].Model < models[j].Model
	})

	rows := make([][]string, 0, len(models))
	for _, m := range models {
		rows = append(rows, rowFromModel(r, m, profile, input.Region, best))
	}
	return rows
}

// rowFromModel builds one CSV row for a (UsageReport, ModelCostBreakdown).
// The cloud comparison is per-report, not per-model, because UsageReport
// doesn't carry per-model cloud numbers today — we attribute the same best
// comparison to each row and note the period in ChargeDescription.
func rowFromModel(
	r finopsv1alpha1.UsageReport,
	m finopsv1alpha1.ModelCostBreakdown,
	profile finopsv1alpha1.CostProfile,
	region string,
	best *finopsv1alpha1.CloudComparisonEntry,
) []string {
	return buildRow(
		m.EstimatedCostUSD,
		periodStart(r), periodEnd(r),
		m.InputTokens+m.OutputTokens,
		fmt.Sprintf("%s/%s/%s", r.Namespace, r.Name, m.Model),
		m.Model,
		m.Namespace, m.Namespace,
		fmt.Sprintf("Inference on %s for period %s", m.Model, r.Status.Period),
		m.Model, // SkuId == model name
		buildTags(r, m, profile),
		profile, r.Spec.CostProfileRef, region,
		m.InputTokens, m.OutputTokens,
		best, m.EstimatedCostUSD,
	)
}

// rowFromTotals is the rollup path when a report has no per-model breakdown.
// This happens when inference is still warming up, pods are missing, or
// scraping failed — we emit one row so finance sees the namespace anyway.
func rowFromTotals(
	r finopsv1alpha1.UsageReport,
	profile finopsv1alpha1.CostProfile,
	region string,
	best *finopsv1alpha1.CloudComparisonEntry,
) []string {
	return buildRow(
		r.Status.EstimatedCostUSD,
		periodStart(r), periodEnd(r),
		r.Status.InputTokens+r.Status.OutputTokens,
		fmt.Sprintf("%s/%s", r.Namespace, r.Name),
		r.Name,
		r.Namespace, r.Namespace,
		fmt.Sprintf("Inference rollup for period %s (no model breakdown)", r.Status.Period),
		"", // no SkuId when no model
		fmt.Sprintf("costProfile=%s", r.Spec.CostProfileRef),
		profile, r.Spec.CostProfileRef, region,
		r.Status.InputTokens, r.Status.OutputTokens,
		best, r.Status.EstimatedCostUSD,
	)
}

// buildRow centralizes the column order so the header and the rows never
// drift. Adding a new column requires adding it here and in Columns.
func buildRow(
	cost float64,
	start, end time.Time,
	totalTokens int64,
	resourceID, resourceName, subAccountID, subAccountName, chargeDesc, skuID, tags string,
	profile finopsv1alpha1.CostProfile,
	profileName, region string,
	tokensIn, tokensOut int64,
	best *finopsv1alpha1.CloudComparisonEntry,
	onPremCost float64,
) []string {
	costStr := money(cost)
	billingStart, billingEnd := monthBounds(start)

	row := []string{
		costStr,                            // BilledCost
		costStr,                            // EffectiveCost — self-reported, same as billed
		costStr,                            // ListCost — same; no discount model on-prem
		costStr,                            // ContractedCost — same; self-reported
		isoTime(start),                     // ChargePeriodStart
		isoTime(end),                       // ChargePeriodEnd
		isoTime(billingStart),              // BillingPeriodStart
		isoTime(billingEnd),                // BillingPeriodEnd
		"USD",                              // Currency
		"On-Prem AI Inference",             // ServiceName
		"AI and Machine Learning",          // ServiceCategory (FOCUS enum value)
		"InferCost",                        // ProviderName
		"Self-Hosted",                      // PublisherName — no third-party biller
		"Self-Hosted",                      // InvoiceIssuerName
		resourceID,                         // ResourceId
		resourceName,                       // ResourceName
		"AI Inference Endpoint",            // ResourceType
		region,                             // Region — caller-supplied (e.g. node label)
		strconv.FormatInt(totalTokens, 10), // UsageQuantity
		"Tokens",                           // UsageUnit
		"1M Tokens",                        // PricingUnit
		"Usage-Based",                      // PricingCategory
		"Usage",                            // ChargeCategory (FOCUS enum)
		"",                                 // ChargeClass (empty = not a correction)
		chargeDesc,                         // ChargeDescription
		"Usage-Based",                      // ChargeFrequency
		skuID,                              // SkuId
		skuID,                              // SkuPriceId
		subAccountID,                       // SubAccountId
		subAccountName,                     // SubAccountName
		tags,                               // Tags
		// x-Infer* extensions
		profileName,
		profile.Spec.Hardware.GPUModel,
		strconv.FormatInt(int64(profile.Spec.Hardware.GPUCount), 10),
		strconv.FormatInt(tokensIn, 10),
		strconv.FormatInt(tokensOut, 10),
		strconv.FormatInt(int64(profile.Spec.Hardware.AmortizationYears), 10),
		strconv.FormatFloat(profile.Spec.Electricity.RatePerKWh, 'f', -1, 64),
		strconv.FormatFloat(profile.Spec.Electricity.PUEFactor, 'f', -1, 64),
	}

	if best != nil {
		row = append(row,
			best.Provider,
			best.Model,
			money(best.EstimatedCloudCostUSD),
			money(best.EstimatedCloudCostUSD-onPremCost),
			strconv.FormatFloat(best.SavingsPercent, 'f', 2, 64),
		)
	} else {
		row = append(row, "", "", "", "", "")
	}
	return row
}

// bestCloudComparison picks the highest-savings comparison (the one that
// makes on-prem look best) because that is the useful benchmark for a
// finance audit trail — "what would it have cost us if we had gone cloud."
// Consumers who need the full comparison matrix can reach into the
// UsageReport CRD directly; FOCUS CSV is a flat export, not a query layer.
func bestCloudComparison(entries []finopsv1alpha1.CloudComparisonEntry) *finopsv1alpha1.CloudComparisonEntry {
	if len(entries) == 0 {
		return nil
	}
	best := &entries[0]
	for i := 1; i < len(entries); i++ {
		if entries[i].SavingsUSD > best.SavingsUSD {
			best = &entries[i]
		}
	}
	return best
}

func buildTags(r finopsv1alpha1.UsageReport, m finopsv1alpha1.ModelCostBreakdown, profile finopsv1alpha1.CostProfile) string {
	// FOCUS Tags is a JSON-object string in v1. Keep it minimal but useful:
	// the dimensions an auditor would want to filter on.
	return fmt.Sprintf(`{"report":"%s","schedule":"%s","model":"%s","gpuModel":"%s"}`,
		r.Name, r.Spec.Schedule, m.Model, profile.Spec.Hardware.GPUModel)
}

func periodStart(r finopsv1alpha1.UsageReport) time.Time {
	if r.Status.PeriodStart != nil {
		return r.Status.PeriodStart.Time
	}
	return time.Time{}
}

func periodEnd(r finopsv1alpha1.UsageReport) time.Time {
	if r.Status.PeriodEnd != nil {
		return r.Status.PeriodEnd.Time
	}
	return time.Time{}
}

func monthBounds(t time.Time) (time.Time, time.Time) {
	if t.IsZero() {
		return t, t
	}
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	return start, end
}

func isoTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func money(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}
