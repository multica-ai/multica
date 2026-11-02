package lark

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestClaimChannelIssueCommentNotificationFirstAttemptSucceeds(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()
	commentID := uuid.NewString()
	issueID := uuid.NewString()
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_issue_comment_notification WHERE comment_id = $1`, commentID)
	}
	cleanup()
	t.Cleanup(cleanup)

	claimed, err := db.New(pool).ClaimChannelIssueCommentNotification(ctx, db.ClaimChannelIssueCommentNotificationParams{
		CommentID: util.MustParseUUID(commentID),
		IssueID:   util.MustParseUUID(issueID),
	})
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if claimed.CommentID != util.MustParseUUID(commentID) || claimed.IssueID != util.MustParseUUID(issueID) || !claimed.ClaimedAt.Valid {
		t.Fatalf("first claim returned %+v, want the inserted row with claimed_at", claimed)
	}
}