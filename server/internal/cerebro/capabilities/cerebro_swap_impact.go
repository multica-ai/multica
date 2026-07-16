package capabilities

import (
	"sort"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// Provider swap impact (FIR-3212, Swap slice).
//
// cerebro_exec_options.go answers "what does the engine this agent runs on do
// with our settings?". It cannot answer the question an operator asks the
// moment before they change engine: "what do I lose if I move this agent to
// Kiro?". Without that answer the Setup screen can only show the destination's
// field list — the operator has to diff two screens in their head and notice on
// their own that the deny-policy they configured stops being enforced.
//
// This file computes that diff once, on the side that owns the matrix. It is
// pure: no DB, no HTTP, no copy. The UI renders the outcomes; it does not
// recompute them, because the vocabulary (silent vs logged drop, prepend vs
// native prompt) is exactly the knowledge that must not be duplicated into a
// frontend that cannot be tested against the installed CLI.
//
// It inherits the StaticCatalog contract: if either side of the swap is unknown
// to the matrix, the answer is StatusUnknown and NOTHING is enumerated. Serving
// a confident "you will lose 6 settings" for a provider we simply have no entry
// for would be worse than silence — the operator would cancel a swap that works.

const (
	// StatusKnown means both providers are in the matrix and the transitions
	// below are authoritative.
	StatusKnown = "known"
	// StatusUnknown means at least one provider has no matrix entry. It never
	// means "the swap loses everything".
	StatusUnknown = "unknown"
)

// Per-field outcomes of a swap.
const (
	// OutcomeRetained — the field is honoured on both engines.
	OutcomeRetained = "retained"
	// OutcomeLost — honoured today, not honoured on the target. These are the
	// rows the UI removes from the form, and the reason it must say why.
	OutcomeLost = "lost"
	// OutcomeGained — not honoured today, honoured on the target.
	OutcomeGained = "gained"
	// OutcomeUnavailable — honoured on neither. Not a consequence of the swap,
	// but reported so the target's field list stays complete.
	OutcomeUnavailable = "unavailable"
)

// System-prompt outcomes. Kept separate from the field outcomes because a
// system prompt does not merely land or not land: it can land with its meaning
// quietly removed, which no honoured/ignored pair can express.
const (
	// PromptOutcomeUnchanged — same delivery semantics on both engines.
	PromptOutcomeUnchanged = "unchanged"
	// PromptOutcomeDowngradedToPrepend — the text still arrives, but the target
	// has no system-prompt channel, so it is spliced into the user message and
	// carries no system-prompt semantics. The instruction does not disappear;
	// its authority does. This is the case a field diff cannot show.
	PromptOutcomeDowngradedToPrepend = "downgraded_to_prepend"
	// PromptOutcomeIgnored — the target discards a system prompt entirely.
	PromptOutcomeIgnored = "ignored"
	// PromptOutcomeGainedNative — the target has a real system-prompt channel
	// where the source had none.
	PromptOutcomeGainedNative = "gained_native"
)

// FieldTransition is one ExecOptions field's fate across the swap.
type FieldTransition struct {
	Field string `json:"field"`
	// FromHandling / ToHandling are the raw matrix verdicts
	// (honoured|ignored_logged|ignored_silent) on each side.
	FromHandling string `json:"from_handling"`
	ToHandling   string `json:"to_handling"`
	// Outcome is the one-word answer the UI groups on.
	Outcome string `json:"outcome"`
	// SilentOnTarget reports that the target drops this value with no log line.
	// It is the difference between a swap that degrades loudly and one where an
	// operator keeps believing a protection is enforced — the warning hierarchy
	// in the UI is built on this bit, not on Outcome alone.
	SilentOnTarget bool `json:"silent_on_target"`
}

// SystemPromptTransition is how the agent's system prompt changes meaning.
type SystemPromptTransition struct {
	FromNative bool     `json:"from_native"`
	ToNative   bool     `json:"to_native"`
	FromModes  []string `json:"from_modes"`
	ToModes    []string `json:"to_modes"`
	Outcome    string   `json:"outcome"`
}

// SwapImpact is the whole answer for one from→to provider pair.
type SwapImpact struct {
	Status string `json:"status"`
	From   string `json:"from"`
	To     string `json:"to"`
	// Fields covers every field in the matrix, sorted by name.
	Fields []FieldTransition `json:"fields"`
	// Lost / Gained are the field names by outcome, for a UI that wants the
	// headline without walking Fields.
	Lost   []string `json:"lost"`
	Gained []string `json:"gained"`
	// SilentlyLost is the subset of Lost the target drops without a trace.
	SilentlyLost []string                `json:"silently_lost"`
	SystemPrompt *SystemPromptTransition `json:"system_prompt,omitempty"`
}

// SwapImpactFor reports what changes when an agent moves from provider `from`
// to provider `to`.
//
// Unknown on either side yields StatusUnknown with empty transitions — see the
// file header for why an unknown provider must not be rendered as a total loss.
func SwapImpactFor(from, to string) SwapImpact {
	out := SwapImpact{
		Status:       StatusUnknown,
		From:         from,
		To:           to,
		Fields:       []FieldTransition{},
		Lost:         []string{},
		Gained:       []string{},
		SilentlyLost: []string{},
	}

	_, fromOK := agent.ExecOptionsSupportFor(from)
	_, toOK := agent.ExecOptionsSupportFor(to)
	if !fromOK || !toOK {
		return out
	}
	out.Status = StatusKnown

	for _, field := range agent.ExecOptionFields() {
		fromHandling, fromKnown := agent.ExecOptionsHandling(from, field)
		toHandling, toKnown := agent.ExecOptionsHandling(to, field)
		if !fromKnown || !toKnown {
			continue
		}

		row := FieldTransition{
			Field:          string(field),
			FromHandling:   string(fromHandling),
			ToHandling:     string(toHandling),
			SilentOnTarget: toHandling == agent.HandlingIgnoredSilent,
		}
		switch {
		case fromHandling.Effective() && toHandling.Effective():
			row.Outcome = OutcomeRetained
		case fromHandling.Effective() && !toHandling.Effective():
			row.Outcome = OutcomeLost
			out.Lost = append(out.Lost, row.Field)
			if row.SilentOnTarget {
				out.SilentlyLost = append(out.SilentlyLost, row.Field)
			}
		case !fromHandling.Effective() && toHandling.Effective():
			row.Outcome = OutcomeGained
			out.Gained = append(out.Gained, row.Field)
		default:
			row.Outcome = OutcomeUnavailable
		}
		out.Fields = append(out.Fields, row)
	}
	sort.Slice(out.Fields, func(i, j int) bool { return out.Fields[i].Field < out.Fields[j].Field })

	out.SystemPrompt = systemPromptTransition(from, to)
	return out
}

// systemPromptTransition classifies the prompt-semantics change. nil when either
// side has no authoritative entry — same "unknown is not a loss" rule.
func systemPromptTransition(from, to string) *SystemPromptTransition {
	fromSupport, fromOK := agent.SystemPromptSupportFor(from)
	toSupport, toOK := agent.SystemPromptSupportFor(to)
	if !fromOK || !toOK {
		return nil
	}

	out := &SystemPromptTransition{
		FromNative: fromSupport.Native,
		ToNative:   toSupport.Native,
		FromModes:  modeStrings(fromSupport.Modes),
		ToModes:    modeStrings(toSupport.Modes),
	}
	switch {
	case len(toSupport.Modes) == 0:
		out.Outcome = PromptOutcomeIgnored
	case fromSupport.Native && !toSupport.Native:
		out.Outcome = PromptOutcomeDowngradedToPrepend
	case !fromSupport.Native && toSupport.Native:
		out.Outcome = PromptOutcomeGainedNative
	default:
		out.Outcome = PromptOutcomeUnchanged
	}
	return out
}

func modeStrings(modes []agent.SystemPromptMode) []string {
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		out = append(out, string(m))
	}
	return out
}
