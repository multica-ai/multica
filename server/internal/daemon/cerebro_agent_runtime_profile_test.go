package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/sessionmode"
)

func TestAgentRuntimeProfileOverridesApplyAfterSessionMode(t *testing.T) {
	profile := sessionmode.Profile{MaxTurns: 80, Timeout: 120 * time.Minute}
	task := Task{Agent: &AgentData{RuntimeConfig: json.RawMessage(`{
		"max_turns":18,
		"timeout_minutes":42
	}`)}}
	got := applyAgentRuntimeProfileOverrides(task, profile)
	if got.MaxTurns != 18 || got.Timeout != 42*time.Minute {
		t.Fatalf("profile = %+v", got)
	}
}

func TestAgentRuntimeProfileOverridesInheritOnMissingOrInvalidValues(t *testing.T) {
	base := sessionmode.Profile{MaxTurns: 80, Timeout: 120 * time.Minute}
	for _, task := range []Task{
		{},
		{Agent: &AgentData{}},
		{Agent: &AgentData{RuntimeConfig: json.RawMessage(`{not json`)}},
		{Agent: &AgentData{RuntimeConfig: json.RawMessage(`{"max_turns":0,"timeout_minutes":-2}`)}},
	} {
		got := applyAgentRuntimeProfileOverrides(task, base)
		if got.MaxTurns != base.MaxTurns || got.Timeout != base.Timeout {
			t.Fatalf("profile = %+v, want inherited %+v", got, base)
		}
	}
}
