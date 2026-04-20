# CostProfile Sample Library

Ready-to-use CostProfile manifests for the GPUs InferCost users ask about
most often. Each file documents the pricing and amortization assumptions
inline so you can adjust the numbers to your actual purchase.

## How to use

1. Pick the closest-fitting profile for your hardware.
2. Edit `nodeSelector` to match your node (hostname, label, or pool selector).
3. Adjust `purchasePriceUSD` to what you actually paid — list prices age fast.
4. Set `ratePerKWh` to your real electricity cost — US avg is ~$0.12 but
   commercial, industrial, and international rates vary widely.
5. Pick `pueFactor`: `1.0` for homelabs/small racks, `1.2-1.4` for typical
   enterprise data centers, `1.5+` for older facilities.
6. `kubectl apply -f <file>`.

## Files

| File | GPU | Typical use |
|------|-----|-------------|
| [nvidia-h100-sxm5.yaml](nvidia-h100-sxm5.yaml) | H100 SXM5 80GB | Training + large-model inference |
| [nvidia-a100-80gb.yaml](nvidia-a100-80gb.yaml) | A100 80GB | Large-model inference |
| [nvidia-a100-40gb.yaml](nvidia-a100-40gb.yaml) | A100 40GB | Mid-model inference |
| [nvidia-l40s.yaml](nvidia-l40s.yaml) | L40S 48GB | Inference-optimized enterprise |
| [nvidia-a6000.yaml](nvidia-a6000.yaml) | RTX A6000 48GB | Prosumer inference |
| [nvidia-rtx-4090.yaml](nvidia-rtx-4090.yaml) | RTX 4090 24GB | Consumer single-GPU |
| [nvidia-rtx-5090.yaml](nvidia-rtx-5090.yaml) | RTX 5090 32GB | Consumer single-GPU, current gen |
| [nvidia-rtx-5060-ti.yaml](nvidia-rtx-5060-ti.yaml) | RTX 5060 Ti 16GB | Hobbyist / lab single-GPU |
| [apple-m2-ultra.yaml](apple-m2-ultra.yaml) | Apple M2 Ultra (Metal) | Mac Studio homelab |

## Notes on the numbers

- **Prices** are mid-2026 approximate list/street prices for reference.
  Enterprise deals and bulk discounts can shift this dramatically.
- **Amortization** is standardized at 3 years (data-center GPUs) or 4 years
  (consumer/prosumer). This is a convention, not a rule — finance teams
  often use 5 years for tax purposes.
- **Maintenance** is set to 5% (consumer) or 10% (enterprise) of purchase
  price per year. Real numbers depend on support contracts.
- **TDP** values come from the manufacturer spec sheet. Use DCGM readings
  for real-time power; TDP is the fallback when DCGM is unavailable.
