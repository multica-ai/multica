package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/handler"
)

func TestIssueEventIssueFromPayload(t *testing.T) {
	assigneeType := "agent"
	assigneeID := "agent-1"

	tests := []struct {
		name string
		in   any
	}{
		{
			name: "handler response",
			in: handler.IssueResponse{
				ID:           "issue-1",
				WorkspaceID:  "workspace-1",
				Title:        "Runtime failed",
				Status:       "blocked",
				Priority:     "high",
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
		},
		{
			name: "service map",
			in: map[string]any{
				"id":            "issue-1",
				"workspace_id":  "workspace-1",
				"title":         "Runtime failed",
				"status":        "blocked",
				"priority":      "high",
				"assignee_type": &assigneeType,
				"assignee_id":   &assigneeID,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := issueEventIssueFromPayload(tt.in)
			if !ok {
				t.Fatal("expected payload to be recognized")
			}
			if got.ID != "issue-1" || got.WorkspaceID != "workspace-1" {
				t.Fatalf("unexpected ids: %+v", got)
			}
			if got.Status != "blocked" || got.Priority != "high" {
				t.Fatalf("unexpected status/priority: %+v", got)
			}
			if got.AssigneeType == nil || *got.AssigneeType != assigneeType {
				t.Fatalf("unexpected assignee type: %+v", got.AssigneeType)
			}
			if got.AssigneeID == nil || *got.AssigneeID != assigneeID {
				t.Fatalf("unexpected assignee id: %+v", got.AssigneeID)
			}
		})
	}
}
