package daemon

import (
	"encoding/json"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/sessionmode"
)

// applyAgentRuntimeProfileOverrides applies the agent's versioned run caps
// after the selected session Mode profile has been resolved. Missing, malformed
// or non-positive values inherit the profile unchanged.
func applyAgentRuntimeProfileOverrides(task Task, profile sessionmode.Profile) sessionmode.Profile {
	if task.Agent == nil || len(task.Agent.RuntimeConfig) == 0 {
		return profile
	}
	var config struct {
		MaxTurns       int `json:"max_turns"`
		TimeoutMinutes int `json:"timeout_minutes"`
	}
	if json.Unmarshal(task.Agent.RuntimeConfig, &config) != nil {
		return profile
	}
	if config.MaxTurns > 0 {
		profile.MaxTurns = config.MaxTurns
	}
	if config.TimeoutMinutes > 0 {
		profile.Timeout = time.Duration(config.TimeoutMinutes) * time.Minute
	}
	return profile
}
