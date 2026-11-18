package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A sticky "current task" follow-up keeps the run's machine even though the
// joining member owns nothing on it: the agent template's owner may always
// drive their own machine, so her runtime claims the task instead of it
// stranding in queued.
func TestSmartRuntimeStickyFollowUpClaimableByAgentOwner(t *testing.T) {
	ctx := t.Context()
	alice := dbfx.User(t, "Sticky Alice", "sticky-alice@example.test")
	dbfx.Member(t, testWorkspaceID, alice, "member")
	joiner := dbfx.User(t, "Thread Joiner", "sticky-joiner@example.test")
	dbfx.Member(t, testWorkspaceID, joiner, "member")
	machine := dbfx.Runtime(t, "Alice machine", testutil.Cols{"provider": "codex", "owner_id": alice})
	agentID := dbfx.Agent(t, "shared-review-agent", "", testutil.Cols{
		"share_runtime": false, "recommended_cli": "codex", "owner_id": alice,
	})
	issueID := dbfx.Issue(t, "Sticky follow-up", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})

	// The follow-up: routed to the run's machine, but attributed to the member
	// who joined the thread. No personal configuration exists for anyone.
	if _, err := testPool.Exec(ctx, `INSERT INTO agent_task_queue
		(agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id,
		 runtime_config_source, created_at)
		VALUES ($1, $2, $3, 'queued', 4, $4, $4, 'recommended_match', now())`,
		parseUUID(agentID), parseUUID(machine), parseUUID(issueID), parseUUID(joiner)); err != nil {
		t.Fatalf("seed queued follow-up: %v", err)
	}

	claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(machine))
	if err != nil || claimed == nil {
		t.Fatalf("the agent owner's machine failed to claim the sticky follow-up (task=%v, err=%v)", claimed != nil, err)
	}
}

// The requester runtime a deferral persisted on the completed task's context
// survives the round trip through plannedCommentRuntimeOriginator; an absent
// or corrupt entry fails closed instead of silently recovering another owner.
func TestPlannedCommentRuntimeOriginatorRoundTrip(t *testing.T) {
	commentID := parseUUID("00000000-0000-7000-8000-00000000abc1")
	requester := "00000000-0000-7000-8000-00000000abc2"
	taskContext := []byte(`{"planned_runtime_originators":{"` + util.UUIDToString(commentID) + `":"` + requester + `"}}`)

	got, err := plannedCommentRuntimeOriginator(taskContext, commentID)
	if err != nil || util.UUIDToString(got) != requester {
		t.Fatalf("round trip = %s (err %v), want %s", util.UUIDToString(got), err, requester)
	}
	if got, err := plannedCommentRuntimeOriginator(taskContext, parseUUID("00000000-0000-7000-8000-00000000abc9")); err != nil || got.Valid {
		t.Fatalf("unknown comment = %v (err %v), want invalid", got.Valid, err)
	}
	if got, err := plannedCommentRuntimeOriginator(nil, commentID); err != nil || got.Valid {
		t.Fatalf("empty context = %v (err %v), want invalid", got.Valid, err)
	}
	if _, err := plannedCommentRuntimeOriginator([]byte(`{"planned_runtime_originators":{"`+util.UUIDToString(commentID)+`":"not-a-uuid"}}`), commentID); err == nil {
		t.Fatal("a malformed entry must error, not fall back to the sticky chain")
	}
}

// A sticky replay continues a run that a plain member (not the template
// owner) claimed on their own private machine. The completion-replay stamp
// vouches for that machine — the fence re-checks the source run really
// dispatched there — so the run owner's daemon claims the follow-up instead
// of it stranding in queued.
func TestSmartRuntimeStickyInheritanceClaimableByRunOwner(t *testing.T) {
	ctx := t.Context()
	templateOwner := dbfx.User(t, "Sticky Template Owner", "sticky-inherit-template@example.test")
	dbfx.Member(t, testWorkspaceID, templateOwner, "member")
	runOwner := dbfx.User(t, "Sticky Run Owner", "sticky-inherit-run-owner@example.test")
	dbfx.Member(t, testWorkspaceID, runOwner, "member")
	joiner := dbfx.User(t, "Sticky Joiner", "sticky-inherit-joiner@example.test")
	dbfx.Member(t, testWorkspaceID, joiner, "member")
	machine := dbfx.Runtime(t, "Run owner machine", testutil.Cols{"provider": "codex", "owner_id": runOwner})
	agentID := dbfx.Agent(t, "inherit-review-agent", "", testutil.Cols{
		"share_runtime": false, "recommended_cli": "codex", "owner_id": templateOwner,
	})
	issueID := dbfx.Issue(t, "Sticky inheritance", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})

	// The source run: dispatched (claimed and delivered) on the run owner's
	// machine, attributed to the run owner.
	sourceID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": machine, "issue_id": issueID, "status": "completed",
		"originator_user_id": runOwner, "accountable_user_id": runOwner,
		"runtime_config_source": "recommended_match", "dispatched_at": testutil.Raw("now()"),
	})
	source := parseUUID(sourceID)

	// The replay: same machine, attributed to the joining member, then stamped
	// by the completion replay through the real marking statement.
	followUp := parseUUID(dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": machine, "issue_id": issueID, "status": "queued",
		"originator_user_id": joiner, "accountable_user_id": joiner,
		"runtime_config_source": "recommended_match",
	}))
	if _, err := testHandler.Queries.MarkTaskStickyRuntimeInheritance(ctx, db.MarkTaskStickyRuntimeInheritanceParams{
		TaskID:       followUp,
		SourceTaskID: source,
	}); err != nil {
		t.Fatalf("stamp sticky inheritance: %v", err)
	}

	claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(machine))
	if err != nil || claimed == nil {
		t.Fatalf("the run owner's machine failed to claim the stamped follow-up (task=%v, err=%v)", claimed != nil, err)
	}
}

// The auto-retry of a failed sticky follow-up keeps claiming on the run's
// machine: CreateRetryTask rewrites the inherited marker to the retry's own
// id (the fence would otherwise read the parent's id and refuse), keeps the
// source run vouching for the machine, and the run owner's daemon claims the
// second attempt.
func TestSmartRuntimeAutoRetryReinheritsStickyMarker(t *testing.T) {
	ctx := t.Context()
	templateOwner := dbfx.User(t, "Retry Template Owner", "retry-inherit-template@example.test")
	dbfx.Member(t, testWorkspaceID, templateOwner, "member")
	runOwner := dbfx.User(t, "Retry Run Owner", "retry-inherit-run-owner@example.test")
	dbfx.Member(t, testWorkspaceID, runOwner, "member")
	joiner := dbfx.User(t, "Retry Joiner", "retry-inherit-joiner@example.test")
	dbfx.Member(t, testWorkspaceID, joiner, "member")
	machine := dbfx.Runtime(t, "Retry run machine", testutil.Cols{"provider": "codex", "owner_id": runOwner})
	agentID := dbfx.Agent(t, "retry-inherit-agent", "", testutil.Cols{
		"share_runtime": false, "recommended_cli": "codex", "owner_id": templateOwner,
	})
	issueID := dbfx.Issue(t, "Retry inheritance", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})

	sourceID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": machine, "issue_id": issueID, "status": "completed",
		"originator_user_id": runOwner, "accountable_user_id": runOwner,
		"runtime_config_source": "recommended_match", "dispatched_at": testutil.Raw("now()"),
	})
	source := parseUUID(sourceID)
	followUpID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": machine, "issue_id": issueID, "status": "queued",
		"originator_user_id": joiner, "accountable_user_id": joiner,
		"runtime_config_source": "recommended_match",
	})
	followUp := parseUUID(followUpID)
	if _, err := testHandler.Queries.MarkTaskStickyRuntimeInheritance(ctx, db.MarkTaskStickyRuntimeInheritanceParams{
		TaskID:       followUp,
		SourceTaskID: source,
	}); err != nil {
		t.Fatalf("stamp sticky inheritance: %v", err)
	}

	// FailTask marks the parent failed in its own transaction before creating
	// the retry; mirror that state here.
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'failed', completed_at = now() WHERE id = $1`, followUp); err != nil {
		t.Fatalf("fail the sticky follow-up: %v", err)
	}
	retry, err := testHandler.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: followUp})
	if err != nil {
		t.Fatalf("create auto retry: %v", err)
	}
	var stickyTaskID, stickyRuntime, stickySource string
	if err := testPool.QueryRow(ctx, `SELECT
			context->>'sticky_runtime_inherited_task_id',
			context->>'sticky_runtime_inherited_runtime_id',
			context->>'sticky_runtime_source_task_id'
		FROM agent_task_queue WHERE id = $1`, retry.ID).Scan(&stickyTaskID, &stickyRuntime, &stickySource); err != nil {
		t.Fatalf("read retry marker: %v", err)
	}
	if stickyTaskID != util.UUIDToString(retry.ID) || stickyRuntime != util.UUIDToString(retry.RuntimeID) || stickySource != util.UUIDToString(source) {
		t.Fatalf("retry marker = (%s, %s, %s), want (self %s, machine %s, source %s)",
			stickyTaskID, stickyRuntime, stickySource, util.UUIDToString(retry.ID), util.UUIDToString(retry.RuntimeID), util.UUIDToString(source))
	}

	claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(machine))
	if err != nil || claimed == nil || claimed.ID != retry.ID {
		t.Fatalf("the run owner's machine failed to claim the retry (task=%v, err=%v)", claimed != nil, err)
	}
}
