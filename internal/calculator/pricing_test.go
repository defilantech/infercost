/*
Copyright 2026.
*/

package calculator

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestEmbeddedPricingMatchesCanonical guards against drift between the
// user-visible config/pricing/cloud-pricing.yaml (which the Helm chart mounts
// as a ConfigMap and which the docs tell operators to edit) and the embedded
// copy inside this package (which the Go binary actually reads by default).
// If they diverge, cost comparisons in the controller silently disagree with
// what users see on disk — exactly the failure mode we cannot allow on a
// product whose value proposition is "honest cost numbers."
func TestEmbeddedPricingMatchesCanonical(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	canonical := filepath.Join(wd, "..", "..", "config", "pricing", "cloud-pricing.yaml")
	canonicalBytes, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatalf("reading canonical pricing file %s: %v", canonical, err)
	}
	if !bytes.Equal(canonicalBytes, pricingYAML) {
		t.Fatalf("embedded pricing catalog (internal/calculator/bundled-cloud-pricing.yaml) has drifted from %s. "+
			"The canonical file is config/pricing/cloud-pricing.yaml. Run: cp config/pricing/cloud-pricing.yaml internal/calculator/bundled-cloud-pricing.yaml",
			canonical)
	}
}

func TestDefaultCloudPricingCatalog_LoadsEmbedded(t *testing.T) {
	c, err := DefaultCloudPricingCatalog()
	if err != nil {
		t.Fatalf("DefaultCloudPricingCatalog error: %v", err)
	}
	if c.LastVerified == "" {
		t.Error("lastVerified is empty")
	}
	if len(c.Sources) == 0 {
		t.Error("sources map is empty")
	}
	if len(c.Providers) == 0 {
		t.Error("no providers loaded")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("embedded catalog fails validation: %v", err)
	}
}

func TestDefaultCloudPricing_ProducesEntriesForAllThreeProviders(t *testing.T) {
	entries := DefaultCloudPricing()
	seen := make(map[string]bool)
	for _, e := range entries {
		seen[e.Provider] = true
	}
	for _, want := range []string{"OpenAI", "Anthropic", "Google"} {
		if !seen[want] {
			t.Errorf("default pricing missing provider %q", want)
		}
	}
}

func TestLoadCloudPricingFile_Roundtrip(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	canonical := filepath.Join(wd, "..", "..", "config", "pricing", "cloud-pricing.yaml")
	c, err := LoadCloudPricingFile(canonical)
	if err != nil {
		t.Fatalf("LoadCloudPricingFile: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("canonical file fails validation: %v", err)
	}
	if len(c.Flatten()) == 0 {
		t.Error("flatten produced no entries")
	}
}

func TestLoadCloudPricingFile_MissingFile(t *testing.T) {
	_, err := LoadCloudPricingFile("/nonexistent/path/pricing.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadCloudPricingFile_Malformed(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(tmp, []byte("::::not:valid:yaml::::"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCloudPricingFile(tmp)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestValidate_CatchesDuplicates(t *testing.T) {
	c := PricingCatalog{
		Providers: []providerWithPrice{
			{Provider: "OpenAI", Models: []modelPricing{
				{Model: "gpt-x", InputPerMillion: 1, OutputPerMillion: 2},
				{Model: "gpt-x", InputPerMillion: 1, OutputPerMillion: 2},
			}},
		},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestValidate_CatchesNegativePricing(t *testing.T) {
	c := PricingCatalog{
		Providers: []providerWithPrice{
			{Provider: "OpenAI", Models: []modelPricing{
				{Model: "gpt-x", InputPerMillion: -1, OutputPerMillion: 2},
			}},
		},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected negative pricing error")
	}
}

func TestSetOverrideCloudPricing(t *testing.T) {
	t.Cleanup(func() { SetOverrideCloudPricing(nil) })

	override := &PricingCatalog{
		Providers: []providerWithPrice{
			{Provider: "LocalLab", Models: []modelPricing{
				{Model: "sim-model", InputPerMillion: 0.01, OutputPerMillion: 0.02},
			}},
		},
	}
	SetOverrideCloudPricing(override)

	got := DefaultCloudPricing()
	if len(got) != 1 || got[0].Provider != "LocalLab" {
		t.Errorf("override not honored, got %+v", got)
	}

	SetOverrideCloudPricing(nil)
	got = DefaultCloudPricing()
	if len(got) == 0 {
		t.Error("clearing override should return embedded default")
	}
	for _, e := range got {
		if e.Provider == "LocalLab" {
			t.Error("override still present after clear")
		}
	}
}
