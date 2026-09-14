package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// An issue in Triage starts no run, from any entry point (MUL-7189 §2.3).
//
// Every case here runs TWICE against the same agent, squad and quick action:
// once on an ordinary `todo` issue, which must produce a run, and once on an
// issue in Triage, which must not. The todo half is not redundant — a private
// agent, an unbound runtime or a missing allow-list row also produce zero rows,
// and without a positive control this whole file would pass on a fixture that
// could never have enqueued anything.

type triageRunFixture struct {
	agentID   string
	squadID   string
	leaderID  string
	actionID  string
	runtimeID string
}

// newTriageRunFixture builds targets a plain workspace member is allowed to
// invoke, so a blocked run is blocked by Triage and not by the permission gate.
func newTriageRunFixture(t *testing.T) triageRunFixture {
	t.Helper()
	runtimeID := dbfx.Runtime(t, "triage no-run runtime")
	invocable := func(name string) string {
		id := dbfx.Agent(t, name, runtimeID, testutil.Cols{
			"visibility":      "workspace",
			"permission_mode": "public_to",
		})
		dbfx.InsertNoID(t, "agent_invocation_target", testutil.Cols{
			"agent_id": id, "target_type": "workspace", "target_id": testWorkspaceID,
		}, "agent_id = $1 AND target_type = 'workspace' AND target_id = $2", id, testWorkspaceID)
		return id
	}
	agentID := invocable("triage no-run agent")
	leaderID := invocable("triage no-run leader")
	squadID := dbfx.Squad(t, "triage no-run squad", leaderID)

	var actionID string
	dbfx.QueryRow(t, `
		INSERT INTO quick_action (
			workspace_id, name, description, assignee_type, assignee_id, prompt,
			visibility, created_by_type, created_by_id
		) VALUES ($1, 'Triage No Run', '', 'agent', $2, 'take a look', 'public', 'member', $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&actionID)
	dbfx.Cleanup(t, `DELETE FROM quick_action WHERE id = $1`, actionID)

	return triageRunFixture{agentID: agentID, squadID: squadID, leaderID: leaderID, actionID: actionID, runtimeID: runtimeID}
}

// issueFor creates an issue in the given status assigned to the fixture agent.
func (f triageRunFixture) issueFor(t *testing.T, status, title string) string {
	t.Helper()
	issueID := dbfx.Issue(t, title, testutil.Cols{
		"status":        status,
		"assignee_type": "agent",
		"assignee_id":   f.agentID,
		"number":        nextWorkspaceIssueNumber(t),
	})
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	return issueID
}

func tasksOn(t *testing.T, issueID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID)
}

func commentsOn(t *testing.T, issueID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID)
}

// triageEntryPoints are the request-driven ways an issue reaches a run. Each
// returns the response so the caller can assert on its shape; the run itself is
// counted from agent_task_queue, which is what the rule is actually about.
type triageEntryPoint struct {
	name string
	call func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response
}

func triageEntryPoints() []triageEntryPoint {
	return []triageEntryPoint{
		{"comment mentioning an agent", func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return testutil.Call(t, testHandler.CreateComment, withURLParam(
				newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
					"content": "[@Agent](mention://agent/" + f.agentID + ") please take a look",
				}), "id", issueID))
		}},
		{"comment mentioning a squad", func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return testutil.Call(t, testHandler.CreateComment, withURLParam(
				newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
					"content": "[@Squad](mention://squad/" + f.squadID + ") please take a look",
				}), "id", issueID))
		}},
		// No mention at all: the issue assignee is the implicit route, and this
		// one fires in every status, which is why Triage has to say otherwise.
		{"plain comment routed to the assignee", func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return testutil.Call(t, testHandler.CreateComment, withURLParam(
				newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
					"content": "any progress?",
				}), "id", issueID))
		}},
		{"manual rerun", func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return testutil.Call(t, testHandler.RerunIssue, withURLParam(
				newRequest(http.MethodPost, "/api/issues/"+issueID+"/rerun", nil), "id", issueID))
		}},
		{"quick action", func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return testutil.Call(t, testHandler.RunQuickAction, testutil.WithURLParams(
				newRequest(http.MethodPost, "/api/issues/"+issueID+"/quick-actions/"+f.actionID+"/run", nil),
				"id", issueID, "quickActionId", f.actionID))
		}},
		// Reassigning is allowed in Triage (accept has not decided the owner
		// yet), so it is the one issue write that still reaches the trigger.
		{"reassignment", func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return testutil.Call(t, testHandler.UpdateIssue, withURLParam(
				newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
					"assignee_type": "squad", "assignee_id": f.squadID,
				}), "id", issueID))
		}},
	}
}

// The control: on an ordinary issue this fixture really does start runs.
func TestTriageNoRunEntryPointsRunOnAnOrdinaryIssue(t *testing.T) {
	f := newTriageRunFixture(t)
	for _, entry := range triageEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			issueID := f.issueFor(t, "todo", "ordinary issue: "+entry.name)
			entry.call(t, f, issueID).WantOneOf(http.StatusOK, http.StatusCreated, http.StatusAccepted)
			if n := tasksOn(t, issueID); n == 0 {
				t.Fatalf("%s started no run on a todo issue, so the triage half of this test proves nothing", entry.name)
			}
		})
	}
}

func TestTriageIssueStartsNoRunFromAnyEntryPoint(t *testing.T) {
	f := newTriageRunFixture(t)
	for _, entry := range triageEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			issueID := f.issueFor(t, issuestatus.Triage, "triage issue: "+entry.name)
			entry.call(t, f, issueID)
			if n := tasksOn(t, issueID); n != 0 {
				t.Fatalf("%s started %d run(s) on an issue in Triage", entry.name, n)
			}
		})
	}
}

// The two entry points the caller is waiting on a response from are refused
// outright, rather than accepted and silently dropped at the queue door.
func TestTriageRefusesRerunAndQuickActionWithAReason(t *testing.T) {
	f := newTriageRunFixture(t)

	t.Run("rerun", func(t *testing.T) {
		issueID := f.issueFor(t, issuestatus.Triage, "triage rerun")
		resp := testutil.Call(t, testHandler.RerunIssue, withURLParam(
			newRequest(http.MethodPost, "/api/issues/"+issueID+"/rerun", nil), "id", issueID))
		if got := resp.Want(http.StatusForbidden).Map()["reason_code"]; got != string(dispatch.ReasonIssueInTriage) {
			t.Fatalf("reason_code = %v, want issue_in_triage: %s", got, resp.Text())
		}
	})

	// A quick action is a comment AND a run. Refusing it after the comment was
	// written would leave a prompt addressed to nobody, so nothing is written.
	t.Run("quick action writes no comment", func(t *testing.T) {
		issueID := f.issueFor(t, issuestatus.Triage, "triage quick action")
		resp := testutil.Call(t, testHandler.RunQuickAction, testutil.WithURLParams(
			newRequest(http.MethodPost, "/api/issues/"+issueID+"/quick-actions/"+f.actionID+"/run", nil),
			"id", issueID, "quickActionId", f.actionID))
		if got := resp.Want(http.StatusForbidden).Map()["reason_code"]; got != string(dispatch.ReasonIssueInTriage) {
			t.Fatalf("reason_code = %v, want issue_in_triage: %s", got, resp.Text())
		}
		if n := commentsOn(t, issueID); n != 0 {
			t.Fatalf("refused quick action left %d comment(s) on the issue", n)
		}
	})
}

// A named target gets an outcome rather than a mention that visibly does
// nothing. The reason is a fact about the issue, so it is the same for every
// target and reveals nothing the author could not already see.
func TestTriageCommentReportsIssueInTriagePerMention(t *testing.T) {
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, issuestatus.Triage, "triage mention outcomes")

	var body struct {
		TriggerOutcomes []struct {
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
			Status     string `json:"status"`
			ReasonCode string `json:"reason_code"`
		} `json:"trigger_outcomes"`
	}
	testutil.Call(t, testHandler.CreateComment, withURLParam(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
			"content": "[@Agent](mention://agent/" + f.agentID + ") and " +
				"[@Squad](mention://squad/" + f.squadID + ") please look",
		}), "id", issueID)).Want(http.StatusCreated).JSON(&body)

	if len(body.TriggerOutcomes) != 2 {
		t.Fatalf("got %d trigger outcome(s), want one per named target: %+v", len(body.TriggerOutcomes), body.TriggerOutcomes)
	}
	want := map[string]string{"agent": f.agentID, "squad": f.squadID}
	for _, o := range body.TriggerOutcomes {
		if o.Status != string(DispatchBlocked) || o.ReasonCode != string(dispatch.ReasonIssueInTriage) {
			t.Errorf("%s outcome = (%s, %s), want (blocked, issue_in_triage)", o.TargetType, o.Status, o.ReasonCode)
		}
		if want[o.TargetType] != o.TargetID {
			t.Errorf("%s outcome names %q, want %q", o.TargetType, o.TargetID, want[o.TargetType])
		}
		delete(want, o.TargetType)
	}
	if len(want) != 0 {
		t.Errorf("no outcome for %v", want)
	}
}

// The preview has to agree with the door. A preview promising a run that the
// enqueue then discards is worse than no preview: dispatchIssueRun drops the
// error, so nothing would ever report the difference.
func TestPreviewIssueTriggerReportsNoRunForTriage(t *testing.T) {
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, issuestatus.Triage, "triage preview")

	if preview := previewIssueTrigger(t, map[string]any{
		"is_create":     true,
		"assignee_type": "agent",
		"assignee_id":   f.agentID,
		"status":        issuestatus.Triage,
	}); preview.TotalCount != 0 {
		t.Errorf("triage create previews %+v, want no run", preview)
	}

	// Reassigning inside Triage is allowed and would otherwise preview a run,
	// since assignment is the one trigger source that does not look at status.
	if preview := previewIssueTrigger(t, map[string]any{
		"issue_ids":     []string{issueID},
		"assignee_type": "agent",
		"assignee_id":   f.leaderID,
	}); preview.TotalCount != 0 {
		t.Errorf("triage reassign previews %+v, want no run", preview)
	}

	// Accept is the way out, and it previews as the issue's first appearance as
	// work rather than as a status change: the backlog-to-elsewhere rule would
	// refuse it, since the issue was never in backlog.
	accepted := previewIssueTrigger(t, map[string]any{
		"issue_ids": []string{issueID},
		"status":    "todo",
	})
	if accepted.TotalCount != 1 || len(accepted.Triggers) != 1 {
		t.Fatalf("accept previews %+v, want exactly one run", accepted)
	}
	if accepted.Triggers[0].Source != "assign" || accepted.Triggers[0].AgentID != f.agentID {
		t.Errorf("accept preview = %+v, want the assignee started by an assign", accepted.Triggers[0])
	}
}

// The queue door itself, on the enqueue entry points no HTTP request reaches.
// This is the check the whole rule rests on: dispatchIssueRun discards enqueue
// errors, so a path that slipped past every short-circuit above would fail
// silently rather than loudly.
func TestEnqueueRefusesAnIssueInTriage(t *testing.T) {
	ctx := context.Background()
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, issuestatus.Triage, "triage queue door")
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}

	calls := map[string]func() error{
		"assignee": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
			return err
		},
		"mention": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, parseUUID(f.agentID), pgtype.UUID{})
			return err
		},
		"thread parent": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForThreadParent(ctx, issue, parseUUID(f.agentID), pgtype.UUID{})
			return err
		},
		"squad leader": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForSquadLeader(ctx, issue, parseUUID(f.leaderID), parseUUID(f.squadID), pgtype.UUID{})
			return err
		},
		"rerun": func() error {
			_, err := testHandler.TaskService.RerunIssue(ctx, issue.ID, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, nil)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, service.ErrIssueInTriage) {
				t.Fatalf("enqueue = %v, want ErrIssueInTriage", err)
			}
		})
	}
	if n := tasksOn(t, issueID); n != 0 {
		t.Fatalf("refused enqueues still wrote %d task row(s)", n)
	}
}

// Auto-retry is the one enqueue with no caller to refuse: the sweeper decides
// on its own. A failed run on an issue in Triage stays failed.
func TestAutoRetrySkipsAnIssueInTriage(t *testing.T) {
	ctx := context.Background()
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, issuestatus.Triage, "triage auto retry")
	taskID := dbfx.Task(t, f.agentID, testutil.Cols{
		"runtime_id":     f.runtimeID,
		"issue_id":       issueID,
		"status":         "failed",
		"failure_reason": "timeout",
		"attempt":        1,
		"max_attempts":   2,
		"started_at":     testutil.Raw("now() - interval '2 minutes'"),
		"completed_at":   testutil.Raw("now() - interval '1 minute'"),
	})
	parent, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	// Nothing about this task is itself unretryable — on a todo issue the same
	// row retries — so the skip below can only come from the issue's status.
	if !retryableOnItsOwn(parent) {
		t.Fatal("fixture task is not retryable on its own terms")
	}
	child, err := testHandler.TaskService.MaybeRetryFailedTask(ctx, parent)
	if err != nil {
		t.Fatalf("MaybeRetryFailedTask = %v, want no error", err)
	}
	if child != nil {
		t.Fatalf("auto-retry created task %s for an issue in Triage", uuidToString(child.ID))
	}
	if n := tasksOn(t, issueID); n != 1 {
		t.Fatalf("issue has %d task(s), want only the original failed one", n)
	}
}

// retryableOnItsOwn mirrors the budget/reason half of the retry decision so the
// test above can prove the skip came from Triage and not from an exhausted
// fixture. It deliberately does not call retryEligible, which is in another
// package and is itself part of what this change altered.
func retryableOnItsOwn(t db.AgentTaskQueue) bool {
	return t.Status == "failed" && t.FailureReason.String == "timeout" &&
		t.Attempt < t.MaxAttempts && t.IssueID.Valid && !t.AutopilotRunID.Valid
}

// A triage run must not outlive Triage as something to repeat (MUL-7189 §5.6).
//
// This is the one path that survives accept: once the issue is runnable again
// nothing above refuses it, and rerun resolves its session from the task the
// user named rather than from GetLastTaskSession — so the query-level exclusion
// cannot see it. It would also target the triager instead of the assignee.
func TestRerunRefusesAHistoricalTriageTaskAfterAccept(t *testing.T) {
	f := newTriageRunFixture(t)
	// Already accepted: the issue itself runs fine, which is what makes the
	// source task the only thing left to refuse.
	issueID := f.issueFor(t, "todo", "accepted out of triage")

	sourceTask := func(cols testutil.Cols) string {
		base := testutil.Cols{
			"runtime_id":   f.runtimeID,
			"issue_id":     issueID,
			"status":       "completed",
			"session_id":   "TRIAGE-SESSION",
			"work_dir":     "/tmp/triage",
			"started_at":   testutil.Raw("now() - interval '2 minutes'"),
			"completed_at": testutil.Raw("now() - interval '1 minute'"),
		}
		for k, v := range cols {
			base[k] = v
		}
		return dbfx.Task(t, f.agentID, base)
	}
	rerun := func(taskID string) *testutil.Response {
		return testutil.Call(t, testHandler.RerunIssue, withURLParam(
			newRequest(http.MethodPost, "/api/issues/"+issueID+"/rerun", map[string]any{"task_id": taskID}), "id", issueID))
	}

	// The control: an ordinary historical task on this issue reruns.
	before := tasksOn(t, issueID)
	rerun(sourceTask(nil)).Want(http.StatusAccepted)
	if got := tasksOn(t, issueID); got != before+2 {
		t.Fatalf("ordinary rerun left %d task(s), want the source plus a rerun", got-before)
	}

	triageID := sourceTask(testutil.Cols{"context": testutil.Raw(`'{"type":"triage"}'::jsonb`)})
	before = tasksOn(t, issueID)
	if body := rerun(triageID).Want(http.StatusBadRequest).Map()["error"]; body == nil {
		t.Fatal("refused triage rerun carried no error message")
	}
	if got := tasksOn(t, issueID); got != before {
		t.Fatalf("refused triage rerun created %d task(s)", got-before)
	}
}

// The claim-side half of the same rule. The service refuses first, so this
// guards a rerun row written by an older server during a rolling deploy — the
// one case where the refusal above was not in effect when the row was created.
func TestRerunSourceScopeRejectsATriageSource(t *testing.T) {
	agent := parseUUID(testUserID)
	issue := pgtype.UUID{Bytes: [16]byte{7}, Valid: true}
	task := db.AgentTaskQueue{AgentID: agent, IssueID: issue}
	ordinary := db.AgentTaskQueue{AgentID: agent, IssueID: issue}
	triage := db.AgentTaskQueue{AgentID: agent, IssueID: issue, Context: []byte(`{"type":"triage"}`)}

	if !rerunSourceMatchesTaskScope(task, ordinary) {
		t.Fatal("an ordinary same-issue source is in scope, so the triage case below proves nothing")
	}
	if rerunSourceMatchesTaskScope(task, triage) {
		t.Error("a triage source is in scope, so its session and workdir would be reused")
	}
}

// FailTask's in-transaction retry is the one enqueue that must refuse WITHOUT
// failing: the transaction also carries the parent's failed status, so aborting
// would leave the task stuck in 'running'.
func TestFailTaskRetryStartsNoRunForATriageIssue(t *testing.T) {
	ctx := context.Background()
	f := newTriageRunFixture(t)

	for _, tc := range []struct {
		status     string
		wantChild  bool
		wantOnFail string
	}{
		{"todo", true, "an ordinary issue retries, so the triage case below proves nothing"},
		{issuestatus.Triage, false, "an issue in Triage was given a retry"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			issueID := f.issueFor(t, tc.status, "fail task retry: "+tc.status)
			taskID := dbfx.Task(t, f.agentID, testutil.Cols{
				"runtime_id":   f.runtimeID,
				"issue_id":     issueID,
				"status":       "running",
				"attempt":      1,
				"max_attempts": 2,
				"started_at":   testutil.Raw("now() - interval '1 minute'"),
			})
			if _, err := testHandler.TaskService.FailTask(ctx, parseUUID(taskID), "runtime went away", "", "", "", "timeout", false, "", ""); err != nil {
				t.Fatalf("FailTask: %v", err)
			}
			// The parent must land failed either way. A guard that aborted the
			// transaction would leave it 'running' and the task stuck forever.
			var status string
			dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
			if status != "failed" {
				t.Fatalf("parent task status = %q, want failed", status)
			}
			children := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND id <> $2`, issueID, taskID)
			if (children > 0) != tc.wantChild {
				t.Fatalf("%s (children = %d)", tc.wantOnFail, children)
			}
		})
	}
}
