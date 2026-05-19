package skillindex

import (
	"encoding/json"
	"strings"
	"testing"
)

func firstEntry(t *testing.T) Entry {
	t.Helper()
	entries, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("skill index is empty")
	}
	return entries[0]
}

func recommendationJSON(t *testing.T, recs []Recommendation) []byte {
	t.Helper()
	data, err := json.Marshal(recs)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}

func TestNormalizeSkillFindResultValid(t *testing.T) {
	entry := firstEntry(t)
	raw := recommendationJSON(t, []Recommendation{{
		Name:        "  " + entry.Name + "  ",
		Description: entry.Description,
		SourceURL:   "  " + entry.SourceURL + "  ",
		Reason:      " Fits the requested React performance work. ",
	}})

	normalized, recs, err := NormalizeSkillFindResult(raw)
	if err != nil {
		t.Fatalf("NormalizeSkillFindResult() error = %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d recs, want 1", len(recs))
	}
	if recs[0].Name != entry.Name || recs[0].SourceURL != entry.SourceURL {
		t.Fatalf("recommendation was not normalized: %+v", recs[0])
	}
	if !json.Valid(normalized) {
		t.Fatalf("normalized output is not valid JSON: %s", normalized)
	}
}

func TestNormalizeSkillFindResultRejectsNonArray(t *testing.T) {
	if _, _, err := NormalizeSkillFindResult([]byte(`{"name":"x"}`)); err == nil {
		t.Fatal("expected non-array JSON to fail")
	}
}

func TestNormalizeSkillFindResultRejectsEmptyArray(t *testing.T) {
	if _, _, err := NormalizeSkillFindResult([]byte(`[]`)); err == nil {
		t.Fatal("expected empty array to fail")
	}
}

func TestNormalizeSkillFindResultRejectsUnknownSourceURL(t *testing.T) {
	raw := recommendationJSON(t, []Recommendation{{
		Name:      "unknown",
		SourceURL: "https://example.test/skills/unknown",
		Reason:    "Looks useful.",
	}})
	if _, _, err := NormalizeSkillFindResult(raw); err == nil {
		t.Fatal("expected unknown source_url to fail")
	}
}

func TestNormalizeSkillFindResultRejectsOverlongReason(t *testing.T) {
	entry := firstEntry(t)
	raw := recommendationJSON(t, []Recommendation{{
		Name:      entry.Name,
		SourceURL: entry.SourceURL,
		Reason:    strings.Repeat("x", MaxReasonRunes+1),
	}})
	if _, _, err := NormalizeSkillFindResult(raw); err == nil {
		t.Fatal("expected overlong reason to fail")
	}
}
