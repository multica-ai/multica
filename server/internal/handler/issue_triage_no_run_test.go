package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Triage has no executor (MUL-7189 §2.3).
//
// Nothing may derive one from the issue — the assignee, its squad's leader, a
// retry. A member naming an agent by hand is a conversation, not an executor,
// and keeps working.
//
// Every entry point below runs TWICE against the same agent, squad and quick
// action: once on an ordinary `todo` issue, where all of them must start a run,
// and once in Triage, where only the named ones may. The todo half is not
// redundant — a private agent, an unbound runtime or a missing allow-list row
// also produce zero rows, and without a positive control the Triage half would
// pass on a fixture that could never have enqueued anything.

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

// issueFor creates an issue assigned to the fixture agent. triageState is the
// value of the column that puts it in Triage; "" is an ordinary issue.
//
// Both halves carry status `todo`, which is the point: Triage is no longer a
// status, so the two issues differ in exactly one field and nothing else can
// explain a difference in behavior between them.
func (f triageRunFixture) issueFor(t *testing.T, triageState, title string) string {
	t.Helper()
	cols := testutil.Cols{
		"status":        "todo",
		"assignee_type": "agent",
		"assignee_id":   f.agentID,
		"number":        nextWorkspaceIssueNumber(t),
	}
	if triageState != "" {
		cols["triage_state"] = triageState
	}
	issueID := dbfx.Issue(t, title, cols)
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	return issueID
}

// triagePending is the only Triage state today.
const triagePending = "pending"

func tasksOn(t *testing.T, issueID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID)
}

func commentsOn(t *testing.T, issueID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID)
}

// triageEntryPoints are the request-driven ways an issue reaches a run. The run
// itself is counted from agent_task_queue, which is what the rule is about;
// runsInTriage records which side of the line the entry point falls on.
type triageEntryPoint struct {
	name string
	// runsInTriage is true when a member named the agent, false when the
	// executor was derived from the issue.
	runsInTriage bool
	call         func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response
}

func triageEntryPoints() []triageEntryPoint {
	comment := func(t *testing.T, issueID, content string) *testutil.Response {
		t.Helper()
		return testutil.Call(t, testHandler.CreateComment, withURLParam(
			newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID))
	}
	return []triageEntryPoint{
		{"comment mentioning an agent", true, func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return comment(t, issueID, "[@Agent](mention://agent/"+f.agentID+") please take a look")
		}},
		{"comment mentioning a squad", true, func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return comment(t, issueID, "[@Squad](mention://squad/"+f.squadID+") please take a look")
		}},
		// A member replying to what the agent said is still that member naming
		// it — the thread is the address.
		{"reply to an agent's comment", true, func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			parentID := dbfx.Comment(t, issueID, "here is what I found", testutil.Cols{
				"author_type": "agent", "author_id": f.agentID,
			})
			return testutil.Call(t, testHandler.CreateComment, withURLParam(
				newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
					"content": "thanks, what about the other case?", "parent_id": parentID,
				}), "id", issueID))
		}},
		// No mention at all: the route is the issue's assignee, which in Triage
		// is only a proposal.
		{"plain comment routed to the assignee", false, func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return comment(t, issueID, "any progress?")
		}},
		{"manual rerun", false, func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return testutil.Call(t, testHandler.RerunIssue, withURLParam(
				newRequest(http.MethodPost, "/api/issues/"+issueID+"/rerun", nil), "id", issueID))
		}},
		// A quick action carries its own target, so the rule would let it
		// through; the first phase refuses it because it is an instruction to
		// DO the action rather than an invitation to talk.
		{"quick action", false, func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
			return testutil.Call(t, testHandler.RunQuickAction, testutil.WithURLParams(
				newRequest(http.MethodPost, "/api/issues/"+issueID+"/quick-actions/"+f.actionID+"/run", nil),
				"id", issueID, "quickActionId", f.actionID))
		}},
		// Reassigning is allowed in Triage (accept has not decided the owner
		// yet), so it is the one issue write that still reaches the trigger.
		{"reassignment", false, func(t *testing.T, f triageRunFixture, issueID string) *testutil.Response {
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
			issueID := f.issueFor(t, "", "ordinary issue: "+entry.name)
			entry.call(t, f, issueID).WantOneOf(http.StatusOK, http.StatusCreated, http.StatusAccepted)
			if n := tasksOn(t, issueID); n == 0 {
				t.Fatalf("%s started no run on a todo issue, so the triage half of this test proves nothing", entry.name)
			}
		})
	}
}

func TestTriageRunsOnlyWhatAMemberNamed(t *testing.T) {
	f := newTriageRunFixture(t)
	for _, entry := range triageEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			issueID := f.issueFor(t, triagePending, "triage issue: "+entry.name)
			entry.call(t, f, issueID)
			n := tasksOn(t, issueID)
			if entry.runsInTriage && n == 0 {
				t.Fatalf("%s is a member naming an agent, but started no run in Triage", entry.name)
			}
			if !entry.runsInTriage && n != 0 {
				t.Fatalf("%s derives its executor from the issue, but started %d run(s) in Triage", entry.name, n)
			}
		})
	}
}

// The two entry points the caller is waiting on a response from are refused
// outright, rather than accepted and silently dropped at the queue door.
func TestTriageRefusesRerunAndQuickActionWithAReason(t *testing.T) {
	f := newTriageRunFixture(t)

	t.Run("rerun", func(t *testing.T) {
		issueID := f.issueFor(t, triagePending, "triage rerun")
		resp := testutil.Call(t, testHandler.RerunIssue, withURLParam(
			newRequest(http.MethodPost, "/api/issues/"+issueID+"/rerun", nil), "id", issueID))
		if got := resp.Want(http.StatusForbidden).Map()["reason_code"]; got != string(dispatch.ReasonIssueInTriage) {
			t.Fatalf("reason_code = %v, want issue_in_triage: %s", got, resp.Text())
		}
	})

	// A quick action is a comment AND a run. Refusing it after the comment was
	// written would leave a prompt addressed to nobody, so nothing is written.
	t.Run("quick action writes no comment", func(t *testing.T) {
		issueID := f.issueFor(t, triagePending, "triage quick action")
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

// The mention that used to be refused. A member @-ing an agent on a Triage
// entry is a conversation, so it dispatches like anywhere else and reports a
// normal queued outcome — no issue_in_triage, no silent nothing.
func TestTriageCommentMentionDispatchesNormally(t *testing.T) {
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, triagePending, "triage mention outcomes")

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
		if o.Status != string(DispatchQueued) {
			t.Errorf("%s outcome = (%s, %s), want queued", o.TargetType, o.Status, o.ReasonCode)
		}
		if want[o.TargetType] != o.TargetID {
			t.Errorf("%s outcome names %q, want %q", o.TargetType, o.TargetID, want[o.TargetType])
		}
		delete(want, o.TargetType)
	}
	if len(want) != 0 {
		t.Errorf("no outcome for %v", want)
	}
	// Both targets are the leader-vs-agent pair, so this is two distinct runs.
	if n := tasksOn(t, issueID); n != 2 {
		t.Errorf("mentions started %d run(s), want 2", n)
	}
}

// The preview has to agree with the door. A preview promising a run that the
// enqueue then discards is worse than no preview: dispatchIssueRun drops the
// error, so nothing would ever report the difference.
func TestPreviewIssueTriggerReportsNoRunForTriage(t *testing.T) {
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, triagePending, "triage preview")

	// Reassigning inside Triage is allowed and would otherwise preview a run:
	// assignment is the one trigger source that never looked at status.
	reassign := map[string]any{
		"issue_ids":     []string{issueID},
		"assignee_type": "agent",
		"assignee_id":   f.leaderID,
	}
	if preview := previewIssueTrigger(t, reassign); preview.TotalCount != 0 {
		t.Errorf("triage reassign previews %+v, want no run", preview)
	}

	// Accept clears the column and the issue becomes ordinary work. The same
	// request now previews a run, which is what makes the zero above the
	// column's doing rather than the fixture's.
	dbfx.Exec(t, `UPDATE issue SET triage_state = NULL WHERE id = $1`, issueID)
	accepted := previewIssueTrigger(t, reassign)
	if accepted.TotalCount != 1 || len(accepted.Triggers) != 1 {
		t.Fatalf("accepted issue previews %+v, want exactly one run", accepted)
	}
	if accepted.Triggers[0].Source != "assign" || accepted.Triggers[0].AgentID != f.leaderID {
		t.Errorf("accepted preview = %+v, want the new assignee started by an assign", accepted.Triggers[0])
	}
}

// The queue door itself, on the enqueue entry points no HTTP request reaches.
// This is the check the whole rule rests on: dispatchIssueRun discards enqueue
// errors, so a path that slipped past every short-circuit above would fail
// silently rather than loudly.
//
// The same funnel is exercised from both sides, because the door does not judge
// the funnel — it judges what the caller says the run is. Passing nothing is
// OriginDerived, so a future enqueue path cannot leak into Triage by omission.
func TestEnqueueRefusesADerivedRunInTriage(t *testing.T) {
	ctx := context.Background()
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, triagePending, "triage queue door")
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	agent, squad := parseUUID(f.agentID), parseUUID(f.squadID)
	leader := parseUUID(f.leaderID)

	derived := map[string]func() error{
		"assignee": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
			return err
		},
		"mention funnel, derived": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, agent, pgtype.UUID{}, service.OriginDerived)
			return err
		},
		"squad leader, derived": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForSquadLeader(ctx, issue, leader, squad, pgtype.UUID{}, service.OriginDerived)
			return err
		},
		"squad leader on assign": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForSquadLeaderByActor(ctx, issue, leader, squad, pgtype.UUID{})
			return err
		},
		"rerun with no source": func() error {
			_, err := testHandler.TaskService.RerunIssue(ctx, issue.ID, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, nil)
			return err
		},
	}
	for name, call := range derived {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, service.ErrIssueInTriage) {
				t.Fatalf("enqueue = %v, want ErrIssueInTriage", err)
			}
		})
	}
	if n := tasksOn(t, issueID); n != 0 {
		t.Fatalf("refused enqueues still wrote %d task row(s)", n)
	}

	// The other side of the same door, so the refusals above are the origin's
	// doing and not the funnel's.
	named := map[string]func() error{
		"mention funnel, named": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, agent, pgtype.UUID{}, service.OriginNamed)
			return err
		},
		"thread parent": func() error {
			_, err := testHandler.TaskService.EnqueueTaskForThreadParent(ctx, issue, leader, pgtype.UUID{})
			return err
		},
	}
	for name, call := range named {
		t.Run(name, func(t *testing.T) {
			if err := call(); err != nil {
				t.Fatalf("named enqueue = %v, want it admitted", err)
			}
		})
	}
	if n := tasksOn(t, issueID); n != len(named) {
		t.Fatalf("named enqueues wrote %d task row(s), want %d", n, len(named))
	}
}

// Auto-retry is the one enqueue with no caller to refuse: the sweeper decides
// on its own. A failed run on an issue in Triage stays failed.
func TestAutoRetrySkipsAnIssueInTriage(t *testing.T) {
	ctx := context.Background()
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, triagePending, "triage auto retry")
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
	issueID := f.issueFor(t, "", "accepted out of triage")

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

// Rerun follows its source. Naming a discussion run repeats a conversation the
// member started, so it is allowed even while the issue sits in Triage; naming
// nothing means "run the assignee again", which is the derived executor Triage
// does not have (covered by the queue-door test above).
func TestRerunInTriageFollowsItsSource(t *testing.T) {
	f := newTriageRunFixture(t)
	issueID := f.issueFor(t, triagePending, "triage rerun by source")

	// A discussion run: what a member's @mention leaves behind on a Triage entry.
	discussionID := dbfx.Task(t, f.agentID, testutil.Cols{
		"runtime_id":   f.runtimeID,
		"issue_id":     issueID,
		"status":       "completed",
		"started_at":   testutil.Raw("now() - interval '2 minutes'"),
		"completed_at": testutil.Raw("now() - interval '1 minute'"),
	})
	before := tasksOn(t, issueID)
	testutil.Call(t, testHandler.RerunIssue, withURLParam(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/rerun", map[string]any{"task_id": discussionID}),
		"id", issueID)).Want(http.StatusAccepted)
	if got := tasksOn(t, issueID); got != before+1 {
		t.Fatalf("rerunning a discussion run added %d task(s), want 1", got-before)
	}

	// The triage run itself is still refused, with its own reason.
	triageID := dbfx.Task(t, f.agentID, testutil.Cols{
		"runtime_id":   f.runtimeID,
		"issue_id":     issueID,
		"status":       "completed",
		"context":      testutil.Raw(`'{"type":"triage"}'::jsonb`),
		"started_at":   testutil.Raw("now() - interval '2 minutes'"),
		"completed_at": testutil.Raw("now() - interval '1 minute'"),
	})
	before = tasksOn(t, issueID)
	testutil.Call(t, testHandler.RerunIssue, withURLParam(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/rerun", map[string]any{"task_id": triageID}),
		"id", issueID)).Want(http.StatusBadRequest)
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
		name        string
		triageState string
		wantChild   bool
		wantOnFail  string
	}{
		{"ordinary", "", true, "an ordinary issue retries, so the triage case below proves nothing"},
		{"triage", triagePending, false, "an issue in Triage was given a retry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issueID := f.issueFor(t, tc.triageState, "fail task retry: "+tc.name)
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
