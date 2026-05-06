package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// insertInboxItem is a small fixture helper for the unread-count tests. The
// existing inbox-listener tests build items via the event bus, but the count
// query is purely about row state (read/archived/route) and inserting the
// rows directly keeps the test focused on the count semantics.
func insertInboxItem(t *testing.T, workspaceID, recipientID, issueID, route string, read, archived bool) {
	t.Helper()
	ctx := context.Background()
	_, err := testPool.Exec(ctx, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id,
			type, severity, issue_id, title, route, read, archived)
		VALUES ($1, 'member', $2, 'new_comment', 'info', $3,
		        'unread-count fixture', $4, $5, $6)
	`, workspaceID, recipientID, issueID, route, read, archived)
	if err != nil {
		t.Fatalf("insertInboxItem: %v", err)
	}
}

// TestCountUnreadInboxForUserAllWorkspaces verifies the cross-workspace
// unread inbox count powering the OS app-icon badge: only unread, non-
// archived, route='inbox' rows count, and rows from every workspace the user
// belongs to are summed regardless of which workspace context the caller is
// in. (The badge is single-icon and OS-level, so workspace scope would be
// the wrong unit.)
func TestCountUnreadInboxForUserAllWorkspaces(t *testing.T) {
	queries := db.New(testPool)
	ctx := context.Background()

	// Recipient lives in the existing test workspace + a freshly minted
	// second workspace. Items in *both* must be counted.
	recipientEmail := "badge-count-recipient@multica.ai"
	recipientID := createTestUser(t, recipientEmail)
	t.Cleanup(func() { cleanupTestUser(t, recipientEmail) })

	otherWorkspaceID := createSecondaryTestWorkspace(t, recipientID)
	t.Cleanup(func() { cleanupSecondaryTestWorkspace(t, otherWorkspaceID) })

	// Issue rows for the FK on inbox_item.issue_id. Workspace doesn't matter
	// for the count query, but the FK requires a real issue.
	issueA := createTestIssue(t, testWorkspaceID, testUserID)
	issueB := createTestIssue(t, otherWorkspaceID, recipientID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueA)
		cleanupInboxForIssue(t, issueB)
		cleanupTestIssue(t, issueA)
		cleanupTestIssue(t, issueB)
	})

	// Workspace A: 2 unread inbox + 1 read + 1 archived + 1 notifications-route.
	insertInboxItem(t, testWorkspaceID, recipientID, issueA, "inbox", false, false)
	insertInboxItem(t, testWorkspaceID, recipientID, issueA, "inbox", false, false)
	insertInboxItem(t, testWorkspaceID, recipientID, issueA, "inbox", true, false)
	insertInboxItem(t, testWorkspaceID, recipientID, issueA, "inbox", false, true)
	insertInboxItem(t, testWorkspaceID, recipientID, issueA, "notifications", false, false)
	// Workspace B: 3 unread inbox.
	insertInboxItem(t, otherWorkspaceID, recipientID, issueB, "inbox", false, false)
	insertInboxItem(t, otherWorkspaceID, recipientID, issueB, "inbox", false, false)
	insertInboxItem(t, otherWorkspaceID, recipientID, issueB, "inbox", false, false)

	count, err := queries.CountUnreadInboxForUserAllWorkspaces(ctx, util.ParseUUID(recipientID))
	if err != nil {
		t.Fatalf("CountUnreadInboxForUserAllWorkspaces: %v", err)
	}
	const want = int64(5)
	if count != want {
		t.Fatalf("count = %d, want %d (2 unread inbox in A + 3 in B; read/archived/notifications excluded)", count, want)
	}

	// Sanity check: a different user has 0.
	otherEmail := "badge-count-bystander@multica.ai"
	otherID := createTestUser(t, otherEmail)
	t.Cleanup(func() { cleanupTestUser(t, otherEmail) })
	otherCount, err := queries.CountUnreadInboxForUserAllWorkspaces(ctx, util.ParseUUID(otherID))
	if err != nil {
		t.Fatalf("CountUnreadInboxForUserAllWorkspaces (bystander): %v", err)
	}
	if otherCount != 0 {
		t.Fatalf("bystander count = %d, want 0", otherCount)
	}
}

// createSecondaryTestWorkspace inserts a workspace that the given user is a
// member of. Used to build cross-workspace inbox state for badge tests.
func createSecondaryTestWorkspace(t *testing.T, userID string) string {
	t.Helper()
	ctx := context.Background()
	var workspaceID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('badge-count-secondary', 'badge-count-secondary-' || substr(gen_random_uuid()::text, 1, 8))
		RETURNING id
	`).Scan(&workspaceID)
	if err != nil {
		t.Fatalf("createSecondaryTestWorkspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create membership: %v", err)
	}
	return workspaceID
}

func cleanupSecondaryTestWorkspace(t *testing.T, workspaceID string) {
	t.Helper()
	testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
}
