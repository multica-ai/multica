package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type workspaceIdleFixture struct {
	fx          *testutil.Fixture
	bus         *events.Bus
	workspaceID string
	ownerID     string
	runtimeID   string
	agentID     string
}

func newWorkspaceIdleFixture(t *testing.T) *workspaceIdleFixture {
	t.Helper()
	seed := uuid.NewString()
	fx := testutil.New(testPool, "", "")
	ownerID := fx.User(t, "Idle Owner", "idle-owner-"+seed+"@multica.test")
	workspaceID := fx.Workspace(t, "Idle Test", "idle-test-"+seed)
	fx.WorkspaceID = workspaceID
	fx.UserID = ownerID
	fx.Cleanup(t, `DELETE FROM workspace_idle_state WHERE workspace_id = $1`, workspaceID)
	fx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
	fx.Member(t, workspaceID, ownerID, "owner")
	runtimeID := fx.Runtime(t, "Idle Test Runtime")
	agentID := fx.Agent(t, "Idle Test Agent", runtimeID)

	bus := events.New()
	registerWorkspaceIdleListeners(bus, testPool)
	return &workspaceIdleFixture{
		fx: fx, bus: bus, workspaceID: workspaceID, ownerID: ownerID,
		runtimeID: runtimeID, agentID: agentID,
	}
}

func (f *workspaceIdleFixture) member(t *testing.T, role string) string {
	t.Helper()
	seed := uuid.NewString()
	userID := f.fx.User(t, "Idle Recipient", "idle-recipient-"+seed+"@multica.test")
	f.fx.Member(t, f.workspaceID, userID, role)
	return userID
}

func (f *workspaceIdleFixture) issue(t *testing.T, creatorType, creatorID, status string) string {
	t.Helper()
	return f.fx.Issue(t, "Idle workflow issue", testutil.Cols{
		"workspace_id": f.workspaceID,
		"creator_type": creatorType,
		"creator_id":   creatorID,
		"status":       status,
	})
}

func (f *workspaceIdleFixture) activeTask(t *testing.T, issueID string) string {
	t.Helper()
	taskID := f.fx.Task(t, f.agentID, testutil.Cols{
		"runtime_id": f.runtimeID,
		"issue_id":   issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})
	f.bus.Publish(events.Event{
		Type:        protocol.EventTaskRunning,
		WorkspaceID: f.workspaceID,
		Payload:     map[string]any{"task_id": taskID, "issue_id": issueID},
	})
	return taskID
}

func (f *workspaceIdleFixture) finishTask(t *testing.T, taskID, eventType string, retryPending bool) {
	t.Helper()
	status := "completed"
	if eventType == protocol.EventTaskFailed {
		status = "failed"
	} else if eventType == protocol.EventTaskCancelled {
		status = "cancelled"
	}
	f.fx.Exec(t, `UPDATE agent_task_queue SET status = $2, completed_at = now() WHERE id = $1`, taskID, status)
	f.bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: f.workspaceID,
		Payload: map[string]any{
			"task_id":       taskID,
			"issue_id":      f.taskIssueID(t, taskID),
			"retry_pending": retryPending,
		},
	})
}

func (f *workspaceIdleFixture) taskIssueID(t *testing.T, taskID string) string {
	t.Helper()
	var issueID string
	f.fx.QueryRow(t, `SELECT issue_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&issueID)
	return issueID
}

func (f *workspaceIdleFixture) inboxCount(t *testing.T, recipientID string) int {
	t.Helper()
	return f.fx.Count(t, `
		SELECT count(*) FROM inbox_item
		WHERE workspace_id = $1 AND recipient_id = $2 AND type = 'workspace_idle'
	`, f.workspaceID, recipientID)
}

func TestWorkspaceIdleNotifiesManagersWhenNoIssueIsInProgress(t *testing.T) {
	f := newWorkspaceIdleFixture(t)
	adminID := f.member(t, "admin")
	issueID := f.issue(t, "member", f.ownerID, "done")
	taskID := f.activeTask(t, issueID)

	f.finishTask(t, taskID, protocol.EventTaskCompleted, false)

	if got := f.inboxCount(t, f.ownerID); got != 1 {
		t.Fatalf("owner workspace_idle inbox rows = %d, want 1", got)
	}
	if got := f.inboxCount(t, adminID); got != 1 {
		t.Fatalf("admin workspace_idle inbox rows = %d, want 1", got)
	}
}

func TestWorkspaceIdleNotifiesInProgressCreatorsAndDelegatorsOnce(t *testing.T) {
	f := newWorkspaceIdleFixture(t)
	creatorID := f.member(t, "member")
	delegatorID := f.member(t, "member")
	creatorIssueID := f.issue(t, "member", creatorID, "in_progress")
	delegatedIssueID := f.issue(t, "agent", f.agentID, "in_progress")
	f.fx.InsertNoID(t, "issue_subscriber", testutil.Cols{
		"issue_id":  delegatedIssueID,
		"user_type": "member",
		"user_id":   delegatorID,
		"reason":    "delegated",
	}, "issue_id = $1 AND user_type = 'member' AND user_id = $2", delegatedIssueID, delegatorID)
	// The same member appearing through both routes must still receive one row.
	f.fx.InsertNoID(t, "issue_subscriber", testutil.Cols{
		"issue_id":  delegatedIssueID,
		"user_type": "member",
		"user_id":   creatorID,
		"reason":    "delegated",
	}, "issue_id = $1 AND user_type = 'member' AND user_id = $2", delegatedIssueID, creatorID)
	taskID := f.activeTask(t, creatorIssueID)

	f.finishTask(t, taskID, protocol.EventTaskCompleted, false)

	if got := f.inboxCount(t, creatorID); got != 1 {
		t.Fatalf("creator workspace_idle inbox rows = %d, want 1", got)
	}
	if got := f.inboxCount(t, delegatorID); got != 1 {
		t.Fatalf("delegator workspace_idle inbox rows = %d, want 1", got)
	}
	if got := f.inboxCount(t, f.ownerID); got != 0 {
		t.Fatalf("unrelated owner workspace_idle inbox rows = %d, want 0", got)
	}

	var body string
	f.fx.QueryRow(t, `
		SELECT body FROM inbox_item
		WHERE workspace_id = $1 AND recipient_id = $2 AND type = 'workspace_idle'
	`, f.workspaceID, creatorID).Scan(&body)
	if !strings.Contains(body, "2 个 in_progress 任务") {
		t.Errorf("body = %q, want the in-progress issue count", body)
	}
}

func TestWorkspaceIdleDoesNotNotifyWhileAnotherTaskIsActive(t *testing.T) {
	f := newWorkspaceIdleFixture(t)
	issueID := f.issue(t, "member", f.ownerID, "done")
	finishedTaskID := f.activeTask(t, issueID)
	f.activeTask(t, issueID)

	f.finishTask(t, finishedTaskID, protocol.EventTaskCompleted, false)

	if got := f.inboxCount(t, f.ownerID); got != 0 {
		t.Fatalf("workspace_idle inbox rows = %d, want 0 while another task is active", got)
	}
}

func TestWorkspaceIdleSkipsRetryPendingFailures(t *testing.T) {
	f := newWorkspaceIdleFixture(t)
	issueID := f.issue(t, "member", f.ownerID, "done")
	taskID := f.activeTask(t, issueID)

	f.finishTask(t, taskID, protocol.EventTaskFailed, true)
	if got := f.inboxCount(t, f.ownerID); got != 0 {
		t.Fatalf("workspace_idle inbox rows = %d, want 0 while a retry is pending", got)
	}

	f.bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: f.workspaceID,
		Payload:     map[string]any{"task_id": taskID, "retry_pending": false},
	})
	if got := f.inboxCount(t, f.ownerID); got != 1 {
		t.Fatalf("workspace_idle inbox rows after terminal failure = %d, want 1", got)
	}
}

func TestWorkspaceIdleConcurrentTerminalEventsProduceOneRound(t *testing.T) {
	f := newWorkspaceIdleFixture(t)
	issueID := f.issue(t, "member", f.ownerID, "done")
	taskA := f.activeTask(t, issueID)
	taskB := f.activeTask(t, issueID)
	f.fx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = ANY($1::uuid[])`, []string{taskA, taskB})

	var wg sync.WaitGroup
	for _, taskID := range []string{taskA, taskB} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.bus.Publish(events.Event{
				Type:        protocol.EventTaskCompleted,
				WorkspaceID: f.workspaceID,
				Payload:     map[string]any{"task_id": taskID},
			})
		}()
	}
	wg.Wait()

	if got := f.inboxCount(t, f.ownerID); got != 1 {
		t.Fatalf("concurrent terminal events created %d workspace_idle rows, want 1", got)
	}
}

func TestWorkspaceIdleCanNotifyAgainAfterANewBusyPeriod(t *testing.T) {
	f := newWorkspaceIdleFixture(t)
	issueID := f.issue(t, "member", f.ownerID, "done")
	firstTaskID := f.activeTask(t, issueID)
	f.finishTask(t, firstTaskID, protocol.EventTaskCompleted, false)

	secondTaskID := f.activeTask(t, issueID)
	f.finishTask(t, secondTaskID, protocol.EventTaskCompleted, false)

	if got := f.inboxCount(t, f.ownerID); got != 2 {
		t.Fatalf("workspace_idle inbox rows across two busy periods = %d, want 2", got)
	}
}

func TestWorkspaceIdleRecognisesCustomInProgressStatus(t *testing.T) {
	f := newWorkspaceIdleFixture(t)
	creatorID := f.member(t, "member")
	f.fx.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": f.workspaceID,
		"key":          "actively_building",
		"name":         "Actively Building",
		"description":  "Custom in-progress state",
		"category":     "in_progress",
		"color":        "#f59e0b",
		"position":     1,
	})
	issueID := f.issue(t, "member", creatorID, "actively_building")
	taskID := f.activeTask(t, issueID)

	f.finishTask(t, taskID, protocol.EventTaskCancelled, false)

	if got := f.inboxCount(t, creatorID); got != 1 {
		t.Fatalf("custom in_progress creator workspace_idle rows = %d, want 1", got)
	}
	if got := f.inboxCount(t, f.ownerID); got != 0 {
		t.Fatalf("manager fallback ran for a custom in_progress status: got %d rows", got)
	}
}

func TestWorkspaceIdleDoesNotStartANotificationCycleForChatOnlyWork(t *testing.T) {
	f := newWorkspaceIdleFixture(t)
	taskID := f.fx.Task(t, f.agentID, testutil.Cols{
		"runtime_id": f.runtimeID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})
	f.bus.Publish(events.Event{
		Type:        protocol.EventTaskRunning,
		WorkspaceID: f.workspaceID,
		Payload:     map[string]any{"task_id": taskID},
	})
	f.fx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskID)
	f.bus.Publish(events.Event{
		Type:        protocol.EventTaskCompleted,
		WorkspaceID: f.workspaceID,
		Payload:     map[string]any{"task_id": taskID},
	})

	if got := f.inboxCount(t, f.ownerID); got != 0 {
		t.Fatalf("chat-only work created %d workspace_idle rows, want 0", got)
	}
}

func TestWorkspaceIdleInboxEventHasNoIssueTarget(t *testing.T) {
	f := newWorkspaceIdleFixture(t)
	issueID := f.issue(t, "member", f.ownerID, "done")
	taskID := f.activeTask(t, issueID)
	var got events.Event
	f.bus.Subscribe(protocol.EventInboxNew, func(e events.Event) { got = e })

	f.finishTask(t, taskID, protocol.EventTaskCompleted, false)

	item, ok := got.Payload.(map[string]any)["item"].(map[string]any)
	if !ok {
		t.Fatalf("inbox event payload = %#v, want item map", got.Payload)
	}
	if item["issue_id"] != (*string)(nil) && item["issue_id"] != nil {
		t.Errorf("workspace idle issue_id = %#v, want nil", item["issue_id"])
	}
	if item["type"] != workspaceIdleNotificationType {
		t.Errorf("workspace idle type = %#v", item["type"])
	}
	if got.ActorType != "system" {
		t.Errorf("workspace idle actor type = %q, want system", got.ActorType)
	}
}
