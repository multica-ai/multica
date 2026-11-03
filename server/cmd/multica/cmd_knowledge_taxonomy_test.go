package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestKnowledgeAddNeedsAKBOrAKindWithADefaultBase(t *testing.T) {
	noKB := knowledgeAddInput{Title: "Storage", Text: "x"}
	if _, err := noKB.content(); err == nil || !strings.Contains(err.Error(), "--kb is required") {
		t.Fatalf("no kb and no kind: err = %v", err)
	}
	withKind := knowledgeAddInput{Title: "Storage", Text: "x", Kind: "adr"}
	if _, err := withKind.content(); err != nil {
		t.Fatalf("a kind may stand in for --kb: %v", err)
	}
}

func TestKnowledgeAddTaxonomyFieldsAreSentOnlyWhenSet(t *testing.T) {
	body := map[string]any{"kb": "market"}
	addTaxonomyFields(body, knowledgeAddInput{
		Kind: "adr", Initiative: "11111111-1111-1111-1111-111111111111", InitiativeFolder: "INI-3-invoicing",
		Confidence: "high", ValidUntil: "2027-01-31", Supersedes: "doc-9", Tags: []string{"billing"},
	})
	want := map[string]any{
		"kb": "market", "kind": "adr", "initiative_id": "11111111-1111-1111-1111-111111111111",
		"initiative_folder": "INI-3-invoicing", "confidence": "high", "valid_until": "2027-01-31",
		"supersedes_id": "doc-9", "tags": []string{"billing"},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %#v", body)
	}

	empty := map[string]any{}
	addTaxonomyFields(empty, knowledgeAddInput{Kind: "  "})
	if len(empty) != 0 {
		t.Fatalf("blank flags were sent: %#v", empty)
	}
}

func TestSummarizeHealthListsTheCounts(t *testing.T) {
	report := map[string]any{"counts": map[string]any{"review_due": 2.0, "missing_kind": 1.0}}
	if got := summarizeHealth(report); got != "missing_kind 1, review_due 2" {
		t.Fatalf("summary = %q", got)
	}
	if got := summarizeHealth(map[string]any{"counts": map[string]any{}}); got != "nothing to report" {
		t.Fatalf("empty = %q", got)
	}
	if got := summarizeHealth("not a report"); got != "" {
		t.Fatalf("non-object = %q", got)
	}
}
