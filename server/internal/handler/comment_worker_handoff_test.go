package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func claimWorkerReplyRun(t *testing.T, runtimeID string) *AgentTaskResponse {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "worker-reply-handoff")
	req = withURLParam(req, "runtimeId", runtimeID)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCoalescedCommentsV1)
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&response)
	return response.Task
}

func completeWorkerReplyRun(t *testing.T, taskID string) {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete", map[string]any{"output": "Processed the inputs delivered to this run"}, testWorkspaceID, "worker-reply-handoff")
	req = withURLParam(req, "taskId", taskID)
	testutil.Call(t, testHandler.CompleteTask, req).Want(http.StatusOK)
}

// A delegated member run that posts nothing still wakes its coordinator (GH
// #8719). CompleteTask synthesizes the fallback comment from the final output;
// the completion path must route it like an explicit worker reply so the
// leader->worker->leader loop does not stall on the platform-generated comment.
func TestCompletionFallbackWakesSquadLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderRuntimeID := dbfx.Runtime(t, "Fallback handoff leader runtime")
	leaderID := dbfx.Agent(t, "Fallback handoff leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback handoff worker runtime")
	workerID := dbfx.Agent(t, "Fallback handoff worker", workerRuntimeID)
	squadID := dbfx.Squad(t, "Fallback handoff squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Fallback results must reach the coordinator", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		// Production claim receipt: the delegation comment reached this run,
		// so completion reconcile must not replay the trigger back at the worker.
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	// The member posts nothing: completing synthesizes the fallback comment.
	completeWorkerReplyRun(t, workerTaskID)
	var fallbackID string
	dbfx.QueryRow(t, `SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID).Scan(&fallbackID)
	if fallbackID == "" {
		t.Fatal("completion fallback comment was not synthesized")
	}
	next := claimWorkerReplyRun(t, leaderRuntimeID)
	if next == nil {
		t.Fatal("completion fallback did not wake the squad leader")
	}
	if !next.IsLeaderTask || !slices.Contains(next.DeliveredCommentIDs, fallbackID) {
		t.Fatal("follow-up must deliver the fallback in the squad leader role")
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'", issueID, workerID); n != 0 {
		t.Fatalf("completion must not re-enqueue the worker run, got %d queued worker task(s)", n)
	}
}

// A silent guest-squad member run wakes its own coordinator, not the issue's
// assigned squad (GH #8719). The fallback carries no mentions, so only the
// parent delegation chain proves the guest squad's routing authority.
func TestCompletionFallbackWakesGuestSquadLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	assignedLeaderID := dbfx.Agent(t, "Fallback guest assigned leader", testRuntimeID)
	assignedSquadID := dbfx.Squad(t, "Fallback guest assigned squad", assignedLeaderID)
	guestLeaderID := dbfx.Agent(t, "Fallback guest squad leader", testRuntimeID)
	guestSquadID := dbfx.Squad(t, "Fallback guest squad", guestLeaderID)
	workerID := dbfx.Agent(t, "Fallback guest worker", testRuntimeID)
	issueID := dbfx.Issue(t, "Fallback guest delegation keeps its authority", testutil.Cols{
		"assignee_type": "squad", "assignee_id": assignedSquadID,
	})
	guestLeaderTaskID := dbfx.Task(t, guestLeaderID, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": guestSquadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	delegationID := dbfx.Comment(t, issueID, "delegate to guest worker", testutil.Cols{
		"author_type": "agent", "author_id": guestLeaderID, "source_task_id": guestLeaderTaskID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": delegationID, "squad_id": guestSquadID,
		"delegated_from_task_id": guestLeaderTaskID,
		"originator_user_id":     testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + delegationID + "'::uuid]"),
	})
	completeWorkerReplyRun(t, workerTaskID)
	var fallbackID string
	dbfx.QueryRow(t, "SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2", workerTaskID, workerID).Scan(&fallbackID)
	if fallbackID == "" {
		t.Fatal("completion fallback comment was not synthesized")
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE AND squad_id = $3", issueID, guestLeaderID, guestSquadID); got != 1 {
		t.Fatalf("guest squad leader received %d task(s), want 1", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'", issueID, assignedLeaderID); got != 0 {
		t.Fatalf("assigned squad leader received %d task(s), want 0", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'", issueID, workerID); got != 0 {
		t.Fatalf("worker received %d task(s), want 0", got)
	}
}

// A synthesized reply whose parent cannot be resolved in the issue workspace
// is not routed at all. Without the parent the guest delegation chain is
// unprovable, and falling through to the assigned-squad fallback would change
// routing authority on a transient lookup (GH #8719 fail-closed).
func TestCompletionFallbackUnresolvedParentDoesNotRoute(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	foreignWS := dbfx.Workspace(t, "Fallback foreign workspace", "fb-8719-foreign")
	foreignIssue := dbfx.Issue(t, "Fallback foreign issue", testutil.Cols{"workspace_id": foreignWS})
	foreignComment := dbfx.Comment(t, foreignIssue, "foreign delegation", testutil.Cols{"workspace_id": foreignWS})
	workerID := dbfx.Agent(t, "Fallback foreign-parent worker", testRuntimeID)
	assignedLeaderID := dbfx.Agent(t, "Fallback unresolved assigned leader", testRuntimeID)
	assignedSquadID := dbfx.Squad(t, "Fallback unresolved assigned squad", assignedLeaderID)
	issueID := dbfx.Issue(t, "Fallback with unresolvable parent is not routed", testutil.Cols{
		"assignee_type": "squad", "assignee_id": assignedSquadID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": foreignComment,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + foreignComment + "'::uuid]"),
	})
	completeWorkerReplyRun(t, workerTaskID)
	var parentID string
	dbfx.QueryRow(t, "SELECT parent_id FROM comment WHERE source_task_id = $1 AND author_id = $2", workerTaskID, workerID).Scan(&parentID)
	if parentID != foreignComment {
		t.Fatal("completion fallback was not synthesized under the foreign parent")
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'", issueID, assignedLeaderID); got != 0 {
		t.Fatalf("unresolved parent woke assigned squad leader: got %d task(s), want 0", got)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'", issueID); n != 0 {
		t.Fatalf("unroutable fallback enqueued %d task(s), want 0", n)
	}
}

// TestCompletionFallbackMentionDoesNotFanOut pins §10 Test A (GH #8719): a
// fallback body naming an unrelated agent must wake only the source
// coordinator, never the named target.
func TestCompletionFallbackMentionDoesNotFanOut(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderRuntimeID := dbfx.Runtime(t, "Fallback fanout leader runtime")
	leaderID := dbfx.Agent(t, "Fallback fanout leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback fanout worker runtime")
	workerID := dbfx.Agent(t, "Fallback fanout worker", workerRuntimeID)
	otherID := dbfx.Agent(t, "Fallback fanout other", workerRuntimeID)
	squadID := dbfx.Squad(t, "Fallback fanout squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Fallback mention must not fan out", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	completeWorkerReplyRun(t, workerTaskID)
	var fallbackID string
	dbfx.QueryRow(t, `SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID).Scan(&fallbackID)
	if fallbackID == "" {
		t.Fatal("completion fallback comment was not synthesized")
	}
	// Rewrite the synthesized body to carry an unrelated @agent mention, then
	// re-run the narrow handoff resolver: only the source coordinator may wake.
	dbfx.Exec(t, `UPDATE comment SET content = $2 WHERE id = $1`, fallbackID,
		fmt.Sprintf("Done. [@Other](mention://agent/%s) please review as well", otherID))
	stored, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(workerTaskID))
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := testHandler.Queries.GetComment(context.Background(), parseUUID(fallbackID))
	if err != nil {
		t.Fatal(err)
	}
	// Drain the coordinator task already enqueued by the completion call so
	// this dispatch proves exactly-once behavior, not a second wake.
	dbfx.Exec(t, `DELETE FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`, issueID, leaderID)
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	var parent *db.Comment
	if fallback.ParentID.Valid {
		p, err := testHandler.Queries.GetComment(context.Background(), fallback.ParentID)
		if err != nil {
			t.Fatal(err)
		}
		parent = &p
	}
	routed, provenInvalid, err := testHandler.TaskService.DispatchCompletionFallbackByLineage(context.Background(), issue, stored, fallback, parent)
	if err != nil || provenInvalid || !routed {
		t.Fatalf("mention-carrying fallback must still route to its coordinator: routed=%v invalid=%v err=%v", routed, provenInvalid, err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'", issueID, otherID); got != 0 {
		t.Fatalf("fallback mention fanned out to unrelated agent: got %d task(s), want 0", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE", issueID, leaderID); got != 1 {
		t.Fatalf("coordinator received %d task(s), want 1", got)
	}
}

// TestCompletionFallbackTerminalOriginatorNotReused pins §10 Test B (GH
// #8719): the terminal worker task's persisted human originator must not
// authorize a generic invocation of an unrelated invocable agent named in the
// fallback body.
func TestCompletionFallbackTerminalOriginatorNotReused(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderRuntimeID := dbfx.Runtime(t, "Fallback originator leader runtime")
	leaderID := dbfx.Agent(t, "Fallback originator leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback originator worker runtime")
	workerID := dbfx.Agent(t, "Fallback originator worker", workerRuntimeID)
	invocableID := dbfx.Agent(t, "Fallback originator invocable", workerRuntimeID, testutil.Cols{
		"visibility": "workspace", "permission_mode": "public_to",
	})
	dbfx.InsertNoID(t, "agent_invocation_target", testutil.Cols{
		"agent_id":    invocableID,
		"target_type": "workspace",
		"target_id":   testWorkspaceID,
	}, "agent_id = $1 AND target_type = 'workspace' AND target_id = $2", invocableID, testWorkspaceID)
	squadID := dbfx.Squad(t, "Fallback originator squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Terminal originator must not re-authorize", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	completeWorkerReplyRun(t, workerTaskID)
	var fallbackID string
	dbfx.QueryRow(t, `SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID).Scan(&fallbackID)
	if fallbackID == "" {
		t.Fatal("completion fallback comment was not synthesized")
	}
	dbfx.Exec(t, `UPDATE comment SET content = $2 WHERE id = $1`, fallbackID,
		fmt.Sprintf("Done. [@B](mention://agent/%s) please take over", invocableID))
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'", issueID, invocableID); got != 0 {
		t.Fatalf("terminal originator re-authorized unrelated agent B: got %d task(s), want 0", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_task = TRUE", issueID, leaderID); got != 1 {
		t.Fatalf("coordinator received %d task(s), want 1", got)
	}
}

// TestCompletionFallbackInvalidParentStaysFailClosed pins §10 Test D (GH
// #8719): an unresolvable lineage must wake nobody — guest, assigned, or
// worker — and the resolver must report it permanently invalid, not transient.
func TestCompletionFallbackInvalidParentStaysFailClosed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	foreignWS := dbfx.Workspace(t, "Fallback failclosed foreign workspace", "fb-8719-failclosed")
	foreignIssue := dbfx.Issue(t, "Fallback failclosed foreign issue", testutil.Cols{"workspace_id": foreignWS})
	foreignComment := dbfx.Comment(t, foreignIssue, "foreign delegation", testutil.Cols{"workspace_id": foreignWS})
	workerID := dbfx.Agent(t, "Fallback failclosed worker", testRuntimeID)
	assignedLeaderID := dbfx.Agent(t, "Fallback failclosed assigned leader", testRuntimeID)
	assignedSquadID := dbfx.Squad(t, "Fallback failclosed assigned squad", assignedLeaderID)
	issueID := dbfx.Issue(t, "Invalid lineage stays fail-closed", testutil.Cols{
		"assignee_type": "squad", "assignee_id": assignedSquadID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": foreignComment,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + foreignComment + "'::uuid]"),
	})
	completeWorkerReplyRun(t, workerTaskID)
	var fallbackID string
	dbfx.QueryRow(t, "SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2", workerTaskID, workerID).Scan(&fallbackID)
	if fallbackID == "" {
		t.Fatal("completion fallback comment was not synthesized")
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'", issueID); got != 0 {
		t.Fatalf("invalid lineage enqueued %d task(s), want 0", got)
	}
	stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(workerTaskID))
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := testHandler.Queries.GetComment(ctx, parseUUID(fallbackID))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	_, provenInvalid, err := testHandler.TaskService.DispatchCompletionFallbackByLineage(ctx, issue, stored, fallback, nil)
	if err != nil || !provenInvalid {
		t.Fatalf("invalid parent must be proven-invalid: invalid=%v err=%v", provenInvalid, err)
	}
	routed, _, err := testHandler.dispatchCompletionFallback(ctx, &stored, parseUUID(fallbackID))
	if err != nil || routed {
		t.Fatalf("fail-closed replay must not route: routed=%v err=%v", routed, err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'", issueID); got != 0 {
		t.Fatalf("fail-closed replay enqueued %d task(s), want 0", got)
	}
}

// TestCompletionFallbackCrossTaskReparseBlocked pins review blocker 1 (GH
// #8719): when another agent B completes later on the same thread, B's
// completion reconcile must NOT re-parse W's fallback body as a fresh generic
// comment — even when the fallback names B with an explicit @mention and B's
// originator could invoke the mention target. The fallback is owned solely by
// the narrow handoff path (and the sweeper replay), never by generic mention
// fan-out from any completing task.
func TestCompletionFallbackCrossTaskReparseBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderRuntimeID := dbfx.Runtime(t, "Fallback cross leader runtime")
	leaderID := dbfx.Agent(t, "Fallback cross leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback cross worker runtime")
	workerID := dbfx.Agent(t, "Fallback cross worker", workerRuntimeID)
	otherRuntimeID := dbfx.Runtime(t, "Fallback cross other runtime")
	otherID := dbfx.Agent(t, "Fallback cross other", otherRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	squadID := dbfx.Squad(t, "Fallback cross squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Fallback cross-task reparse stays blocked", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	// B already runs on the same thread BEFORE W completes, so W's fallback
	// lands inside B's reconciliation window (created after B, same thread).
	// Creating B after W completes would exclude the fallback on the time
	// filter and let this test pass vacuously, even without the guard.
	otherTaskID := dbfx.Task(t, otherID, testutil.Cols{
		"runtime_id": otherRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	completeWorkerReplyRun(t, workerTaskID)
	var fallbackID string
	dbfx.QueryRow(t, `SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID).Scan(&fallbackID)
	if fallbackID == "" {
		t.Fatal("completion fallback comment was not synthesized")
	}
	// Rewrite the fallback body to mention an unrelated agent B explicitly.
	dbfx.Exec(t, `UPDATE comment SET content = $2 WHERE id = $1`, fallbackID,
		fmt.Sprintf("Done. [@B](mention://agent/%s) please take over", otherID))
	// Prove the test is not vacuous: W's fallback must be in B's
	// reconcilable set, so B's reconcile pass actually sees it.
	bTask, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(otherTaskID))
	if err != nil {
		t.Fatal(err)
	}
	bPlanned := append([]pgtype.UUID{}, bTask.CoalescedCommentIds...)
	if bTask.TriggerCommentID.Valid {
		bPlanned = append(bPlanned, bTask.TriggerCommentID)
	}
	bReconcilable, err := testHandler.Queries.ListReconcilableCommentsForIssueSince(ctx, db.ListReconcilableCommentsForIssueSinceParams{
		CommentThreadID:   bTask.CommentThreadID,
		IssueID:           bTask.IssueID,
		Since:             bTask.CreatedAt,
		PlannedCommentIds: bPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	bSeesFallback := false
	for _, rc := range bReconcilable {
		if uuidToString(rc.ID) == fallbackID {
			bSeesFallback = true
			break
		}
	}
	if !bSeesFallback {
		t.Fatal("W fallback is not in B's reconcilable set: cross-task guard would pass vacuously")
	}
	// B completes: its reconcile pass must not re-parse W's fallback body
	// into a fresh invocation of B.
	completeWorkerReplyRun(t, otherTaskID)
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, otherID); got != 0 {
		t.Fatalf("cross-task reconcile re-parsed fallback mention: got %d runnable B task(s), want 0", got)
	}
	// The fallback still belongs to W's coordinator handoff, not to B.
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory') AND is_leader_task = TRUE", issueID, leaderID); got != 1 {
		t.Fatalf("coordinator handoff lost during cross-task completion: got %d, want 1", got)
	}
}

// TestCompletionFallbackReplayIsIdempotent pins §10 Test E (GH #8719):
// dispatching the same fallback obligation twice must leave exactly one
// runnable coordinator task, and a completion-callback replay must duplicate
// neither the fallback comment nor the coordinator task.
func TestCompletionFallbackReplayIsIdempotent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderRuntimeID := dbfx.Runtime(t, "Fallback idempotent leader runtime")
	leaderID := dbfx.Agent(t, "Fallback idempotent leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback idempotent worker runtime")
	workerID := dbfx.Agent(t, "Fallback idempotent worker", workerRuntimeID)
	squadID := dbfx.Squad(t, "Fallback idempotent squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Fallback replay stays idempotent", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	completeWorkerReplyRun(t, workerTaskID)
	var fallbackID string
	dbfx.QueryRow(t, `SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID).Scan(&fallbackID)
	if fallbackID == "" {
		t.Fatal("completion fallback comment was not synthesized")
	}
	stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(workerTaskID))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := testHandler.dispatchCompletionFallback(ctx, &stored, parseUUID(fallbackID)); err != nil {
			t.Fatalf("replay %d failed: %v", i, err)
		}
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 1 {
		t.Fatalf("coordinator has %d runnable task(s), want 1", got)
	}
	completeWorkerReplyRun(t, workerTaskID)
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID); got != 1 {
		t.Fatalf("callback replay duplicated fallback comment: got %d, want 1", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 1 {
		t.Fatalf("callback replay duplicated coordinator task: got %d, want 1", got)
	}
}

// TestCompletionFallbackTransientHandoffFailureIsRecoverable pins §10 Test C
// (GH #8719): a failed first route leaves the persisted fallback replayable,
// and completion reconciliation eventually delivers it to one coordinator.
func TestCompletionFallbackTransientHandoffFailureIsRecoverable(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderRuntimeID := dbfx.Runtime(t, "Fallback recovery leader runtime")
	leaderID := dbfx.Agent(t, "Fallback recovery leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback recovery worker runtime")
	workerID := dbfx.Agent(t, "Fallback recovery worker", workerRuntimeID)
	squadID := dbfx.Squad(t, "Fallback recovery squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Transient fallback handoff failure is recoverable", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) do the work", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	completed, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || !fallbackID.Valid {
		t.Fatalf("complete worker task: transitioned=%v fallback_valid=%v err=%v", transitioned, fallbackID.Valid, err)
	}

	// Cancel only the first routing attempt after completion committed. This is
	// an injected transient DB error; the fallback row must remain the durable
	// obligation and the retryable error must not be classified as invalid.
	failedCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, provenInvalid, err := testHandler.dispatchCompletionFallback(failedCtx, completed, fallbackID); err == nil || provenInvalid {
		t.Fatalf("first route attempt = (invalid=%v, err=%v), want transient error", provenInvalid, err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM comment WHERE id = $1", uuidToString(fallbackID)); got != 1 {
		t.Fatalf("fallback comment after failed route: got %d rows, want 1", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 0 {
		t.Fatalf("failed route created %d coordinator task(s), want 0", got)
	}

	// Fail the same-request reconcile too, then prove the post-request sweeper
	// replay — not the in-request path — delivers the handoff. This pins
	// review blocker 2: after HTTP success with every in-request attempt down,
	// a real later replay must still exist.
	if _, provenInvalid, err := testHandler.dispatchCompletionFallback(failedCtx, completed, fallbackID); err == nil || provenInvalid {
		t.Fatalf("second route attempt = (invalid=%v, err=%v), want transient error", provenInvalid, err)
	}
	testHandler.reconcileCommentsOnCompletion(failedCtx, completed)
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 0 {
		t.Fatalf("failed same-request reconcile created %d coordinator task(s), want 0", got)
	}
	result, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10)
	if err != nil {
		t.Fatalf("sweeper replay failed: %v", err)
	}
	if result.Replayed != 1 {
		t.Fatalf("sweeper replayed %d fallback(s), want 1 (scanned=%d)", result.Replayed, result.Scanned)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 1 {
		t.Fatalf("sweeper replay left %d runnable coordinator task(s), want exactly 1", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM comment WHERE id = $1", uuidToString(fallbackID)); got != 1 {
		t.Fatalf("fallback comment after recovery: got %d rows, want 1", got)
	}

	next := claimWorkerReplyRun(t, leaderRuntimeID)
	if next == nil || !next.IsLeaderTask || !slices.Contains(next.DeliveredCommentIDs, uuidToString(fallbackID)) {
		t.Fatalf("recovery did not deliver the fallback to the coordinator: task=%+v", next)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 1 {
		t.Fatalf("recovery created %d runnable coordinator task(s), want exactly 1", got)
	}
	if state := completionFallbackState(t, workerTaskID); state != "recorded" {
		t.Fatalf("recovered fallback state = %q, want recorded", state)
	}
}

// TestCompletionFallbackExplicitReplyUnchanged pins §10 Test F (GH #8719): the
// explicit worker reply path keeps its normal routing and still wakes the
// leader exactly once.
func TestCompletionFallbackExplicitReplyUnchanged(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	leaderRuntimeID := dbfx.Runtime(t, "Fallback explicit leader runtime")
	leaderID := dbfx.Agent(t, "Fallback explicit leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback explicit worker runtime")
	workerID := dbfx.Agent(t, "Fallback explicit worker", workerRuntimeID)
	squadID := dbfx.Squad(t, "Fallback explicit squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Explicit worker reply unchanged", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"started_at":         testutil.Raw("now()"),
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "Verification complete: all checks pass", "parent_id": rootID,
	}), "id", issueID)
	req.Header.Set("X-Agent-ID", workerID)
	req.Header.Set("X-Task-ID", workerTaskID)
	var response CommentResponse
	testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated).JSON(&response)
	_ = response
	// The review blocker-3 scenario is an explicit reply the leader ALREADY
	// handled and terminated. Model it without a raw status flip (which
	// would leave the reply uncovered and force a reconcile re-wake): claim
	// the wake the explicit reply enqueued, start it, and complete it —
	// recording the reply as delivered. The worker's later completion must
	// then produce no duplicate wake and no synthesized fallback.
	first := claimWorkerReplyRun(t, leaderRuntimeID)
	if first == nil || !first.IsLeaderTask {
		t.Fatal("explicit reply did not enqueue the first leader wake")
	}
	if _, err := testHandler.TaskService.StartTask(context.Background(), parseUUID(first.ID)); err != nil {
		t.Fatalf("start first leader run: %v", err)
	}
	completeWorkerReplyRun(t, first.ID)
	completeWorkerReplyRun(t, workerTaskID)
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID); got != 1 {
		t.Fatalf("explicit reply plus completion left %d worker comment(s), want 1 (no synthesized fallback)", got)
	}
	next := claimWorkerReplyRun(t, leaderRuntimeID)
	if next != nil {
		t.Fatalf("handled reply must not re-wake the terminated leader, got task %s", next.ID)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 0 {
		t.Fatalf("leader has %d runnable task(s) after handled completion, want 0", got)
	}
}

// TestCompletionFallbackSuppressedReplyNotReclassified pins the suppression
// counterexample (GH #8719): an explicit worker reply that suppresses the
// coordinator at creation must not be reclassified as a synthesized
// completion fallback when the worker completes — neither by completion
// reconcile nor by the sweeper replay. The mention resolves (Test F proves
// the unsuppressed twin wakes the leader), so suppression is the only
// reason no task exists at creation.
func TestCompletionFallbackSuppressedReplyNotReclassified(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderRuntimeID := dbfx.Runtime(t, "Fallback suppressed leader runtime")
	leaderID := dbfx.Agent(t, "Fallback suppressed leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback suppressed worker runtime")
	workerID := dbfx.Agent(t, "Fallback suppressed worker", workerRuntimeID)
	squadID := dbfx.Squad(t, "Fallback suppressed squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Suppressed worker reply stays suppressed", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	// Explicit worker reply mentioning the leader, suppressed for the leader.
	req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content":            fmt.Sprintf("Done. [@Leader](mention://agent/%s) reviewed, no follow-up needed", leaderID),
		"parent_id":          rootID,
		"suppress_agent_ids": []string{leaderID},
	}), "id", issueID)
	req.Header.Set("X-Agent-ID", workerID)
	req.Header.Set("X-Task-ID", workerTaskID)
	var response CommentResponse
	testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated).JSON(&response)
	replyID := response.ID
	// Suppression honored at creation: no coordinator task.
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 0 {
		t.Fatalf("suppressed reply created %d leader task(s), want 0", got)
	}
	completeWorkerReplyRun(t, workerTaskID)
	// No synthesis: the run posted its explicit reply.
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID); got != 1 {
		t.Fatalf("worker completion left %d worker comment(s), want exactly the explicit reply", got)
	}
	// The suppressed reply must not wake the leader via fallback reclassification.
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 0 {
		t.Fatalf("suppressed reply reclassified as fallback: got %d leader task(s), want 0", got)
	}
	// Nor may the sweeper replay resurrect it.
	pending, err := testHandler.Queries.ListPendingCompletionFallbacks(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range pending {
		if uuidToString(row.FallbackID) == replyID {
			t.Fatal("suppressed reply listed as pending completion fallback")
		}
	}
	// Legacy replies may lack source_task_id; the owed-run scan must still
	// honor the same issue/agent/start-time check as completion synthesis.
	dbfx.Exec(t, `UPDATE comment SET source_task_id = NULL WHERE id = $1`, replyID)
	owed, err := testHandler.Queries.ListCompletionFallbackOwedRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range owed {
		if uuidToString(id) == workerTaskID {
			t.Fatal("explicit reply without source task listed as owed fallback")
		}
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper replay failed: %v", err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 0 {
		t.Fatalf("sweeper replayed suppressed reply: got %d leader task(s), want 0", got)
	}
}

// failRecordCompletionTxStarter begins real transactions whose
// RecordCompletionFallbackComment statement fails, standing in for any
// failure between synthesizing the fallback comment and recording its id.
type failRecordCompletionTxStarter struct {
	delegate *pgxpool.Pool
}

type failRecordCompletionTx struct {
	pgx.Tx
}

func (s *failRecordCompletionTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failRecordCompletionTx{Tx: tx}, nil
}

func (t failRecordCompletionTx) Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	if strings.Contains(query, "-- name: RecordCompletionFallbackComment") {
		return pgconn.CommandTag{}, errors.New("injected fallback record failure")
	}
	return t.Tx.Exec(ctx, query, args...)
}

// TestCompletionFallbackAtomicBoundary pins the record-failure hole (GH
// #8719): when recording the exact id fails, the synthesis transaction rolls
// back atomically (no orphan comment, no record, no wake), and the sweeper
// late synthesis recovers exactly one covered coordinator run. It does not
// depend on completion-callback replay, which early-returns on
// transitioned=false.
func TestCompletionFallbackAtomicBoundary(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderRuntimeID := dbfx.Runtime(t, "Fallback atomic leader runtime")
	leaderID := dbfx.Agent(t, "Fallback atomic leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback atomic worker runtime")
	workerID := dbfx.Agent(t, "Fallback atomic worker", workerRuntimeID)
	squadID := dbfx.Squad(t, "Fallback atomic squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Atomic fallback boundary", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) do the work", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	originalTxStarter := testHandler.TaskService.TxStarter
	testHandler.TaskService.TxStarter = &failRecordCompletionTxStarter{delegate: testPool}
	completed, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte("{\"output\":\"The delegated work is complete.\"}"), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || fallbackID.Valid {
		t.Fatalf("atomic boundary = (transitioned=%v fallback_valid=%v err=%v), want completed run with no fallback id", transitioned, fallbackID.Valid, err)
	}
	_ = completed
	// (a) atomic rollback: no orphan comment, no record, no wake.
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID); got != 0 {
		t.Fatalf("failed record left %d orphan comment(s), want 0 (rollback)", got)
	}
	stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(workerTaskID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.CompletionFallbackCommentID.Valid {
		t.Fatal("failed record left an exact id behind")
	}
	if state := completionFallbackState(t, workerTaskID); state != "pending" {
		t.Fatalf("failed synthesis left state = %q, want pending (explicit obligation for the sweeper)", state)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 0 {
		t.Fatalf("failed record created %d leader task(s), want 0", got)
	}
	testHandler.TaskService.TxStarter = originalTxStarter
	// (b) sweeper late synthesis recovers exactly one covered coordinator run.
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper recovery failed: %v", err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 1 {
		t.Fatalf("sweeper recovery left %d runnable coordinator task(s), want exactly 1", got)
	}
	var recoveredID string
	dbfx.QueryRow(t, `SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, workerID).Scan(&recoveredID)
	if recoveredID == "" {
		t.Fatal("late synthesis did not persist a fallback comment")
	}
	next := claimWorkerReplyRun(t, leaderRuntimeID)
	if next == nil || !next.IsLeaderTask || !slices.Contains(next.DeliveredCommentIDs, recoveredID) {
		t.Fatalf("recovery did not deliver the fallback to the coordinator: task=%+v", next)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 1 {
		t.Fatalf("recovery created %d runnable coordinator task(s), want exactly 1", got)
	}
}

// lockRendezvousTxStarter forces the completion-callback-vs-sweeper race to
// interleave on the run row lock (GH #8719): the callback synthesis tx
// acquires GetAgentTaskForUpdate, confirms NULL, then holds the lock while
// the sweeper blocks on the same row. Releasing the callback lets it commit;
// the sweeper then replays the recorded winner instead of inserting again.
// The release gate is an observed pg_stat_activity lock wait pinned to both
// backend PIDs and the same run row, never a timer or an unlinked waiter
// count: waitSweeperRowLockWait must see the sweeper PID blocked by the
// callback PID first.
type lockRendezvousTxStarter struct {
	delegate       *pgxpool.Pool
	started        chan struct{}
	release        chan struct{}
	calls          atomic.Int32
	timedOut       atomic.Bool
	mu             sync.Mutex
	callbackPID    int32
	sweeperPID     int32
	callbackTaskID pgtype.UUID
	sweeperTaskID  pgtype.UUID
}

// recordSynthesisCaller pins one GetAgentTaskForUpdate call to its backend
// PID and target run. The first call is the callback (it runs before the
// sweeper starts); the second is the sweeper. PIDs come from
// pg_backend_pid() on the caller's own transaction, so the release gate can
// require the exact sweeper-PID-blocked-by-callback-PID edge.
func (s *lockRendezvousTxStarter) recordSynthesisCaller(pid int32, taskID pgtype.UUID) {
	if pid == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.callbackPID == 0 {
		s.callbackPID = pid
		s.callbackTaskID = taskID
	} else if s.sweeperPID == 0 {
		s.sweeperPID = pid
		s.sweeperTaskID = taskID
	}
}

func (s *lockRendezvousTxStarter) pids() (sweeperPID, callbackPID int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sweeperPID, s.callbackPID
}

// sameRunLocked reports whether both synthesizers targeted the expected
// worker run. Call only after the gate observed the sweeper blocked, so both
// records are present; anything unset or mismatched fails the test.
func (s *lockRendezvousTxStarter) sameRunLocked(workerTaskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.callbackTaskID.Valid || !s.sweeperTaskID.Valid {
		return false
	}
	return uuidToString(s.callbackTaskID) == workerTaskID && uuidToString(s.sweeperTaskID) == workerTaskID
}

func (s *lockRendezvousTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &lockRendezvousTx{Tx: tx, starter: s}, nil
}

type lockRendezvousTx struct {
	pgx.Tx
	starter *lockRendezvousTxStarter
}

func (t *lockRendezvousTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if !strings.Contains(sql, "GetAgentTaskForUpdate") {
		return t.Tx.QueryRow(ctx, sql, args...)
	}
	// Pin this synthesis call to its backend PID and target run before it can
	// block: pg_backend_pid() runs on the caller's own transaction, so the
	// release gate can later demand the exact sweeper-blocked-by-callback edge
	// instead of counting any waiter on the shared database.
	var pid int32
	if err := t.Tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err == nil {
		var taskID pgtype.UUID
		if len(args) > 0 {
			taskID, _ = args[0].(pgtype.UUID)
		}
		t.starter.recordSynthesisCaller(pid, taskID)
	}
	if t.starter.calls.Add(1) != 1 {
		return t.Tx.QueryRow(ctx, sql, args...)
	}
	// First synthesis call (the callback): acquire the row lock and confirm
	// NULL through our own SELECT ... FOR UPDATE, then hold the lock while
	// the test drives the sweeper into it. The check names the id column
	// explicitly so later schema appends cannot silently shift it (a
	// positional Scan-dest check broke exactly that way under migration 552).
	var record pgtype.UUID
	if err := t.Tx.QueryRow(ctx, `SELECT completion_fallback_comment_id FROM agent_task_queue WHERE id = $1 FOR UPDATE`, args...).Scan(&record); err == nil && !record.Valid {
		close(t.starter.started)
		select {
		case <-t.starter.release:
		case <-time.After(30 * time.Second):
			t.starter.timedOut.Store(true)
		}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

// waitSweeperRowLockWait blocks until pg_stat_activity shows the recorded
// sweeper backend actively waiting on a lock while running the synthesis
// lock query with the recorded callback backend in its blocker set
// (pg_blocking_pids), i.e. our sweeper is queued on the row our callback
// holds. It then asserts both synthesizers targeted the same worker run.
// A stray waiter on the shared database can never satisfy the pinned PIDs,
// and lookup errors, timeouts, and PID/task mismatches fail instead of
// releasing early, so the callback release can never precede the proven
// same-row lock wait.
func waitSweeperRowLockWait(t *testing.T, ctx context.Context, starter *lockRendezvousTxStarter, workerTaskID string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		sweeperPID, callbackPID := starter.pids()
		if sweeperPID != 0 && callbackPID != 0 {
			var waiting int
			if err := testPool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND pid = $1 AND state = 'active' AND wait_event_type = 'Lock' AND query LIKE '%GetAgentTaskForUpdate%' AND $2 = ANY (pg_blocking_pids(pid))`, sweeperPID, callbackPID).Scan(&waiting); err != nil {
				t.Fatalf("poll sweeper row-lock wait: %v", err)
			}
			if waiting > 0 {
				if !starter.sameRunLocked(workerTaskID) {
					t.Fatal("synthesis callers did not target the same worker run")
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("sweeper never blocked on the contended run row lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCompletionFallbackSingleWinner pins the synthesis race (GH #8719): when
// the completion callback and the sweeper late synthesis target the same
// unrecorded run at once, row-level serialization elects exactly one winner.
// One fallback comment, one recorded id, one coordinator task, one delivery —
// the loser inserts nothing and replays the winner.
func TestCompletionFallbackSingleWinner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderRuntimeID := dbfx.Runtime(t, "Fallback winner leader runtime")
	leaderID := dbfx.Agent(t, "Fallback winner leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Fallback winner worker runtime")
	workerID := dbfx.Agent(t, "Fallback winner worker", workerRuntimeID)
	squadID := dbfx.Squad(t, "Fallback winner squad", leaderID)
	dbfx.SquadMember(t, squadID, "agent", workerID)
	issueID := dbfx.Issue(t, "Single fallback winner", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
	})
	sourceID := dbfx.Task(t, leaderID, testutil.Cols{
		"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) do the work", workerID), testutil.Cols{
		"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
	})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	starter := &lockRendezvousTxStarter{
		delegate: testPool,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	originalTxStarter := testHandler.TaskService.TxStarter
	testHandler.TaskService.TxStarter = starter
	defer func() { testHandler.TaskService.TxStarter = originalTxStarter }()
	done := make(chan int, 1)
	go func() {
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+workerTaskID+"/complete", map[string]any{"output": "Processed the inputs delivered to this run"}, testWorkspaceID, "worker-reply-handoff")
		req = withURLParam(req, "taskId", workerTaskID)
		rec := httptest.NewRecorder()
		testHandler.CompleteTask(rec, req)
		done <- rec.Code
	}()
	select {
	case <-starter.started:
	case <-time.After(30 * time.Second):
		t.Fatal("completion synthesis never acquired the row lock")
	}
	// The callback holds the run row lock (NULL confirmed) while the sweeper
	// blocks on the same row; releasing the callback lets it commit first so
	// the sweeper must replay the recorded winner.
	sweeperDone := make(chan error, 1)
	go func() {
		_, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10)
		sweeperDone <- err
	}()
	// Release only after the sweeper is observably queued on the same row:
	// waitSweeperRowLockWait demands the recorded sweeper PID blocked by the
	// recorded callback PID plus the same target run. The 20ms pacing is poll
	// granularity only; the release gate is the observed blocker edge, so a
	// timeout fails loudly instead of degenerating to a sequential run.
	waitSweeperRowLockWait(t, ctx, starter, workerTaskID)
	close(starter.release)
	select {
	case err := <-sweeperDone:
		if err != nil {
			t.Fatalf("concurrent sweeper run failed: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("sweeper did not return after the winner committed")
	}
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("concurrent completion callback status = %d, want 200", code)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("completion callback did not return after release")
	}
	if starter.timedOut.Load() {
		t.Fatal("rendezvous timed out: race did not interleave as designed")
	}
	// Exactly one winner: one comment, one recorded id, one coordinator task.
	var winnerID string
	dbfx.QueryRow(t, "SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2", workerTaskID, workerID).Scan(&winnerID)
	if winnerID == "" {
		t.Fatal("no fallback comment survived the race")
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2", workerTaskID, workerID); got != 1 {
		t.Fatalf("race left %d fallback comment(s), want exactly 1", got)
	}
	stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(workerTaskID))
	if err != nil {
		t.Fatal(err)
	}
	if !stored.CompletionFallbackCommentID.Valid || uuidToString(stored.CompletionFallbackCommentID) != winnerID {
		t.Fatal("recorded id does not match the single fallback comment")
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 1 {
		t.Fatalf("race left %d runnable coordinator task(s), want exactly 1", got)
	}
	next := claimWorkerReplyRun(t, leaderRuntimeID)
	if next == nil || !next.IsLeaderTask || !slices.Contains(next.DeliveredCommentIDs, winnerID) {
		t.Fatalf("race recovery did not deliver the fallback to the coordinator: task=%+v", next)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, leaderID); got != 1 {
		t.Fatalf("race recovery created %d runnable coordinator task(s), want exactly 1", got)
	}
}

// A worker progress comment wakes the leader; its final reply must also reach a
// leader run even if the first wake was claimed before the reply arrived.
func TestWorkerReplyDelivery(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, state := range []string{"queued", "dispatched", "running"} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/explicit=%t", state, explicit), func(t *testing.T) {
				ctx := context.Background()
				leaderRuntimeID := dbfx.Runtime(t, "Worker handoff leader runtime")
				leaderID := dbfx.Agent(t, "Worker handoff leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
				workerRuntimeID := dbfx.Runtime(t, "Worker handoff worker runtime")
				workerID := dbfx.Agent(t, "Worker handoff worker", workerRuntimeID)
				squadID := dbfx.Squad(t, "Worker handoff squad", leaderID)
				dbfx.SquadMember(t, squadID, "agent", workerID)
				issueID := dbfx.Issue(t, "Worker results must reach the coordinator", testutil.Cols{
					"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
				})
				sourceID := dbfx.Task(t, leaderID, testutil.Cols{
					"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
					"is_leader_task": true, "squad_id": squadID,
					"originator_user_id": testUserID, "accountable_user_id": testUserID,
				})
				rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", workerID), testutil.Cols{
					"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
				})
				workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
					"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
					"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
					"originator_user_id": testUserID, "accountable_user_id": testUserID,
				})
				post := func(content string) CommentResponse {
					req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
						"content": content, "parent_id": rootID,
					}), "id", issueID)
					req.Header.Set("X-Agent-ID", workerID)
					req.Header.Set("X-Task-ID", workerTaskID)
					var response CommentResponse
					testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated).JSON(&response)
					if response.AuthorType != "agent" || response.SourceTaskID == nil || *response.SourceTaskID != workerTaskID {
						t.Fatal("worker reply lost its authenticated source task")
					}
					return response
				}
				progress := post("Verification started; final results will follow in this thread")
				var leaderTaskID string
				dbfx.QueryRow(t, `SELECT id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`, issueID, leaderID).Scan(&leaderTaskID)
				var first *AgentTaskResponse
				if state != "queued" {
					first = claimWorkerReplyRun(t, leaderRuntimeID)
					if first == nil || first.ID != leaderTaskID || !slices.Contains(first.DeliveredCommentIDs, progress.ID) {
						t.Fatal("first leader run did not receive the worker's progress")
					}
				}
				if state == "running" {
					if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(leaderTaskID)); err != nil {
						t.Fatal(err)
					}
				}
				content := "Verification complete: found a correctness issue; please coordinate the repair"
				if explicit {
					content = fmt.Sprintf("[@Squad](mention://squad/%s) %s", squadID, content)
				}
				details := post("Evidence: the final reply can arrive after the leader claims its inputs")
				result := post(content)
				stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(leaderTaskID))
				if err != nil {
					t.Fatal(err)
				}
				if state == "dispatched" && !explicit {
					for _, id := range []string{details.ID, result.ID} {
						if !slices.Contains(stored.CoalescedCommentIds, parseUUID(id)) || slices.Contains(stored.DeliveredCommentIds, parseUUID(id)) {
							t.Fatal("accepted worker reply must be planned without changing the earlier delivery receipt")
						}
					}
					if stored.TriggerCommentID != parseUUID(progress.ID) {
						t.Fatal("registering a worker reply must not replace an already claimed trigger")
					}
				}
				if state == "queued" {
					first = claimWorkerReplyRun(t, leaderRuntimeID)
					if first == nil || first.ID != leaderTaskID || !slices.Contains(first.DeliveredCommentIDs, result.ID) || !slices.Contains(first.DeliveredCommentIDs, details.ID) || first.TriggerCommentContent != content {
						t.Fatal("queued leader must coalesce and receive the worker result")
					}
				} else if slices.Contains(first.DeliveredCommentIDs, result.ID) {
					t.Fatal("an earlier claim cannot have delivered a later comment")
				}
				if state != "running" {
					if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(leaderTaskID)); err != nil {
						t.Fatal(err)
					}
				}
				if other := claimWorkerReplyRun(t, leaderRuntimeID); other != nil {
					t.Fatal("same issue/leader must remain serialized")
				}
				completeWorkerReplyRun(t, leaderTaskID)
				next := claimWorkerReplyRun(t, leaderRuntimeID)
				if state == "queued" {
					if next != nil {
						t.Fatal("already delivered result must not create another leader run")
					}
					return
				}
				if next == nil {
					t.Fatal("worker result persisted but was neither delivered nor followed by another leader run")
				}
				if !next.IsLeaderTask || !slices.Contains(next.DeliveredCommentIDs, result.ID) || !slices.Contains(next.DeliveredCommentIDs, details.ID) || next.TriggerCommentContent != content {
					t.Fatal("follow-up must deliver the result in the squad leader role")
				}
				successor, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(next.ID))
				if err != nil {
					t.Fatal(err)
				}
				if successor.SquadID != parseUUID(squadID) || successor.OriginatorUserID != parseUUID(testUserID) || successor.AccountableUserID != parseUUID(testUserID) || successor.DelegatedFromTaskID != parseUUID(workerTaskID) {
					t.Fatal("worker follow-up lost squad or human delegation provenance")
				}
				if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(next.ID)); err != nil {
					t.Fatal(err)
				}
				completeWorkerReplyRun(t, next.ID)
				if another := claimWorkerReplyRun(t, leaderRuntimeID); another != nil {
					t.Fatal("worker result must not generate an endless leader follow-up loop")
				}
			})
		}
	}
}

// Unplanned replies must be in the completing run's thread and timestamp window:
// otherwise a passing negative case could merely be the SQL scope excluding it.
func TestWorkerReplyReconcileBoundaries(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, mode := range []string{"accepted", "unplanned", "suppressed", "leader_reply", "note", "archived_leader", "reassigned", "registration_failure", "completed_before_registration"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := dbfx.Runtime(t, "Worker replay boundary leader")
			leaderID := dbfx.Agent(t, "Worker replay boundary leader", runtimeID)
			workerRuntimeID := dbfx.Runtime(t, "Worker replay boundary worker")
			workerID := dbfx.Agent(t, "Worker replay boundary worker", workerRuntimeID)
			squadID := dbfx.Squad(t, "Worker replay boundary squad", leaderID)
			dbfx.SquadMember(t, squadID, "agent", workerID)
			issueID := dbfx.Issue(t, "Worker replay boundary", testutil.Cols{
				"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
			})
			rootID := dbfx.Comment(t, issueID, "Coordinate this work", testutil.Cols{"created_at": testutil.Raw("now() - interval '6 minutes'")})
			taskID := dbfx.Task(t, leaderID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "status": "queued",
				"trigger_comment_id": rootID, "is_leader_task": true, "squad_id": squadID,
				"originator_user_id": testUserID, "accountable_user_id": testUserID,
				"created_at": testutil.Raw("now() - interval '5 minutes'"),
			})
			workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
				"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
				"trigger_comment_id": rootID, "squad_id": squadID,
				"originator_user_id": testUserID, "accountable_user_id": testUserID,
			})
			first := claimWorkerReplyRun(t, runtimeID)
			if first == nil || first.ID != taskID {
				t.Fatal("leader run was not claimed")
			}
			var replyID string
			if mode == "unplanned" {
				replyID = dbfx.Comment(t, issueID, "An ordinary reply with no accepted dispatch", testutil.Cols{
					"parent_id": rootID, "author_type": "agent", "author_id": workerID, "source_task_id": workerTaskID,
				})
			} else {
				body := map[string]any{"content": "The worker has results", "parent_id": rootID}
				if mode == "note" {
					body["content"] = "/note an informational update"
				}
				if mode == "suppressed" {
					body["suppress_agent_ids"] = []string{leaderID}
				}
				req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body), "id", issueID)
				req.Header.Set("X-Agent-ID", workerID)
				req.Header.Set("X-Task-ID", workerTaskID)
				if mode == "leader_reply" {
					req.Header.Set("X-Agent-ID", leaderID)
					req.Header.Set("X-Task-ID", taskID)
				}
				var response CommentResponse
				h := *testHandler
				registration := &workerReplyRegistrationDB{DBTX: testPool}
				if mode == "registration_failure" {
					registration.err = errors.New("injected worker reply registration failure")
				}
				if mode == "completed_before_registration" {
					registration.before = func() {
						if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(taskID)); err != nil {
							t.Fatal(err)
						}
						completeWorkerReplyRun(t, taskID)
					}
				}
				if mode == "registration_failure" || mode == "completed_before_registration" {
					h.Queries = db.New(registration)
				}
				testutil.Call(t, h.CreateComment, req).Want(http.StatusCreated).JSON(&response)
				if (mode == "registration_failure" || mode == "completed_before_registration") && registration.calls != 1 {
					t.Fatalf("registration probe called %d times", registration.calls)
				}
				replyID = response.ID
			}
			task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			comments, err := testHandler.Queries.ListReconcilableCommentsForIssueSince(ctx, db.ListReconcilableCommentsForIssueSinceParams{
				IssueID: task.IssueID, CommentThreadID: task.CommentThreadID, Since: task.CreatedAt,
				PlannedCommentIds: task.CoalescedCommentIds,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(comments, func(c db.Comment) bool { return c.ID == parseUUID(replyID) }) {
				t.Fatal("reply must reach the replay filter, not be excluded by the SQL thread/time window")
			}
			wantPlanned := mode == "accepted" || mode == "archived_leader" || mode == "reassigned"
			if slices.Contains(task.CoalescedCommentIds, parseUUID(replyID)) != wantPlanned {
				t.Fatalf("unexpected creation-time obligation for %s", mode)
			}
			if mode == "archived_leader" {
				dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, leaderID)
			}
			if mode == "reassigned" {
				dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, workerID)
			}
			if mode != "completed_before_registration" {
				if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(taskID)); err != nil {
					t.Fatal(err)
				}
				completeWorkerReplyRun(t, taskID)
			}
			queued := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, issueID)
			if mode != "accepted" && mode != "completed_before_registration" {
				if queued != 0 {
					t.Fatalf("%s must not create a completion-driven run, got %d", mode, queued)
				}
				return
			}
			if queued != 1 {
				t.Fatalf("accepted reply must create exactly one successor, got %d", queued)
			}
			next := claimWorkerReplyRun(t, runtimeID)
			if next == nil || !slices.Contains(next.DeliveredCommentIDs, replyID) {
				t.Fatal("accepted reply was not delivered")
			}
			if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(next.ID)); err != nil {
				t.Fatal(err)
			}
			completeWorkerReplyRun(t, next.ID)
			if extra := claimWorkerReplyRun(t, runtimeID); extra != nil {
				t.Fatal("delivered worker reply replayed again")
			}
		})
	}
}

// If completion commits while registration waits for the task row lock, the
// caller must see a miss and enqueue fresh, not attach an obligation to a run
// whose completion snapshot can no longer include it.
func TestWorkerReplyRegistrationLosesCompletionRace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtimeID := dbfx.Runtime(t, "Worker registration race")
	agentID := dbfx.Agent(t, "Worker registration race", runtimeID)
	issueID := dbfx.Issue(t, "Worker registration race")
	rootID := dbfx.Comment(t, issueID, "Initial input")
	replyID := dbfx.Comment(t, issueID, "Worker result", testutil.Cols{"parent_id": rootID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running", "trigger_comment_id": rootID,
	})
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error(err)
		}
	}()
	if _, err := tx.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskID); err != nil {
		t.Fatal(err)
	}
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	pid := conn.Conn().PgConn().PID()
	done := make(chan struct{})
	var registrationErr error
	go func() {
		_, registrationErr = db.New(conn).RegisterPlannedCommentForActiveTask(ctx, db.RegisterPlannedCommentForActiveTaskParams{
			IssueID: parseUUID(issueID), AgentID: parseUUID(agentID), CommentID: parseUUID(replyID),
		})
		close(done)
	}()
	// Stop the registration query before returning its connection to the pool,
	// including assertion failures while it is waiting for the task row lock.
	defer func() { cancel(); <-done }()
	for {
		var blocked bool
		if err := testPool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("registration did not block on the completing row")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	if !errors.Is(registrationErr, pgx.ErrNoRows) {
		t.Fatalf("registration after completion = %v, want no rows", registrationErr)
	}
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(task.CoalescedCommentIds, parseUUID(replyID)) {
		t.Fatal("completed run acquired an undeliverable obligation")
	}
}

// Intercept only the new obligation write; all routing, comment persistence,
// completion, and successor enqueue still use their real database paths.
type workerReplyRegistrationDB struct {
	db.DBTX
	before func()
	err    error
	calls  int
}

// fallbackObligationFixture builds one leader + worker + squad + squad-assigned
// issue + completed leader source task + delegation root comment for the GH
// #8719 obligation-state tests.
type fallbackObligationFixture struct {
	leaderRuntimeID, workerRuntimeID                       string
	leaderID, workerID, squadID, issueID, sourceID, rootID string
}

func newFallbackObligationFixture(t *testing.T, name string) fallbackObligationFixture {
	t.Helper()
	var fx fallbackObligationFixture
	fx.leaderRuntimeID = dbfx.Runtime(t, "Fallback obligation leader runtime "+name)
	fx.leaderID = dbfx.Agent(t, "Fallback obligation leader "+name, fx.leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	fx.workerRuntimeID = dbfx.Runtime(t, "Fallback obligation worker runtime "+name)
	fx.workerID = dbfx.Agent(t, "Fallback obligation worker "+name, fx.workerRuntimeID)
	fx.squadID = dbfx.Squad(t, "Fallback obligation squad "+name, fx.leaderID)
	dbfx.SquadMember(t, fx.squadID, "agent", fx.workerID)
	fx.issueID = dbfx.Issue(t, "Fallback obligation "+name, testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": fx.squadID,
	})
	fx.sourceID = dbfx.Task(t, fx.leaderID, testutil.Cols{
		"runtime_id": fx.leaderRuntimeID, "issue_id": fx.issueID, "status": "completed",
		"is_leader_task": true, "squad_id": fx.squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	fx.rootID = dbfx.Comment(t, fx.issueID, fmt.Sprintf("[@Worker](mention://agent/%s) do the work "+name, fx.workerID), testutil.Cols{
		"author_type": "agent", "author_id": fx.leaderID, "source_task_id": fx.sourceID,
	})
	return fx
}

// completionFallbackState reads the durable obligation state of a worker run:
// "" for legacy/NULL rows, otherwise pending/recorded/settled.
func completionFallbackState(t *testing.T, taskID string) string {
	t.Helper()
	var state *string
	dbfx.QueryRow(t, `SELECT completion_fallback_state FROM agent_task_queue WHERE id = $1`, taskID).Scan(&state)
	if state == nil {
		return ""
	}
	return *state
}

func (d *workerReplyRegistrationDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "-- name: RegisterPlannedCommentForActiveTask :one") {
		d.calls++
		if d.before != nil {
			d.before()
		}
		if d.err != nil {
			return errRow{err: d.err}
		}
	}
	return d.DBTX.QueryRow(ctx, query, args...)
}

// fallbackLineageFixture builds two squads (A led by leaderA, B led by
// leaderB), a worker member of A, and an issue assigned to assigneeSquad
// ("A" or "B") plus a completed leader source task of squad A, for the GH
// #8719 assigned-lineage proof tests.
type fallbackLineageFixture struct {
	leaderARuntime, workerRuntime, leaderBRuntime              string
	leaderA, worker, leaderB, squadA, squadB, issueID, sourceA string
}

func newFallbackLineageFixture(t *testing.T, name, assignee string) fallbackLineageFixture {
	t.Helper()
	var fx fallbackLineageFixture
	fx.leaderARuntime = dbfx.Runtime(t, "Fallback lineage A leader runtime "+name)
	fx.leaderA = dbfx.Agent(t, "Fallback lineage A leader "+name, fx.leaderARuntime, testutil.Cols{"max_concurrent_tasks": 3})
	fx.workerRuntime = dbfx.Runtime(t, "Fallback lineage worker runtime "+name)
	fx.worker = dbfx.Agent(t, "Fallback lineage worker "+name, fx.workerRuntime)
	fx.leaderBRuntime = dbfx.Runtime(t, "Fallback lineage B leader runtime "+name)
	fx.leaderB = dbfx.Agent(t, "Fallback lineage B leader "+name, fx.leaderBRuntime, testutil.Cols{"max_concurrent_tasks": 3})
	fx.squadA = dbfx.Squad(t, "Fallback lineage squad A "+name, fx.leaderA)
	dbfx.SquadMember(t, fx.squadA, "agent", fx.worker)
	fx.squadB = dbfx.Squad(t, "Fallback lineage squad B "+name, fx.leaderB)
	assigneeID := fx.squadA
	if assignee == "B" {
		assigneeID = fx.squadB
	}
	fx.issueID = dbfx.Issue(t, "Fallback lineage "+name, testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": assigneeID,
	})
	fx.sourceA = dbfx.Task(t, fx.leaderA, testutil.Cols{
		"runtime_id": fx.leaderARuntime, "issue_id": fx.issueID, "status": "completed",
		"is_leader_task": true, "squad_id": fx.squadA,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	return fx
}

func runnableTaskCount(t *testing.T, issueID, agentID string) int {
	t.Helper()
	return dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID, agentID)
}

func createFallbackWorkerRun(t *testing.T, fx fallbackObligationFixture) string {
	t.Helper()
	return dbfx.Task(t, fx.workerID, testutil.Cols{
		"runtime_id": fx.workerRuntimeID, "issue_id": fx.issueID, "status": "running",
		"trigger_comment_id": fx.rootID, "squad_id": fx.squadID, "delegated_from_task_id": fx.sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + fx.rootID + "'::uuid]"),
	})
}

func completeFallbackWithoutDispatch(t *testing.T, workerTaskID string) pgtype.UUID {
	t.Helper()
	_, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		context.Background(), parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || !fallbackID.Valid {
		t.Fatalf("record fallback: transitioned=%v fallback=%v err=%v", transitioned, fallbackID.Valid, err)
	}
	return fallbackID
}

func TestCompletionFallbackDifferentHeadSweeperReplayDoesNotCoalesceOldRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackObligationFixture(t, "sweeper-head-fence")
	head1, head2 := strings.Repeat("a", 40), strings.Repeat("b", 40)
	prNumber := int32(500000 + time.Now().UnixNano()%400000)
	var prID string
	if err := testPool.QueryRow(ctx, `INSERT INTO github_pull_request (workspace_id, installation_id, repo_owner, repo_name, pr_number, title, state, html_url, pr_created_at, pr_updated_at, head_sha) VALUES ($1, 1, 'multica-ai', 'multica', $2, 'fallback head fence', 'open', 'https://example.test/pr', now(), now(), $3) RETURNING id`, testWorkspaceID, prNumber, head1).Scan(&prID); err != nil {
		t.Fatalf("seed linked PR: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, prID)
		testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, prID)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1, $2)`, fx.issueID, prID); err != nil {
		t.Fatalf("link PR: %v", err)
	}
	var oldTaskID string
	if err := testPool.QueryRow(ctx, `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, comment_thread_id, status, priority, is_leader_task, squad_id, context, originator_user_id, accountable_user_id) VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], $4, 'queued', 0, true, $5, jsonb_build_object('head_sha', $6::text), $7, $7) RETURNING id`, fx.leaderID, fx.leaderRuntimeID, fx.issueID, fx.rootID, fx.squadID, head1, testUserID).Scan(&oldTaskID); err != nil {
		t.Fatalf("seed old-head coordinator: %v", err)
	}
	workerTaskID := createFallbackWorkerRun(t, fx)
	fallbackID := completeFallbackWithoutDispatch(t, workerTaskID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET coalesced_comment_ids = ARRAY[$2::uuid] WHERE id = $1`, oldTaskID, fallbackID)
	dbfx.Exec(t, `UPDATE github_pull_request SET head_sha = $2, pr_updated_at = now() WHERE id = $1`, prID, head2)
	readOld := func() string {
		t.Helper()
		var snapshot string
		err := testPool.QueryRow(ctx, `SELECT jsonb_build_array(trigger_comment_id::text, coalesced_comment_ids::text, originator_user_id::text, accountable_user_id::text, originator_source::text, context->>'head_sha')::text FROM agent_task_queue WHERE id = $1`, oldTaskID).Scan(&snapshot)
		if err != nil {
			t.Fatalf("read H1 coordinator: %v", err)
		}
		return snapshot
	}
	before := readOld()
	if !strings.Contains(before, head1) {
		t.Fatalf("H1 fixture has no old head: %s", before)
	}
	_, _ = testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10)
	if after := readOld(); after != before {
		t.Fatalf("H2 fallback mutated H1 trigger/coalescing/attribution: before=%s after=%s", before, after)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND id <> $3 AND status = 'queued' AND (trigger_comment_id = $4 OR $4 = ANY(coalesced_comment_ids))`, fx.issueID, fx.leaderID, oldTaskID, fallbackID); got != 0 {
		t.Fatalf("H2 fallback was carried by %d queued task(s) while H1 owned the slot", got)
	}
	pending, err := testHandler.Queries.ListPendingCompletionFallbacks(ctx, 100)
	if err != nil {
		t.Fatalf("list pending fallbacks: %v", err)
	}
	found := false
	for _, row := range pending {
		if row.FallbackID == fallbackID {
			found = true
		}
	}
	if !found {
		t.Fatal("fallback obligation was cleared instead of staying pending behind H1")
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, oldTaskID)
	dbfx.Task(t, fx.workerID, testutil.Cols{
		"runtime_id": fx.workerRuntimeID, "issue_id": fx.issueID, "status": "queued",
		"trigger_comment_id": uuidToString(fallbackID),
		"context": testutil.Raw(`'{"head_sha":"` + head2 + `"}'::jsonb`),
	})
	pending, err = testHandler.Queries.ListPendingCompletionFallbacks(ctx, 100)
	if err != nil {
		t.Fatalf("list pending with wrong-agent H2 carrier: %v", err)
	}
	found = false
	for _, row := range pending {
		found = found || row.FallbackID == fallbackID
	}
	if !found {
		t.Fatal("wrong-agent H2 carrier hid the coordinator obligation")
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper after H1 released its slot: %v", err)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory') AND context->>'head_sha' = $3 AND (trigger_comment_id = $4 OR $4 = ANY(coalesced_comment_ids))`, fx.issueID, fx.leaderID, head2, fallbackID); got != 1 {
		t.Fatalf("H2 fallback coverage after H1 release = %d, want exactly 1", got)
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("idempotent H2 sweep: %v", err)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND context->>'head_sha' = $3 AND (trigger_comment_id = $4 OR $4 = ANY(coalesced_comment_ids))`, fx.issueID, fx.leaderID, head2, fallbackID); got != 1 {
		t.Fatalf("H2 fallback carriers after repeat sweep = %d, want exactly 1", got)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_type = 'agent' AND author_id = $2`, workerTaskID, fx.workerID); got != 1 {
		t.Fatalf("recorded fallback comments = %d, want exactly 1", got)
	}
	if after := readOld(); after != before {
		t.Fatalf("completed H1 row changed while dispatching H2: before=%s after=%s", before, after)
	}
}

func TestCompletionFallbackCallbackAndSweeperParity(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	type outcome struct {
		leader, squad, trigger, thread                   bool
		coalesced                                        bool
		originator, accountable, attributionSource, head string
	}
	outcomes := make([]outcome, 0, 2)
	for _, path := range []string{"callback", "sweeper"} {
		t.Run(path, func(t *testing.T) {
			ctx := context.Background()
			fx := newFallbackObligationFixture(t, "dispatch-parity-"+path)
			workerTaskID := createFallbackWorkerRun(t, fx)
			var fallbackID pgtype.UUID
			if path == "callback" {
				completeWorkerReplyRun(t, workerTaskID)
				worker, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(workerTaskID))
				if err != nil || !worker.CompletionFallbackCommentID.Valid {
					t.Fatalf("callback did not record fallback: err=%v", err)
				}
				fallbackID = worker.CompletionFallbackCommentID
			} else {
				fallbackID = completeFallbackWithoutDispatch(t, workerTaskID)
				if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
					t.Fatalf("sweeper dispatch: %v", err)
				}
			}
			if got := runnableTaskCount(t, fx.issueID, fx.leaderID); got != 1 {
				t.Fatalf("%s created %d coordinator tasks, want exactly 1", path, got)
			}
			var got outcome
			err := testPool.QueryRow(ctx, `
                SELECT is_leader_task, squad_id = $2::uuid, trigger_comment_id = $3::uuid,
                       $3::uuid = ANY(coalesced_comment_ids), comment_thread_id = $4::uuid,
                       COALESCE(originator_user_id::text, ''), COALESCE(accountable_user_id::text, ''),
                       COALESCE(originator_source::text, ''), COALESCE(context->>'head_sha', '')
                FROM agent_task_queue
                WHERE issue_id = $1 AND agent_id = $5 AND status = 'queued'
                  AND (trigger_comment_id = $3::uuid OR $3::uuid = ANY(coalesced_comment_ids))
            `, fx.issueID, fx.squadID, fallbackID, fx.rootID, fx.leaderID).Scan(
				&got.leader, &got.squad, &got.trigger, &got.coalesced, &got.thread,
				&got.originator, &got.accountable, &got.attributionSource, &got.head,
			)
			if err != nil {
				t.Fatalf("read %s fallback carrier: %v", path, err)
			}
			if !got.leader || !got.squad || !got.trigger || got.coalesced || !got.thread || got.originator != testUserID || got.accountable != testUserID || got.head != "" {
				t.Fatalf("%s dispatch has unexpected role/ownership/attribution: %+v", path, got)
			}
			outcomes = append(outcomes, got)
		})
	}
	if len(outcomes) != 2 || outcomes[0] != outcomes[1] {
		t.Fatalf("callback and sweeper dispatch differ: %+v", outcomes)
	}
}

func TestCompletionFallbackMediaPendingDeferredTaskCoversFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackObligationFixture(t, "media-deferred-coverage")
	workerTaskID := createFallbackWorkerRun(t, fx)
	fallbackID := completeFallbackWithoutDispatch(t, workerTaskID)
	carrierID := dbfx.Task(t, fx.leaderID, testutil.Cols{
		"runtime_id": fx.leaderRuntimeID, "issue_id": fx.issueID, "status": "deferred",
		"trigger_comment_id": uuidToString(fallbackID), "is_leader_task": true, "squad_id": fx.squadID,
		"context": testutil.Raw("'{\"channel_issue_media_pending\":true}'::jsonb"),
	})
	covered, err := testHandler.Queries.HasTaskCoveringCompletionFallback(ctx, db.HasTaskCoveringCompletionFallbackParams{
		IssueID: parseUUID(fx.issueID), AgentID: parseUUID(fx.leaderID), CommentID: fallbackID, ExcludeTaskID: parseUUID(workerTaskID),
	})
	if err != nil || !covered {
		t.Fatalf("media-pending deferred carrier coverage = %v, err=%v; want true", covered, err)
	}
	pending, err := testHandler.Queries.ListPendingCompletionFallbacks(ctx, 100)
	if err != nil {
		t.Fatalf("list pending completion fallbacks: %v", err)
	}
	for _, row := range pending {
		if row.FallbackID == fallbackID {
			t.Fatal("media-pending deferred carrier remained in pending fallback scan")
		}
	}
	_, _ = testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10)
	if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued' AND id <> $3 AND (trigger_comment_id = $4 OR $4 = ANY(coalesced_comment_ids))`, fx.issueID, fx.leaderID, carrierID, fallbackID); got != 0 {
		t.Fatalf("deferred carrier caused %d duplicate fallback dispatches", got)
	}
}

// TestCompletionFallbackReassignedSquadDoesNotWake pins Blocker 3 (GH #8719
// human spec): a worker delegated by squad A whose issue is reassigned to
// squad B must not wake B's leader on fallback recovery. The exact parent
// delegation still routes to the original delegator (guest path preserved).
func TestCompletionFallbackReassignedSquadDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackLineageFixture(t, "reassignment", "A")
	rootID := dbfx.Comment(t, fx.issueID, fmt.Sprintf("[@Worker](mention://agent/%s) do the reassigned work", fx.worker), testutil.Cols{
		"author_type": "agent", "author_id": fx.leaderA, "source_task_id": fx.sourceA,
	})
	workerTaskID := dbfx.Task(t, fx.worker, testutil.Cols{
		"runtime_id": fx.workerRuntime, "issue_id": fx.issueID, "status": "running",
		"trigger_comment_id": rootID, "squad_id": fx.squadA, "delegated_from_task_id": fx.sourceA,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + rootID + "'::uuid]"),
	})
	dbfx.Exec(t, `UPDATE issue SET assignee_id = $1 WHERE id = $2`, fx.squadB, fx.issueID)
	_, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || !fallbackID.Valid {
		t.Fatalf("reassigned completion = (transitioned=%v fallback_valid=%v err=%v), want synthesized fallback", transitioned, fallbackID.Valid, err)
	}
	if got := runnableTaskCount(t, fx.issueID, fx.leaderB); got != 0 {
		t.Fatalf("reassignment woke %d squad B leader task(s), want 0", got)
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper run failed: %v", err)
	}
	if got := runnableTaskCount(t, fx.issueID, fx.leaderB); got != 0 {
		t.Fatalf("sweeper woke %d squad B leader task(s) after reassignment, want 0", got)
	}
	if got := runnableTaskCount(t, fx.issueID, fx.leaderA); got != 1 {
		t.Fatalf("original delegator has %d runnable task(s), want exactly 1 (guest lineage preserved)", got)
	}
}

// TestCompletionFallbackForeignSourceIssueDoesNotWake pins Blocker 3: a
// DelegatedFromTaskID pointing at another issue proves nothing about this
// issue's coordinator, so no one wakes and the row settles.
func TestCompletionFallbackForeignSourceIssueDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackLineageFixture(t, "foreign source", "B")
	foreignIssueID := dbfx.Issue(t, "Fallback lineage foreign issue", testutil.Cols{"status": "in_progress"})
	foreignSourceID := dbfx.Task(t, fx.leaderA, testutil.Cols{
		"runtime_id": fx.leaderARuntime, "issue_id": foreignIssueID, "status": "completed",
		"is_leader_task": true, "squad_id": fx.squadA,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	workerTaskID := dbfx.Task(t, fx.worker, testutil.Cols{
		"runtime_id": fx.workerRuntime, "issue_id": fx.issueID, "status": "running",
		"squad_id": fx.squadA, "delegated_from_task_id": foreignSourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	_, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || !fallbackID.Valid {
		t.Fatalf("foreign-source completion = (transitioned=%v fallback_valid=%v err=%v), want synthesized fallback", transitioned, fallbackID.Valid, err)
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper run failed: %v", err)
	}
	if got := runnableTaskCount(t, fx.issueID, fx.leaderB); got != 0 {
		t.Fatalf("foreign source woke %d task(s), want 0", got)
	}
	if got := runnableTaskCount(t, fx.issueID, fx.leaderA); got != 0 {
		t.Fatalf("foreign source woke %d original-squad task(s), want 0", got)
	}
	if state := completionFallbackState(t, workerTaskID); state != "settled" {
		t.Fatalf("foreign-source row state = %q, want settled", state)
	}
}

// TestCompletionFallbackNonLeaderSourceDoesNotWake pins Blocker 3: a
// DelegatedFromTaskID pointing at a non-leader task proves no delegation
// edge, so no one wakes and the row settles.
func TestCompletionFallbackNonLeaderSourceDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackLineageFixture(t, "non-leader source", "B")
	nonLeaderSourceID := dbfx.Task(t, fx.worker, testutil.Cols{
		"runtime_id": fx.workerRuntime, "issue_id": fx.issueID, "status": "completed",
		"squad_id":           fx.squadA,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"result": testutil.Raw(`'{"output":"prior work."}'::jsonb`),
	})
	workerTaskID := dbfx.Task(t, fx.worker, testutil.Cols{
		"runtime_id": fx.workerRuntime, "issue_id": fx.issueID, "status": "running",
		"squad_id": fx.squadA, "delegated_from_task_id": nonLeaderSourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	_, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || !fallbackID.Valid {
		t.Fatalf("non-leader-source completion = (transitioned=%v fallback_valid=%v err=%v), want synthesized fallback", transitioned, fallbackID.Valid, err)
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper run failed: %v", err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status IN ('queued','dispatched','running','waiting_local_directory')", fx.issueID); got != 0 {
		t.Fatalf("non-leader source woke %d task(s), want 0", got)
	}
	if state := completionFallbackState(t, workerTaskID); state != "settled" {
		t.Fatalf("non-leader-source row state = %q, want settled", state)
	}
}

// TestCompletionFallbackSquadMismatchDoesNotWake pins Blocker 3: a source
// task whose squad differs from the current target squad proves no edge to
// this coordinator, so no one wakes and the row settles.
func TestCompletionFallbackSquadMismatchDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackLineageFixture(t, "squad mismatch", "B")
	workerTaskID := dbfx.Task(t, fx.worker, testutil.Cols{
		"runtime_id": fx.workerRuntime, "issue_id": fx.issueID, "status": "running",
		"squad_id": fx.squadA, "delegated_from_task_id": fx.sourceA,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	_, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || !fallbackID.Valid {
		t.Fatalf("mismatch completion = (transitioned=%v fallback_valid=%v err=%v), want synthesized fallback", transitioned, fallbackID.Valid, err)
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper run failed: %v", err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status IN ('queued','dispatched','running','waiting_local_directory')", fx.issueID); got != 0 {
		t.Fatalf("squad mismatch woke %d task(s), want 0", got)
	}
	if state := completionFallbackState(t, workerTaskID); state != "settled" {
		t.Fatalf("mismatch row state = %q, want settled", state)
	}
}

// TestCompletionFallbackHistoricalRowNotRecovered pins Blocker 1 (GH #8719
// human spec): a pre-migration completed delegated worker row (NULL fallback
// state) is never a recovery obligation, even though it matches the old
// NULL-record-id shape. The sweeper must create no fallback and wake nobody,
// and must leave the row untouched.
func TestCompletionFallbackHistoricalRowNotRecovered(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackObligationFixture(t, "historical row")
	workerTaskID := dbfx.Task(t, fx.workerID, testutil.Cols{
		"runtime_id": fx.workerRuntimeID, "issue_id": fx.issueID, "status": "completed",
		"trigger_comment_id": fx.rootID, "squad_id": fx.squadID, "delegated_from_task_id": fx.sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + fx.rootID + "'::uuid]"),
		"result":                testutil.Raw(`'{"output":"The delegated work is complete."}'::jsonb`),
		"completed_at":          testutil.Raw("now() - interval '30 days'"),
	})
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper run failed: %v", err)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, fx.workerID); got != 0 {
		t.Fatalf("historical row synthesized %d fallback comment(s), want 0", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", fx.issueID, fx.leaderID); got != 0 {
		t.Fatalf("historical row woke %d coordinator task(s), want 0", got)
	}
	if state := completionFallbackState(t, workerTaskID); state != "" {
		t.Fatalf("historical row state = %q, want NULL (untouched)", state)
	}
}

// TestCompletionFallbackPendingRowLateSynthesized pins the explicit pending
// obligation path (GH #8719 human spec Blocker 1): only a row the new
// completion path marked pending is a late-synthesis candidate. The sweeper
// must synthesize exactly one fallback, record its id, and wake exactly one
// covered coordinator run.
func TestCompletionFallbackPendingRowLateSynthesized(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackObligationFixture(t, "pending row")
	workerTaskID := dbfx.Task(t, fx.workerID, testutil.Cols{
		"runtime_id": fx.workerRuntimeID, "issue_id": fx.issueID, "status": "completed",
		"trigger_comment_id": fx.rootID, "squad_id": fx.squadID, "delegated_from_task_id": fx.sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + fx.rootID + "'::uuid]"),
		"result":                testutil.Raw(`'{"output":"The delegated work is complete."}'::jsonb`),
	})
	dbfx.Exec(t, `UPDATE agent_task_queue SET completion_fallback_state = 'pending' WHERE id = $1`, workerTaskID)
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper run failed: %v", err)
	}
	var fallbackID string
	dbfx.QueryRow(t, `SELECT id FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, fx.workerID).Scan(&fallbackID)
	if fallbackID == "" {
		t.Fatal("pending row was not late-synthesized")
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, fx.workerID); got != 1 {
		t.Fatalf("pending row left %d fallback comment(s), want exactly 1", got)
	}
	stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(workerTaskID))
	if err != nil {
		t.Fatal(err)
	}
	if !stored.CompletionFallbackCommentID.Valid || uuidToString(stored.CompletionFallbackCommentID) != fallbackID {
		t.Fatal("pending row did not record the synthesized fallback id")
	}
	if state := completionFallbackState(t, workerTaskID); state != "recorded" {
		t.Fatalf("pending row state = %q, want recorded", state)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", fx.issueID, fx.leaderID); got != 1 {
		t.Fatalf("pending row recovery left %d runnable coordinator task(s), want exactly 1", got)
	}
	next := claimWorkerReplyRun(t, fx.leaderRuntimeID)
	if next == nil || !next.IsLeaderTask || !slices.Contains(next.DeliveredCommentIDs, fallbackID) {
		t.Fatalf("recovery did not deliver the fallback to the coordinator: task=%+v", next)
	}
}

// TestCompletionFallbackTrivialSettlesOnce pins Blocker 1 for trivial output
// (GH #8719 human spec): a run the completion path judges not to need a
// fallback settles durably, so later sweeps never reselect it.
func TestCompletionFallbackTrivialSettlesOnce(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackObligationFixture(t, "trivial settle")
	workerTaskID := dbfx.Task(t, fx.workerID, testutil.Cols{
		"runtime_id": fx.workerRuntimeID, "issue_id": fx.issueID, "status": "running",
		"trigger_comment_id": fx.rootID, "squad_id": fx.squadID, "delegated_from_task_id": fx.sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + fx.rootID + "'::uuid]"),
	})
	_, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"done"}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || fallbackID.Valid {
		t.Fatalf("trivial completion = (transitioned=%v fallback_valid=%v err=%v), want completed run with no fallback", transitioned, fallbackID.Valid, err)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, fx.workerID); got != 0 {
		t.Fatalf("trivial output synthesized %d comment(s), want 0", got)
	}
	if state := completionFallbackState(t, workerTaskID); state != "settled" {
		t.Fatalf("trivial row state = %q, want settled", state)
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper run failed: %v", err)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, fx.workerID); got != 0 {
		t.Fatalf("sweeper resynthesized %d comment(s) for the settled trivial row, want 0", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", fx.issueID, fx.leaderID); got != 0 {
		t.Fatalf("settled trivial row woke %d coordinator task(s), want 0", got)
	}
}

// TestCompletionFallbackPermanentInvalidSettles pins Blocker 1 + review 2a
// (GH #8719): a recorded fallback whose lineage proves permanently invalid
// settles on the first sweep that discovers it, so later sweeps never
// redispatch it and the bounded scan keeps progressing.
func TestCompletionFallbackPermanentInvalidSettles(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	foreignWS := dbfx.Workspace(t, "Fallback invalid settle foreign workspace", "fb-8719-invalid-settle")
	foreignIssue := dbfx.Issue(t, "Fallback invalid settle foreign issue", testutil.Cols{"workspace_id": foreignWS})
	foreignComment := dbfx.Comment(t, foreignIssue, "foreign delegation", testutil.Cols{"workspace_id": foreignWS})
	workerID := dbfx.Agent(t, "Fallback invalid settle worker", testRuntimeID)
	issueID := dbfx.Issue(t, "Fallback invalid lineage settles", testutil.Cols{"status": "in_progress"})
	workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running",
		"trigger_comment_id": foreignComment,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + foreignComment + "'::uuid]"),
	})
	_, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || !fallbackID.Valid {
		t.Fatalf("invalid-lineage completion = (transitioned=%v fallback_valid=%v err=%v), want synthesized fallback", transitioned, fallbackID.Valid, err)
	}
	if state := completionFallbackState(t, workerTaskID); state != "recorded" {
		t.Fatalf("synthesized row state = %q, want recorded", state)
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("first sweeper run failed: %v", err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID); got != 0 {
		t.Fatalf("invalid lineage woke %d task(s), want 0", got)
	}
	if state := completionFallbackState(t, workerTaskID); state != "settled" {
		t.Fatalf("invalid row state after first sweep = %q, want settled", state)
	}
	rows, err := testHandler.Queries.ListPendingCompletionFallbacks(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if uuidToString(row.WorkerTaskID) == workerTaskID {
			t.Fatal("settled invalid fallback still listed as pending")
		}
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("second sweeper run failed: %v", err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status IN ('queued','dispatched','running','waiting_local_directory')", issueID); got != 0 {
		t.Fatalf("second sweep woke %d task(s), want 0", got)
	}
}

// failMarkPendingTxStarter begins real transactions whose
// MarkCompletionFallbackPending statement fails, standing in for any
// failure while recording the pending obligation inside the completion
// transaction. The completion must roll back atomically: the run stays
// runnable with a NULL state instead of completing unmarked (GH #8719).
type failMarkPendingTxStarter struct {
	delegate *pgxpool.Pool
}

type failMarkPendingTx struct {
	pgx.Tx
}

func (s *failMarkPendingTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failMarkPendingTx{Tx: tx}, nil
}

func (t failMarkPendingTx) Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	if strings.Contains(query, "-- name: MarkCompletionFallbackPending") {
		return pgconn.CommandTag{}, errors.New("injected pending mark failure")
	}
	return t.Tx.Exec(ctx, query, args...)
}

// TestCompletionFallbackPendingMarkFailureAbortsCompletion pins the pending
// path (GH #8719): a mark failure inside the completion transaction aborts
// the completion itself -- the run stays runnable with a NULL state, never
// completing unmarked and invisible to the owed scan. A retry then succeeds,
// and a synthesis failure on the retry still leaves a pending obligation the
// sweeper late-synthesizes into exactly one covered coordinator run.
func TestCompletionFallbackPendingMarkFailureAbortsCompletion(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackObligationFixture(t, "mark failure aborts")
	workerTaskID := dbfx.Task(t, fx.workerID, testutil.Cols{
		"runtime_id": fx.workerRuntimeID, "issue_id": fx.issueID, "status": "running",
		"trigger_comment_id": fx.rootID, "squad_id": fx.squadID, "delegated_from_task_id": fx.sourceID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"delivered_comment_ids": testutil.Raw("ARRAY['" + fx.rootID + "'::uuid]"),
	})
	originalTxStarter := testHandler.TaskService.TxStarter
	testHandler.TaskService.TxStarter = &failMarkPendingTxStarter{delegate: testPool}
	_, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	testHandler.TaskService.TxStarter = originalTxStarter
	if err == nil || transitioned || fallbackID.Valid {
		t.Fatalf("mark failure = (transitioned=%v fallback_valid=%v err=%v), want aborted completion", transitioned, fallbackID.Valid, err)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, workerTaskID).Scan(&status)
	if status != "running" {
		t.Fatalf("aborted completion left status = %q, want running", status)
	}
	if state := completionFallbackState(t, workerTaskID); state != "" {
		t.Fatalf("aborted completion left state = %q, want NULL", state)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM comment WHERE source_task_id = $1 AND author_id = $2`, workerTaskID, fx.workerID); got != 0 {
		t.Fatalf("aborted completion left %d comment(s), want 0", got)
	}
	// Retry with a synthesis failure: the run completes with a pending
	// obligation and no fallback of its own...
	testHandler.TaskService.TxStarter = &failRecordCompletionTxStarter{delegate: testPool}
	_, transitioned, fallbackID, err = testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	testHandler.TaskService.TxStarter = originalTxStarter
	if err != nil || !transitioned || fallbackID.Valid {
		t.Fatalf("retry = (transitioned=%v fallback_valid=%v err=%v), want completed run with no fallback id", transitioned, fallbackID.Valid, err)
	}
	if state := completionFallbackState(t, workerTaskID); state != "pending" {
		t.Fatalf("retry left state = %q, want pending", state)
	}
	// ...which the next sweep late-synthesizes into exactly one covered run.
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper recovery failed: %v", err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched','running','waiting_local_directory')", fx.issueID, fx.leaderID); got != 1 {
		t.Fatalf("sweeper recovery left %d runnable coordinator task(s), want exactly 1", got)
	}
}

// TestCompletionFallbackSameSquadNonLeaderSourceDoesNotWake pins the
// always-verify half of the lineage proof (GH #8719 human spec): a
// DelegatedFromTaskID edge is proven even when the worker's squad already
// matches the target squad. A same-squad worker pointing at a non-leader
// source task must wake nobody; skipping verification on squad equality
// would wake the leader here.
func TestCompletionFallbackSameSquadNonLeaderSourceDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newFallbackLineageFixture(t, "same-squad nonleader source", "A")
	nonLeaderID := dbfx.Task(t, fx.worker, testutil.Cols{
		"runtime_id": fx.workerRuntime, "issue_id": fx.issueID, "status": "completed",
		"is_leader_task": false, "squad_id": fx.squadA,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	workerTaskID := dbfx.Task(t, fx.worker, testutil.Cols{
		"runtime_id": fx.workerRuntime, "issue_id": fx.issueID, "status": "running",
		"squad_id": fx.squadA, "delegated_from_task_id": nonLeaderID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	_, transitioned, fallbackID, err := testHandler.TaskService.CompleteTaskWithTransition(
		ctx, parseUUID(workerTaskID), []byte(`{"output":"The delegated work is complete."}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || !fallbackID.Valid {
		t.Fatalf("completion = (transitioned=%v fallback_valid=%v err=%v), want synthesized fallback", transitioned, fallbackID.Valid, err)
	}
	if _, err := testHandler.TaskService.RecoverPendingDelegatedFailures(ctx, 10); err != nil {
		t.Fatalf("sweeper run failed: %v", err)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status IN ('queued','dispatched','running','waiting_local_directory')", fx.issueID); got != 0 {
		t.Fatalf("same-squad non-leader source woke %d task(s), want 0", got)
	}
	if state := completionFallbackState(t, workerTaskID); state != "settled" {
		t.Fatalf("row state = %q, want settled", state)
	}
}
