package daemon

import "encoding/json"

func speedModeForTask(task Task) string {
	if task.Agent == nil || len(task.Agent.RuntimeConfig) == 0 {
		return ""
	}
	var config struct {
		SpeedMode string `json:"speed_mode"`
	}
	if err := json.Unmarshal(task.Agent.RuntimeConfig, &config); err != nil {
		return ""
	}
	if config.SpeedMode != "fast" {
		return ""
	}
	return config.SpeedMode
}

// speedModeForExec avoids passing a second Claude --settings value when the
// daemon already merged fastMode into the tool-policy settings document.
func speedModeForExec(speedMode, provider string, toolPolicy *toolPolicySpawn) string {
	if speedMode == "fast" && provider == "claude" && toolPolicy != nil && toolPolicy.SettingsPath != "" {
		return ""
	}
	return speedMode
}
