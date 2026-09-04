package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// batchClaimResponse mirrors the {"tasks":[...]} envelope ClaimTasksByRuntime
// returns, with the few fields these tests assert on.
type batchClaimResponse struct {
	Tasks []struct {
		ID        string `json:"id"`
		RuntimeID string `json:"runtime_id"`
		AuthToken string `json:"auth_token"`
	} `json:"tasks"`
	ClaimPollHintSupported      bool  `json:"claim_poll_hint_supported"`
	NextDeferredTaskAfterMillis int64 `json:"next_deferred_task_after_ms"`
}

func seedQueuedIssueTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&id); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id) })
	return id
}

func postBatchClaim(t *testing.T, workspaceID string, runtimeIDs []string, maxTasks int) *httptest.ResponseRecorder {
	return postBatchClaimWithCapabilities(t, workspaceID, runtimeIDs, maxTasks, "")
}

func postBatchClaimWithCapabilities(t *testing.T, workspaceID string, runtimeIDs []string, maxTasks int, capabilities string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := batchClaimRequest(workspaceID, runtimeIDs, maxTasks, capabilities)
	testHandler.ClaimTasksByRuntime(w, req)
	return w
}

func batchClaimRequest(workspaceID string, runtimeIDs []string, maxTasks int, capabilities string) *http.Request {
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/claim",
		map[string]any{"daemon_id": batchClaimTestDaemonID, "runtime_ids": runtimeIDs, "max_tasks": maxTasks}, workspaceID, batchClaimTestDaemonID)
	if capabilities != "" {
		req.Header.Set("X-Client-Capabilities", capabilities)
	}
	return req
}

// batchClaimTestDaemonID is the daemon id used by both the mdt_ token context
// and the request body in batch-claim handler tests, so the daemon_id
// consistency check passes on the happy path.
const batchClaimTestDaemonID = "batch-claim-review"

func TestClaimTasksByRuntime_ClaimPollHintSchedulesNextDeferredTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Batch claim deferred hint")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Batch claim deferred hint agent")
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
		"status":     "deferred",
		"fire_at":    testutil.Raw("now() + interval '5 seconds'"),
	})

	hinted := testutil.Decode[batchClaimResponse](t, testHandler.ClaimTasksByRuntime,
		batchClaimRequest(testWorkspaceID, []string{runtimeID}, 1, protocol.DaemonCapabilityClaimPollHintsV1), http.StatusOK)
	if !hinted.ClaimPollHintSupported {
		t.Fatal("response did not confirm claim poll hint support")
	}
	if hinted.NextDeferredTaskAfterMillis <= 0 || hinted.NextDeferredTaskAfterMillis > 5000 {
		t.Fatalf("next deferred delay = %dms, want 1..5000ms", hinted.NextDeferredTaskAfterMillis)
	}

	legacy := testutil.Decode[batchClaimResponse](t, testHandler.ClaimTasksByRuntime,
		batchClaimRequest(testWorkspaceID, []string{runtimeID}, 1, ""), http.StatusOK)
	if legacy.ClaimPollHintSupported || legacy.NextDeferredTaskAfterMillis != 0 {
		t.Fatalf("legacy response unexpectedly exposed poll hints: %+v", legacy)
	}
}

func TestNextDeferredTaskFireAtForRuntimes_OmitsIneligibleOverdueTasks(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name        string
		runtimeCols testutil.Cols
		occupied    bool
		wantHint    bool
	}{
		{name: "eligible", wantHint: true},
		{name: "occupied", occupied: true},
		{name: "runtime offline", runtimeCols: testutil.Cols{"status": "offline"}},
		{name: "runtime stale", runtimeCols: testutil.Cols{
			"last_seen_at": testutil.Raw("now() - interval '10 minutes'"),
			"updated_at":   testutil.Raw("now() - interval '10 minutes'"),
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "deferred hint "+tt.name, tt.runtimeCols)
			agentID := dbfx.Agent(t, "deferred hint "+tt.name, runtimeID)
			issueID := dbfx.Issue(t, "deferred hint "+tt.name)
			dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID,
				"issue_id":   issueID,
				"status":     "deferred",
				"fire_at":    testutil.Raw("now() - interval '1 second'"),
			})
			if tt.occupied {
				dbfx.Task(t, agentID, testutil.Cols{
					"runtime_id": runtimeID,
					"issue_id":   issueID,
					"status":     "queued",
				})
			}

			next, err := testHandler.Queries.NextDeferredTaskFireAtForRuntimes(t.Context(), db.NextDeferredTaskFireAtForRuntimesParams{
				RuntimeIds:       []pgtype.UUID{parseUUID(runtimeID)},
				RuntimeStaleSecs: service.RuntimeClaimFreshnessSeconds,
			})
			if err != nil {
				t.Fatalf("NextDeferredTaskFireAtForRuntimes: %v", err)
			}
			if next.Valid != tt.wantHint {
				t.Fatalf("hint validity = %v, want %v", next.Valid, tt.wantHint)
			}
		})
	}
}

func TestClaimPollHintDelayHasOneSecondFloor(t *testing.T) {
	now := time.Unix(1_000, 0)
	for _, tt := range []struct {
		name   string
		fireAt time.Time
		want   time.Duration
	}{
		{name: "overdue", fireAt: now.Add(-time.Minute), want: time.Second},
		{name: "sub-second", fireAt: now.Add(100 * time.Millisecond), want: time.Second},
		{name: "future", fireAt: now.Add(5 * time.Second), want: 5 * time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := claimPollHintDelay(now, tt.fireAt); got != tt.want {
				t.Fatalf("claimPollHintDelay() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestClaimTasksByRuntime_RoutesAcrossRuntimesAndMintsTokens covers the happy
// path: one call claims across two runtimes on the same machine, returns one
// task per runtime (per-agent dedup), and mints a task-scoped token for each.
func TestClaimTasksByRuntime_RoutesAcrossRuntimesAndMintsTokens(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	rt1 := createClaimReclaimRuntime(t, ctx, "Batch claim rt1")
	rt2 := createClaimReclaimRuntime(t, ctx, "Batch claim rt2")
	a1, i1 := createClaimReclaimAgentAndIssue(t, ctx, rt1, "Batch claim a1")
	a2, i2 := createClaimReclaimAgentAndIssue(t, ctx, rt2, "Batch claim a2")
	seedQueuedIssueTask(t, ctx, a1, rt1, i1)
	seedQueuedIssueTask(t, ctx, a2, rt2, i2)

	w := postBatchClaim(t, testWorkspaceID, []string{rt1, rt2}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Fatalf("claimed %d tasks, want 2: %s", len(resp.Tasks), w.Body.String())
	}
	seen := map[string]int{}
	for _, task := range resp.Tasks {
		seen[task.RuntimeID]++
		if !strings.HasPrefix(task.AuthToken, "mat_") {
			t.Fatalf("task %s missing mat_ task token, got %q", task.ID, task.AuthToken)
		}
	}
	if seen[rt1] != 1 || seen[rt2] != 1 {
		t.Fatalf("runtime distribution = %v, want one task each for rt1/rt2", seen)
	}
}

// TestClaimTasksByRuntime_SkipsCrossWorkspaceRuntime is the security-critical
// case: a daemon token scoped to workspace A must not claim a task routed to a
// runtime in workspace B, even when B's runtime_id is included in the request.
func TestClaimTasksByRuntime_SkipsCrossWorkspaceRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// A foreign workspace with its own runtime + agent + queued task.
	var foreignUser, foreignWS string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Foreign User', 'batch-foreign@multica.ai') RETURNING id`).Scan(&foreignUser); err != nil {
		t.Fatalf("foreign user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, foreignUser) })
	if err := testPool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('Foreign WS','batch-foreign-ws','x','FGN') RETURNING id`).Scan(&foreignWS); err != nil {
		t.Fatalf("foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWS) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'owner')`, foreignWS, foreignUser); err != nil {
		t.Fatalf("foreign member: %v", err)
	}
	var foreignRT, foreignAgent, foreignIssue string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, NULL, 'Foreign RT', 'cloud', 'handler_test_runtime', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id`, foreignWS, foreignUser).Scan(&foreignRT); err != nil {
		t.Fatalf("foreign runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'Foreign Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id`, foreignWS, foreignRT, foreignUser).Scan(&foreignAgent); err != nil {
		t.Fatalf("foreign agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'foreign issue', 'in_progress', 'none', $2, 'member', 1, 0)
		RETURNING id`, foreignWS, foreignUser).Scan(&foreignIssue); err != nil {
		t.Fatalf("foreign issue: %v", err)
	}
	foreignTask := seedQueuedIssueTask(t, ctx, foreignAgent, foreignRT, foreignIssue)

	// Daemon token scoped to the (unrelated) handler-test workspace.
	w := postBatchClaim(t, testWorkspaceID, []string{foreignRT}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("cross-workspace claim leaked %d tasks, want 0: %s", len(resp.Tasks), w.Body.String())
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, foreignTask).Scan(&status); err != nil {
		t.Fatalf("read foreign task status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("foreign task status = %s, want still queued (untouched)", status)
	}
}

// TestClaimTasksByRuntime_CancelsTaskWhenRuntimeOwnerMissing pins the
// unscoped-credential guard: a runtime with no owner cannot mint a task token,
// so the claimed task must be cancelled and omitted from the response rather
// than shipped without a scoped credential.
func TestClaimTasksByRuntime_CancelsTaskWhenRuntimeOwnerMissing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var rtNull string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, NULL, 'Ownerless RT', 'cloud', 'handler_test_runtime', 'online', 'x', '{}'::jsonb, now(), 'private', NULL)
		RETURNING id`, testWorkspaceID).Scan(&rtNull); err != nil {
		t.Fatalf("ownerless runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, rtNull) })

	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, rtNull, "Ownerless agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, rtNull, issueID)

	w := postBatchClaim(t, testWorkspaceID, []string{rtNull}, 1)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Fatalf("claimed %d tasks from owner-less runtime, want 0: %s", len(resp.Tasks), w.Body.String())
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("task status = %s, want cancelled (owner missing)", status)
	}
}

// TestFailClaimedTaskBeforeLaunchSettlesDispatchedTask pins the claim-build
// failure behavior used by required Plugin contributions. A durable rejection
// must become a visible terminal task instead of remaining dispatched until
// stale reclaim delivers the same impossible task again.
func TestFailClaimedTaskBeforeLaunchSettlesDispatchedTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Prelaunch failure rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Prelaunch failure agent")
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'dispatched', dispatched_at = now()
		WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}

	failure := testHandler.failClaimedTaskBeforeLaunch(
		ctx,
		&task,
		"Required Remote MCP is unavailable. Test the Plugin connection, then retry.",
		taskfailure.ReasonAgentMissingConfig,
		"error_required_remote_mcp",
		http.StatusConflict,
		"required Remote MCP contribution is unavailable",
	)
	if failure == nil || failure.outcome != "error_required_remote_mcp" || failure.status != http.StatusConflict {
		t.Fatalf("failure = %+v", failure)
	}

	var status, errorMessage, failureReason string
	if err := testPool.QueryRow(ctx, `
		SELECT status, error, failure_reason
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&status, &errorMessage, &failureReason); err != nil {
		t.Fatalf("read settled task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if errorMessage != "Required Remote MCP is unavailable. Test the Plugin connection, then retry." {
		t.Fatalf("task error = %q", errorMessage)
	}
	if failureReason != taskfailure.ReasonAgentMissingConfig.String() {
		t.Fatalf("failure_reason = %q", failureReason)
	}
}
