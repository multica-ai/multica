package issuepolicy

import (
	"errors"
	"strings"
)

var (
	ErrAgentIssueCreation   = errors.New("agents cannot create additional issues from an issue-bound run")
	ErrAgentTerminalStatus  = errors.New("agents cannot set blocked, done, or cancelled status")
	ErrAgentHierarchyChange = errors.New("agents cannot create or modify issue hierarchy")
)

func ValidateCreate(actorType, originType string, hasParent bool) error {
	if actorType != "agent" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(originType), "quick_create") && !hasParent {
		return nil
	}
	return ErrAgentIssueCreation
}

func ValidateStatus(actorType, status string) error {
	if actorType != "agent" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "backlog", "todo", "in_progress", "in_review":
		return nil
	default:
		return ErrAgentTerminalStatus
	}
}

func ValidateHierarchyChange(actorType string, touched bool) error {
	if actorType == "agent" && touched {
		return ErrAgentHierarchyChange
	}
	return nil
}
