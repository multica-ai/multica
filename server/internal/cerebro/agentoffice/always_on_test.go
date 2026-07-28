package agentoffice

import (
	"strings"
	"testing"
)

// FIR-3805 — an always-on id is only meaningful for a skill that is still
// bound. These pin the normalisation that keeps the two lists consistent, and
// the review rendering that makes a flip visible on its own line.

func TestNormalizeAlwaysOnSkillsDropsUnboundIDs(t *testing.T) {
	got := NormalizeAlwaysOnSkills(ContextSnapshot{
		SkillIDs:         []string{"a", "b"},
		AlwaysOnSkillIDs: []string{"a", "c"},
	})
	if len(got.AlwaysOnSkillIDs) != 1 || got.AlwaysOnSkillIDs[0] != "a" {
		t.Fatalf("expected only the bound id to survive, got %v", got.AlwaysOnSkillIDs)
	}
}

func TestNormalizeAlwaysOnSkillsDedupesAndOrdersLikeSkillIDs(t *testing.T) {
	got := NormalizeAlwaysOnSkills(ContextSnapshot{
		SkillIDs:         []string{"a", "b", "c"},
		AlwaysOnSkillIDs: []string{"c", "a", "a"},
	})
	want := []string{"a", "c"}
	if len(got.AlwaysOnSkillIDs) != len(want) {
		t.Fatalf("expected %v, got %v", want, got.AlwaysOnSkillIDs)
	}
	for i := range want {
		if got.AlwaysOnSkillIDs[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got.AlwaysOnSkillIDs)
		}
	}
}

// Two snapshots describing the same state must compare equal byte-for-byte, or
// the direct-edit recorder cuts a new version for a no-op edit.
func TestNormalizeAlwaysOnSkillsMakesEquivalentSnapshotsEncodeEqual(t *testing.T) {
	a := NormalizeAlwaysOnSkills(ContextSnapshot{
		SkillIDs:         []string{"a", "b"},
		AlwaysOnSkillIDs: []string{"b", "a"},
	})
	b := NormalizeAlwaysOnSkills(ContextSnapshot{
		SkillIDs:         []string{"a", "b"},
		AlwaysOnSkillIDs: []string{"a", "b"},
	})
	if string(EncodeSnapshot(a)) != string(EncodeSnapshot(b)) {
		t.Fatalf("equivalent snapshots encoded differently:\n%s\n%s", EncodeSnapshot(a), EncodeSnapshot(b))
	}
}

func TestNormalizeAlwaysOnSkillsEmptyStaysNil(t *testing.T) {
	got := NormalizeAlwaysOnSkills(ContextSnapshot{SkillIDs: []string{"a"}})
	if got.AlwaysOnSkillIDs != nil {
		t.Fatalf("expected nil, got %v", got.AlwaysOnSkillIDs)
	}
}

// A reviewer must be able to see "this skill became always-on" as its own
// change, not buried inside the skills line.
func TestRenderSnapshotShowsAlwaysOnSkillsOnItsOwnLine(t *testing.T) {
	out := RenderSnapshot(ContextSnapshot{
		SkillIDs:         []string{"a", "b"},
		AlwaysOnSkillIDs: []string{"a"},
	})
	if !strings.Contains(out, "always_on_skills: a\n") {
		t.Fatalf("expected an always_on_skills line, got:\n%s", out)
	}
}

func TestDiffSnapshotsSurfacesAnAlwaysOnFlip(t *testing.T) {
	base := ContextSnapshot{SkillIDs: []string{"a"}}
	proposed := ContextSnapshot{SkillIDs: []string{"a"}, AlwaysOnSkillIDs: []string{"a"}}
	if diff := DiffSnapshots(base, proposed); !strings.Contains(diff, "always_on_skills") {
		t.Fatalf("expected the diff to mention always_on_skills, got:\n%s", diff)
	}
}

// Snapshots written before FIR-3805 have no always_on_skill_ids key at all;
// decoding one must not fail or invent a flag.
func TestDecodeSnapshotWithoutAlwaysOnKey(t *testing.T) {
	snap := DecodeSnapshot([]byte(`{"skill_ids":["a","b"]}`))
	if len(snap.SkillIDs) != 2 {
		t.Fatalf("expected the bound skills to survive, got %v", snap.SkillIDs)
	}
	if snap.AlwaysOnSkillIDs != nil {
		t.Fatalf("expected no always-on ids, got %v", snap.AlwaysOnSkillIDs)
	}
}
