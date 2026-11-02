package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func pinRequest(workspaceID, squadID string, body map[string]any) *http.Request {
	req := withURLParam(newRequest("PUT", "/api/squads/"+squadID+"/attention/pin", body), "id", squadID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	return req
}

func intPtr(n int) *int { return &n }

func TestPinSquadAttention_OnlyOwnersAndAdmins(t *testing.T) {
	e := newDigestEnv(t)
	squad := e.squad(t, "Pinned by whom")
	member := dbfx.User(t, "Plain member", "plain-"+squad+"@example.com")
	dbfx.Member(t, e.WorkspaceID, member, "member")
	admin := dbfx.User(t, "Admin", "admin-"+squad+"@example.com")
	dbfx.Member(t, e.WorkspaceID, admin, "admin")
	body := map[string]any{"pinned": true, "pending_cap": 2, "weekly_cap": 2}

	testutil.Call(t, testHandler.PinSquadAttention, asUser(pinRequest(e.WorkspaceID, squad, body), member)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.PinSquadAttention, asUser(pinRequest(e.WorkspaceID, squad, body), admin)).Want(http.StatusOK)

	// An agent actor is refused even though its task belongs to the workspace.
	taskID := dbfx.Task(t, e.AgentID, testutil.Cols{"status": "running", "runtime_id": e.RuntimeID})
	testutil.Call(t, testHandler.PinSquadAttention, asAgent(pinRequest(e.WorkspaceID, squad, body), e.AgentID, taskID)).Want(http.StatusForbidden)
}

func TestPinSquadAttention_ValidatesTheCaps(t *testing.T) {
	e := newDigestEnv(t)
	squad := e.squad(t, "Validated pin")
	for _, body := range []map[string]any{
		{"pinned": true},
		{"pinned": true, "pending_cap": 2},
		{"pinned": true, "pending_cap": 0, "weekly_cap": 2},
		{"pinned": true, "pending_cap": 2, "weekly_cap": service.AttentionMaxCap + 1},
	} {
		testutil.Call(t, testHandler.PinSquadAttention, pinRequest(e.WorkspaceID, squad, body)).Want(http.StatusBadRequest)
	}
	got := getAttention(t, e.WorkspaceID, squad)
	if got.Pinned || got.PendingCap != 3 {
		t.Fatalf("refused pins must change nothing: %+v", got)
	}
}

// A pin changes the effective caps at once and survives the nightly recompute.
// Clearing it returns the squad to its computed caps.
func TestPinSquadAttention_SetsSurvivesRecomputeAndClears(t *testing.T) {
	e := newDigestEnv(t)
	squad := e.squad(t, "Pin lifecycle")
	seedDecisions(t, e.WorkspaceID, squad, "rejected", 5) // computed 1/1
	recomputeAttention(t)

	testutil.Call(t, testHandler.PinSquadAttention, pinRequest(e.WorkspaceID, squad,
		map[string]any{"pinned": true, "pending_cap": 2, "weekly_cap": 4})).Want(http.StatusOK)
	got := getAttention(t, e.WorkspaceID, squad)
	if !got.Pinned || got.PendingCap != 2 || got.WeeklyCap != 4 || got.Reason != service.AttentionReasonPinned {
		t.Fatalf("pin should be effective at once: %+v", got)
	}
	if got.ComputedPendingCap != 1 || got.ComputedWeeklyCap != 1 {
		t.Errorf("the computed caps are reported alongside the pin: %+v", got)
	}
	if got.PinnedPendingCap == nil || *got.PinnedPendingCap != 2 || got.PinnedWeeklyCap == nil || *got.PinnedWeeklyCap != 4 {
		t.Errorf("the pinned caps should be reported: %+v", got)
	}

	recomputeAttention(t)
	if after := getAttention(t, e.WorkspaceID, squad); after.PendingCap != 2 || after.WeeklyCap != 4 || !after.Pinned {
		t.Fatalf("the nightly recompute must not undo the pin: %+v", after)
	}

	testutil.Call(t, testHandler.PinSquadAttention, pinRequest(e.WorkspaceID, squad, map[string]any{"pinned": false})).Want(http.StatusOK)
	cleared := getAttention(t, e.WorkspaceID, squad)
	if cleared.Pinned || cleared.PinnedPendingCap != nil || cleared.PinnedWeeklyCap != nil ||
		cleared.PendingCap != 1 || cleared.WeeklyCap != 1 || cleared.Reason != service.AttentionReasonBelowThreshold {
		t.Fatalf("clearing the pin returns the computed caps: %+v", cleared)
	}
}

// The pinned caps are what the filing limits check. Here the weekly cap of 1
// refuses a second filing that the default of 3 would allow.
func TestPinSquadAttention_PinnedCapsLimitFilings(t *testing.T) {
	fx := newProposalTestFixture(t)
	fx.fileProposal(t, "before the pin", nil)
	testutil.Call(t, testHandler.PinSquadAttention, pinRequest(testWorkspaceID, fx.SquadID,
		map[string]any{"pinned": true, "pending_cap": 3, "weekly_cap": 1})).Want(http.StatusOK)

	res := fx.filingAsLeader(t, "after the pin").Want(http.StatusConflict)
	if res.Map()["code"] != "proposal_limit" {
		t.Fatalf("pinned weekly cap should refuse with proposal_limit, got %v", res.Map()["code"])
	}

	testutil.Call(t, testHandler.PinSquadAttention, pinRequest(testWorkspaceID, fx.SquadID, map[string]any{"pinned": false})).Want(http.StatusOK)
	fx.filingAsLeader(t, "after clearing").Want(http.StatusCreated)
}

// The workspace cap is fixed: a squad pinned to the maximum still cannot push
// the workspace's pending queue past ten.
func TestPinSquadAttention_DoesNotMoveTheWorkspaceCap(t *testing.T) {
	fx := newProposalTestFixture(t)
	testutil.Call(t, testHandler.PinSquadAttention, pinRequest(testWorkspaceID, fx.SquadID,
		map[string]any{"pinned": true, "pending_cap": service.AttentionMaxCap, "weekly_cap": service.AttentionMaxCap})).Want(http.StatusOK)

	// Fill the workspace's queue from a neighbouring squad, so only the
	// workspace cap is in play for the pinned squad.
	neighbour := dbfx.Squad(t, "Cap neighbour "+fx.Prefix, fx.LeaderID)
	registerProposalCleanup(t, fx.Prefix, neighbour)
	pending := dbfx.Count(t, `SELECT count(*) FROM proposal WHERE workspace_id = $1 AND state = 'pending'`, testWorkspaceID)
	for i := pending; i < service.ProposalMaxPendingPerWorkspace; i++ {
		dbfx.Insert(t, "proposal", testutil.Cols{
			"workspace_id":    testWorkspaceID,
			"squad_id":        neighbour,
			"issue_id":        testutil.Raw("gen_random_uuid()"),
			"state":           "pending",
			"created_by_type": "agent",
			"created_by_id":   testutil.Raw("gen_random_uuid()"),
		})
	}

	res := fx.filingAsLeader(t, "over the workspace cap").Want(http.StatusConflict)
	if res.Map()["code"] != "proposal_limit" || !strings.Contains(res.Map()["error"].(string), "across the workspace") {
		t.Fatalf("the workspace cap must refuse even with a pinned squad: %v", res.Map())
	}
}

// A pin is per squad: another squad in the workspace keeps its own caps.
func TestPinSquadAttention_IsolatedToTheSquad(t *testing.T) {
	e := newDigestEnv(t)
	pinned := e.squad(t, "Pinned squad")
	other := e.squad(t, "Neighbour squad")
	testutil.Call(t, testHandler.PinSquadAttention, pinRequest(e.WorkspaceID, pinned,
		map[string]any{"pinned": true, "pending_cap": 1, "weekly_cap": 1})).Want(http.StatusOK)

	if got := getAttention(t, e.WorkspaceID, other); got.Pinned || got.PendingCap != 3 || got.WeeklyCap != 3 {
		t.Fatalf("a neighbour squad must keep its own caps: %+v", got)
	}

	outsider := newDigestEnv(t)
	testutil.Call(t, testHandler.PinSquadAttention, pinRequest(outsider.WorkspaceID, pinned,
		map[string]any{"pinned": true, "pending_cap": 1, "weekly_cap": 1})).Want(http.StatusNotFound)
}
