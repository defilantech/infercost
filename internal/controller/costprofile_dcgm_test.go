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

func testProfile(tdp *int32) finopsv1alpha1.CostProfile {
	return finopsv1alpha1.CostProfile{
		Spec: finopsv1alpha1.CostProfileSpec{
			Hardware: finopsv1alpha1.HardwareSpec{
				GPUModel: "NVIDIA GeForce RTX 5060 Ti",
				GPUCount: 2,
				TDPWatts: tdp,
			},
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": "shadowstack",
			},
		},
	}
}

// readDCGMPower must tell the operator precisely why a number is what it is.
// That's the whole point of this feature: no silent fallbacks, no "why is my
// dashboard flat" mystery. Each test below pins one of the four return paths.

func TestReadDCGMPower_EndpointNotConfigured(t *testing.T) {
	tdp := int32(180)
	r := &CostProfileReconciler{DCGMEndpoint: "", ScrapeClient: scraper.NewClient(2 * time.Second)}

	power, cond := r.readDCGMPower(context.Background(), testProfile(&tdp))

	if want := float64(360); power != want {
		t.Errorf("power = %.0f, want %.0f (2x180 TDP)", power, want)
	}
	if cond.Status != metav1.ConditionFalse || cond.Reason != ReasonDCGMNotConfigured {
		t.Errorf("condition = %s/%s, want False/DCGMNotConfigured", cond.Status, cond.Reason)
	}
	if !strings.Contains(cond.Message, "TDP fallback") || !strings.Contains(cond.Message, "infercost.ai/docs/dcgm") {
		t.Errorf("message missing guidance: %q", cond.Message)
	}
}

func TestReadDCGMPower_EndpointUnreachable(t *testing.T) {
	tdp := int32(180)
	// Point at a port that's not listening. ScrapeDCGM must error quickly.
	r := &CostProfileReconciler{DCGMEndpoint: "http://127.0.0.1:1/metrics", ScrapeClient: scraper.NewClient(500 * time.Millisecond)}

	power, cond := r.readDCGMPower(context.Background(), testProfile(&tdp))

	if want := float64(360); power != want {
		t.Errorf("power = %.0f, want %.0f (TDP fallback)", power, want)
	}
	if cond.Status != metav1.ConditionFalse || cond.Reason != ReasonDCGMScrapeError {
		t.Errorf("condition = %s/%s, want False/DCGMScrapeError", cond.Status, cond.Reason)
	}
	if !strings.Contains(cond.Message, "DCGM scrape failed") {
		t.Errorf("message should say scrape failed: %q", cond.Message)
	}
}

func TestReadDCGMPower_NoReadingsForNode(t *testing.T) {
	// DCGM returns readings, but none match the profile's node selector —
	// the common "nodeSelector typo" failure mode.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`# HELP DCGM_FI_DEV_POWER_USAGE Power draw.
# TYPE DCGM_FI_DEV_POWER_USAGE gauge
DCGM_FI_DEV_POWER_USAGE{Hostname="other-node",gpu="0",UUID="GPU-xxx",modelName="RTX 4090",device="nvidia0"} 230
`))
	}))
	defer server.Close()

	tdp := int32(180)
	r := &CostProfileReconciler{DCGMEndpoint: server.URL, ScrapeClient: scraper.NewClient(2 * time.Second)}

	power, cond := r.readDCGMPower(context.Background(), testProfile(&tdp))

	if want := float64(360); power != want {
		t.Errorf("power = %.0f, want %.0f (TDP fallback when no node match)", power, want)
	}
	if cond.Status != metav1.ConditionUnknown || cond.Reason != ReasonDCGMNoReadings {
		t.Errorf("condition = %s/%s, want Unknown/DCGMNoReadings", cond.Status, cond.Reason)
	}
	if !strings.Contains(cond.Message, "nodeSelector") {
		t.Errorf("message should point at nodeSelector as likely cause: %q", cond.Message)
	}
}

func TestReadDCGMPower_HealthyReadings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`# HELP DCGM_FI_DEV_POWER_USAGE Power draw.
# TYPE DCGM_FI_DEV_POWER_USAGE gauge
DCGM_FI_DEV_POWER_USAGE{Hostname="shadowstack",gpu="0",UUID="GPU-aaa",modelName="RTX 5060 Ti",device="nvidia0"} 140
DCGM_FI_DEV_POWER_USAGE{Hostname="shadowstack",gpu="1",UUID="GPU-bbb",modelName="RTX 5060 Ti",device="nvidia1"} 120
`))
	}))
	defer server.Close()

	tdp := int32(180)
	r := &CostProfileReconciler{DCGMEndpoint: server.URL, ScrapeClient: scraper.NewClient(2 * time.Second)}

	power, cond := r.readDCGMPower(context.Background(), testProfile(&tdp))

	if want := float64(260); power != want {
		t.Errorf("power = %.0f, want %.0f (140+120 real-time)", power, want)
	}
	if cond.Status != metav1.ConditionTrue || cond.Reason != ReasonDCGMHealthy {
		t.Errorf("condition = %s/%s, want True/DCGMHealthy", cond.Status, cond.Reason)
	}
}

func TestReadDCGMPower_NoTDPAndNoDCGM(t *testing.T) {
	// Worst case: no DCGM configured AND no TDP on the spec. Power is zero,
	// but the condition still needs to spell out what's missing.
	r := &CostProfileReconciler{DCGMEndpoint: "", ScrapeClient: scraper.NewClient(2 * time.Second)}
	profile := finopsv1alpha1.CostProfile{
		Spec: finopsv1alpha1.CostProfileSpec{
			Hardware: finopsv1alpha1.HardwareSpec{GPUModel: "?", GPUCount: 1},
		},
	}

	power, cond := r.readDCGMPower(context.Background(), profile)
	if power != 0 {
		t.Errorf("power = %.0f, want 0", power)
	}
	if cond.Status != metav1.ConditionFalse || cond.Reason != ReasonDCGMNotConfigured {
		t.Errorf("condition = %s/%s, want False/DCGMNotConfigured", cond.Status, cond.Reason)
	}
}
