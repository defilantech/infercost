# Cloud comparison and break-even analysis

InferCost answers the FinOps question that a flat `$/MTok` cannot: **at what daily
volume does running this workload on your own GPUs beat the cloud?** On-prem cost
per token depends on throughput (idle hardware still costs money); cloud cost per
token does not. Break-even collapses both sides into one comparable number.

## The number

For each configured cloud target, the `UsageReport` status reports:

```yaml
status:
  breakEvenAnalysis:
    - provider: Anthropic
      model: claude-sonnet-4-6
      breakEvenTokensPerDay: 142000
      currentUtilizationTokensPerDay: 6000
      percentOfBreakEven: 4.2
      verdict: cloud-cheaper-at-current-utilization
```

- **breakEvenTokensPerDay** — the daily token volume at which on-prem daily cost
  equals what those tokens would cost on this cloud model. Above it, on-prem is cheaper.
- **currentUtilizationTokensPerDay** — your current throughput extrapolated to a full day.
- **percentOfBreakEven** — `currentUtilizationTokensPerDay / breakEvenTokensPerDay * 100`.
  At or above 100%, on-prem wins at your current utilization.
- **verdict** — `on-prem-cheaper-at-current-utilization` or `cloud-cheaper-at-current-utilization`.

## Methodology

```
breakEvenTokensPerDay = dailyHardwareCost / cloudCostPerToken
```

**dailyHardwareCost** is the fixed daily cost of owning the hardware, the cost that
must be covered before on-prem is worth it:

```
dailyHardwareCost = amortizationPerHour * 24
                  + (idleWatts / 1000) * 24 * ratePerKWh * pue
```

- `amortizationPerHour` comes from the CostProfile status (purchase price + maintenance,
  amortized over the useful life).
- `idleWatts` is `spec.electricity.idleWattsThreshold`, or a default of 20% of
  `TDPWatts × GPUCount` (30 W × GPUCount when TDP isn't declared). Idle electricity is
  included because the GPUs draw power whenever they're on, not just while serving.
- `pue` is `spec.electricity.pueFactor` (defaults to 1.0).

**cloudCostPerToken** blends the model's input and output prices by your workload's
**actual input/output ratio** for the period (50/50 when no tokens have been observed):

```
cloudCostPerToken = (inFrac * inputPerMillion + outFrac * outputPerMillion) / 1e6
```

## Choosing comparison targets

A break-even number is only meaningful against a *comparable* cloud model: a small
self-hosted coder model competes with a budget cloud model, not a flagship. Choose
the model(s) you would otherwise use, per CostProfile:

```yaml
apiVersion: finops.infercost.ai/v1alpha1
kind: CostProfile
spec:
  cloudComparison:
    targets:
      - provider: Anthropic
        model: claude-sonnet-4-6
      - provider: OpenAI
        model: gpt-5.4-nano
```

When `cloudComparison` is omitted, InferCost defaults to one mid-tier model per
provider (`gpt-5.4-mini`, `claude-sonnet-4-6`, `gemini-2.5-flash`) so the analysis
works out of the box. Targets are validated against the bundled pricing catalog
(see [pricing-refresh.md](pricing-refresh.md)); unrecognized targets are skipped and
logged rather than producing a misleading number.

## Surfaces

- **CRD status:** `kubectl get usagereport <name> -o yaml` (and the `Break-even %`
  print column shows the first target).
- **REST API:** `GET /api/v1/break-even` returns the per-target entries.
- **Prometheus / Grafana:** `infercost_break_even_tokens_per_day` and
  `infercost_percent_of_break_even` (labels `cost_profile`, `provider`, `cloud_model`)
  drive the "Percent of Break-even" gauge in the bundled dashboard (green at >= 100%).
