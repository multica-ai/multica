package sprints

import (
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestWorkspaceSprintToResponseIncludesProgress(t *testing.T) {
	got := workspaceSprintToResponse(cerebrodb.ListCerebroSprintsByWorkspaceRow{
		IssueCount: 4,
		DoneCount:  3,
	})

	if got.IssueCount != 4 || got.DoneCount != 3 {
		t.Fatalf("progress = %d/%d, want 3/4", got.DoneCount, got.IssueCount)
	}
}
