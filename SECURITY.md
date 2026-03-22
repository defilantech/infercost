# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability in InferCost, please report it responsibly.

### How to Report

1. **Do NOT open a public GitHub issue** for security vulnerabilities
2. Email security concerns to: **contact@defilan.com**
3. Use GitHub's [private vulnerability reporting](https://github.com/defilantech/infercost/security/advisories/new)

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- **Acknowledgment**: Within 48 hours
- **Initial Assessment**: Within 7 days
- **Resolution Target**: Within 30 days for critical issues

### Scope

This security policy applies to:
- InferCost controller
- CLI (`infercost`)
- REST API
- Container images published to GHCR

### Out of Scope

- Third-party dependencies (report to upstream)
- DCGM Exporter vulnerabilities (report to NVIDIA)
- llama.cpp / vLLM vulnerabilities (report to upstream)
- LLMKube vulnerabilities (report to [LLMKube](https://github.com/defilantech/llmkube/security))

## Security Best Practices

When deploying InferCost:

1. **ClusterIP only**: The API server binds to ClusterIP by default — do not expose externally without authentication
2. **Read-only API**: The REST API is read-only by design — no mutation endpoints exist
3. **RBAC**: Restrict who can create/modify CostProfile CRDs
4. **Network Policies**: Isolate the InferCost controller pod
5. **TLS**: Enable TLS for the metrics and API endpoints in production
6. **CostProfile data**: CostProfiles contain financial data (hardware prices, electricity rates) — treat as business-sensitive
