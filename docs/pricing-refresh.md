# Refreshing the cloud pricing catalog

InferCost ships with a list-price catalog for OpenAI, Anthropic, and Google
models at `config/pricing/cloud-pricing.yaml`. That file is the single source
of truth. It is embedded into the controller binary at build time and is also
shipped inside the Helm chart so operators can mount it as a ConfigMap and
edit in place.

**When to refresh**: any time a provider changes list pricing. Cloud providers
adjust prices on their own schedule — sometimes quarterly, sometimes more
often. If the `lastVerified` date on the file is older than ~3 months, open a
refresh PR even if nothing has visibly changed (confirming "no change since X"
is still useful).

## Refresh procedure

1. Check the source-of-truth pricing pages listed in the `sources:` block of
   the YAML. These are the authoritative provider docs, not third-party
   aggregators.

2. Edit `config/pricing/cloud-pricing.yaml`:
   - Bump `lastVerified` to today's date (YYYY-MM-DD).
   - Update any changed `inputPerMillion` / `outputPerMillion` values.
   - Add new models to the `providers:` block if a provider shipped something
     new.
   - Remove deprecated / EOL models only when the provider has formally
     retired the API endpoint, not just announced a deprecation.

3. Sync the embedded copy. The controller binary embeds a byte-identical copy
   at `internal/calculator/bundled-cloud-pricing.yaml` so the CLI and the
   controller never disagree with what's on disk. A test enforces this.

   ```bash
   cp config/pricing/cloud-pricing.yaml internal/calculator/bundled-cloud-pricing.yaml
   ```

4. Sync the Helm chart copy. Same reason — the chart ships the file as a
   ConfigMap default, so it must match what's embedded in the image.

   ```bash
   cp config/pricing/cloud-pricing.yaml charts/infercost/pricing/cloud-pricing.yaml
   ```

5. Run the full test suite. `TestEmbeddedPricingMatchesCanonical` fails loud
   if step 3 was skipped; `TestLoadCloudPricingFile_Roundtrip` fails loud if
   the YAML is malformed.

   ```bash
   go test ./... -count=1
   ```

6. Commit with a `chore:` prefix and a diff-summary body:

   ```
   chore: refresh cloud pricing catalog (2026-07-15)

   Sources:
     OpenAI:    gpt-5.5 input $2.25 (was $2.50); output unchanged
     Anthropic: no change since 2026-03-21
     Google:    gemini-2.5-flash-lite input $0.08 (was $0.10)
   ```

## Operator override

Operators with negotiated enterprise rates should not edit the shipped
catalog. Instead:

1. Enable the override in Helm values:

   ```yaml
   pricing:
     override:
       enabled: true
       inline: |
         lastVerified: 2026-07-01
         sources:
           OpenAI: "Negotiated agreement with OpenAI 2026-Q2"
         providers:
           - provider: OpenAI
             models:
               - model: gpt-5.4
                 inputPerMillion: 1.75    # negotiated 30% off list
                 outputPerMillion: 10.50
   ```

2. `helm upgrade infercost infercost/infercost -f values.yaml`.

The controller logs on startup confirm which catalog is in use:

```
INFO  using embedded pricing catalog        lastVerified=2026-03-21 providers=3
INFO  loaded pricing override               lastVerified=2026-07-01 providers=1 path=/etc/infercost/pricing/cloud-pricing.yaml
```

## Schema

```yaml
lastVerified: YYYY-MM-DD       # required
sources:                       # required, at least one entry
  <provider-name>: <url-or-description>
providers:                     # required, non-empty
  - provider: <name>           # required
    models:                    # required, non-empty per provider
      - model: <id>            # required, unique within provider
        tier: flagship|mid|budget   # optional, for grouping in UI
        inputPerMillion: <float>    # required, >= 0
        outputPerMillion: <float>   # required, >= 0
        notes: <string>             # optional
```

`Validate()` rejects: missing providers, duplicate `(provider, model)` pairs,
negative pricing. Malformed overrides cause the controller to fail at startup
rather than silently fall back to list prices.
