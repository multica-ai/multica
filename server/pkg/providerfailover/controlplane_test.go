package providerfailover

import "testing"

func TestEffectKey_Deterministic(t *testing.T) {
	t.Parallel()
	a := EffectKey("chain-1", EffectTaskSpawn, "child-9")
	b := EffectKey("chain-1", EffectTaskSpawn, "child-9")
	if a == "" || a != b {
		t.Fatalf("same inputs must yield the same non-empty key, got %q and %q", a, b)
	}
}

func TestEffectKey_DistinctInputsDiffer(t *testing.T) {
	t.Parallel()
	keys := map[string]bool{}
	inputs := []struct {
		chain  string
		effect ControlPlaneEffect
		target string
	}{
		{"chain-1", EffectTaskSpawn, "child-9"},
		{"chain-2", EffectTaskSpawn, "child-9"},     // different chain
		{"chain-1", EffectStagePromotion, "child-9"}, // different effect
		{"chain-1", EffectTaskSpawn, "child-8"},      // different target
		// Delimiter-injection guard: naive concatenation of these two would
		// collide with one of the above, the length/NUL separation must not.
		{"chain-1x", EffectTaskSpawn, "child-9"},
		{"chain-1", EffectTaskSpawn, "xchild-9"},
	}
	for _, in := range inputs {
		k := EffectKey(in.chain, in.effect, in.target)
		if k == "" {
			t.Fatalf("unexpected empty key for %+v", in)
		}
		if keys[k] {
			t.Fatalf("key collision for %+v", in)
		}
		keys[k] = true
	}
}

func TestEffectKey_UnkeyableInputsEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		chain  string
		effect ControlPlaneEffect
		target string
	}{
		{"", EffectTaskSpawn, "child-9"},
		{"chain-1", "", "child-9"},
		{"chain-1", EffectTaskSpawn, ""},
		{"  ", EffectTaskSpawn, "child-9"}, // whitespace-only trims to empty
	}
	for _, c := range cases {
		if k := EffectKey(c.chain, c.effect, c.target); k != "" {
			t.Errorf("unkeyable effect %+v should yield empty key, got %q", c, k)
		}
	}
}
