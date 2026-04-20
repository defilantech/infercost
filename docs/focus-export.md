# FOCUS-compatible CSV export

InferCost exports UsageReport data as CSV that conforms to the [FOCUS
specification](https://focus.finops.org/) (FinOps Open Cost and Usage
Specification). Standard FOCUS v1 columns are populated so the export
drops into any FOCUS-aware consumer (Kubecost, Cloudability, internal BI)
without a custom importer. On-prem inference specifics — GPU model, token
counts, power, cloud-equivalent cost — live in `x-Infer*` extension
columns, which FOCUS explicitly allows for vendor-specific dimensions.

## Why

FOCUS v1 was designed around SaaS cloud billing. It has no column for
"GPU amortization" or "tokens consumed" — the fields an on-prem AI FinOps
program actually needs. Rather than invent a proprietary schema or fork
FOCUS, InferCost follows the spec's extension convention (`x-` prefix)
and populates the standard columns with the closest natural analogue.

A finance team that already pipes OpenCost → Kubecost → their BI stack
can add InferCost's CSV as another data source and slice the whole AI
spend alongside cloud spend on the same dashboard.

## Running the export

CLI:

```bash
# All UsageReports in the cluster, to stdout
infercost export focus

# Scope to one namespace + write to a file
infercost export focus --namespace engineering --out engineering-april.csv

# Filter by period (matches UsageReport.status.period)
infercost export focus --period 2026-04 --out april-2026.csv

# Tag with a region for multi-cluster aggregation
infercost export focus --region us-east-1-prod --out prod-cluster.csv
```

Pipe into standard tools:

```bash
# Only charges above $1
infercost export focus | awk -F, 'NR==1 || $1+0 > 1.0'

# Total spend by namespace
infercost export focus --period 2026-04 | \
  awk -F, 'NR>1 {by_ns[$29]+=$1} END {for (n in by_ns) print n, by_ns[n]}'
```

## Row shape

One row per `(UsageReport, ModelCostBreakdown)`. A report with three
models produces three rows; a report with no model breakdown (e.g.
warming up, pods missing) produces one rollup row so the namespace still
appears in finance exports.

## Column reference

### Standard FOCUS v1 columns

| Column | Value | Notes |
|--------|-------|-------|
| `BilledCost` | Computed on-prem cost for the row | Same as EffectiveCost / ListCost / ContractedCost — no discount model on-prem |
| `EffectiveCost` | Same as BilledCost | |
| `ListCost` | Same as BilledCost | |
| `ContractedCost` | Same as BilledCost | |
| `ChargePeriodStart` | `UsageReport.status.periodStart` | RFC3339 |
| `ChargePeriodEnd` | `UsageReport.status.periodEnd` | RFC3339 |
| `BillingPeriodStart` | First of the month containing the charge | RFC3339 |
| `BillingPeriodEnd` | First of the next month | RFC3339 |
| `Currency` | `USD` | Always |
| `ServiceName` | `On-Prem AI Inference` | |
| `ServiceCategory` | `AI and Machine Learning` | FOCUS v1 canonical enum value |
| `ProviderName` | `InferCost` | |
| `PublisherName` | `Self-Hosted` | |
| `InvoiceIssuerName` | `Self-Hosted` | |
| `ResourceId` | `<ns>/<report>/<model>` (or `<ns>/<report>`) | Stable per row |
| `ResourceName` | Model name (or report name for rollups) | |
| `ResourceType` | `AI Inference Endpoint` | |
| `Region` | `--region` flag value | Empty unless explicitly set |
| `UsageQuantity` | Total tokens (input + output) | |
| `UsageUnit` | `Tokens` | |
| `PricingUnit` | `1M Tokens` | |
| `PricingCategory` | `Usage-Based` | |
| `ChargeCategory` | `Usage` | FOCUS v1 enum |
| `ChargeClass` | empty | Non-empty would indicate a correction |
| `ChargeDescription` | Human-readable summary of the charge | |
| `ChargeFrequency` | `Usage-Based` | |
| `SkuId` | Model name | |
| `SkuPriceId` | Model name | |
| `SubAccountId` | Kubernetes namespace | For per-team chargeback |
| `SubAccountName` | Kubernetes namespace | |
| `Tags` | JSON object with report/schedule/model/gpuModel | |

### InferCost extension columns (`x-Infer*`)

| Column | Value |
|--------|-------|
| `x-InferCostProfile` | Name of the CostProfile this row references |
| `x-InferGpuModel` | GPU model declared on the profile |
| `x-InferGpuCount` | Number of GPUs on the profile |
| `x-InferTokensInput` | Input tokens for this row |
| `x-InferTokensOutput` | Output tokens for this row |
| `x-InferAmortizationYears` | Amortization window from the profile |
| `x-InferElectricityRatePerKWh` | Rate from the profile |
| `x-InferPUEFactor` | PUE from the profile |
| `x-InferCloudEquivalentProvider` | Provider with highest savings vs on-prem |
| `x-InferCloudEquivalentModel` | Model name in that provider |
| `x-InferCloudEquivalentCostUSD` | What the tokens would have cost there |
| `x-InferSavingsUSD` | CloudEquivalent − on-prem |
| `x-InferSavingsPercent` | Savings as a percentage |

The cloud-equivalent columns are populated from the highest-savings entry
in `UsageReport.status.cloudComparison`. Consumers who need the full
per-provider comparison should query the CRD directly — FOCUS is a flat
export, not a query layer.

## FOCUS version alignment

InferCost tracks FOCUS v1.0. When FOCUS v1.1 lands with AI-inference
coverage, `x-Infer*` columns will migrate to the standard names in a
minor release with a documented column alias. Until then, the `x-Infer*`
prefix guarantees import compatibility (FOCUS requires importers to
tolerate unknown `x-` columns rather than reject them).

## Importing into Kubecost / Cloudability

Both tools accept FOCUS CSV via their custom-import flows. The standard
columns are enough for high-level spend dashboards. To unlock per-token
and per-GPU-model views, add the `x-Infer*` columns as custom fields.

Open a GitHub issue if you hit a downstream tool that rejects a
specific row — we will adjust the emission rather than tell users to
work around it.
