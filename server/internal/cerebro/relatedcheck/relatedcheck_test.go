package relatedcheck

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/duplicatecheck"
)

func TestSearchTermsDropsFillerAndShortWords(t *testing.T) {
	got := SearchTerms("Hvordan kan vi gøre så alle agenter tjekker om der er et issue på det samme?", "")
	joined := strings.Join(got, ",")
	for _, unwanted := range []string{"alle", "samme", "hvordan"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("stopword %q survived: %v", unwanted, got)
		}
	}
	// "agenter" and "issue" are the nouns that make this issue findable.
	if !strings.Contains(joined, "agenter") || !strings.Contains(joined, "issue") {
		t.Fatalf("expected the meaningful nouns, got %v", got)
	}
}

func TestSearchTermsFallsBackToDescription(t *testing.T) {
	got := SearchTerms("Related", "The duplicate detection should run when an agent is assigned.\nsecond line\nthird line\nfourth line")
	if len(got) < 2 {
		t.Fatalf("expected description terms to be mined, got %v", got)
	}
	if strings.Contains(strings.Join(got, ","), "fourth") {
		t.Fatalf("only the first lines of the description should be mined, got %v", got)
	}
}

func TestSearchTermsIsCappedAndDeduped(t *testing.T) {
	got := SearchTerms("alpha alpha bravo charlie delta echo foxtrot golf hotel india juliett", "")
	if len(got) > maxTerms {
		t.Fatalf("expected at most %d terms, got %d: %v", maxTerms, len(got), got)
	}
	seen := map[string]bool{}
	for _, term := range got {
		if seen[term] {
			t.Fatalf("duplicate term %q in %v", term, got)
		}
		seen[term] = true
	}
}

func TestSearchTermsEmitsOnlyLettersAndDigits(t *testing.T) {
	// The candidate query interpolates terms into ILIKE patterns, so a term
	// carrying % or _ would silently widen the search.
	got := SearchTerms("100% broken_state -- drop%table", "")
	for _, term := range got {
		if strings.ContainsAny(term, "%_-' \"") {
			t.Fatalf("term %q carries a LIKE metacharacter: %v", term, got)
		}
	}
}

func TestSelectMatchesDropsUnrelatedAndPutsDuplicatesFirst(t *testing.T) {
	candidates := []Candidate{
		{ID: "a", Identifier: "FIR-1", Title: "First", Status: "todo"},
		{ID: "b", Identifier: "FIR-2", Title: "Second", Status: "in_progress"},
		{ID: "c", Identifier: "FIR-3", Title: "Third", Status: "todo"},
	}
	judged := []duplicatecheck.JudgedCandidate{
		{ID: "a", Verdict: duplicatecheck.VerdictRelated, Reason: "same area"},
		{ID: "b", Verdict: duplicatecheck.VerdictUnrelated},
		{ID: "c", Verdict: duplicatecheck.VerdictDuplicate, Reason: "same sag"},
	}

	got := selectMatches(candidates, judged, 5)
	if len(got) != 2 {
		t.Fatalf("expected the unrelated candidate to be dropped, got %d: %+v", len(got), got)
	}
	if got[0].ID != "c" || got[0].Verdict != "duplicate" {
		t.Fatalf("expected the duplicate first, got %+v", got[0])
	}
	if got[1].ID != "a" || got[1].Verdict != "related" {
		t.Fatalf("expected the related match second, got %+v", got[1])
	}
}

func TestSelectMatchesRespectsTheLimitAndIgnoresUnknownIDs(t *testing.T) {
	candidates := []Candidate{{ID: "a"}, {ID: "b"}}
	judged := []duplicatecheck.JudgedCandidate{
		{ID: "a", Verdict: duplicatecheck.VerdictRelated},
		{ID: "b", Verdict: duplicatecheck.VerdictRelated},
		{ID: "ghost", Verdict: duplicatecheck.VerdictDuplicate},
	}
	got := selectMatches(candidates, judged, 1)
	if len(got) != 1 {
		t.Fatalf("expected the limit to apply, got %d", len(got))
	}
	for _, m := range got {
		if m.ID == "ghost" {
			t.Fatal("a verdict for an ID we never sent must be ignored")
		}
	}
}

func TestRenderCommentStaysSilentWithoutNewLinks(t *testing.T) {
	// A repeat check on an already-linked issue must not comment again.
	res := Result{Matches: []Match{{ID: "a", Identifier: "FIR-1", Verdict: "related"}}, NewLinks: 0}
	if body := RenderComment(res); body != "" {
		t.Fatalf("expected silence on a repeat check, got %q", body)
	}
	if body := RenderComment(Result{}); body != "" {
		t.Fatalf("expected silence with no matches, got %q", body)
	}
}

func TestRenderCommentFlagsDuplicatesAndLinksIssues(t *testing.T) {
	dup := Match{ID: "dup-id", Identifier: "FIR-1", Title: "Same thing", Status: "in_progress", Verdict: "duplicate", Reason: "Both ask for the same check.", Linked: true}
	res := Result{Matches: []Match{dup}, Duplicate: &dup, NewLinks: 1}

	body := RenderComment(res)
	if !strings.Contains(body, "duplicate") {
		t.Fatalf("expected the duplicate wording, got %q", body)
	}
	if !strings.Contains(body, "[FIR-1](mention://issue/dup-id)") {
		t.Fatalf("expected a clickable issue mention, got %q", body)
	}
	if !strings.Contains(body, "Both ask for the same check.") {
		t.Fatalf("expected the judge reason to be shown, got %q", body)
	}
	if !strings.Contains(body, "before starting work") {
		t.Fatalf("expected the stop-and-ask nudge on a duplicate, got %q", body)
	}
}

func TestRenderCommentOmitsTheNudgeWhenOnlyRelated(t *testing.T) {
	res := Result{
		Matches:  []Match{{ID: "a", Identifier: "FIR-2", Title: "Nearby", Status: "todo", Verdict: "related", Linked: true}},
		NewLinks: 1,
	}
	body := RenderComment(res)
	if strings.Contains(body, "before starting work") {
		t.Fatalf("a related-only result must not tell the agent to stop, got %q", body)
	}
	if !strings.Contains(body, "Related work already exists.") {
		t.Fatalf("expected the related heading, got %q", body)
	}
}

func TestFormatIdentifierFallsBackToTheNumber(t *testing.T) {
	if got := FormatIdentifier("FIR", 4183); got != "FIR-4183" {
		t.Fatalf("got %q", got)
	}
	if got := FormatIdentifier("  ", 12); got != "#12" {
		t.Fatalf("expected a bare number when the workspace has no prefix, got %q", got)
	}
}

func TestSummaryReportsSkipReasons(t *testing.T) {
	if got := Summary(Result{Skipped: "no judge configured"}); !strings.Contains(got, "no judge configured") {
		t.Fatalf("got %q", got)
	}
	if got := Summary(Result{Candidates: 7}); !strings.Contains(got, "7 candidates") {
		t.Fatalf("got %q", got)
	}
}
