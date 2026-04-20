# OpenCost engagement runbook

**Status**: Prepared, not executed. Do not post until CNCF Landscape PR is
merged and at least one production deployment is referenceable.

## The opportunity

OpenCost issue [#3533](https://github.com/opencost/opencost/issues/3533)
is an open request titled roughly "AI inference cost allocation." The
OpenCost team has acknowledged it is out of scope for their roadmap but
valuable. That issue has a small active audience of FinOps engineers who
exactly match the InferCost early-adopter profile.

The move is **not**: "hey we built this, check it out." That reads as
hijacking an issue thread and gets flagged by maintainers (rightly).

The move **is**: post a complementary-integration proposal that positions
OpenCost + InferCost as a two-tool stack, not competitors, and demonstrates
we have read their codebase and allocation API.

## Why we are waiting

A comment that proposes integration must reference an existing integration.
Without a working OpenCost-allocation-API consumer in the InferCost code,
the comment is aspirational and the OpenCost maintainers will politely
ignore it.

Execute this runbook **only when** InferCost can read OpenCost's
`/allocation` API and use those allocations as its infrastructure-cost
basis, enriching them with inference economics. This is a v1.1 feature,
not v1.0 — it is complex enough to warrant a dedicated PR and design
review against the OpenCost schema.

## Pre-execution checklist

- [ ] InferCost can consume the OpenCost allocation API and reconcile its
      GPU-hour allocations against InferCost's CostProfile CRDs
- [ ] An end-to-end test deploys OpenCost + InferCost side-by-side on
      shadowstack and shows consistent GPU-hour numbers
- [ ] A one-pager at `docs/integrations/opencost.md` describes the
      integration, the schema mapping, and the configuration flags
- [ ] InferCost v1 is live on CNCF Landscape (runbook above completed)

## Comment template

Post as a top-level comment on
[opencost/opencost#3533](https://github.com/opencost/opencost/issues/3533).
Keep it under 250 words — long comments get skimmed.

```
We built InferCost (https://infercost.ai, Apache 2.0) explicitly to
complement OpenCost's infrastructure cost allocation with inference
economics — the gap this issue identifies.

Division of concerns as we see it:
  - OpenCost: GPU-hour allocation, namespace/workload attribution,
    cluster-wide cost accounting (which you already do well)
  - InferCost: per-token economics, hardware amortization over those
    allocated GPU-hours, cloud-API comparison, FOCUS export for AI
    inference

InferCost consumes OpenCost's /allocation API as the infrastructure-cost
basis and writes per-token cost attribution back out via Prometheus
metrics and a FOCUS-compatible CSV export. We documented the integration
at infercost.ai/docs/integrations/opencost.

We are not proposing OpenCost absorb this — the CRDs and calculations are
specific enough to the AI workload class (token metrics, DCGM power,
model-name attribution) that folding them into OpenCost's general-purpose
model would likely hurt both tools. But if the OpenCost maintainers see
value in pointing users here for the AI-inference piece, we'd welcome
that, and we'd be happy to reciprocate from our side.

Happy to answer questions or discuss schema alignment.
```

## What to avoid

- **Do not link to a LinkedIn post.** FinOps engineers on GitHub are
  allergic to social-media-style promotion on technical threads.
- **Do not open a parallel PR on OpenCost.** The comment is the move.
  Any PR would be a drive-by and unwelcome.
- **Do not respond defensively** if someone questions the overlap. The
  response is: "Fair question — here is how the scopes differ in the
  actual code at X link." Link, don't argue.

## What to do after posting

- Star OpenCost. Follow a few of their maintainers on GitHub.
- Reply to every comment on the thread within 24 hours. Dropped threads
  are worse than silence.
- If an OpenCost maintainer engages positively, invite a cross-link from
  their integrations doc. Don't propose this unprompted.

## Timing

Post on a Tuesday or Wednesday. Monday is noise from weekend digest
readers; Thursday/Friday loses the work-week attention window. Same week
as CNCF Landscape merge and HN post — all three moves reinforce each
other.
