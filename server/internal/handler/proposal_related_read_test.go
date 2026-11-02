package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProposalReads_RelatedByTitleOnListAndGet(t *testing.T) {
	fx := newProposalTestFixture(t)
	earlier := fx.fileProposal(t, "pilot usage pricing", nil)
	later := fx.fileProposal(t, "pilot usage-based pricing for new accounts", nil)

	var got ProposalResponse
	testutil.Call(t, testHandler.GetProposal, getRequest("/api/proposals/"+later.ID, later.ID)).Want(http.StatusOK).JSON(&got)
	rel := findRelated(got.Related, earlier.ID)
	if rel == nil {
		t.Fatalf("GET of the later proposal should report the earlier one: %+v", got.Related)
	}
	if rel.Kind != "proposal" || rel.State == nil || *rel.State != "pending" ||
		rel.IssueIdentifier == nil || *rel.IssueIdentifier == "" || rel.CreatedAt == nil || rel.Score < 0.5 {
		t.Errorf("related entry missing its proposal fields: %+v", rel)
	}

	// An older proposal does not report a newer one: relatedness looks back.
	var older ProposalResponse
	testutil.Call(t, testHandler.GetProposal, getRequest("/api/proposals/"+earlier.ID, earlier.ID)).Want(http.StatusOK).JSON(&older)
	if older.Related == nil || len(older.Related) != 0 {
		t.Errorf("the earlier proposal should have an empty related array, got %+v", older.Related)
	}

	var list []ProposalResponse
	req := newRequest("GET", "/api/proposals?squad_id="+fx.SquadID, nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testutil.Call(t, testHandler.ListProposals, req).Want(http.StatusOK).JSON(&list)
	var listedLater *ProposalResponse
	for i := range list {
		if list[i].ID == later.ID {
			listedLater = &list[i]
		}
		if list[i].Related == nil {
			t.Fatalf("list item %s has null related", list[i].ID)
		}
	}
	if listedLater == nil || findRelated(listedLater.Related, earlier.ID) == nil {
		t.Fatalf("the list should give the later proposal the same related entry")
	}
}
