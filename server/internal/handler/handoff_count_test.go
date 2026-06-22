package handler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestHandoffRecordExcludedFromNewCommentCount verifies a type='handoff' row
// does NOT inflate CountNewCommentsSince — the count that tells a claiming agent
// how much conversation it must catch up on (MUL-3375 §12). A normal member
// comment counts; a handoff record does not.
func TestHandoffRecordExcludedFromNewCommentCount(t *testing.T) {
	ctx := context.Background()
	agentID := seededReadyAgentID(t)
	issue := createIssueForTest(t, map[string]any{"title": "handoff count", "status": "todo"})

	issueUUID := parseUUID(issue.ID)
	wsUUID := parseUUID(testWorkspaceID)
	since := pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}

	countParams := db.CountNewCommentsSinceParams{
		IssueID:     issueUUID,
		WorkspaceID: wsUUID,
		Since:       since,
		// A valid anchor that matches no real comment id. The query uses
		// `id <> @anchor_id`, and SQL `id <> NULL` is NULL (excludes every row),
		// so the production caller always passes a real anchor — mirror that.
		AnchorID: parseUUID("00000000-0000-0000-0000-000000000001"),
		AuthorID: parseUUID(agentID),
	}

	// A normal member comment is counted.
	if _, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issueUUID,
		WorkspaceID: wsUUID,
		AuthorType:  "member",
		AuthorID:    parseUUID(testUserID),
		Content:     "real conversation",
		Type:        "comment",
	}); err != nil {
		t.Fatalf("create member comment: %v", err)
	}
	afterReal, err := testHandler.Queries.CountNewCommentsSince(ctx, countParams)
	if err != nil {
		t.Fatalf("count after real comment: %v", err)
	}
	if afterReal != 1 {
		t.Fatalf("expected 1 new comment after a real comment, got %d", afterReal)
	}

	// A handoff record must NOT bump the count.
	testHandler.TaskService.RecordHandoff(ctx, db.Issue{
		ID:          issueUUID,
		WorkspaceID: wsUUID,
		Title:       "handoff count",
	}, "member", testUserID, "scope to login only")

	afterHandoff, err := testHandler.Queries.CountNewCommentsSince(ctx, countParams)
	if err != nil {
		t.Fatalf("count after handoff: %v", err)
	}
	if afterHandoff != 1 {
		t.Fatalf("handoff record must not inflate new-comment count: expected 1, got %d", afterHandoff)
	}
}
