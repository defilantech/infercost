/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	finopsv1alpha1 "github.com/defilantech/infercost/api/v1alpha1"
	"github.com/defilantech/infercost/internal/scraper"
)

// appleProfile mirrors testProfile but for an Apple Silicon host. The
// gpuModel is what looksApple matches on, so this is the discriminator
// the dispatcher uses to pick Metal over DCGM.
func appleProfile(tdp *int32) finopsv1alpha1.CostProfile {
	return finopsv1alpha1.CostProfile{
		Spec: finopsv1alpha1.CostProfileSpec{
			Hardware: finopsv1alpha1.HardwareSpec{
				GPUModel: "Apple M5 Max",
				GPUCount: 1,
				TDPWatts: tdp,
			},
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": "m5-max-laptop",
			},
		},
	}
}

const metalAgentMetricsHealthy = `# HELP llmkube_metal_agent_apple_power_combined_watts Combined CPU + GPU + ANE package power in watts from macOS powermetrics.
# TYPE llmkube_metal_agent_apple_power_combined_watts gauge
llmkube_metal_agent_apple_power_combined_watts 42.5
# HELP llmkube_metal_agent_apple_power_gpu_watts GPU subsystem power.
# TYPE llmkube_metal_agent_apple_power_gpu_watts gauge
llmkube_metal_agent_apple_power_gpu_watts 31.2
# HELP llmkube_metal_agent_apple_power_cpu_watts CPU subsystem power.
# TYPE llmkube_metal_agent_apple_power_cpu_watts gauge
llmkube_metal_agent_apple_power_cpu_watts 11.3
# HELP llmkube_metal_agent_apple_power_ane_watts ANE power.
# TYPE llmkube_metal_agent_apple_power_ane_watts gauge
llmkube_metal_agent_apple_power_ane_watts 0
`

const metalAgentMetricsSamplerOff = `# HELP llmkube_metal_agent_apple_power_combined_watts Combined CPU + GPU + ANE package power in watts.
# TYPE llmkube_metal_agent_apple_power_combined_watts gauge
llmkube_metal_agent_apple_power_combined_watts 0
`

func TestReadMetalPower_EndpointNotConfigured(t *testing.T) {
	tdp := int32(90)
	r := &CostProfileReconciler{MetalEndpoint: "", ScrapeClient: scraper.NewClient(2 * time.Second)}

	power, cond := r.readMetalPower(context.Background(), appleProfile(&tdp))

	if want := float64(90); power != want {
		t.Errorf("power = %.0f, want %.0f (TDP fallback)", power, want)
	}
	if cond.Status != metav1.ConditionFalse || cond.Reason != ReasonMetalNotConfigured {
		t.Errorf("condition = %s/%s, want False/MetalNotConfigured", cond.Status, cond.Reason)
	}
	if !strings.Contains(cond.Message, "--apple-power-enabled") {
		t.Errorf("message should point at the agent flag: %q", cond.Message)
	}
}

func TestReadMetalPower_EndpointUnreachable(t *testing.T) {
	tdp := int32(90)
	r := &CostProfileReconciler{
		MetalEndpoint: "http://127.0.0.1:1/metrics",
		ScrapeClient:  scraper.NewClient(500 * time.Millisecond),
	}

	power, cond := r.readMetalPower(context.Background(), appleProfile(&tdp))

	if want := float64(90); power != want {
		t.Errorf("power = %.0f, want %.0f (TDP fallback)", power, want)
	}
	if cond.Status != metav1.ConditionFalse || cond.Reason != ReasonMetalScrapeError {
		t.Errorf("condition = %s/%s, want False/MetalScrapeError", cond.Status, cond.Reason)
	}
}

func TestReadMetalPower_SamplerOff(t *testing.T) {
	// Agent reachable but powermetrics sampler disabled — operator forgot
	// --apple-power-enabled or sudoers entry. The condition reason must
	// distinguish this from a network error so docs can guide the fix.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metalAgentMetricsSamplerOff))
	}))
	defer server.Close()

	tdp := int32(90)
	r := &CostProfileReconciler{MetalEndpoint: server.URL, ScrapeClient: scraper.NewClient(2 * time.Second)}

	power, cond := r.readMetalPower(context.Background(), appleProfile(&tdp))

	if want := float64(90); power != want {
		t.Errorf("power = %.0f, want %.0f (TDP fallback when sampler off)", power, want)
	}
	if cond.Status != metav1.ConditionUnknown || cond.Reason != ReasonMetalSamplerOff {
		t.Errorf("condition = %s/%s, want Unknown/MetalSamplerOff", cond.Status, cond.Reason)
	}
	if !strings.Contains(cond.Message, "sudoers") {
		t.Errorf("message should mention sudoers as likely cause: %q", cond.Message)
	}
}

func TestReadMetalPower_HealthyReadings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metalAgentMetricsHealthy))
	}))
	defer server.Close()

	tdp := int32(90)
	r := &CostProfileReconciler{MetalEndpoint: server.URL, ScrapeClient: scraper.NewClient(2 * time.Second)}

	power, cond := r.readMetalPower(context.Background(), appleProfile(&tdp))

	if power != 42.5 {
		t.Errorf("power = %.1f, want 42.5 (combined gauge)", power)
	}
	if cond.Status != metav1.ConditionTrue || cond.Reason != ReasonMetalHealthy {
		t.Errorf("condition = %s/%s, want True/MetalHealthy", cond.Status, cond.Reason)
	}
	// The status message should expose the per-component breakdown so
	// `kubectl describe costprofile` is useful for debugging dashboards.
	for _, want := range []string{"42.5", "gpu=31.2", "cpu=11.3", "ane=0.0"} {
		if !strings.Contains(cond.Message, want) {
			t.Errorf("message missing %q: %q", want, cond.Message)
		}
	}
}

// readPower is the dispatcher. These cover the routing decisions; the actual
// scrape paths are covered above and in costprofile_dcgm_test.go.

func TestReadPower_AppleProfileWithMetalEndpointUsesMetal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metalAgentMetricsHealthy))
	}))
	defer server.Close()

	tdp := int32(90)
	r := &CostProfileReconciler{
		MetalEndpoint: server.URL,
		// DCGM endpoint is also set; dispatcher must not pick it for an Apple profile.
		DCGMEndpoint: "http://should-not-be-called.invalid",
		ScrapeClient: scraper.NewClient(2 * time.Second),
	}

	_, cond := r.readPower(context.Background(), appleProfile(&tdp))
	if cond.Type != ConditionMetalReachable {
		t.Errorf("expected MetalReachable condition for Apple profile, got %q", cond.Type)
	}
}

func TestReadPower_NVIDIAProfileWithBothEndpointsUsesDCGM(t *testing.T) {
	// NVIDIA profile + both endpoints set: dispatcher must pick DCGM. We
	// verify by checking the condition type — readDCGMPower will return a
	// scrape-error condition because the endpoint is bogus, but the *type*
	// proves which path was taken.
	tdp := int32(180)
	r := &CostProfileReconciler{
		MetalEndpoint: "http://should-not-be-called.invalid",
		DCGMEndpoint:  "http://127.0.0.1:1/metrics",
		ScrapeClient:  scraper.NewClient(500 * time.Millisecond),
	}

	_, cond := r.readPower(context.Background(), testProfile(&tdp))
	if cond.Type != ConditionDCGMReachable {
		t.Errorf("expected DCGMReachable condition for NVIDIA profile, got %q", cond.Type)
	}
}

func TestReadPower_AppleProfileWithoutMetalEndpointFallsThroughToDCGM(t *testing.T) {
	// Apple profile but operator hasn't set --metal-endpoint. The dispatcher
	// falls through to readDCGMPower, which itself does TDP fallback when
	// DCGM is also unset. This isn't ideal UX (the operator probably wants
	// MetalNotConfigured guidance, not DCGM guidance) but it's the safe
	// behavior — the alternative would be silently returning zero power.
	// Documented in the apple-m2-ultra.yaml sample.
	tdp := int32(90)
	r := &CostProfileReconciler{ScrapeClient: scraper.NewClient(2 * time.Second)}

	_, cond := r.readPower(context.Background(), appleProfile(&tdp))
	if cond.Type != ConditionDCGMReachable {
		t.Errorf("expected DCGMReachable (fallback path) when neither endpoint is set, got %q", cond.Type)
	}
}

func TestLooksApple(t *testing.T) {
	cases := map[string]bool{
		"Apple M5 Max":               true,
		"Apple M2 Ultra":             true,
		"apple m3 pro":               true,
		"  Apple M4  ":               true,
		"NVIDIA GeForce RTX 5060 Ti": false,
		"AMD Radeon Pro W7900":       false,
		"Apple":                      false, // no trailing space → no model name
		"":                           false,
		"M5 Max":                     false, // missing vendor prefix
	}
	for in, want := range cases {
		if got := looksApple(in); got != want {
			t.Errorf("looksApple(%q) = %v, want %v", in, got, want)
		}
	}
}
