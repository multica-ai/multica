package agentoffice

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidSemver(t *testing.T) {
	for _, s := range []string{"1.0.0", "0.0.1", "10.20.30"} {
		if !ValidSemver(s) {
			t.Errorf("ValidSemver(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"1.0", "v1.0.0", "1.0.0-rc1", "", "a.b.c"} {
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
		{"10.0.0", "2.0.0", true}, // length-aware, not lexicographic
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"bad", "1.0.0", false},
	}
	for _, c := range cases {
		if got := SemverGT(c.a, c.b); got != c.want {
			t.Errorf("SemverGT(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestBumpPatch(t *testing.T) {
	cases := map[string]string{
		"1.0.0":   "1.0.1",
		"2.3.9":   "2.3.10",
		"0.0.0":   "0.0.1",
		"garbage": "1.0.0",
	}
	for in, want := range cases {
		if got := BumpPatch(in); got != want {
			t.Errorf("BumpPatch(%q) = %q, want %q", in, got, want)
		}
		if want != "1.0.0" && !SemverGT(BumpPatch(in), in) {
			t.Errorf("BumpPatch(%q) must be > input", in)
		}
	}
}

func TestBuildRollbackProposal(t *testing.T) {
	// A rollback is a PROPOSED change: the derived proposed version must be
	// strictly greater than current so it satisfies the propose-flow rule, and
	// the title must name the target version for an auditable review.
	p := BuildRollbackProposal("1.4.2", "1.2.0", "revert bad prompt")
	if p.ProposedVersion != "1.4.3" {
		t.Errorf("ProposedVersion = %q, want 1.4.3 (next patch above current)", p.ProposedVersion)
	}
	if !SemverGT(p.ProposedVersion, "1.4.2") {
		t.Errorf("proposed %q must be > current 1.4.2", p.ProposedVersion)
	}
	if p.Title != "Rollback to 1.2.0" {
		t.Errorf("Title = %q, want %q", p.Title, "Rollback to 1.2.0")
	}
	if p.Description != "revert bad prompt" {
		t.Errorf("Description = %q, want the supplied comment", p.Description)
	}

	// An empty comment falls back to a descriptive default rather than blank.
	d := BuildRollbackProposal("2.0.0", "1.9.0", "   ")
	if d.Description != "Roll back to 1.9.0" {
		t.Errorf("empty comment should default the description, got %q", d.Description)
	}
	if d.ProposedVersion != "2.0.1" {
		t.Errorf("ProposedVersion = %q, want 2.0.1", d.ProposedVersion)
	}
}

func TestEncodeDecodeSnapshotRoundTrip(t *testing.T) {
	s := ContextSnapshot{
		Instructions:  "be helpful\nfollow the gates",
		Description:   "a test agent",
		Model:         "claude-opus-4-8",
		ThinkingLevel: "high",
		McpConfig:     json.RawMessage(`{"servers":["a"]}`),
		SkillIDs:      []string{"id-1", "id-2"},
		CustomEnvKeys: []string{"FOO", "BAR"},
	}
	out := DecodeSnapshot(EncodeSnapshot(s))
	if out.Instructions != s.Instructions || out.Model != s.Model || out.ThinkingLevel != s.ThinkingLevel {
		t.Errorf("scalar fields did not round-trip: %+v", out)
	}
	if len(out.SkillIDs) != 2 || out.SkillIDs[0] != "id-1" {
		t.Errorf("skill ids did not round-trip: %+v", out.SkillIDs)
	}
}

func TestEncodeSnapshotNeverNil(t *testing.T) {
	if b := EncodeSnapshot(ContextSnapshot{}); len(b) == 0 {
		t.Error("EncodeSnapshot returned empty bytes for zero snapshot")
	}
}

func TestCustomEnvKeysSortedNoValues(t *testing.T) {
	keys := customEnvKeys([]byte(`{"ZED":"secret","ALPHA":"x","MID":"y"}`))
	want := []string{"ALPHA", "MID", "ZED"}
	if len(keys) != len(want) {
		t.Fatalf("got %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("key[%d] = %q, want %q (must be sorted)", i, keys[i], want[i])
		}
	}
	// Ensure no secret values leak into the rendering.
	for _, k := range keys {
		if strings.Contains(k, "secret") {
			t.Errorf("custom env value leaked into key: %q", k)
		}
	}
}

func TestCustomEnvKeysMalformed(t *testing.T) {
	if got := customEnvKeys([]byte("not json")); len(got) != 0 {
		t.Errorf("malformed env should yield empty keys, got %v", got)
	}
	if got := customEnvKeys(nil); len(got) != 0 {
		t.Errorf("nil env should yield empty keys, got %v", got)
	}
}

func TestDiffSnapshots(t *testing.T) {
	base := ContextSnapshot{Instructions: "line one\nline two", Model: "m1"}
	proposed := ContextSnapshot{Instructions: "line one\nline two changed", Model: "m2"}

	diff := DiffSnapshots(base, proposed)
	if diff == "" {
		t.Fatal("expected a non-empty diff")
	}
	if !strings.Contains(diff, "+model: m2") || !strings.Contains(diff, "-model: m1") {
		t.Errorf("diff missing model change:\n%s", diff)
	}
	if !strings.Contains(diff, "+line two changed") {
		t.Errorf("diff missing instruction change:\n%s", diff)
	}

	// Identical snapshots produce no diff.
	if d := DiffSnapshots(base, base); d != "" {
		t.Errorf("identical snapshots should diff to empty, got:\n%s", d)
	}
}
