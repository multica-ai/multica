package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type stubReadinessDB struct {
	pingErr    error
	queryErr   error
	applied    bool
	pingCalls  atomic.Int32
	queryCalls atomic.Int32
}

func (s *stubReadinessDB) Ping(context.Context) error {
	s.pingCalls.Add(1)
	return s.pingErr
}

func (s *stubReadinessDB) QueryRow(context.Context, string, ...any) pgx.Row {
	s.queryCalls.Add(1)
	return stubRow{applied: s.applied, err: s.queryErr}
}

type stubRow struct {
	applied bool
	err     error
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*bool)) = r.applied
	return nil
}

func TestServerHealthReadyHandlerDBPingFailure(t *testing.T) {
	db := &stubReadinessDB{pingErr: errors.New("db unavailable")}
	h := &serverHealth{
		db:              db,
		latestMigration: "056_example",
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.readyHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var resp readinessResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Status != "not_ready" {
		t.Fatalf("status = %q, want %q", resp.Status, "not_ready")
	}
	if resp.Checks.DB != "error" {
		t.Fatalf("db check = %q, want %q", resp.Checks.DB, "error")
	}
	if resp.Checks.Migrations != "unknown" {
		t.Fatalf("migrations check = %q, want %q", resp.Checks.Migrations, "unknown")
	}
}

func TestServerHealthReadyHandlerMigrationOutOfDate(t *testing.T) {
	db := &stubReadinessDB{applied: false}
	h := &serverHealth{
		db:              db,
		latestMigration: "056_example",
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.readyHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var resp readinessResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Status != "not_ready" {
		t.Fatalf("status = %q, want %q", resp.Status, "not_ready")
	}
	if resp.Checks.DB != "ok" {
		t.Fatalf("db check = %q, want %q", resp.Checks.DB, "ok")
	}
	if resp.Checks.Migrations != "out_of_date" {
		t.Fatalf("migrations check = %q, want %q", resp.Checks.Migrations, "out_of_date")
	}
}

func TestServerHealthReadinessCachesResult(t *testing.T) {
	db := &stubReadinessDB{applied: true}
	h := &serverHealth{
		db:              db,
		latestMigration: "056_example",
		cacheTTL:        time.Minute,
	}

	resp1, status1 := h.readiness(context.Background())
	resp2, status2 := h.readiness(context.Background())

	if status1 != http.StatusOK || status2 != http.StatusOK {
		t.Fatalf("expected cached readiness status 200, got %d and %d", status1, status2)
	}
	if resp1.Status != "ok" || resp2.Status != "ok" {
		t.Fatalf("expected cached readiness status ok, got %q and %q", resp1.Status, resp2.Status)
	}
	if got := db.pingCalls.Load(); got != 1 {
		t.Fatalf("Ping calls = %d, want 1", got)
	}
	if got := db.queryCalls.Load(); got != 1 {
		t.Fatalf("QueryRow calls = %d, want 1", got)
	}
}
