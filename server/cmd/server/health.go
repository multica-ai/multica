package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/migrations"
)

const readinessQuery = `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`

const readinessCacheTTL = 3 * time.Second

type readinessDB interface {
	Ping(ctx context.Context) error
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type serverHealth struct {
	db              readinessDB
	latestMigration string
	initErr         error
	cacheTTL        time.Duration
	refreshMu       sync.Mutex
	cache           atomic.Pointer[cachedReadiness]
}

type cachedReadiness struct {
	response   readinessResponse
	statusCode int
	expiresAt  time.Time
}

type liveResponse struct {
	Status string `json:"status"`
}

type readinessResponse struct {
	Status string          `json:"status"`
	Checks readinessChecks `json:"checks"`
}

type readinessChecks struct {
	DB         string `json:"db"`
	Migrations string `json:"migrations"`
}

func newServerHealth(pool *pgxpool.Pool) *serverHealth {
	latestMigration, err := migrations.LatestVersion()
	return &serverHealth{
		db:              pool,
		latestMigration: latestMigration,
		initErr:         err,
		cacheTTL:        readinessCacheTTL,
	}
}

func (h *serverHealth) liveHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, liveResponse{Status: "ok"})
}

func (h *serverHealth) readyHandler(w http.ResponseWriter, r *http.Request) {
	resp, status := h.readiness(r.Context())
	writeJSON(w, status, resp)
}

func (h *serverHealth) readiness(parent context.Context) (readinessResponse, int) {
	if h.cacheTTL <= 0 {
		return h.computeReadiness(parent)
	}

	now := time.Now()
	if cached := h.loadCachedReadiness(now); cached != nil {
		return cached.response, cached.statusCode
	}

	h.refreshMu.Lock()
	defer h.refreshMu.Unlock()

	now = time.Now()
	if cached := h.loadCachedReadiness(now); cached != nil {
		return cached.response, cached.statusCode
	}

	resp, status := h.computeReadiness(parent)
	h.cache.Store(&cachedReadiness{
		response:   resp,
		statusCode: status,
		expiresAt:  now.Add(h.cacheTTL),
	})
	return resp, status
}

func (h *serverHealth) loadCachedReadiness(now time.Time) *cachedReadiness {
	cached := h.cache.Load()
	if cached == nil || !now.Before(cached.expiresAt) {
		return nil
	}
	return cached
}

func (h *serverHealth) computeReadiness(parent context.Context) (readinessResponse, int) {
	resp := readinessResponse{
		Status: "ok",
		Checks: readinessChecks{
			DB:         "ok",
			Migrations: "ok",
		},
	}

	if h.db == nil {
		resp.Status = "not_ready"
		resp.Checks.DB = "error"
		resp.Checks.Migrations = "unknown"
		return resp, http.StatusServiceUnavailable
	}

	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		resp.Status = "not_ready"
		resp.Checks.DB = "error"
		resp.Checks.Migrations = "unknown"
		return resp, http.StatusServiceUnavailable
	}

	if h.initErr != nil || h.latestMigration == "" {
		resp.Status = "not_ready"
		resp.Checks.Migrations = "error"
		return resp, http.StatusServiceUnavailable
	}

	var applied bool
	if err := h.db.QueryRow(ctx, readinessQuery, h.latestMigration).Scan(&applied); err != nil {
		resp.Status = "not_ready"
		resp.Checks.Migrations = "error"
		return resp, http.StatusServiceUnavailable
	}

	if !applied {
		resp.Status = "not_ready"
		resp.Checks.Migrations = "out_of_date"
		return resp, http.StatusServiceUnavailable
	}

	return resp, http.StatusOK
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
