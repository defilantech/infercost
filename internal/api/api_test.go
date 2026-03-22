package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStore_CostData(t *testing.T) {
	s := NewStore()

	if got := s.GetCostData(); got != nil {
		t.Errorf("expected nil cost data on new store, got %+v", got)
	}

	data := CostData{
		ProfileName:    "test-profile",
		GPUModel:       "RTX 5060 Ti",
		GPUCount:       2,
		HourlyCostUSD:  0.05,
		PowerDrawWatts: 100,
		LastUpdated:    time.Now(),
	}
	s.SetCostData(data)

	got := s.GetCostData()
	if got == nil {
		t.Fatal("expected cost data, got nil")
	}
	if got.ProfileName != "test-profile" {
		t.Errorf("ProfileName = %q, want %q", got.ProfileName, "test-profile")
	}
	if got.GPUCount != 2 {
		t.Errorf("GPUCount = %d, want 2", got.GPUCount)
	}
}

func TestStore_Models(t *testing.T) {
	s := NewStore()

	if got := s.GetModels(); len(got) != 0 {
		t.Errorf("expected empty models on new store, got %d", len(got))
	}

	models := []ModelData{
		{Model: "qwen3-32b", Namespace: "default", InputTokens: 100000, OutputTokens: 200000},
		{Model: "nomic-embed", Namespace: "default", InputTokens: 0, OutputTokens: 0},
	}
	s.SetModels(models)

	got := s.GetModels()
	if len(got) != 2 {
		t.Fatalf("expected 2 models, got %d", len(got))
	}
	if got[0].Model != "qwen3-32b" {
		t.Errorf("first model = %q, want %q", got[0].Model, "qwen3-32b")
	}

	// Verify returned slice is a copy (not aliased).
	got[0].Model = "modified"
	original := s.GetModels()
	if original[0].Model == "modified" {
		t.Error("GetModels returned aliased slice, not a copy")
	}
}

func TestStore_Comparisons(t *testing.T) {
	s := NewStore()

	comparisons := BuildComparisons(100000, 200000, 0.50)
	if len(comparisons) == 0 {
		t.Fatal("BuildComparisons returned empty slice")
	}

	s.SetComparisons(comparisons)
	got := s.GetComparisons()
	if len(got) != len(comparisons) {
		t.Errorf("expected %d comparisons, got %d", len(comparisons), len(got))
	}

	// Verify each comparison has required fields.
	for _, c := range got {
		if c.Provider == "" {
			t.Error("comparison has empty provider")
		}
		if c.Model == "" {
			t.Error("comparison has empty model")
		}
		if c.InputPerMTok <= 0 {
			t.Errorf("%s %s has non-positive InputPerMTok: %v", c.Provider, c.Model, c.InputPerMTok)
		}
	}
}

func TestServer_Healthz(t *testing.T) {
	store := NewStore()
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("healthz status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("healthz status = %q, want %q", body["status"], "ok")
	}
}

func TestServer_CostsCurrent_NoData(t *testing.T) {
	store := NewStore()
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/costs/current", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("costs/current with no data status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestServer_CostsCurrent_WithData(t *testing.T) {
	store := NewStore()
	store.SetCostData(CostData{
		ProfileName:    "test",
		GPUModel:       "RTX 5060 Ti",
		GPUCount:       2,
		HourlyCostUSD:  0.05,
		PowerDrawWatts: 85.5,
	})
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/costs/current", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body CostData
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if body.GPUModel != "RTX 5060 Ti" {
		t.Errorf("GPUModel = %q, want %q", body.GPUModel, "RTX 5060 Ti")
	}
}

func TestServer_Models(t *testing.T) {
	store := NewStore()
	store.SetModels([]ModelData{
		{Model: "qwen3-32b", Namespace: "default"},
	})
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/models", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if count, ok := body["count"].(float64); !ok || int(count) != 1 {
		t.Errorf("count = %v, want 1", body["count"])
	}
}

func TestServer_Compare(t *testing.T) {
	store := NewStore()
	store.SetComparisons(BuildComparisons(100000, 200000, 0.50))
	store.SetCostData(CostData{HourlyCostUSD: 0.05, UptimeHours: 10})
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/compare", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if body["disclaimer"] == nil {
		t.Error("compare response missing disclaimer")
	}
	if body["pricingSources"] == nil {
		t.Error("compare response missing pricingSources")
	}
	if body["lastVerified"] == nil {
		t.Error("compare response missing lastVerified")
	}
}

func TestServer_Status(t *testing.T) {
	store := NewStore()
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if _, ok := body["infrastructure"]; !ok {
		t.Error("status response missing infrastructure key")
	}
	if _, ok := body["models"]; !ok {
		t.Error("status response missing models key")
	}
	if _, ok := body["comparisons"]; !ok {
		t.Error("status response missing comparisons key")
	}
}
