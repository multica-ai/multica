package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// These tests pin the authorization contract of RecordSquadLeaderEvaluation
// after MUL-6622 / GH #7487. Two gates, in order: the caller owns the task, and
// the caller is still the squad's leader. The target issue's own assignee is
// deliberately not consulted.

type squadEvalFixture struct {
	SquadID      string
	LeaderID     string
	OtherID      string
	SquadIssueID string // issue assigned to the squad
}

func newSquadEvalFixture(t *testing.T) squadEvalFixture {
	t.Helper()

	leaderID := createHandlerTestAgent(t, "Squad Eval Leader", nil)
	otherID := createHandlerTestAgent(t, "Squad Eval Other", nil)
	squadID := dbfx.Squad(t, "Squad Eval", leaderID)
	issueID := dbfx.Issue(t, "squad eval owner issue", testutil.Cols{
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})

	return squadEvalFixture{
		SquadID:      squadID,
		LeaderID:     leaderID,
		OtherID:      otherID,
		SquadIssueID: issueID,
	}
}

// leaderTask seeds a running task bound to issueID. isLeaderTask / squadID carry
// the exact provenance under test; an empty squadID leaves squad_id NULL.
func leaderTask(t *testing.T, agentID, issueID string, isLeaderTask bool, squadID string) string {
	t.Helper()

	cols := testutil.Cols{
		"runtime_id":     handlerTestRuntimeID(t),
		"status":         "running",
		"issue_id":       issueID,
		"started_at":     testutil.Raw("now()"),
		"is_leader_task": isLeaderTask,
	}
	if squadID != "" {
		cols["squad_id"] = squadID
	}
	return dbfx.Task(t, agentID, cols)
}

func evaluationRequest(issueID, agentID, taskID, outcome string) *http.Request {
	return testutil.WithHeaders(
		testutil.WithURLParams(
			newRequest(http.MethodPost, "/api/issues/"+issueID+"/squad-evaluated",
				map[string]any{"outcome": outcome, "reason": "test reason"}),
			"id", issueID,
		),
		"X-Agent-ID", agentID,
		"X-Task-ID", taskID,
	)
}

type recordedEvaluation struct {
	ActorID string
	SquadID string
	Outcome string
}

func loadEvaluations(t *testing.T, issueID string) []recordedEvaluation {
	t.Helper()

	rows, err := testPool.Query(context.Background(), `
		SELECT actor_id, details->>'squad_id', details->>'outcome'
		FROM activity_log
		WHERE issue_id = $1 AND action = 'squad_leader_evaluated'
		ORDER BY created_at ASC
	`, issueID)
	if err != nil {
		t.Fatalf("load evaluations: %v", err)
	}
	defer rows.Close()

	var out []recordedEvaluation
	for rows.Next() {
		var e recordedEvaluation
		if err := rows.Scan(&e.ActorID, &e.SquadID, &e.Outcome); err != nil {
			t.Fatalf("scan evaluation: %v", err)
		}
		out = append(out, e)
	}
	return out
}

// The regression: a leader task on an issue owned by an individual agent (the
// `@squad`-mention path) used to be rejected with "issue is not assigned to a
// squad", dropping the decision entirely.
func TestRecordSquadLeaderEvaluation_AcceptedOnNonSquadAssignedIssue(t *testing.T) {
	fx := newSquadEvalFixture(t)
	issueID := dbfx.Issue(t, "agent-owned issue", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   fx.OtherID,
	})
	taskID := leaderTask(t, fx.LeaderID, issueID, true, fx.SquadID)

	testutil.Call(t, testHandler.RecordSquadLeaderEvaluation,
		evaluationRequest(issueID, fx.LeaderID, taskID, "no_action")).Want(http.StatusCreated)

	got := loadEvaluations(t, issueID)
	if len(got) != 1 {
		t.Fatalf("expected exactly one recorded evaluation, got %d", len(got))
	}
	// actor_id must be the task's agent: the no_action comment suppression
	// lookup matches on task.agent_id.
	if got[0].ActorID != fx.LeaderID {
		t.Fatalf("actor_id: want task agent %s, got %s", fx.LeaderID, got[0].ActorID)
	}
	if got[0].SquadID != fx.SquadID {
		t.Fatalf("details.squad_id: want task squad %s, got %s", fx.SquadID, got[0].SquadID)
	}
	if got[0].Outcome != "no_action" {
		t.Fatalf("outcome: want no_action, got %s", got[0].Outcome)
	}
}

// A child issue the leader itself is running on records fine too — the parent's
// squad assignment is irrelevant to the check.
func TestRecordSquadLeaderEvaluation_AcceptedOnChildIssueBoundTask(t *testing.T) {
	fx := newSquadEvalFixture(t)
	childID := dbfx.Issue(t, "squad child issue", testutil.Cols{
		"assignee_type":   "agent",
		"assignee_id":     fx.OtherID,
		"parent_issue_id": fx.SquadIssueID,
	})
	taskID := leaderTask(t, fx.LeaderID, childID, true, fx.SquadID)

	testutil.Call(t, testHandler.RecordSquadLeaderEvaluation,
		evaluationRequest(childID, fx.LeaderID, taskID, "action")).Want(http.StatusCreated)

	if got := loadEvaluations(t, childID); len(got) != 1 {
		t.Fatalf("expected one recorded evaluation on the child, got %d", len(got))
	}
}

// Behavior narrowing made explicit: the leader agent running a task that is NOT
// a leader task is not running as the leader, so it may not record.
func TestRecordSquadLeaderEvaluation_RejectsNonLeaderTask(t *testing.T) {
	fx := newSquadEvalFixture(t)
	taskID := leaderTask(t, fx.LeaderID, fx.SquadIssueID, false, fx.SquadID)

	testutil.Call(t, testHandler.RecordSquadLeaderEvaluation,
		evaluationRequest(fx.SquadIssueID, fx.LeaderID, taskID, "no_action")).Want(http.StatusBadRequest)

	if got := loadEvaluations(t, fx.SquadIssueID); len(got) != 0 {
		t.Fatalf("expected no evaluation recorded, got %d", len(got))
	}
}

// A leader task without a stamped squad cannot be attributed to a squad.
func TestRecordSquadLeaderEvaluation_RejectsLeaderTaskWithoutSquadID(t *testing.T) {
	fx := newSquadEvalFixture(t)
	taskID := leaderTask(t, fx.LeaderID, fx.SquadIssueID, true, "")

	testutil.Call(t, testHandler.RecordSquadLeaderEvaluation,
		evaluationRequest(fx.SquadIssueID, fx.LeaderID, taskID, "no_action")).Want(http.StatusBadRequest)

	if got := loadEvaluations(t, fx.SquadIssueID); len(got) != 0 {
		t.Fatalf("expected no evaluation recorded, got %d", len(got))
	}
}

// Recording still binds to the task's own issue, and the error names it — the
// stage-barrier case wakes the leader on the PARENT, so a leader that reaches
// for the child id gets told where to record instead of a dead end. Naming it is
// only safe because the ownership gate has already passed.
func TestRecordSquadLeaderEvaluation_RejectsCrossIssueTaskAndNamesTaskIssue(t *testing.T) {
	fx := newSquadEvalFixture(t)
	childID := dbfx.Issue(t, "stage barrier child", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   fx.OtherID,
	})
	taskID := leaderTask(t, fx.LeaderID, fx.SquadIssueID, true, fx.SquadID)

	body := testutil.Call(t, testHandler.RecordSquadLeaderEvaluation,
		evaluationRequest(childID, fx.LeaderID, taskID, "no_action")).Want(http.StatusBadRequest).Text()

	if !strings.Contains(body, fx.SquadIssueID) {
		t.Fatalf("expected the error to name the task's issue %s, got %q", fx.SquadIssueID, body)
	}
	if got := loadEvaluations(t, childID); len(got) != 0 {
		t.Fatalf("expected no evaluation recorded on the child, got %d", len(got))
	}
}

// An agent that is not the task's agent may not record on its behalf, and the
// rejection must not disclose anything about that task.
func TestRecordSquadLeaderEvaluation_RejectsForeignAgentWithoutLeakingTaskIssue(t *testing.T) {
	fx := newSquadEvalFixture(t)
	leaderOnly := dbfx.Issue(t, "leader-only issue", testutil.Cols{
		"assignee_type": "squad",
		"assignee_id":   fx.SquadID,
	})
	taskID := leaderTask(t, fx.LeaderID, leaderOnly, true, fx.SquadID)

	body := testutil.Call(t, testHandler.RecordSquadLeaderEvaluation,
		evaluationRequest(fx.SquadIssueID, fx.OtherID, taskID, "no_action")).Want(http.StatusForbidden).Text()

	if strings.Contains(body, leaderOnly) {
		t.Fatalf("403 body leaked the task's issue id %s: %q", leaderOnly, body)
	}
	if got := loadEvaluations(t, fx.SquadIssueID); len(got) != 0 {
		t.Fatalf("expected no evaluation recorded, got %d", len(got))
	}
}

// Tenant isolation: a task id from another workspace must be unreadable through
// an issue the caller legitimately owns, and its issue id must not appear in the
// response. GetAgentTask is a global lookup, so the workspace-scoped query plus
// the ownership gate are what stop the probe.
func TestRecordSquadLeaderEvaluation_RejectsForeignWorkspaceTaskWithoutLeakingItsIssue(t *testing.T) {
	fx := newSquadEvalFixture(t)

	foreignUser := dbfx.User(t, "Squad Eval Foreign User", "squad-eval-foreign@example.com")
	foreignWorkspace := dbfx.Workspace(t, "Squad Eval Foreign", "squad-eval-foreign")
	dbfx.Member(t, foreignWorkspace, foreignUser, "owner")
	foreignRuntime := dbfx.Runtime(t, "Squad Eval Foreign Runtime", testutil.Cols{
		"workspace_id": foreignWorkspace,
		"owner_id":     foreignUser,
	})
	foreignAgent := dbfx.Agent(t, "Squad Eval Foreign Agent", foreignRuntime, testutil.Cols{
		"workspace_id": foreignWorkspace,
		"owner_id":     foreignUser,
	})
	foreignSquad := dbfx.Squad(t, "Squad Eval Foreign Squad", foreignAgent, testutil.Cols{
		"workspace_id": foreignWorkspace,
		"creator_id":   foreignUser,
	})
	foreignIssue := dbfx.Issue(t, "foreign workspace issue", testutil.Cols{
		"workspace_id":  foreignWorkspace,
		"creator_id":    foreignUser,
		"assignee_type": "squad",
		"assignee_id":   foreignSquad,
	})
	foreignTask := dbfx.Task(t, foreignAgent, testutil.Cols{
		"runtime_id":     foreignRuntime,
		"status":         "running",
		"issue_id":       foreignIssue,
		"started_at":     testutil.Raw("now()"),
		"is_leader_task": true,
		"squad_id":       foreignSquad,
	})

	body := testutil.Call(t, testHandler.RecordSquadLeaderEvaluation,
		evaluationRequest(fx.SquadIssueID, fx.LeaderID, foreignTask, "no_action")).
		Want(http.StatusBadRequest).Text()

	if strings.Contains(body, foreignIssue) {
		t.Fatalf("rejection leaked a foreign workspace issue id %s: %q", foreignIssue, body)
	}
	if got := loadEvaluations(t, fx.SquadIssueID); len(got) != 0 {
		t.Fatalf("expected no evaluation recorded, got %d", len(got))
	}
}

// The claim path clears the leader role when the leader was swapped before the
// claim, but leaves is_leader_task = true on the row. The row therefore records
// enqueue-time intent, not the delivered role, so a run that was downgraded to
// an ordinary agent turn must not be able to write a leader verdict here.
func TestRecordSquadLeaderEvaluation_RejectsAfterLeaderChange(t *testing.T) {
	fx := newSquadEvalFixture(t)
	taskID := leaderTask(t, fx.LeaderID, fx.SquadIssueID, true, fx.SquadID)

	newLeader := createHandlerTestAgent(t, "Squad Eval New Leader", nil)
	dbfx.Exec(t, `UPDATE squad SET leader_id = $1 WHERE id = $2`, newLeader, fx.SquadID)

	testutil.Call(t, testHandler.RecordSquadLeaderEvaluation,
		evaluationRequest(fx.SquadIssueID, fx.LeaderID, taskID, "no_action")).Want(http.StatusForbidden)

	if got := loadEvaluations(t, fx.SquadIssueID); len(got) != 0 {
		t.Fatalf("expected no evaluation recorded after a leader change, got %d", len(got))
	}
}
