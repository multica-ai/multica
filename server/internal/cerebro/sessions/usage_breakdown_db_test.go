package sessions

// FIR-1931 integration test: the per-session usage breakdown sums task_usage
// per thread, keeps sessions separate, aggregates an issue total, folds the
// cold-start run into session 1, and falls back to the pricing-table estimate
// when a run reports no gateway cost. Skips when no test DB is reachable.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func callUsageBreakdown(h *Handler, issueID, workspaceID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("issueId", issueID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.SetMemberContext(ctx, workspaceID, db.Member{})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.UsageBreakdown(rec, req)
	return rec
}

// TestUsageBreakdown_PerSessionTotalsAndCostFallback proves: two sessions show as
// two separate rows with their own sums, the issue total equals the sum, session
// 1 (oldest) adopts the cold-start run, and the cost falls back to the estimate
// when cost_cents is 0.
func TestUsageBreakdown_PerSessionTotalsAndCostFallback(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool))

	first := seedRootComment(t, issueID, workspaceID)  // session 1 (oldest)
	second := seedRootComment(t, issueID, workspaceID) // session 2
	// Backdate session 1 so it is unambiguously the oldest.
	if _, err := sessTestPool.Exec(context.Background(),
		`UPDATE comment SET created_at = now() - interval '1 hour' WHERE id = $1::uuid`, first); err != nil {
		t.Fatalf("backdate first root: %v", err)
	}

	// Session 1: one real run (200k input) + the cold-start orphan run (50k input).
	seedTaskWithUsage(t, issueID, workspaceID, first, "claude-opus-4-8", 200_000, 0)
	seedInitialRunWithUsage(t, issueID, workspaceID, "claude-opus-4-8", 50_000)
	// Session 2: one run (100k input).
	seedTaskWithUsage(t, issueID, workspaceID, second, "claude-opus-4-8", 100_000, 0)

	var resp usageBreakdownResponse
	if err := json.Unmarshal(callUsageBreakdown(h, issueID, workspaceID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(resp.Sessions))
	}
	s1, s2 := resp.Sessions[0], resp.Sessions[1] // ordered oldest-first
	if s1.SessionID != first {
		t.Errorf("session[0] = %s, want the oldest root %s", s1.SessionID, first)
	}
	// Session 1 = its own 200k run + the adopted 50k cold-start run = 250k, 2 runs.
	if s1.InputTokens != 250_000 {
		t.Errorf("session 1 input = %d, want 250000 (own run + cold-start)", s1.InputTokens)
	}
	if s1.RunCount != 2 {
		t.Errorf("session 1 run_count = %d, want 2", s1.RunCount)
	}
	if s2.InputTokens != 100_000 {
		t.Errorf("session 2 input = %d, want 100000", s2.InputTokens)
	}
	if s2.RunCount != 1 {
		t.Errorf("session 2 run_count = %d, want 1", s2.RunCount)
	}

	// Issue total = sum of the two sessions.
	if resp.TotalInputTokens != 350_000 {
		t.Errorf("total input = %d, want 350000", resp.TotalInputTokens)
	}

	// Cost fell back to the pricing estimate (cost_cents seeded as 0) — must be > 0
	// for 350k of Opus input, and the total must equal the sum of the rows.
	if resp.TotalCostCents <= 0 {
		t.Errorf("total cost = %d, want > 0 (pricing fallback)", resp.TotalCostCents)
	}
	if resp.TotalCostCents != s1.CostCents+s2.CostCents {
		t.Errorf("total cost %d != sum of sessions %d", resp.TotalCostCents, s1.CostCents+s2.CostCents)
	}
}

// TestUsageBreakdown_EmptyIssue proves an issue with a thread but no runs returns
// an empty (non-null) session list and zero totals.
func TestUsageBreakdown_EmptyIssue(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool))
	seedRootComment(t, issueID, workspaceID)

	var resp usageBreakdownResponse
	if err := json.Unmarshal(callUsageBreakdown(h, issueID, workspaceID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Sessions == nil {
		t.Errorf("sessions = nil, want empty slice (never null in JSON)")
	}
	if len(resp.Sessions) != 0 {
		t.Errorf("sessions = %d, want 0 (no runs yet)", len(resp.Sessions))
	}
	if resp.TotalInputTokens != 0 || resp.TotalCostCents != 0 {
		t.Errorf("totals nonzero on empty issue")
	}
}
