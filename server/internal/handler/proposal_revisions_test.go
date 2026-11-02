package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func listRevisionsRequest(proposalID string) *http.Request {
	return withURLParam(newRequest("GET", "/api/proposals/"+proposalID+"/revisions", nil), "id", proposalID)
}

func TestProposalRevisions_FiledAndResubmittedBodiesAreKept(t *testing.T) {
	fx := newProposalTestFixture(t)
	p := fx.fileProposal(t, "history", nil)
	reviseProposal(t, p.ID, "Narrow the ask.")
	testutil.Call(t, testHandler.ReviseProposal, resubmitRequest(fx, p.ID, "history")).Want(http.StatusOK)

	var revs []ProposalRevisionResponse
	testutil.Call(t, testHandler.ListProposalRevisions, listRevisionsRequest(p.ID)).Want(http.StatusOK).JSON(&revs)
	if len(revs) != 2 {
		t.Fatalf("want two revisions (filed and resubmitted), got %+v", revs)
	}
	if revs[0].Revision != 2 || revs[1].Revision != 1 {
		t.Fatalf("revisions must be newest first, got %d then %d", revs[0].Revision, revs[1].Revision)
	}
	if !strings.Contains(revs[0].Body, "Pilot usage pricing on the new-account cohort only.") {
		t.Errorf("revision 2 should carry the resubmitted body: %q", revs[0].Body)
	}
	if !strings.Contains(revs[1].Body, "Competitors shipped usage-based pricing.") {
		t.Errorf("revision 1 should keep the body as filed: %q", revs[1].Body)
	}
	for _, rev := range revs {
		if rev.CreatedByType != "agent" || rev.CreatedByID != fx.LeaderID || rev.CreatedAt == "" {
			t.Errorf("revision %d author/time = %s/%s/%q, want the leader agent and a timestamp",
				rev.Revision, rev.CreatedByType, rev.CreatedByID, rev.CreatedAt)
		}
	}

	// The newest history entry is the body the issue now carries.
	var description string
	dbfx.QueryRow(t, `SELECT description FROM issue WHERE id = $1`, p.IssueID).Scan(&description)
	if revs[0].Body != description {
		t.Errorf("the newest revision should equal the issue description\nrevision: %q\nissue:    %q", revs[0].Body, description)
	}
}

func TestProposalRevisions_ProposalWithoutHistoryGetsAnEmptyArray(t *testing.T) {
	fx := newProposalTestFixture(t)
	// A row from before the history table existed: no revision rows at all.
	id := seedProposalRow(t, fx.SquadID, "pending", false, 0)

	var revs []ProposalRevisionResponse
	testutil.Call(t, testHandler.ListProposalRevisions, listRevisionsRequest(id)).Want(http.StatusOK).JSON(&revs)
	if revs == nil || len(revs) != 0 {
		t.Fatalf("a proposal with no history must answer an empty array, got %#v", revs)
	}
}

func TestProposalRevisions_UnknownProposalIsNotFound(t *testing.T) {
	testutil.Call(t, testHandler.ListProposalRevisions, listRevisionsRequest(uuid.NewString())).Want(http.StatusNotFound)
}
