package sessions

import "testing"

func TestResolveStartMode(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"", startModeHandoff, true},        // default carries the handoff forward
		{"handoff", startModeHandoff, true}, // explicit carry-over
		{"blank", startModeBlank, true},     // explicit blank session
		{"Handoff", "", false},              // case-sensitive: unknown
		{"none", "", false},                 // unknown value is rejected, not silently defaulted
		{"reset", "", false},                // unknown value is rejected
	}
	for _, c := range cases {
		got, ok := resolveStartMode(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("resolveStartMode(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestNormalizeHandoff(t *testing.T) {
	if got := normalizeHandoff(nil); got != nil {
		t.Fatalf("normalizeHandoff(nil) = %v, want nil", got)
	}
	plan := "  https://example.com/plan  "
	in := &handoffBrief{
		Summary:   "  did work  ",
		Done:      []string{" a ", "", "b"},
		Remaining: []string{"", "  "},
		PlanRef:   &plan,
	}
	got := normalizeHandoff(in)
	if got.Summary != "did work" {
		t.Errorf("summary = %q, want %q", got.Summary, "did work")
	}
	if len(got.Done) != 2 || got.Done[0] != "a" || got.Done[1] != "b" {
		t.Errorf("done = %v, want [a b]", got.Done)
	}
	if len(got.Remaining) != 0 {
		t.Errorf("remaining = %v, want []", got.Remaining)
	}
	if got.PlanRef == nil || *got.PlanRef != "https://example.com/plan" {
		t.Errorf("plan_ref = %v, want trimmed url", got.PlanRef)
	}
}

func TestSessionNameFromHandoff(t *testing.T) {
	// Auto-generated briefs (agentAuthored=false) never rename — keep "Session N".
	if got := sessionNameFromHandoff(&handoffBrief{Summary: "Auto-generated handoff: 3 comments."}, false); got != "" {
		t.Errorf("auto-generated brief named the session: %q", got)
	}
	if got := sessionNameFromHandoff(nil, true); got != "" {
		t.Errorf("nil brief = %q, want empty", got)
	}
	if got := sessionNameFromHandoff(&handoffBrief{Summary: "   "}, true); got != "" {
		t.Errorf("blank summary = %q, want empty", got)
	}
	// Agent-authored: first line only, becomes the session title.
	if got := sessionNameFromHandoff(&handoffBrief{Summary: "Shipped login redirect fix\nmore detail"}, true); got != "Shipped login redirect fix" {
		t.Errorf("name = %q, want first line only", got)
	}
	// Long summaries are capped to a title length.
	long := "This is a very long handoff summary that goes well beyond sixty characters and keeps going"
	got := sessionNameFromHandoff(&handoffBrief{Summary: long}, true)
	if len([]rune(got)) > 61 { // 60 runes + ellipsis
		t.Errorf("name not capped: %q (%d runes)", got, len([]rune(got)))
	}
}

func TestSnippet(t *testing.T) {
	if got := snippet("line one\nline two", 100); got != "line one line two" {
		t.Errorf("snippet collapse = %q", got)
	}
	if got := snippet("abcdefghij", 5); got != "abcde…" {
		t.Errorf("snippet truncate = %q, want abcde…", got)
	}
}

func TestHandoffBody_PreservesNewlinesAndCaps(t *testing.T) {
	// FIR-1839 B: unlike snippet, the handoff body keeps internal newlines so the
	// opened handoff is readable in full.
	if got := handoffBody("line one\nline two", 100); got != "line one\nline two" {
		t.Errorf("handoffBody newlines = %q, want preserved", got)
	}
	if got := handoffBody("  padded \n end  ", 100); got != "padded \n end" {
		t.Errorf("handoffBody edge-trim = %q", got)
	}
	if got := handoffBody("abcdefghij", 5); got != "abcde…" {
		t.Errorf("handoffBody cap = %q, want abcde…", got)
	}
}
