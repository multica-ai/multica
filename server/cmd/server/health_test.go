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
	pingErr      error
	queryErr     error
	appliedCount int
	pingCalls    atomic.Int32
	queryCalls   atomic.Int32
}

func (s *stubReadinessDB) Ping(context.Context) error {
	s.pingCalls.Add(1)
	return s.pingErr
}

func (s *stubReadinessDB) QueryRow(context.Context, string, ...any) pgx.Row {
	s.queryCalls.Add(1)
	return stubRow{appliedCount: s.appliedCount, err: s.queryErr}
}

type stubRow struct {
	appliedCount int
	err          error
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int)) = r.appliedCount
	return nil
}

func TestServerHealthReadyHandlerDBPingFailure(t *testing.T) {
	db := &stubReadinessDB{pingErr: errors.New("db unavailable")}
	h := &serverHealth{
		db:                 db,
		requiredMigrations: []string{"056_example"},
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
	db := &stubReadinessDB{appliedCount: 0}
	h := &serverHealth{
		db:                 db,
		requiredMigrations: []string{"056_example"},
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

func TestServerHealthReadyHandlerMigrationPartiallyApplied(t *testing.T) {
	// Three migrations required but only two recorded — the out-of-order case
	// the old "is the latest version applied?" check used to mask. Readiness
	// must report not_ready, not ok.
	db := &stubReadinessDB{appliedCount: 2}
	h := &serverHealth{
		db:                 db,
		requiredMigrations: []string{"120_a", "120_b", "121_c"},
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
	if resp.Checks.Migrations != "out_of_date" {
		t.Fatalf("migrations check = %q, want %q", resp.Checks.Migrations, "out_of_date")
	}
}

func TestServerHealthReadinessCachesResult(t *testing.T) {
	db := &stubReadinessDB{appliedCount: 1}
	h := &serverHealth{
		db:                 db,
		requiredMigrations: []string{"056_example"},
		cacheTTL:           time.Minute,
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

// A restart that fails to bind leaves the previous instance serving the port,
// and every /health probe still returns 200. The process identity below is what
// lets a caller tell "my build answered" from "something answered".
func TestServerHealthLiveHandlerReportsProcessIdentity(t *testing.T) {
	startedAt := time.Date(2026, 8, 20, 10, 30, 0, 0, time.UTC)
	h := &serverHealth{startedAt: startedAt, pid: 4242}

	rec := httptest.NewRecorder()
	h.liveHandler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body liveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status = %q, want ok", body.Status)
	}
	if body.PID != 4242 {
		t.Fatalf("pid = %d, want 4242", body.PID)
	}
	if body.StartedAt != "2026-08-20T10:30:00Z" {
		t.Fatalf("started_at = %q, want 2026-08-20T10:30:00Z", body.StartedAt)
	}
	if body.Commit != commit {
		t.Fatalf("commit = %q, want %q", body.Commit, commit)
	}
}

// newServerHealth is the only production constructor, so startedAt is never
// zero in a running server. Tests build the struct literally; a zero time must
// not be rendered as year 0001, which a caller would read as "started before
// my launch" and report as a stale instance.
func TestServerHealthLiveHandlerOmitsZeroStartedAt(t *testing.T) {
	h := &serverHealth{pid: 7}

	rec := httptest.NewRecorder()
	h.liveHandler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	var body liveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	if body.StartedAt != "" {
		t.Fatalf("started_at = %q, want empty for a zero time", body.StartedAt)
	}
}
