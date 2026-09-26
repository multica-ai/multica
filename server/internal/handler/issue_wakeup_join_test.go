package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// actAsRun makes a request come from an agent's run, as the CLI does inside a
// task.
func actAsRun(req *http.Request, agentID, taskID string) *http.Request {
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	req.Header.Set("X-Actor-Source", "task_token")
	return req
}

func setStatusAsRun(t *testing.T, issueID, status, agentID, taskID string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": status}), "id", issueID)
	if agentID != "" {
		req = actAsRun(req, agentID, taskID)
	}
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set status %q: %d %s", status, w.Code, w.Body.String())
	}
}

func commentOn(t *testing.T, issueID, content, agentID, taskID string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	if agentID != "" {
		req = actAsRun(req, agentID, taskID)
	}
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("comment: %d %s", w.Code, w.Body.String())
	}
}

func wakeupRunsOf(t *testing.T, wakeupID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id' = $1`, wakeupID)
}

func lastWakeupOutcome(t *testing.T, issueID string) (outcome, taskID string) {
	t.Helper()
	var raw []byte
	dbfx.QueryRow(t, `SELECT details FROM activity_log WHERE issue_id = $1 AND action = 'wakeup_triggered'
		ORDER BY created_at DESC, id DESC LIMIT 1`, issueID).Scan(&raw)
	var details struct {
		Outcome string `json:"outcome"`
		TaskID  string `json:"task_id"`
	}
	_ = json.Unmarshal(raw, &details)
	return details.Outcome, details.TaskID
}

// claimRun claims a queued run as a daemon does and returns what its prompt
// gets from wakeups that joined it. Only a daemon that advertises
// joined-wakeups-v1 takes them along.
func claimRun(t *testing.T, taskID string, rendersJoined bool) string {
	t.Helper()
	ctx := context.Background()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = clock_timestamp() WHERE id = $1`, taskID)
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntime(ctx, task.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	req := newDaemonTokenRequest("POST", "/claim", nil, testWorkspaceID, "joined-wakeups-test")
	if rendersJoined {
		req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityJoinedWakeupsV1)
	}
	resp, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, &task, runtime, uuidToString(task.RuntimeID), testWorkspaceID)
	if failure != nil {
		t.Fatalf("claim: %+v", failure)
	}
	return resp.WakeupJoined
}

func createRule(t *testing.T, issueID, agentID string, source pgtype.UUID, in service.WakeupInput) string {
	t.Helper()
	in.AgentID = agentID
	w, err := (&service.IssueWakeupService{Tasks: testHandler.TaskService}).Create(context.Background(), parseUUID(issueID), parseUUID(testUserID), source, in)
	if err != nil {
		t.Fatal(err)
	}
	return uuidToString(w.ID)
}

// #8849: the parent's agent closes the last sub-issue of a stage from its own
// run on the parent. That run knows, so the rule records the stage without
// queuing a second run; once that run has ended, or when someone else closes
// the stage, the agent is woken as before.
func TestChildDoneDoesNotWakeTheRunThatClosedTheStage(t *testing.T) {
	cases := []struct {
		name      string
		runStatus string
		byAgent   bool
		wantRuns  int
		wantEntry string
	}{
		{"own run still going", "running", true, 0, "acknowledged"},
		{"own run already ended", "completed", true, 1, "woke"},
		{"someone else closed it", "running", false, 1, "woke"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newChildDoneFixture(t, "in_progress")
			agentID := handlerTestAgentID(t)
			setIssueAssigneeDirect(t, fx.parent.ID, "agent", agentID)
			run := dbfx.Task(t, agentID, testutil.Cols{"issue_id": fx.parent.ID, "runtime_id": testRuntimeID, "status": tc.runStatus})
			if tc.byAgent {
				setStatusAsRun(t, fx.child.ID, "done", agentID, run)
			} else {
				updateChildStatus(t, fx.child.ID, "done")
			}
			if runs := childDoneRuns(t, fx.parent.ID); len(runs) != tc.wantRuns {
				t.Fatalf("runs = %+v, want %d", runs, tc.wantRuns)
			}
			if entries := childDoneEntries(t, fx.parent.ID); len(entries) != 1 || entries[0].Outcome != tc.wantEntry {
				t.Fatalf("entries = %+v, want %s", entries, tc.wantEntry)
			}
		})
	}
}

// A comment from the agent itself never wakes it; a member's comment does.
func TestWakeupSkipsTheAgentsOwnComment(t *testing.T) {
	issue := dbfx.Issue(t, "own comment")
	t.Cleanup(func() { cleanupChildDoneIssue(issue) })
	agentID := handlerTestAgentID(t)
	rule := createRule(t, issue, agentID, pgtype.UUID{}, service.WakeupInput{Instruction: "Reply to new comments", Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}})
	run := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "running"})

	commentOn(t, issue, "Deployed the fix.", agentID, run)
	runWakeupTick(t)
	if n := wakeupRunsOf(t, rule); n != 0 {
		t.Fatalf("the agent's own comment started %d runs", n)
	}
	if outcome, _ := lastWakeupOutcome(t, issue); outcome != "acknowledged" {
		t.Fatalf("outcome = %q, want acknowledged", outcome)
	}

	commentOn(t, issue, "Can you double-check staging?", "", "")
	runWakeupTick(t)
	if n := wakeupRunsOf(t, rule); n != 1 {
		t.Fatalf("a member's comment started %d runs, want 1", n)
	}
}

// A rule that fires while its agent already has a run waiting on the issue
// keeps its inputs for that run: no second run, and the run carries the
// instruction and every fact once it is claimed.
func TestWakeupJoinsTheAgentsWaitingRun(t *testing.T) {
	issue := dbfx.Issue(t, "join waiting run")
	t.Cleanup(func() { cleanupChildDoneIssue(issue) })
	agentID := handlerTestAgentID(t)
	setIssueAssigneeDirect(t, issue, "agent", agentID)
	waiting := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "queued", "originator_user_id": testUserID, "accountable_user_id": testUserID})
	rule := createRule(t, issue, agentID, pgtype.UUID{}, service.WakeupInput{Instruction: "Summarize the discussion", Kind: "event", Mode: "continuous", EventTypes: []string{"comment.created"}})

	commentOn(t, issue, "Here is more context.", "", "")
	runWakeupTick(t)
	commentOn(t, issue, "One more thing.", "", "")
	runWakeupTick(t)
	if n := wakeupRunsOf(t, rule); n != 0 {
		t.Fatalf("the rule queued %d runs of its own", n)
	}

	notes := claimRun(t, waiting, true)
	if strings.Count(notes, "Wakeup "+rule) != 1 || !strings.Contains(notes, "Summarize the discussion") || strings.Count(notes, "comment.created") != 2 {
		t.Fatalf("joined notes = %q", notes)
	}
	outcome, taskID := lastWakeupOutcome(t, issue)
	if outcome != "merged" || taskID != waiting {
		t.Fatalf("outcome = %q task = %q, want merged into %s", outcome, taskID, waiting)
	}
	if n := wakeupRunsOf(t, rule); n != 0 {
		t.Fatalf("the rule queued %d runs of its own", n)
	}
}

// A daemon that cannot render joined wakeups claims the waiting run without
// them; the rule then starts its own run.
func TestWakeupRunsOnItsOwnAfterAnOlderDaemonsClaim(t *testing.T) {
	issue := dbfx.Issue(t, "older daemon")
	t.Cleanup(func() { cleanupChildDoneIssue(issue) })
	agentID := handlerTestAgentID(t)
	waiting := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "queued", "originator_user_id": testUserID, "accountable_user_id": testUserID})
	rule := createRule(t, issue, agentID, pgtype.UUID{}, service.WakeupInput{Instruction: "Summarize the discussion", Kind: "event", EventTypes: []string{"comment.created"}})

	commentOn(t, issue, "Here is more context.", "", "")
	runWakeupTick(t)
	if notes := claimRun(t, waiting, false); notes != "" {
		t.Fatalf("an older daemon's claim got %q", notes)
	}
	runWakeupTick(t)
	if n := wakeupRunsOf(t, rule); n != 1 {
		t.Fatalf("runs of the rule = %d, want 1", n)
	}
}

// A condition the agent's own unfinished run satisfied does not wake it when
// the agent set up the rule; a person's rule still does, because the running
// agent does not have that instruction.
func TestConditionRuleAndTheAgentsOwnChange(t *testing.T) {
	cases := []struct {
		name     string
		ownRule  bool
		wantRuns int
	}{
		{"rule the agent set up", true, 0},
		{"rule a person set up", false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := dbfx.Issue(t, "own condition", testutil.Cols{"status": "in_progress"})
			t.Cleanup(func() { cleanupChildDoneIssue(issue) })
			agentID := handlerTestAgentID(t)
			var source pgtype.UUID
			if tc.ownRule {
				earlier := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "completed"})
				source = parseUUID(earlier)
			}
			rule := createRule(t, issue, agentID, source, service.WakeupInput{Instruction: "Run the release checklist", Kind: "event",
				Condition: json.RawMessage(`{"type":"issue_field","field":"status","value":"in_review"}`)})
			runWakeupTick(t) // records the starting state
			run := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID, "status": "running"})
			setStatusAsRun(t, issue, "in_review", agentID, run)
			runWakeupTick(t)
			if n := wakeupRunsOf(t, rule); n != tc.wantRuns {
				t.Fatalf("runs = %d, want %d", n, tc.wantRuns)
			}
		})
	}
}
