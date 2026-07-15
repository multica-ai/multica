package agentoffice

import (
	"encoding/json"
	"fmt"
	"strings"

	// Aliased because `agent` is the name of the live DB row throughout this
	// package. The mode vocabulary must come from the same place the backends
	// read it, so there is exactly one list.
	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
)

// System-prompt delivery mode as versioned agent configuration (FIR-3212, slice 3).
//
// Slice 1 added SystemPromptMode to agent.ExecOptions and taught claude.go to
// honour it. Nothing set it: the field had no configured source, so every run
// still got the empty default and the capability was unreachable. This file is
// the source.
//
// It lives in the agent's runtime_config JSONB rather than a dedicated column
// for two reasons, both grounded in the existing code:
//
//   - Precedent. runtime_config is already the home of the only other per-agent
//     knob that feeds ExecOptions: OpenclawMode, decoded by
//     daemon/openclaw_runtime_config.go. Same struct, same source, same shape —
//     a second mechanism for the same job would be the parallel abstraction the
//     repo conventions warn against.
//   - Versioning comes free and stays honest. runtime_config is already part of
//     the composite in agent_context_version.snapshot, so the mode inherits
//     version history, change requests, review and rollback with no new table
//     and no new versioning system. A dedicated column would instead have to be
//     hand-written into ~28 generated scan sites across two sqlc packages, which
//     `sqlc generate` can no longer verify on this repo (it fails on three
//     pre-existing query errors on a clean tree), and which no CI job checks.
//
// The trade-off is that the mode is not independently queryable across the
// fleet — "which agents replace their prompt?" needs a JSONB lookup, not a
// column scan. That is worth it while the mode is per-agent configuration; if a
// fleet-wide audit becomes a real need, promoting it to a column is a mechanical
// follow-up that this accessor already hides from every caller.

// SystemPromptModeKey is the runtime_config key holding the delivery mode.
// Exported so the daemon-side decoder reads the same string this package writes.
const SystemPromptModeKey = "system_prompt_mode"

// ValidSystemPromptMode reports whether mode is one the backends understand.
//
// The empty string is valid and means "leave it to the backend". Anything else
// must be a mode agentpkg.SystemPromptMode defines — a value no backend
// recognises would be stored, versioned, shown in review and then silently
// dropped at run time, which is the exact dishonesty FIR-3212 exists to remove.
//
// This validates the VOCABULARY only. Whether the agent's own runtime honours
// the mode is a separate question, answered by ValidateSystemPromptModeForProvider
// below — callers that know the agent must use that one instead.
func ValidSystemPromptMode(mode string) bool {
	switch agentpkg.SystemPromptMode(mode) {
	case agentpkg.SystemPromptModeDefault,
		agentpkg.SystemPromptModeAppend,
		agentpkg.SystemPromptModeReplace,
		agentpkg.SystemPromptModePrepend:
		return true
	}
	return false
}

// ValidateSystemPromptModeForProvider reports whether mode is one that provider
// can actually honour, on top of the vocabulary check.
//
// An earlier revision of this file argued the question "has no answer at config
// time" because an agent could be claimed by runtimes on different providers.
// The schema says otherwise: agent.runtime_id is NOT NULL with an FK to
// agent_runtime (migration 004), agent_runtime.provider is NOT NULL, and
// agent_task_queue.runtime_id is copied from agent.runtime_id — so every task
// for an agent runs on exactly one provider, known here. Without this check
// Agent Office happily stored, versioned, diffed, approved and rolled out a
// setting the runtime matrix itself says cannot work ("append" to Codex, whose
// channel always replaces; any mode to Hermes, which discards the prompt), and
// the run then silently dropped it.
//
// Two deliberate non-rejections:
//   - The default mode is always valid. It means "do what you already do", so it
//     is never a change, even on a runtime that ignores system prompts entirely.
//   - An unknown or empty provider is never rejected. SystemPromptSupportFor's
//     contract is that ok=false means "no authoritative entry", NOT "supports
//     nothing"; rejecting would block configuring an agent on a runtime we have
//     not catalogued yet, which is a worse failure than allowing it.
func ValidateSystemPromptModeForProvider(provider, mode string) error {
	if !ValidSystemPromptMode(mode) {
		return fmt.Errorf("unknown system_prompt_mode %q: want one of append, replace, prepend, or empty for the runtime default", mode)
	}
	if agentpkg.SystemPromptMode(mode) == agentpkg.SystemPromptModeDefault {
		return nil
	}
	support, ok := agentpkg.SystemPromptSupportFor(provider)
	if !ok {
		return nil
	}
	if support.Supports(agentpkg.SystemPromptMode(mode)) {
		return nil
	}
	if len(support.Modes) == 0 {
		return fmt.Errorf("runtime %q ignores the system prompt entirely, so system_prompt_mode %q would be stored but never applied: leave it empty for the runtime default", provider, mode)
	}
	accepted := make([]string, 0, len(support.Modes))
	for _, m := range support.Modes {
		accepted = append(accepted, string(m))
	}
	return fmt.Errorf("runtime %q does not support system_prompt_mode %q: it accepts %s", provider, mode, strings.Join(accepted, ", "))
}

// SystemPromptModeOf reads the mode out of a snapshot's runtime_config.
//
// Absent, malformed, or unknown values all read as the default. A malformed blob
// is not an error here: runtime_config is free-form and owned by several
// features, so a key this package does not understand must never break reading
// the one it does. An unknown value cannot occur through WithSystemPromptMode,
// but can arrive from a hand-written API payload — reading it as the default
// keeps that agent on its prior behaviour instead of guessing.
func SystemPromptModeOf(snap ContextSnapshot) string {
	if len(snap.RuntimeConfig) == 0 {
		return string(agentpkg.SystemPromptModeDefault)
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(snap.RuntimeConfig, &cfg); err != nil {
		return string(agentpkg.SystemPromptModeDefault)
	}
	raw, ok := cfg[SystemPromptModeKey]
	if !ok {
		return string(agentpkg.SystemPromptModeDefault)
	}
	var mode string
	if err := json.Unmarshal(raw, &mode); err != nil || !ValidSystemPromptMode(mode) {
		return string(agentpkg.SystemPromptModeDefault)
	}
	return mode
}

// rawSystemPromptMode returns the mode exactly as stored, without normalising an
// unrecognised value to the default, plus whether the key was present at all.
//
// SystemPromptModeOf is the right reader everywhere the answer is "what will
// this agent do" — it must never fail. Validation is the one caller that needs
// to tell "no mode set" apart from "a mode we do not understand", so it reads
// through here instead.
func rawSystemPromptMode(snap ContextSnapshot) (string, bool) {
	if len(snap.RuntimeConfig) == 0 {
		return "", false
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(snap.RuntimeConfig, &cfg); err != nil {
		// A blob that does not parse carries no mode to validate; it is left to
		// whatever already tolerates malformed runtime_config today.
		return "", false
	}
	encoded, ok := cfg[SystemPromptModeKey]
	if !ok {
		return "", false
	}
	var mode string
	if err := json.Unmarshal(encoded, &mode); err != nil {
		// Present but not a string (e.g. a number) — report it so validation
		// rejects rather than silently ignores it.
		return string(encoded), true
	}
	return mode, true
}

// WithSystemPromptMode returns a copy of snap with the delivery mode set,
// leaving every other runtime_config key untouched.
//
// Setting the default mode REMOVES the key rather than storing an empty string,
// so an agent that never chose a mode and one that chose the default produce
// byte-identical snapshots. Without that, clearing a mode would show up in the
// review diff as a phantom change to a field nobody set.
func WithSystemPromptMode(snap ContextSnapshot, mode string) (ContextSnapshot, error) {
	if !ValidSystemPromptMode(mode) {
		return snap, fmt.Errorf("unknown system_prompt_mode %q: want one of append, replace, prepend, or empty for the runtime default", mode)
	}

	cfg := map[string]json.RawMessage{}
	if len(snap.RuntimeConfig) > 0 {
		// A malformed existing blob is replaced rather than merged into: there is
		// nothing to preserve, and failing here would make the agent
		// unconfigurable until someone hand-repaired the JSON.
		_ = json.Unmarshal(snap.RuntimeConfig, &cfg)
	}

	if agentpkg.SystemPromptMode(mode) == agentpkg.SystemPromptModeDefault {
		delete(cfg, SystemPromptModeKey)
	} else {
		encoded, err := json.Marshal(mode)
		if err != nil {
			return snap, fmt.Errorf("encode system_prompt_mode: %w", err)
		}
		cfg[SystemPromptModeKey] = encoded
	}

	// An empty map marshals to "{}", never nil, so the NOT NULL column and the
	// snapshot's own encoder both keep a valid value.
	blob, err := json.Marshal(cfg)
	if err != nil {
		return snap, fmt.Errorf("encode runtime_config: %w", err)
	}
	snap.RuntimeConfig = blob
	return snap, nil
}
