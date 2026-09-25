package featureflag

import (
	"context"
	"testing"
)

func TestChainProviderFirstHitWins(t *testing.T) {
	t.Parallel()
	a := NewStaticProvider()
	a.Set("shared", Rule{Default: true})

	b := NewStaticProvider()
	b.Set("shared", Rule{Default: false})

	chain := NewChainProvider(a, b)
	d, ok := chain.Lookup(context.Background(), "shared")
	if !ok || !d.Enabled {
		t.Fatalf("first provider must win, got %+v ok=%v", d, ok)
	}
}

func TestChainProviderFallsThrough(t *testing.T) {
	t.Parallel()
	a := NewStaticProvider() // empty
	b := NewStaticProvider()
	b.Set("only_in_b", Rule{Default: true})

	chain := NewChainProvider(a, b)
	d, ok := chain.Lookup(context.Background(), "only_in_b")
	if !ok || !d.Enabled {
		t.Fatalf("chain must fall through to the next provider, got %+v ok=%v", d, ok)
	}
}

func TestChainProviderEmpty(t *testing.T) {
	t.Parallel()
	chain := NewChainProvider()
	_, ok := chain.Lookup(context.Background(), "any")
	if ok {
		t.Fatalf("empty chain must report not-found")
	}
}

func TestChainProviderSkipsNil(t *testing.T) {
	t.Parallel()
	sp := NewStaticProvider()
	sp.Set("real", Rule{Default: true})

	chain := NewChainProvider(nil, sp, nil)
	d, ok := chain.Lookup(context.Background(), "real")
	if !ok || !d.Enabled {
		t.Fatalf("chain must skip nil providers, got %+v ok=%v", d, ok)
	}
}

func TestChainProviderEnvBeatsStatic(t *testing.T) {
	t.Parallel()
	// This is the production-shaped chain: env override on top, static
	// config below. An Ops engineer flipping FF_KILL_SWITCH=false must
	// be able to disable a flag that is otherwise true in static config.
	static := NewStaticProvider()
	static.Set("kill_switch", Rule{Default: true})

	env := newMockEnv(map[string]string{"FF_KILL_SWITCH": "false"})

	chain := NewChainProvider(env, static)
	d, _ := chain.Lookup(context.Background(), "kill_switch")
	if d.Enabled {
		t.Fatalf("env override must beat static default, got %+v", d)
	}
}
