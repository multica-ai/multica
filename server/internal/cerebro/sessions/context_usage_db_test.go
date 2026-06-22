package sessions

// FIR-1856 integration test: the ContextUsage handler must prefer the last-turn
// footprint (cerebro_task_context_footprint) over the cumulative task_usage when
// a footprint exists. This is the end-to-end proof against a real DB that a Codex
// session showing 1955k/272k cumulative reads its true ~40% last-turn fullness.
//
// Skips cleanly when no test DB is reachable (shares sessTestPool/TestMain with
// handler_db_test.go).

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

func callContextUsage(h *Handler, issueID, workspaceID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("issueId", issueID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.SetMemberContext(ctx, workspaceID, db.Member{})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ContextUsage(rec, req)
	return rec
}

// seedTaskWithUsage inserts a runtime + task + task_usage row for the issue and
// returns the task id. The task_usage figures are the cumulative lifetime sum.
func seedTaskWithUsage(t *testing.T, issueID, workspaceID, model string, cumInput, cumCacheRead int64) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := sessTestPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		 VALUES ($1::uuid, 'ctx-test-runtime', 'local', 'codex') RETURNING id::text`,
		workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	var agentID string
	if err := sessTestPool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode)
		 VALUES ($1::uuid, 'ctx-test-agent', 'local') RETURNING id::text`,
		workspaceID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	var taskID string
	if err := sessTestPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (issue_id, agent_id, runtime_id)
		 VALUES ($1::uuid, $2::uuid, $3::uuid) RETURNING id::text`,
		issueID, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := sessTestPool.Exec(ctx,
		`INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens)
		 VALUES ($1::uuid, 'codex', $2, $3, 0, $4, 0)`,
		taskID, model, cumInput, cumCacheRead); err != nil {
		t.Fatalf("create task_usage: %v", err)
	}
	return taskID
}

func TestContextUsage_PrefersFootprintOverCumulative(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool))

	// The Codex bug shape: cumulative 1955k against the 272k gpt-5.5 window.
	taskID := seedTaskWithUsage(t, issueID, workspaceID, "gpt-5.5", 1_955_000, 1_700_000)

	// Without a footprint, the gauge uses the cumulative and pins at 100%.
	var before contextUsageResponse
	if err := json.Unmarshal(callContextUsage(h, issueID, workspaceID).Body.Bytes(), &before); err != nil {
		t.Fatalf("decode before: %v", err)
	}
	if before.UsedPercent != 100 {
		t.Fatalf("pre-footprint used_percent = %d, want 100 (cumulative pins the gauge)", before.UsedPercent)
	}

	// Record the last-turn footprint: 110k prompt, 100k of it cached.
	if _, err := sessTestPool.Exec(context.Background(),
		`INSERT INTO cerebro_task_context_footprint (task_id, model, input_tokens, cache_read_tokens)
		 VALUES ($1::uuid, 'gpt-5.5', 110000, 100000)`, taskID); err != nil {
		t.Fatalf("insert footprint: %v", err)
	}

	var after contextUsageResponse
	if err := json.Unmarshal(callContextUsage(h, issueID, workspaceID).Body.Bytes(), &after); err != nil {
		t.Fatalf("decode after: %v", err)
	}
	if after.ContextTokens != 110_000 {
		t.Errorf("context_tokens = %d, want 110000 (last-turn footprint, not 1955000)", after.ContextTokens)
	}
	if after.UsedPercent != 40 { // 110000 / 272000 = 40.4%
		t.Errorf("used_percent = %d, want 40 (not the bugged 100)", after.UsedPercent)
	}
	if after.CacheSharePercent != 90 { // 100000 / 110000
		t.Errorf("cache_share_percent = %d, want 90", after.CacheSharePercent)
	}
}
