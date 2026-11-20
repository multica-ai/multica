package handler

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func updateIssueForOutcome(t *testing.T, issueID string, body map[string]any) IssueResponse {
	t.Helper()
	var resp IssueResponse
	testutil.Call(t, testHandler.UpdateIssue,
		withURLParam(newRequest("PUT", "/api/issues/"+issueID, body), "id", issueID)).Want(http.StatusOK).JSON(&resp)
	return resp
}

func wantRunOutcome(t *testing.T, got *IssueRunOutcome, targetType, targetID string, status DispatchStatus, reason DispatchReasonCode) {
	t.Helper()
	want := IssueRunOutcome{TargetType: targetType, TargetID: targetID, Status: status, ReasonCode: reason}
	if got == nil || *got != want {
		t.Fatalf("run_outcome = %+v, want %+v", got, want)
	}
}

// An issue write tells its caller whether it started a run and, if not, why
// (#10845). Only the caller gets the outcome: the issue:updated broadcast
// reaches every member and carries none.
func TestUpdateIssueReportsRunOutcome(t *testing.T) {
	agentID := seededReadyAgentID(t)
	assign := map[string]any{"assignee_type": "agent", "assignee_id": agentID}

	var mu sync.Mutex
	broadcastOutcomes := map[string]*IssueRunOutcome{}
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		if resp, ok := payload["issue"].(IssueResponse); ok {
			mu.Lock()
			broadcastOutcomes[resp.ID] = resp.RunOutcome
			mu.Unlock()
		}
	})

	t.Run("assignment that starts a run", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "outcome queued", "status": "todo"})
		resp := updateIssueForOutcome(t, issue.ID, assign)
		wantRunOutcome(t, resp.RunOutcome, "agent", agentID, DispatchQueued, ReasonQueued)
		if got := taskCountFor(t, issue.ID, agentID); got != 1 {
			t.Fatalf("tasks = %d, want 1", got)
		}
		mu.Lock()
		broadcast, seen := broadcastOutcomes[issue.ID]
		mu.Unlock()
		if !seen || broadcast != nil {
			t.Fatalf("issue:updated broadcast seen=%v run_outcome=%+v, want a broadcast without one", seen, broadcast)
		}
	})

	t.Run("assignment to an agent that already has a pending run", func(t *testing.T) {
		// The assign path skips the pending check, so the enqueue meets the
		// pending-slot index and coalesces into the existing task.
		issue := createIssueForTest(t, map[string]any{"title": "outcome coalesced", "status": "todo"})
		var runtimeID string
		dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
		dbfx.Task(t, agentID, testutil.Cols{"issue_id": issue.ID, "runtime_id": runtimeID, "status": "queued"})
		resp := updateIssueForOutcome(t, issue.ID, assign)
		wantRunOutcome(t, resp.RunOutcome, "agent", agentID, DispatchCoalesced, ReasonCoalesced)
		if got := taskCountFor(t, issue.ID, agentID); got != 1 {
			t.Fatalf("tasks = %d, want the existing one only", got)
		}
	})

	t.Run("assignment while in triage", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "outcome triage", "status": "todo"})
		dbfx.Exec(t, `UPDATE issue SET triage_state = 'pending' WHERE id = $1`, issue.ID)
		resp := updateIssueForOutcome(t, issue.ID, assign)
		wantRunOutcome(t, resp.RunOutcome, "agent", agentID, DispatchBlocked, ReasonIssueInTriage)
	})

	t.Run("assignment with suppress_run", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "outcome suppressed", "status": "todo"})
		body := map[string]any{"assignee_type": "agent", "assignee_id": agentID, "suppress_run": true}
		resp := updateIssueForOutcome(t, issue.ID, body)
		wantRunOutcome(t, resp.RunOutcome, "agent", agentID, DispatchDeferred, ReasonDeferred)
		if got := taskCountFor(t, issue.ID, agentID); got != 0 {
			t.Fatalf("tasks = %d, want none", got)
		}
	})

	t.Run("edit that could not start a run", func(t *testing.T) {
		issue := createIssueForTest(t, map[string]any{"title": "outcome edit", "status": "todo"})
		updateIssueForOutcome(t, issue.ID, assign)
		resp := updateIssueForOutcome(t, issue.ID, map[string]any{"title": "outcome edit renamed"})
		if resp.RunOutcome != nil {
			t.Fatalf("title edit run_outcome = %+v, want none", resp.RunOutcome)
		}
	})

	t.Run("promotion that starts an agent the caller may not invoke", func(t *testing.T) {
		// The write starts the run, but the outcome would expose a private
		// agent's readiness to a caller who cannot invoke it.
		var runtimeID string
		dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
		otherUser := dbfx.User(t, "outcome-other-owner", fmt.Sprintf("outcome-other-%d@example.com", time.Now().UnixNano()))
		privateAgent := dbfx.Agent(t, "Outcome Private Agent", runtimeID, testutil.Cols{"owner_id": otherUser})
		issue := createIssueForTest(t, map[string]any{"title": "outcome private", "status": "backlog"})
		dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issue.ID, privateAgent)
		dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)

		resp := updateIssueForOutcome(t, issue.ID, map[string]any{"status": "todo"})
		if resp.RunOutcome != nil {
			t.Fatalf("run_outcome = %+v, want none for an agent the caller may not invoke", resp.RunOutcome)
		}
		if got := taskCountFor(t, issue.ID, privateAgent); got != 1 {
			t.Fatalf("tasks = %d, want the promotion to start one run", got)
		}
	})
}

func TestBatchUpdateIssuesReportsRunOutcomePerIssue(t *testing.T) {
	agentID := seededReadyAgentID(t)
	ready := createIssueForTest(t, map[string]any{"title": "batch outcome queued", "status": "todo"})
	triage := createIssueForTest(t, map[string]any{"title": "batch outcome triage", "status": "todo"})
	dbfx.Exec(t, `UPDATE issue SET triage_state = 'pending' WHERE id = $1`, triage.ID)
	body := func(dryRun bool) map[string]any {
		return map[string]any{
			"issue_ids": []string{ready.ID, triage.ID},
			"updates":   map[string]any{"assignee_type": "agent", "assignee_id": agentID},
			"dry_run":   dryRun,
		}
	}

	code, preview, raw := batchUpdate(t, body(true))
	if code != http.StatusOK {
		t.Fatalf("dry run: %d %s", code, raw)
	}
	for _, issue := range []IssueResponse{ready, triage} {
		if got := resultFor(t, preview, issue.ID).RunOutcome; got != nil {
			t.Fatalf("dry-run item %s run_outcome = %+v, want none", issue.ID, got)
		}
	}

	code, resp, raw := batchUpdate(t, body(false))
	if code != http.StatusOK {
		t.Fatalf("batch update: %d %s", code, raw)
	}
	wantRunOutcome(t, resultFor(t, resp, ready.ID).RunOutcome, "agent", agentID, DispatchQueued, ReasonQueued)
	wantRunOutcome(t, resultFor(t, resp, triage.ID).RunOutcome, "agent", agentID, DispatchBlocked, ReasonIssueInTriage)
}

func TestIssueWritesOmitRunOutcomeForMemberAssignees(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			status  string
			triage  bool
			promote bool
		}{
			{name: "backlog assignment", status: "backlog"},
			{name: "triage assignment", status: "todo", triage: true},
			{name: "active assignment", status: "todo"},
			{name: "backlog promotion", status: "backlog", promote: true},
			{name: "triage promotion", status: "backlog", triage: true, promote: true},
		} {
			t.Run(fmt.Sprintf("batch=%v/%s", batch, tc.name), func(t *testing.T) {
				cols := testutil.Cols{"status": tc.status}
				if tc.triage {
					cols["triage_state"] = "pending"
				}
				updates := map[string]any{"assignee_type": "member", "assignee_id": testUserID}
				if tc.promote {
					cols["assignee_type"], cols["assignee_id"] = "member", testUserID
					updates = map[string]any{"status": "todo"}
				}
				issueID := dbfx.Issue(t, "member outcome "+tc.name, cols)
				var outcome *IssueRunOutcome
				if batch {
					code, resp, raw := batchUpdate(t, map[string]any{"issue_ids": []string{issueID}, "updates": updates})
					if code != http.StatusOK {
						t.Fatalf("batch update: %d %s", code, raw)
					}
					outcome = resultFor(t, resp, issueID).RunOutcome
				} else {
					resp := updateIssueForOutcome(t, issueID, updates)
					outcome = resp.RunOutcome
				}
				if outcome != nil {
					t.Errorf("member run_outcome = %+v, want none", outcome)
				}
				var assigneeType, assigneeID string
				dbfx.QueryRow(t, `SELECT assignee_type, assignee_id FROM issue WHERE id = $1`, issueID).Scan(&assigneeType, &assigneeID)
				if assigneeType != "member" || assigneeID != testUserID {
					t.Fatalf("assignee = %s/%s, want member/%s", assigneeType, assigneeID, testUserID)
				}
				var tasks int
				dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&tasks)
				if tasks != 0 {
					t.Fatalf("member tasks = %d, want none", tasks)
				}
			})
		}
	}
}

func TestUpdateIssueOutcomeVisibilityDoesNotLogInvocationDenial(t *testing.T) {
	readyAgent := seededReadyAgentID(t)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, readyAgent).Scan(&runtimeID)
	actor := dbfx.Agent(t, "outcome caller", runtimeID)
	callerIssue := dbfx.Issue(t, "outcome caller issue")
	// The request's delegation chain has no human, while the promoted issue
	// has a member creator. Enqueue and outcome visibility judge those separately.
	callerTask := dbfx.Task(t, actor, testutil.Cols{"issue_id": callerIssue, "runtime_id": runtimeID, "status": "running"})

	for _, targetType := range []string{"agent", "squad"} {
		t.Run(targetType, func(t *testing.T) {
			leader := dbfx.Agent(t, "outcome private "+targetType, runtimeID)
			targetID := leader
			if targetType == "squad" {
				targetID = dbfx.Squad(t, "outcome private squad", leader)
			}
			issueID := dbfx.Issue(t, "outcome promotion "+targetType, testutil.Cols{
				"status": "backlog", "assignee_type": targetType, "assignee_id": targetID,
			})
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			var resp IssueResponse
			testutil.Call(t, testHandler.UpdateIssue, asRun(withURLParam(
				newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "todo"}), "id", issueID), actor, callerTask)).
				Want(http.StatusOK).JSON(&resp)
			if resp.RunOutcome != nil {
				t.Errorf("run_outcome = %+v, want none without invocation permission", resp.RunOutcome)
			}
			if got := taskCountFor(t, issueID, leader); got != 1 {
				t.Fatalf("tasks = %d, want promotion to start one run", got)
			}
			if strings.Contains(logs.String(), "agent invoke denied") {
				t.Errorf("outcome visibility logged a denial after enqueue:\n%s", logs.String())
			}
		})
	}
}
