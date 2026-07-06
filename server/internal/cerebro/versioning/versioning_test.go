package versioning

import (
	"net/http"
	"strings"
	"testing"
)

func TestValidSemver(t *testing.T) {
	valid := []string{"1.0.0", "0.0.1", "10.20.30"}
	invalid := []string{"", "1.0", "1.0.0.0", "v1.0.0", "1.0.0-beta", "a.b.c"}
	for _, s := range valid {
		if !ValidSemver(s) {
			t.Errorf("ValidSemver(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if ValidSemver(s) {
			t.Errorf("ValidSemver(%q) = true, want false", s)
		}
	}
}

func TestSemverGT(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.1.0", "1.0.9", true},
		{"2.0.0", "1.9.9", true},
		{"1.0.10", "1.0.2", true}, // length-first compare: 10 > 2
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"bogus", "1.0.0", false},
		{"1.0.0", "bogus", false},
	}
	for _, c := range cases {
		if got := SemverGT(c.a, c.b); got != c.want {
			t.Errorf("SemverGT(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestBumpPatch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.0.0", "1.0.1"},
		{"2.3.9", "2.3.10"},
		{"bogus", "1.0.0"},
	}
	for _, c := range cases {
		if got := BumpPatch(c.in); got != c.want {
			t.Errorf("BumpPatch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUnifiedDiff(t *testing.T) {
	if d := UnifiedDiff("same\n", "same\n", "x"); d != "" {
		t.Errorf("identical inputs should produce empty diff, got %q", d)
	}
	d := UnifiedDiff("a\nb\nc\n", "a\nB\nc\n", "entity")
	if !strings.Contains(d, "--- entity (base)") || !strings.Contains(d, "+++ entity (proposed)") {
		t.Errorf("diff missing header: %q", d)
	}
	if !strings.Contains(d, "-b\n") || !strings.Contains(d, "+B\n") {
		t.Errorf("diff missing change lines: %q", d)
	}
	if !strings.Contains(d, " a\n") || !strings.Contains(d, " c\n") {
		t.Errorf("diff missing context lines: %q", d)
	}
	// Pure addition / removal.
	if d := UnifiedDiff("", "new\n", "x"); !strings.Contains(d, "+new\n") {
		t.Errorf("addition diff wrong: %q", d)
	}
	if d := UnifiedDiff("old\n", "", "x"); !strings.Contains(d, "-old\n") {
		t.Errorf("removal diff wrong: %q", d)
	}
}

func TestStatusForMergeError(t *testing.T) {
	if got := StatusForMergeError(ErrStaleProposal); got != http.StatusConflict {
		t.Errorf("stale: got %d", got)
	}
	if got := StatusForMergeError(ErrNotPending); got != http.StatusConflict {
		t.Errorf("not pending: got %d", got)
	}
	if got := StatusForMergeError(http.ErrBodyNotAllowed); got != http.StatusInternalServerError {
		t.Errorf("other: got %d", got)
	}
}
