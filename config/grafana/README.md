# InferCost Grafana Dashboard

Pre-built dashboard for visualizing InferCost metrics.

## Import

### Option A: Grafana UI

1. Open Grafana → Dashboards → Import
2. Upload `infercost-dashboard.json` or paste its contents
3. Select your Prometheus datasource
4. Click Import

### Option B: Grafana Sidecar (Helm)

If your Grafana uses the sidecar for auto-provisioning dashboards (common with kube-prometheus-stack), create a ConfigMap:

```bash
kubectl create configmap infercost-dashboard \
  --from-file=infercost-dashboard.json \
  -n monitoring
kubectl label configmap infercost-dashboard grafana_dashboard=1 -n monitoring
```

### Option C: Grafana API

```bash
curl -X POST \
  -H "Content-Type: application/json" \
  -u "admin:PASSWORD" \
  "http://GRAFANA_HOST/api/dashboards/db" \
  -d "{\"dashboard\": $(cat infercost-dashboard.json), \"overwrite\": true}"
```

## Panels

| Section | Panels |
|---------|--------|
| **Overview** | Hourly Cost, Monthly Projected, Amortization/hr, Electricity/hr, GPU Power Draw, Cost/MTok |
| **Cloud Comparison** | Cloud Equivalent Cost (bar chart), Savings vs Cloud (bar gauge with negative values) |
| **GPU & Power** | GPU Power Over Time, Hourly Cost Breakdown (stacked), Token Throughput |
| **Token Counters** | Cumulative Tokens by Model, Cost Per Million Tokens Over Time |

## Requirements

- Prometheus scraping the InferCost controller's `/metrics` endpoint
- InferCost controller running with at least one CostProfile applied
