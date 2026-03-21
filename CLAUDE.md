# InferCost — True Cost Intelligence for On-Prem AI Inference

Kubernetes-native cost intelligence platform that makes on-premises AI inference costs fully attributable — from GPU amortization through electricity to per-request economics, across any inference workload.

**Repo**: github.com/defilantech/infercost | **License**: Apache 2.0
**Domain**: infercost.ai
**Module**: `github.com/defilantech/infercost` (Go 1.26)
**API Group**: `finops.infercost.ai`
**Status**: M0 — PoC development
**Positioning**: Independent product. Works with any K8s inference stack. First-class LLMKube integration.

## Problem

No tool today computes the true cost of on-premises LLM inference:
- Cloud FinOps tools (OpenCost, Kubecost) see GPU-hours but not tokens
- LLM gateways (LiteLLM, Langfuse) see tokens but set on-prem cost to $0
- Nobody combines: hardware amortization + electricity + GPU utilization + token-level attribution

## Architecture

```
Data Sources (already in Prometheus)          InferCost Controller              Outputs
┌─────────────────────────────┐          ┌──────────────────────────┐     ┌────────────────────┐
│ DCGM Exporter               │          │                          │     │ UsageReport CRDs   │
│  └─ GPU power draw (watts)  │──────────│  GPU Power Scraper       │     │ Prometheus metrics  │
│                             │          │         │                │     │ Grafana dashboards  │
│ llama.cpp /metrics          │          │  Token Counter Scraper   │────▶│ Cloud equivalent $  │
│  └─ tokens predicted/prompt │──────────│         │                │     │ Per-namespace costs │
│                             │          │  Cost Calculator         │     │ Per-user costs      │
│ CostProfile CRD             │          │         │                │     │ Budget alerts       │
│  └─ amortization + electric │──────────│  Attribution Aggregator  │     └────────────────────┘
│                             │          │         │                │
│ LiteLLM PostgreSQL (opt)    │          │  Cloud Equivalent Calc   │
│  └─ per-user token counts   │──────────│         │                │
└─────────────────────────────┘          │  Report Writer           │
                                         └──────────────────────────┘
```

## CRDs

- **CostProfile**: Declares hardware economics for a node/pool (GPU model, purchase price, amortization period, electricity rate, PUE factor)
- **TokenBudget**: Per-namespace spend limits with alert thresholds
- **UsageReport**: Auto-populated cost reports (estimated cost, cloud equivalent, savings, breakdown by model/namespace)

API group: `finops.infercost.ai`

## Key Formula

```
token_cost = (GPU_amortization_$/hr + electricity_kWh_rate * GPU_power_draw_kW * PUE) / tokens_per_hour
```

## Data Sources

| Source | What It Provides | Required? |
|--------|-----------------|-----------|
| DCGM Exporter | GPU power draw per device (watts) | Yes (NVIDIA) |
| llama.cpp /metrics | Token counts per pod | Yes |
| CostProfile CRD | Hardware cost model | Yes |
| LiteLLM PostgreSQL | Per-user/key token attribution | Optional (enables per-user tracking) |
| LLMKube CRDs | GPU counts, model refs, replicas | Optional (enriches reports) |
| vLLM /metrics | Token counts (alternative to llama.cpp) | Optional |

## Integration Points

- **Prometheus**: Primary data source. Reads DCGM + inference metrics.
- **LLMKube**: Watches InferenceService CRDs for GPU/model metadata. Not a hard dependency.
- **LiteLLM**: Reads PostgreSQL spend tables for per-user attribution. Optional.
- **Grafana**: Ships pre-built dashboard JSON.
- **OpenCost/Kubecost**: Can consume their GPU-hour data as cross-check. Optional.

## Quick Reference

```bash
make manifests       # Generate CRDs, RBAC
make generate        # Generate DeepCopy methods
make fmt             # go fmt
make vet             # go vet
make test            # Unit tests (envtest)
make build           # Build controller binary
make docker-build    # Build controller image
make install         # Install CRDs into cluster
make deploy          # Deploy controller to cluster
```

## Product Ladder

Observe → Report → Alert → Enforce → Optimize → Comply

## Build Phases

1. **M0 PoC**: CostProfile CRD, cost calculator, CLI, deploy to Shadowstack
2. **M1 MVP (v0.1.0)**: Helm chart, UsageReport writer, Grafana dashboard, docs, public launch
3. **M2 Attribution (v0.2.0)**: Per-namespace/user cost, TokenBudget CRD, alerting
4. **M3 Enterprise (v0.3.0)**: Budget enforcement, audit export, FOCUS-compatible output
5. **M4 Optimize (v0.4.0)**: Recommendations, multi-cluster, FinOps/CNCF submissions

## Standards Alignment

- **OTel GenAI**: Metric naming follows `gen_ai.usage.*` semantic conventions
- **FOCUS**: FOCUS-compatible export + `x-Infer*` extension schema for on-prem
- **OpenCost**: Complementary — they do infra cost, we do inference economics

## Commit Messages & Release Please

Same conventions as LLMKube:

| Prefix | When to use | Version bump |
|--------|------------|--------------|
| `feat:` | New features, new CRD fields | minor (0.x.0) |
| `fix:` | Bug fixes | patch (0.0.x) |
| `chore:` | Deps, CI, tooling | none |
| `docs:` | Documentation-only | patch |

**DCO sign-off required** — always use `git commit -s`.
**Do NOT include Co-Authored-By lines** — no mention of Claude/AI in commits or PRs.

## Clusters

- `shadowstack` kubectl context — LLMKube inference pods, DCGM metrics
- `microk8s` kubectl context — Prometheus, Grafana monitoring stack
