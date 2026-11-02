package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// fileProposalWithPriorArt files a proposal and returns the whole create response,
// which carries the related entries.
func (fx proposalTestFixture) fileProposalWithPriorArt(t *testing.T, name string, over map[string]any) ProposalCreateResponse {
	t.Helper()
	req := asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title(name), over)), fx.LeaderID, fx.LeaderTaskID)
	var out ProposalCreateResponse
	testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusCreated).JSON(&out)
	if out.Related == nil {
		t.Fatalf("related must be an array, never null")
	}
	return out
}

func findRelated(related []ProposalRelatedResponse, id string) *ProposalRelatedResponse {
	for i := range related {
		if related[i].ID == id {
			return &related[i]
		}
	}
	return nil
}

func TestCreateProposal_RelatedProposalsByTitle(t *testing.T) {
	fx := newProposalTestFixture(t)
	earlier := fx.fileProposal(t, "pilot usage pricing", nil)
	unrelated := fx.fileProposal(t, "quarterly hiring plan", nil)

	out := fx.fileProposalWithPriorArt(t, "usage pricing pilot", nil)
	match := findRelated(out.Related, earlier.ID)
	if match == nil {
		t.Fatalf("the same words in another order should be related: %+v", out.Related)
	}
	if match.Kind != "proposal" || match.Title != earlier.Title || match.Score < 0.5 {
		t.Errorf("related entry = %+v", match)
	}
	if match.State == nil || *match.State != "pending" || match.IssueIdentifier == nil || match.CreatedAt == nil {
		t.Errorf("a proposal entry carries its state, identifier and creation time: %+v", match)
	}
	if findRelated(out.Related, unrelated.ID) != nil {
		t.Errorf("an unrelated title must not be reported: %+v", out.Related)
	}
	if findRelated(out.Related, out.ID) != nil {
		t.Errorf("a proposal is never its own prior art")
	}
}

func TestCreateProposal_RelatedIgnoresOldAndOtherWorkspaceProposals(t *testing.T) {
	fx := newProposalTestFixture(t)
	old := fx.fileProposal(t, "pilot usage pricing old", nil)
	dbfx.Exec(t, `UPDATE proposal SET created_at = now() - interval '100 days' WHERE id = $1`, old.ID)

	// A proposal in another workspace with the same words. Its squad need not
	// exist: the lookup is scoped by workspace, not by squad.
	otherWS := dbfx.Workspace(t, "Prop Prior Other WS", fmt.Sprintf("prop-prior-other-%d", time.Now().UnixNano()))
	dbfx.Member(t, otherWS, testUserID, "owner")
	other := testutil.New(testPool, otherWS, testUserID)
	issueID := other.Issue(t, fx.title("pilot usage pricing other"))
	otherProposal := other.Insert(t, "proposal", testutil.Cols{
		"workspace_id":    otherWS,
		"squad_id":        testutil.Raw("gen_random_uuid()"),
		"issue_id":        issueID,
		"created_by_type": "agent",
		"created_by_id":   testutil.Raw("gen_random_uuid()"),
	})

	out := fx.fileProposalWithPriorArt(t, "usage pricing pilot", nil)
	if findRelated(out.Related, old.ID) != nil {
		t.Errorf("a proposal older than 90 days must not be reported: %+v", out.Related)
	}
	if findRelated(out.Related, otherProposal) != nil {
		t.Errorf("another workspace's proposal must not be reported: %+v", out.Related)
	}
}

func TestCreateProposal_RelatedDecisionsFromTheKnowledgeBase(t *testing.T) {
	newKnowledgeFixture(t)
	pfx := newProposalTestFixture(t)

	// The same words, decided: the decision record joins the decisions base.
	body := map[string]any{
		"summary":        "Pilot usage pricing for new accounts",
		"recommendation": "Pilot usage pricing for new accounts",
	}
	decided := pfx.fileProposal(t, "usage pricing pilot", body)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(decided.ID, map[string]any{"decision": "approve", "note": "Go ahead"})).Want(http.StatusOK)

	out := pfx.fileProposalWithPriorArt(t, "usage pricing pilot", body)
	var decision *ProposalRelatedResponse
	for i := range out.Related {
		if out.Related[i].Kind == "decision" {
			decision = &out.Related[i]
		}
	}
	if decision == nil {
		t.Fatalf("the decision record should be related: %+v", out.Related)
	}
	if decision.Score < 0.75 || decision.ID == "" || decision.Title != pfx.title("usage pricing pilot") {
		t.Errorf("decision entry = %+v", decision)
	}
	if decision.State != nil || decision.IssueIdentifier != nil || decision.CreatedAt != nil {
		t.Errorf("a decision entry carries no proposal fields: %+v", decision)
	}
}

func TestCreateProposal_PriorArtFailureNeverBlocksFiling(t *testing.T) {
	t.Run("knowledge not enabled", func(t *testing.T) {
		useKnowledge(t)
		fx := newProposalTestFixture(t)
		out := fx.fileProposalWithPriorArt(t, "knowledge off", nil)
		if out.State != "pending" {
			t.Fatalf("filing with knowledge off: %+v", out.ProposalResponse)
		}
	})

	t.Run("the decisions search fails", func(t *testing.T) {
		kf := newKnowledgeFixture(t)
		kf.fake.FailAlways(knowledge.OpSearch, errors.New("sidecar exploded"))
		fx := newProposalTestFixture(t)
		out := fx.fileProposalWithPriorArt(t, "search fails", nil)
		if out.State != "pending" || findRelatedKind(out.Related, "decision") {
			t.Fatalf("filing with a failing search: state=%s related=%+v", out.State, out.Related)
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM proposal WHERE id = $1`, out.ID); n != 1 {
			t.Errorf("the proposal must be stored, got %d rows", n)
		}
	})
}

func findRelatedKind(related []ProposalRelatedResponse, kind string) bool {
	for _, r := range related {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

// The related field is present, as an empty array, when nothing is related.
func TestCreateProposal_RelatedIsAnEmptyArrayWhenNothingMatches(t *testing.T) {
	useKnowledge(t)
	fx := newProposalTestFixture(t)
	req := asAgent(newRequest("POST", "/api/proposals", proposalBody(fx.SquadID, fx.title("nothing like it"), nil)), fx.LeaderID, fx.LeaderTaskID)
	res := testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusCreated)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(res.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["related"]) != "[]" {
		t.Errorf("related = %s, want []", raw["related"])
	}
}
