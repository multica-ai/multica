package agentoffice

import (
	"encoding/json"
	"fmt"
)

// Brief-layer delivery modes as versioned agent configuration (FIR-3212).
//
// system_prompt_mode (cerebro_system_prompt_mode.go) configured the FIRST layer
// an agent reads: how its role text replaces or joins the runtime's built-in
// system prompt. These two keys configure the next two layers of the same
// startup text, measured in the FIR-3212 brief audit as the two largest:
//
//   - workspace_brief_mode — the shared Multica workspace brief (Available
//     Commands, platform rules, project context, …) that runtime_config.go
//     glues onto every agent without exception. "off" reduces the injected
//     brief to the agent's own identity blocks, for agents (triage, CFO,
//     support) whose role text carries their whole contract. The per-turn task
//     prompt still delivers the trigger workflow, so the agent loop survives.
//   - tools_brief_mode — the generated Connections tool list, the single
//     largest layer (~6.5k tokens of raw machine names on a fully-wired
//     agent). "summary" folds each connection to one line with a count and a
//     live discovery pointer instead of a line per generated endpoint tool;
//     "off" drops the section entirely (FIR-4500), which is also what the
//     workspace-level cerebro_tools_brief flag forces when it is turned off.
//
// Both live in the agent's runtime_config JSONB for the same two reasons
// system_prompt_mode does: precedent (runtime_config is the established home of
// per-agent knobs the daemon decodes at claim time) and free, honest versioning
// (runtime_config is part of agent_context_version.snapshot, so both keys
// inherit change requests, review, diff and rollback with no new machinery).
//
// Unlike system_prompt_mode there is no per-provider validation: both layers
// are rendered by execenv into the runtime config file (CLAUDE.md / AGENTS.md /
// GEMINI.md) before any provider-specific channel is involved, so every
// provider honours them identically and a provider matrix would be dead code.

// WorkspaceBriefModeKey is the runtime_config key controlling the shared
// workspace brief layer. Exported so the daemon-side decoder reads the same
// string this package writes.
const WorkspaceBriefModeKey = "workspace_brief_mode"

// ToolsBriefModeKey is the runtime_config key controlling the Connections &
// MCP tools layer of the brief.
const ToolsBriefModeKey = "tools_brief_mode"

// Workspace-brief modes. The empty string is the stored form of the default —
// WithBriefLayerMode removes the key for it, so "never configured" and
// "explicitly default" produce byte-identical snapshots (same rule as
// system_prompt_mode).
const (
	// WorkspaceBriefModeDefault renders the full shared brief, exactly as
	// every agent gets today.
	WorkspaceBriefModeDefault = ""
	// WorkspaceBriefModeFull is the explicit spelling of the default. Accepted
	// so a hand-written payload saying "full" is not rejected as unknown, but
	// normalised to the default (key removed) on write.
	WorkspaceBriefModeFull = "full"
	// WorkspaceBriefModeOff strips the shared workspace brief: the injected
	// config file keeps only the agent's own identity layers (Agent Identity,
	// Requesting User, Task Initiator, communication profile) plus the
	// comment-formatting guardrail the reply contract depends on.
	WorkspaceBriefModeOff = "off"
)

// Tools-brief modes.
const (
	// ToolsBriefModeDefault lists every resolved tool individually, as today.
	ToolsBriefModeDefault = ""
	// ToolsBriefModeFull is the explicit spelling of the default; normalised
	// to it on write, same as WorkspaceBriefModeFull.
	ToolsBriefModeFull = "full"
	// ToolsBriefModeSummary folds each connection's generated endpoint tools
	// to a single line (name, count, ask-count, discovery pointer). Platform
	// MCP tools stay listed individually — each one is a distinct capability,
	// not a generated endpoint family.
	ToolsBriefModeSummary = "summary"
	// ToolsBriefModeOff drops the Connections & MCP tools section altogether
	// (FIR-4500). Nothing is lost but documentation: a tool is callable through
	// the runtime's own tool schemas and the live
	// multica_connection_tools_status lookup, never through this list. It is
	// also the value the workspace-level cerebro_tools_brief flag forces when
	// an admin turns the section off for everyone.
	ToolsBriefModeOff = "off"
)

// ValidWorkspaceBriefMode reports whether mode is a workspace-brief mode the
// renderer understands. Anything else would be stored, versioned, shown in
// review and then silently dropped at run time — the exact dishonesty FIR-3212
// exists to remove.
func ValidWorkspaceBriefMode(mode string) bool {
	switch mode {
	case WorkspaceBriefModeDefault, WorkspaceBriefModeFull, WorkspaceBriefModeOff:
		return true
	}
	return false
}

// ValidToolsBriefMode reports whether mode is a tools-brief mode the renderer
// understands.
func ValidToolsBriefMode(mode string) bool {
	switch mode {
	case ToolsBriefModeDefault, ToolsBriefModeFull, ToolsBriefModeSummary, ToolsBriefModeOff:
		return true
	}
	return false
}

// WorkspaceBriefModeOf reads the workspace-brief mode out of a snapshot's
// runtime_config. Absent, malformed, or unknown values all read as the default
// — runtime_config is free-form and owned by several features, so a key this
// package does not understand must never break reading the one it does. "full"
// normalises to the default so both spellings behave identically downstream.
func WorkspaceBriefModeOf(snap ContextSnapshot) string {
	return briefLayerModeOf(snap, WorkspaceBriefModeKey, ValidWorkspaceBriefMode, WorkspaceBriefModeFull)
}

// ToolsBriefModeOf reads the tools-brief mode out of a snapshot's
// runtime_config, with the same tolerance and normalisation rules.
func ToolsBriefModeOf(snap ContextSnapshot) string {
	return briefLayerModeOf(snap, ToolsBriefModeKey, ValidToolsBriefMode, ToolsBriefModeFull)
}

func briefLayerModeOf(snap ContextSnapshot, key string, valid func(string) bool, fullSpelling string) string {
	mode, ok := rawBriefLayerMode(snap, key)
	if !ok || !valid(mode) || mode == fullSpelling {
		return ""
	}
	return mode
}

// rawBriefLayerMode returns the mode exactly as stored plus whether the key was
// present at all. Validation is the one caller that needs to tell "no mode set"
// apart from "a mode we do not understand", so it reads through here instead of
// the normalising *Of readers.
func rawBriefLayerMode(snap ContextSnapshot, key string) (string, bool) {
	if len(snap.RuntimeConfig) == 0 {
		return "", false
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(snap.RuntimeConfig, &cfg); err != nil {
		return "", false
	}
	encoded, ok := cfg[key]
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

// WithWorkspaceBriefMode returns a copy of snap with the workspace-brief mode
// set, leaving every other runtime_config key untouched. Setting the default
// (or its "full" spelling) REMOVES the key, so an agent that never chose a mode
// and one that chose the default produce byte-identical snapshots.
func WithWorkspaceBriefMode(snap ContextSnapshot, mode string) (ContextSnapshot, error) {
	if !ValidWorkspaceBriefMode(mode) {
		return snap, fmt.Errorf("unknown workspace_brief_mode %q: want off, or empty/full for the default", mode)
	}
	return withBriefLayerMode(snap, WorkspaceBriefModeKey, mode, mode == WorkspaceBriefModeDefault || mode == WorkspaceBriefModeFull)
}

// WithToolsBriefMode returns a copy of snap with the tools-brief mode set,
// under the same key-removal rule as WithWorkspaceBriefMode.
func WithToolsBriefMode(snap ContextSnapshot, mode string) (ContextSnapshot, error) {
	if !ValidToolsBriefMode(mode) {
		return snap, fmt.Errorf("unknown tools_brief_mode %q: want summary or off, or empty/full for the default", mode)
	}
	return withBriefLayerMode(snap, ToolsBriefModeKey, mode, mode == ToolsBriefModeDefault || mode == ToolsBriefModeFull)
}

func withBriefLayerMode(snap ContextSnapshot, key, mode string, isDefault bool) (ContextSnapshot, error) {
	cfg := map[string]json.RawMessage{}
	if len(snap.RuntimeConfig) > 0 {
		// A malformed existing blob is replaced rather than merged into: there
		// is nothing to preserve, and failing here would make the agent
		// unconfigurable until someone hand-repaired the JSON.
		_ = json.Unmarshal(snap.RuntimeConfig, &cfg)
	}

	if isDefault {
		delete(cfg, key)
	} else {
		encoded, err := json.Marshal(mode)
		if err != nil {
			return snap, fmt.Errorf("encode %s: %w", key, err)
		}
		cfg[key] = encoded
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

// ValidateSnapshotBriefLayerModes rejects a snapshot carrying a brief-layer
// mode the renderer does not understand. Mirrors the system_prompt_mode
// chokepoint: the proposed_snapshot path and a raw runtime_config override both
// bypass the With* writers, so this is the only place that stops an unknown
// mode being stored, versioned, approved, and then silently dropped at run
// time. No provider dimension — see the package comment above.
func ValidateSnapshotBriefLayerModes(snap ContextSnapshot) error {
	if mode, ok := rawBriefLayerMode(snap, WorkspaceBriefModeKey); ok && !ValidWorkspaceBriefMode(mode) {
		return fmt.Errorf("unknown workspace_brief_mode %q: want off, or empty/full for the default", mode)
	}
	if mode, ok := rawBriefLayerMode(snap, ToolsBriefModeKey); ok && !ValidToolsBriefMode(mode) {
		return fmt.Errorf("unknown tools_brief_mode %q: want summary or off, or empty/full for the default", mode)
	}
	return nil
}
