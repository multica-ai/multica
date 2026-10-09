package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func agentMention(name, id string) string {
	return fmt.Sprintf("[@%s](mention://agent/%s)", name, id)
}

func postSteeredComment(t *testing.T, issueID, parentID, content string, steerTaskIDs ...string) *testutil.Response {
	t.Helper()
	body := map[string]any{"content": content, "steer_task_ids": steerTaskIDs}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	return testutil.Call(t, testHandler.CreateComment, withURLParam(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body), "id", issueID))
}

// runningSteerableTask starts another agent's running, steer-capable turn on
// the fixture's issue.
func runningSteerableTask(t *testing.T, f supplementFixture, name string) (agentID, taskID string) {
	t.Helper()
	runtimeID := dbfx.Runtime(t, "steer-"+name, testutil.Cols{"provider": "claude"})
	agentID = dbfx.Agent(t, name, runtimeID)
	taskID = dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": f.issueID, "runtime_id": runtimeID, "status": "running", "started_at": testutil.Raw("now()"),
	})
	dbfx.Exec(t, `INSERT INTO task_supplement_capability (task_id, workspace_id, issue_id, capability) VALUES ($1, $2, $3, $4)`,
		taskID, testWorkspaceID, f.issueID, protocol.DaemonCapabilityTaskSupplementV1)
	dbfx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id = $1`, taskID)
	dbfx.Cleanup(t, `DELETE FROM task_supplement WHERE task_id = $1`, taskID)
	return agentID, taskID
}

func completeTaskViaDaemon(t *testing.T, taskID string) {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": "done"}, testWorkspaceID, "legit-daemon")
	testutil.Call(t, testHandler.CompleteTask, withURLParam(req, "taskId", taskID)).Want(http.StatusOK)
}

func outcomeFor(outcomes []CommentTriggerOutcome, agentID string) (CommentTriggerOutcome, bool) {
	for _, outcome := range outcomes {
		if outcome.TargetType == "agent" && outcome.TargetID == agentID {
			return outcome, true
		}
	}
	return CommentTriggerOutcome{}, false
}

func TestCreateCommentSteersTheThreadAgentsRunningTurn(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	dbfx.Exec(t, `UPDATE comment SET content = $2 WHERE id = $1`, f.triggerID,
		agentMention("Supplement", f.agentID)+" original objective")
	dbfx.Exec(t, `UPDATE agent_task_queue SET delivered_comment_ids = ARRAY[trigger_comment_id] WHERE id = $1`, f.taskID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)

	var created CommentResponse
	postSteeredComment(t, f.issueID, f.triggerID, "Only fix web.", f.taskID).Want(http.StatusCreated).JSON(&created)
	if len(created.Supplements) != 1 {
		t.Fatalf("receipts = %+v, want one for the running turn", created.Supplements)
	}
	receipt := created.Supplements[0]
	if receipt.TaskID != f.taskID || receipt.AgentID != f.agentID || receipt.Status != "pending" {
		t.Fatalf("receipt = %+v, want pending for task %s / agent %s", receipt, f.taskID, f.agentID)
	}
	if created.SupplementTaskID != f.taskID || created.SupplementStatus != "pending" {
		t.Fatalf("legacy receipt fields = %s/%s, want the first receipt", created.SupplementTaskID, created.SupplementStatus)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, f.issueID); n != 1 {
		t.Fatalf("steering created %d runs, want the running turn only", n)
	}

	completeTaskViaDaemon(t, f.taskID)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, f.issueID); n != 1 {
		t.Fatalf("completion replayed a steered comment into %d runs", n)
	}
}

func TestCreateCommentSteerKeepsTheNormalTriggerWhenTheTurnCannotTakeInput(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     string
		negotiated bool
	}{
		{name: "turn without the capability", status: "running", negotiated: false},
		{name: "turn not started", status: "dispatched", negotiated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSupplementFixture(t, "codex", tc.status, tc.negotiated)
			dbfx.Exec(t, `UPDATE comment SET content = $2 WHERE id = $1`, f.triggerID,
				agentMention("Supplement", f.agentID)+" original objective")
			dbfx.Exec(t, `UPDATE agent_task_queue SET delivered_comment_ids = ARRAY[trigger_comment_id] WHERE id = $1`, f.taskID)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)

			var created CommentResponse
			postSteeredComment(t, f.issueID, f.triggerID, "Only fix web.", f.taskID).Want(http.StatusCreated).JSON(&created)
			if len(created.Supplements) != 0 {
				t.Fatalf("bound a turn that cannot take input: %+v", created.Supplements)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE comment_id = $1`, created.ID); n != 0 {
				t.Fatalf("stored %d receipts", n)
			}
			if tc.status != "running" {
				return
			}
			// The comment keeps its deferred trigger and becomes the follow-up.
			completeTaskViaDaemon(t, f.taskID)
			var trigger string
			dbfx.QueryRow(t, `SELECT trigger_comment_id FROM agent_task_queue WHERE issue_id = $1 AND id <> $2`,
				f.issueID, f.taskID).Scan(&trigger)
			if trigger != created.ID {
				t.Fatalf("follow-up trigger = %s, want the unsteered comment %s", trigger, created.ID)
			}
		})
	}
}

func TestCreateCommentSteersOnlyRunningRecipients(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)
	idleAgentID := dbfx.Agent(t, "Idle recipient", f.runtimeID)
	content := agentMention("Supplement", f.agentID) + " only web; " + agentMention("Idle recipient", idleAgentID) + " check desktop"

	var created CommentResponse
	postSteeredComment(t, f.issueID, "", content, f.taskID).Want(http.StatusCreated).JSON(&created)
	if len(created.Supplements) != 1 || created.Supplements[0].AgentID != f.agentID {
		t.Fatalf("receipts = %+v, want the running agent only", created.Supplements)
	}
	if outcome, ok := outcomeFor(created.TriggerOutcomes, f.agentID); !ok || outcome.Status != DispatchSteered || outcome.ReasonCode != ReasonSteered {
		t.Fatalf("running recipient outcome = %+v (%v), want steered", outcome, ok)
	}
	if outcome, ok := outcomeFor(created.TriggerOutcomes, idleAgentID); !ok || outcome.Status == DispatchSteered || outcome.Status == DispatchBlocked {
		t.Fatalf("idle recipient outcome = %+v (%v), want a normal trigger", outcome, ok)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 AND trigger_comment_id = $2`, idleAgentID, created.ID); n != 1 {
		t.Fatalf("idle recipient has %d runs for the comment, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, f.agentID); n != 1 {
		t.Fatalf("steered recipient has %d runs, want its running turn only", n)
	}
}

func TestCreateCommentSteersSeveralRunningTurns(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	otherAgentID, otherTaskID := runningSteerableTask(t, f, "Second runner")
	content := agentMention("Supplement", f.agentID) + " and " + agentMention("Second runner", otherAgentID) + " stop touching desktop"

	var created CommentResponse
	postSteeredComment(t, f.issueID, "", content, f.taskID, otherTaskID).Want(http.StatusCreated).JSON(&created)
	got := map[string]string{}
	for _, receipt := range created.Supplements {
		got[receipt.AgentID] = receipt.TaskID
	}
	if len(created.Supplements) != 2 || got[f.agentID] != f.taskID || got[otherAgentID] != otherTaskID {
		t.Fatalf("receipts = %+v, want one per running turn", created.Supplements)
	}

	var timeline []TimelineEntry
	testutil.Call(t, testHandler.ListTimeline, withURLParam(
		newRequest(http.MethodGet, "/api/issues/"+f.issueID+"/timeline", nil), "id", f.issueID)).Want(http.StatusOK).JSON(&timeline)
	for _, entry := range timeline {
		if entry.ID == created.ID && len(entry.Supplements) != 2 {
			t.Fatalf("timeline receipts = %+v, want both runs", entry.Supplements)
		}
	}
}

func TestCompletionReplaysASteeredCommentOnlyForAgentsItDidNotSteer(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	dbfx.Exec(t, `UPDATE agent_task_queue SET delivered_comment_ids = ARRAY[trigger_comment_id] WHERE id = $1`, f.taskID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)
	otherAgentID, otherTaskID := runningSteerableTask(t, f, "Waits its turn")
	content := agentMention("Supplement", f.agentID) + " only web; " + agentMention("Waits its turn", otherAgentID) + " next round"

	var created CommentResponse
	postSteeredComment(t, f.issueID, "", content, f.taskID).Want(http.StatusCreated).JSON(&created)
	if len(created.Supplements) != 1 || created.Supplements[0].AgentID != f.agentID {
		t.Fatalf("receipts = %+v, want the steered agent only", created.Supplements)
	}

	completeTaskViaDaemon(t, otherTaskID)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1 AND trigger_comment_id = $2`, otherAgentID, created.ID); n != 1 {
		t.Fatalf("unsteered recipient got %d follow-ups, want 1", n)
	}
	completeTaskViaDaemon(t, f.taskID)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, f.agentID); n != 1 {
		t.Fatalf("steered recipient has %d runs, want no follow-up", n)
	}
}

func TestCreateCommentSteerNeverRetargetsAnotherTurn(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	dbfx.Exec(t, `UPDATE comment SET content = $2 WHERE id = $1`, f.triggerID, agentMention("A", f.agentID)+" original thread")
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)
	// The chosen turn ends before the send arrives, and the same agent has
	// meanwhile started a turn for another thread.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, f.taskID)
	otherRoot := dbfx.Comment(t, f.issueID, agentMention("A", f.agentID)+" unrelated thread")
	successor := dbfx.Task(t, f.agentID, testutil.Cols{
		"issue_id": f.issueID, "runtime_id": f.runtimeID, "trigger_comment_id": otherRoot,
		"status": "running", "started_at": testutil.Raw("now()"),
	})
	dbfx.Exec(t, `INSERT INTO task_supplement_capability (task_id, workspace_id, issue_id, capability) VALUES ($1, $2, $3, $4)`,
		successor, testWorkspaceID, f.issueID, protocol.DaemonCapabilityTaskSupplementV1)
	dbfx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id = $1`, successor)
	dbfx.Cleanup(t, `DELETE FROM task_supplement WHERE task_id = $1`, successor)

	var sent CommentResponse
	postSteeredComment(t, f.issueID, f.triggerID, "Only change the original task", f.taskID).Want(http.StatusCreated).JSON(&sent)
	if len(sent.Supplements) != 0 {
		t.Fatalf("the chosen turn ended, but the send steered another turn: %+v", sent.Supplements)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE task_id = $1`, successor); n != 0 {
		t.Fatalf("successor turn received %d steered messages", n)
	}
}

func TestCompletionKeepsAnAgentTheAuthorChoseNotToStart(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)
	otherAgent, otherTask := runningSteerableTask(t, f, "Other recipient")
	content := agentMention("A", f.agentID) + " and " + agentMention("B", otherAgent) + " only do this once"
	dbfx.Exec(t, `UPDATE comment SET content = $2 WHERE id = $1`, f.triggerID, content)
	dbfx.Exec(t, `UPDATE agent_task_queue SET trigger_comment_id = $2, comment_thread_id = $2, delivered_comment_ids = ARRAY[$2::uuid] WHERE id = $1`, otherTask, f.triggerID)
	// B is working in this thread; the reply goes to A only ("Won't start
	// this time" for B, or "Send as a new run" for a receipt A missed).
	body := map[string]any{"content": content, "parent_id": f.triggerID, "suppress_agent_ids": []string{otherAgent}}
	testutil.Call(t, testHandler.CreateComment, withURLParam(
		newRequest(http.MethodPost, "/api/issues/"+f.issueID+"/comments", body), "id", f.issueID)).Want(http.StatusCreated)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, otherAgent); n != 1 {
		t.Fatalf("the reply started another B run: %d", n)
	}
	completeTaskViaDaemon(t, otherTask)
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, otherAgent); n != 1 {
		t.Fatalf("B's completion replayed the reply into %d runs, want 1", n)
	}
}

func TestCreateCommentSteerIgnoresATurnOfAnotherIssue(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)
	// The same agent is also running on another issue.
	otherIssueID := dbfx.Issue(t, "Another issue")
	elsewhere := dbfx.Task(t, f.agentID, testutil.Cols{
		"issue_id": otherIssueID, "runtime_id": f.runtimeID, "status": "running", "started_at": testutil.Raw("now()"),
	})
	dbfx.Exec(t, `INSERT INTO task_supplement_capability (task_id, workspace_id, issue_id, capability) VALUES ($1, $2, $3, $4)`,
		elsewhere, testWorkspaceID, otherIssueID, protocol.DaemonCapabilityTaskSupplementV1)
	dbfx.Cleanup(t, `DELETE FROM task_supplement_capability WHERE task_id = $1`, elsewhere)
	content := agentMention("Supplement", f.agentID) + " only web"

	var created CommentResponse
	postSteeredComment(t, f.issueID, "", content, elsewhere).Want(http.StatusCreated).JSON(&created)
	if len(created.Supplements) != 0 {
		t.Fatalf("receipts = %+v, want none for a turn of another issue", created.Supplements)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE task_id = $1`, elsewhere); n != 0 {
		t.Fatalf("another issue's turn received %d messages", n)
	}
	if outcome, ok := outcomeFor(created.TriggerOutcomes, f.agentID); !ok || outcome.Status == DispatchSteered {
		t.Fatalf("recipient outcome = %+v (%v), want its normal trigger", outcome, ok)
	}
}

func TestUpdateCommentKeepsTheLatestEditsSuppression(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, f.issueID)
	commentID := dbfx.Comment(t, f.issueID, "initial body")
	h := *testHandler
	h.Bus = events.New()
	laterContent := agentMention("A", f.agentID) + " latest edit"
	nested := false
	h.Bus.Subscribe(protocol.EventCommentUpdated, func(events.Event) {
		if nested {
			return
		}
		nested = true
		// A second editor saves right after the first edit's text commits.
		later := withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
			"content": laterContent, "expected_revision": 2, "suppress_agent_ids": []string{},
		}), "commentId", commentID)
		testutil.Call(t, h.UpdateComment, later).Want(http.StatusOK)
	})
	first := withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
		"content": agentMention("A", f.agentID) + " first edit", "expected_revision": 1, "suppress_agent_ids": []string{f.agentID},
	}), "commentId", commentID)
	testutil.Call(t, h.UpdateComment, first).Want(http.StatusOK)
	saved, err := testHandler.Queries.GetComment(context.Background(), parseUUID(commentID))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Content != laterContent || saved.Revision != 3 {
		t.Fatalf("latest body/revision = %q/%d", saved.Content, saved.Revision)
	}
	if len(saved.SuppressedAgentIds) != 0 {
		t.Fatalf("the latest edit starts A, but an earlier edit's choice stayed: %v", saved.SuppressedAgentIds)
	}
}
