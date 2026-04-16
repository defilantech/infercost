package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testModifiedValue = "modified"

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
	got[0].Model = testModifiedValue
	original := s.GetModels()
	if original[0].Model == testModifiedValue {
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

func TestStore_NamespaceCosts(t *testing.T) {
	s := NewStore()

	if got := s.GetNamespaceCosts(); len(got) != 0 {
		t.Errorf("expected empty namespace costs on new store, got %d", len(got))
	}

	costs := []NamespaceCostData{
		{Namespace: "team-a", EstimatedCostUSD: 0.50, TokenCount: 100000, Models: []string{"qwen3-32b"}},
		{Namespace: "team-b", EstimatedCostUSD: 0.30, TokenCount: 60000, Models: []string{"nomic-embed", "qwen3-32b"}},
	}
	s.SetNamespaceCosts(costs)

	got := s.GetNamespaceCosts()
	if len(got) != 2 {
		t.Fatalf("expected 2 namespace costs, got %d", len(got))
	}
	if got[0].Namespace != "team-a" {
		t.Errorf("first namespace = %q, want %q", got[0].Namespace, "team-a")
	}
	if got[0].EstimatedCostUSD != 0.50 {
		t.Errorf("team-a cost = %v, want 0.50", got[0].EstimatedCostUSD)
	}

	// Verify returned slice is a copy (not aliased).
	got[0].Namespace = testModifiedValue
	original := s.GetNamespaceCosts()
	if original[0].Namespace == testModifiedValue {
		t.Error("GetNamespaceCosts returned aliased slice, not a copy")
	}
}

func TestServer_CostsByNamespace_NoData(t *testing.T) {
	store := NewStore()
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/costs/by-namespace", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if count, ok := body["count"].(float64); !ok || int(count) != 0 {
		t.Errorf("count = %v, want 0", body["count"])
	}
}

func TestServer_CostsByNamespace_WithData(t *testing.T) {
	store := NewStore()
	store.SetNamespaceCosts([]NamespaceCostData{
		{Namespace: "team-a", EstimatedCostUSD: 0.50, TokenCount: 100000, Models: []string{"qwen3-32b"}},
		{Namespace: "team-b", EstimatedCostUSD: 0.30, TokenCount: 60000, Models: []string{"nomic-embed"}},
	})
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/costs/by-namespace", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if count, ok := body["count"].(float64); !ok || int(count) != 2 {
		t.Errorf("count = %v, want 2", body["count"])
	}
}

func TestServer_CostsByNamespace_Filter(t *testing.T) {
	store := NewStore()
	store.SetNamespaceCosts([]NamespaceCostData{
		{Namespace: "team-a", EstimatedCostUSD: 0.50, TokenCount: 100000, Models: []string{"qwen3-32b"}},
		{Namespace: "team-b", EstimatedCostUSD: 0.30, TokenCount: 60000, Models: []string{"nomic-embed"}},
		{Namespace: "team-c", EstimatedCostUSD: 0.10, TokenCount: 20000, Models: []string{"phi-3"}},
	})
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/costs/by-namespace?namespace=team-b", nil)
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

	nsCosts, ok := body["namespaceCosts"].([]any)
	if !ok || len(nsCosts) != 1 {
		t.Fatalf("expected 1 namespace cost, got %v", body["namespaceCosts"])
	}
	first := nsCosts[0].(map[string]any)
	if first["namespace"] != "team-b" {
		t.Errorf("namespace = %v, want team-b", first["namespace"])
	}
}

func TestServer_CostsByNamespace_FilterNoMatch(t *testing.T) {
	store := NewStore()
	store.SetNamespaceCosts([]NamespaceCostData{
		{Namespace: "team-a", EstimatedCostUSD: 0.50, TokenCount: 100000},
	})
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/costs/by-namespace?namespace=nonexistent", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if count, ok := body["count"].(float64); !ok || int(count) != 0 {
		t.Errorf("count = %v, want 0", body["count"])
	}
}

func TestStore_Budgets(t *testing.T) {
	s := NewStore()

	if got := s.GetBudgets(); len(got) != 0 {
		t.Errorf("expected empty budgets on new store, got %d", len(got))
	}

	budgets := []BudgetData{
		{Name: "eng-budget", Namespace: "default", MonthlyLimitUSD: 500, CurrentSpendUSD: 250, UtilizationPercent: 50, Status: "ok"},
		{Name: "ml-budget", Namespace: "ml-team", MonthlyLimitUSD: 1000, CurrentSpendUSD: 900, UtilizationPercent: 90, Status: "warning"},
	}
	s.SetBudgets(budgets)

	got := s.GetBudgets()
	if len(got) != 2 {
		t.Fatalf("expected 2 budgets, got %d", len(got))
	}
	if got[0].Name != "eng-budget" {
		t.Errorf("first budget name = %q, want %q", got[0].Name, "eng-budget")
	}
	if got[1].UtilizationPercent != 90 {
		t.Errorf("second budget utilization = %v, want 90", got[1].UtilizationPercent)
	}

	// Verify returned slice is a copy (not aliased).
	got[0].Name = testModifiedValue
	original := s.GetBudgets()
	if original[0].Name == testModifiedValue {
		t.Error("GetBudgets returned aliased slice, not a copy")
	}
}

func TestServer_Budgets_NoData(t *testing.T) {
	store := NewStore()
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/budgets", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if count, ok := body["count"].(float64); !ok || int(count) != 0 {
		t.Errorf("count = %v, want 0", body["count"])
	}
}

func TestServer_Budgets_WithData(t *testing.T) {
	store := NewStore()
	store.SetBudgets([]BudgetData{
		{Name: "eng-budget", Namespace: "default", MonthlyLimitUSD: 500, CurrentSpendUSD: 250, UtilizationPercent: 50, Status: "ok"},
		{Name: "ml-budget", Namespace: "ml-team", MonthlyLimitUSD: 1000, CurrentSpendUSD: 900, UtilizationPercent: 90, Status: "warning"},
	})
	server := NewServer(":0", store)

	req := httptest.NewRequest("GET", "/api/v1/budgets", nil)
	w := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if count, ok := body["count"].(float64); !ok || int(count) != 2 {
		t.Errorf("count = %v, want 2", body["count"])
	}

	budgets, ok := body["budgets"].([]any)
	if !ok || len(budgets) != 2 {
		t.Fatalf("expected 2 budgets, got %v", body["budgets"])
	}
	first := budgets[0].(map[string]any)
	if first["name"] != "eng-budget" {
		t.Errorf("first budget name = %v, want eng-budget", first["name"])
	}
	if first["status"] != "ok" {
		t.Errorf("first budget status = %v, want ok", first["status"])
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
