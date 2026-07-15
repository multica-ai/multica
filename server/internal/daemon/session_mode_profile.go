package daemon

// CEREBRO-PATCH(session-mode-config-runtime): FIR-3111 resolves the versioned Mode snapshot pinned at claim.

import "github.com/multica-ai/multica/server/internal/cerebro/sessionmode"

func effectiveSessionModeProfile(task Task) sessionmode.Profile {
	mode, valid := sessionmode.Normalize(task.SessionMode)
	if task.SessionMode == "" && task.PlanMode {
		mode, valid = sessionmode.Plan, true
	}
	if valid && task.SessionModeConfig != nil {
		config := *task.SessionModeConfig
		config.Mode = mode
		if sessionmode.ValidateConfig(config) == nil {
			return config.Profile()
		}
	}
	return sessionmode.ProfileFor(mode)
}
