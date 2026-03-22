# Contributing to InferCost

Thank you for your interest in contributing to InferCost! This project makes on-premises AI inference costs visible, attributable, and actionable.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [How Can I Contribute?](#how-can-i-contribute)
- [Development Setup](#development-setup)
- [Making Changes](#making-changes)
- [Pull Request Process](#pull-request-process)
- [Coding Standards](#coding-standards)
- [Community](#community)

## Code of Conduct

Please read our [Code of Conduct](CODE_OF_CONDUCT.md). We are committed to providing a welcoming and inclusive environment for everyone.

## How Can I Contribute?

### Reporting Bugs

Before creating bug reports, please check existing issues.

**Good bug reports include:**
- Clear, descriptive title
- Steps to reproduce the problem
- Expected vs actual behavior
- InferCost version, Kubernetes version, GPU type
- CostProfile YAML and controller logs

**Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md)** when creating issues.

### Suggesting Features

We track feature requests via GitHub Issues with the `enhancement` label.

**Good feature requests include:**
- Clear use case and problem statement
- Proposed solution (if you have one)
- Impact on existing functionality

### First-Time Contributors

Look for issues labeled:
- `good-first-issue` — Small, well-defined tasks
- `help-wanted` — Larger tasks where we need help
- `documentation` — Documentation improvements

### Areas We Need Help

**High Priority:**
- Additional GPU CostProfile examples (A100, L40S, RTX 4090, etc.)
- vLLM metrics scraper support
- Helm chart
- Cloud pricing updates and verification

**Medium Priority:**
- Multi-cluster aggregation
- LiteLLM PostgreSQL integration for per-user attribution
- TokenBudget CRD with PrometheusRule generation
- FOCUS spec export format

## Development Setup

### Prerequisites

- **Go 1.26+**: Install from [golang.org](https://golang.org/dl/)
- **Docker**: For building container images
- **kubectl**: Configured with a Kubernetes cluster
- **Kubebuilder**: `brew install kubebuilder` (or [install manually](https://book.kubebuilder.io/quick-start.html))

### Clone and Build

```bash
git clone git@github.com:defilantech/infercost.git
cd infercost

go mod download
make manifests
make build          # Builds controller + CLI
make test           # Run tests
make lint           # Run linter
```

### Running Locally

```bash
# Install CRDs into your cluster
make install

# Run the controller locally
go run ./cmd/main.go \
  --metrics-bind-address=:8090 \
  --metrics-secure=false \
  --health-probe-bind-address=:8091 \
  --dcgm-endpoint=http://<dcgm-exporter>:9400/metrics

# Apply a CostProfile
kubectl apply -f config/samples/finops_v1alpha1_costprofile.yaml

# Check results
kubectl get costprofiles
```

## Making Changes

### Branching Strategy

- `main` — Stable, production-ready code
- `feat/*` — New features
- `fix/*` — Bug fixes
- `docs/*` — Documentation changes

### Commit Messages

We use descriptive commit messages with conventional prefixes for Release Please:

| Prefix | When to use | Version Bump |
|--------|------------|--------------|
| `feat:` | New features, CRD fields | Minor (0.x.0) |
| `fix:` | Bug fixes | Patch (0.0.x) |
| `docs:` | Documentation only | Patch |
| `chore:` | CI, deps, tooling | None |
| `test:` | Test-only changes | None |

**All commits must be signed off** (`git commit -s`) per the [Developer Certificate of Origin](https://developercertificate.org/).

### Testing

```bash
make test            # Unit tests (envtest)
make lint            # golangci-lint
make manifests       # Verify CRDs are up to date
git diff --exit-code # Should show no changes
```

**Writing tests:**
- Table-driven tests for multiple cases
- Test both success and error paths
- Use httptest for HTTP-dependent tests (scraper, API)
- Test CRD validation
- Verify Prometheus metrics are correctly set

## Pull Request Process

### Before Submitting

- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] All commits are signed off (`git commit -s`)
- [ ] Documentation updated (if user-facing change)
- [ ] Branch is up-to-date with `main`

### PR Title Format

PR titles drive changelog generation via Release Please:

```
feat: Add vLLM metrics scraper support
fix: Correct cloud comparison when token count is zero
docs: Add H100 CostProfile example
```

### Review Process

1. Automated checks run (tests, lint, DCO)
2. Maintainer review (usually within 2-3 days)
3. Address feedback by pushing new commits
4. Approval and squash-merge to `main`

## Coding Standards

### Go Code

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` and `golangci-lint`
- Keep functions focused and testable
- Add comments for exported types/functions

### CRD Design

- Follow [Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- Use `+kubebuilder` markers for validation
- Provide meaningful status conditions
- Add examples in `config/samples/`

### Cost Calculations

- Document the formula being implemented
- Include units in variable names (e.g., `powerDrawWatts`, `costPerHourUSD`)
- Use float64 for all monetary values
- Always show your math in code comments for non-obvious calculations

## Community

- **GitHub Issues**: Bug reports, feature requests
- **GitHub Discussions**: Q&A, ideas, general discussion

## License

By contributing to InferCost, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).

---

**Thank you for contributing to InferCost!** Every PR, issue report, and doc improvement helps make AI inference cost tracking accessible to everyone.
