package daemon

import (
	"encoding/json"
	"testing"
)

// FIR-3212: daemon-side decode of the brief-layer modes — the read half of the
// values agentoffice writes into runtime_config.

func TestBriefLayerModesForTaskReadsAgentRuntimeConfig(t *testing.T) {
	task := Task{Agent: &AgentData{
		RuntimeConfig: json.RawMessage(`{"workspace_brief_mode":"off","tools_brief_mode":"summary"}`),
	}}
	ws, tools := briefLayerModesForTask(task)
	if ws != "off" || tools != "summary" {
		t.Errorf("got (%q, %q), want (off, summary)", ws, tools)
	}
}

// A task dispatched without agent data (transient GetAgent failure on the claim
// path) must decode to the defaults, not panic the daemon process.
func TestBriefLayerModesForTaskNilAgentSafe(t *testing.T) {
	ws, tools := briefLayerModesForTask(Task{})
	if ws != "" || tools != "" {
		t.Errorf("got (%q, %q), want defaults", ws, tools)
	}
}

// FIR-4500: the workspace-level cerebro_tools_brief verdict arrives as
// Task.ToolsBriefDisabled and forces the tools section off. It wins over the
// agent's own tools_brief_mode and leaves the workspace-brief mode untouched.
func TestBriefLayerModesForTaskWorkspaceFlagForcesToolsOff(t *testing.T) {
	task := Task{
		ToolsBriefDisabled: true,
		Agent: &AgentData{
			RuntimeConfig: json.RawMessage(`{"workspace_brief_mode":"off","tools_brief_mode":"summary"}`),
		},
	}
	ws, tools := briefLayerModesForTask(task)
	if ws != "off" || tools != "off" {
		t.Errorf("got (%q, %q), want (off, off)", ws, tools)
	}
}

// The same verdict must survive a task dispatched without agent data — the
// workspace said "do not ship this section", which cannot depend on an agent
// lookup having succeeded.
func TestBriefLayerModesForTaskWorkspaceFlagOffNilAgent(t *testing.T) {
	ws, tools := briefLayerModesForTask(Task{ToolsBriefDisabled: true})
	if ws != "" || tools != "off" {
		t.Errorf("got (%q, %q), want (\"\", off)", ws, tools)
	}
}

// Absent, malformed, and unknown values all decode to the full-brief default —
// an agent is never silently switched to a thinner brief it did not ask for.
func TestBriefLayerModesForTaskDefaultsOnBadValues(t *testing.T) {
	for name, raw := range map[string]string{
		"empty object":   `{}`,
		"malformed blob": `{ not json`,
		"unknown values": `{"workspace_brief_mode":"minimal","tools_brief_mode":"tiny"}`,
	} {
		task := Task{Agent: &AgentData{RuntimeConfig: json.RawMessage(raw)}}
		ws, tools := briefLayerModesForTask(task)
		if ws != "" || tools != "" {
			t.Errorf("%s: got (%q, %q), want defaults", name, ws, tools)
		}
	}
}
