package handler

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	maxPlanLimitWindows       = 8
	maxPlanLimitWindowMinutes = 365 * 24 * 60
)

func validatePlanLimitsSnapshot(snapshot *protocol.PlanLimitsSnapshot, runtimeProvider string) ([]byte, error) {
	if snapshot == nil {
		return nil, nil
	}
	if snapshot.Provider != runtimeProvider {
		return nil, fmt.Errorf("provider does not match runtime")
	}
	if snapshot.Status != protocol.PlanLimitsStatusAvailable && snapshot.Status != protocol.PlanLimitsStatusExhausted {
		return nil, fmt.Errorf("unsupported status")
	}
	if snapshot.ObservedAt <= 0 {
		return nil, fmt.Errorf("observed_at must be positive")
	}
	if len(snapshot.Windows) > maxPlanLimitWindows {
		return nil, fmt.Errorf("too many windows")
	}
	if snapshot.Status == protocol.PlanLimitsStatusAvailable && len(snapshot.Windows) == 0 {
		return nil, fmt.Errorf("available snapshot requires a window")
	}

	seen := make(map[string]struct{}, len(snapshot.Windows))
	for _, window := range snapshot.Windows {
		if !validPlanLimitWindowName(window.Name) {
			return nil, fmt.Errorf("invalid window name")
		}
		if _, exists := seen[window.Name]; exists {
			return nil, fmt.Errorf("duplicate window name")
		}
		seen[window.Name] = struct{}{}
		if window.UsedPercent == nil && window.ResetsAt == nil {
			return nil, fmt.Errorf("window requires usage or reset data")
		}
		if window.UsedPercent != nil && (math.IsNaN(*window.UsedPercent) || math.IsInf(*window.UsedPercent, 0) || *window.UsedPercent < 0 || *window.UsedPercent > 100) {
			return nil, fmt.Errorf("used_percent must be between 0 and 100")
		}
		if window.WindowMinutes != nil && (*window.WindowMinutes <= 0 || *window.WindowMinutes > maxPlanLimitWindowMinutes) {
			return nil, fmt.Errorf("window_minutes is out of range")
		}
		if window.ResetsAt != nil && *window.ResetsAt <= 0 {
			return nil, fmt.Errorf("resets_at must be positive")
		}
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal plan limits: %w", err)
	}
	return data, nil
}

func validPlanLimitWindowName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if name != trimmed || name == "" || len(name) > 32 {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
