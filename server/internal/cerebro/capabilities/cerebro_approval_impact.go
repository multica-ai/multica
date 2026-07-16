package capabilities

import (
	"sort"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// Approval consequences (FIR-3212, Approval slice).
//
// cerebro_swap_impact.go answers "what do I lose if I move this agent to another
// engine?". This file answers the question asked on the approval screen, where
// the engine is not changing at all: "if I approve this proposal, what actually
// changes about what this agent does?"
//
// The change-request queue already renders a field diff, and a field diff is
// where the approver's information stops. It shows that `instructions` went from
// one blob of text to another. It cannot show that this agent runs on hermes,
// which discards the system prompt outright (hermes.go:72) — so the approver
// reads a large, confident green diff, approves it, and the agent's behaviour is
// byte-for-byte unchanged. Nothing in the UI ever told them. That is the failure
// this file exists to stop: a proposal can be perfectly formed, correctly
// versioned, properly approved, and still be a no-op.
//
// The mapping from a snapshot key to a runtime consequence is the whole point,
// and it must live here rather than in the browser for the same reason the swap
// diff does: the vocabulary (silent vs logged drop, native vs prepended prompt)
// is only meaningful next to the matrix that is tested against the installed
// CLI. A copy of it in the frontend is a copy nobody can check.
//
// It inherits the StaticCatalog contract: an unknown provider yields
// StatusUnknown and enumerates NOTHING. "We cannot say whether this lands" must
// never be served as "this will not land" — that would talk an owner out of
// approving a change that works perfectly.

// Delivery says WHO acts on a field, which decides whether the engine's opinion
// of it is even relevant.
const (
	// DeliveryEngine — the value is handed to the engine, so the engine decides
	// whether it lands. These are the rows the matrix governs.
	DeliveryEngine = "engine"
	// DeliveryMultica — Multica applies it around the engine process (the
	// sandbox, the skill bundle, the injected secret names). It lands on every
	// engine, and crediting the engine for it would be wrong.
	DeliveryMultica = "multica"
	// DeliveryNone — the field reaches no run at all; it is bookkeeping.
	DeliveryNone = "none"
)

// Consequences of approving a change to one field.
const (
	// ConsequenceTakesEffect — approving this changes the agent's next run.
	ConsequenceTakesEffect = "takes_effect"
	// ConsequenceNoEffectLogged — the engine drops it and says so in the log.
	// Approving changes nothing, but the evidence exists after the fact.
	ConsequenceNoEffectLogged = "no_effect_logged"
	// ConsequenceNoEffectSilent — the engine drops it with no trace. This is the
	// worst class and the one the UI must rank first: the approver has no way to
	// discover the change did not land, so they keep believing it did.
	ConsequenceNoEffectSilent = "no_effect_silent"
	// ConsequenceNoRuntimeEffect — the field never reaches a run on any engine
	// (the agent's description). Not the engine's fault, so it is deliberately
	// NOT reported as a drop — warning about a blameless engine trains the
	// approver to ignore the panel.
	ConsequenceNoRuntimeEffect = "no_runtime_effect"
	// ConsequenceUnknownField — a snapshot key this file has no mapping for.
	// Reported rather than skipped: silently omitting a changed field would make
	// the panel under-report the proposal's scope, which is the one thing an
	// approval screen may not do.
	ConsequenceUnknownField = "unknown_field"
)

// How the agent's instructions reach the model after the change.
const (
	// PromptDeliveryNative — a real system-prompt channel (a CLI flag or an API
	// field).
	PromptDeliveryNative = "native"
	// PromptDeliveryPrepended — the text is spliced into the user message. The
	// new instruction arrives, but with no system-prompt authority. A field diff
	// cannot express "it landed, but weaker", which is why this is its own axis.
	PromptDeliveryPrepended = "prepended"
	// PromptDeliveryIgnored — the engine discards a system prompt entirely, so
	// rewriting the instructions changes nothing.
	PromptDeliveryIgnored = "ignored"
)

// fieldDelivery maps a versioned snapshot key to how its value reaches a run.
// An engine-delivered key names the ExecOptions field that carries it, so the
// matrix — not an opinion in this file — decides the consequence.
var fieldDelivery = map[string]struct {
	by        string
	execField agent.ExecOptionField
}{
	// The instructions ARE the system prompt (daemon.go builds it from them).
	"instructions": {by: DeliveryEngine, execField: agent.FieldSystemPrompt},
	// runtime_config carries system_prompt_mode — see
	// daemon/cerebro_system_prompt_mode.go. It changes how the prompt is
	// delivered, so the system-prompt entry governs it too.
	"runtime_config": {by: DeliveryEngine, execField: agent.FieldSystemPrompt},
	"model":          {by: DeliveryEngine, execField: agent.FieldModel},
	"thinking_level": {by: DeliveryEngine, execField: agent.FieldThinkingLevel},
	"mcp_config":     {by: DeliveryEngine, execField: agent.FieldMCPConfig},
	"custom_args":    {by: DeliveryEngine, execField: agent.FieldCustomArgs},

	// Applied by Multica around the engine process, so they hold everywhere.
	"persona_sandbox": {by: DeliveryMultica},
	"skill_ids":       {by: DeliveryMultica},
	"custom_env_keys": {by: DeliveryMultica},

	// A label on the agent. It reaches no run.
	"description": {by: DeliveryNone},
}

// promptFields are the snapshot keys whose change alters what the model reads as
// its system prompt. Only these warrant the prompt-delivery sentence — attaching
// it to a model-only change would warn about semantics that approval leaves
// untouched.
var promptFields = []string{"instructions", "runtime_config"}

// ApprovalSnapshotFields lists every snapshot key this file can classify, sorted.
// Exported so a test (and a caller enumerating the whole bundle) does not have to
// restate the list and drift from it.
func ApprovalSnapshotFields() []string {
	out := make([]string, 0, len(fieldDelivery))
	for key := range fieldDelivery {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// FieldConsequence is what approving a change to one snapshot field will do.
type FieldConsequence struct {
	// Field is the snapshot key, matching what the field diff renders.
	Field string `json:"field"`
	// DeliveredBy is engine | multica | none — who acts on the value.
	DeliveredBy string `json:"delivered_by"`
	// ExecField is the ExecOptions field carrying it, empty unless engine-delivered.
	ExecField string `json:"exec_field,omitempty"`
	// Handling is the raw matrix verdict, empty unless engine-delivered.
	Handling string `json:"handling,omitempty"`
	// Consequence is the one-word answer the UI groups on.
	Consequence string `json:"consequence"`
	// Silent reports that the engine drops the value with no log line. The UI's
	// warning order is built on this bit, not on Consequence alone.
	Silent bool `json:"silent"`
}

// PromptEffect is how the agent's instructions will reach the model once the
// change is approved.
type PromptEffect struct {
	Native   bool     `json:"native"`
	Modes    []string `json:"modes"`
	Delivery string   `json:"delivery"`
}

// ApprovalImpact is the whole answer for one proposal on one engine.
type ApprovalImpact struct {
	Status   string `json:"status"`
	Provider string `json:"provider"`
	// Fields covers every changed key the caller asked about, sorted by name.
	Fields []FieldConsequence `json:"fields"`
	// Effective / Ineffective are field names by consequence, so a UI can render
	// the headline without walking Fields. Ineffective holds ONLY engine drops —
	// a field that reaches no run by design (description) is not the engine's
	// doing and does not belong in a warning.
	Effective   []string `json:"effective"`
	Ineffective []string `json:"ineffective"`
	// SilentlyIneffective is the subset of Ineffective with no log line.
	SilentlyIneffective []string      `json:"silently_ineffective"`
	SystemPrompt        *PromptEffect `json:"system_prompt,omitempty"`
}

// ApprovalImpactFor reports what approving a proposal that changes changedFields
// will actually do to an agent running on provider.
//
// Unknown provider yields StatusUnknown with nothing enumerated — see the file
// header for why absence of proof must not be served as proof of absence.
func ApprovalImpactFor(provider string, changedFields []string) ApprovalImpact {
	out := ApprovalImpact{
		Status:              StatusUnknown,
		Provider:            provider,
		Fields:              []FieldConsequence{},
		Effective:           []string{},
		Ineffective:         []string{},
		SilentlyIneffective: []string{},
	}

	if _, ok := agent.ExecOptionsSupportFor(provider); !ok {
		return out
	}
	out.Status = StatusKnown

	seen := map[string]bool{}
	touchesPrompt := false
	for _, field := range changedFields {
		if seen[field] {
			continue
		}
		seen[field] = true

		row := consequenceFor(provider, field)
		out.Fields = append(out.Fields, row)

		switch row.Consequence {
		case ConsequenceTakesEffect:
			out.Effective = append(out.Effective, field)
		case ConsequenceNoEffectLogged, ConsequenceNoEffectSilent:
			out.Ineffective = append(out.Ineffective, field)
			if row.Silent {
				out.SilentlyIneffective = append(out.SilentlyIneffective, field)
			}
		}

		for _, pf := range promptFields {
			if field == pf {
				touchesPrompt = true
			}
		}
	}
	sort.Slice(out.Fields, func(i, j int) bool { return out.Fields[i].Field < out.Fields[j].Field })

	if touchesPrompt {
		out.SystemPrompt = promptEffect(provider)
	}
	return out
}

// consequenceFor classifies one changed snapshot key on one provider.
func consequenceFor(provider, field string) FieldConsequence {
	delivery, known := fieldDelivery[field]
	if !known {
		return FieldConsequence{
			Field:       field,
			DeliveredBy: DeliveryNone,
			Consequence: ConsequenceUnknownField,
		}
	}

	switch delivery.by {
	case DeliveryMultica:
		return FieldConsequence{
			Field:       field,
			DeliveredBy: DeliveryMultica,
			Consequence: ConsequenceTakesEffect,
		}
	case DeliveryNone:
		return FieldConsequence{
			Field:       field,
			DeliveredBy: DeliveryNone,
			Consequence: ConsequenceNoRuntimeEffect,
		}
	}

	handling, known := agent.ExecOptionsHandling(provider, delivery.execField)
	row := FieldConsequence{
		Field:       field,
		DeliveredBy: DeliveryEngine,
		ExecField:   string(delivery.execField),
		Handling:    string(handling),
		Silent:      known && handling == agent.HandlingIgnoredSilent,
	}
	// A provider in the matrix with no entry for this field answers
	// ignored_silent/known=true, so !known here means the provider itself is
	// unknown — which ApprovalImpactFor has already excluded.
	switch {
	case handling.Effective():
		row.Consequence = ConsequenceTakesEffect
	case handling == agent.HandlingIgnoredLogged:
		row.Consequence = ConsequenceNoEffectLogged
	default:
		row.Consequence = ConsequenceNoEffectSilent
	}
	return row
}

// promptEffect describes how the instructions will reach the model. nil when the
// provider has no authoritative prompt entry — same "unknown is not a loss" rule.
func promptEffect(provider string) *PromptEffect {
	support, ok := agent.SystemPromptSupportFor(provider)
	if !ok {
		return nil
	}
	out := &PromptEffect{
		Native: support.Native,
		Modes:  modeStrings(support.Modes),
	}
	switch {
	case len(support.Modes) == 0:
		out.Delivery = PromptDeliveryIgnored
	case support.Native:
		out.Delivery = PromptDeliveryNative
	default:
		out.Delivery = PromptDeliveryPrepended
	}
	return out
}
