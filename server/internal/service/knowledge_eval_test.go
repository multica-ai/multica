package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestEvalHitRateArithmetic(t *testing.T) {
	cases := []struct {
		hits, total int
		want        float64
	}{
		{0, 0, 0},
		{0, 4, 0},
		{3, 4, 0.75},
		{4, 4, 1},
		{1, 3, 1.0 / 3.0},
	}
	for _, c := range cases {
		if got := EvalHitRate(c.hits, c.total); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("EvalHitRate(%d, %d) = %v, want %v", c.hits, c.total, got, c.want)
		}
	}
}

func TestEvalMatchJudgesOnlyTheTopResults(t *testing.T) {
	hits := []KnowledgeHitView{
		{DocumentID: "d1", KnowledgeKind: "policy"},
		{DocumentID: "d2", KnowledgeKind: "adr"},
	}
	if ok, rank := evalMatch(db.KnowledgeEvalCase{ExpectedKind: "adr"}, hits); !ok || rank != 2 {
		t.Fatalf("kind: ok %v rank %d", ok, rank)
	}
	if ok, _ := evalMatch(db.KnowledgeEvalCase{ExpectedDocumentID: "d2"}, hits); !ok {
		t.Fatal("a named document in the results did not hit")
	}
	if ok, _ := evalMatch(db.KnowledgeEvalCase{ExpectedDocumentID: "d9"}, hits); ok {
		t.Fatal("a missing document hit")
	}
	long := make([]KnowledgeHitView, EvalTopK+1)
	long[EvalTopK] = KnowledgeHitView{DocumentID: "late", KnowledgeKind: "adr"}
	if ok, _ := evalMatch(db.KnowledgeEvalCase{ExpectedKind: "adr"}, long); ok {
		t.Fatal("a result below the top five counted")
	}
}

func TestRetrievalHealthRaisesARiskOnlyForAFallBeyondTheThreshold(t *testing.T) {
	run := func(rate float64) db.KnowledgeEvalRun {
		return db.KnowledgeEvalRun{Total: 10, Hits: int32(math.Round(rate * 10)), HitRate: rate, RanAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}
	}
	if got := retrievalHealth(nil); got != nil {
		t.Fatalf("no runs: %+v", got)
	}
	first := retrievalHealth([]db.KnowledgeEvalRun{run(0.9)})
	if first.Dropped || first.PreviousHitRate != nil {
		t.Fatalf("a single run cannot drop: %+v", first)
	}
	small := retrievalHealth([]db.KnowledgeEvalRun{run(0.8), run(0.9)})
	if small.Dropped || small.Risk != "" {
		t.Fatalf("a fall of ten points is the threshold, not beyond it: %+v", small)
	}
	dropped := retrievalHealth([]db.KnowledgeEvalRun{run(0.5), run(0.9)})
	if !dropped.Dropped || dropped.Risk == "" || *dropped.PreviousHitRate != 0.9 {
		t.Fatalf("a fall from 90%% to 50%% carries no risk: %+v", dropped)
	}
}

func TestEvalCaseNeedsAQuestionAndOneExpectation(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	member := memberActor(e)
	bad := []EvalCaseInput{
		{Question: "  ", ExpectedKind: "adr"},
		{Question: "q"},
		{Question: "q", ExpectedKind: "adr", ExpectedDocumentID: "doc-1"},
		{Question: "q", ExpectedKind: "no_such_kind"},
	}
	for _, in := range bad {
		if _, err := e.svc.CreateEvalCase(ctx, e.ws, member, in); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
	c, err := e.svc.CreateEvalCase(ctx, e.ws, member, EvalCaseInput{Question: " pricing? ", ExpectedDocumentID: "doc-1"})
	if err != nil || c.Question != "pricing?" || c.CreatedByType != KnowledgeActorMember {
		t.Fatalf("case = %+v, %v", c, err)
	}
	if err := e.svc.DeleteEvalCase(ctx, e.ws, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteEvalCase(ctx, e.ws, c.ID); !errors.Is(err, ErrKnowledgeEvalCaseNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

// TestWrongExpectationLowersTheRate runs the eval twice: once with a case the
// search can answer, then after adding a case that expects a kind no document
// has. The second run records a lower rate, and the digest raises the risk.
func TestWrongExpectationLowersTheRate(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{Kind: "adr", Title: "Storage", Content: adrBody, Publish: true})
	member := memberActor(e)

	if _, err := e.svc.CreateEvalCase(ctx, e.ws, member, EvalCaseInput{Question: "partitioned tables", ExpectedKind: "adr"}); err != nil {
		t.Fatal(err)
	}
	first, err := e.svc.RunEval(ctx, e.ws, time.Now())
	if err != nil || first.State != KnowledgeStateReady || first.Hits != 1 || first.HitRate != 1 {
		t.Fatalf("first run = %+v, %v", first, err)
	}

	if _, err := e.svc.CreateEvalCase(ctx, e.ws, member, EvalCaseInput{Question: "partitioned tables", ExpectedKind: "policy"}); err != nil {
		t.Fatal(err)
	}
	second, err := e.svc.RunEval(ctx, e.ws, time.Now())
	if err != nil || second.Total != 2 || second.Hits != 1 || second.HitRate != 0.5 {
		t.Fatalf("second run = %+v, %v", second, err)
	}
	if second.Results[1].Hit || second.Results[0].Rank != 1 {
		t.Fatalf("per-case results = %+v", second.Results)
	}

	runs, err := e.q.ListKnowledgeEvalRuns(ctx, db.ListKnowledgeEvalRunsParams{WorkspaceID: e.ws, Limit: 2})
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs = %d, %v", len(runs), err)
	}
	risk := retrievalHealth(runs)
	if risk == nil || !risk.Dropped || risk.Risk == "" || risk.HitRate != 0.5 {
		t.Fatalf("digest retrieval = %+v", risk)
	}
}

func TestOfflineEvalRecordsNothing(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{Kind: "adr", Title: "Storage", Content: adrBody, Publish: true})
	if _, err := e.svc.CreateEvalCase(ctx, e.ws, memberActor(e), EvalCaseInput{Question: "partitioned tables", ExpectedKind: "adr"}); err != nil {
		t.Fatal(err)
	}
	e.fake.SetUnavailable(true)
	run, err := e.svc.RunEval(ctx, e.ws, time.Now())
	e.fake.ClearFailures()
	if err != nil || run.State != KnowledgeStateOffline {
		t.Fatalf("offline run = %+v, %v", run, err)
	}
	if runs, _ := e.svc.ListEvalRuns(ctx, e.ws, 10); len(runs) != 0 {
		t.Fatalf("an offline run was recorded: %d", len(runs))
	}
}

func TestEvalWithNoCasesRecordsNothing(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	run, err := e.svc.RunEval(context.Background(), e.ws, time.Now())
	if err != nil || run.Total != 0 || run.ID != "" {
		t.Fatalf("empty run = %+v, %v", run, err)
	}
}
