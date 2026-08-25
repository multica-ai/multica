package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRecordPlanLimitsSharesOnlyBuiltInProviderRuntimes(t *testing.T) {
	t.Parallel()

	used := 37.0
	d := &Daemon{runtimeIndex: map[string]Runtime{
		"codex-a":      {ID: "codex-a", Provider: "codex"},
		"codex-b":      {ID: "codex-b", Provider: "codex"},
		"codex-custom": {ID: "codex-custom", Provider: "codex", ProfileID: "profile-1"},
		"claude-a":     {ID: "claude-a", Provider: "claude"},
	}}
	d.recordPlanLimits("codex-a", &protocol.PlanLimitsSnapshot{
		Provider:   "wrong-provider",
		Status:     protocol.PlanLimitsStatusAvailable,
		ObservedAt: 123,
		Windows: []protocol.PlanLimitWindow{{
			Name:        "primary",
			UsedPercent: &used,
		}},
	})

	for _, id := range []string{"codex-a", "codex-b"} {
		got := d.planLimitsForRuntime(id)
		if got == nil || got.Provider != "codex" || got.Windows[0].UsedPercent == nil || *got.Windows[0].UsedPercent != 37 {
			t.Fatalf("%s snapshot = %+v", id, got)
		}
	}
	for _, id := range []string{"codex-custom", "claude-a"} {
		if got := d.planLimitsForRuntime(id); got != nil {
			t.Fatalf("%s unexpectedly inherited snapshot: %+v", id, got)
		}
	}

	// Callers receive a deep copy and cannot mutate the next heartbeat.
	got := d.planLimitsForRuntime("codex-a")
	*got.Windows[0].UsedPercent = 99
	if next := d.planLimitsForRuntime("codex-a"); *next.Windows[0].UsedPercent != 37 {
		t.Fatalf("stored snapshot mutated through caller: %+v", next)
	}
}

func TestRecordPlanLimitsKeepsCustomProfileIsolated(t *testing.T) {
	t.Parallel()

	d := &Daemon{runtimeIndex: map[string]Runtime{
		"custom-a": {ID: "custom-a", Provider: "codex", ProfileID: "profile-a"},
		"custom-b": {ID: "custom-b", Provider: "codex", ProfileID: "profile-b"},
	}}
	d.recordPlanLimits("custom-a", &protocol.PlanLimitsSnapshot{
		Provider:   "codex",
		Status:     protocol.PlanLimitsStatusExhausted,
		ObservedAt: 123,
	})

	if got := d.planLimitsForRuntime("custom-a"); got == nil {
		t.Fatal("source custom runtime lost its snapshot")
	}
	if got := d.planLimitsForRuntime("custom-b"); got != nil {
		t.Fatalf("other custom runtime inherited snapshot: %+v", got)
	}
}
