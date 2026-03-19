# InferCost — AI FinOps for On-Premises LLM Inference

Kubernetes-native cost intelligence platform that tracks the true cost of self-hosted LLM inference: hardware amortization, electricity, per-token and per-user attribution, with cloud-equivalent comparison.

**Repo**: github.com/defilantech/infercost | **License**: Apache 2.0
**Domain**: infercost.ai
**Module**: `github.com/defilantech/infercost` (Go)
**Status**: Pre-development (scaffolding phase)
**Companion to**: [LLMKube](https://github.com/defilantech/llmkube) (works standalone but has first-class LLMKube integration)

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

## Build Phases

1. **MVP**: CostProfile CRD, cost calculator, UsageReport writer, Grafana dashboard
2. **User Attribution**: LiteLLM PostgreSQL integration, per-user/team cost views
3. **Budget Enforcement**: TokenBudget CRD, PrometheusRule alerting
4. **Enterprise**: Monthly report export, FOCUS spec compliance, multi-cluster

## Commit Messages & Release Please

Same conventions as LLMKube:

| Prefix | When to use | Version bump |
|--------|------------|--------------|
| `feat:` | New features, new CRD fields | minor (0.x.0) |
| `fix:` | Bug fixes | patch (0.0.x) |
| `chore:` | Deps, CI, tooling | none |
| `docs:` | Documentation-only | patch |

## Clusters

- `shadowstack` kubectl context — LLMKube inference pods, DCGM metrics
- `microk8s` kubectl context — Prometheus, Grafana monitoring stack
