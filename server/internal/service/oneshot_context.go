package service

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

const OneShotContextType = "oneshot"

type OneShotRunInput struct {
	RuntimeID    string   `json:"runtime_id"`
	ModelID      string   `json:"model_id,omitempty"`
	Effort       string   `json:"effort,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	SystemPrompt []string `json:"system_prompt,omitempty"`
	UserPrompt   string   `json:"user_prompt"`
}

type OneShotContext struct {
	Type        string `json:"type"`
	WorkspaceID string `json:"workspace_id"`
	OneShotRunInput
}

// NewOneShotContext captures job input without changing prompt bytes. Resource
// authorization belongs to submission and claim, not identifier validation.
func NewOneShotContext(workspaceID string, input OneShotRunInput) (OneShotContext, error) {
	workspace, err := oneShotUUID(workspaceID, "workspace_id")
	if err != nil {
		return OneShotContext{}, err
	}
	runtime, err := oneShotUUID(input.RuntimeID, "runtime_id")
	if err != nil {
		return OneShotContext{}, err
	}
	if strings.TrimSpace(input.UserPrompt) == "" {
		return OneShotContext{}, fmt.Errorf("user_prompt is required")
	}
	var skills []string
	seen := make(map[string]struct{}, len(input.Skills))
	for i, id := range input.Skills {
		skill, err := oneShotUUID(id, fmt.Sprintf("skills[%d]", i))
		if err != nil {
			return OneShotContext{}, err
		}
		if _, exists := seen[skill]; !exists {
			seen[skill] = struct{}{}
			skills = append(skills, skill)
		}
	}
	input.RuntimeID = runtime
	input.Skills = skills
	input.SystemPrompt = append([]string(nil), input.SystemPrompt...)
	return OneShotContext{
		Type: OneShotContextType, WorkspaceID: workspace, OneShotRunInput: input,
	}, nil
}

func oneShotUUID(value, field string) (string, error) {
	id, err := util.ParseUUID(value)
	if err != nil {
		return "", fmt.Errorf("%s must be a UUID", field)
	}
	return util.UUIDToString(id), nil
}
