package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
)

func TestKnowledgeIssueQueryUsesTitleAndDescriptionOnOneLine(t *testing.T) {
	cases := []struct {
		name, title, description, want string
	}{
		{"title only", "  Fix login  ", "", "Fix login"},
		{"title and description", "Fix login", "  The\nbutton   does nothing.  ", "Fix login The button does nothing."},
		{"no title means no query", "   ", "a description", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := knowledgeIssueQuery(tc.title, tc.description); got != tc.want {
				t.Fatalf("query = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKnowledgeIssueQueryIsCapped(t *testing.T) {
	query := knowledgeIssueQuery("Anvils", strings.Repeat("coyote ", 400))
	if got := utf8.RuneCountInString(query); got != knowledgeIssueQueryRunes+1 {
		t.Fatalf("query has %d runes, want the cap plus an ellipsis", got)
	}
	if !strings.HasSuffix(query, "…") {
		t.Fatalf("a capped query must say so: %q", query[len(query)-8:])
	}
}

func TestSelectIssueContextKeepsTheBestPassagesThatClearTheFloor(t *testing.T) {
	hits := []KnowledgeHitView{
		{Title: "below the floor", Score: 0.49},
		{Title: "second", Score: 0.7},
		{Title: "best", Score: 0.9},
		{Title: "third", Score: 0.6},
		{Title: "fourth, cut by the limit", Score: 0.55},
	}
	got := selectIssueContext(hits)
	if len(got) != knowledgeIssueContextLimit {
		t.Fatalf("kept %d passages, want %d: %+v", len(got), knowledgeIssueContextLimit, got)
	}
	for i, want := range []string{"best", "second", "third"} {
		if got[i].Title != want {
			t.Fatalf("passage %d = %q, want %q", i, got[i].Title, want)
		}
	}
}

func TestSelectIssueContextKeepsAScoreExactlyAtTheFloor(t *testing.T) {
	got := selectIssueContext([]KnowledgeHitView{{Title: "at the floor", Score: knowledgeIssueContextMinScore}})
	if len(got) != 1 {
		t.Fatalf("a score at the floor must be kept: %+v", got)
	}
}

func TestSelectIssueContextIsEmptyWhenNothingIsRelevant(t *testing.T) {
	// Retrieval returns weak matches for an unrelated issue; none may reach the run.
	if got := selectIssueContext([]KnowledgeHitView{{Title: "weak", Score: 0.1}, {Title: "weaker", Score: 0.4}}); len(got) != 0 {
		t.Fatalf("unrelated passages must not reach the run: %+v", got)
	}
}

func TestIssueContextWithoutKnowledgeIsEmptyAndMakesNoCall(t *testing.T) {
	// A service with no knowledge client is "not offered": the retrieval fails
	// before touching the database, and the run gets nothing.
	ws := mustUUID(t, "00000000-0000-0000-0000-000000000001")
	if got := (&KnowledgeService{}).IssueContext(context.Background(), ws, "Fix login", "The button"); got != nil {
		t.Fatalf("an unoffered deployment must add no context: %+v", got)
	}
}

func TestIssueContextWithoutATitleSkipsRetrieval(t *testing.T) {
	var none *KnowledgeService
	if got := none.IssueContext(context.Background(), pgtype.UUID{}, "", "a description"); got != nil {
		t.Fatalf("an untitled issue has nothing to search for: %+v", got)
	}
}

func TestKnowledgeIssueContextAttachesPublishedPassagesOnly(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	// The Fake matches every query word in a passage's title or content, so
	// "anvil" and "coyote" select these three passages. The draft is written
	// by an agent and stays unpublished, so it must not reach the run.
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "market", Title: "Anvil pricing", Content: "Anvils cost coyote dollars.", Publish: true})
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "product", Title: "Anvil roadmap", Content: "Anvil roadmap for coyotes.", Publish: true})
	e.mustCreate(agentActor(e), KnowledgeDocumentInput{KB: "market", Title: "Anvil draft", Content: "Anvil draft notes about coyotes."})

	hits := e.svc.IssueContext(context.Background(), e.ws, "Anvil", "coyote")
	if len(hits) != 2 {
		t.Fatalf("hits = %+v, want the two published passages", hits)
	}
	for _, hit := range hits {
		if hit.Title == "Anvil draft" {
			t.Fatal("a draft must not reach an issue run")
		}
	}
}

func TestKnowledgeIssueContextCapsAtTheRunLimit(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	for _, title := range []string{"Anvil one", "Anvil two", "Anvil three", "Anvil four"} {
		e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "market", Title: title, Content: "Anvil notes.", Publish: true})
	}
	if hits := e.svc.IssueContext(context.Background(), e.ws, "Anvil", "notes"); len(hits) != knowledgeIssueContextLimit {
		t.Fatalf("a run receives %d passages, want %d: %+v", len(hits), knowledgeIssueContextLimit, hits)
	}
}

func TestKnowledgeIssueContextFailuresLeaveTheRunWithoutContext(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	e.mustCreate(memberActor(e), KnowledgeDocumentInput{KB: "market", Title: "Anvil pricing", Content: "Anvils cost coyote dollars.", Publish: true})

	e.fake.FailAlways(knowledge.OpSearch, errors.New("sidecar down"))
	if hits := e.svc.IssueContext(context.Background(), e.ws, "Anvil", "coyote"); hits != nil {
		t.Fatalf("a failed retrieval must add nothing: %+v", hits)
	}
	e.fake.ClearFailures()

	// A budget already spent is a failure too: the retrieval gives up and the
	// run proceeds.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if hits := e.svc.IssueContext(ctx, e.ws, "Anvil", "coyote"); hits != nil {
		t.Fatalf("a cancelled retrieval must add nothing: %+v", hits)
	}
}

func TestKnowledgeIssueContextIsEmptyBeforeEnabling(t *testing.T) {
	e := newKnowledgeEnv(t)
	if hits := e.svc.IssueContext(context.Background(), e.ws, "Anvil", "coyote"); hits != nil {
		t.Fatalf("a workspace without knowledge must add nothing: %+v", hits)
	}
}
