package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
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
