package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
)

func TestIssueUpdateForSideEffects(t *testing.T) {
	t.Run("accepts normal handler payload", func(t *testing.T) {
		want := handler.IssueResponse{ID: "issue-1", WorkspaceID: "workspace-1", Status: "in_review"}
		got, ok := issueUpdateForSideEffects(map[string]any{"issue": want})
		if !ok || got.ID != want.ID || got.WorkspaceID != want.WorkspaceID || got.Status != want.Status {
			t.Fatalf("issueUpdateForSideEffects() = %+v, %v; want %+v, true", got, ok, want)
		}
	})

	t.Run("accepts marked task completion fallback", func(t *testing.T) {
		got, ok := issueUpdateForSideEffects(map[string]any{
			service.TaskCompletionStatusFallbackField: true,
			"issue": map[string]any{
				"id":           "issue-1",
				"workspace_id": "workspace-1",
				"status":       "in_review",
			},
		})
		if !ok || got.ID != "issue-1" || got.WorkspaceID != "workspace-1" || got.Status != "in_review" {
			t.Fatalf("issueUpdateForSideEffects() = %+v, %v", got, ok)
		}
	})

	t.Run("keeps unmarked service maps realtime only", func(t *testing.T) {
		_, ok := issueUpdateForSideEffects(map[string]any{
			"issue": map[string]any{
				"id":           "issue-1",
				"workspace_id": "workspace-1",
				"status":       "todo",
			},
		})
		if ok {
			t.Fatal("unmarked service map unexpectedly enabled side effects")
		}
	})
}
