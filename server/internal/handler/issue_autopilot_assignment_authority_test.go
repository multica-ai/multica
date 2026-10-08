package handler

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// MUL-6951. An autopilot run carries the human who armed its trigger as the run's
// ORIGINATOR, so the assign surfaces judge it with the ordinary invoke gate — the
// same predicate that human gets acting directly. There is no autopilot-shaped
// authority to borrow and no scope to bound it with.
//
// What that leaves worth testing is the gate itself at every surface an agent run
// can assign through (create top-level, create child, update, batch):
//
//   - the run's human holds invoke rights   -> assigned and dispatched once
//   - the run's human holds none            -> 403 / silently skipped in a batch
//   - the run carries no human at all       -> fail closed
//
// These files previously pinned the borrow bounds instead (MUL-4857's
// autopilot-created-issue binding, MUL-6691's authored-by-this-task binding).
// Those bounds existed only because the run had no human of its own; with one,
// they describe nothing.

var runOnlyFixtureSeq atomic.Int64

// runOnlyAutopilotFixture is the run_only shape: a member-created autopilot with
// a live run, and a dispatched leader task attributed to the trigger owner, with
// NO issue of its own — the state a scan run works in.
type runOnlyAutopilotFixture struct {
	LeaderAgentID string
	LeaderTaskID  string
	AutopilotID   string
	RunID         string
	RuntimeID     string
}

// newRunOnlyAutopilotFixture wires the fixture with triggerOwnerUserID as the
// run's human (originator AND accountable — migrations 190/197 require the pair to
// match) and targetAgentID as the autopilot's assignee. Pass an override to strip
// the attribution and build the no-human case.
func newRunOnlyAutopilotFixture(t *testing.T, targetAgentID, triggerOwnerUserID string, over ...testutil.Cols) runOnlyAutopilotFixture {
	t.Helper()

	runtimeID := handlerTestRuntimeID(t)
	leaderID := dbfx.Agent(t, fmt.Sprintf("mul6951-run-only-leader-%d", runOnlyFixtureSeq.Add(1)), runtimeID, testutil.Cols{
		"permission_mode": "public_to",
		"visibility":      "workspace",
	})
	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "MUL-6951 run_only",
		"assignee_id":     targetAgentID,
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   triggerOwnerUserID,
	})
	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID,
		"status":       "running",
		"source":       "schedule",
	})

	taskCols := testutil.Cols{
		"runtime_id":          runtimeID,
		"status":              "running",
		"autopilot_run_id":    runID,
		"originator_user_id":  triggerOwnerUserID,
		"accountable_user_id": triggerOwnerUserID,
		"originator_source":   "trigger_owner",
	}
	for _, o := range over {
		for k, v := range o {
			taskCols[k] = v
		}
	}
	taskID := dbfx.Task(t, leaderID, taskCols)

	return runOnlyAutopilotFixture{
		LeaderAgentID: leaderID,
		LeaderTaskID:  taskID,
		AutopilotID:   autopilotID,
		RunID:         runID,
		RuntimeID:     runtimeID,
	}
}

// noHumanOnRun strips the run's attribution, producing the shape the gate must
// fail closed on: an agent run that reached this point with no human at the top
// of its chain.
func noHumanOnRun() testutil.Cols {
	return testutil.Cols{
		"originator_user_id":  nil,
		"accountable_user_id": nil,
		"originator_source":   nil,
	}
}

// topLevelIssueRequest is a parentless `issue create` with an assignee, spoken by
// an agent run — the request a run_only leader makes when it turns scan results
// into work.
func topLevelIssueRequest(t *testing.T, assigneeType, assigneeID, status, actorAgentID, taskID string) *http.Request {
	t.Helper()
	r := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "MUL-6951 top-level " + t.Name(),
		"status":          status,
		"priority":        "low",
		"assignee_type":   assigneeType,
		"assignee_id":     assigneeID,
		"allow_duplicate": true,
	})
	if actorAgentID != "" {
		r.Header.Set("X-Agent-ID", actorAgentID)
	}
	if taskID != "" {
		r.Header.Set("X-Task-ID", taskID)
	}
	return r
}

func autopilotChildIssueRequest(t *testing.T, assigneeType, assigneeID, parentIssueID, status, actorAgentID, taskID string) *http.Request {
	t.Helper()

	r := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "autopilot private-assignee child " + t.Name(),
		"status":          status,
		"priority":        "low",
		"assignee_type":   assigneeType,
		"assignee_id":     assigneeID,
		"parent_issue_id": parentIssueID,
		"allow_duplicate": true,
	})
	if actorAgentID != "" {
		r.Header.Set("X-Agent-ID", actorAgentID)
	}
	if taskID != "" {
		r.Header.Set("X-Task-ID", taskID)
	}
	return r
}

func cleanupAutopilotChildIssue(t *testing.T, issueID string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
}

// createUnassignedIssueAsRun has the run create a parentless issue with no
// assignee (always allowed) and returns its id.
func createUnassignedIssueAsRun(t *testing.T, actorAgentID, taskID string) string {
	t.Helper()
	r := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "MUL-6951 unassigned " + t.Name(),
		"status":          "todo",
		"priority":        "low",
		"allow_duplicate": true,
	})
	r.Header.Set("X-Agent-ID", actorAgentID)
	r.Header.Set("X-Task-ID", taskID)

	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue, r).Want(http.StatusCreated).JSON(&created)
	cleanupAutopilotChildIssue(t, created.ID)
	return created.ID
}

// seedMemberIssue inserts an issue created by a human member — the pre-existing
// record an autopilot may now be asked to route.
func seedMemberIssue(t *testing.T, creatorMemberID string) string {
	t.Helper()
	return dbfx.Insert(t, "issue", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"creator_type": "member",
		"creator_id":   creatorMemberID,
		"title":        "MUL-6951 member-created " + t.Name(),
		"status":       "todo",
		"number":       nextWorkspaceIssueNumber(t),
	})
}

// tasksFor returns (task count, non-null originator count) for an (issue, agent)
// pair, so a test can assert both "was it dispatched" and "did the run's human
// travel onto the dispatched task".
func tasksFor(t *testing.T, issueID, agentID string) (int, int) {
	t.Helper()
	var total, withOriginator int
	dbfx.QueryRow(t, `
		SELECT count(*), count(originator_user_id) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
	`, issueID, agentID).Scan(&total, &withOriginator)
	return total, withOriginator
}

// assigneeOf returns an issue's assignee agent id, or "" when unassigned.
func assigneeOf(t *testing.T, issueID string) string {
	t.Helper()
	var assignee *string
	dbfx.QueryRow(t, `SELECT assignee_id::text FROM issue WHERE id = $1`, issueID).Scan(&assignee)
	if assignee == nil {
		return ""
	}
	return *assignee
}

// batchAssignAsRun points every issue in issueIDs at targetAgentID through
// BatchUpdateIssues, speaking as an agent run.
//
// The header trio mirrors a real task token: the auth middleware writes
// X-User-ID (the token's bound member), X-Agent-ID and X-Task-ID together, so
// this endpoint's requireUserID is satisfied while resolveActor still classifies
// the caller as an agent. headerUserID is deliberately a member with NO rights on
// the target, so any success proves the authority came from the run's originator
// rather than from the header user.
func batchAssignAsRun(t *testing.T, headerUserID, agentID, taskID, targetAgentID string, issueIDs ...string) *testutil.Response {
	t.Helper()
	req := newRequestAs(headerUserID, http.MethodPost, "/api/issues/batch?workspace_id="+testWorkspaceID, map[string]any{
		"issue_ids": issueIDs,
		"updates": map[string]any{
			"assignee_type": "agent",
			"assignee_id":   targetAgentID,
		},
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return testutil.Call(t, testHandler.BatchUpdateIssues, req)
}

func TestCreateIssue_AutopilotRunAssignsPrivateAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("top-level create dispatches the private worker once", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)

		var created IssueResponse
		testutil.Call(t, testHandler.CreateIssue,
			topLevelIssueRequest(t, "agent", workerID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
		).Want(http.StatusCreated).JSON(&created)
		cleanupAutopilotChildIssue(t, created.ID)

		total, withOriginator := tasksFor(t, created.ID, workerID)
		if total != 1 {
			t.Fatalf("dispatched %d tasks, want 1", total)
		}
		// MUL-6951: the dispatched run inherits the trigger owner, so the chain no
		// longer has to re-establish authority at each hop. Under the previous
		// design this count was 0 and every hop re-borrowed.
		if withOriginator != 1 {
			t.Fatalf("%d of %d dispatched tasks carry an originator, want all", withOriginator, total)
		}
	})

	t.Run("top-level create accepts a private-leader squad", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		squadID := dbfx.Squad(t, "MUL-6951 squad "+t.Name(), workerID, testutil.Cols{"creator_id": ownerID})
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)

		var created IssueResponse
		testutil.Call(t, testHandler.CreateIssue,
			topLevelIssueRequest(t, "squad", squadID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
		).Want(http.StatusCreated).JSON(&created)
		cleanupAutopilotChildIssue(t, created.ID)
	})

	t.Run("backlog child parks without enqueueing", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		parentID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		var created IssueResponse
		testutil.Call(t, testHandler.CreateIssue,
			autopilotChildIssueRequest(t, "agent", workerID, parentID, "backlog", fx.LeaderAgentID, fx.LeaderTaskID),
		).Want(http.StatusCreated).JSON(&created)
		cleanupAutopilotChildIssue(t, created.ID)

		if total, _ := tasksFor(t, created.ID, workerID); total != 0 {
			t.Fatalf("a backlog child must not enqueue, got %d tasks", total)
		}
	})

	t.Run("child under an issue the run did not create is admitted", func(t *testing.T) {
		// The old binding refused this: the parent was neither autopilot-created
		// nor authored by this task. The run's human owns the worker, so it is
		// simply allowed now.
		workerID, ownerID, otherMemberID := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		parentID := seedMemberIssue(t, otherMemberID)

		var created IssueResponse
		testutil.Call(t, testHandler.CreateIssue,
			autopilotChildIssueRequest(t, "agent", workerID, parentID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
		).Want(http.StatusCreated).JSON(&created)
		cleanupAutopilotChildIssue(t, created.ID)

		if total, _ := tasksFor(t, created.ID, workerID); total != 1 {
			t.Fatalf("dispatched %d tasks, want 1", total)
		}
	})

	t.Run("denials", func(t *testing.T) {
		t.Run("run's human holds no invoke rights", func(t *testing.T) {
			workerID, _, plainMemberID := privateAgentTestFixture(t)
			fx := newRunOnlyAutopilotFixture(t, workerID, plainMemberID)

			testutil.Call(t, testHandler.CreateIssue,
				topLevelIssueRequest(t, "agent", workerID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
			).Want(http.StatusForbidden)
		})

		t.Run("run carries no human", func(t *testing.T) {
			workerID, ownerID, _ := privateAgentTestFixture(t)
			fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, noHumanOnRun())

			testutil.Call(t, testHandler.CreateIssue,
				topLevelIssueRequest(t, "agent", workerID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
			).Want(http.StatusForbidden)
		})

		t.Run("cross-workspace parent is rejected before the assignee gate", func(t *testing.T) {
			// 400, not 403: the parent lookup still runs first so the caller learns
			// which input was wrong. This is the reason CreateIssue keeps loading the
			// parent even though the gate no longer needs it.
			workerID, ownerID, _ := privateAgentTestFixture(t)
			fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
			foreignUserID := dbfx.User(t, "MUL-6951 Foreign", fmt.Sprintf("mul6951-foreign-%d@multica.test", runOnlyFixtureSeq.Add(1)))
			foreignWorkspaceID := dbfx.Workspace(t, "MUL-6951 Foreign WS", fmt.Sprintf("mul6951-foreign-ws-%d", runOnlyFixtureSeq.Add(1)))
			var foreignIssueID string
			dbfx.QueryRow(t, `
				INSERT INTO issue (workspace_id, creator_type, creator_id, title, status, number)
				VALUES ($1, 'member', $2, 'MUL-6951 foreign parent', 'todo', 1) RETURNING id
			`, foreignWorkspaceID, foreignUserID).Scan(&foreignIssueID)
			dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, foreignIssueID)

			testutil.Call(t, testHandler.CreateIssue,
				autopilotChildIssueRequest(t, "agent", workerID, foreignIssueID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
			).Want(http.StatusBadRequest)
		})
	})
}

func TestUpdateIssue_AutopilotRunAssignsPrivateAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("assigns an issue created by another member", func(t *testing.T) {
		// The capability #7902 tried to reach with a fourth borrow scope. With a
		// real originator it needs no special case: the run's human may assign any
		// issue in the workspace they could assign by hand.
		workerID, ownerID, otherMemberID := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		issueID := seedMemberIssue(t, otherMemberID)

		agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, issueID, workerID).Want(http.StatusOK)

		if got := assigneeOf(t, issueID); got != workerID {
			t.Fatalf("assignee = %q, want %q", got, workerID)
		}
		if total, _ := tasksFor(t, issueID, workerID); total != 1 {
			t.Fatalf("enqueued %d tasks, want 1", total)
		}
	})

	t.Run("assigns an issue it created itself", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		issueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, issueID, workerID).Want(http.StatusOK)

		if got := assigneeOf(t, issueID); got != workerID {
			t.Fatalf("assignee = %q, want %q", got, workerID)
		}
	})

	t.Run("run's human holds no invoke rights", func(t *testing.T) {
		workerID, _, plainMemberID := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, plainMemberID)
		issueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, issueID, workerID).Want(http.StatusForbidden)

		if got := assigneeOf(t, issueID); got != "" {
			t.Fatalf("issue was assigned to %q", got)
		}
	})

	t.Run("run carries no human", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, noHumanOnRun())
		issueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, issueID, workerID).Want(http.StatusForbidden)

		if got := assigneeOf(t, issueID); got != "" {
			t.Fatalf("issue was assigned to %q", got)
		}
	})

	// MUL-6951 (Elon review). A run represents its human only while it is live.
	// Task-token revocation at the terminal transition is the primary guard, but it
	// is best-effort and non-fatal on failure with a 24h expiry behind it, so a
	// failed revocation would otherwise leave a finished run spending a member's
	// invoke rights. Every terminal status must be refused.
	for _, status := range []string{"completed", "failed", "cancelled"} {
		t.Run("run already "+status, func(t *testing.T) {
			workerID, ownerID, _ := privateAgentTestFixture(t)
			fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
			issueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

			// The originator is intact and would otherwise admit — only the task
			// reaching a terminal state withdraws it.
			dbfx.Exec(t, `UPDATE agent_task_queue SET status = $1 WHERE id = $2`, status, fx.LeaderTaskID)

			agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, issueID, workerID).Want(http.StatusForbidden)

			if got := assigneeOf(t, issueID); got != "" {
				t.Fatalf("a %s run assigned the issue to %q", status, got)
			}
			if total, _ := tasksFor(t, issueID, workerID); total != 0 {
				t.Fatalf("a %s run enqueued %d tasks", status, total)
			}
		})
	}

	t.Run("plain member is still refused", func(t *testing.T) {
		workerID, _, plainMemberID := privateAgentTestFixture(t)
		issueID := seedMemberIssue(t, plainMemberID)

		testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
			newRequestAs(plainMemberID, http.MethodPatch, "/api/issues/"+issueID, map[string]any{
				"assignee_type": "agent",
				"assignee_id":   workerID,
			}),
			"id", issueID,
		)).Want(http.StatusForbidden)
	})
}

// BatchUpdateIssues is a real agent-reachable authorization point, so it needs its
// own coverage even though it now runs the same predicate as the single update.
func TestBatchUpdateIssues_AutopilotRunAssign(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("assigns every issue when the run's human holds rights", func(t *testing.T) {
		workerID, ownerID, otherMemberID := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		ownIssueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)
		otherIssueID := seedMemberIssue(t, otherMemberID)

		batchAssignAsRun(t, otherMemberID, fx.LeaderAgentID, fx.LeaderTaskID, workerID, ownIssueID, otherIssueID).
			Want(http.StatusOK)

		for _, id := range []string{ownIssueID, otherIssueID} {
			if got := assigneeOf(t, id); got != workerID {
				t.Fatalf("issue %s assignee = %q, want %q", id, got, workerID)
			}
		}
	})

	t.Run("assigns nothing when the run's human holds no rights", func(t *testing.T) {
		// The batch endpoint SKIPS an issue whose assignee validation fails and
		// still answers 200 — a partial success the caller is not told about. That
		// is pre-existing behaviour, pinned here because the mixed-outcome case is
		// now reachable through ordinary permissions rather than a borrow scope.
		workerID, _, plainMemberID := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, plainMemberID)
		issueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		batchAssignAsRun(t, plainMemberID, fx.LeaderAgentID, fx.LeaderTaskID, workerID, issueID).
			Want(http.StatusOK)

		if got := assigneeOf(t, issueID); got != "" {
			t.Fatalf("issue was assigned to %q despite no invoke rights", got)
		}
		if total, _ := tasksFor(t, issueID, workerID); total != 0 {
			t.Fatalf("enqueued %d tasks despite no invoke rights", total)
		}
	})

	t.Run("assigns nothing when the run carries no human", func(t *testing.T) {
		workerID, ownerID, plainMemberID := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, noHumanOnRun())
		issueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		batchAssignAsRun(t, plainMemberID, fx.LeaderAgentID, fx.LeaderTaskID, workerID, issueID).
			Want(http.StatusOK)

		if got := assigneeOf(t, issueID); got != "" {
			t.Fatalf("issue was assigned to %q by a run with no human", got)
		}
	})
}
