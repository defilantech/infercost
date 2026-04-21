# Changelog

## [0.2.1](https://github.com/defilantech/infercost/compare/v0.2.0...v0.2.1) (2026-04-21)


### Bug Fixes

* bump Chart.yaml to 0.2.0 and wire release-please to maintain it ([#34](https://github.com/defilantech/infercost/issues/34)) ([59e9de0](https://github.com/defilantech/infercost/commit/59e9de091ab718ea7e2e3decb424726f7c0a6a3e))

## [0.2.0](https://github.com/defilantech/infercost/compare/v0.1.0...v0.2.0) (2026-04-21)


### Features

* add team attribution, budget tracking, and alerting (M2) ([#17](https://github.com/defilantech/infercost/issues/17)) ([c475fa9](https://github.com/defilantech/infercost/commit/c475fa970d1b3a9fe65e8ab898a97e63100f9263))
* add vLLM scraper with per-pod backend selection ([#26](https://github.com/defilantech/infercost/issues/26)) ([30be439](https://github.com/defilantech/infercost/commit/30be439e144ae9038373bd4a10f9592a08d04902))
* FOCUS-compatible CSV export for UsageReport data ([#31](https://github.com/defilantech/infercost/issues/31)) ([277423c](https://github.com/defilantech/infercost/commit/277423c922964c425cb1ddce75d32d9a60fa241a))
* ship PodMonitor template for automatic Prometheus discovery ([#24](https://github.com/defilantech/infercost/issues/24)) ([57e8eb8](https://github.com/defilantech/infercost/commit/57e8eb8733dd494549820ce2c1d1582fef46746f)), closes [#18](https://github.com/defilantech/infercost/issues/18)
* surface DCGMReachable status condition on CostProfile ([#28](https://github.com/defilantech/infercost/issues/28)) ([f44b538](https://github.com/defilantech/infercost/commit/f44b5388cd1a4c2a202d6c1052b09d7ce2fcdaf1))
* unify cloud pricing on canonical YAML with refresh workflow ([#29](https://github.com/defilantech/infercost/issues/29)) ([aed344a](https://github.com/defilantech/infercost/commit/aed344afa083fcb9462f385c1b5d683b28353910))


### Bug Fixes

* bust Go Report Card badge cache ([38ee787](https://github.com/defilantech/infercost/commit/38ee7873ec0679910f71fd199964e08e86256a45))
* stop UsageReport reconcile hot-loop on status updates ([#25](https://github.com/defilantech/infercost/issues/25)) ([98ee49e](https://github.com/defilantech/infercost/commit/98ee49e9b21f14d359308913d1671194d57a79d8))


### Documentation

* add CostProfile sample library for common GPUs ([#27](https://github.com/defilantech/infercost/issues/27)) ([8d9418d](https://github.com/defilantech/infercost/commit/8d9418de456696df488740d5545171e3107ad788))
* batch-fill missing godoc comments ([#32](https://github.com/defilantech/infercost/issues/32)) ([b92b57b](https://github.com/defilantech/infercost/commit/b92b57bf5dc51dc8a49a7ff0f0e7e52cf720948c))
* write launch runbooks (CNCF, OpenCost, HN/Reddit, FinOps) — not executed yet ([#30](https://github.com/defilantech/infercost/issues/30)) ([4525822](https://github.com/defilantech/infercost/commit/45258220db37449baaf7962da581f4c80466c2c5))

## [0.1.0](https://github.com/defilantech/infercost/compare/v0.0.1...v0.1.0) (2026-03-22)


### Features

* add Helm chart for InferCost deployment ([3502b88](https://github.com/defilantech/infercost/commit/3502b8807d28c8b72f7b55e611522a7df6d790c4))


### Bug Fixes

* add OCI labels to Dockerfiles for GHCR repo linking ([e78f18d](https://github.com/defilantech/infercost/commit/e78f18d4a106053485794614f009f6d15d10f123))


### Documentation

* add logo to README ([b9b40a8](https://github.com/defilantech/infercost/commit/b9b40a89d7e563a5b301df4fd26c0b92278de1ed))
