package prautomation

import "testing"

func TestSourcesHaveNoKeywordSemantics(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   int
	}{{"manual", 0}, {"title_branch", 2}, {"all", 4}} {
		got := Identifiers(Policy{Source: tc.source}, "Part of MUL-1", "mul-2/work", "Closes MUL-3; Related to MUL-4")
		if len(got) != tc.want {
			t.Fatalf("%s: got %v", tc.source, got)
		}
	}
	if got := Identifiers(Policy{Source: "all"}, "MUL-1", "mul-1", "fix MUL-1"); len(got) != 1 || got["MUL-1"] != "title" {
		t.Fatal(got)
	}
}
func TestCompletionIsAllMerged(t *testing.T) {
	policy := Policy{AutoComplete: true}
	for _, state := range []string{"open", "draft", "closed", "unknown", "merged"} {
		links := []Link{{PRID: "a", State: "merged", Connected: true}, {PRID: "b", State: state, Connected: true}}
		got := Decide(policy, false, false, false, links)
		if got.Complete != (state == "merged") {
			t.Fatalf("%s: %+v", state, got)
		}
	}
	if Decide(policy, false, false, false, nil).Complete {
		t.Fatal("empty links completed")
	}
	for _, tc := range []struct {
		terminal, triage, disabled, connected bool
		source, reason                        string
	}{
		{true, false, false, true, "manual", "terminal"}, {false, true, false, true, "title", "triage"},
		{false, false, true, true, "title", "issue_disabled"}, {false, false, false, false, "title", "sync_required"},
		{false, false, false, true, "pending", "sync_required"},
	} {
		got := Decide(policy, tc.terminal, tc.triage, tc.disabled, []Link{{State: "merged", Connected: tc.connected, Source: tc.source}})
		if got.Complete || got.Reason != tc.reason {
			t.Fatalf("%+v => %+v", tc, got)
		}
	}
}
