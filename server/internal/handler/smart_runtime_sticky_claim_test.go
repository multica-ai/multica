package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	service "github.com/multica-ai/multica/server/internal/service"
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

// With two online machines of the same CLI owned by the run's member, a
// sticky continuation pins the machine the pinned run actually resolved to
// (A) instead of drifting to whichever heartbeated last (B), and stamps its
// own inheritance marker so A can claim it.
func TestSmartRuntimeContinuationPinsLatestRunMachine(t *testing.T) {
	ctx := t.Context()
	templateOwner := dbfx.User(t, "Pin Template Owner", "pin-template@example.test")
	dbfx.Member(t, testWorkspaceID, templateOwner, "member")
	runOwner := dbfx.User(t, "Pin Run Owner", "pin-run-owner@example.test")
	dbfx.Member(t, testWorkspaceID, runOwner, "member")
	machineA := dbfx.Runtime(t, "Pin machine A", testutil.Cols{"provider": "codex", "owner_id": runOwner})
	// B heartbeats after A, so a fresh resolution would prefer B.
	machineB := dbfx.Runtime(t, "Pin machine B", testutil.Cols{
		"provider": "codex", "owner_id": runOwner, "last_seen_at": testutil.Raw("now() + interval '1 second'"),
	})
	_ = machineB
	agentID := dbfx.Agent(t, "pin-review-agent", "", testutil.Cols{
		"share_runtime": false, "recommended_cli": "codex", "owner_id": templateOwner,
	})
	issueID := dbfx.Issue(t, "Pin continuation", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})

	// The run: claimed and delivered on machine A.
	sourceID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": machineA, "issue_id": issueID, "status": "completed",
		"originator_user_id": runOwner, "accountable_user_id": runOwner,
		"runtime_config_source": "recommended_match", "dispatched_at": testutil.Raw("now()"),
	})

	// The joining member's sticky continuation enqueues straight onto A.
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, parseUUID(agentID), pgtype.UUID{}, service.OriginNamed)
	if err != nil {
		t.Fatal(err)
	}
	if util.UUIDToString(task.RuntimeID) != machineA {
		t.Fatalf("continuation runtime = %s, want the pinned machine A %s", util.UUIDToString(task.RuntimeID), machineA)
	}
	var markerTask, markerSource string
	if err := testPool.QueryRow(ctx, `SELECT context->>'sticky_runtime_inherited_task_id', context->>'sticky_runtime_source_task_id'
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&markerTask, &markerSource); err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if markerTask != util.UUIDToString(task.ID) || markerSource != sourceID {
		t.Fatalf("marker = (task %s, source %s), want (self %s, source %s)",
			markerTask, markerSource, util.UUIDToString(task.ID), sourceID)
	}
	if claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(machineA)); err != nil || claimed == nil || claimed.ID != task.ID {
		t.Fatalf("machine A failed to claim the pinned continuation (task=%v, err=%v)", claimed != nil, err)
	}
}

// Two smart agents on one issue run on two of Bob's machines. A sticky
// continuation of agent A must scope the recovery to A's own runs — it must
// not inherit agent B's machine just because B's run is newer.
func TestSmartRuntimeContinuationScopesToTargetAgent(t *testing.T) {
	ctx := t.Context()
	templateOwner := dbfx.User(t, "Scope Template Owner", "scope-template@example.test")
	dbfx.Member(t, testWorkspaceID, templateOwner, "member")
	runOwner := dbfx.User(t, "Scope Run Owner", "scope-run-owner@example.test")
	dbfx.Member(t, testWorkspaceID, runOwner, "member")
	codexMachine := dbfx.Runtime(t, "Scope Codex machine", testutil.Cols{"provider": "codex", "owner_id": runOwner})
	claudeMachine := dbfx.Runtime(t, "Scope Claude machine", testutil.Cols{"provider": "claude", "owner_id": runOwner})
	studioID := dbfx.Agent(t, "studio-helper", "", testutil.Cols{
		"share_runtime": false, "recommended_cli": "codex", "owner_id": templateOwner,
	})
	reviewID := dbfx.Agent(t, "review-helper", "", testutil.Cols{
		"share_runtime": false, "recommended_cli": "claude", "owner_id": templateOwner,
	})
	issueID := dbfx.Issue(t, "Scoped continuation", testutil.Cols{"assignee_type": "agent", "assignee_id": studioID})

	// Studio Helper ran on the Codex machine, then Review Helper ran on the
	// Claude machine — the newest run on this issue belongs to the OTHER agent.
	dbfx.Task(t, studioID, testutil.Cols{
		"runtime_id": codexMachine, "issue_id": issueID, "status": "completed",
		"originator_user_id": runOwner, "accountable_user_id": runOwner,
		"runtime_config_source": "recommended_match", "dispatched_at": testutil.Raw("now()"),
	})
	dbfx.Task(t, reviewID, testutil.Cols{
		"runtime_id": claudeMachine, "issue_id": issueID, "status": "completed",
		"originator_user_id": runOwner, "accountable_user_id": runOwner,
		"runtime_config_source": "recommended_match", "dispatched_at": testutil.Raw("now()"),
	})

	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, parseUUID(studioID), pgtype.UUID{}, service.OriginNamed)
	if err != nil {
		t.Fatal(err)
	}
	if util.UUIDToString(task.RuntimeID) != codexMachine {
		t.Fatalf("continuation runtime = %s, want Studio Helper's Codex machine %s", util.UUIDToString(task.RuntimeID), codexMachine)
	}
	if claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(codexMachine)); err != nil || claimed == nil || claimed.ID != task.ID {
		t.Fatalf("the Codex machine failed to claim the scoped continuation (task=%v, err=%v)", claimed != nil, err)
	}
}

// A sticky continuation adopts the source run's whole snapshot: after Bob
// saves a personal Claude configuration for the agent, the continuation of a
// run that executed on his Codex with gpt-5.4 keeps Codex / gpt-5.4 — the
// machine never advertised the later-saved Claude model.
func TestSmartRuntimeContinuationAdoptsRunSnapshot(t *testing.T) {
	ctx := t.Context()
	templateOwner := dbfx.User(t, "Snapshot Template Owner", "snapshot-template@example.test")
	dbfx.Member(t, testWorkspaceID, templateOwner, "member")
	runOwner := dbfx.User(t, "Snapshot Run Owner", "snapshot-run-owner@example.test")
	dbfx.Member(t, testWorkspaceID, runOwner, "member")
	codexMachine := dbfx.Runtime(t, "Snapshot Codex machine", testutil.Cols{"provider": "codex", "owner_id": runOwner})
	claudeMachine := dbfx.Runtime(t, "Snapshot Claude machine", testutil.Cols{"provider": "claude", "owner_id": runOwner})
	agentID := dbfx.Agent(t, "snapshot-review-agent", "", testutil.Cols{
		"share_runtime": false, "recommended_cli": "codex", "owner_id": templateOwner,
	})
	issueID := dbfx.Issue(t, "Snapshot continuation", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})

	// The run: Codex machine, gpt-5.4.
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": codexMachine, "issue_id": issueID, "status": "completed",
		"originator_user_id": runOwner, "accountable_user_id": runOwner,
		"runtime_config_source": "recommended_match", "dispatched_at": testutil.Raw("now()"),
		"effective_model": "gpt-5.4",
	})
	// Saved AFTER the run: Bob's personal Claude configuration.
	if _, err := testPool.Exec(ctx, `INSERT INTO agent_user_runtime_config
		(agent_id, user_id, runtime_id, model)
		VALUES ($1, $2, $3, 'claude-sonnet-4-6')`,
		parseUUID(agentID), parseUUID(runOwner), parseUUID(claudeMachine)); err != nil {
		t.Fatalf("save personal Claude config: %v", err)
	}

	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, parseUUID(agentID), pgtype.UUID{}, service.OriginNamed)
	if err != nil {
		t.Fatal(err)
	}
	if util.UUIDToString(task.RuntimeID) != codexMachine || task.EffectiveModel.String != "gpt-5.4" {
		t.Fatalf("continuation = (%s, %s), want the run snapshot (Codex machine, gpt-5.4)",
			util.UUIDToString(task.RuntimeID), task.EffectiveModel.String)
	}
	if claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(codexMachine)); err != nil || claimed == nil || claimed.ID != task.ID {
		t.Fatalf("the Codex machine failed to claim the snapshot continuation (task=%v, err=%v)", claimed != nil, err)
	}
}

// Two threads of one agent: thread 1 (Codex) enqueued FIRST but claimed LAST,
// thread 2 (Claude) enqueued later and ran earlier. The popover sorts by
// start time and shows thread 1's Codex run as "current task" — the server's
// snapshot recovery must pick the same run, not the later-enqueued one.
func TestSmartRuntimeContinuationMatchesFrontendSort(t *testing.T) {
	ctx := t.Context()
	templateOwner := dbfx.User(t, "Sort Template Owner", "sort-template@example.test")
	dbfx.Member(t, testWorkspaceID, templateOwner, "member")
	runOwner := dbfx.User(t, "Sort Run Owner", "sort-run-owner@example.test")
	dbfx.Member(t, testWorkspaceID, runOwner, "member")
	codexMachine := dbfx.Runtime(t, "Sort Codex machine", testutil.Cols{"provider": "codex", "owner_id": runOwner})
	claudeMachine := dbfx.Runtime(t, "Sort Claude machine", testutil.Cols{"provider": "claude", "owner_id": runOwner})
	agentID := dbfx.Agent(t, "sort-review-agent", "", testutil.Cols{
		"share_runtime": false, "recommended_cli": "codex", "owner_id": templateOwner,
	})
	issueID := dbfx.Issue(t, "Sort continuation", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})

	// Thread 1: enqueued first, claimed (started) last — the popover's pick.
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": codexMachine, "issue_id": issueID, "status": "completed",
		"originator_user_id": runOwner, "accountable_user_id": runOwner,
		"runtime_config_source": "recommended_match",
		"effective_model":       "gpt-5.4",
		"created_at":            testutil.Raw("now() - interval '10 minutes'"),
		"started_at":            testutil.Raw("now() - interval '1 minute'"),
	})
	// Thread 2: enqueued later, ran and finished earlier.
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": claudeMachine, "issue_id": issueID, "status": "completed",
		"originator_user_id": runOwner, "accountable_user_id": runOwner,
		"runtime_config_source": "recommended_match",
		"effective_model":       "claude-sonnet-4-6",
		"created_at":            testutil.Raw("now() - interval '5 minutes'"),
		"started_at":            testutil.Raw("now() - interval '8 minutes'"),
	})

	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, parseUUID(agentID), pgtype.UUID{}, service.OriginNamed)
	if err != nil {
		t.Fatal(err)
	}
	if util.UUIDToString(task.RuntimeID) != codexMachine || task.EffectiveModel.String != "gpt-5.4" {
		t.Fatalf("continuation = (%s, %s), want the popover's pick (Codex machine, gpt-5.4)",
			util.UUIDToString(task.RuntimeID), task.EffectiveModel.String)
	}
}

// Same-second starts must stay distinguishable in the task-runs payload: the
// popover sorts by these instants, and second-precision strings collapsed two
// same-second runs into one ordering slot while the server's recovery used
// the full database timestamps.
func TestTaskRunsStartedAtKeepsSubSecondPrecision(t *testing.T) {
	owner := dbfx.User(t, "Nano owner", "nano-owner@example.test")
	dbfx.Member(t, testWorkspaceID, owner, "member")
	machine := dbfx.Runtime(t, "Nano machine", testutil.Cols{"provider": "codex", "owner_id": owner})
	agentID := dbfx.Agent(t, "nano-agent", machine, testutil.Cols{})
	issueID := dbfx.Issue(t, "Nano precision")
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": machine, "issue_id": issueID, "status": "completed",
		"started_at": testutil.Raw("now()"),
	})
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": machine, "issue_id": issueID, "status": "completed",
		"started_at": testutil.Raw("now() + interval '70 milliseconds'"),
	})

	req := newOwnerDaemonRequest(http.MethodGet, "/api/issues/"+issueID+"/task-runs", nil)
	req = withURLParam(req, "id", issueID)
	w := testutil.Call(t, testHandler.ListTasksByIssue, req).Want(http.StatusOK)
	var runs []struct {
		ID        string  `json:"id"`
		StartedAt *string `json:"started_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode task runs: %v", err)
	}
	precisionLost := 0
	for _, run := range runs {
		if run.StartedAt == nil || !strings.Contains(*run.StartedAt, ".") {
			precisionLost++
		}
	}
	if precisionLost > 0 {
		t.Fatalf("%d of %d runs lost sub-second started_at precision", precisionLost, len(runs))
	}
}
