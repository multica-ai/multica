package agentoffice

import (
	"encoding/json"
	"strings"
	"testing"

	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
)

// FIR-3212 slice 3: the system-prompt delivery mode becomes real configuration.
//
// Slice 1 put SystemPromptMode on agent.ExecOptions and taught claude.go to act
// on it, but nothing could ever set it — the field had no configured source, so
// every run got the empty default. These tests pin the config half.
//
// The mode lives in the agent's runtime_config JSONB, which is already the home
// of the only other per-agent ExecOptions knob (OpenclawMode, decoded in
// daemon/openclaw_runtime_config.go) and is already part of the versioned
// composite. That gives versioning, change requests, and rollback for free.

func TestSystemPromptModeOfReadsTheRuntimeConfigKey(t *testing.T) {
	snap := ContextSnapshot{
		RuntimeConfig: json.RawMessage(`{"system_prompt_mode":"replace"}`),
	}
	if got := SystemPromptModeOf(snap); got != string(agentpkg.SystemPromptModeReplace) {
		t.Errorf("got %q, want %q", got, agentpkg.SystemPromptModeReplace)
	}
}

// An agent that never set a mode must read as the default — "leave it to the
// backend" — which is exactly the behaviour every agent had before FIR-3212.
func TestSystemPromptModeOfDefaultsWhenAbsent(t *testing.T) {
	for name, snap := range map[string]ContextSnapshot{
		"no runtime_config": {},
		"empty object":      {RuntimeConfig: json.RawMessage(`{}`)},
		"other keys only":   {RuntimeConfig: json.RawMessage(`{"mode":"pro"}`)},
		"malformed blob":    {RuntimeConfig: json.RawMessage(`{ not json`)},
	} {
		if got := SystemPromptModeOf(snap); got != string(agentpkg.SystemPromptModeDefault) {
			t.Errorf("%s: got %q, want the default", name, got)
		}
	}
}

// The mode shares runtime_config with unrelated knobs (openclaw's "mode" is the
// live example). Writing ours must not disturb them.
func TestWithSystemPromptModePreservesOtherRuntimeConfigKeys(t *testing.T) {
	snap := ContextSnapshot{RuntimeConfig: json.RawMessage(`{"mode":"pro","gateway":"eu"}`)}

	got, err := WithSystemPromptMode(snap, string(agentpkg.SystemPromptModeAppend))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(got.RuntimeConfig, &m); err != nil {
		t.Fatalf("result must be valid JSON: %v", err)
	}
	if m["mode"] != "pro" || m["gateway"] != "eu" {
		t.Errorf("unrelated runtime_config keys were lost: %v", m)
	}
	if m["system_prompt_mode"] != "append" {
		t.Errorf("mode not written: %v", m)
	}
}

// Clearing back to the default must remove the key rather than store an empty
// string, so a snapshot of an agent that never set a mode and one that set it
// back to default are byte-identical — otherwise the diff shows a phantom change.
func TestWithSystemPromptModeDefaultRemovesTheKey(t *testing.T) {
	snap := ContextSnapshot{RuntimeConfig: json.RawMessage(`{"system_prompt_mode":"replace","mode":"pro"}`)}

	got, err := WithSystemPromptMode(snap, string(agentpkg.SystemPromptModeDefault))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]any
	_ = json.Unmarshal(got.RuntimeConfig, &m)
	if _, present := m["system_prompt_mode"]; present {
		t.Errorf("clearing to default must drop the key, got %v", m)
	}
	if m["mode"] != "pro" {
		t.Errorf("unrelated key lost while clearing: %v", m)
	}
}

// The honesty requirement: a mode no backend understands must be rejected at
// write time, not stored, versioned, shown in review and then silently dropped
// at run time.
func TestWithSystemPromptModeRejectsUnknownVocabulary(t *testing.T) {
	for _, mode := range []string{"replace-all", "APPEND", "yes", "none", "0"} {
		if _, err := WithSystemPromptMode(ContextSnapshot{}, mode); err == nil {
			t.Errorf("mode %q is not real and must be rejected", mode)
		}
	}
}

func TestValidSystemPromptModeAcceptsTheKnownVocabulary(t *testing.T) {
	for _, mode := range []string{"", "append", "replace", "prepend"} {
		if !ValidSystemPromptMode(mode) {
			t.Errorf("mode %q is part of the vocabulary and must validate", mode)
		}
	}
}

// The validation gate reads through rawSystemPromptMode so it can tell "no mode
// set" (nothing to check) from "a mode we do not understand" (reject). If this
// collapsed to the default like SystemPromptModeOf does, a proposed_snapshot
// carrying a bogus mode would sail through review and die silently at run time.
func TestRawSystemPromptModeDistinguishesUnsetFromUnknown(t *testing.T) {
	cases := []struct {
		name      string
		cfg       string
		wantMode  string
		wantFound bool
	}{
		{"absent", `{"mode":"pro"}`, "", false},
		{"no runtime_config", ``, "", false},
		{"valid", `{"system_prompt_mode":"replace"}`, "replace", true},
		{"unknown but present", `{"system_prompt_mode":"replace-all"}`, "replace-all", true},
		{"wrong type but present", `{"system_prompt_mode":7}`, "7", true},
	}
	for _, tc := range cases {
		snap := ContextSnapshot{}
		if tc.cfg != "" {
			snap.RuntimeConfig = json.RawMessage(tc.cfg)
		}
		mode, found := rawSystemPromptMode(snap)
		if found != tc.wantFound || mode != tc.wantMode {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.name, mode, found, tc.wantMode, tc.wantFound)
		}
	}
}

// A reviewer approving a change request reads RenderSnapshot's output. A knob
// that changes how the entire system prompt reaches the model must not be
// legible only as a nested key inside a compacted JSON blob.
func TestRenderSnapshotShowsSystemPromptModeOnItsOwnLine(t *testing.T) {
	snap := ContextSnapshot{RuntimeConfig: json.RawMessage(`{"system_prompt_mode":"replace"}`)}
	if out := RenderSnapshot(snap); !strings.Contains(out, "system_prompt_mode: replace") {
		t.Errorf("render must surface the prompt mode, got:\n%s", out)
	}
}

func TestDiffSnapshotsCatchesSystemPromptModeChange(t *testing.T) {
	base := ContextSnapshot{RuntimeConfig: json.RawMessage(`{"system_prompt_mode":"append"}`)}
	proposed := ContextSnapshot{RuntimeConfig: json.RawMessage(`{"system_prompt_mode":"replace"}`)}

	diff := DiffSnapshots(base, proposed)
	if !strings.Contains(diff, "system_prompt_mode: replace") {
		t.Errorf("a prompt-mode flip must appear in the review diff, got:\n%s", diff)
	}
}
