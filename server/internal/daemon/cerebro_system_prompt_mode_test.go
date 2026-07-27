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

// A task can reach runTask with no agent data at all: the claim path in
// handler/daemon.go only sets resp.Agent when GetAgent succeeds, so a transient
// database error hands the daemon a task whose Agent is nil. Every other
// agent-derived ExecOptions field guards for that; the mode must too, or one
// failed lookup panics the whole daemon process instead of the single task.
func TestSystemPromptModeForTaskWithoutAgentData(t *testing.T) {
	if got := systemPromptModeForTask(Task{}); got != agent.SystemPromptModeDefault {
		t.Errorf("got %q, want the default", got)
	}
}

func TestSystemPromptModeForTaskReadsAgentRuntimeConfig(t *testing.T) {
	task := Task{Agent: &AgentData{RuntimeConfig: json.RawMessage(`{"system_prompt_mode":"replace"}`)}}

	if got := systemPromptModeForTask(task); got != agent.SystemPromptModeReplace {
		t.Errorf("got %q, want replace", got)
	}
}

// The mode reaching ExecOptions is only half the trip. Until the brief itself
// travels inline, ClaudeSystemPromptArgs gets an empty prompt and emits no flag
// at all — so a configured mode was stored, versioned and rendered while
// changing nothing at run time. This is the regression guard for that gap.
func TestNeedsInlineRuntimeBriefForConfiguredMode(t *testing.T) {
	for name, tc := range map[string]struct {
		provider string
		mode     agent.SystemPromptMode
		want     bool
	}{
		// The reason FIR-3212 was raised: replace the full Claude Code
		// instruction. Before this predicate the answer here was false.
		"claude replaces its own prompt": {"claude", agent.SystemPromptModeReplace, true},
		"claude appends":                 {"claude", agent.SystemPromptModeAppend, true},
		"codex replaces":                 {"codex", agent.SystemPromptModeReplace, true},
		"pi appends":                     {"pi", agent.SystemPromptModeAppend, true},

		// Unconfigured agents — the whole live fleet today — must not change.
		"claude default": {"claude", agent.SystemPromptModeDefault, false},
		"codex default":  {"codex", agent.SystemPromptModeDefault, false},

		// A mode the provider cannot honour must not drag the brief inline:
		// the run would carry the payload and still not do what was asked.
		"claude cannot prepend": {"claude", agent.SystemPromptModePrepend, false},
		"codex cannot append":   {"codex", agent.SystemPromptModeAppend, false},

		// No authoritative entry means unknown, never "inject and hope".
		"uncatalogued provider": {"brand-new-cli", agent.SystemPromptModeReplace, false},

		// Providers that ignore the system prompt entirely.
		"gemini ignores it": {"gemini", agent.SystemPromptModeReplace, false},
	} {
		if got := needsInlineRuntimeBrief(tc.provider, tc.mode); got != tc.want {
			t.Errorf("%s: needsInlineRuntimeBrief(%q, %q) = %v, want %v", name, tc.provider, tc.mode, got, tc.want)
		}
	}
}

// The five providers that cannot be trusted to read the workdir config file got
// the brief inline before this change and must keep getting it, mode or no mode.
func TestNeedsInlineRuntimeBriefKeepsFileUnreliableProviders(t *testing.T) {
	for _, provider := range []string{"openclaw", "opencode", "hermes", "kiro", "kimi"} {
		if !needsInlineRuntimeBrief(provider, agent.SystemPromptModeDefault) {
			t.Errorf("%s lost its inline brief", provider)
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
