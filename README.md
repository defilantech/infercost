# InferCost

**True cost intelligence for on-premises AI inference.**

[![Tests](https://github.com/defilantech/infercost/actions/workflows/test.yml/badge.svg)](https://github.com/defilantech/infercost/actions/workflows/test.yml)
[![Lint](https://github.com/defilantech/infercost/actions/workflows/lint.yml/badge.svg)](https://github.com/defilantech/infercost/actions/workflows/lint.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/defilantech/infercost)](https://goreportcard.com/report/github.com/defilantech/infercost)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

InferCost is a Kubernetes-native platform that makes on-premises AI inference costs fully attributable — from GPU hardware amortization through electricity to per-request token economics. It fills the gap that [FOCUS](https://focus.finops.org/) explicitly scopes out, that [OpenCost](https://www.opencost.io/) can't compute, and that every LLM gateway sets to $0.

**Website**: [infercost.ai](https://infercost.ai) | **License**: Apache 2.0

## The Problem

No tool today computes the true cost of on-premises AI inference:

- **Cloud FinOps tools** (OpenCost, Kubecost) see GPU-hours but not tokens
- **LLM gateways** (LiteLLM, Langfuse) see tokens but assume on-prem cost is $0
- **Nobody** combines: hardware amortization + electricity + GPU utilization + token attribution = true cost-per-token

Organizations moving to on-prem inference are making million-dollar hardware decisions with zero visibility into true unit economics.

## What InferCost Does

One controller pod. No database. No UI to host. Plugs into infrastructure you already run.

```
token_cost = (GPU_amortization + electricity x power_draw x PUE) / tokens_per_hour
  → attributed per team and per user
    → compared against what OpenAI, Anthropic, or Google would charge
```

**Install**: `helm install infercost infercost/infercost` + apply one CostProfile describing your hardware.
**Time to value**: First cost data in under 5 minutes.

## Features

- **True cost-per-token** — Computed from hardware amortization, real-time GPU power draw (DCGM), and electricity rates
- **Cloud comparison** — "This would cost $X on OpenAI/Anthropic/Google" with verified pricing and honest results (including when cloud is cheaper)
- **Per-team attribution** — Costs broken down by Kubernetes namespace (zero config)
- **Prometheus metrics** — 12 gauges scrapeable by any monitoring tool (not locked to Grafana)
- **REST API** — Programmatic access to cost data for custom dashboards, bots, integrations
- **CLI** — `infercost status` and `infercost compare` for terminal-based cost analysis
- **Pre-built Grafana dashboard** — Ships as JSON, auto-provisionable via sidecar
- **Multi-backend** — Scrapes llama.cpp, vLLM, or any Prometheus-compatible inference engine

## Quick Start

### Prerequisites

- Kubernetes cluster with GPU workloads
- [DCGM Exporter](https://github.com/NVIDIA/dcgm-exporter) running (for GPU power metrics)
- Inference pods exposing Prometheus metrics (llama.cpp `--metrics`, vLLM `/metrics`)

### Install CRDs and Controller

```bash
# Install CRDs
kubectl apply -f https://raw.githubusercontent.com/defilantech/infercost/main/config/crd/bases/finops.infercost.ai_costprofiles.yaml
kubectl apply -f https://raw.githubusercontent.com/defilantech/infercost/main/config/crd/bases/finops.infercost.ai_usagereports.yaml
```

### Declare Your Hardware Economics

```yaml
# costprofile.yaml
apiVersion: finops.infercost.ai/v1alpha1
kind: CostProfile
metadata:
  name: my-gpu-cluster
spec:
  hardware:
    gpuModel: "NVIDIA H100 SXM5"
    gpuCount: 8
    purchasePriceUSD: 280000
    amortizationYears: 3
    maintenancePercentPerYear: 0.10
  electricity:
    ratePerKWh: 0.12
    pueFactor: 1.4
  nodeSelector:
    kubernetes.io/hostname: gpu-node-01
```

```bash
kubectl apply -f costprofile.yaml
```

### See Your Costs

```bash
$ kubectl get costprofiles
NAME             GPU MODEL           GPUs   $/HR    POWER (W)   AGE
my-gpu-cluster   NVIDIA H100 SXM5    8      $1.24   2400W       5m
```

### CLI

```bash
$ infercost status

INFRASTRUCTURE COSTS
====================
PROFILE         GPU MODEL         GPUs  $/HOUR   AMORT    ELEC     POWER    AGE
my-gpu-cluster  NVIDIA H100 SXM5  8     $1.2400  $1.0700  $0.1700  2400W    5m

  my-gpu-cluster projected: $893/month, $10,862/year

INFERENCE MODELS
================
MODEL        NAMESPACE    INPUT TOKENS  OUTPUT TOKENS  TOKENS/SEC  STATUS
llama-70b    production   45.2M         12.8M          42.5        Active (3 req)

QUICK COMPARISON
================
  PROVIDER    MODEL              CLOUD COST   SAVINGS
  Anthropic   claude-opus-4-6    $832.00      $794 (95%)
  OpenAI      gpt-5.4            $523.00      $485 (93%)
  Google      gemini-2.5-pro     $312.00      $274 (88%)
```

### REST API

```bash
$ curl http://localhost:8092/api/v1/costs/current
{
  "profileName": "my-gpu-cluster",
  "gpuModel": "NVIDIA H100 SXM5",
  "hourlyCostUSD": 1.24,
  "powerDrawWatts": 2400,
  "monthlyProjectedUSD": 893.00
}

$ curl http://localhost:8092/api/v1/compare
{
  "comparisons": [...],
  "pricingSources": {
    "OpenAI": "https://developers.openai.com/api/docs/pricing",
    "Anthropic": "https://platform.claude.com/docs/en/about-claude/pricing"
  },
  "disclaimer": "Cloud pricing shown is list price..."
}
```

### Grafana Dashboard

Import the pre-built dashboard from `config/grafana/infercost-dashboard.json` or auto-provision via Grafana sidecar:

```bash
kubectl create configmap infercost-dashboard \
  --from-file=config/grafana/infercost-dashboard.json \
  -n monitoring
kubectl label configmap infercost-dashboard grafana_dashboard=1 -n monitoring
```

## Architecture

```
Data Sources (already in Prometheus)    InferCost (single pod)         Outputs
┌─────────────────────────────┐    ┌─────────────────────────┐    ┌───────────────────┐
│ DCGM Exporter               │    │                         │    │ Prometheus metrics │
│  └─ GPU power draw (watts)  │───>│  GPU Power Scraper      │    │ REST API           │
│                             │    │         │               │    │ Grafana dashboard  │
│ llama.cpp / vLLM /metrics   │    │  Token Counter Scraper  │───>│ UsageReport CRDs   │
│  └─ tokens predicted/prompt │───>│         │               │    │ CLI                │
│                             │    │  Cost Calculator        │    └───────────────────┘
│ CostProfile CRD             │    │         │               │
│  └─ amortization + electric │───>│  Cloud Comparator       │
│                             │    │         │               │
│ LiteLLM PostgreSQL (opt)    │    │  Report Writer          │
│  └─ per-user token counts   │───>│                         │
└─────────────────────────────┘    └─────────────────────────┘
```

## CRDs

| CRD | Purpose |
|-----|---------|
| **CostProfile** | Declares hardware economics for a node/pool (GPU model, purchase price, amortization, electricity rate, PUE) |
| **UsageReport** | Auto-populated cost reports with per-model/namespace breakdown and cloud comparison |
| **TokenBudget** | Per-namespace spend limits with alert thresholds *(coming soon)* |

## Roadmap

| Phase | Status | Capabilities |
|-------|--------|-------------|
| **Observe** | Live | Cost-per-token, GPU power tracking, efficiency metrics |
| **Report** | Live | Per-team/model attribution, cloud comparison, UsageReport CRDs |
| **Alert** | Coming Soon | Budget thresholds, anomaly detection via Alertmanager |
| **Enforce** | Planned | Rate-limit over-budget teams, graceful model degradation |
| **Optimize** | Planned | Model switching recommendations, scale-down scheduling |
| **Comply** | Planned | Audit log export (EU AI Act, SOC 2), FOCUS spec compatible |

## Cloud Pricing

InferCost ships with verified list prices for 9 models across OpenAI, Anthropic, and Google (last verified: 2026-03-21). Prices are configurable via `config/pricing/cloud-pricing.yaml`.

Cloud comparison is honest — when cloud is cheaper than on-prem, InferCost shows negative savings. This helps you make informed decisions about which workloads belong on-prem vs. cloud.

Sources: [openai.com/pricing](https://developers.openai.com/api/docs/pricing) | [platform.claude.com/pricing](https://platform.claude.com/docs/en/about-claude/pricing) | [ai.google.dev/pricing](https://ai.google.dev/gemini-api/docs/pricing)

## Standards Alignment

- **OpenTelemetry GenAI** — Metric naming follows `gen_ai.usage.*` semantic conventions
- **FOCUS Spec** — Compatible export format with `x-Infer*` extension columns for on-prem dimensions
- **OpenCost** — Complementary (OpenCost = infrastructure cost allocation, InferCost = inference economics)
- **Kubernetes-native** — CRDs, controller-runtime, Kubebuilder scaffolding

## Development

```bash
make manifests       # Generate CRDs, RBAC
make generate        # Generate DeepCopy methods
make build           # Build controller + CLI
make test            # Unit tests (envtest)
make lint            # golangci-lint
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding standards, and PR process.

## Companion Project

InferCost works with any Kubernetes inference stack. It has first-class integration with [LLMKube](https://github.com/defilantech/llmkube) — a Kubernetes operator for deploying and managing local LLM inference with llama.cpp.

## License

Apache License 2.0 — see [LICENSE](LICENSE).

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) and sign off your commits (`git commit -s`) per the [Developer Certificate of Origin](https://developercertificate.org/).
