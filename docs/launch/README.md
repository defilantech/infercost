# Launch playbooks

This directory holds the step-by-step runbooks for the external-facing
moves that turn InferCost from "open-source tool in public" into a known
entity in the FinOps / Kubernetes ecosystem.

**These are intentionally not executed yet.** The project is pre-v1. Shipping
a half-baked CNCF Landscape entry or a speculative "we exist" comment on the
OpenCost GitHub issue is worse than silence — we only get one first
impression with each audience.

The trigger for executing these runbooks is **v1.0.0 feature-complete**,
defined as the point where a first-time user can install the Helm chart on a
fresh GPU cluster and get accurate, attributable, FOCUS-aligned cost data
without reading the source code. Until then, these documents exist so the
moves are ready to execute in order the moment the product earns them.

| Runbook | When to run |
|---------|------------|
| [cncf-landscape.md](cncf-landscape.md) | v1.0.0 shipped, at least one production deployment, docs site live |
| [opencost-engagement.md](opencost-engagement.md) | Same trigger — these land together for compounding effect |
| [hn-reddit-launch.md](hn-reddit-launch.md) | Same week as CNCF/OpenCost, 48h after to pick up the trail |
| [finops-foundation.md](finops-foundation.md) | Month 2-3 post-launch, after ≥3 real deployments and a draft WG paper |

## Triggering a launch

Launches are a branch of work, not a spontaneous event. To execute:

1. Verify the **v1 feature-complete checklist** at the top of each runbook.
   If any item is unchecked, stop and fix it before posting anything public.
2. Schedule all three posts (CNCF Landscape PR, OpenCost comment, HN/Reddit)
   within a 72-hour window. They reinforce each other — spacing them out
   hurts reach.
3. Monitor the week after and reply to every comment within 24 hours.
   Launches where the maintainer disappears get archived as "yet another
   one-shot project."
