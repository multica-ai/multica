package featureflag

import (
	"context"
	"testing"
)

func TestStaticProviderDefault(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("flag_a", Rule{Default: true})
	sp.Set("flag_b", Rule{Default: false})

	d, ok := sp.Lookup(context.Background(), "flag_a")
	if !ok || !d.Enabled || d.Reason != ReasonStatic {
		t.Fatalf("flag_a should be statically enabled, got %+v ok=%v", d, ok)
	}
	d, ok = sp.Lookup(context.Background(), "flag_b")
	if !ok || d.Enabled || d.Reason != ReasonStatic {
		t.Fatalf("flag_b should be statically disabled, got %+v ok=%v", d, ok)
	}
	_, ok = sp.Lookup(context.Background(), "missing")
	if ok {
		t.Fatalf("missing flag must report not-found")
	}
}

func TestStaticProviderAllowAndDeny(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("internal_feature", Rule{
		Default: false,
		Allow:   []string{"user-internal"},
		Deny:    []string{"user-banned"},
	})

	allowCtx := WithEvalContext(context.Background(), EvalContext{UserID: "user-internal"})
	d, _ := sp.Lookup(allowCtx, "internal_feature")
	if !d.Enabled {
		t.Fatalf("allowlisted user must see the flag enabled")
	}

	denyCtx := WithEvalContext(context.Background(), EvalContext{UserID: "user-banned"})
	d, _ = sp.Lookup(denyCtx, "internal_feature")
	if d.Enabled {
		t.Fatalf("denylisted user must see the flag disabled")
	}

	otherCtx := WithEvalContext(context.Background(), EvalContext{UserID: "user-random"})
	d, _ = sp.Lookup(otherCtx, "internal_feature")
	if d.Enabled {
		t.Fatalf("everyone else should fall back to Default=false")
	}
}

func TestStaticProviderDenyWinsOverAllow(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("conflict", Rule{
		Default: false,
		Allow:   []string{"same-user"},
		Deny:    []string{"same-user"},
	})
	ctx := WithEvalContext(context.Background(), EvalContext{UserID: "same-user"})
	d, _ := sp.Lookup(ctx, "conflict")
	if d.Enabled {
		t.Fatalf("Deny must win over Allow")
	}
}

func TestStaticProviderPercentRolloutDeterministic(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("gradual", Rule{
		Default: false,
		Percent: &PercentRollout{Percent: 50},
	})

	// The same identifier must produce the same decision across many calls.
	ctx := WithEvalContext(context.Background(), EvalContext{UserID: "stable-user"})
	first, _ := sp.Lookup(ctx, "gradual")
	for i := 0; i < 100; i++ {
		d, _ := sp.Lookup(ctx, "gradual")
		if d.Enabled != first.Enabled {
			t.Fatalf("percent rollout flapped between calls: first=%v iter=%v", first, d)
		}
	}
}

func TestStaticProviderPercentRolloutDistribution(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("split", Rule{Percent: &PercentRollout{Percent: 50}})

	enabled := 0
	const N = 1000
	for i := 0; i < N; i++ {
		ctx := WithEvalContext(context.Background(), EvalContext{
			UserID: randomUserID(i),
		})
		d, _ := sp.Lookup(ctx, "split")
		if d.Enabled {
			enabled++
		}
	}
	// A 50% rollout over 1000 distinct users should land near 500.
	// We allow a generous +/- 100 window so the test is not flaky on
	// CI; the goal is to catch a misconfigured hash, not to validate
	// statistical properties of FNV.
	if enabled < 400 || enabled > 600 {
		t.Fatalf("50%% rollout produced %d/1000 enabled — distribution looks broken", enabled)
	}
}

func TestStaticProviderPercentRolloutBy(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("ws_rollout", Rule{Percent: &PercentRollout{Percent: 100, By: "workspace_id"}})

	// Percent=100 with By=workspace_id should always enable, even when
	// UserID is unset.
	ctx := WithEvalContext(context.Background(), EvalContext{WorkspaceID: "any-workspace"})
	d, _ := sp.Lookup(ctx, "ws_rollout")
	if !d.Enabled || d.Reason != ReasonPercent {
		t.Fatalf("100%% workspace rollout should always enable, got %+v", d)
	}
}

func TestStaticProviderPercentZero(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("off_for_everyone", Rule{Percent: &PercentRollout{Percent: 0}})
	ctx := WithEvalContext(context.Background(), EvalContext{UserID: "anyone"})
	d, _ := sp.Lookup(ctx, "off_for_everyone")
	if d.Enabled {
		t.Fatalf("0%% rollout must disable everyone")
	}
}

func TestStaticProviderLoadRulesAtomic(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("old", Rule{Default: true})
	sp.LoadRules(map[string]Rule{
		"new": {Default: true},
	})
	if _, ok := sp.Lookup(context.Background(), "old"); ok {
		t.Fatalf("LoadRules must replace, not merge, the rule map")
	}
	if d, ok := sp.Lookup(context.Background(), "new"); !ok || !d.Enabled {
		t.Fatalf("LoadRules failed to install new rule, got %+v ok=%v", d, ok)
	}
}

func TestStaticProviderKeysSorted(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("zeta", Rule{})
	sp.Set("alpha", Rule{})
	sp.Set("mu", Rule{})

	keys := sp.Keys()
	want := []string{"alpha", "mu", "zeta"}
	if len(keys) != len(want) {
		t.Fatalf("expected %d keys, got %d", len(want), len(keys))
	}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("keys not sorted: %v", keys)
		}
	}
}

func TestStaticProviderCustomAttribute(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("plan_gate", Rule{
		Default: false,
		Allow:   []string{"enterprise"},
		AllowBy: "plan",
	})
	ctx := WithEvalContext(context.Background(), EvalContext{
		UserID:     "anyone",
		Attributes: map[string]string{"plan": "enterprise"},
	})
	d, _ := sp.Lookup(ctx, "plan_gate")
	if !d.Enabled {
		t.Fatalf("plan=enterprise should pass allowlist, got %+v", d)
	}
}

// TestStaticProviderVariantOnlyWhenEnabled is the regression test for the
// review feedback from MUL-3615: a Rule with Variant="experiment-v2" but
// enabled=false (deny match, percent miss, default-off) MUST surface
// Variant="off", not the on-variant. Otherwise a caller branching on
// Variant() would route control users into the experiment arm.
func TestStaticProviderVariantOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("exp", Rule{
		Default: false,
		Variant: "experiment-v2",
		Deny:    []string{"banned-user"},
		Percent: &PercentRollout{Percent: 0}, // 0% rollout: nobody is in.
	})

	for _, userID := range []string{"banned-user", "random-user", ""} {
		ctx := WithEvalContext(context.Background(), EvalContext{UserID: userID})
		d, _ := sp.Lookup(ctx, "exp")
		if d.Enabled {
			t.Fatalf("user=%q must be disabled at 0%% rollout, got %+v", userID, d)
		}
		if d.Variant != "off" {
			t.Fatalf("user=%q got Variant=%q, want %q — on-variant must not leak when disabled",
				userID, d.Variant, "off")
		}
	}
}

func TestStaticProviderVariantWhenEnabled(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("exp", Rule{
		Default: false,
		Variant: "experiment-v2",
		Allow:   []string{"rolled-in-user"},
	})
	ctx := WithEvalContext(context.Background(), EvalContext{UserID: "rolled-in-user"})
	d, _ := sp.Lookup(ctx, "exp")
	if !d.Enabled || d.Variant != "experiment-v2" {
		t.Fatalf("enabled user should see the on-variant, got %+v", d)
	}
}

// randomUserID returns a stable user identifier derived from i. It exists
// so the rollout distribution test is deterministic across runs (no rand).
func randomUserID(i int) string {
	// Use a base-26 spread so adjacent ids differ in multiple bytes,
	// which exercises the hash better than a numeric suffix.
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	buf := []byte{
		alphabet[(i/676)%26],
		alphabet[(i/26)%26],
		alphabet[i%26],
		'-',
		byte('0' + (i % 10)),
	}
	return string(buf)
}
