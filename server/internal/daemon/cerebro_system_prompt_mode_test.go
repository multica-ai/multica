package daemon

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// FIR-3212 slice 3: the configured mode must survive the trip from the agent's
// runtime_config to ExecOptions. Slice 1's plumbing is unreachable without it.

func TestDecodeSystemPromptModeReadsConfiguredMode(t *testing.T) {
	for _, mode := range []agent.SystemPromptMode{
		agent.SystemPromptModeAppend,
		agent.SystemPromptModeReplace,
		agent.SystemPromptModePrepend,
	} {
		raw := json.RawMessage(`{"system_prompt_mode":"` + string(mode) + `"}`)
		if got := decodeSystemPromptMode(raw); got != mode {
			t.Errorf("got %q, want %q", got, mode)
		}
	}
}

// Every agent alive today has no mode configured. They must all keep the exact
// behaviour they had before FIR-3212 — this is what makes the slice safe to ship.
func TestDecodeSystemPromptModeDefaultsForUnconfiguredAgents(t *testing.T) {
	for name, raw := range map[string]json.RawMessage{
		"nil runtime_config": nil,
		"empty object":       json.RawMessage(`{}`),
		"malformed blob":     json.RawMessage(`{ not json`),
		"unknown mode":       json.RawMessage(`{"system_prompt_mode":"replace-all"}`),
		"wrong type":         json.RawMessage(`{"system_prompt_mode":7}`),
	} {
		if got := decodeSystemPromptMode(raw); got != agent.SystemPromptModeDefault {
			t.Errorf("%s: got %q, want the default", name, got)
		}
	}
}

// runtime_config is shared with openclaw's own knob. Reading ours must not
// depend on being the only key present.
func TestDecodeSystemPromptModeCoexistsWithOpenclawMode(t *testing.T) {
	raw := json.RawMessage(`{"mode":"pro","system_prompt_mode":"replace"}`)

	if got := decodeSystemPromptMode(raw); got != agent.SystemPromptModeReplace {
		t.Errorf("got %q, want replace", got)
	}
	// The openclaw decoder must still read its own key from the same blob.
	if openclawMode, _ := decodeOpenclawRuntimeConfig(raw, nil); openclawMode != "pro" {
		t.Errorf("openclaw mode was disturbed: got %q, want pro", openclawMode)
	}
}
