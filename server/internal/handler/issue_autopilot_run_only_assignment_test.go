package handler

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// runOnlyFixtureSeq keeps leader agent names unique: the agent table has a
// per-workspace name constraint and one test builds two fixtures.
var runOnlyFixtureSeq atomic.Int64

// MUL-6691 / GH #7563. A schedule/webhook autopilot run carries no top-of-chain
// human originator by design (MUL-4302), so canInvokeAgent fails closed for a
// private agent. MUL-4857 restored the autopilot's authority for ONE action —
// creating a child under the autopilot's own issue — which left the reported
// flow broken in two places:
//
//   - a run_only leader has no autopilot issue at all, so its top-level
//     `issue create` had nothing to borrow against;
//   - assigning an ALREADY-created issue went through UpdateIssue, which passed
//     no scope whatsoever, so even the create_issue lineage MUL-4857 accepts was
//     refused there. That is the exact reported symptom: DRA-109/DRA-110 got
//     created and then could not be pointed at anyone.
//
// These tests pin the repaired positive paths and, more importantly, the bounds:
// the borrowed authority comes from the task's OWN accountable human, only while
// the task runs, only for work the task verifiably owns, and never from
// owner_fallback (which is the agent owner, i.e. the white-list itself).

// runOnlyAutopilotFixture is the run_only shape: a member-created autopilot with
// a live run, and a dispatched leader task that carries autopilot attribution
// (accountable set, originator NULL) and NO issue — the state the reported Squad
// scan runs in.
type runOnlyAutopilotFixture struct {
	LeaderAgentID string
	LeaderTaskID  string
	AutopilotID   string
	RunID         string
	RuntimeID     string
}

// newRunOnlyAutopilotFixture wires the fixture with accountableUserID as the
// task's accountable human (the trigger_owner MUL-4302 resolves) and
// targetAgentID as the autopilot's assignee.
func newRunOnlyAutopilotFixture(t *testing.T, targetAgentID, accountableUserID string, over ...testutil.Cols) runOnlyAutopilotFixture {
	t.Helper()

	runtimeID := handlerTestRuntimeID(t)
	leaderID := dbfx.Agent(t, fmt.Sprintf("mul6691-run-only-leader-%d", runOnlyFixtureSeq.Add(1)), runtimeID, testutil.Cols{
		"permission_mode": "public_to",
		"visibility":      "workspace",
	})
	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "MUL-6691 run_only",
		"assignee_id":     targetAgentID,
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   accountableUserID,
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
		"accountable_user_id": accountableUserID,
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

// topLevelIssueRequest is a parentless `issue create` with an assignee, spoken by
// an agent run — the request a run_only leader makes when it turns scan results
// into work.
func topLevelIssueRequest(t *testing.T, assigneeType, assigneeID, status, actorAgentID, taskID string) *http.Request {
	t.Helper()
	r := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "MUL-6691 top-level " + t.Name(),
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

// createUnassignedIssueAsRun has the run create a parentless issue with no
// assignee (always allowed) and returns its id. This is the DRA-109 state the
// report got stuck in.
func createUnassignedIssueAsRun(t *testing.T, actorAgentID, taskID string) string {
	t.Helper()
	r := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "MUL-6691 unassigned " + t.Name(),
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

// stampAutopilotAttribution gives a task the attribution a production autopilot
// enqueue writes (MUL-4302): an accountable human, no originator, and the source
// label that proves autopilot lineage. The raw fixtures insert bare task rows.
func stampAutopilotAttribution(t *testing.T, taskID, accountableUserID, source string) {
	t.Helper()
	dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET accountable_user_id = $1, originator_source = $2
		WHERE id = $3
	`, accountableUserID, source, taskID)
}

// tasksFor returns (task count, non-null originator count) for an (issue, agent)
// pair, so a test can assert both "was it dispatched" and "did the borrowed
// authority leak into attribution".
func tasksFor(t *testing.T, issueID, agentID string) (int, int) {
	t.Helper()
	var total, withOriginator int
	dbfx.QueryRow(t, `
		SELECT count(*), count(originator_user_id) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
	`, issueID, agentID).Scan(&total, &withOriginator)
	return total, withOriginator
}

func TestCreateIssue_RunOnlyAutopilotLeaderAssignsPrivateWorker(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("top-level create dispatches the private worker once, unattributed", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)

		var created IssueResponse
		testutil.Call(t, testHandler.CreateIssue,
			topLevelIssueRequest(t, "agent", workerID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
		).Want(http.StatusCreated).JSON(&created)
		cleanupAutopilotChildIssue(t, created.ID)

		total, withOriginator := tasksFor(t, created.ID, workerID)
		if total != 1 {
			t.Fatalf("run_only top-level create must enqueue the private worker exactly once, got %d tasks", total)
		}
		if withOriginator != 0 {
			t.Fatal("borrowed authority is authorization-only; the worker task must stay unattributed")
		}
	})

	t.Run("top-level create accepts a private-leader squad", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		squadID := dbfx.Squad(t, "MUL-6691 Private Leader Squad", workerID)

		var created IssueResponse
		testutil.Call(t, testHandler.CreateIssue,
			topLevelIssueRequest(t, "squad", squadID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
		).Want(http.StatusCreated).JSON(&created)
		cleanupAutopilotChildIssue(t, created.ID)

		total, withOriginator := tasksFor(t, created.ID, workerID)
		if total != 1 || withOriginator != 0 {
			t.Fatalf("squad assignment must dispatch its private leader once and unattributed, got %d tasks (%d attributed)", total, withOriginator)
		}
	})

	t.Run("sub-issue under an issue the run created is admitted", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		parentID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		var created IssueResponse
		testutil.Call(t, testHandler.CreateIssue,
			autopilotChildIssueRequest(t, "agent", workerID, parentID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
		).Want(http.StatusCreated).JSON(&created)
		cleanupAutopilotChildIssue(t, created.ID)
	})

	t.Run("denials", func(t *testing.T) {
		cases := []struct {
			name string
			// setup returns the actor agent id and task id used for the request.
			setup func(t *testing.T, workerID, ownerID, plainMemberID string) (string, string)
		}{
			{
				// A finished run must not keep lending its authority.
				name: "task is no longer running",
				setup: func(t *testing.T, workerID, ownerID, _ string) (string, string) {
					fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, testutil.Cols{"status": "completed"})
					return fx.LeaderAgentID, fx.LeaderTaskID
				},
			},
			{
				// owner_fallback names the AGENT OWNER, so honouring it would let
				// any unattributed run invoke its own owner's private agents.
				name: "originator_source is owner_fallback",
				setup: func(t *testing.T, workerID, ownerID, _ string) (string, string) {
					fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, testutil.Cols{"originator_source": "owner_fallback"})
					return fx.LeaderAgentID, fx.LeaderTaskID
				},
			},
			{
				// delegation is a DESCENDANT run; authority must not propagate.
				name: "originator_source is delegation",
				setup: func(t *testing.T, workerID, ownerID, _ string) (string, string) {
					fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, testutil.Cols{"originator_source": "delegation"})
					return fx.LeaderAgentID, fx.LeaderTaskID
				},
			},
			{
				name: "attribution stamp is missing",
				setup: func(t *testing.T, workerID, ownerID, _ string) (string, string) {
					fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, testutil.Cols{
						"originator_source":   nil,
						"accountable_user_id": nil,
					})
					return fx.LeaderAgentID, fx.LeaderTaskID
				},
			},
			{
				// The accountable human simply has no rights on the target.
				name: "accountable user cannot invoke the target",
				setup: func(t *testing.T, workerID, _, plainMemberID string) (string, string) {
					fx := newRunOnlyAutopilotFixture(t, workerID, plainMemberID)
					return fx.LeaderAgentID, fx.LeaderTaskID
				},
			},
			{
				// Accountable is the agent owner but no longer (or never) a member
				// of this workspace.
				name: "accountable user is not a workspace member",
				setup: func(t *testing.T, workerID, ownerID, _ string) (string, string) {
					// Email keyed to this test's agent id so the row is unique;
					// the owner_id is restored on cleanup (registered AFTER the
					// user, so it runs BEFORE the user delete) to keep the FK
					// from stranding the row.
					outsiderID := dbfx.User(t, "MUL-6691 Outsider",
						fmt.Sprintf("mul6691-outsider-%s@multica.test", workerID))
					dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE id = $2`, outsiderID, workerID)
					t.Cleanup(func() {
						dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE id = $2`, ownerID, workerID)
					})
					fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, testutil.Cols{"accountable_user_id": outsiderID})
					return fx.LeaderAgentID, fx.LeaderTaskID
				},
			},
			{
				// Lineage is fine but the speaking actor is not the task's agent.
				name: "actor is not the task agent",
				setup: func(t *testing.T, workerID, ownerID, _ string) (string, string) {
					fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
					return workerID, fx.LeaderTaskID
				},
			},
			{
				// No autopilot run behind the task at all.
				name: "task belongs to no autopilot run",
				setup: func(t *testing.T, workerID, ownerID, _ string) (string, string) {
					fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, testutil.Cols{"autopilot_run_id": nil})
					return fx.LeaderAgentID, fx.LeaderTaskID
				},
			},
			{
				// The run's autopilot lives in another workspace.
				name: "autopilot run belongs to another workspace",
				setup: func(t *testing.T, workerID, ownerID, _ string) (string, string) {
					fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
					foreignWorkspaceID := dbfx.Workspace(t, "MUL-6691 Foreign", "mul6691-foreign-"+workerID[:8])
					dbfx.Exec(t, `UPDATE autopilot SET workspace_id = $1 WHERE id = $2`, foreignWorkspaceID, fx.AutopilotID)
					return fx.LeaderAgentID, fx.LeaderTaskID
				},
			},
			{
				// A real originator always wins, even when it has fewer rights
				// than the accountable human would have lent.
				name: "real originator without rights is not replaced",
				setup: func(t *testing.T, workerID, ownerID, plainMemberID string) (string, string) {
					fx := newRunOnlyAutopilotFixture(t, workerID, ownerID, testutil.Cols{
						"originator_user_id":  plainMemberID,
						"accountable_user_id": plainMemberID,
						"originator_source":   "direct_human",
					})
					return fx.LeaderAgentID, fx.LeaderTaskID
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				workerID, ownerID, plainMemberID := privateAgentTestFixture(t)
				actorAgentID, taskID := tc.setup(t, workerID, ownerID, plainMemberID)

				testutil.Call(t, testHandler.CreateIssue,
					topLevelIssueRequest(t, "agent", workerID, "todo", actorAgentID, taskID),
				).Want(http.StatusForbidden)

				if count := dbfx.Count(t,
					`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2`,
					testWorkspaceID, "MUL-6691 top-level "+t.Name()); count != 0 {
					t.Fatalf("refused create left %d issue rows behind", count)
				}
			})
		}
	})
}

func TestUpdateIssue_AutopilotLeaderAssignsPrivateWorker(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	// The reported symptom, end to end: create unassigned, then assign.
	t.Run("run_only run assigns an issue it created", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		issueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, issueID, workerID).Want(http.StatusOK)

		total, withOriginator := tasksFor(t, issueID, workerID)
		if total != 1 {
			t.Fatalf("assignment must dispatch the private worker exactly once, got %d tasks", total)
		}
		if withOriginator != 0 {
			t.Fatal("borrowed authority is authorization-only; the worker task must stay unattributed")
		}
	})

	t.Run("run_only run assigns a private-leader squad on an issue it created", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		squadID := dbfx.Squad(t, "MUL-6691 Update Squad", workerID)
		issueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
			asRun(newRequest(http.MethodPatch, "/api/issues/"+issueID, map[string]any{
				"assignee_type": "squad",
				"assignee_id":   squadID,
			}), fx.LeaderAgentID, fx.LeaderTaskID),
			"id", issueID,
		)).Want(http.StatusOK)
	})

	// create_issue mode: the same lineage MUL-4857 already accepts for child
	// creation now also works for the assignment verb — but ONLY through the
	// precise accountable-human rule, never through the coarse creator fallback.
	t.Run("create_issue leader with an attribution stamp assigns via its accountable human", func(t *testing.T) {
		workerID, ownerID, plainMemberID := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
		// Production stamps this task trigger_owner/accountable (MUL-4302); the
		// fixture does not, so set it explicitly to exercise that branch. Point
		// the autopilot's creator at someone with no rights so ONLY the
		// accountable human can carry the assignment.
		dbfx.Exec(t, `UPDATE autopilot SET created_by_id = $1 WHERE id = $2`, plainMemberID, fx.AutopilotID)
		stampAutopilotAttribution(t, fx.LeaderTaskID, ownerID, "trigger_owner")

		agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, uuidToString(fx.Issue.ID), workerID).Want(http.StatusOK)
	})

	// Review must-fix 2: autopilotDelegationAuthority performs no liveness or
	// attribution check of its own, so it stays confined to the child-creation
	// surface where it already shipped. On the assign verb, a task that fails the
	// strict rule must NOT be rescued by the autopilot's creator having rights.
	t.Run("creator fallback does not reach the assign verb", func(t *testing.T) {
		cases := []struct {
			name             string
			accountable      string // "" leaves accountable_user_id NULL
			originatorSource any    // nil leaves originator_source NULL
			taskStatus       string
		}{
			// The autopilot creator (ownerID) owns the target throughout, so any
			// 200 here would mean the fallback carried the request.
			{name: "no attribution stamp at all", originatorSource: nil, taskStatus: "running"},
			{name: "owner_fallback", accountable: "owner", originatorSource: "owner_fallback", taskStatus: "running"},
			{name: "delegation", accountable: "owner", originatorSource: "delegation", taskStatus: "running"},
			{name: "task no longer running", accountable: "owner", originatorSource: "trigger_owner", taskStatus: "completed"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				workerID, ownerID, _ := privateAgentTestFixture(t)
				fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
				accountable := any(nil)
				if tc.accountable == "owner" {
					accountable = ownerID
				}
				dbfx.Exec(t, `
					UPDATE agent_task_queue
					SET accountable_user_id = $1, originator_source = $2, status = $3
					WHERE id = $4
				`, accountable, tc.originatorSource, tc.taskStatus, fx.LeaderTaskID)

				agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, uuidToString(fx.Issue.ID), workerID).
					Want(http.StatusForbidden)
			})
		}
	})

	// The request path requires a live run on BOTH branches, including the
	// legacy child-creation fallback.
	t.Run("creator fallback still requires a running task on child creation", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, fx.LeaderTaskID)

		testutil.Call(t, testHandler.CreateIssue,
			autopilotChildIssueRequest(t, "agent", workerID, uuidToString(fx.Issue.ID), "backlog", fx.LeaderAgentID, fx.LeaderTaskID),
		).Want(http.StatusForbidden)
	})

	// Review must-fix 1: a create_issue-mode task must not escape its
	// `task.issue_id == issue.id` bound by creating a fresh top-level issue —
	// which stamps origin_id with its own task id — and assigning that instead.
	t.Run("create_issue leader cannot launder authority through a new top-level issue", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
		stampAutopilotAttribution(t, fx.LeaderTaskID, ownerID, "trigger_owner")

		// Creating the unassigned issue is allowed (no assignee, no gate).
		launderedID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		// Assigning it is not: the issue is agent_create-bound to this task, but
		// the task has no run_only lineage to prove.
		agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, launderedID, workerID).Want(http.StatusForbidden)

		// Nor is creating a private-assignee child under it.
		testutil.Call(t, testHandler.CreateIssue,
			autopilotChildIssueRequest(t, "agent", workerID, launderedID, "todo", fx.LeaderAgentID, fx.LeaderTaskID),
		).Want(http.StatusForbidden)

		if total, _ := tasksFor(t, launderedID, workerID); total != 0 {
			t.Fatalf("laundered assignment enqueued %d tasks", total)
		}
	})

	t.Run("run_only run cannot assign an issue it did not create", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		foreignIssueID := seedBareIssue(t, fx.LeaderAgentID)

		agentAssigns(t, fx.LeaderAgentID, fx.LeaderTaskID, foreignIssueID, workerID).Want(http.StatusForbidden)

		if total, _ := tasksFor(t, foreignIssueID, workerID); total != 0 {
			t.Fatalf("refused assignment enqueued %d tasks", total)
		}
	})

	t.Run("run_only run cannot assign an issue created by a sibling run", func(t *testing.T) {
		workerID, ownerID, _ := privateAgentTestFixture(t)
		owning := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		issueID := createUnassignedIssueAsRun(t, owning.LeaderAgentID, owning.LeaderTaskID)

		// A second, equally well-formed run of its own autopilot. Binding on the
		// creating TASK (not the autopilot) is what keeps it out.
		sibling := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		agentAssigns(t, sibling.LeaderAgentID, sibling.LeaderTaskID, issueID, workerID).Want(http.StatusForbidden)
	})

	t.Run("plain member is still refused", func(t *testing.T) {
		workerID, _, plainMemberID := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, plainMemberID)
		issueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
			newRequestAs(plainMemberID, http.MethodPatch, "/api/issues/"+issueID, map[string]any{
				"assignee_type": "agent",
				"assignee_id":   workerID,
			}),
			"id", issueID,
		)).Want(http.StatusForbidden)
	})
}

// batchAssignAsRun points every issue in issueIDs at targetAgentID through
// BatchUpdateIssues, speaking as an agent run.
//
// The header trio mirrors a real task token: the auth middleware writes
// X-User-ID (the token's bound member), X-Agent-ID and X-Task-ID together, so
// this endpoint's requireUserID is satisfied while resolveActor still classifies
// the caller as an agent. headerUserID is deliberately a member with NO rights on
// the target, so any success proves the authority came from the run's accountable
// human rather than from the header user.
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

// Review must-fix 3: BatchUpdateIssues is a real agent-reachable authorization
// point, not just defence in depth, so the per-issue scope needs its own
// coverage — both that it works and that it cannot be widened.
func TestBatchUpdateIssues_AutopilotAuthorityIsPerIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("bound issue is updated and a foreign issue in the same batch is not", func(t *testing.T) {
		workerID, ownerID, plainMemberID := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		ownIssueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)
		foreignIssueID := seedBareIssue(t, fx.LeaderAgentID)

		batchAssignAsRun(t, plainMemberID, fx.LeaderAgentID, fx.LeaderTaskID, workerID, ownIssueID, foreignIssueID).
			Want(http.StatusOK)

		if got := assigneeOf(t, ownIssueID); got != workerID {
			t.Fatalf("bound issue assignee = %q, want %q", got, workerID)
		}
		if got := assigneeOf(t, foreignIssueID); got != "" {
			t.Fatalf("foreign issue in the same batch was assigned to %q; per-issue scoping leaked", got)
		}
		if total, _ := tasksFor(t, foreignIssueID, workerID); total != 0 {
			t.Fatalf("foreign issue enqueued %d tasks", total)
		}
	})

	t.Run("refuses the create_issue laundering path", func(t *testing.T) {
		workerID, ownerID, plainMemberID := privateAgentTestFixture(t)
		fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
		stampAutopilotAttribution(t, fx.LeaderTaskID, ownerID, "trigger_owner")
		launderedID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)

		batchAssignAsRun(t, plainMemberID, fx.LeaderAgentID, fx.LeaderTaskID, workerID, launderedID).
			Want(http.StatusOK)

		if got := assigneeOf(t, launderedID); got != "" {
			t.Fatalf("laundered issue was assigned to %q; the run_only lineage check did not apply", got)
		}
	})

	t.Run("refuses a task that is no longer running", func(t *testing.T) {
		workerID, ownerID, plainMemberID := privateAgentTestFixture(t)
		fx := newRunOnlyAutopilotFixture(t, workerID, ownerID)
		ownIssueID := createUnassignedIssueAsRun(t, fx.LeaderAgentID, fx.LeaderTaskID)
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, fx.LeaderTaskID)

		batchAssignAsRun(t, plainMemberID, fx.LeaderAgentID, fx.LeaderTaskID, workerID, ownIssueID).
			Want(http.StatusOK)

		if got := assigneeOf(t, ownIssueID); got != "" {
			t.Fatalf("completed task assigned the issue to %q", got)
		}
	})
}
