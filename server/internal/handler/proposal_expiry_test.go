package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// setExpiry moves a proposal's decision window to the given SQL expression, as
// if the days had passed.
func setExpiry(t *testing.T, proposalID, sqlExpr string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE proposal SET expires_at = `+sqlExpr+` WHERE id = $1`, proposalID)
}

func runExpiry(t *testing.T) int {
	t.Helper()
	n, err := testHandler.ExpireDueProposals(context.Background())
	if err != nil {
		t.Fatalf("ExpireDueProposals: %v", err)
	}
	return n
}

func TestProposalExpiry_WindowIsFourteenDays(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "window", nil)
	var window time.Duration
	dbfx.QueryRow(t, `SELECT expires_at - created_at FROM proposal WHERE id = $1`, p.ID).Scan(&window)
	if window < 14*24*time.Hour-time.Minute || window > 14*24*time.Hour+time.Minute {
		t.Errorf("decision window = %s, want 14 days", window)
	}
	if p.ExpiresAt == nil || p.Revision != 1 {
		t.Errorf("a new proposal carries its expiry and revision 1: %+v", p)
	}
}

func TestProposalExpiry_NotBeforeTheWindowEnds(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "not yet", nil)
	setExpiry(t, p.ID, "now() + interval '1 day'")
	runExpiry(t)
	if state := proposalState(t, p.ID); state != "pending" {
		t.Fatalf("a proposal with a day left expired: state %s", state)
	}
}

func TestProposalExpiry_ExpiresAfterTheWindowWithANoteAndALeaderWakeUp(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "expires", nil)
	setExpiry(t, p.ID, "now() - interval '1 second'")

	runExpiry(t)

	var state string
	var decidedAt *time.Time
	var decidedBy *string
	dbfx.QueryRow(t, `SELECT state, decided_at, decided_by::text FROM proposal WHERE id = $1`, p.ID).Scan(&state, &decidedAt, &decidedBy)
	if state != "expired" {
		t.Fatalf("state = %s, want expired", state)
	}
	if decidedAt == nil || decidedBy != nil {
		t.Errorf("expiry records when it happened and no decider: decided_at=%v decided_by=%v", decidedAt, decidedBy)
	}

	var issueStatus string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, p.IssueID).Scan(&issueStatus)
	if issueStatus != "cancelled" {
		t.Errorf("backing issue status = %s, want cancelled", issueStatus)
	}

	var authorType, commentType, content string
	dbfx.QueryRow(t, `SELECT author_type, type, content FROM comment WHERE issue_id = $1`, p.IssueID).Scan(&authorType, &commentType, &content)
	if authorType != "system" || commentType != "system" || !strings.Contains(content, "expired") {
		t.Errorf("the expiry note = %s/%s %q, want a system note saying it expired", authorType, commentType, content)
	}

	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, p.IssueID, fx.LeaderID); n != 1 {
		t.Errorf("the leader should be woken once on the expiry note, got %d tasks", n)
	}
}

func TestProposalExpiry_LeaderIsWokenOnceAcrossTwoRuns(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "twice", nil)
	setExpiry(t, p.ID, "now() - interval '1 second'")

	runExpiry(t)
	// The second run finds nothing due for this proposal: its state already
	// moved, and the expiry update repeats the predicate under the row lock.
	runExpiry(t)
	if state := proposalState(t, p.ID); state != "expired" {
		t.Fatalf("second run left the proposal %s", state)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, p.IssueID); n != 1 {
		t.Errorf("the expiry note was written %d times, want once", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, p.IssueID, fx.LeaderID); n != 1 {
		t.Errorf("the leader was woken %d times, want once", n)
	}
}

func TestProposalExpiry_LeavesDecidedRevisingAndSnoozedAlone(t *testing.T) {
	fx := newProposalTestFixture(t)

	approved := fx.fileProposal(t, "expiry approved", nil)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(approved.ID, map[string]any{"decision": "approve"})).Want(http.StatusOK)
	setExpiry(t, approved.ID, "now() - interval '1 second'")

	revising := fx.fileProposal(t, "expiry revising", nil)
	reviseProposal(t, revising.ID, "Narrow it.")
	setExpiry(t, revising.ID, "now() - interval '1 second'")

	snoozed := fx.fileProposal(t, "expiry snoozed", nil)
	until := time.Now().AddDate(0, 0, 5).Format(time.RFC3339)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(snoozed.ID, map[string]any{"decision": "snooze", "until": until})).Want(http.StatusOK)

	runExpiry(t)

	if state := proposalState(t, approved.ID); state != "approved" {
		t.Errorf("a decided proposal became %s", state)
	}
	if state := proposalState(t, revising.ID); state != "revising" {
		t.Errorf("a revising proposal became %s; only pending proposals expire", state)
	}
	if state := proposalState(t, snoozed.ID); state != "pending" {
		t.Errorf("a snoozed proposal became %s before its window ended", state)
	}
}

func TestProposalExpiry_FreesThePendingCap(t *testing.T) {
	fx := newProposalTestFixture(t)
	// Two old pending proposals fill the squad's queue without using the weekly
	// window, so only the pending cap is in play.
	seedProposalRow(t, fx.SquadID, "pending", false, 8*24*time.Hour)
	seedProposalRow(t, fx.SquadID, "pending", false, 8*24*time.Hour)
	waiting := fx.fileProposal(t, "cap waiting", nil)

	res := testutil.Call(t, testHandler.CreateProposal, asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("cap blocked"), nil)), fx.LeaderID, fx.LeaderTaskID)).Want(http.StatusConflict)
	if res.Map()["code"] != "proposal_limit" {
		t.Fatalf("code = %v, want proposal_limit with three waiting", res.Map()["code"])
	}

	setExpiry(t, waiting.ID, "now() - interval '1 second'")
	runExpiry(t)

	testutil.Call(t, testHandler.CreateProposal, asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("cap freed"), nil)), fx.LeaderID, fx.LeaderTaskID)).Want(http.StatusCreated)
}

func TestProposalExpiry_ExpiredProposalsAreFilterable(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "filter expired", nil)
	setExpiry(t, p.ID, "now() - interval '1 second'")
	runExpiry(t)

	var out []ProposalResponse
	testutil.Call(t, testHandler.ListProposals, newRequest("GET", "/api/proposals?state=expired&squad_id="+fx.SquadID, nil)).Want(http.StatusOK).JSON(&out)
	if len(out) != 1 || out[0].ID != p.ID || out[0].State != "expired" || out[0].ExpiresAt == nil {
		t.Errorf("expired filter returned %+v", out)
	}
}
