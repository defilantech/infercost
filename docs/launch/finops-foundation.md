# FinOps Foundation engagement runbook

**Status**: Prepared, not executed. Expected execution window: month 2-3
after v1 launch, once we have ≥3 real deployments and a draft working-group
paper.

## What we want from the FinOps Foundation

Not a membership for its own sake. The specific goals, in order:

1. **Legitimacy with finance teams.** A CTO evaluating InferCost hears
   "FinOps Foundation" and it moves the tool from "open-source side
   project" to "aligned with the standards body the CFO's team knows
   about." That matters in enterprise sales conversations even when we
   aren't selling anything.

2. **FOCUS spec contribution.** The FOCUS specification explicitly scopes
   out on-premises inference economics. InferCost is one of the few
   working implementations of a FOCUS-compatible on-prem schema. There is
   a concrete contribution to make in the AI working group.

3. **AI FinOps working group.** It is forming now. Being a contributor
   from day one beats being a latecomer. The working group produces the
   thought-leadership papers that FinOps Certified Practitioners read.

## Why we are waiting

Two reasons:

- **Joining as a pitch is different from joining as a contributor.**
  Walking in with "we built a tool" gets us a polite Slack welcome. Walking
  in with a 15-page draft WG paper titled "On-Premises LLM Inference Cost
  Attribution: Schema, Implementation, and FOCUS Alignment" gets us a
  working-group seat and co-authorship credit on the AI FinOps paper the
  foundation will publish.

- **Membership without deployment references is hollow.** The first
  question at any WG meeting is "who is running this in production?" We
  need at least three referenceable deployments — AFI (if the disclosure
  is approved), plus two external adopters — before we can answer that
  question credibly.

## Pre-execution checklist

- [ ] v1.0 released and on CNCF Landscape
- [ ] AT LEAST three production deployments (named or anonymized with
      FinOps-leadership sign-off)
- [ ] Working-group paper draft: 10-15 pages, PDF, addresses
      - On-prem inference economics: why it is different from general
        cloud FinOps
      - A proposed schema extension for FOCUS (the `x-Infer*` columns
        we already ship)
      - A reference implementation (InferCost) with a deployment
        architecture diagram
      - At least one worked example with real numbers
- [ ] FOCUS-compatible export verified against the current FOCUS spec
      version (not an old draft)

## Membership tier

**General membership** is the right tier when we engage. It gives
working-group participation and voting rights on paper adoption. We do
not need FinOps Certified Platform status yet — that is a larger lift
(paid audit, feature-gating requirements) and makes sense only once the
enterprise tier ships with multi-cluster aggregation, audit export, and
SLA support.

Target escalation:
- **Month 3 post-launch**: General membership + submit WG paper
- **Month 9 post-launch**: WG paper accepted / published
- **Month 15-18**: Consider FinOps Certified Platform audit

## Working-group paper: target outline

```
1. Scope: On-prem inference economics (what FOCUS v1 does not cover)
2. Why the gap exists
   - GPU allocation ≠ token allocation
   - Hardware amortization changes per-token cost over time
   - Electricity is a dimension FOCUS does not model
3. Proposed schema
   - x-InferGpuModel, x-InferGpuCount
   - x-InferTokensInput, x-InferTokensOutput
   - x-InferPowerKWh, x-InferPUEFactor, x-InferAmortizationMonths
4. Reference implementation: InferCost
   - CostProfile, UsageReport CRDs
   - DCGM + llama.cpp/vLLM data flow
   - FOCUS CSV export semantics
5. Worked example
   - 2x RTX 5060 Ti, 30-day period, ~50M tokens
   - Cost breakdown: amortization, electricity, per-token, per-namespace
   - Cloud equivalent comparison (list prices, with caveats)
6. Recommendations for FOCUS v1.x
   - Adopt x-Infer* as a standard extension for on-prem inference
   - Align with OpenTelemetry GenAI semantic conventions
7. Limitations and open questions
   - Multi-tenant attribution when pods share GPUs
   - Apple Silicon power telemetry
   - Batch discount modeling in cloud comparisons
```

## Who to reach out to (not now — when executing)

Research target contacts closer to execution. The FinOps Foundation roster
rotates; any names written here would be stale by month 3. When the time
comes:
- Identify current WG chairs via FinOps Foundation website
- Check who has recently published on AI/ML costs
- Message through the foundation's Slack or official contact form, not
  cold LinkedIn

## What NOT to do

- **Do not pay for individual practitioner certification** before the
  tool is deployed. The cert is personal, not organizational, and the
  study effort is real. If it is useful, it is useful at month 6+.
- **Do not speak for the foundation** in any public forum. "FinOps
  Foundation member" ≠ "FinOps Foundation spokesperson." Getting this
  wrong burns the relationship.
- **Do not publicly announce membership before the working-group paper
  is submitted.** The announcement with "we just joined" is less
  interesting than the announcement with "we just published a paper with
  the FinOps Foundation."
