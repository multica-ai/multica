package github

import (
	"reflect"
	"testing"
)

// TestStatusMapping pins the GitHub board Status <-> Multica status enum
// translation the inbound importer and outbound patcher both rely on.
// Two properties matter most:
//   - Unknown / empty board options downgrade to "backlog" rather than
//     crashing (the repo's enum-drift rule).
//   - "Approved" is a lossy fold into in_review; the raw value is kept in
//     metadata for push-back, so the fold itself must stay deterministic.
func TestStatusMapping(t *testing.T) {
	t.Parallel()

	toMultica := map[string]string{
		"Backlog":     "backlog",
		"To Do":       "todo",
		"In Progress": "in_progress",
		"In Review":   "in_review",
		"Approved":    "in_review", // lossy fold
		"Done":        "done",
		"Wont do":     "cancelled",
		" In Review ": "in_review", // surrounding whitespace tolerated
		"":            "backlog",   // unset -> downgrade, never crash
		"Frobnicated": "backlog",   // unknown -> downgrade, never crash
	}
	for gh, want := range toMultica {
		if got := MapStatusToMultica(gh); got != want {
			t.Errorf("MapStatusToMultica(%q) = %q; want %q", gh, got, want)
		}
	}

	toGitHub := map[string]struct {
		want string
		ok   bool
	}{
		"backlog":     {"Backlog", true},
		"todo":        {"To Do", true},
		"in_progress": {"In Progress", true},
		"in_review":   {"In Review", true},
		"done":        {"Done", true},
		"cancelled":   {"Wont do", true},
		"blocked":     {"In Progress", true}, // no board option; rides In Progress
		"bogus":       {"", false},
	}
	for multica, exp := range toGitHub {
		got, ok := MapStatusToGitHub(multica)
		if got != exp.want || ok != exp.ok {
			t.Errorf("MapStatusToGitHub(%q) = (%q,%v); want (%q,%v)", multica, got, ok, exp.want, exp.ok)
		}
	}
}

// TestPriorityMapping checks the P0..P3 <-> Multica priority translation,
// including the unmapped-defaults-to-none inbound rule.
func TestPriorityMapping(t *testing.T) {
	t.Parallel()

	in := map[string]string{
		"P0": "urgent",
		"P1": "high",
		"P2": "medium",
		"P3": "low",
		"":   "none",
		"P9": "none",
	}
	for gh, want := range in {
		if got := MapPriorityToMultica(gh); got != want {
			t.Errorf("MapPriorityToMultica(%q) = %q; want %q", gh, got, want)
		}
	}

	out := map[string]struct {
		want string
		ok   bool
	}{
		"urgent": {"P0", true},
		"high":   {"P1", true},
		"medium": {"P2", true},
		"low":    {"P3", true},
		"none":   {"", false},
	}
	for multica, exp := range out {
		got, ok := MapPriorityToGitHub(multica)
		if got != exp.want || ok != exp.ok {
			t.Errorf("MapPriorityToGitHub(%q) = (%q,%v); want (%q,%v)", multica, got, ok, exp.want, exp.ok)
		}
	}
}

func TestDeriveLabels(t *testing.T) {
	t.Parallel()

	it := Item{
		Labels: []string{"type:bug", "area:execution", " pod:dlt ", "type:bug"}, // dup + whitespace
		Fields: map[string]string{"Area": "AI", "Pod": "Agentic"},
	}
	got := DeriveLabels(it)
	want := []string{"area:ai", "area:execution", "pod:agentic", "pod:dlt", "type:bug"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeriveLabels = %v; want %v (deduped + sorted, board fields lower-cased)", got, want)
	}

	// No labels, no fields -> empty, never nil-panic downstream.
	if got := DeriveLabels(Item{}); len(got) != 0 {
		t.Errorf("DeriveLabels(empty) = %v; want empty", got)
	}
}

func TestPodFromItem(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		it   Item
		want string
	}{
		{"board field wins", Item{Fields: map[string]string{"Pod": "DLT"}, Labels: []string{"pod:studio"}}, "DLT"},
		{"label fallback canonicalized", Item{Labels: []string{"pod:dlt"}}, "DLT"},
		{"label fallback truetest", Item{Labels: []string{"POD:truetest"}}, "TrueTest"},
		{"unknown pod label -> empty", Item{Labels: []string{"pod:marketing"}}, ""},
		{"no pod -> empty (unrouted)", Item{Labels: []string{"area:ai"}}, ""},
	}
	for _, c := range cases {
		if got := PodFromItem(c.it); got != c.want {
			t.Errorf("%s: PodFromItem = %q; want %q", c.name, got, c.want)
		}
	}
}

func TestIssueType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		it   Item
		want string
	}{
		{"type label wins over title", Item{Labels: []string{"type:intent"}, Title: "[bug] x"}, "intent"},
		{"bracket prefix fallback", Item{Title: "[Bug] login broken"}, "bug"},
		{"intent bracket", Item{Title: "[intent] AI Runner"}, "intent"},
		{"no signal", Item{Title: "just a title"}, ""},
	}
	for _, c := range cases {
		if got := IssueType(c.it); got != c.want {
			t.Errorf("%s: IssueType = %q; want %q", c.name, got, c.want)
		}
	}
}

// TestMetadataFor verifies the issue.metadata stamp: gh_item_id is always
// present, optional fields appear only when populated, and the keys match
// the metadata contract (no empty values leaking through).
func TestMetadataFor(t *testing.T) {
	t.Parallel()

	full := Item{
		ItemID: "PVTI_abc",
		Repo:   "katalon-studio/product",
		Number: 271,
		URL:    "https://github.com/katalon-studio/product/issues/271",
		Labels: []string{"type:task", "pod:dlt"},
		Fields: map[string]string{
			"Status":      "In Progress",
			"Area":        "AI",
			"Intent Ref":  "MUL-42",
			"Target date": "2026-06-30",
		},
	}
	m := MetadataFor(full)
	want := map[string]string{
		"gh_item_id":  "PVTI_abc",
		"gh_repo":     "katalon-studio/product",
		"gh_number":   "271",
		"gh_url":      "https://github.com/katalon-studio/product/issues/271",
		"gh_status":   "In Progress",
		"area":        "ai",
		"pod":         "DLT",
		"issue_type":  "task",
		"intent_ref":  "MUL-42",
		"target_date": "2026-06-30",
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("MetadataFor(full) = %#v; want %#v", m, want)
	}

	// Minimal item: only gh_item_id, no empty-valued keys.
	min := MetadataFor(Item{ItemID: "PVTI_x"})
	if len(min) != 1 || min["gh_item_id"] != "PVTI_x" {
		t.Errorf("MetadataFor(minimal) = %#v; want only gh_item_id", min)
	}
	for k, v := range min {
		if v == "" {
			t.Errorf("MetadataFor leaked empty value for key %q", k)
		}
	}
}

func TestItoa(t *testing.T) {
	t.Parallel()

	cases := map[int]string{0: "0", 7: "7", 42: "42", 271: "271", -5: "-5", 1000000: "1000000"}
	for n, want := range cases {
		if got := itoa(n); got != want {
			t.Errorf("itoa(%d) = %q; want %q", n, got, want)
		}
	}
}
