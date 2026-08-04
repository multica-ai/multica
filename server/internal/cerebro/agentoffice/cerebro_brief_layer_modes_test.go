package agentoffice

import (
	"encoding/json"
	"strings"
	"testing"
)

// FIR-3212 (agent configuration, full scope): the two remaining brief layers
// become real, versioned configuration. workspace_brief_mode turns the shared
// workspace brief off for an agent; tools_brief_mode folds the generated
// Connections tool list. These tests pin the config half — storage, tolerant
// reads, key-removal on default, and the validation chokepoint.

func TestBriefLayerModesOfReadTheRuntimeConfigKeys(t *testing.T) {
	snap := ContextSnapshot{
		RuntimeConfig: json.RawMessage(`{"workspace_brief_mode":"off","tools_brief_mode":"summary"}`),
	}
	if got := WorkspaceBriefModeOf(snap); got != WorkspaceBriefModeOff {
		t.Errorf("workspace: got %q, want %q", got, WorkspaceBriefModeOff)
	}
	if got := ToolsBriefModeOf(snap); got != ToolsBriefModeSummary {
		t.Errorf("tools: got %q, want %q", got, ToolsBriefModeSummary)
	}
}

// An agent that never set a mode must read as the default — the full brief
// every agent had before FIR-3212. Malformed and unknown values degrade the
// same way: never a thinner brief the agent did not ask for.
func TestBriefLayerModesDefaultWhenAbsentOrInvalid(t *testing.T) {
	for name, snap := range map[string]ContextSnapshot{
		"no runtime_config": {},
		"empty object":      {RuntimeConfig: json.RawMessage(`{}`)},
		"other keys only":   {RuntimeConfig: json.RawMessage(`{"mode":"pro"}`)},
		"malformed blob":    {RuntimeConfig: json.RawMessage(`{ not json`)},
		"unknown values":    {RuntimeConfig: json.RawMessage(`{"workspace_brief_mode":"minimal","tools_brief_mode":"tiny"}`)},
		"non-string values": {RuntimeConfig: json.RawMessage(`{"workspace_brief_mode":7,"tools_brief_mode":true}`)},
	} {
		if got := WorkspaceBriefModeOf(snap); got != WorkspaceBriefModeDefault {
			t.Errorf("%s: workspace got %q, want the default", name, got)
		}
		if got := ToolsBriefModeOf(snap); got != ToolsBriefModeDefault {
			t.Errorf("%s: tools got %q, want the default", name, got)
		}
	}
}

// "full" is accepted on write but normalises to the default on read, so both
// spellings behave identically downstream.
func TestBriefLayerModesFullSpellingNormalisesToDefault(t *testing.T) {
	snap := ContextSnapshot{
		RuntimeConfig: json.RawMessage(`{"workspace_brief_mode":"full","tools_brief_mode":"full"}`),
	}
	if got := WorkspaceBriefModeOf(snap); got != WorkspaceBriefModeDefault {
		t.Errorf("workspace: got %q, want the default", got)
	}
	if got := ToolsBriefModeOf(snap); got != ToolsBriefModeDefault {
		t.Errorf("tools: got %q, want the default", got)
	}
}

// The modes share runtime_config with unrelated knobs (openclaw's "mode",
// system_prompt_mode). Writing ours must not disturb them.
func TestWithBriefLayerModesPreserveOtherRuntimeConfigKeys(t *testing.T) {
	snap := ContextSnapshot{RuntimeConfig: json.RawMessage(`{"mode":"pro","system_prompt_mode":"replace"}`)}

	snap, err := WithWorkspaceBriefMode(snap, WorkspaceBriefModeOff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snap, err = WithToolsBriefMode(snap, ToolsBriefModeSummary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(snap.RuntimeConfig, &m); err != nil {
		t.Fatalf("result must be valid JSON: %v", err)
	}
	if m["mode"] != "pro" || m["system_prompt_mode"] != "replace" {
		t.Errorf("unrelated runtime_config keys were lost: %v", m)
	}
	if m[WorkspaceBriefModeKey] != "off" || m[ToolsBriefModeKey] != "summary" {
		t.Errorf("modes not written: %v", m)
	}
}

// Clearing back to the default (either spelling) must remove the key, so an
// agent that never chose a mode and one that chose the default produce
// byte-identical snapshots — otherwise review diffs show a phantom change.
func TestWithBriefLayerModesDefaultRemovesTheKey(t *testing.T) {
	for _, mode := range []string{WorkspaceBriefModeDefault, WorkspaceBriefModeFull} {
		snap := ContextSnapshot{RuntimeConfig: json.RawMessage(`{"workspace_brief_mode":"off","keep":"me"}`)}
		got, err := WithWorkspaceBriefMode(snap, mode)
		if err != nil {
			t.Fatalf("mode %q: unexpected error: %v", mode, err)
		}
		var m map[string]any
		if err := json.Unmarshal(got.RuntimeConfig, &m); err != nil {
			t.Fatalf("result must be valid JSON: %v", err)
		}
		if _, present := m[WorkspaceBriefModeKey]; present {
			t.Errorf("mode %q: key must be removed, got %v", mode, m)
		}
		if m["keep"] != "me" {
			t.Errorf("mode %q: unrelated key lost: %v", mode, m)
		}
	}
}

func TestWithBriefLayerModesRejectUnknownValues(t *testing.T) {
	if _, err := WithWorkspaceBriefMode(ContextSnapshot{}, "minimal"); err == nil {
		t.Error("workspace: unknown mode must be rejected")
	}
	if _, err := WithToolsBriefMode(ContextSnapshot{}, "tiny"); err == nil {
		t.Error("tools: unknown mode must be rejected")
	}
}

// FIR-4500: "off" is a real tools-brief mode, stored as written (not normalised
// away like "full"), so an agent can drop the section on its own and the
// workspace flag has a value to force.
func TestWithToolsBriefModeOffRoundTrips(t *testing.T) {
	snap, err := WithToolsBriefMode(ContextSnapshot{}, ToolsBriefModeOff)
	if err != nil {
		t.Fatalf("WithToolsBriefMode(off): %v", err)
	}
	if got := ToolsBriefModeOf(snap); got != ToolsBriefModeOff {
		t.Errorf("got %q, want %q", got, ToolsBriefModeOff)
	}
}

// The chokepoint catches modes smuggled in via proposed_snapshot or a raw
// runtime_config override — stored-then-silently-dropped is the failure
// FIR-3212 exists to remove. Non-string values must be rejected, not ignored.
func TestValidateSnapshotBriefLayerModes(t *testing.T) {
	for name, tc := range map[string]struct {
		raw     string
		wantErr string
	}{
		"absent keys are fine":   {raw: `{"mode":"pro"}`},
		"valid values are fine":  {raw: `{"workspace_brief_mode":"off","tools_brief_mode":"summary"}`},
		"tools off is fine":      {raw: `{"tools_brief_mode":"off"}`},
		"full spelling is fine":  {raw: `{"workspace_brief_mode":"full"}`},
		"unknown workspace mode": {raw: `{"workspace_brief_mode":"minimal"}`, wantErr: "workspace_brief_mode"},
		"unknown tools mode":     {raw: `{"tools_brief_mode":"tiny"}`, wantErr: "tools_brief_mode"},
		"non-string value":       {raw: `{"workspace_brief_mode":7}`, wantErr: "workspace_brief_mode"},
	} {
		err := ValidateSnapshotBriefLayerModes(ContextSnapshot{RuntimeConfig: json.RawMessage(tc.raw)})
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: unexpected error: %v", name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: got %v, want error mentioning %q", name, err, tc.wantErr)
		}
	}
}

// RenderSnapshot lifts both modes out of the compacted runtime_config blob so a
// reviewer sees "this proposal thins the agent's brief" as its own line.
func TestRenderSnapshotShowsBriefLayerModes(t *testing.T) {
	out := RenderSnapshot(ContextSnapshot{
		RuntimeConfig: json.RawMessage(`{"workspace_brief_mode":"off","tools_brief_mode":"summary"}`),
	})
	if !strings.Contains(out, "workspace_brief_mode: off\n") {
		t.Errorf("workspace mode not rendered:\n%s", out)
	}
	if !strings.Contains(out, "tools_brief_mode: summary\n") {
		t.Errorf("tools mode not rendered:\n%s", out)
	}
}
