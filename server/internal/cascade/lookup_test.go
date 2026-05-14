package cascade

import "testing"

func TestLookupIssueIdentifier_TitleMatches(t *testing.T) {
	cases := []struct {
		name, title, want string
	}{
		{"basic", "[PUL-102] feat(x): y", "PUL-102"},
		{"lowercase", "[pul-99] fix(z): w", "PUL-99"},
		{"mul prefix", "[MUL-1] foo", "MUL-1"},
		{"long prefix", "[PROJECT-12345] thing", "PROJECT-12345"},
		{"deep reference not matched", "fix typo in PUL-99 ref", ""},
		{"no brackets", "PUL-12 feat: x", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LookupIssueIdentifier(tc.title, "")
			if got != tc.want {
				t.Errorf("LookupIssueIdentifier(%q, \"\") = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestLookupIssueIdentifier_BranchFallback(t *testing.T) {
	cases := []struct {
		name, branch, want string
	}{
		{"basic agent branch", "agent-1/pul-102-foo", "PUL-102"},
		{"multi-digit agent", "agent-2/pul-99-bar", "PUL-99"},
		{"alphanumeric agent id", "agent-a1b/MUL-7-thing", "MUL-7"},
		{"slash slug", "agent-1/pul-1/x", "PUL-1"},
		{"underscore separator", "agent-3/pul-42_xy", "PUL-42"},
		{"feat branch not matched (not agent-)", "feat/pul-7-foo", ""},
		{"main not matched", "main", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LookupIssueIdentifier("", tc.branch)
			if got != tc.want {
				t.Errorf("LookupIssueIdentifier(\"\", %q) = %q, want %q", tc.branch, got, tc.want)
			}
		})
	}
}

func TestLookupIssueIdentifier_TitlePrefersBranch(t *testing.T) {
	// When both match, title wins (primary). Pin this so a future
	// refactor doesn't accidentally swap priority.
	got := LookupIssueIdentifier("[PUL-1] feat", "agent-1/pul-2-x")
	if got != "PUL-1" {
		t.Errorf("title should be primary: got %q, want PUL-1", got)
	}
}

func TestLookupIssueIdentifier_TitleEditedBranchSurvives(t *testing.T) {
	// G4 scenario: user edited the title and dropped [PUL-N]; branch
	// is the fallback that keeps the lookup working.
	got := LookupIssueIdentifier("now without prefix", "agent-1/pul-42-x")
	if got != "PUL-42" {
		t.Errorf("branch fallback failed: got %q, want PUL-42", got)
	}
}

func TestInScope(t *testing.T) {
	cases := []struct {
		name, title, branch string
		want                bool
	}{
		{"agent branch alone", "manual title", "agent-1/x", true},
		{"bracket title alone", "[PUL-1] x", "feat/y", true},
		{"both present", "[PUL-1] x", "agent-2/pul-1-x", true},
		{"manual PR", "fix login", "feat/login-redirect", false},
		{"empty inputs", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InScope(tc.title, tc.branch); got != tc.want {
				t.Errorf("InScope(%q, %q) = %v, want %v", tc.title, tc.branch, got, tc.want)
			}
		})
	}
}
