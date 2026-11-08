package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
)

// Kiro CLI v2 exposes reasoning metadata through the same model options
// extension its TUI uses, rather than ACP configOptions. Observed on 2.34.0.
func enrichKiroModelThinking(ctx context.Context, request acpRequestFn, models []Model, session json.RawMessage) {
	sessionID := extractACPSessionID(session)
	if sessionID == "" {
		return
	}
	raw, err := request(ctx, "_kiro.dev/commands/options", map[string]any{
		"sessionId": sessionID,
		"command":   "model",
	})
	if err != nil {
		return
	}
	thinking, err := parseKiroModelThinking(raw)
	if err != nil {
		return
	}
	for i := range models {
		models[i].Thinking = thinking[models[i].ID]
	}
}

func parseKiroModelThinking(raw json.RawMessage) (map[string]*ModelThinking, error) {
	var result struct {
		Options []struct {
			Value     string `json:"value"`
			Reasoning struct {
				EffortLevels  []string `json:"effortLevels"`
				DefaultEffort string   `json:"defaultEffort"`
			} `json:"reasoning"`
		} `json:"options"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	byModel := make(map[string]*ModelThinking)
	for _, option := range result.Options {
		id := strings.TrimSpace(option.Value)
		if id == "" {
			continue
		}
		thinking := &ModelThinking{}
		seen := make(map[string]bool)
		for _, value := range option.Reasoning.EffortLevels {
			value = strings.TrimSpace(value)
			// `default` is an internal schema value, not a selectable effort.
			if value == "" || value == "default" || seen[value] || !isValidDynamicThinkingValue(value) {
				continue
			}
			seen[value] = true
			thinking.SupportedLevels = append(thinking.SupportedLevels, ThinkingLevel{
				Value: value,
				Label: strings.Title(value), //nolint:staticcheck
			})
		}
		if len(thinking.SupportedLevels) == 0 {
			continue
		}
		if value := strings.TrimSpace(option.Reasoning.DefaultEffort); seen[value] {
			thinking.DefaultLevel = value
		}
		byModel[id] = thinking
	}
	return byModel, nil
}

// Apply to the live session after model selection, including resumed sessions.
// The effort command changes this session without saving a CLI-wide default.
func applyKiroEffort(ctx context.Context, request acpRequestFn, logger *slog.Logger, sessionID, level string) {
	if level == "" {
		return
	}
	raw, err := request(ctx, "_kiro.dev/commands/execute", map[string]any{
		"sessionId": sessionID,
		"command": map[string]any{
			"command": "effort",
			"args":    map[string]any{"level": level},
		},
	})
	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	if err != nil || !result.Success {
		logger.Warn("kiro did not apply the requested reasoning effort; sending the prompt anyway",
			"requested_level", level, "error", err, "message", result.Message)
		return
	}
	logger.Info("kiro session reasoning effort applied", "level", level)
}
