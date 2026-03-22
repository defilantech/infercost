---
name: Bug Report
about: Report a bug or unexpected behavior
title: '[BUG] '
labels: bug
assignees: ''
---

## Bug Description

A clear and concise description of what the bug is.

## Steps to Reproduce

1. Apply CostProfile '...'
2. Run command '...'
3. Observe error '...'

## Expected Behavior

What you expected to happen.

## Actual Behavior

What actually happened.

## Environment

**InferCost Version:**
```bash
infercost version
# Output:
```

**Kubernetes Version:**
```bash
kubectl version
# Output:
```

**GPU Type:**
- [ ] NVIDIA H100
- [ ] NVIDIA A100
- [ ] NVIDIA L40S
- [ ] NVIDIA RTX 4090/5060 Ti
- [ ] Other:

**Inference Engine:**
- [ ] llama.cpp
- [ ] vLLM
- [ ] Other:

## Logs

**Controller Logs:**
```bash
kubectl logs -n infercost-system deployment/infercost-controller-manager --tail=50
# Paste logs here
```

## YAML Manifests

**CostProfile:**
```yaml
# Paste your CostProfile YAML here
```

## Additional Context

Add any other context about the problem here.
