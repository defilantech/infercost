# CNCF Landscape submission runbook

**Status**: Prepared, not executed. Do not run until the v1 feature-complete
checklist at the bottom is green.

The [CNCF Landscape](https://landscape.cncf.io) is the cloud-native ecosystem
map. Getting InferCost on it is the single highest-signal "this is a real
project" move for the FinOps, Platform Engineering, and Kubernetes-tooling
audiences. It is not a certification or an endorsement — it is a directory —
but it is the directory operators consult when they are evaluating tools.
Missing from the Landscape and someone asking "does this exist?" on Reddit
lose to a one-line entry on the Landscape every time.

## Why we are waiting

The Landscape page for each entry renders publicly within a week of merge
and is visible to anyone who clicks around the FinOps or Observability
categories. If the linked README walks a prospective user into "no data"
Grafana dashboards, half-working vLLM support, or a hardcoded pricing table
with a six-month-old `lastVerified` date, that user files the project under
"not serious yet" and does not come back. Once is enough.

## What the submission is

A YAML entry added to
[cncf/landscape](https://github.com/cncf/landscape) under `landscape.yml` in
the category best matching InferCost. FinOps is the natural home:

```
- category: Provisioning
  subcategories:
    - subcategory: Continuous Integration & Delivery     # wrong
    - subcategory: FinOps                                # right
```

Full entry skeleton:

```yaml
- item:
    name: InferCost
    homepage_url: https://infercost.ai
    logo: infercost.svg         # uploaded separately under hosted_logos/
    repo_url: https://github.com/defilantech/infercost
    description: >-
      Kubernetes-native cost intelligence for on-premises AI inference —
      computes true cost-per-token from hardware amortization, DCGM-reported
      GPU power draw, and electricity rates, then compares against cloud
      API pricing.
    crunchbase: "" # none; independent project
    open_source: true
    twitter: "https://twitter.com/defilan"
    extra:
      annotations_issue: "" # optional tracking link
      accepted: ""
```

The logo must be an SVG with a consistent viewBox; the Landscape renders at
different sizes. InferCost's existing `docs/images/logo.svg` is the source.

## Category choice

FinOps is right. InferCost is not OpenCost (infrastructure cost allocation)
and not a generic observability tool — the product is specifically about the
economics of a specific workload class (AI inference). The FinOps
subcategory is sparsely populated, which is good: InferCost shows up alongside
OpenCost, Kubecost, and the smaller FinOps tools, not buried in the 400-item
Monitoring subcategory.

## Step-by-step

1. **Fork `cncf/landscape`** to the personal GitHub account that will sign
   the PR.

2. **Add the logo.** `hosted_logos/infercost.svg` — same file as
   `docs/images/logo.svg` in the repo. Verify it renders in the Landscape
   preview (there is a `make preview` in the landscape repo).

3. **Edit `landscape.yml`.** Find the FinOps subcategory in the
   `Provisioning` category. Add the entry from the skeleton above, preserving
   alphabetical order within the subcategory.

4. **Run the landscape repo's validation.** It catches common mistakes
   (bad logo path, broken repo URL, missing fields):

   ```bash
   make check-missing
   make validate-names
   ```

5. **Open a PR titled** `feat: add InferCost to FinOps subcategory`.
   Body: one paragraph on what InferCost is, links to the live website and a
   recent release, and a note on the team size (honest: solo maintainer with
   active community).

6. **Review cycle.** The CNCF Landscape maintainers typically respond within
   a week. Expect at most one round of changes — usually about the
   description length or a logo tweak.

7. **Merge + post-merge verify.** Once merged, confirm the entry appears at
   `landscape.cncf.io/card-mode?category=fin-ops` within 24 hours.

## v1 feature-complete checklist (must be green before submitting)

- [ ] Install story: `helm install` on a fresh GPU cluster → cost data in
      Grafana within 5 minutes with no extra config
- [ ] vLLM scraper shipping in the chart, tested against a real vLLM pod
- [ ] `DCGMReachable` condition surfaces on CostProfile in every failure
      mode (PR #28 landed)
- [ ] FOCUS export produces a valid CSV importable into Kubecost or a
      FinOps BI tool without transformation
- [ ] Cloud pricing `lastVerified` is no older than 30 days
- [ ] Documentation site live at infercost.ai/docs with Quickstart, CRD
      reference, Troubleshooting, FAQ
- [ ] Launch blog post published at infercost.ai/blog
- [ ] At least one non-founder production deployment referenceable
      (anonymized if needed)
- [ ] GitHub repo: no open `bug` issues from the initial release, CI
      consistently green on main
- [ ] README under 600 lines, with a screenshot of the Grafana dashboard
      showing real data

## Why the bar is this high

The Landscape entry is a promise that what the prospective user will
experience matches the description. "Kubernetes-native cost intelligence"
sets the expectation of a cohesive product, not a starter kit. A prospect
who installs and hits rough edges within 5 minutes will downgrade not just
their opinion of InferCost but their willingness to trust anything they find
through the CNCF Landscape.

## Timing

Submit on a **Monday or Tuesday**. CNCF reviewers process PRs during the
working week; weekend submissions sit for days. Announce the merged entry on
LinkedIn and X the same day it goes live — the Landscape URL is the most
credible link we can share.
