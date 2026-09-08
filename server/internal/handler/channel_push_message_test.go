package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The ledger is the whole reply half's index: a push that is not findable by
// (installation, platform message id) is a push nobody can reply to.
func TestChannelPushMessageRoundTrip(t *testing.T) {
	ctx := context.Background()
	workspaceID := dbfx.Workspace(t, "Push ledger", "push-ledger-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	issueID := dbfx.Issue(t, "Pushed issue", testutil.Cols{
		"workspace_id": workspaceID, "status": "in_review",
	})
	inboxItemID := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": workspaceID, "recipient_type": "member",
		"recipient_id": testUserID, "type": "status_changed",
		"severity": "info", "issue_id": issueID, "title": "Needs review",
	})
	installationID := uuid.NewString()

	created, err := testHandler.Queries.CreateChannelPushMessage(ctx, db.CreateChannelPushMessageParams{
		InstallationID:   parseUUID(installationID),
		ChannelType:      "lark",
		ChannelMessageID: "om_abc123",
		WorkspaceID:      parseUUID(workspaceID),
		RecipientUserID:  parseUUID(testUserID),
		IssueID:          parseUUID(issueID),
		InboxItemID:      parseUUID(inboxItemID),
	})
	if err != nil {
		t.Fatalf("CreateChannelPushMessage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel_push_message WHERE installation_id = $1`, parseUUID(installationID))
	})
	if uuidToString(created.IssueID) != issueID {
		t.Errorf("created.IssueID = %q, want %q", uuidToString(created.IssueID), issueID)
	}

	found, err := testHandler.Queries.FindChannelPushMessage(ctx, db.FindChannelPushMessageParams{
		InstallationID:   parseUUID(installationID),
		ChannelMessageID: "om_abc123",
	})
	if err != nil {
		t.Fatalf("FindChannelPushMessage: %v", err)
	}
	if uuidToString(found.RecipientUserID) != testUserID {
		t.Errorf("found.RecipientUserID = %q, want %q", uuidToString(found.RecipientUserID), testUserID)
	}

	// A miss must be pgx.ErrNoRows, not a zero row: the router branches on it.
	_, err = testHandler.Queries.FindChannelPushMessage(ctx, db.FindChannelPushMessageParams{
		InstallationID:   parseUUID(installationID),
		ChannelMessageID: "om_not_ours",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("FindChannelPushMessage(miss) error = %v, want pgx.ErrNoRows", err)
	}
}

// A quick-create failure has no issue to inject a reply into. The column is
// nullable precisely for this, and the router uses it to decide replyability.
func TestChannelPushMessageAllowsNullIssue(t *testing.T) {
	ctx := context.Background()
	workspaceID := dbfx.Workspace(t, "Push ledger null issue", "push-null-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	inboxItemID := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": workspaceID, "recipient_type": "member",
		"recipient_id": testUserID, "type": "quick_create_failed",
		"severity": "attention", "title": "Quick create failed",
	})
	installationID := uuid.NewString()

	created, err := testHandler.Queries.CreateChannelPushMessage(ctx, db.CreateChannelPushMessageParams{
		InstallationID:   parseUUID(installationID),
		ChannelType:      "lark",
		ChannelMessageID: "om_null_issue",
		WorkspaceID:      parseUUID(workspaceID),
		RecipientUserID:  parseUUID(testUserID),
		InboxItemID:      parseUUID(inboxItemID),
	})
	if err != nil {
		t.Fatalf("CreateChannelPushMessage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel_push_message WHERE installation_id = $1`, parseUUID(installationID))
	})
	if created.IssueID.Valid {
		t.Errorf("created.IssueID.Valid = true, want false for a push with no issue")
	}
}
