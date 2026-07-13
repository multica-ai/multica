package note

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
var t1 = time.Date(2026, 7, 7, 13, 0, 0, 0, time.UTC)

func seed(body, actor string) []LineAttr {
	return AdvanceLineAttrs("", nil, body, actor, t0)
}

func TestAdvanceLineAttrs_SeedAttributesEveryLine(t *testing.T) {
	attrs := seed("a\nb\nc", "alice")
	if len(attrs) != 3 {
		t.Fatalf("want 3 attrs, got %d", len(attrs))
	}
	for i, a := range attrs {
		if a.CreatedBy != "alice" || a.UpdatedBy != "alice" {
			t.Fatalf("line %d not attributed to alice: %+v", i, a)
		}
	}
}

func TestAdvanceLineAttrs_EmptyBodyHasNoLines(t *testing.T) {
	if got := seed("", "alice"); len(got) != 0 {
		t.Fatalf("empty body must yield 0 attrs, got %d", len(got))
	}
}

func TestAdvanceLineAttrs_AppendKeepsExistingAttribution(t *testing.T) {
	attrs := seed("a\nb", "alice")
	next := AdvanceLineAttrs("a\nb", attrs, "a\nb\nc", "bob", t1)
	if len(next) != 3 {
		t.Fatalf("want 3 attrs, got %d", len(next))
	}
	if next[0].CreatedBy != "alice" || next[1].CreatedBy != "alice" {
		t.Fatalf("existing lines lost attribution: %+v", next)
	}
	if next[2].CreatedBy != "bob" || next[2].UpdatedBy != "bob" {
		t.Fatalf("new line not attributed to bob: %+v", next[2])
	}
}

func TestAdvanceLineAttrs_EditKeepsCreatorTracksEditor(t *testing.T) {
	attrs := seed("a\nb\nc", "alice")
	next := AdvanceLineAttrs("a\nb\nc", attrs, "a\nB!\nc", "bob", t1)
	if len(next) != 3 {
		t.Fatalf("want 3 attrs, got %d", len(next))
	}
	if next[1].CreatedBy != "alice" {
		t.Fatalf("modified line lost creator: %+v", next[1])
	}
	if next[1].UpdatedBy != "bob" {
		t.Fatalf("modified line must track editor bob: %+v", next[1])
	}
	if next[0].UpdatedBy != "alice" || next[2].UpdatedBy != "alice" {
		t.Fatalf("untouched lines must keep original attribution: %+v", next)
	}
}

func TestAdvanceLineAttrs_InsertInMiddle(t *testing.T) {
	attrs := seed("a\nb", "alice")
	next := AdvanceLineAttrs("a\nb", attrs, "a\nx\nb", "bob", t1)
	if len(next) != 3 {
		t.Fatalf("want 3 attrs, got %d", len(next))
	}
	if next[0].CreatedBy != "alice" || next[2].CreatedBy != "alice" {
		t.Fatalf("surrounding lines lost attribution: %+v", next)
	}
	if next[1].CreatedBy != "bob" {
		t.Fatalf("inserted line not attributed to bob: %+v", next[1])
	}
}

func TestAdvanceLineAttrs_DeleteShrinksAttrs(t *testing.T) {
	attrs := seed("a\nb\nc", "alice")
	next := AdvanceLineAttrs("a\nb\nc", attrs, "a\nc", "bob", t1)
	if len(next) != 2 {
		t.Fatalf("want 2 attrs, got %d", len(next))
	}
	if next[0].CreatedBy != "alice" || next[1].CreatedBy != "alice" {
		t.Fatalf("kept lines lost attribution: %+v", next)
	}
}

func TestAdvanceLineAttrs_ReplacedRunPairsPositionally(t *testing.T) {
	attrs := seed("head\nold1\nold2\ntail", "alice")
	next := AdvanceLineAttrs("head\nold1\nold2\ntail", attrs, "head\nnew1\nnew2\nnew3\ntail", "bob", t1)
	if len(next) != 5 {
		t.Fatalf("want 5 attrs, got %d", len(next))
	}
	// new1/new2 are modifies of old1/old2: creator stays alice, editor bob.
	for i := 1; i <= 2; i++ {
		if next[i].CreatedBy != "alice" || next[i].UpdatedBy != "bob" {
			t.Fatalf("line %d: want alice/bob, got %+v", i, next[i])
		}
	}
	// new3 exceeds the removed run: brand new by bob.
	if next[3].CreatedBy != "bob" {
		t.Fatalf("extra inserted line: want creator bob, got %+v", next[3])
	}
}

func TestAdvanceLineAttrs_UnknownActorLeavesUnknownAttribution(t *testing.T) {
	attrs := seed("a", "alice")
	next := AdvanceLineAttrs("a", attrs, "a\nmystery", "", t1)
	if len(next) != 2 {
		t.Fatalf("want 2 attrs, got %d", len(next))
	}
	if next[1].CreatedBy != "" || next[1].UpdatedBy != "" {
		t.Fatalf("unknown actor must record empty attribution: %+v", next[1])
	}
	if next[0].CreatedBy != "alice" {
		t.Fatalf("kept line lost attribution: %+v", next[0])
	}
}

func TestAdvanceLineAttrs_CorruptAttrsLengthTolerated(t *testing.T) {
	// Stored attrs shorter than the old body's line count must not panic and
	// must still attribute the edit.
	short := []LineAttr{{CreatedBy: "alice", UpdatedBy: "alice"}}
	next := AdvanceLineAttrs("a\nb\nc", short, "a\nb\nc\nd", "bob", t1)
	if len(next) != 4 {
		t.Fatalf("want 4 attrs, got %d", len(next))
	}
	if next[3].CreatedBy != "bob" {
		t.Fatalf("new line not attributed: %+v", next[3])
	}
}

func TestAdvanceLineAttrs_MovedBlockKeepsAttribution(t *testing.T) {
	attrs := seed("one\ntwo\nthree", "alice")
	// "three" moves up past "two" — LCS keeps the larger common subsequence.
	next := AdvanceLineAttrs("one\ntwo\nthree", attrs, "one\nthree\ntwo", "bob", t1)
	if len(next) != 3 {
		t.Fatalf("want 3 attrs, got %d", len(next))
	}
	if next[0].CreatedBy != "alice" {
		t.Fatalf("unchanged first line lost attribution: %+v", next[0])
	}
}

func TestParseLineAttrs_CorruptPayloadDegrades(t *testing.T) {
	if got := parseLineAttrs([]byte("not json")); got != nil {
		t.Fatalf("corrupt payload must yield nil, got %+v", got)
	}
	if got := parseLineAttrs(nil); got != nil {
		t.Fatalf("empty payload must yield nil, got %+v", got)
	}
}
