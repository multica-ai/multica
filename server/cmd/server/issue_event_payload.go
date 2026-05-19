package main

import "github.com/multica-ai/multica/server/internal/handler"

type issueEventIssue struct {
	ID           string
	WorkspaceID  string
	Title        string
	Description  *string
	Status       string
	Priority     string
	AssigneeType *string
	AssigneeID   *string
	StartDate    *string
	DueDate      *string
}

func issueEventIssueFromPayload(v any) (issueEventIssue, bool) {
	switch issue := v.(type) {
	case handler.IssueResponse:
		return issueEventIssue{
			ID:           issue.ID,
			WorkspaceID:  issue.WorkspaceID,
			Title:        issue.Title,
			Description:  issue.Description,
			Status:       issue.Status,
			Priority:     issue.Priority,
			AssigneeType: issue.AssigneeType,
			AssigneeID:   issue.AssigneeID,
			StartDate:    issue.StartDate,
			DueDate:      issue.DueDate,
		}, true
	case map[string]any:
		return issueEventIssue{
			ID:           stringFromEventMap(issue, "id"),
			WorkspaceID:  stringFromEventMap(issue, "workspace_id"),
			Title:        stringFromEventMap(issue, "title"),
			Description:  stringPtrFromEventMap(issue, "description"),
			Status:       stringFromEventMap(issue, "status"),
			Priority:     stringFromEventMap(issue, "priority"),
			AssigneeType: stringPtrFromEventMap(issue, "assignee_type"),
			AssigneeID:   stringPtrFromEventMap(issue, "assignee_id"),
			StartDate:    stringPtrFromEventMap(issue, "start_date"),
			DueDate:      stringPtrFromEventMap(issue, "due_date"),
		}, true
	default:
		return issueEventIssue{}, false
	}
}

func stringFromEventMap(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func stringPtrFromEventMap(m map[string]any, key string) *string {
	switch v := m[key].(type) {
	case *string:
		return v
	case string:
		return &v
	default:
		return nil
	}
}
