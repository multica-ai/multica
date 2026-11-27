package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func ParseOneShotContext(task db.AgentTaskQueue) (OneShotContext, bool) {
	if task.IssueID.Valid || task.ChatSessionID.Valid || task.AutopilotRunID.Valid || task.DocID.Valid {
		return OneShotContext{}, false
	}
	var ctx OneShotContext
	if json.Unmarshal(task.Context, &ctx) != nil || ctx.Type != OneShotContextType {
		return OneShotContext{}, false
	}
	validated, err := NewOneShotContext(ctx.WorkspaceID, ctx.OneShotRunInput)
	return validated, err == nil
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
	for _, value := range append([]string{input.UserPrompt, input.ModelID, input.Effort}, input.SystemPrompt...) {
		if strings.ContainsRune(value, '\x00') {
			return OneShotContext{}, fmt.Errorf("run input must not contain NUL characters")
		}
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

// OneShotOwner is the authenticated submitter, never a caller-supplied identity.
func OneShotOwner(task db.AgentTaskQueue) string {
	if _, ok := ParseOneShotContext(task); ok {
		return util.UUIDToString(task.OriginatorUserID)
	}
	return ""
}
