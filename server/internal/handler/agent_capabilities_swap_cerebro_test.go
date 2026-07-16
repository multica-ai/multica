package handler

// CEREBRO-PATCH(agent-capabilities-swap-test): FIR-3212 Swap slice — the swap
// preview card assembled from two runtime rows. DB-free, like the other
// capabilities-card tests: the matrix logic itself is covered in
// internal/cerebro/capabilities.

import (
	"testing"

	cerebrocapabilities "github.com/multica-ai/multica/server/internal/cerebro/capabilities"
)

func TestBuildAgentCapabilitySwap_ClaudeToKiro(t *testing.T) {
	got := buildAgentCapabilitySwap(
		"agent-1",
		runtimeExecOptionsFromProvider("claude", "1.2.3", "rt-claude"),
		runtimeExecOptionsFromProvider("kiro", "0.9.0", "rt-kiro"),
	)

	if got.AgentID != "agent-1" {
		t.Fatalf("AgentID = %q", got.AgentID)
	}
	if got.Impact.Status != cerebrocapabilities.StatusKnown {
		t.Fatalf("impact status = %q, want known", got.Impact.Status)
	}
	if got.Current.Provider != "claude" || got.Target.Provider != "kiro" {
		t.Fatalf("providers = %q→%q, want claude→kiro", got.Current.Provider, got.Target.Provider)
	}
	// The target's own field list must ride along: the Setup form renders the
	// destination's supported fields from this, without a second round-trip.
	if len(got.Target.ExecOptions) == 0 {
		t.Fatal("Target.ExecOptions is empty; the swap card must carry the destination field list")
	}
	if len(got.Impact.SilentlyLost) == 0 {
		t.Fatal("claude→kiro drops a deny-policy silently; SilentlyLost must say so")
	}
}

func TestBuildAgentCapabilitySwap_UnknownTargetIsNotATotalLoss(t *testing.T) {
	got := buildAgentCapabilitySwap(
		"agent-1",
		runtimeExecOptionsFromProvider("claude", "1.2.3", "rt-claude"),
		runtimeExecOptionsFromProvider("mystery-engine", "", "rt-x"),
	)

	if got.Impact.Status != cerebrocapabilities.StatusUnknown {
		t.Fatalf("impact status = %q, want unknown", got.Impact.Status)
	}
	if len(got.Impact.Lost) != 0 || len(got.Impact.Fields) != 0 {
		t.Fatalf("unknown target must enumerate nothing, got %+v", got.Impact)
	}
	if got.Target.Status != capStatusUnknown {
		t.Fatalf("target status = %q, want unknown", got.Target.Status)
	}
}

func TestBuildAgentCapabilitySwap_SameRuntimeIsNoOp(t *testing.T) {
	current := runtimeExecOptionsFromProvider("claude", "1.2.3", "rt-claude")
	got := buildAgentCapabilitySwap("agent-1", current, current)

	if !got.SameRuntime {
		t.Fatal("SameRuntime = false when target runtime id equals current")
	}
	if len(got.Impact.Lost) != 0 || len(got.Impact.Gained) != 0 {
		t.Fatalf("no-op swap must lose/gain nothing, got %+v", got.Impact)
	}
}
