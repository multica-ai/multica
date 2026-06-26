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

// callContextUsageForSession is callContextUsage targeting a specific thread root
// via the session_id query param (FIR-1874 the session_id is the thread root id).
func callContextUsageForSession(h *Handler, issueID, workspaceID, sessionID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/?session_id="+sessionID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("issueId", issueID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.SetMemberContext(ctx, workspaceID, db.Member{})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ContextUsage(rec, req)
	return rec
}

// seedInitialRunWithUsage inserts a cold-start run (trigger_comment_id NULL, as
// fired at issue creation before any comment exists) with recorded usage, and
// returns the task id. Mirrors seedTaskWithUsage but anchors to no comment.
func seedInitialRunWithUsage(t *testing.T, issueID, workspaceID, model string, cumInput int64) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := sessTestPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		 VALUES ($1::uuid, 'ctx-test-runtime-'||gen_random_uuid(), 'local', 'codex') RETURNING id::text`,
		workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	var agentID string
	if err := sessTestPool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode)
		 VALUES ($1::uuid, 'ctx-test-agent-'||gen_random_uuid(), 'local') RETURNING id::text`,
		workspaceID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	var taskID string
	if err := sessTestPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (issue_id, agent_id, runtime_id, trigger_comment_id)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, NULL) RETURNING id::text`,
		issueID, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create initial task: %v", err)
	}
	if _, err := sessTestPool.Exec(ctx,
		`INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens)
		 VALUES ($1::uuid, 'codex', $2, $3, 0, 0, 0)`,
		taskID, model, cumInput); err != nil {
		t.Fatalf("create task_usage: %v", err)
	}
	return taskID
}

// TestContextUsage_BindsInitialRunToFirstSession proves the FIR-1931 fix: the
// cold-start run (no trigger comment) is adopted by the oldest session so its
// context shows instead of session 1 reading 0.
func TestContextUsage_BindsInitialRunToFirstSession(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	seedRootComment(t, issueID, workspaceID) // session 1 root, the active+oldest session
	seedInitialRunWithUsage(t, issueID, workspaceID, "claude-opus-4-8", 250_000)

	var resp contextUsageResponse
	if err := json.Unmarshal(callContextUsage(h, issueID, workspaceID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.HasData {
		t.Fatalf("has_data = false: the cold-start run was not adopted by session 1 (the FIR-1931 bug)")
	}
	if resp.ContextTokens != 250_000 {
		t.Errorf("context_tokens = %d, want 250000", resp.ContextTokens)
	}
	if resp.UsedPercent != 25 { // 250000 / 1000000 (opus-4-8 1M window)
		t.Errorf("used_percent = %d, want 25", resp.UsedPercent)
	}
}

// TestContextUsage_SecondSessionDoesNotAdoptInitialRun is the guard: a session
// that is NOT the oldest must never inherit the orphan cold-start run.
func TestContextUsage_SecondSessionDoesNotAdoptInitialRun(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	first := seedRootComment(t, issueID, workspaceID)  // session 1 (oldest)
	second := seedRootComment(t, issueID, workspaceID) // session 2 (newer)
	// Backdate session 1 so it is unambiguously the oldest regardless of insert
	// timing — keeps the "is this the first session?" check deterministic.
	if _, err := sessTestPool.Exec(context.Background(),
		`UPDATE comment SET created_at = now() - interval '1 hour' WHERE id = $1::uuid`, first); err != nil {
		t.Fatalf("backdate first root: %v", err)
	}
	seedInitialRunWithUsage(t, issueID, workspaceID, "claude-opus-4-8", 250_000)

	// Target session 2 explicitly; it is not the oldest, so it must NOT adopt the
	// orphan initial run.
	var resp contextUsageResponse
	if err := json.Unmarshal(callContextUsageForSession(h, issueID, workspaceID, second).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.HasData {
		t.Fatalf("session 2 wrongly adopted the cold-start run (context_tokens=%d)", resp.ContextTokens)
	}
}

// seedReplyComment inserts a reply comment under parentID and returns its id.
// Used to build threads deeper than the root + direct-reply shape.
func seedReplyComment(t *testing.T, issueID, workspaceID, parentID string) string {
	t.Helper()
	var id string
	if err := sessTestPool.QueryRow(context.Background(),
		`INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id)
		 VALUES ($1::uuid, $2::uuid, 'agent', gen_random_uuid(), 'reply in this thread', 'comment', $3::uuid)
		 RETURNING id::text`, issueID, workspaceID, parentID).Scan(&id); err != nil {
		t.Fatalf("create reply comment: %v", err)
	}
	return id
}

// TestContextUsage_BindsRunsAtAnyThreadDepth proves the FIR-1931 fix: a run
// triggered by a comment nested DEEPER than a direct reply (depth ≥2) still
// counts toward its session. The old membership check (root + direct replies
// only) dropped it, leaving the gauge blank for a session that had real usage.
func TestContextUsage_BindsRunsAtAnyThreadDepth(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	// root → depth-1 reply → depth-2 reply. The run fires inside the depth-2
	// comment (the shape produced when an agent, forced to reply at parent =
	// its depth-1 trigger, lands at depth 2 and a follow-up triggers there).
	rootID := seedRootComment(t, issueID, workspaceID)
	depth1 := seedReplyComment(t, issueID, workspaceID, rootID)
	depth2 := seedReplyComment(t, issueID, workspaceID, depth1)

	// 300k whole-prompt read against the 1M claude-opus-4-7 window = 30%.
	seedTaskWithUsage(t, issueID, workspaceID, depth2, "claude-opus-4-7", 300_000, 0)

	var resp contextUsageResponse
	if err := json.Unmarshal(callContextUsage(h, issueID, workspaceID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.HasData {
		t.Fatalf("has_data = false: a depth-2-triggered run was dropped from its session (the FIR-1931 bug)")
	}
	if resp.Model != "claude-opus-4-7" {
		t.Errorf("model = %q, want claude-opus-4-7", resp.Model)
	}
	if resp.ContextTokens != 300_000 {
		t.Errorf("context_tokens = %d, want 300000", resp.ContextTokens)
	}
	if resp.MaxContextTokens != 1_000_000 {
		t.Errorf("max_context_tokens = %d, want 1000000", resp.MaxContextTokens)
	}
	if resp.UsedPercent != 30 {
		t.Errorf("used_percent = %d, want 30", resp.UsedPercent)
	}
}

// seedTaskWithUsage inserts a runtime + task + task_usage row for the issue and
// returns the task id. The task_usage figures are the cumulative lifetime sum.
// FIR-1874 (thread = session): membership is now by trigger_comment_id, so the
// task is anchored to triggerCommentID — the thread root the run fired inside.
func seedTaskWithUsage(t *testing.T, issueID, workspaceID, triggerCommentID, model string, cumInput, cumCacheRead int64) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := sessTestPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		 VALUES ($1::uuid, 'ctx-test-runtime-'||gen_random_uuid(), 'local', 'codex') RETURNING id::text`,
		workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	var agentID string
	if err := sessTestPool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode)
		 VALUES ($1::uuid, 'ctx-test-agent-'||gen_random_uuid(), 'local') RETURNING id::text`,
		workspaceID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	var taskID string
	if err := sessTestPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (issue_id, agent_id, runtime_id, trigger_comment_id)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid) RETURNING id::text`,
		issueID, agentID, runtimeID, triggerCommentID).Scan(&taskID); err != nil {
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
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	// FIR-1874: a session is a thread, so the run must be triggered inside one.
	rootID := seedRootComment(t, issueID, workspaceID)

	// The Codex bug shape: cumulative 1955k against the 272k gpt-5.5 window.
	taskID := seedTaskWithUsage(t, issueID, workspaceID, rootID, "gpt-5.5", 1_955_000, 1_700_000)

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

// TestContextUsage_ClampsOverWindowCumulative proves FIR-1931 Fix C: a heavy run
// with NO last-turn footprint, whose cumulative cache_read alone is several times
// the window, must report a token figure clamped to the window (never 6986k /
// 1000k) and flag approximate=true so the bar prefixes "~".
func TestContextUsage_ClampsOverWindowCumulative(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	rootID := seedRootComment(t, issueID, workspaceID)
	// 7M cumulative cache_read on the 1M opus-4-8 window, no footprint recorded.
	seedTaskWithUsage(t, issueID, workspaceID, rootID, "claude-opus-4-8", 0, 7_000_000)

	var resp contextUsageResponse
	if err := json.Unmarshal(callContextUsage(h, issueID, workspaceID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.HasData {
		t.Fatalf("has_data = false, want true")
	}
	if resp.ContextTokens != 1_000_000 {
		t.Errorf("context_tokens = %d, want 1000000 (clamped to window, not 7000000)", resp.ContextTokens)
	}
	if resp.UsedPercent != 100 {
		t.Errorf("used_percent = %d, want 100", resp.UsedPercent)
	}
	if !resp.Approximate {
		t.Errorf("approximate = false, want true for the cumulative fallback")
	}
}

// TestContextUsage_FootprintIsNotApproximate proves the companion of Fix C: a run
// WITH a recorded last-turn footprint reports its exact figure and approximate is
// false, so the bar shows the precise count with no "~".
func TestContextUsage_FootprintIsNotApproximate(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	rootID := seedRootComment(t, issueID, workspaceID)
	taskID := seedTaskWithUsage(t, issueID, workspaceID, rootID, "claude-opus-4-8", 1_955_000, 1_700_000)
	if _, err := sessTestPool.Exec(context.Background(),
		`INSERT INTO cerebro_task_context_footprint (task_id, model, input_tokens, cache_read_tokens)
		 VALUES ($1::uuid, 'claude-opus-4-8', 300000, 0)`, taskID); err != nil {
		t.Fatalf("insert footprint: %v", err)
	}

	var resp contextUsageResponse
	if err := json.Unmarshal(callContextUsage(h, issueID, workspaceID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ContextTokens != 300_000 {
		t.Errorf("context_tokens = %d, want 300000", resp.ContextTokens)
	}
	if resp.Approximate {
		t.Errorf("approximate = true, want false for the last-turn footprint")
	}
}

// TestContextUsage_TracksCompactions proves FIR-1960: the lightweight context
// measurement reports the session's compaction count and whether the latest run
// was itself a compaction, reusing the same heuristic as the development curve.
// Three runs 200k → 700k → 100k on the 1M opus window: the final sharp drop
// (100k < 60% of 700k, prev was 70% full) is one compaction, and it is the
// latest run.
func TestContextUsage_TracksCompactions(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	rootID := seedRootComment(t, issueID, workspaceID)
	taskID := seedTaskWithUsage(t, issueID, workspaceID, rootID, "claude-opus-4-8", 100_000, 0)
	seedFootprintHistory(t, taskID, "claude-opus-4-8", 200_000, 0, 300)
	seedFootprintHistory(t, taskID, "claude-opus-4-8", 700_000, 0, 200)
	seedFootprintHistory(t, taskID, "claude-opus-4-8", 100_000, 0, 100)

	var resp contextUsageResponse
	if err := json.Unmarshal(callContextUsageForSession(h, issueID, workspaceID, rootID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.HasData {
		t.Fatalf("has_data = false, want true")
	}
	if resp.Compactions != 1 {
		t.Errorf("compactions = %d, want 1", resp.Compactions)
	}
	if !resp.LastRunCompaction {
		t.Errorf("last_run_compaction = false, want true (latest run was the 700k → 100k drop)")
	}
}

// TestContextUsage_NoCompactionWhenSteady proves a session whose context only
// rises reports zero compactions and last_run_compaction=false.
func TestContextUsage_NoCompactionWhenSteady(t *testing.T) {
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	rootID := seedRootComment(t, issueID, workspaceID)
	taskID := seedTaskWithUsage(t, issueID, workspaceID, rootID, "claude-opus-4-8", 100_000, 0)
	seedFootprintHistory(t, taskID, "claude-opus-4-8", 200_000, 0, 300)
	seedFootprintHistory(t, taskID, "claude-opus-4-8", 400_000, 0, 200)
	seedFootprintHistory(t, taskID, "claude-opus-4-8", 600_000, 0, 100)

	var resp contextUsageResponse
	if err := json.Unmarshal(callContextUsageForSession(h, issueID, workspaceID, rootID).Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Compactions != 0 {
		t.Errorf("compactions = %d, want 0", resp.Compactions)
	}
	if resp.LastRunCompaction {
		t.Errorf("last_run_compaction = true, want false (context only rose)")
	}
}
