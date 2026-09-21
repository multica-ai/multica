package service

import (
	"encoding/json"
)

// DefaultAgentTaskMaxAttempts is the platform default for agent_task_queue.max_attempts
// on newly enqueued root tasks (first run + three auto-retries for retryable
// infrastructure failures). Matches migration 368_agent_task_max_attempts_default_4.
const DefaultAgentTaskMaxAttempts int32 = 4

// AgentTaskMaxAttemptsSettingFloor/Ceiling bound workspace.settings.agent_task.max_attempts.
const (
	AgentTaskMaxAttemptsSettingFloor   int32 = 1
	AgentTaskMaxAttemptsSettingCeiling int32 = 10
)

// ParseAgentTaskMaxAttemptsSetting reads workspace.settings JSON and returns
// the configured agent-task max_attempts when present and in [1,10].
// ok=false means "use the column/platform default".
//
// Schema:
//
//	{"agent_task":{"max_attempts":4}}
//
// The DB trigger agent_task_apply_workspace_max_attempts enforces the same key
// at insert time; this helper exists so API/docs/tests share one contract.
func ParseAgentTaskMaxAttemptsSetting(settingsJSON []byte) (maxAttempts int32, ok bool) {
	if len(settingsJSON) == 0 {
		return 0, false
	}
	var s struct {
		AgentTask *struct {
			MaxAttempts *int `json:"max_attempts"`
		} `json:"agent_task"`
	}
	if err := json.Unmarshal(settingsJSON, &s); err != nil || s.AgentTask == nil || s.AgentTask.MaxAttempts == nil {
		return 0, false
	}
	v := int32(*s.AgentTask.MaxAttempts)
	if v < AgentTaskMaxAttemptsSettingFloor || v > AgentTaskMaxAttemptsSettingCeiling {
		return 0, false
	}
	return v, true
}
