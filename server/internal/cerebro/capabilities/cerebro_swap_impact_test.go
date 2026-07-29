package capabilities

import "testing"

func fieldByName(t *testing.T, impact SwapImpact, field string) FieldTransition {
	t.Helper()
	for _, row := range impact.Fields {
		if row.Field == field {
			return row
		}
	}
	t.Fatalf("field %q not present in impact; got %+v", field, impact.Fields)
	return FieldTransition{}
}

func TestSwapImpactUnknownProviderNeverClaimsUnsupported(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{"claude", "not-a-provider"},
		{"not-a-provider", "claude"},
	} {
		impact := SwapImpactFor(tc.from, tc.to)
		if impact.Status != StatusUnknown {
			t.Fatalf("swap %s→%s: status = %q, want %q", tc.from, tc.to, impact.Status, StatusUnknown)
		}
		if len(impact.Fields) != 0 || len(impact.Lost) != 0 {
			t.Fatalf("swap %s→%s: unknown must not enumerate losses, got %+v", tc.from, tc.to, impact)
		}
	}
}

func TestSwapImpactSameProviderChangesNothing(t *testing.T) {
	impact := SwapImpactFor("claude", "claude")
	if impact.Status != StatusKnown {
		t.Fatalf("status = %q, want %q", impact.Status, StatusKnown)
	}
	if len(impact.Lost) != 0 || len(impact.Gained) != 0 {
		t.Fatalf("same provider must lose/gain nothing, got lost=%v gained=%v", impact.Lost, impact.Gained)
	}
	for _, row := range impact.Fields {
		if row.Outcome == OutcomeLost || row.Outcome == OutcomeGained {
			t.Fatalf("field %s: outcome = %q on identity swap", row.Field, row.Outcome)
		}
	}
}

// claude honours every field in the matrix; kiro is prepend-only argv backend.
// This is the mockup's case (M2: the same agent moved to Kiro).
func TestSwapImpactClaudeToKiroReportsLostFields(t *testing.T) {
	impact := SwapImpactFor("claude", "kiro")
	if impact.Status != StatusKnown {
		t.Fatalf("status = %q, want %q", impact.Status, StatusKnown)
	}

	tools := fieldByName(t, impact, "disallowed_tools")
	if tools.Outcome != OutcomeLost {
		t.Fatalf("disallowed_tools: outcome = %q, want %q", tools.Outcome, OutcomeLost)
	}
	// The deny-policy is the case FIR-3212 exists for: it is dropped with no
	// trace, so the swap must mark it as a SILENT loss, not a plain one.
	if !tools.SilentOnTarget {
		t.Fatalf("disallowed_tools: SilentOnTarget = false; a silently dropped deny-policy must be flagged")
	}
	if !contains(impact.Lost, "disallowed_tools") {
		t.Fatalf("Lost = %v, want disallowed_tools", impact.Lost)
	}

	model := fieldByName(t, impact, "model")
	if model.Outcome != OutcomeRetained {
		t.Fatalf("model: outcome = %q, want %q", model.Outcome, OutcomeRetained)
	}
}

func TestSwapImpactFlagsSystemPromptSemanticsDowngrade(t *testing.T) {
	impact := SwapImpactFor("claude", "kiro")
	sp := impact.SystemPrompt
	if sp == nil {
		t.Fatal("SystemPrompt transition missing for a known→known swap")
	}
	if !sp.FromNative || sp.ToNative {
		t.Fatalf("native: from = %v, to = %v; want true→false", sp.FromNative, sp.ToNative)
	}
	if sp.Outcome != PromptOutcomeDowngradedToPrepend {
		t.Fatalf("outcome = %q, want %q", sp.Outcome, PromptOutcomeDowngradedToPrepend)
	}
}

func TestSwapImpactFlagsSystemPromptFullyIgnored(t *testing.T) {
	// gemini ignores a system prompt entirely (systemPromptSupport: Modes nil).
	impact := SwapImpactFor("claude", "gemini")
	sp := impact.SystemPrompt
	if sp == nil {
		t.Fatal("SystemPrompt transition missing")
	}
	if sp.Outcome != PromptOutcomeIgnored {
		t.Fatalf("outcome = %q, want %q", sp.Outcome, PromptOutcomeIgnored)
	}
}

func TestSwapImpactReportsGainedFields(t *testing.T) {
	impact := SwapImpactFor("kiro", "claude")
	tools := fieldByName(t, impact, "disallowed_tools")
	if tools.Outcome != OutcomeGained {
		t.Fatalf("disallowed_tools: outcome = %q, want %q", tools.Outcome, OutcomeGained)
	}
	if !contains(impact.Gained, "disallowed_tools") {
		t.Fatalf("Gained = %v, want disallowed_tools", impact.Gained)
	}
	sp := impact.SystemPrompt
	if sp == nil || sp.Outcome != PromptOutcomeGainedNative {
		t.Fatalf("system prompt outcome = %+v, want %q", sp, PromptOutcomeGainedNative)
	}
}

func TestSwapImpactFieldsAreStablySorted(t *testing.T) {
	impact := SwapImpactFor("claude", "kiro")
	for i := 1; i < len(impact.Fields); i++ {
		if impact.Fields[i-1].Field >= impact.Fields[i].Field {
			t.Fatalf("fields not sorted: %q before %q", impact.Fields[i-1].Field, impact.Fields[i].Field)
		}
	}
}

func TestSwapImpactIncludesVersionedRuntimeSettings(t *testing.T) {
	impact := SwapImpactFor("claude", "kiro")
	if got := fieldByName(t, impact, "speed_mode").Outcome; got != OutcomeLost {
		t.Fatalf("speed_mode outcome = %q, want %q", got, OutcomeLost)
	}
	if got := fieldByName(t, impact, "max_turns").Outcome; got != OutcomeLost {
		t.Fatalf("max_turns outcome = %q, want %q", got, OutcomeLost)
	}
	if got := fieldByName(t, impact, "timeout_minutes").Outcome; got != OutcomeRetained {
		t.Fatalf("timeout_minutes outcome = %q, want %q", got, OutcomeRetained)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
