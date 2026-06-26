package sessions

// FIR-1931 integration test: the per-session context timeline returns the run
// footprints in observation order and flags a compaction where the context
// dropped sharply from a fairly-full previous run. Skips when no test DB.

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

func callContextTimeline(h *Handler, issueID, workspaceID, sessionID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("issueId", issueID)
	rctx.URLParams.Add("sessionId", sessionID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.SetMemberContext(ctx, workspaceID, db.Member{})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ContextTimeline(rec, req)
	return rec
}

// seedFootprintHistory inserts a history row for a task at a controlled
// observation time (secondsAgo before now, so ascending order = decreasing
// secondsAgo).
func seedFootprintHistory(t *testing.T, taskID, model string, input, cacheRead, secondsAgo int64) {
	t.Helper()
	if _, err := sessTestPool.Exec(context.Background(),
		`INSERT INTO cerebro_task_context_footprint_history (task_id, model, input_tokens, cache_read_tokens, observed_at)
		 VALUES ($1::uuid, $2, $3, $4, now() - ($5::bigint * interval '1 second'))`,
		taskID, model, input, cacheRead, secondsAgo); err != nil {
		t.Fatalf("insert footprint history: %v", err)
	}
}

// TestContextTimeline_OrdersAndFlagsCompaction proves the curve is returned
// oldest-first and a sharp drop after a full run is flagged as a compaction.
func TestContextTimeline_OrdersAndFlagsCompaction(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	rootID := seedRootComment(t, issueID, workspaceID)
	taskID := seedTaskWithUsage(t, issueID, workspaceID, rootID, "claude-opus-4-8", 100_000, 0)

	// Three runs on the 1M opus-4-8 window: 200k → 700k (rising) → 100k (sharp
	// drop: 100k < 60% of 700k, and 700k was 70% of the window → compaction).
	seedFootprintHistory(t, taskID, "claude-opus-4-8", 200_000, 0, 300)
	seedFootprintHistory(t, taskID, "claude-opus-4-8", 700_000, 0, 200)
	seedFootprintHistory(t, taskID, "claude-opus-4-8", 100_000, 0, 100)

	var resp contextTimelineResponse
	if err := json.Unmarshal(callContextTimeline(h, issueID, workspaceID, rootID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.HasData || len(resp.Points) != 3 {
		t.Fatalf("points = %d (has_data=%v), want 3", len(resp.Points), resp.HasData)
	}
	if resp.Points[0].UsedPercent != 20 || resp.Points[1].UsedPercent != 70 || resp.Points[2].UsedPercent != 10 {
		t.Errorf("used_percent = [%d %d %d], want [20 70 10]",
			resp.Points[0].UsedPercent, resp.Points[1].UsedPercent, resp.Points[2].UsedPercent)
	}
	if resp.Points[0].IsCompaction || resp.Points[1].IsCompaction {
		t.Errorf("points 0/1 wrongly flagged as compaction")
	}
	if !resp.Points[2].IsCompaction {
		t.Errorf("point 2 (700k → 100k drop) not flagged as compaction")
	}
}

// TestContextTimeline_EmptyWhenNoHistory proves a session with no recorded
// history returns has_data=false and a non-null empty points list.
func TestContextTimeline_EmptyWhenNoHistory(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)
	rootID := seedRootComment(t, issueID, workspaceID)

	var resp contextTimelineResponse
	if err := json.Unmarshal(callContextTimeline(h, issueID, workspaceID, rootID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.HasData {
		t.Errorf("has_data = true, want false (no history)")
	}
	if resp.Points == nil {
		t.Errorf("points = nil, want empty slice (never null)")
	}
}
