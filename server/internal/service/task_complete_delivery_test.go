package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// deliveryFixture is a run of agent on source issue A that reports to target
// issue B. It counts the agent's comments on each issue.
type deliveryFixture struct {
	f       principalFixture
	s       *TaskService
	source  pgtype.UUID
	target  string
	agentID string
}

func newDeliveryFixture(t *testing.T) deliveryFixture {
	t.Helper()
	f, wake, source, agentID := wakeFixture(t)
	f.Cleanup(t, "DELETE FROM comment WHERE issue_id=$1", source)
	target := f.Issue(t, "Delivery target")
	f.Cleanup(t, "DELETE FROM comment WHERE issue_id=$1", target)
	return deliveryFixture{f: f, s: wake.Tasks, source: source, target: target, agentID: agentID}
}

// runningTask starts a run of the agent on the source issue a minute ago.
func (d deliveryFixture) runningTask(t *testing.T, over testutil.Cols) pgtype.UUID {
	t.Helper()
	cols := testutil.Cols{
		"issue_id":   d.source,
		"status":     "running",
		"started_at": testutil.Raw("now() - interval '1 minute'"),
		"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + d.agentID + "')"),
	}
	for k, v := range over {
		cols[k] = v
	}
	return parseTestUUID(t, d.f.Task(t, d.agentID, cols))
}

// reportOnTarget posts the agent's explicit report on the target issue, as
// `multica issue comment add <target>` does during the run.
func (d deliveryFixture) reportOnTarget(t *testing.T) {
	t.Helper()
	d.f.Comment(t, d.target, "Report for the target issue.", testutil.Cols{"author_type": "agent", "author_id": d.agentID})
}

func (d deliveryFixture) agentComments(t *testing.T, issue any) int {
	t.Helper()
	return d.f.Count(t, "SELECT count(*) FROM comment WHERE issue_id=$1 AND author_type='agent' AND author_id=$2", issue, d.agentID)
}

func (d deliveryFixture) complete(t *testing.T, task pgtype.UUID, output string) bool {
	t.Helper()
	result, err := json.Marshal(protocol.TaskCompletedPayload{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	_, transitioned, err := d.s.CompleteTaskWithTransition(context.Background(), task, result, "", "", "", false, "", "")
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	return transitioned
}

// A run on issue A that reported on issue B and then produced empty output
// (the Codex adapter's result for an empty final_answer after commentary)
// leaves exactly the one report on B and posts nothing on A, for assignment-
// and comment-triggered runs alike.
func TestCompleteTaskEmptyOutputAfterCrossIssueReportPostsNothingOnSource(t *testing.T) {
	d := newDeliveryFixture(t)

	for _, tc := range []struct {
		name string
		over testutil.Cols
	}{
		{name: "assignment run", over: nil},
		{name: "comment-triggered run", over: testutil.Cols{"trigger_comment_id": d.f.Comment(t, util.UUIDToString(d.source), "please report on B")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sourceBefore, targetBefore := d.agentComments(t, d.source), d.agentComments(t, d.target)
			task := d.runningTask(t, tc.over)
			d.reportOnTarget(t)

			if !d.complete(t, task, "") {
				t.Fatal("the first completion callback did not transition the task")
			}
			if got := d.agentComments(t, d.source) - sourceBefore; got != 0 {
				t.Fatalf("source issue got %d agent comments, want none", got)
			}
			if got := d.agentComments(t, d.target) - targetBefore; got != 1 {
				t.Fatalf("target issue has %d new agent comments, want exactly the report", got)
			}
		})
	}
}

// The fallback guard only looks at the source issue, so a non-empty output
// still becomes a comment on A even after an explicit report on B. This pins
// the existing contract: there is no destination or suppression field, and
// the adapter fix does not add one.
func TestCompleteTaskNonEmptyOutputAfterCrossIssueReportStillFallsBackToSource(t *testing.T) {
	d := newDeliveryFixture(t)
	task := d.runningTask(t, nil)
	d.reportOnTarget(t)

	if !d.complete(t, task, "Final answer text.") {
		t.Fatal("the first completion callback did not transition the task")
	}
	if got := d.f.Count(t, "SELECT count(*) FROM comment WHERE issue_id=$1 AND source_task_id=$2", d.source, task); got != 1 {
		t.Fatalf("source issue got %d fallback comments, want 1 under the current contract", got)
	}
	if got := d.agentComments(t, d.target); got != 1 {
		t.Fatalf("target issue has %d agent comments, want exactly the report", got)
	}
}

// A replayed terminal callback (the daemon retries until acknowledged) is an
// idempotent no-op: one completion, one result comment, no new task.
func TestCompleteTaskRepeatedCallbackPostsOneResult(t *testing.T) {
	d := newDeliveryFixture(t)

	for _, tc := range []struct {
		name         string
		output       string
		wantComments int
	}{
		{name: "non-empty output", output: "Final answer text.", wantComments: 1},
		{name: "empty output", output: "", wantComments: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := d.runningTask(t, nil)
			if !d.complete(t, task, tc.output) {
				t.Fatal("the first completion callback did not transition the task")
			}
			tasksAfterFirst := d.f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", d.source)

			if d.complete(t, task, tc.output) {
				t.Fatal("a replayed completion callback transitioned the task again")
			}
			if got := d.f.Count(t, "SELECT count(*) FROM comment WHERE issue_id=$1 AND source_task_id=$2", d.source, task); got != tc.wantComments {
				t.Fatalf("source issue has %d result comments after the replay, want %d", got, tc.wantComments)
			}
			if got := d.f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", d.source); got != tasksAfterFirst {
				t.Fatalf("the replayed callback queued %d new tasks", got-tasksAfterFirst)
			}
		})
	}
}

// A completion callback that arrives after the run already failed or was
// cancelled neither flips the status nor publishes the output.
func TestCompleteTaskAfterFailedOrCancelledPublishesNothing(t *testing.T) {
	d := newDeliveryFixture(t)

	for _, status := range []string{"failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			task := d.runningTask(t, testutil.Cols{"status": status, "completed_at": testutil.Raw("now()")})
			if d.complete(t, task, "Late final answer.") {
				t.Fatalf("completion transitioned a %s task", status)
			}
			if got := d.f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status=$2", task, status); got != 1 {
				t.Fatalf("task status changed from %s", status)
			}
			if got := d.f.Count(t, "SELECT count(*) FROM comment WHERE source_task_id=$1", task); got != 0 {
				t.Fatalf("late completion of a %s task posted %d comments", status, got)
			}
		})
	}
}
