package main

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// insertInboxItem inserts an inbox row at a fixed created_at so the dedup
// "latest row per issue wins" rule has a deterministic outcome in tests.
// The query under test uses DISTINCT ON (COALESCE(issue_id, id)) ORDER BY
// created_at DESC, so test fixtures need explicit ordering — rows inserted
// "right after one another" can't be assumed to differ in NOW().
func insertInboxItem(t *testing.T, workspaceID, recipientID, issueID, route string, read, archived bool, createdAt time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := testPool.Exec(ctx, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id,
			type, severity, issue_id, title, route, read, archived, created_at)
		VALUES ($1, 'member', $2, 'new_comment', 'info', $3,
		        'unread-count fixture', $4, $5, $6, $7)
	`, workspaceID, recipientID, issueID, route, read, archived, createdAt)
	if err != nil {
		t.Fatalf("insertInboxItem: %v", err)
	}
}

// TestCountUnreadInboxForUserAllWorkspaces verifies the cross-workspace
// unread-inbox count powering the OS app-icon badge.
//
// The badge needs to mirror what the user sees in the inbox sidebar, which
// groups items by issue (one row per issue, latest wins). Counting raw
// inbox_item rows like the first cut did gave a number several times higher
// than the sidebar — once you have N events on an issue you'd see N on the
// home-screen icon but 1 in the app. This test guards the dedup so that
// regression doesn't slip back in.
func TestCountUnreadInboxForUserAllWorkspaces(t *testing.T) {
	queries := db.New(testPool)
	ctx := context.Background()

	// Recipient is a member of the existing test workspace and a freshly
	// minted secondary workspace. Items in *both* must be considered.
	recipientEmail := "badge-count-recipient@multica.ai"
	recipientID := createTestUser(t, recipientEmail)
	t.Cleanup(func() { cleanupTestUser(t, recipientEmail) })

	otherWorkspaceID := createSecondaryTestWorkspace(t, recipientID)
	t.Cleanup(func() { cleanupSecondaryTestWorkspace(t, otherWorkspaceID) })

	// FK requires real issues — workspace placement doesn't matter for
	// the count itself.
	issueA := createTestIssue(t, testWorkspaceID, testUserID)
	issueB := createTestIssue(t, otherWorkspaceID, recipientID)
	issueC := createTestIssue(t, testWorkspaceID, testUserID)
	issueD := createTestIssue(t, otherWorkspaceID, recipientID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueA)
		cleanupInboxForIssue(t, issueB)
		cleanupInboxForIssue(t, issueC)
		cleanupInboxForIssue(t, issueD)
		cleanupTestIssue(t, issueA)
		cleanupTestIssue(t, issueB)
		cleanupTestIssue(t, issueC)
		cleanupTestIssue(t, issueD)
	})

	t0 := time.Now().UTC().Add(-time.Hour)
	at := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

	// Issue A — three events, latest unread → counts as 1.
	insertInboxItem(t, testWorkspaceID, recipientID, issueA, "inbox", true, false, at(0))
	insertInboxItem(t, testWorkspaceID, recipientID, issueA, "inbox", true, false, at(1))
	insertInboxItem(t, testWorkspaceID, recipientID, issueA, "inbox", false, false, at(2))

	// Issue B — three events, latest read → counts as 0 (sidebar would
	// show this as "you've already seen the most recent activity").
	insertInboxItem(t, otherWorkspaceID, recipientID, issueB, "inbox", false, false, at(0))
	insertInboxItem(t, otherWorkspaceID, recipientID, issueB, "inbox", false, false, at(1))
	insertInboxItem(t, otherWorkspaceID, recipientID, issueB, "inbox", true, false, at(2))

	// Issue C — single unread row → counts as 1.
	insertInboxItem(t, testWorkspaceID, recipientID, issueC, "inbox", false, false, at(0))

	// Issue D — single unread row, but archived → doesn't count.
	insertInboxItem(t, otherWorkspaceID, recipientID, issueD, "inbox", false, true, at(0))

	// Wrong-route (notifications) row on issueC: must not register.
	insertInboxItem(t, testWorkspaceID, recipientID, issueC, "notifications", false, false, at(3))

	recipientUUID, err := util.ParseUUID(recipientID)
	if err != nil {
		t.Fatalf("ParseUUID(recipientID): %v", err)
	}
	count, err := queries.CountUnreadInboxForUserAllWorkspaces(ctx, recipientUUID)
	if err != nil {
		t.Fatalf("CountUnreadInboxForUserAllWorkspaces: %v", err)
	}
	const want = int64(2)
	if count != want {
		t.Fatalf("count = %d, want %d (A unread-latest + C single unread; B latest=read, D archived, notifications excluded)", count, want)
	}

	// A bystander with no inbox rows must read 0.
	otherEmail := "badge-count-bystander@multica.ai"
	otherID := createTestUser(t, otherEmail)
	t.Cleanup(func() { cleanupTestUser(t, otherEmail) })
	otherUUID, err := util.ParseUUID(otherID)
	if err != nil {
		t.Fatalf("ParseUUID(otherID): %v", err)
	}
	otherCount, err := queries.CountUnreadInboxForUserAllWorkspaces(ctx, otherUUID)
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
