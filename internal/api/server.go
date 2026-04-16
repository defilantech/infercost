package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Server serves the InferCost REST API.
type Server struct {
	store      *Store
	httpServer *http.Server
}

// NewServer creates an API server bound to the given address.
func NewServer(addr string, store *Store) *Server {
	mux := http.NewServeMux()
	s := &Server{
		store: store,
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}

	mux.HandleFunc("GET /api/v1/costs/current", s.handleCostsCurrent)
	mux.HandleFunc("GET /api/v1/costs/by-namespace", s.handleCostsByNamespace)
	mux.HandleFunc("GET /api/v1/models", s.handleModels)
	mux.HandleFunc("GET /api/v1/compare", s.handleCompare)
	mux.HandleFunc("GET /api/v1/budgets", s.handleBudgets)
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	return s
}

// Start begins serving the API. Blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	log := logf.FromContext(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error(err, "API server shutdown error")
		}
	}()

	log.Info("starting API server", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("API server: %w", err)
	}
	return nil
}

func (s *Server) handleCostsCurrent(w http.ResponseWriter, _ *http.Request) {
	data := s.store.GetCostData()
	if data == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "no cost data available yet — waiting for first reconcile",
		})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleCostsByNamespace(w http.ResponseWriter, r *http.Request) {
	costs := s.store.GetNamespaceCosts()

	// Filter by namespace query parameter if provided.
	if nsFilter := r.URL.Query().Get("namespace"); nsFilter != "" {
		var filtered []NamespaceCostData
		for _, c := range costs {
			if c.Namespace == nsFilter {
				filtered = append(filtered, c)
			}
		}
		costs = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"namespaceCosts": costs,
		"count":          len(costs),
	})
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	models := s.store.GetModels()
	writeJSON(w, http.StatusOK, map[string]any{
		"models": models,
		"count":  len(models),
	})
}

func (s *Server) handleCompare(w http.ResponseWriter, _ *http.Request) {
	comparisons := s.store.GetComparisons()
	cost := s.store.GetCostData()

	response := map[string]any{
		"comparisons": comparisons,
		"pricingSources": map[string]string{
			"OpenAI":    "https://developers.openai.com/api/docs/pricing",
			"Anthropic": "https://platform.claude.com/docs/en/about-claude/pricing",
			"Google":    "https://ai.google.dev/gemini-api/docs/pricing",
		},
		"lastVerified": "2026-03-21",
		"disclaimer":   "Cloud pricing shown is list price. Does not reflect negotiated enterprise rates or batch discounts.",
	}

	if cost != nil {
		response["onPremCostUSD"] = cost.HourlyCostUSD * cost.UptimeHours
		response["uptimeHours"] = cost.UptimeHours
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleBudgets(w http.ResponseWriter, _ *http.Request) {
	budgets := s.store.GetBudgets()
	writeJSON(w, http.StatusOK, map[string]any{
		"budgets": budgets,
		"count":   len(budgets),
	})
}

// handleStatus returns a combined view — costs, models, and top-level comparison.
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"infrastructure": s.store.GetCostData(),
		"models":         s.store.GetModels(),
		"comparisons":    s.store.GetComparisons(),
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(data)
}
