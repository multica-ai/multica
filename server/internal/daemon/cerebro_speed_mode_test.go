package daemon

import (
	"encoding/json"
	"testing"
)

func TestSpeedModeForTaskReadsRuntimeConfig(t *testing.T) {
	task := Task{Agent: &AgentData{RuntimeConfig: json.RawMessage(`{"speed_mode":"fast","system_prompt_mode":"replace"}`)}}
	if got := speedModeForTask(task); got != "fast" {
		t.Fatalf("speed mode = %q, want fast", got)
	}
}

func TestSpeedModeForTaskDefaultsSafely(t *testing.T) {
	tests := []Task{
		{},
		{Agent: &AgentData{}},
		{Agent: &AgentData{RuntimeConfig: json.RawMessage(`{"speed_mode":"turbo"}`)}},
		{Agent: &AgentData{RuntimeConfig: json.RawMessage(`{not json`)}},
	}
	for _, task := range tests {
		if got := speedModeForTask(task); got != "" {
			t.Fatalf("speed mode = %q, want empty default", got)
		}
	}
}

func TestSpeedModeForExecUsesMergedClaudeSettings(t *testing.T) {
	toolPolicy := &toolPolicySpawn{SettingsPath: "/tmp/settings.json"}
	if got := speedModeForExec("fast", "claude", toolPolicy); got != "" {
		t.Fatalf("Claude exec speed = %q, want empty because settings already contain fastMode", got)
	}
	if got := speedModeForExec("fast", "codex", toolPolicy); got != "fast" {
		t.Fatalf("Codex exec speed = %q, want fast", got)
	}
}
