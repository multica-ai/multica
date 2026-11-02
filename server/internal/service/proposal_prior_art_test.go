package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// uuidN is a UUID whose last byte is n, for tests that need distinct ids.
func uuidN(n byte) pgtype.UUID {
	var b [16]byte
	b[15] = n
	return pgtype.UUID{Bytes: b, Valid: true}
}

func TestTitleOverlap(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want float64
	}{
		{"identical", "Pilot usage pricing", "Pilot usage pricing", 1},
		{"reordered", "Pilot usage pricing", "usage pricing pilot", 1},
		{"case and punctuation", "PILOT: usage, pricing!", "pilot usage pricing", 1},
		{"function words do not count", "The pilot of the usage", "pilot usage", 1},
		{"plural s is ignored", "Price changes", "price change", 1},
		// "usage-based" splits into two words, and "accounts" stems to "account":
		// 3 shared of 6 distinct words.
		{"three of six", "Pilot usage pricing", "Pilot usage-based pricing for new accounts", 0.5},
		// The threshold is inclusive: exactly half the words is related.
		{"exactly the threshold", "alpha beta", "alpha beta gamma delta", 0.5},
		{"just under the threshold", "alpha beta gamma", "alpha beta delta epsilon", 0.4},
		{"one shared word", "Pilot usage pricing", "Pilot hiring plan", 0.2},
		{"nothing shared", "Quarterly hiring plan", "Pilot usage pricing", 0},
		{"empty title", "", "Pilot usage pricing", 0},
		{"only function words", "the of and", "Pilot usage pricing", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := titleOverlap(tc.a, tc.b)
			if got < tc.want-1e-9 || got > tc.want+1e-9 {
				t.Fatalf("titleOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			if rev := titleOverlap(tc.b, tc.a); rev != got {
				t.Errorf("overlap is not symmetric: %v vs %v", got, rev)
			}
		})
	}
}

func candidate(id pgtype.UUID, title string) relatedCandidate {
	return relatedCandidate{ID: id, State: "pending", Title: title, CreatedAt: time.Unix(1_700_000_000, 0), Identifier: "MUL-1"}
}

func TestRelatedByTitleKeepsSimilarProposalsMostSimilarFirst(t *testing.T) {
	self, near, far, unrelated := uuidN(1), uuidN(2), uuidN(3), uuidN(4)

	candidates := []relatedCandidate{
		candidate(self, "Pilot usage pricing"),
		candidate(far, "Pilot usage pricing for new accounts"),
		candidate(near, "Usage pricing pilot"),
		candidate(unrelated, "Quarterly hiring plan"),
	}
	got := relatedByTitle(candidates, self, "Pilot usage pricing")
	if len(got) != 2 {
		t.Fatalf("got %d related, want 2 (self and the unrelated one are out): %+v", len(got), got)
	}
	if got[0].ID != util.UUIDToString(near) || got[1].ID != util.UUIDToString(far) {
		t.Errorf("order = %s, %s; want the closer title first", got[0].ID, got[1].ID)
	}
	if got[0].Kind != RelatedKindProposal || got[0].IssueIdentifier != "MUL-1" || got[0].State != "pending" {
		t.Errorf("proposal entry = %+v", got[0])
	}
}

func TestRelatedByTitleIsCapped(t *testing.T) {
	self := uuidN(0xff)
	var candidates []relatedCandidate
	for i := 0; i < priorArtTitleLimit+3; i++ {
		candidates = append(candidates, candidate(uuidN(byte(i+1)), "Pilot usage pricing"))
	}
	if got := relatedByTitle(candidates, self, "Pilot usage pricing"); len(got) != priorArtTitleLimit {
		t.Fatalf("got %d, want the cap of %d", len(got), priorArtTitleLimit)
	}
}

func TestRelatedDecisionsNeedTheScoreFloorAndTheDecisionsBase(t *testing.T) {
	hits := []KnowledgeHitView{
		{DocumentID: "doc-exact", KnowledgeBase: DecisionsKnowledgeKey, Title: "Exact", Score: 0.75},
		{DocumentID: "doc-below", KnowledgeBase: DecisionsKnowledgeKey, Title: "Below", Score: 0.74},
		{DocumentID: "doc-market", KnowledgeBase: "market", Title: "Market", Score: 0.99},
		{DocumentID: "doc-best", KnowledgeBase: DecisionsKnowledgeKey, Title: "Best", Score: 0.9},
	}
	got := relatedDecisions(hits)
	if len(got) != 2 {
		t.Fatalf("got %+v, want the two decisions at or above 0.75", got)
	}
	if got[0].ID != "doc-best" || got[1].ID != "doc-exact" {
		t.Errorf("order = %s, %s; want best first", got[0].ID, got[1].ID)
	}
	for _, r := range got {
		if r.Kind != RelatedKindDecision || r.State != "" {
			t.Errorf("decision entry = %+v", r)
		}
	}
}

// stubDecisionSearch is the knowledge side of the prior-art lookup, scripted
// per test. The knowledge package's fake scores every hit 1.0, so the score
// floor is tested with scripted scores here, and the fake covers the rest.
type stubDecisionSearch struct {
	res   KnowledgeSearchResult
	err   error
	block bool
	calls int
	query string
	kb    string
	limit int
}

func (s *stubDecisionSearch) Search(ctx context.Context, _ pgtype.UUID, query, kb string, limit int) (KnowledgeSearchResult, error) {
	s.calls++
	s.query, s.kb, s.limit = query, kb, limit
	if s.block {
		<-ctx.Done()
		return KnowledgeSearchResult{}, ctx.Err()
	}
	return s.res, s.err
}

func TestDecisionPriorArtSearchesTheDecisionsBaseOnly(t *testing.T) {
	stub := &stubDecisionSearch{res: KnowledgeSearchResult{State: KnowledgeStateReady, Hits: []KnowledgeHitView{
		{DocumentID: "d1", KnowledgeBase: DecisionsKnowledgeKey, Title: "Pilot", Score: 0.8},
	}}}
	svc := &ProposalService{DecisionSearch: stub}
	got := svc.relatedDecisionsFor(context.Background(), pgtype.UUID{}, pgtype.UUID{}, "Pilot usage pricing", "For new accounts")
	if len(got) != 1 || got[0].ID != "d1" {
		t.Fatalf("related = %+v", got)
	}
	if stub.kb != DecisionsKnowledgeKey || stub.limit != priorArtDecisionLimit {
		t.Errorf("search scope = kb %q limit %d", stub.kb, stub.limit)
	}
	if stub.query != "Pilot usage pricing For new accounts" {
		t.Errorf("query = %q, want the title and summary flattened", stub.query)
	}
}

func TestDecisionPriorArtNeverBlocksOrFails(t *testing.T) {
	cases := []struct {
		name string
		stub *stubDecisionSearch
	}{
		{"search errors", &stubDecisionSearch{err: errors.New("sidecar down")}},
		{"knowledge not ready", &stubDecisionSearch{err: &KnowledgeNotReadyError{State: KnowledgeStateDisabled}}},
		{"offline sidecar", &stubDecisionSearch{res: KnowledgeSearchResult{State: KnowledgeStateOffline, Hits: []KnowledgeHitView{
			{DocumentID: "d1", KnowledgeBase: DecisionsKnowledgeKey, Score: 1},
		}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &ProposalService{DecisionSearch: tc.stub}
			if got := svc.relatedDecisionsFor(context.Background(), pgtype.UUID{}, pgtype.UUID{}, "Pilot usage pricing", "x"); len(got) != 0 {
				t.Fatalf("related = %+v, want none", got)
			}
		})
	}
}

func TestDecisionPriorArtStopsAtItsTimeout(t *testing.T) {
	stub := &stubDecisionSearch{block: true}
	svc := &ProposalService{DecisionSearch: stub}
	start := time.Now()
	got := svc.relatedDecisionsFor(context.Background(), pgtype.UUID{}, pgtype.UUID{}, "Pilot usage pricing", "x")
	elapsed := time.Since(start)
	if len(got) != 0 {
		t.Fatalf("related = %+v, want none after the timeout", got)
	}
	if elapsed < priorArtSearchTimeout-100*time.Millisecond || elapsed > priorArtSearchTimeout+2*time.Second {
		t.Errorf("returned after %s, want about %s", elapsed, priorArtSearchTimeout)
	}
}

func TestDecisionPriorArtSkipsTheSearchWithoutAQuery(t *testing.T) {
	stub := &stubDecisionSearch{}
	svc := &ProposalService{DecisionSearch: stub}
	if got := svc.relatedDecisionsFor(context.Background(), pgtype.UUID{}, pgtype.UUID{}, "  ", "summary"); len(got) != 0 || stub.calls != 0 {
		t.Fatalf("an empty title must not search: calls=%d related=%+v", stub.calls, got)
	}
	svc = &ProposalService{}
	if got := svc.relatedDecisionsFor(context.Background(), pgtype.UUID{}, pgtype.UUID{}, "Pilot", "x"); len(got) != 0 {
		t.Fatalf("no searcher configured must yield nothing: %+v", got)
	}
}
