package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// reviseProposal asks the squad to change a pending proposal, as the boss would.
func reviseProposal(t *testing.T, proposalID, note string) {
	t.Helper()
	testutil.Call(t, testHandler.DecideProposal, decideRequest(proposalID, map[string]any{"decision": "revise", "note": note})).Want(http.StatusOK)
}

// resubmitRequest is the leader's revised body, sent to the revision endpoint.
func resubmitRequest(fx proposalTestFixture, proposalID string, name string) *http.Request {
	body := proposalBody(fx.SquadID, fx.title(name), map[string]any{
		"summary": "Pilot usage pricing on the new-account cohort only.",
		"ask":     "Approve a six-week pilot.",
	})
	delete(body, "squad_id")
	req := asAgent(newRequest("POST", "/api/proposals/"+proposalID+"/revision", body), fx.LeaderID, fx.LeaderTaskID)
	return withURLParam(req, "id", proposalID)
}

func proposalState(t *testing.T, proposalID string) string {
	t.Helper()
	var state string
	dbfx.QueryRow(t, `SELECT state FROM proposal WHERE id = $1`, proposalID).Scan(&state)
	return state
}

func TestDecideProposal_ReviseSendsTheProposalBackToTheSquad(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "revise", nil)

	// The note is required: a request for changes must say what to change.
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "revise"})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "revise", "note": "   "})).Want(http.StatusBadRequest)

	const note = "Price the new-account cohort only."
	var out ProposalResponse
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "revise", "note": note})).
		Want(http.StatusOK).JSON(&out)
	if out.State != "revising" || out.DecisionNote != note || out.DecidedBy == nil || *out.DecidedBy != testUserID {
		t.Fatalf("unexpected revising proposal: %+v", out)
	}
	if out.Revision != 1 {
		t.Errorf("revision = %d, want 1 until the squad resubmits", out.Revision)
	}

	var assigneeType, assigneeID, status string
	dbfx.QueryRow(t, `SELECT assignee_type, assignee_id::text, status FROM issue WHERE id = $1`, p.IssueID).Scan(&assigneeType, &assigneeID, &status)
	if assigneeType != "squad" || assigneeID != fx.SquadID {
		t.Errorf("backing issue assigned to %s/%s, want squad/%s", assigneeType, assigneeID, fx.SquadID)
	}
	if status != "in_review" {
		t.Errorf("backing issue status = %s, want in_review (a revision does not close it)", status)
	}

	var content string
	dbfx.QueryRow(t, `SELECT content FROM comment WHERE issue_id = $1`, p.IssueID).Scan(&content)
	if !strings.Contains(content, note) || !strings.Contains(content, fmt.Sprintf("mention://agent/%s", fx.LeaderID)) {
		t.Errorf("the note should mention the leader and carry the changes asked for: %q", content)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')`, p.IssueID, fx.LeaderID); n != 1 {
		t.Errorf("the squad leader should be woken once by the note, got %d queued tasks", n)
	}
}

func TestDecideProposal_RevisingRefusesDecisionsUntilResubmitted(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "revising refusals", nil)
	reviseProposal(t, p.ID, "Narrow the ask.")

	for _, body := range []map[string]any{
		{"decision": "approve"},
		{"decision": "reject", "note": "no"},
		{"decision": "revise", "note": "again"},
		{"decision": "snooze", "until": time.Now().Add(48 * time.Hour).Format(time.RFC3339)},
	} {
		res := testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, body)).Want(http.StatusConflict)
		if res.Map()["code"] != "proposal_revising" {
			t.Errorf("%v: code = %v, want proposal_revising", body["decision"], res.Map()["code"])
		}
	}
	if state := proposalState(t, p.ID); state != "revising" {
		t.Fatalf("refused decisions changed the proposal to %s", state)
	}
	var issueStatus string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, p.IssueID).Scan(&issueStatus)
	if issueStatus != "in_review" {
		t.Errorf("refused decisions changed the issue to %s", issueStatus)
	}
}

func TestDecideProposal_RefusesAgentsForReviseAndSnooze(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "agent refusals", nil)
	until := time.Now().Add(48 * time.Hour).Format(time.RFC3339)

	testutil.Call(t, testHandler.DecideProposal, asAgent(decideRequest(p.ID, map[string]any{"decision": "revise", "note": "x"}), fx.LeaderID, fx.LeaderTaskID)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DecideProposal, asAgent(decideRequest(p.ID, map[string]any{"decision": "snooze", "until": until}), fx.LeaderID, fx.LeaderTaskID)).Want(http.StatusForbidden)
	if state := proposalState(t, p.ID); state != "pending" {
		t.Fatalf("an agent's decision changed the proposal to %s", state)
	}
}

func TestDecideProposal_SnoozeKeepsItPendingAndChecksTheDate(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "snooze", nil)

	cases := []struct {
		name  string
		until string
		want  int
		code  string
	}{
		{"in the past", time.Now().Add(-time.Hour).Format(time.RFC3339), http.StatusBadRequest, "snooze_out_of_range"},
		{"more than 30 days away", time.Now().Add(31 * 24 * time.Hour).Format(time.RFC3339), http.StatusBadRequest, "snooze_out_of_range"},
		{"not a date", "next tuesday", http.StatusBadRequest, ""},
		{"missing", "", http.StatusBadRequest, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "snooze", "until": tc.until})).Want(tc.want)
			if tc.code != "" && res.Map()["code"] != tc.code {
				t.Errorf("code = %v, want %s", res.Map()["code"], tc.code)
			}
		})
	}
	if state := proposalState(t, p.ID); state != "pending" {
		t.Fatalf("refused snoozes changed the proposal to %s", state)
	}

	// A bare date is midnight UTC on that day, so three days ahead is always
	// in the future and inside the limit.
	until := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	var out ProposalResponse
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "snooze", "until": until})).
		Want(http.StatusOK).JSON(&out)
	if out.State != "pending" || out.SnoozedUntil == nil || out.DecidedBy != nil || out.DecidedAt != nil {
		t.Fatalf("a snoozed proposal stays pending with no decision recorded: %+v", out)
	}
	if out.ExpiresAt == nil {
		t.Fatalf("a snoozed proposal still has an expiry: %+v", out)
	}
	var snoozed, expires time.Time
	dbfx.QueryRow(t, `SELECT snoozed_until, expires_at FROM proposal WHERE id = $1`, p.ID).Scan(&snoozed, &expires)
	if want := time.Now().AddDate(0, 0, 3); snoozed.Before(want.Add(-24*time.Hour)) || snoozed.After(want.Add(24*time.Hour)) {
		t.Errorf("snoozed_until = %s, want about three days ahead", snoozed)
	}
	// The window restarts from the end of the snooze, so it cannot run out
	// while the boss has set the proposal aside.
	if expires.Before(snoozed.Add(13 * 24 * time.Hour)) {
		t.Errorf("expires_at = %s, want at least 14 days after the snooze ends (%s)", expires, snoozed)
	}

	// A snoozed proposal can still be decided.
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "approve"})).Want(http.StatusOK)
}

func TestSnoozedProposalCountsTowardThePendingCap(t *testing.T) {
	fx := newProposalTestFixture(t)
	first := fx.fileProposal(t, "snooze cap one", nil)
	fx.fileProposal(t, "snooze cap two", nil)
	fx.fileProposal(t, "snooze cap three", nil)

	until := time.Now().AddDate(0, 0, 7).Format(time.RFC3339)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(first.ID, map[string]any{"decision": "snooze", "until": until})).Want(http.StatusOK)

	req := asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("snooze cap four"), nil)), fx.LeaderID, fx.LeaderTaskID)
	res := testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusConflict)
	if res.Map()["code"] != "proposal_limit" {
		t.Fatalf("code = %v, want proposal_limit", res.Map()["code"])
	}
	if msg, _ := res.Map()["error"].(string); !strings.Contains(msg, "waiting for the boss's decision") {
		t.Errorf("a snoozed proposal should count as waiting: %q", msg)
	}
}

func TestResubmitProposal_ReturnsTheRevisedProposalToTheQueue(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "resubmit", nil)
	reviseProposal(t, p.ID, "Narrow it to new accounts.")

	var out ProposalResponse
	testutil.Call(t, testHandler.ReviseProposal, resubmitRequest(fx, p.ID, "resubmit v2")).Want(http.StatusOK).JSON(&out)
	if out.State != "pending" || out.Revision != 2 || out.Title != fx.title("resubmit v2") {
		t.Fatalf("unexpected resubmitted proposal: %+v", out)
	}
	if out.DecidedBy != nil || out.DecidedAt != nil || out.DecisionNote != "" {
		t.Errorf("resubmission clears the decision fields: %+v", out)
	}
	if out.ExpiresAt == nil {
		t.Errorf("resubmitted proposal should have a fresh expiry: %+v", out)
	}

	var description string
	var assigneeType *string
	dbfx.QueryRow(t, `SELECT description, assignee_type FROM issue WHERE id = $1`, p.IssueID).Scan(&description, &assigneeType)
	if !strings.Contains(description, "Pilot usage pricing on the new-account cohort only.") {
		t.Errorf("the backing issue should carry the revised body:\n%s", description)
	}
	if assigneeType != nil {
		t.Errorf("a non-urgent revision goes back unassigned, got assignee_type %s", *assigneeType)
	}

	// The revision is a normal decision target again.
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "approve"})).Want(http.StatusOK)
}

func TestResubmitProposal_RefusalsAndTheRefilingRules(t *testing.T) {
	// Each case files its own proposals: a squad may file three a week, and
	// these cases would exceed it between them.
	t.Run("a pending proposal has nothing to resubmit", func(t *testing.T) {
		fx := newProposalTestFixture(t)
		p := fx.fileProposal(t, "not revising", nil)
		res := testutil.Call(t, testHandler.ReviseProposal, resubmitRequest(fx, p.ID, "not revising v2")).Want(http.StatusConflict)
		if res.Map()["code"] != "proposal_not_revising" {
			t.Errorf("code = %v, want proposal_not_revising", res.Map()["code"])
		}
	})
	t.Run("an agent outside the squad is refused", func(t *testing.T) {
		fx := newProposalTestFixture(t)
		outsider := createHandlerTestAgent(t, "Prop Revise Outsider "+fx.Prefix, nil)
		outsiderTask := createHandlerTestTaskForAgent(t, outsider)
		p := fx.fileProposal(t, "outsider resubmit", nil)
		reviseProposal(t, p.ID, "Try again.")
		req := asAgent(newRequest("POST", "/api/proposals/"+p.ID+"/revision", proposalBody(fx.SquadID, fx.title("outsider v2"), nil)), outsider, outsiderTask)
		testutil.Call(t, testHandler.ReviseProposal, withURLParam(req, "id", p.ID)).Want(http.StatusForbidden)
		if state := proposalState(t, p.ID); state != "revising" {
			t.Errorf("a refused resubmission changed the proposal to %s", state)
		}
	})
	t.Run("a decided proposal is refused", func(t *testing.T) {
		fx := newProposalTestFixture(t)
		// Approved while pending, so it is decided rather than revising.
		p := fx.fileProposal(t, "decided resubmit", nil)
		testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "approve"})).Want(http.StatusOK)
		req := asAgent(newRequest("POST", "/api/proposals/"+p.ID+"/revision", proposalBody(fx.SquadID, fx.title("decided v2"), nil)), fx.LeaderID, fx.LeaderTaskID)
		res := testutil.Call(t, testHandler.ReviseProposal, withURLParam(req, "id", p.ID)).Want(http.StatusConflict)
		if res.Map()["code"] != "proposal_decided" {
			t.Errorf("code = %v, want proposal_decided", res.Map()["code"])
		}
	})
	t.Run("an incomplete body is refused", func(t *testing.T) {
		fx := newProposalTestFixture(t)
		p := fx.fileProposal(t, "incomplete resubmit", nil)
		reviseProposal(t, p.ID, "Fix the ask.")
		req := asAgent(newRequest("POST", "/api/proposals/"+p.ID+"/revision", proposalBody(fx.SquadID, fx.title("incomplete v2"), map[string]any{"ask": " "})), fx.LeaderID, fx.LeaderTaskID)
		testutil.Call(t, testHandler.ReviseProposal, withURLParam(req, "id", p.ID)).Want(http.StatusBadRequest)
		if state := proposalState(t, p.ID); state != "revising" {
			t.Errorf("an invalid resubmission changed the proposal to %s", state)
		}
	})
}

func TestResubmitProposal_CountsAgainstThePendingCaps(t *testing.T) {
	fx := newProposalTestFixture(t)
	// Two old pending proposals fill the squad's queue without touching the
	// weekly window, so only the pending cap is in play.
	seedProposalRow(t, fx.SquadID, "pending", false, 8*24*time.Hour)
	seedProposalRow(t, fx.SquadID, "pending", false, 8*24*time.Hour)
	first := fx.fileProposal(t, "cap resubmit first", nil)
	reviseProposal(t, first.ID, "Narrow it.")
	// The queue has room again, so a new filing takes the third slot.
	fx.fileProposal(t, "cap resubmit second", nil)

	// Back in the queue the revision would be the fourth pending proposal.
	res := testutil.Call(t, testHandler.ReviseProposal, resubmitRequest(fx, first.ID, "cap resubmit first v2")).Want(http.StatusConflict)
	if res.Map()["code"] != "proposal_limit" {
		t.Fatalf("code = %v, want proposal_limit", res.Map()["code"])
	}
	if msg, _ := res.Map()["error"].(string); !strings.Contains(msg, "before sending this revision back") {
		t.Errorf("the refusal should say what to do: %q", msg)
	}
	if state := proposalState(t, first.ID); state != "revising" {
		t.Fatalf("a refused resubmission changed the proposal to %s", state)
	}

	// Deciding one of the pending proposals frees a slot for the revision.
	var pendingID string
	dbfx.QueryRow(t, `SELECT id::text FROM proposal WHERE squad_id = $1 AND state = 'pending' AND issue_id IN (SELECT id FROM issue WHERE title = $2) LIMIT 1`, fx.SquadID, fx.title("cap resubmit second")).Scan(&pendingID)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(pendingID, map[string]any{"decision": "reject", "note": "Not now."})).Want(http.StatusOK)
	testutil.Call(t, testHandler.ReviseProposal, resubmitRequest(fx, first.ID, "cap resubmit first v2")).Want(http.StatusOK)
}

func TestRevisionDoesNotCountAsANewFiling(t *testing.T) {
	fx := newProposalTestFixture(t)
	first := fx.fileProposal(t, "weekly one", nil)
	second := fx.fileProposal(t, "weekly two", nil)

	// Revising and resubmitting both proposals files nothing new: the weekly
	// count stays at two, so a third filing is still allowed.
	reviseProposal(t, first.ID, "Narrow it.")
	testutil.Call(t, testHandler.ReviseProposal, resubmitRequest(fx, first.ID, "weekly one v2")).Want(http.StatusOK)
	reviseProposal(t, second.ID, "Narrow it.")
	testutil.Call(t, testHandler.ReviseProposal, resubmitRequest(fx, second.ID, "weekly two v2")).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT count(*) FROM proposal WHERE squad_id = $1`, fx.SquadID); n != 2 {
		t.Fatalf("revisions created rows: %d proposals, want 2", n)
	}
	fx.fileProposal(t, "weekly three", nil)

	// Now the squad has filed three this week. Approving one frees a pending
	// slot, so the weekly cap is the one that refuses a fourth.
	testutil.Call(t, testHandler.DecideProposal, decideRequest(first.ID, map[string]any{"decision": "approve"})).Want(http.StatusOK)
	res := testutil.Call(t, testHandler.CreateProposal, asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("weekly four"), nil)), fx.LeaderID, fx.LeaderTaskID)).Want(http.StatusConflict)
	if msg, _ := res.Map()["error"].(string); !strings.Contains(msg, "last 7 days") {
		t.Errorf("the fourth filing should hit the weekly cap: %q", msg)
	}
}

func TestRevisionEndpointsAreWorkspaceScoped(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "scoped revise", nil)
	reviseProposal(t, p.ID, "Narrow it.")

	otherWS := dbfx.Workspace(t, "Prop Revise Other WS", fmt.Sprintf("prop-revise-other-%d", time.Now().UnixNano()))
	dbfx.Member(t, otherWS, testUserID, "owner")
	inOther := func(req *http.Request) *http.Request { return testutil.WithHeaders(req, "X-Workspace-ID", otherWS) }

	testutil.Call(t, testHandler.DecideProposal, inOther(decideRequest(p.ID, map[string]any{"decision": "snooze", "until": time.Now().Add(48 * time.Hour).Format(time.RFC3339)}))).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.ReviseProposal, inOther(resubmitRequest(fx, p.ID, "scoped v2"))).Want(http.StatusNotFound)
	if state := proposalState(t, p.ID); state != "revising" {
		t.Errorf("another workspace changed the proposal to %s", state)
	}
}
