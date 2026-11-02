package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// seedDecidedProposal inserts a proposal that was decided decidedAgo before now.
// It was created createdAgo before now, so a seeded decision does not count
// against the weekly filing cap unless a test says so. Both are Postgres interval
// strings such as "30 days".
func seedDecidedProposal(t *testing.T, workspaceID, squadID, state, createdAgo, decidedAgo string) string {
	t.Helper()
	return dbfx.Insert(t, "proposal", testutil.Cols{
		"workspace_id":    workspaceID,
		"squad_id":        squadID,
		"issue_id":        testutil.Raw("gen_random_uuid()"),
		"state":           state,
		"created_by_type": "agent",
		"created_by_id":   testutil.Raw("gen_random_uuid()"),
		"created_at":      testutil.Raw("now() - interval '" + createdAgo + "'"),
		"decided_at":      testutil.Raw("now() - interval '" + decidedAgo + "'"),
	})
}

// seedDecisions gives a squad n decisions of one state, decided a day ago.
func seedDecisions(t *testing.T, workspaceID, squadID, state string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		seedDecidedProposal(t, workspaceID, squadID, state, "30 days", "1 day")
	}
}

func recomputeAttention(t *testing.T) {
	t.Helper()
	if _, err := testHandler.RecomputeSquadAttention(context.Background()); err != nil {
		t.Fatalf("recompute squad attention: %v", err)
	}
}

func getAttention(t *testing.T, workspaceID, squadID string) SquadAttentionResponse {
	t.Helper()
	req := withURLParam(newRequest("GET", "/api/squads/"+squadID+"/attention", nil), "id", squadID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	var out SquadAttentionResponse
	testutil.Call(t, testHandler.GetSquadAttention, req).Want(http.StatusOK).JSON(&out)
	return out
}

// TestSquadAttention_BandsAtTheirBoundaries recomputes once for a squad per row
// and checks the caps, rate, sample and reason the read reports.
func TestSquadAttention_BandsAtTheirBoundaries(t *testing.T) {
	e := newDigestEnv(t)
	cases := []struct {
		name                  string
		approved, rejected    int
		expired               int
		wantPending, wantWeek int
		wantReason            string
		wantSample            int
	}{
		{"four decided is too small", 4, 0, 0, 3, 3, service.AttentionReasonSampleTooSmall, 4},
		{"exactly 20 percent keeps the defaults", 1, 4, 0, 3, 3, service.AttentionReasonDefault, 5},
		{"zero of five is below", 0, 5, 0, 1, 1, service.AttentionReasonBelowThreshold, 5},
		{"exactly 60 percent keeps the defaults", 3, 2, 0, 3, 3, service.AttentionReasonDefault, 5},
		{"four of five is above", 4, 1, 0, 4, 5, service.AttentionReasonAboveThreshold, 5},
		{"expired counts as a non-acceptance", 1, 0, 4, 3, 3, service.AttentionReasonDefault, 5},
	}
	squads := make([]string, len(cases))
	for i, c := range cases {
		squads[i] = e.squad(t, "Band "+c.name)
		seedDecisions(t, e.WorkspaceID, squads[i], "approved", c.approved)
		seedDecisions(t, e.WorkspaceID, squads[i], "rejected", c.rejected)
		seedDecisions(t, e.WorkspaceID, squads[i], "expired", c.expired)
	}
	recomputeAttention(t)

	for i, c := range cases {
		got := getAttention(t, e.WorkspaceID, squads[i])
		if got.PendingCap != c.wantPending || got.WeeklyCap != c.wantWeek || got.Reason != c.wantReason || got.SampleSize != c.wantSample {
			t.Errorf("%s: caps %d/%d reason %s sample %d, want %d/%d %s %d",
				c.name, got.PendingCap, got.WeeklyCap, got.Reason, got.SampleSize,
				c.wantPending, c.wantWeek, c.wantReason, c.wantSample)
		}
		if got.Pinned || got.PinnedPendingCap != nil {
			t.Errorf("%s: a squad that was never pinned reports a pin: %+v", c.name, got)
		}
		if got.AcceptanceRate == nil {
			t.Errorf("%s: a squad with decisions reports a rate", c.name)
		}
	}
}

func TestSquadAttention_MinimumSampleAndWindow(t *testing.T) {
	e := newDigestEnv(t)

	// Four decisions in the window plus one from 61 days ago: the old one does
	// not count, so the sample is four.
	squad := e.squad(t, "Window squad")
	seedDecisions(t, e.WorkspaceID, squad, "approved", 4)
	seedDecidedProposal(t, e.WorkspaceID, squad, "approved", "90 days", "61 days")
	recomputeAttention(t)
	got := getAttention(t, e.WorkspaceID, squad)
	if got.SampleSize != 4 || got.Reason != service.AttentionReasonSampleTooSmall || got.PendingCap != 3 || got.WeeklyCap != 3 {
		t.Fatalf("four decisions in the window must keep the defaults: %+v", got)
	}

	// A squad whose only decisions are older than the window has no sample.
	old := e.squad(t, "Old squad")
	seedDecisions(t, e.WorkspaceID, old, "approved", 5)
	dbfx.Exec(t, `UPDATE proposal SET decided_at = now() - interval '61 days' WHERE squad_id = $1`, old)
	recomputeAttention(t)
	got = getAttention(t, e.WorkspaceID, old)
	if got.SampleSize != 0 || got.Reason != service.AttentionReasonSampleTooSmall || got.PendingCap != 3 || got.AcceptanceRate != nil {
		t.Fatalf("decisions older than 60 days must not count: %+v", got)
	}
}

func TestSquadAttention_UnknownSquadAndDefaultsWithoutARow(t *testing.T) {
	e := newDigestEnv(t)
	squad := e.squad(t, "Never decided")
	got := getAttention(t, e.WorkspaceID, squad)
	if got.PendingCap != 3 || got.WeeklyCap != 3 || got.SampleSize != 0 || got.Reason != service.AttentionReasonSampleTooSmall {
		t.Fatalf("a squad with no row has the defaults: %+v", got)
	}

	req := withURLParam(newRequest("GET", "/api/squads/"+uuid.NewString()+"/attention", nil), "id", uuid.NewString())
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	testutil.Call(t, testHandler.GetSquadAttention, req).Want(http.StatusNotFound)
}

// Isolation: one squad's band does not move another squad's caps, and a squad
// in another workspace is not seen at all.
func TestSquadAttention_IsolatedPerSquadAndWorkspace(t *testing.T) {
	e := newDigestEnv(t)
	low := e.squad(t, "Low squad")
	seedDecisions(t, e.WorkspaceID, low, "rejected", 5)
	fine := e.squad(t, "Quiet squad")
	recomputeAttention(t)

	if got := getAttention(t, e.WorkspaceID, low); got.PendingCap != 1 || got.WeeklyCap != 1 {
		t.Fatalf("the rejected squad should be at 1/1: %+v", got)
	}
	if got := getAttention(t, e.WorkspaceID, fine); got.PendingCap != 3 || got.WeeklyCap != 3 || got.Reason != service.AttentionReasonSampleTooSmall {
		t.Fatalf("a squad with no decisions must keep the defaults: %+v", got)
	}

	other := newDigestEnv(t)
	req := withURLParam(newRequest("GET", "/api/squads/"+low+"/attention", nil), "id", low)
	req.Header.Set("X-Workspace-ID", other.WorkspaceID)
	testutil.Call(t, testHandler.GetSquadAttention, req).Want(http.StatusNotFound)
}
