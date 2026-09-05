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
| `marginalCostPerMillionTokens` | Electricity-only \$ / 1M tokens during active time | What each token costs in power, once you own the hardware |
| `activeEnergyKWh` | Integrated kWh across active intervals | Numerator of the marginal calculation, exposed so operators can sanity-check |

- `breakEvenAnalysis[]` | Per cloud target, the daily token volume at which on-prem cost equals the cloud cost, plus current throughput and a verdict | The decision metric: see [cloud-comparison.md](cloud-comparison.md)

Future fields (tracked in GitHub issues [#37, #41](https://github.com/defilantech/infercost/issues?q=is%3Aissue+label%3Aarea%2Fcost-model)):

- Active-hour amortization (#37) — will replace the current wall-clock amortization denominator when enabled
- Utilization-aware status message (#41)

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
2. **Marginal comparison** (`marginalCostPerMillionTokens`) — compare
   on-prem electricity-during-serving to cloud per-token. InferCost
   computes this today from DCGM samples; see the example below.

### Marginal vs amortized — an example

Take the live shadowstack cluster on a low-traffic day at 4% utilization:

| Metric | Value | What it means |
|---|---|---|
| `costPerMillionTokens` | ~\$16 | Full cost of ownership at today's throughput |
| `marginalCostPerMillionTokens` | ~\$0.004 | Electricity you actually burned generating tokens |
| Anthropic Opus output rate | ~\$15 / MTok | Marginal cloud pricing (all you ever pay) |
| OpenAI GPT-5.4-nano output rate | ~\$0.40 / MTok | Marginal cloud pricing |

Comparing amortized on-prem (~\$16) to Opus (~\$15) says "cloud wins."
Comparing marginal on-prem (~\$0.004) to Opus (~\$15) says "on-prem
wins by 3750×." Neither number is wrong; they answer different
questions.

The honest answer is: *at 4% utilization you're wasting most of the
hardware you already bought. Route more traffic to it (or buy a
smaller GPU) before comparing to cloud APIs.* At 100% utilization the
amortized and marginal numbers converge — on-prem wins decisively at
any non-trivial scale.

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

InferCost records one power sample per CostProfile on every reconcile
(30s), then computes active/idle hours and energy from them for each
reporting period.

**Default (no `--data-dir`):** samples are kept in a 48-hour in-memory
window. Daily and week-to-date reports are exact from that window;
monthly reports extrapolate linearly for the older hours. A controller
restart resets the buffer.

**Persistent (with `--data-dir`):** the sampler is backed by a local
bbolt file and retention defaults to 45 days, so daily, weekly, and
monthly reports are all computed exactly from recorded samples rather
than extrapolated, and history survives restarts. This is a local state
file owned by the operator, not an external database to host.

### Why a local store instead of Prometheus remote-write

The cluster's Prometheus already scrapes the controller's
`infercost_gpu_power_watts` gauge, so a remote-write path would be
redundant for monitoring. But relying on Prometheus as the durable
history source has two problems for a financial source of truth:

- **Retention is not ours.** Prometheus retention is often 15 days by
  default, so monthly accuracy would depend on whoever runs Prometheus
  raising it. A local store keeps retention under our control.
- **Threshold semantics.** The active/idle classification is captured
  per sample (`ActiveW`) at record time, so raising the idle threshold
  later never retroactively relabels old samples. Reading raw power back
  from Prometheus re-derives classification at query time, silently
  changing what a past period means.

A local bbolt store keeps the operator self-contained, gives deterministic
history, and remains a single-replica write, which bbolt handles cleanly.

### Storage layout

`--data-dir` points at a directory (default `/var/lib/infercost` in the
Helm chart), and InferCost opens a `samples.db` inside it. In the chart,
`sampleStore.enabled` enables the flag and mounts a volume; the default is
an `emptyDir` (survives a container restart) and an optional PVC is
available via `sampleStore.persistence.enabled` or an
`existingClaim` for history that survives pod replacement. `--sample-retention`
overrides the 45-day default.

## Grafana dashboard

The `infercost-ai-inference-costs` dashboard renders:

- `costPerMillionTokens` (single-stat, headline amortized \$/MTok)
- `utilizationPercent` (gauge, 0-100%)
- Power-over-time (line chart, with the idle threshold drawn)
- Per-model and per-namespace cost breakdowns

Dashboards update on every UsageReport reconcile (default every 5
minutes). The power panel updates on CostProfile reconcile (every 30
seconds) for real-time load visibility.
