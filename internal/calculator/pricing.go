/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
*/

package calculator

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"sync"

	"sigs.k8s.io/yaml"
)

// pricingYAML is the canonical cloud pricing catalog, embedded at build time
// from config/pricing/cloud-pricing.yaml so DefaultCloudPricing never has to
// ship hand-duplicated numbers. Operators can override at runtime by passing
// --pricing-file or by mounting a ConfigMap at the same path.
//
//go:embed bundled-cloud-pricing.yaml
var pricingYAML []byte

// PricingCatalog is the parsed form of cloud-pricing.yaml. It carries the
// metadata header (lastVerified, per-provider sources) alongside the model
// entries so any UI or export can cite when the numbers were checked.
type PricingCatalog struct {
	LastVerified string              `json:"lastVerified,omitempty" yaml:"lastVerified,omitempty"`
	Sources      map[string]string   `json:"sources,omitempty" yaml:"sources,omitempty"`
	Providers    []providerWithPrice `json:"providers,omitempty" yaml:"providers,omitempty"`
}

type providerWithPrice struct {
	Provider string         `json:"provider" yaml:"provider"`
	Models   []modelPricing `json:"models" yaml:"models"`
}

type modelPricing struct {
	Model            string  `json:"model" yaml:"model"`
	Tier             string  `json:"tier,omitempty" yaml:"tier,omitempty"`
	InputPerMillion  float64 `json:"inputPerMillion" yaml:"inputPerMillion"`
	OutputPerMillion float64 `json:"outputPerMillion" yaml:"outputPerMillion"`
	Notes            string  `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// Flatten converts the nested provider/model tree into the flat CloudPricing
// slice that CompareToCloud and the existing call sites already consume. The
// nested shape is friendlier to humans editing the YAML; the flat shape is
// friendlier to iteration.
func (c *PricingCatalog) Flatten() []CloudPricing {
	total := 0
	for _, p := range c.Providers {
		total += len(p.Models)
	}
	out := make([]CloudPricing, 0, total)
	for _, p := range c.Providers {
		for _, m := range p.Models {
			out = append(out, CloudPricing{
				Provider:         p.Provider,
				Model:            m.Model,
				InputPerMillion:  m.InputPerMillion,
				OutputPerMillion: m.OutputPerMillion,
			})
		}
	}
	return out
}

var (
	defaultCatalogOnce sync.Once
	defaultCatalog     PricingCatalog
	defaultCatalogErr  error

	overrideMu      sync.RWMutex
	overrideCatalog *PricingCatalog
)

// DefaultCloudPricingCatalog returns the embedded pricing catalog metadata +
// models. Parsed once and cached. Returns an error only if the embedded YAML
// itself is malformed, which is a build-time bug.
func DefaultCloudPricingCatalog() (PricingCatalog, error) {
	defaultCatalogOnce.Do(func() {
		defaultCatalogErr = yaml.Unmarshal(pricingYAML, &defaultCatalog)
	})
	return defaultCatalog, defaultCatalogErr
}

// SetOverrideCloudPricing installs a parsed catalog as the active pricing
// source. Call once at startup from main when --pricing-file is set. Passing
// nil clears the override and falls back to the embedded default.
func SetOverrideCloudPricing(c *PricingCatalog) {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	overrideCatalog = c
}

// LoadCloudPricingFile reads a pricing catalog from disk. It expects the same
// YAML shape as the embedded default so a ConfigMap drop-in is a copy, not a
// schema translation.
func LoadCloudPricingFile(path string) (PricingCatalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PricingCatalog{}, fmt.Errorf("reading pricing file: %w", err)
	}
	var c PricingCatalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return PricingCatalog{}, fmt.Errorf("parsing pricing file %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return PricingCatalog{}, fmt.Errorf("validating pricing file %s: %w", path, err)
	}
	return c, nil
}

// Validate checks a catalog for structural problems that would silently
// produce wrong cost comparisons. Cheap to run at load time.
func (c *PricingCatalog) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("catalog has no providers")
	}
	seen := make(map[string]bool)
	for _, p := range c.Providers {
		if p.Provider == "" {
			return fmt.Errorf("provider with empty name")
		}
		for _, m := range p.Models {
			if m.Model == "" {
				return fmt.Errorf("%s has a model with empty name", p.Provider)
			}
			key := strings.ToLower(p.Provider + "/" + m.Model)
			if seen[key] {
				return fmt.Errorf("duplicate entry %s/%s", p.Provider, m.Model)
			}
			seen[key] = true
			if m.InputPerMillion < 0 || m.OutputPerMillion < 0 {
				return fmt.Errorf("%s/%s has negative pricing", p.Provider, m.Model)
			}
		}
	}
	return nil
}

// DefaultCloudPricing returns the flat CloudPricing slice used by
// CompareToCloud. It prefers the operator override (set via
// SetOverrideCloudPricing) and falls back to the embedded catalog.
//
// The embedded catalog is the single source of truth — numbers are maintained
// in config/pricing/cloud-pricing.yaml and loaded into the binary at build.
// When the embedded YAML is malformed the function panics, which surfaces the
// mistake during CI rather than silently returning zero pricing.
func DefaultCloudPricing() []CloudPricing {
	overrideMu.RLock()
	override := overrideCatalog
	overrideMu.RUnlock()
	if override != nil {
		return override.Flatten()
	}
	c, err := DefaultCloudPricingCatalog()
	if err != nil {
		panic(fmt.Sprintf("embedded cloud pricing catalog is malformed: %v", err))
	}
	return c.Flatten()
}
