package capabilities

import "testing"

func consequenceByField(t *testing.T, impact ApprovalImpact, field string) FieldConsequence {
	t.Helper()
	for _, row := range impact.Fields {
		if row.Field == field {
			return row
		}
	}
	t.Fatalf("field %q not present in impact; got %+v", field, impact.Fields)
	return FieldConsequence{}
}

func TestApprovalImpactUnknownProviderNeverClaimsIneffective(t *testing.T) {
	impact := ApprovalImpactFor("not-a-provider", []string{"instructions", "model"})
	if impact.Status != StatusUnknown {
		t.Fatalf("status = %q, want %q", impact.Status, StatusUnknown)
	}
	if len(impact.Fields) != 0 || len(impact.Ineffective) != 0 {
		t.Fatalf("unknown provider must not enumerate consequences, got %+v", impact)
	}
}

func TestApprovalImpactNoChangedFieldsIsKnownAndEmpty(t *testing.T) {
	impact := ApprovalImpactFor("claude", nil)
	if impact.Status != StatusKnown {
		t.Fatalf("status = %q, want %q", impact.Status, StatusKnown)
	}
	if len(impact.Fields) != 0 {
		t.Fatalf("no changed fields must yield no consequences, got %+v", impact.Fields)
	}
}

// claude honours every field in the matrix: approving a proposal there changes
// the agent for real, which is the baseline the ineffective cases contrast with.
func TestApprovalImpactOnClaudeEveryChangeTakesEffect(t *testing.T) {
	impact := ApprovalImpactFor("claude", ApprovalSnapshotFields())
	if impact.Status != StatusKnown {
		t.Fatalf("status = %q, want %q", impact.Status, StatusKnown)
	}
	for _, row := range impact.Fields {
		// description is a label; it reaches no run on any engine.
		if row.Field == "description" {
			continue
		}
		if row.Consequence != ConsequenceTakesEffect {
			t.Fatalf("field %s on claude: consequence = %q, want %q", row.Field, row.Consequence, ConsequenceTakesEffect)
		}
	}
	if len(impact.Ineffective) != 0 {
		t.Fatalf("claude must not report ineffective fields, got %v", impact.Ineffective)
	}
}

// The case the field diff cannot show, and the reason this slice exists: hermes
// discards the system prompt (hermes.go:72). A proposal that rewrites the
// agent's instructions renders as a big green diff and changes NOTHING at
// runtime. The approver must be told before they approve it.
func TestApprovalImpactInstructionsOnHermesHaveNoEffect(t *testing.T) {
	impact := ApprovalImpactFor("hermes", []string{"instructions"})

	row := consequenceByField(t, impact, "instructions")
	if row.Consequence != ConsequenceNoEffectLogged {
		t.Fatalf("instructions on hermes: consequence = %q, want %q", row.Consequence, ConsequenceNoEffectLogged)
	}
	if row.DeliveredBy != DeliveryEngine {
		t.Fatalf("instructions delivered_by = %q, want %q", row.DeliveredBy, DeliveryEngine)
	}
	if !contains(impact.Ineffective, "instructions") {
		t.Fatalf("instructions must be listed as ineffective, got %v", impact.Ineffective)
	}
	// hermes logs the discard, so it is not the silent class.
	if contains(impact.SilentlyIneffective, "instructions") {
		t.Fatalf("hermes logs the discard; instructions must not be silently ineffective, got %v", impact.SilentlyIneffective)
	}
}

// codex has no field for a deny-policy, so it drops custom_args-adjacent
// settings we do not send it at all. The silent class is what the warning
// hierarchy ranks on: nothing in the log tells the approver it did not land.
func TestApprovalImpactSilentDropIsRankedAboveLoggedDrop(t *testing.T) {
	// openclaw has no MCP entry in the matrix → absent means ignored_silent.
	impact := ApprovalImpactFor("openclaw", []string{"mcp_config"})

	row := consequenceByField(t, impact, "mcp_config")
	if row.Consequence != ConsequenceNoEffectSilent {
		t.Fatalf("mcp_config on openclaw: consequence = %q, want %q", row.Consequence, ConsequenceNoEffectSilent)
	}
	if !row.Silent {
		t.Fatal("mcp_config on openclaw must be marked silent")
	}
	if !contains(impact.SilentlyIneffective, "mcp_config") {
		t.Fatalf("mcp_config must be silently ineffective, got %v", impact.SilentlyIneffective)
	}
	if !contains(impact.Ineffective, "mcp_config") {
		t.Fatalf("silently ineffective must also be ineffective, got %v", impact.Ineffective)
	}
}

// Multica applies the skill bundle and the secret names itself,
// around the engine process. Those consequences hold on every engine — marking
// them "engine honours it" would credit the engine for our own enforcement, and
// marking them ineffective on a prepend-only engine would be a lie.
func TestApprovalImpactMulticaEnforcedFieldsTakeEffectOnEveryEngine(t *testing.T) {
	for _, provider := range []string{"claude", "hermes", "openclaw"} {
		impact := ApprovalImpactFor(provider, []string{"skill_ids", "custom_env_keys"})
		for _, field := range []string{"skill_ids", "custom_env_keys"} {
			row := consequenceByField(t, impact, field)
			if row.Consequence != ConsequenceTakesEffect {
				t.Fatalf("%s on %s: consequence = %q, want %q", field, provider, row.Consequence, ConsequenceTakesEffect)
			}
			if row.DeliveredBy != DeliveryMultica {
				t.Fatalf("%s on %s: delivered_by = %q, want %q", field, provider, row.DeliveredBy, DeliveryMultica)
			}
		}
	}
}

// A description change is honest about doing nothing: it never reaches a run on
// any engine. Silently classing it as "takes effect" would tell an approver a
// behaviour change is coming when none is.
func TestApprovalImpactDescriptionHasNoRuntimeEffect(t *testing.T) {
	impact := ApprovalImpactFor("claude", []string{"description"})

	row := consequenceByField(t, impact, "description")
	if row.Consequence != ConsequenceNoRuntimeEffect {
		t.Fatalf("description: consequence = %q, want %q", row.Consequence, ConsequenceNoRuntimeEffect)
	}
	if row.DeliveredBy != DeliveryNone {
		t.Fatalf("description: delivered_by = %q, want %q", row.DeliveredBy, DeliveryNone)
	}
	// It reaches no run, but it is not the engine dropping a setting — keeping it
	// out of Ineffective stops the UI warning about an engine that is blameless.
	if contains(impact.Ineffective, "description") {
		t.Fatalf("description must not be reported as an engine drop, got %v", impact.Ineffective)
	}
}

// An instruction change on a prepend-only engine LANDS but loses its authority.
// That is neither "takes effect" nor "no effect", so it gets its own sentence —
// the same reason the swap slice gives the system prompt its own transition.
func TestApprovalImpactInstructionsOnPrependEngineReportPromptDelivery(t *testing.T) {
	impact := ApprovalImpactFor("kiro", []string{"instructions"})

	if impact.SystemPrompt == nil {
		t.Fatal("instructions change on a prepend engine must report prompt delivery")
	}
	if impact.SystemPrompt.Native {
		t.Fatal("kiro has no native system-prompt channel; Native must be false")
	}
	if impact.SystemPrompt.Delivery != PromptDeliveryPrepended {
		t.Fatalf("delivery = %q, want %q", impact.SystemPrompt.Delivery, PromptDeliveryPrepended)
	}
	// It still lands, so the field itself takes effect.
	row := consequenceByField(t, impact, "instructions")
	if row.Consequence != ConsequenceTakesEffect {
		t.Fatalf("instructions on kiro: consequence = %q, want %q", row.Consequence, ConsequenceTakesEffect)
	}
}

// The prompt sentence is only warranted when the proposal actually touches the
// prompt. A model-only change on a prepend engine must not warn about prompt
// semantics that this approval does not alter.
func TestApprovalImpactPromptDeliveryOnlyWhenPromptFieldsChange(t *testing.T) {
	impact := ApprovalImpactFor("kiro", []string{"model"})
	if impact.SystemPrompt != nil {
		t.Fatalf("prompt delivery must not be reported for a model-only change, got %+v", impact.SystemPrompt)
	}
}

// system_prompt_mode is its own labelled snapshot field, so a prompt delivery
// change does not hide inside a generic runtime_config row.
func TestApprovalImpactSystemPromptModeReportsPromptDelivery(t *testing.T) {
	impact := ApprovalImpactFor("claude", []string{"system_prompt_mode"})
	if impact.SystemPrompt == nil {
		t.Fatal("system_prompt_mode must report prompt delivery")
	}
	if impact.SystemPrompt.Delivery != PromptDeliveryNative {
		t.Fatalf("delivery on claude = %q, want %q", impact.SystemPrompt.Delivery, PromptDeliveryNative)
	}
}

func TestApprovalImpactClassifiesVersionedRuntimeSettings(t *testing.T) {
	impact := ApprovalImpactFor("claude", []string{"runtime_id", "speed_mode", "max_turns", "timeout_minutes"})
	for _, field := range []string{"runtime_id", "speed_mode", "max_turns", "timeout_minutes"} {
		row := consequenceByField(t, impact, field)
		if row.Consequence != ConsequenceTakesEffect {
			t.Fatalf("%s consequence = %q, want %q", field, row.Consequence, ConsequenceTakesEffect)
		}
	}
}

// An unrecognised snapshot key must not be silently dropped: a caller asking
// about a field we cannot classify has to see that we cannot classify it,
// otherwise the panel quietly under-reports the proposal's scope.
func TestApprovalImpactUnknownFieldIsReportedNotDropped(t *testing.T) {
	impact := ApprovalImpactFor("claude", []string{"a_field_we_do_not_know"})

	row := consequenceByField(t, impact, "a_field_we_do_not_know")
	if row.Consequence != ConsequenceUnknownField {
		t.Fatalf("unknown field: consequence = %q, want %q", row.Consequence, ConsequenceUnknownField)
	}
	if contains(impact.Ineffective, "a_field_we_do_not_know") {
		t.Fatalf("an unclassifiable field must not be reported as an engine drop, got %v", impact.Ineffective)
	}
}

func TestApprovalImpactFieldsAreDeduplicatedAndOrdered(t *testing.T) {
	impact := ApprovalImpactFor("claude", []string{"model", "instructions", "model"})
	if len(impact.Fields) != 2 {
		t.Fatalf("duplicate keys must collapse, got %+v", impact.Fields)
	}
	if impact.Fields[0].Field != "instructions" || impact.Fields[1].Field != "model" {
		t.Fatalf("fields must be sorted by name, got %+v", impact.Fields)
	}
}
