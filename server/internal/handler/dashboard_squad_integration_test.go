package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestDashboardSquadAttributionAndEndpoints verifies the end-to-end Squad usage
// rollup pipeline (migration 735–739), legacy NULL-squad bucket healing, the 3
// additive Squad dashboard endpoints, and `?squad_id=` scoping across the
// existing dashboard endpoints.
func TestDashboardSquadAttributionAndEndpoints(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Dedicated isolated runtime + 2 agents + 1 squad so ambient test rows
	// never collide with assertions.
	runtimeID := createClaimReclaimRuntime(t, ctx, "squad-dash-rt")
	leaderID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "squad-dash-leader")
	memberID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "squad-dash-member")

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Dashboard Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	const provider = "squad-e2e-prov"
	const model = "claude-sonnet-4-6"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly_dirty WHERE runtime_id = $1`, runtimeID)
	})

	now := time.Now().UTC()
	hourBucket := now.Truncate(time.Hour)
	started := now.Add(-15 * time.Minute)
	completed := now.Add(-5 * time.Minute) // 600s each

	// Pre-seed a stale legacy (pre-735) squad_id=NULL bucket for the leader in
	// the same hour. Migration 739's dirty_from_updates UNION must heal this
	// legacy NULL row down to 0 (delete it) once the squad-attributed task
	// rolls up, proving zero double-counting.
	dbfx.Exec(t, `
		INSERT INTO task_usage_hourly (
			bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id,
			provider, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, task_count, event_count
		)
		VALUES ($1, $2, $3, $4, NULL, NULL, $5, $6, 99999, 0, 0, 0, 1, 1)
	`, hourBucket, testWorkspaceID, runtimeID, leaderID, provider, model)

	// 1. Leader task (squad_id = squadID, completed, 1000 input tokens)
	leaderTaskID := dbfx.Task(t, leaderID, testutil.Cols{
		"issue_id":       issueID,
		"runtime_id":     runtimeID,
		"status":         "completed",
		"is_leader_task": true,
		"squad_id":       squadID,
		"started_at":     started,
		"completed_at":   completed,
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":       leaderTaskID,
		"provider":      provider,
		"model":         model,
		"input_tokens":  1000,
		"output_tokens": 200,
		"created_at":    now.Add(-4 * time.Minute),
		"updated_at":    now.Add(-4 * time.Minute),
	})

	// 2. Delegated member task (usage_squad_id = squadID, squad_id = NULL, is_leader_task = false, failed, 2000 input tokens)
	memberSquadTaskID := dbfx.Task(t, memberID, testutil.Cols{
		"issue_id":               issueID,
		"runtime_id":             runtimeID,
		"status":                 "failed",
		"failure_reason":         "timeout",
		"is_leader_task":         false,
		"usage_squad_id":         squadID,
		"delegated_from_task_id": leaderTaskID,
		"started_at":             started,
		"completed_at":           completed,
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":       memberSquadTaskID,
		"provider":      provider,
		"model":         model,
		"input_tokens":  2000,
		"output_tokens": 400,
		"created_at":    now.Add(-3 * time.Minute),
		"updated_at":    now.Add(-3 * time.Minute),
	})

	// 3. Standalone member task (squad_id = NULL, completed, 500 input tokens)
	standaloneTaskID := dbfx.Task(t, memberID, testutil.Cols{
		"issue_id":       issueID,
		"runtime_id":     runtimeID,
		"status":         "completed",
		"is_leader_task": false,
		"started_at":     started,
		"completed_at":   completed,
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":       standaloneTaskID,
		"provider":      provider,
		"model":         model,
		"input_tokens":  500,
		"output_tokens": 100,
		"created_at":    now.Add(-2 * time.Minute),
		"updated_at":    now.Add(-2 * time.Minute),
	})

	// Run the hourly rollup window over the last hour.
	var affected int64
	dbfx.QueryRow(t, `SELECT rollup_task_usage_hourly_window($1, $2)`,
		now.Add(-10*time.Minute), now.Add(time.Minute),
	).Scan(&affected)
	if affected < 3 {
		t.Fatalf("expected rollup to upsert >=3 rows, got %d", affected)
	}

	// Verify the legacy NULL squad bucket for leaderID was healed/deleted!
	var legacyCount int
	dbfx.QueryRow(t, `
		SELECT COUNT(*) FROM task_usage_hourly
		WHERE runtime_id = $1 AND agent_id = $2 AND squad_id IS NULL AND provider = $3
	`, runtimeID, leaderID, provider).Scan(&legacyCount)
	if legacyCount != 0 {
		t.Fatalf("legacy squad_id=NULL bucket for leader was NOT healed; count=%d", legacyCount)
	}

	// Helper to call dashboard handlers as the workspace owner.
	getJSON := func(handlerFn func(http.ResponseWriter, *http.Request), path string, wantStatus int, out any) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest(http.MethodGet, path, nil)
		handlerFn(w, req)
		if w.Code != wantStatus {
			t.Fatalf("GET %s: status=%d, want %d; body=%s", path, w.Code, wantStatus, w.Body.String())
		}
		if out != nil {
			if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
				t.Fatalf("decode GET %s: %v", path, err)
			}
		}
	}

	// 1) Invalid squad_id must return 400.
	getJSON(testHandler.GetDashboardUsageDaily, "/api/dashboard/usage/daily?days=7&squad_id=not-a-uuid", http.StatusBadRequest, nil)

	// 2) GET /api/dashboard/usage/by-squad returns both squad rows and standalone row.
	var bySquad []struct {
		SquadID     string `json:"squad_id"`
		AgentID     string `json:"agent_id"`
		Provider    string `json:"provider"`
		InputTokens int64  `json:"input_tokens"`
		TaskCount   int32  `json:"task_count"`
	}
	getJSON(testHandler.GetDashboardUsageBySquad, "/api/dashboard/usage/by-squad?days=7&tz=UTC", http.StatusOK, &bySquad)
	squadTokens := int64(0)
	standaloneTokens := int64(0)
	for _, r := range bySquad {
		if r.Provider != provider {
			continue
		}
		switch r.SquadID {
		case squadID:
			squadTokens += r.InputTokens
		case "":
			standaloneTokens += r.InputTokens
		}
	}
	if squadTokens != 3000 {
		t.Fatalf("by-squad squad input_tokens = %d, want 3000 (leader 1000 + member 2000)", squadTokens)
	}
	if standaloneTokens != 500 {
		t.Fatalf("by-squad standalone input_tokens = %d, want 500", standaloneTokens)
	}

	// 3) GET /api/dashboard/usage/by-agent?squad_id=<squadID> scopes to the squad only.
	var byAgentScoped []struct {
		AgentID     string `json:"agent_id"`
		Provider    string `json:"provider"`
		InputTokens int64  `json:"input_tokens"`
	}
	getJSON(testHandler.GetDashboardUsageByAgent, "/api/dashboard/usage/by-agent?days=7&tz=UTC&squad_id="+squadID, http.StatusOK, &byAgentScoped)
	scopedTotal := int64(0)
	for _, r := range byAgentScoped {
		if r.Provider == provider {
			scopedTotal += r.InputTokens
		}
	}
	if scopedTotal != 3000 {
		t.Fatalf("by-agent?squad_id=<S> input_tokens = %d, want 3000", scopedTotal)
	}

	// 4) GET /api/dashboard/usage/by-agent?squad_id=__standalone__ scopes to non-squad tasks only.
	var byAgentStandalone []struct {
		AgentID     string `json:"agent_id"`
		Provider    string `json:"provider"`
		InputTokens int64  `json:"input_tokens"`
	}
	getJSON(testHandler.GetDashboardUsageByAgent, "/api/dashboard/usage/by-agent?days=7&tz=UTC&squad_id=__standalone__", http.StatusOK, &byAgentStandalone)
	standaloneScopedTotal := int64(0)
	for _, r := range byAgentStandalone {
		if r.Provider == provider {
			standaloneScopedTotal += r.InputTokens
		}
	}
	if standaloneScopedTotal != 500 {
		t.Fatalf("by-agent?squad_id=__standalone__ input_tokens = %d, want 500", standaloneScopedTotal)
	}

	// 5) GET /api/dashboard/squad-runtime returns per-(squad, agent) runtime totals.
	var squadRuntime []struct {
		SquadID      string  `json:"squad_id"`
		AgentID      string  `json:"agent_id"`
		TotalSeconds float64 `json:"total_seconds"`
		TaskCount    int32   `json:"task_count"`
		FailedCount  int32   `json:"failed_count"`
	}
	getJSON(testHandler.GetDashboardSquadRunTime, "/api/dashboard/squad-runtime?days=7&tz=UTC", http.StatusOK, &squadRuntime)
	var squadTasks, squadFailed int32
	for _, r := range squadRuntime {
		if r.SquadID == squadID {
			squadTasks += r.TaskCount
			squadFailed += r.FailedCount
		}
	}
	if squadTasks != 2 || squadFailed != 1 {
		t.Fatalf("squad-runtime for squad = tasks:%d failed:%d, want 2/1", squadTasks, squadFailed)
	}

	// 6) GET /api/dashboard/failures/by-squad returns both succeeded ("") and failed ("timeout") rows.
	var failuresBySquad []struct {
		SquadID       string `json:"squad_id"`
		FailureReason string `json:"failure_reason"`
		TaskCount     int32  `json:"task_count"`
	}
	getJSON(testHandler.GetDashboardFailuresBySquad, "/api/dashboard/failures/by-squad?days=7&tz=UTC", http.StatusOK, &failuresBySquad)
	var squadSuccesses, squadTimeouts int32
	for _, r := range failuresBySquad {
		if r.SquadID != squadID {
			continue
		}
		switch r.FailureReason {
		case "":
			squadSuccesses += r.TaskCount
		case "timeout":
			squadTimeouts += r.TaskCount
		}
	}
	if squadSuccesses != 1 || squadTimeouts != 1 {
		t.Fatalf("failures/by-squad for squad = ok:%d timeout:%d, want 1/1", squadSuccesses, squadTimeouts)
	}
}

// TestSquadInheritanceAndDaemonClaimSafety verifies that:
//  1. A member task delegated from a Squad leader comment inherits squad_id
//     with is_leader_task=false.
//  2. A sub-issue created by a Squad task (origin_type='agent_create') causes
//     tasks enqueued for that sub-issue to inherit squad_id with
//     is_leader_task=false.
//  3. ListIssueTasks exposes squad_id to the UI so execution avatars collapse.
//  4. ClaimTaskByRuntime strips squad_id (resp.Task.SquadID == "") and keeps
//     is_leader_task=false with NO Squad leader briefing, preserving daemon
//     compatibility with taskIsSquadLeader (daemon/prompt.go).
func TestSquadInheritanceAndDaemonClaimSafety(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderRuntimeID := createClaimReclaimRuntime(t, ctx, "inherit-leader-rt")
	memberRuntimeID := createClaimReclaimRuntime(t, ctx, "inherit-member-rt")
	leaderID, parentIssueID := createClaimReclaimAgentAndIssue(t, ctx, leaderRuntimeID, "inherit-leader")
	memberID, standaloneIssueID := createClaimReclaimAgentAndIssue(t, ctx, memberRuntimeID, "inherit-member")
	dbfx.Exec(t, `UPDATE agent SET instructions = '' WHERE id IN ($1, $2)`, leaderID, memberID)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Inherit Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	// Seed the leader's running task on the parent issue with squad_id = squadID.
	leaderTaskID := dbfx.Task(t, leaderID, testutil.Cols{
		"issue_id":       parentIssueID,
		"runtime_id":     leaderRuntimeID,
		"status":         "running",
		"is_leader_task": true,
		"squad_id":       squadID,
	})

	// Case A: Leader leaves a comment @mentioning memberID with source_task_id = leaderTaskID.
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, source_task_id)
		VALUES ($1, $2, 'agent', $3, 'please implement this', $4)
		RETURNING id
	`, testWorkspaceID, parentIssueID, leaderID, leaderTaskID).Scan(&commentID); err != nil {
		t.Fatalf("create leader delegation comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID) })

	parentIssue, err := testHandler.Queries.GetIssue(ctx, parseUUID(parentIssueID))
	if err != nil {
		t.Fatalf("GetIssue(parent): %v", err)
	}
	delegatedTask, err := testHandler.TaskService.EnqueueTaskForMention(
		ctx, parentIssue, parseUUID(memberID), parseUUID(commentID), service.OriginNamed,
	)
	if err != nil {
		t.Fatalf("EnqueueTaskForMention: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, uuidToString(delegatedTask.ID)) })

	if uuidToString(delegatedTask.UsageSquadID) != squadID {
		t.Fatalf("delegated task usage_squad_id = %q, want %q", uuidToString(delegatedTask.UsageSquadID), squadID)
	}
	if delegatedTask.SquadID.Valid {
		t.Fatalf("delegated task must keep role-proof squad_id NULL, got %q", uuidToString(delegatedTask.SquadID))
	}
	if delegatedTask.IsLeaderTask {
		t.Fatalf("delegated task must have is_leader_task=false")
	}

	// Verify ListTasksByIssue exposes squad_id to the UI.
	wList := httptest.NewRecorder()
	reqList := withURLParam(newRequest(http.MethodGet, "/api/issues/"+parentIssueID+"/task-runs", nil), "id", parentIssueID)
	testHandler.ListTasksByIssue(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("ListIssueTasks status=%d: %s", wList.Code, wList.Body.String())
	}
	var listed []AgentTaskResponse
	if err := json.Unmarshal(wList.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode ListIssueTasks: %v", err)
	}
	foundDelegated := false
	for _, item := range listed {
		if item.ID == uuidToString(delegatedTask.ID) {
			foundDelegated = true
			if item.SquadID != squadID {
				t.Fatalf("ListIssueTasks delegated task squad_id = %q, want %q", item.SquadID, squadID)
			}
			if item.IsLeaderTask {
				t.Fatalf("ListIssueTasks delegated task is_leader_task = true, want false")
			}
		}
	}
	if !foundDelegated {
		t.Fatalf("delegated task %s missing from ListIssueTasks", uuidToString(delegatedTask.ID))
	}

	// CRITICAL: ClaimTaskByRuntime MUST strip squad_id on non-quick-create claims
	// so installed daemons running taskIsSquadLeader never inject leader framing.
	wClaim := httptest.NewRecorder()
	reqClaim := withURLParam(
		newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+memberRuntimeID+"/tasks/claim", nil),
		"runtimeId", memberRuntimeID,
	)
	testHandler.ClaimTaskByRuntime(wClaim, reqClaim)
	if wClaim.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime status=%d: %s", wClaim.Code, wClaim.Body.String())
	}
	var claimed struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(wClaim.Body.Bytes(), &claimed); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimed.Task == nil || claimed.Task.ID != uuidToString(delegatedTask.ID) {
		t.Fatalf("claimed task = %+v, want %s", claimed.Task, uuidToString(delegatedTask.ID))
	}
	if claimed.Task.SquadID != "" {
		t.Fatalf("CRITICAL: ClaimTaskByRuntime leaked squad_id=%q on delegated member task!", claimed.Task.SquadID)
	}
	if claimed.Task.IsLeaderTask {
		t.Fatalf("ClaimTaskByRuntime reported is_leader_task=true on delegated member task")
	}
	if claimed.Task.Agent != nil && (strings.Contains(claimed.Task.Agent.Instructions, "## Squad Operating Protocol") ||
		strings.Contains(claimed.Task.Agent.Instructions, "## Squad Roster")) {
		t.Fatalf("ClaimTaskByRuntime injected squad briefing into delegated member task: %s", claimed.Task.Agent.Instructions)
	}

	// Case B: Sub-issue created by the Squad leader task (origin_type='agent_create',
	// origin_task_id=leaderTaskID) inherits squad_id when assigned to memberID.
	var childIssueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, number, title, status, priority,
			assignee_type, assignee_id, creator_type, creator_id,
			parent_issue_id, origin_type, origin_id
		)
		VALUES (
			$1, (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1),
			'Squad Child Issue', 'todo', 'none',
			'agent', $2, 'agent', $3,
			$4, 'agent_create', $5
		)
		RETURNING id
	`, testWorkspaceID, memberID, leaderID, parentIssueID, leaderTaskID).Scan(&childIssueID); err != nil {
		t.Fatalf("create squad child issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, childIssueID) })

	childIssue, err := testHandler.Queries.GetIssue(ctx, parseUUID(childIssueID))
	if err != nil {
		t.Fatalf("GetIssue(child): %v", err)
	}
	childTask, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, childIssue)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue(child): %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, uuidToString(childTask.ID)) })
	if uuidToString(childTask.UsageSquadID) != squadID {
		t.Fatalf("sub-issue task usage_squad_id = %q, want %q", uuidToString(childTask.UsageSquadID), squadID)
	}
	if childTask.SquadID.Valid {
		t.Fatalf("sub-issue task must keep role-proof squad_id NULL, got %q", uuidToString(childTask.SquadID))
	}
	if childTask.IsLeaderTask {
		t.Fatalf("sub-issue task must have is_leader_task=false")
	}

	// Case C: Standalone human-created issue assigned to memberID stays usage_squad_id = NULL & squad_id = NULL.
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, standaloneIssueID, memberID)
	standaloneIssue, err := testHandler.Queries.GetIssue(ctx, parseUUID(standaloneIssueID))
	if err != nil {
		t.Fatalf("GetIssue(standalone): %v", err)
	}
	standaloneTask, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, standaloneIssue)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue(standalone): %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, uuidToString(standaloneTask.ID)) })
	if standaloneTask.SquadID.Valid || standaloneTask.UsageSquadID.Valid {
		t.Fatalf("standalone task must have NULL squad_id and usage_squad_id, got squad=%q usage_squad=%q",
			uuidToString(standaloneTask.SquadID), uuidToString(standaloneTask.UsageSquadID))
	}
}

// TestDashboardRollupHealsLegacyNullBucketOnProjectMove reproduces the
// trigger-enqueued dirty-key scenario: a pre-735 task_usage_hourly row sits
// under (old_project, squad_id=NULL) while the underlying task carries a
// squad_id. Moving the issue to a new project enqueues dirty keys via trigger
// (not watermark). Rollup must heal/delete the legacy NULL row on old_project
// and write only the new (new_project, squad_id) row — zero double-counting.
func TestDashboardRollupHealsLegacyNullBucketOnProjectMove(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "squad-move-rt")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "squad-move-agent")

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Move Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	projA := dbfx.Project(t, "Move Proj A")
	projB := dbfx.Project(t, "Move Proj B")

	dbfx.Exec(t, `UPDATE issue SET project_id = $2 WHERE id = $1`, issueID, projA)

	const provider = "squad-move-prov"
	const model = "claude-sonnet-4-6"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly_dirty WHERE runtime_id = $1`, runtimeID)
	})

	// Place task_usage in an older hour outside the rollup's p_from..p_to
	// watermark window so ONLY dirty_from_queue (enqueued by the project-move
	// trigger) drives the rollup — exercising the exact bug path.
	oldTime := time.Now().UTC().Add(-6 * time.Hour)
	oldBucket := oldTime.Truncate(time.Hour)

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":       issueID,
		"runtime_id":     runtimeID,
		"status":         "completed",
		"usage_squad_id": squadID,
		"started_at":     oldTime.Add(-2 * time.Minute),
		"completed_at":   oldTime,
	})
	dbfx.Insert(t, "task_usage", testutil.Cols{
		"task_id":       taskID,
		"provider":      provider,
		"model":         model,
		"input_tokens":  1000,
		"output_tokens": 0,
		"created_at":    oldTime,
		"updated_at":    oldTime,
	})

	// Simulate pre-735 state: the bucket on projA was rolled up with squad_id = NULL.
	dbfx.Exec(t, `
		INSERT INTO task_usage_hourly (
			bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id,
			provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			task_count, event_count
		)
		VALUES ($1, $2, $3, $4, $5, NULL, $6, $7, 1000, 0, 0, 0, 1, 1)
	`, oldBucket, testWorkspaceID, runtimeID, agentID, projA, provider, model)

	// Move issue from projA to projB — fires enqueue_task_usage_hourly_dirty_for_issue_project.
	dbfx.Exec(t, `UPDATE issue SET project_id = $2 WHERE id = $1`, issueID, projB)

	// Run rollup over ONLY the last 5 minutes (excludes oldTime from dirty_raw!).
	now := time.Now().UTC()
	var affected int64
	dbfx.QueryRow(t, `SELECT rollup_task_usage_hourly_window($1, $2)`,
		now.Add(-5*time.Minute), now.Add(time.Minute),
	).Scan(&affected)

	var totalTokens int64
	var oldProjRows int
	dbfx.QueryRow(t, `
		SELECT COALESCE(SUM(input_tokens), 0),
		       COUNT(*) FILTER (WHERE project_id = $2)
		  FROM task_usage_hourly
		 WHERE runtime_id = $1 AND provider = $3
	`, runtimeID, projA, provider).Scan(&totalTokens, &oldProjRows)

	if oldProjRows != 0 {
		t.Fatalf("legacy NULL bucket on old project survived move: oldProjRows=%d", oldProjRows)
	}
	if totalTokens != 1000 {
		t.Fatalf("total input_tokens after project move = %d, want 1000 (no double-counting)", totalTokens)
	}
}
