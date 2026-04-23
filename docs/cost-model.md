# Cost model

InferCost surfaces several related numbers. This document explains what each
one means, which question it answers, and when it misleads.

## The fields on `UsageReport.status`

| Field | What it is | What it tells you |
|---|---|---|
| `estimatedCostUSD` | Total on-prem cost for the reporting period | What your hardware cost you over the period, regardless of utilization |
| `costPerMillionTokens` | Amortized \$ / 1M tokens | Full cost of ownership per token, assuming today's utilization continues |
| `utilizationPercent` | Fraction of the period GPUs drew above idle power | How much of the period the hardware actually worked |
| `activeHoursInPeriod` | Hours with power above the idle threshold | Numerator of utilization |
| `totalHoursInPeriod` | Wall-clock hours elapsed in the period | Denominator of utilization |
| `gpuEfficiencyRatio` | Same as `utilizationPercent / 100` | Compatibility with the Grafana dashboard |

Future fields (tracked in GitHub issues [#37–#41](https://github.com/defilantech/infercost/issues?q=is%3Aissue+label%3Aarea%2Fcost-model)):

- `marginalCostPerMillionTokens` — electricity-only \$/MTok during active hours
- Break-even tokens-per-day per cloud provider

## What's "active"?

A GPU is "active" when DCGM reports power draw above the
`CostProfile.spec.electricity.idleWattsThreshold`. When the operator
doesn't set one, InferCost defaults to:

- 20% of `hardware.tdpWatts × hardware.gpuCount` when TDP is declared
- `30W × hardware.gpuCount` otherwise

Examples for common 2-GPU setups:

| Hardware | Default threshold |
|---|---|
| 2× RTX 5060 Ti (150 W TDP) | 60 W total |
| 2× RTX 4090 (450 W TDP) | 180 W total |
| 2× H100 SXM5 (700 W TDP) | 280 W total |

The threshold is a sampling decision, not a hardware claim. Raise it if
your baseline idle power is higher than the default (e.g., on a
multi-tenant node where other workloads keep power elevated).

## Why `costPerMillionTokens` can mislead

`estimatedCostUSD` is `hourlyCostUSD × totalHoursInPeriod`. It includes
every hour of the period, not just active hours. If the GPUs sat idle
for 23 of 24 hours of a daily report, you still paid hardware
amortization and idle electricity for all 24 — so `estimatedCostUSD`
reflects that.

`costPerMillionTokens` divides that total cost by whatever tokens you
served, which can produce a very high number when utilization is low.
This is **mathematically correct** for answering *"what did today's
workload cost per token, given that we paid for the whole day of
hardware?"*

It is **not** the right number to compare to cloud API pricing
directly, because cloud APIs bill marginally (only while you're
serving). Comparing amortized on-prem to marginal cloud is apples-to-
oranges.

## When cloud looks cheaper than it is

Cloud API prices assume you'll only pay when you use them. On-prem
costs money whether you use it or not (you bought the card). The fair
comparison is one of:

1. **Break-even tokens/day** (issue [#39](https://github.com/defilantech/infercost/issues/39)) — at
   what daily token volume does the on-prem bill match the cloud bill?
2. **Marginal comparison** (issue [#38](https://github.com/defilantech/infercost/issues/38)) — compare
   on-prem electricity-during-serving to cloud per-token.

Until those land, the most honest framing is:

> *Today: \$0.066 on 3,993 tokens, utilization 4%. At 100% utilization
> on this hardware, the same \$0.066 would have bought ~100× more
> tokens.*

## When to raise `idleWattsThreshold`

The default (20% of TDP) is conservative. If your dashboards show
spuriously high utilization when the GPU isn't actually serving
requests (e.g., a CUDA daemon or a monitoring workload keeping the
cards at 25% TDP), raise the threshold. The CostProfile field is live:
the next reconcile tick picks up the new threshold and applies it to
future samples only — previously-recorded samples keep their original
classification, so changing the threshold doesn't retroactively rewrite
yesterday's numbers.

## Samples and retention

InferCost keeps the most recent 48 hours of samples in process memory.
Daily and "week-to-date" reports are computed exactly from those
samples. Monthly reports extrapolate linearly from the retained window
for older hours — accurate enough for monthly attribution, not a
substitute for a Prometheus-backed historian when operators need
historical drilldown.

Controller restarts reset the in-memory buffer. This is an MVP-level
trade-off; a future iteration will persist samples to a ConfigMap or
scrape them from the cluster's Prometheus.

## Grafana dashboard

The `infercost-ai-inference-costs` dashboard renders:

- `costPerMillionTokens` (single-stat, headline amortized \$/MTok)
- `utilizationPercent` (gauge, 0-100%)
- Power-over-time (line chart, with the idle threshold drawn)
- Per-model and per-namespace cost breakdowns

Dashboards update on every UsageReport reconcile (default every 5
minutes). The power panel updates on CostProfile reconcile (every 30
seconds) for real-time load visibility.
