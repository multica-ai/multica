package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// filingAsLeader files one proposal for the fixture squad and returns the
// response: the caller checks the status.
func (fx proposalTestFixture) filingAsLeader(t *testing.T, name string) *testutil.Response {
	t.Helper()
	req := asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title(name), nil)), fx.LeaderID, fx.LeaderTaskID)
	return testutil.Call(t, testHandler.CreateProposal, req)
}

// The filing limits read the squad's computed caps. A squad whose decisions
// are mostly rejected is held to one filing a week.
func TestProposalCaps_FollowTheComputedBand(t *testing.T) {
	fx := newProposalTestFixture(t)
	seedDecisions(t, testWorkspaceID, fx.SquadID, "rejected", 5)
	recomputeAttention(t)

	fx.fileProposal(t, "low band first", nil)
	res := fx.filingAsLeader(t, "low band second").Want(http.StatusConflict)
	if res.Map()["code"] != "proposal_limit" {
		t.Fatalf("second filing in the week must be refused with proposal_limit, got %v", res.Map()["code"])
	}
}

// The high band raises the pending cap to four, so the fourth proposal waiting
// for the boss is allowed. The default would refuse it.
func TestProposalCaps_HighBandAllowsAFourthPending(t *testing.T) {
	fx := newProposalTestFixture(t)
	seedDecisions(t, testWorkspaceID, fx.SquadID, "approved", 4)
	seedDecisions(t, testWorkspaceID, fx.SquadID, "rejected", 1)
	recomputeAttention(t)

	for i := 1; i <= 4; i++ {
		fx.filingAsLeader(t, "high band "+string(rune('a'+i))).Want(http.StatusCreated)
	}
	fx.filingAsLeader(t, "high band fifth").Want(http.StatusConflict)
}

// Recomputing again with the same decisions writes the same caps.
func TestRecomputeSquadAttention_IsIdempotent(t *testing.T) {
	e := newDigestEnv(t)
	squad := e.squad(t, "Idempotent squad")
	seedDecisions(t, e.WorkspaceID, squad, "rejected", 5)
	recomputeAttention(t)
	first := getAttention(t, e.WorkspaceID, squad)
	recomputeAttention(t)
	second := getAttention(t, e.WorkspaceID, squad)
	if first.PendingCap != second.PendingCap || first.WeeklyCap != second.WeeklyCap || first.SampleSize != second.SampleSize || first.Reason != second.Reason {
		t.Fatalf("second recompute changed the caps: %+v then %+v", first, second)
	}
}
