package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProposalDecisionIsRecordedInTheDecisionsBase(t *testing.T) {
	fx := newKnowledgeFixture(t)
	pfx := newProposalTestFixture(t)

	approved := pfx.fileProposal(t, "approve", nil)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(approved.ID, map[string]any{"decision": "approve", "note": "Go ahead."})).Want(http.StatusOK)
	rejected := pfx.fileProposal(t, "reject", nil)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(rejected.ID, map[string]any{"decision": "reject", "note": "Too early."})).Want(http.StatusOK)

	for _, p := range []struct{ id, decision, note string }{{approved.ID, "approved", "Go ahead."}, {rejected.ID, "rejected", "Too early."}} {
		var state, source, by, content string
		dbfx.QueryRow(t, `SELECT state, source_type, created_by_type, title FROM knowledge_link WHERE workspace_id = $1 AND idempotency_key = $2`,
			testWorkspaceID, "proposal:"+p.id+":decision").Scan(&state, &source, &by, &content)
		if state != "published" || source != "proposal" || by != "system" || !strings.Contains(content, "PROPTEST") {
			t.Fatalf("link for %s = %q %q %q %q", p.decision, state, source, by, content)
		}
		var hits service.KnowledgeSearchResult
		testutil.Call(t, testHandler.SearchKnowledge, searchRequest(p.note, "kb", "decisions")).Want(http.StatusOK).JSON(&hits)
		if len(hits.Hits) == 0 {
			t.Fatalf("decision %s is not retrievable", p.decision)
		}
		text := hits.Hits[0].Content
		for _, want := range []string{"Decision: " + p.decision, "Squad: Prop Squad", "Pilot usage-based pricing", p.note, "/issues/"} {
			if !strings.Contains(text, want) {
				t.Errorf("decision record lacks %q:\n%s", want, text)
			}
		}
	}
	if fx.fake.Calls(knowledge.OpCreateManual) != 2 {
		t.Fatalf("expected two records, got %d writes", fx.fake.Calls(knowledge.OpCreateManual))
	}
	// A second decision on the same proposal conflicts and writes nothing.
	testutil.Call(t, testHandler.DecideProposal, decideRequest(approved.ID, map[string]any{"decision": "reject"})).Want(http.StatusConflict)
	if fx.fake.Calls(knowledge.OpCreateManual) != 2 {
		t.Fatalf("a refused decision must not write, got %d writes", fx.fake.Calls(knowledge.OpCreateManual))
	}
}

func TestProposalDecisionSurvivesAnOfflineKnowledgeBase(t *testing.T) {
	fx := newKnowledgeFixture(t)
	pfx := newProposalTestFixture(t)
	p := pfx.fileProposal(t, "offline", nil)

	fx.fake.SetUnavailable(true)
	var out ProposalResponse
	testutil.Call(t, testHandler.DecideProposal, decideRequest(p.ID, map[string]any{"decision": "approve", "note": "Fine."})).Want(http.StatusOK).JSON(&out)
	if out.State != "approved" {
		t.Fatalf("decision = %+v", out)
	}
	var state, pending string
	dbfx.QueryRow(t, `SELECT state, pending_content FROM knowledge_link WHERE workspace_id = $1 AND idempotency_key = $2`,
		testWorkspaceID, "proposal:"+p.ID+":decision").Scan(&state, &pending)
	if state != "failed" || !strings.Contains(pending, "Fine.") {
		t.Fatalf("offline record = %q %q", state, pending)
	}

	// The next decision, once the sidecar is back, also finishes this one.
	fx.fake.ClearFailures()
	next := pfx.fileProposal(t, "later", nil)
	testutil.Call(t, testHandler.DecideProposal, decideRequest(next.ID, map[string]any{"decision": "reject"})).Want(http.StatusOK)
	dbfx.QueryRow(t, `SELECT state FROM knowledge_link WHERE workspace_id = $1 AND idempotency_key = $2`,
		testWorkspaceID, "proposal:"+p.ID+":decision").Scan(&state)
	if state != "published" {
		t.Fatalf("retried record = %q", state)
	}
}
