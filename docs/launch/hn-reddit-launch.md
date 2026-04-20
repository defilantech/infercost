# Hacker News + Reddit launch runbook

**Status**: Prepared, not executed. Do not post until both the CNCF
Landscape and OpenCost runbooks are complete.

## Order of posts (72-hour window)

1. **Day 1, Tuesday 9:00 AM ET** — Hacker News "Show HN"
2. **Day 1, Tuesday 11:00 AM ET** — Reddit r/kubernetes
3. **Day 2, Wednesday 9:00 AM ET** — Reddit r/LocalLLaMA

Never post the same day to multiple venues. Cross-promotion reads as spam.
Stagger, and reply to every comment.

## Show HN

**Title**: `Show HN: InferCost – True cost-per-token for on-prem AI inference`

Hacker News titles: 80 chars max, no emoji, no exclamation points, no
marketing superlatives ("great", "revolutionary", "best"). The `Show HN:`
prefix is a specific convention — it signals "I built this and you can
use/run it now."

**URL**: https://infercost.ai (not the GitHub repo — the HN community
penalizes repo-link submissions)

**First comment** (post immediately after submitting):

```
Maintainer here. Background: we built this because the cloud-API FinOps
tools treat on-prem GPU workloads as $0 cost, and OpenCost/Kubecost track
GPU-hours but not tokens. Neither answered "what does it actually cost to
run our model?"

InferCost installs via Helm, reads DCGM for real-time GPU power, reads
llama.cpp or vLLM /metrics for token counts, computes
(amortization + electricity * power * PUE) / tokens, and writes the result
to Prometheus + a CRD you can query with kubectl.

The cloud comparison is honest — when cloud is cheaper at your
utilization, the tool says so. No rigged numbers.

Repo: https://github.com/defilantech/infercost
Docs: https://infercost.ai/docs

Happy to answer anything — about the math, the schema, or why we went
CRDs instead of a SaaS dashboard.
```

## r/kubernetes

**Title**: `InferCost – a Kubernetes operator that computes true cost-per-token for on-prem GPU inference workloads`

Subreddit rules require the post be self-contained and not just a
repo-dump. The body below is the full content — no external link-only
post.

```
I built a Kubernetes operator that answers a question I kept hitting at
work: what is our actual per-token cost when we run Llama or Qwen on our
own GPUs? The standard FinOps tools (Kubecost, OpenCost) track
GPU-hours but don't understand tokens. The AI observability tools
(Langfuse, LiteLLM) track tokens but set on-prem infra cost to zero.

InferCost combines them. Two CRDs (`CostProfile` declaring your hardware
economics, `UsageReport` auto-computed), a controller pod, and a
pre-built Grafana dashboard.

The math:
  cost_per_token = (GPU_amortization + electricity * power_draw * PUE)
                   / tokens_per_hour

Power draw comes from DCGM (real-time), tokens come from
llama.cpp/vLLM /metrics. Cloud comparison uses a versioned YAML catalog
of list prices you can override via ConfigMap for negotiated rates.

Repo: https://github.com/defilantech/infercost
Helm: `helm repo add infercost https://defilantech.github.io/infercost`
Docs: https://infercost.ai/docs

Apache 2.0, solo maintainer so far, using it myself on a 2x RTX 5060 Ti
lab cluster. Feedback welcome — especially from anyone running vLLM in
a multi-tenant setup.
```

## r/LocalLLaMA

**Title**: `I tracked the actual cost of running Qwen3 on my home lab vs paying Claude/GPT API – here's the receipt`

LocalLLaMA loves receipts and specific numbers. The title is deliberately
personal (my lab, my numbers) — the "corporate pitch" vibe flops there.

```
Ran Qwen3-Coder-30B on my 2x RTX 5060 Ti setup for a week, ran the same
prompts through my InferCost controller, and compared against what
Anthropic + OpenAI would have charged. Screenshots of the Grafana
dashboard in the post.

Hardware amortization: $960 / 4 years / 2920 nights = $0.08/night
Electricity: 260W sustained @ $0.08/kWh * 8hr/night = $0.17/night
Total: ~$0.25 to generate 1.8M tokens overnight.

Claude Sonnet 4.6 for the same tokens: $31.50.
GPT-5.4: $24.00.

That's the raw number. It does NOT mean "local always wins" — at low
utilization the API wins and InferCost's honest comparison will show
that. But for batch/overnight agentic workloads, the crossover point is
pretty low.

The tool:
- GitHub: https://github.com/defilantech/infercost
- Apache 2.0, runs on any K8s cluster with DCGM installed
- Scrapes llama.cpp or vLLM /metrics
- Ships a Grafana dashboard and a FOCUS-compatible CSV export

Not trying to sell anything. Happy to share the raw CSV if someone
wants to sanity-check my math.
```

## Common pitfalls

- **Do not reply defensively.** HN and r/kubernetes both have skeptical
  cultures. "That's a good point — here is the math" beats "actually
  you're wrong" every time.
- **Do not rate-limit yourself.** Reply to every serious comment within
  2 hours for the first 12 hours, then within 24 hours. Dead threads
  sink.
- **Do not edit the post after the first hour.** On HN especially, edits
  reset the age and signal desperation.
- **Do not post to r/selfhosted, r/homelab, r/devops in the same week.**
  Cross-posting to adjacent subs looks like spam even when the content
  is good. Pick the two or three best venues and commit.

## Metrics to watch

- HN: front page = >30 upvotes in first 90 min. If it's <10 at 2 hours,
  the post is dead — do not spam-refresh. Move on.
- r/kubernetes: 50 upvotes / 20 comments in 24h is a solid post.
- r/LocalLLaMA: screenshots and cost receipts get disproportionately
  strong engagement here. Target 100+ upvotes.
- GitHub stars velocity for 7 days after each post. Expect 10-30 stars
  from HN on a middle-tier day, 5-15 from each Reddit post.

## Recovery plan if one post flops

Sometimes HN just misses you on the day. That is not fatal. If the HN
post gets <5 upvotes after 4 hours, do not repost — Show HN is one-shot.
Instead lean harder into Reddit and LinkedIn, and try HN again in 6+
months with a major release.
