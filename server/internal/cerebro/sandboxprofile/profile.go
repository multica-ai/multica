package sandboxprofile

// profile.go turns the raw runtime sandbox policy (agent_runtime.sandbox_policy
// jsonb) into a small set of named, human-readable isolation profiles for the
// runtime screen (FIR-2230 phase 6 — sandbox as an interface, not raw JSON).
//
// The profile is the OUTER WALL: it decides, in plain language, how locked-down
// the machine is for an agent. The per-tool permission table (toolpolicy) is the
// set of rules INSIDE that wall. A profile is never stored on its own — it is
// derived from, and expands back into, the exact same sandbox_policy the daemon
// already enforces in buildSandboxConfig. So "change profile" is not a cosmetic
// label: it rewrites what the agent can really do on the host.
//
// We deliberately keep our own copy of the policy shape here (matching the JSON
// tags on daemon.RuntimeSandboxPolicy) so the server's handler layer can classify
// and expand profiles without importing the daemon binary's package.

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ErrUnknownProfile is returned by Resolve for a profile id that is not a
// selectable preset (e.g. "custom" or a typo).
var ErrUnknownProfile = errors.New("unknown sandbox profile")

// Policy mirrors the JSON shape of daemon.RuntimeSandboxPolicy. Only the fields
// that actually reach the seatbelt sandbox are modelled; anything else in the
// stored JSON makes the policy "custom".
type Policy struct {
	NetworkMode        string   `json:"network_mode,omitempty"`
	AllowedHosts       []string `json:"allowed_hosts,omitempty"`
	ExtraWritablePaths []string `json:"extra_writable_paths,omitempty"`
	DeniedPaths        []string `json:"denied_paths,omitempty"`
	DenyShell          bool     `json:"deny_shell,omitempty"`
	DeniedExecutables  []string `json:"denied_executables,omitempty"`
}

// Profile ids. These are stable API/storage-independent identifiers; the labels
// and descriptions below are what the human reads.
const (
	// Developer: the default open profile — the agent can run shell commands and
	// write to its workspace, within the outer sandbox. This is the empty policy.
	ProfileDeveloper = "developer"
	// ReadOnly: locked down — no shell or scripting access on the machine. The
	// agent keeps its built-in tools (subject to the permission table) but cannot
	// spawn bash/sh/zsh or scripting executables.
	ProfileReadOnly = "read_only"
	// Custom: a hand-tuned policy that does not match a named preset. The raw
	// editor is shown so an admin can see and adjust the exact rules.
	ProfileCustom = "custom"
)

// Definition is one selectable profile as the UI presents it.
type Definition struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Selectable is false for "custom": you don't pick custom, you land on it by
	// editing the raw policy into something with no matching preset.
	Selectable bool `json:"selectable"`
}

// Definitions returns the profiles in display order. Kept as a function (not a
// package var) so callers can't mutate the shared slice.
func Definitions() []Definition {
	return []Definition{
		{
			ID:          ProfileDeveloper,
			Label:       "Developer",
			Description: "Full developer access inside the sandbox. The agent can run shell commands and write to its workspace.",
			Selectable:  true,
		},
		{
			ID:          ProfileReadOnly,
			Label:       "Read-only",
			Description: "Locked down. No shell or scripting access on the machine — the agent can only use its built-in tools.",
			Selectable:  true,
		},
		{
			ID:          ProfileCustom,
			Label:       "Custom",
			Description: "Hand-tuned rules that don't match a preset. Edit the raw policy below.",
			Selectable:  false,
		},
	}
}

// policyForProfile returns the canonical policy for a selectable profile.
func policyForProfile(id string) (Policy, bool) {
	switch id {
	case ProfileDeveloper:
		return Policy{}, true
	case ProfileReadOnly:
		return Policy{DenyShell: true}, true
	default:
		return Policy{}, false
	}
}

// PolicyJSONForProfile expands a selectable profile id into the canonical
// sandbox_policy JSON to persist. ok is false for unknown / non-selectable ids
// (e.g. "custom"), so the caller can reject the request rather than silently
// wiping a hand-tuned policy.
func PolicyJSONForProfile(id string) (json.RawMessage, bool) {
	p, ok := policyForProfile(id)
	if !ok {
		return nil, false
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// Resolve picks the sandbox_policy to persist for a sandbox-policy update.
// When profile is set it must be a selectable preset, which expands to its
// canonical policy (ignoring any raw policy in the same request) so the
// security semantics stay server-authoritative. When profile is empty the raw
// policy passes through unchanged for the existing custom-edit path.
func Resolve(profile string, raw json.RawMessage) (json.RawMessage, error) {
	if profile == "" {
		return raw, nil
	}
	expanded, ok := PolicyJSONForProfile(profile)
	if !ok {
		return nil, ErrUnknownProfile
	}
	return expanded, nil
}

// Classify maps a stored sandbox_policy to the profile it currently equals.
// This is the "real status" the screen shows: it reflects the policy the daemon
// actually enforces, never a hardcoded guess. An empty / unset / unparseable
// policy classifies as Developer (the system default). Anything that carries
// extra rules beyond a known preset is Custom.
func Classify(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == "{}" {
		return ProfileDeveloper
	}
	var p Policy
	if err := json.Unmarshal(trimmed, &p); err != nil {
		// Stored value isn't shaped like a policy we understand → treat as custom
		// so the admin sees the raw editor instead of a wrong label.
		return ProfileCustom
	}
	for _, def := range Definitions() {
		if !def.Selectable {
			continue
		}
		preset, _ := policyForProfile(def.ID)
		if policiesEqual(p, preset) {
			return def.ID
		}
	}
	return ProfileCustom
}

// policiesEqual compares two policies by their effective, normalized content.
// We marshal both back to JSON with omitempty so zero-valued and absent fields
// compare equal (e.g. {} == {"deny_shell":false}).
func policiesEqual(a, b Policy) bool {
	aj, err1 := json.Marshal(a)
	bj, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}
