package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type priorityClaimFixture struct {
	fx        *dbfx.Fixture
	queries   *db.Queries
	service   *TaskService
	userID    string
	runtimeID string
	agentID   string
}

func newPriorityClaimFixture(t *testing.T, maxConcurrent int) priorityClaimFixture {
	t.Helper()
	pool := newTaskClaimRacePool(t)
	suffix := time.Now().UnixNano()
	fx := dbfx.New(pool, "", "")
	userID := fx.User(t, "Priority Claim", fmt.Sprintf("priority-claim-%d@multica.test", suffix))
	workspaceID := fx.Workspace(t, "Priority Claim", fmt.Sprintf("priority-claim-%d", suffix), dbfx.Cols{
		"issue_prefix": "PC",
	})
	fx.WorkspaceID = workspaceID
	fx.UserID = userID
	fx.Member(t, workspaceID, userID, "owner")
	runtimeID := fx.Runtime(t, "Priority Claim Runtime", dbfx.Cols{
		"provider": "priority_claim_test",
	})
	agentID := fx.Agent(t, "Priority Claim Agent", runtimeID, dbfx.Cols{
		"max_concurrent_tasks": maxConcurrent,
	})
	queries := db.New(pool)
	return priorityClaimFixture{
		fx:        fx,
		queries:   queries,
		service:   NewTaskService(queries, pool, nil, events.New()),
		userID:    userID,
		runtimeID: runtimeID,
		agentID:   agentID,
	}
}

func (f priorityClaimFixture) issue(t *testing.T, title, priority string) db.Issue {
	t.Helper()
	id := f.fx.Issue(t, title, dbfx.Cols{
		"priority":      priority,
		"assignee_type": "agent",
		"assignee_id":   f.agentID,
	})
	issue, err := f.queries.GetIssue(context.Background(), util.MustParseUUID(id))
	if err != nil {
		t.Fatalf("load issue %s: %v", title, err)
	}
	return issue
}

func (f priorityClaimFixture) trackServiceTask(t *testing.T, task db.AgentTaskQueue) {
	t.Helper()
	f.fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id = $1`, util.UUIDToString(task.ID))
}

func TestIssueTaskClaimUsesPriorityThenFIFO(t *testing.T) {
	ctx := context.Background()
	f := newPriorityClaimFixture(t, 10)

	want := []struct {
		name string
		rank int32
	}{
		{name: "urgent", rank: 4},
		{name: "high", rank: 3},
		{name: "medium", rank: 2},
		{name: "low", rank: 1},
		{name: "none", rank: 0},
	}
	ids := make(map[string]string, len(want))
	// Insert in the reverse of dispatch order. The result must come from
	// priority, not enqueue time.
	for i := len(want) - 1; i >= 0; i-- {
		issue := f.issue(t, "priority "+want[i].name, want[i].name)
		task, err := f.service.EnqueueTaskForIssue(ctx, issue)
		if err != nil {
			t.Fatalf("enqueue %s: %v", want[i].name, err)
		}
		f.trackServiceTask(t, task)
		ids[want[i].name] = util.UUIDToString(task.ID)
	}

	for _, expected := range want {
		claimed, err := f.service.ClaimTaskForRuntime(ctx, util.MustParseUUID(f.runtimeID))
		if err != nil {
			t.Fatalf("claim %s: %v", expected.name, err)
		}
		if claimed == nil {
			t.Fatalf("claim %s: got no task", expected.name)
		}
		if got := util.UUIDToString(claimed.ID); got != ids[expected.name] {
			t.Fatalf("claim %s: task = %s, want %s", expected.name, got, ids[expected.name])
		}
		if claimed.Priority != expected.rank {
			t.Fatalf("claim %s: rank = %d, want %d", expected.name, claimed.Priority, expected.rank)
		}
	}

	older := f.issue(t, "older high", "high")
	olderTask, err := f.service.EnqueueTaskForIssue(ctx, older)
	if err != nil {
		t.Fatalf("enqueue older high: %v", err)
	}
	f.trackServiceTask(t, olderTask)
	newer := f.issue(t, "newer high", "high")
	newerTask, err := f.service.EnqueueTaskForIssue(ctx, newer)
	if err != nil {
		t.Fatalf("enqueue newer high: %v", err)
	}
	f.trackServiceTask(t, newerTask)
	f.fx.Exec(t, `UPDATE agent_task_queue SET created_at = now() - interval '1 second' WHERE id = $1`, util.UUIDToString(olderTask.ID))
	claimed, err := f.service.ClaimTaskForRuntime(ctx, util.MustParseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("claim FIFO high: %v", err)
	}
	if claimed == nil || claimed.ID != olderTask.ID {
		t.Fatalf("same-priority claim did not preserve FIFO: got %+v, want %s", claimed, util.UUIDToString(olderTask.ID))
	}
}

func TestHighPriorityTakesNextFreeSlotWithoutPreemption(t *testing.T) {
	ctx := context.Background()
	f := newPriorityClaimFixture(t, 2)
	runningIssue := f.issue(t, "healthy running low", "low")
	queuedLowIssue := f.issue(t, "queued low", "low")
	queuedHighIssue := f.issue(t, "arriving high", "high")

	runningID := f.fx.Task(t, f.agentID, dbfx.Cols{
		"runtime_id": f.runtimeID,
		"issue_id":   util.UUIDToString(runningIssue.ID),
		"status":     "running",
		"priority":   1,
		"started_at": dbfx.Raw("now() - interval '1 minute'"),
	})
	lowID := f.fx.Task(t, f.agentID, dbfx.Cols{
		"runtime_id": f.runtimeID,
		"issue_id":   util.UUIDToString(queuedLowIssue.ID),
		"priority":   1,
	})
	highID := f.fx.Task(t, f.agentID, dbfx.Cols{
		"runtime_id": f.runtimeID,
		"issue_id":   util.UUIDToString(queuedHighIssue.ID),
		"priority":   3,
	})

	claimed, err := f.service.ClaimTaskForRuntime(ctx, util.MustParseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("claim next free slot: %v", err)
	}
	if claimed == nil || util.UUIDToString(claimed.ID) != highID {
		t.Fatalf("claimed = %+v, want arriving high %s", claimed, highID)
	}

	var runningStatus, lowStatus string
	f.fx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, runningID).Scan(&runningStatus)
	f.fx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, lowID).Scan(&lowStatus)
	if runningStatus != "running" {
		t.Fatalf("healthy low run was preempted: status = %s", runningStatus)
	}
	if lowStatus != "queued" {
		t.Fatalf("lower-priority successor = %s, want queued", lowStatus)
	}
}

func TestBatchClaimPrioritizesAcrossAgentsOnOneRuntime(t *testing.T) {
	ctx := context.Background()
	f := newPriorityClaimFixture(t, 1)
	otherAgentID := f.fx.Agent(t, "Other Priority Agent", f.runtimeID)
	lowIssue := f.issue(t, "batch low", "low")
	highIssueID := f.fx.Issue(t, "batch high", dbfx.Cols{
		"priority":      "high",
		"assignee_type": "agent",
		"assignee_id":   otherAgentID,
	})
	lowTaskID := f.fx.Task(t, f.agentID, dbfx.Cols{
		"runtime_id": f.runtimeID,
		"issue_id":   util.UUIDToString(lowIssue.ID),
		"priority":   1,
	})
	highTaskID := f.fx.Task(t, otherAgentID, dbfx.Cols{
		"runtime_id": f.runtimeID,
		"issue_id":   highIssueID,
		"priority":   3,
	})

	claimed, err := f.service.ClaimTasksForRuntimes(ctx, []pgtype.UUID{util.MustParseUUID(f.runtimeID)}, 1)
	if err != nil {
		t.Fatalf("batch claim: %v", err)
	}
	if len(claimed) != 1 || util.UUIDToString(claimed[0].ID) != highTaskID {
		t.Fatalf("batch claimed = %+v, want high task %s", claimed, highTaskID)
	}
	var lowStatus string
	f.fx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, lowTaskID).Scan(&lowStatus)
	if lowStatus != "queued" {
		t.Fatalf("batch lower-priority task = %s, want queued", lowStatus)
	}
}

// TestSharedIssueDispatchFunnelsPreserveQueuePriority exercises each
// distinct task-construction funnel. Assignment, status promotion, sub-issue
// creation, and autopilot create_issue all use EnqueueTaskForIssue; explicit
// mention, wake-comment, and thread-parent paths all use the mention helper.
// Rerun and automatic retry have separate construction paths and are covered
// directly below.
func TestSharedIssueDispatchFunnelsPreserveQueuePriority(t *testing.T) {
	ctx := context.Background()
	f := newPriorityClaimFixture(t, 10)

	assignmentIssue := f.issue(t, "assignment high", "high")
	assignment, err := f.service.EnqueueTaskForIssue(ctx, assignmentIssue)
	if err != nil {
		t.Fatalf("assignment enqueue: %v", err)
	}
	f.trackServiceTask(t, assignment)
	if assignment.Priority != 3 {
		t.Fatalf("assignment priority = %d, want 3", assignment.Priority)
	}

	mentionIssue := f.issue(t, "mention medium", "medium")
	mention, err := f.service.EnqueueTaskForMention(ctx, mentionIssue, util.MustParseUUID(f.agentID), pgtype.UUID{})
	if err != nil {
		t.Fatalf("mention enqueue: %v", err)
	}
	f.trackServiceTask(t, mention)
	if mention.Priority != 2 {
		t.Fatalf("mention priority = %d, want 2", mention.Priority)
	}

	rerunIssue := f.issue(t, "rerun urgent", "urgent")
	sourceID := f.fx.Task(t, f.agentID, dbfx.Cols{
		"runtime_id":   f.runtimeID,
		"issue_id":     util.UUIDToString(rerunIssue.ID),
		"status":       "completed",
		"priority":     0,
		"completed_at": dbfx.Raw("now()"),
	})
	rerun, err := f.service.RerunIssue(
		ctx,
		rerunIssue.ID,
		util.MustParseUUID(sourceID),
		pgtype.UUID{},
		util.MustParseUUID(f.userID),
		func(db.Agent) bool { return true },
	)
	if err != nil {
		t.Fatalf("rerun enqueue: %v", err)
	}
	f.trackServiceTask(t, *rerun)
	if rerun.Priority != 4 {
		t.Fatalf("rerun priority = %d, want current issue rank 4", rerun.Priority)
	}

	retryIssue := f.issue(t, "retry low", "low")
	failedID := f.fx.Task(t, f.agentID, dbfx.Cols{
		"runtime_id":     f.runtimeID,
		"issue_id":       util.UUIDToString(retryIssue.ID),
		"status":         "failed",
		"priority":       1,
		"attempt":        1,
		"max_attempts":   2,
		"failure_reason": "runtime_recovery",
		"completed_at":   dbfx.Raw("now()"),
	})
	failed, err := f.queries.GetAgentTask(ctx, util.MustParseUUID(failedID))
	if err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	retry, err := f.service.MaybeRetryFailedTask(ctx, failed)
	if err != nil {
		t.Fatalf("retry enqueue: %v", err)
	}
	if retry == nil || retry.Priority != 1 {
		t.Fatalf("retry priority = %+v, want inherited rank 1", retry)
	}
	f.trackServiceTask(t, *retry)
}

func TestIssuePriorityConversionsAreStable(t *testing.T) {
	for _, tc := range []struct {
		name string
		rank int32
	}{
		{name: "urgent", rank: 4},
		{name: "high", rank: 3},
		{name: "medium", rank: 2},
		{name: "low", rank: 1},
		{name: "none", rank: 0},
	} {
		if got := priorityToInt(tc.name); got != tc.rank {
			t.Errorf("priorityToInt(%q) = %d, want %d", tc.name, got, tc.rank)
		}
		if got := priorityFromInt(tc.rank); got != tc.name {
			t.Errorf("priorityFromInt(%d) = %q, want %q", tc.rank, got, tc.name)
		}
	}
	if got := priorityFromInt(99); got != "unknown" {
		t.Fatalf("unknown rank = %q, want unknown", got)
	}
}
