package main

// CEREBRO-PATCH(cerebro-reminder): FIR-2154 DB regression test. A fired "free"
// reminder (anchor 'none', no conversation to re-surface) must land a
// standalone, unread reminder row in the recipient's inbox. Before the fix the
// sweeper's surfaceFiredReminder early-returned for any reminder without a
// conversation_id, so free and project reminders were marked 'fired' but never
// appeared in the inbox at their planned time.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCerebroReminderSweeper_FreeReminderSurfacesInInbox(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	cerebro := cerebrodb.New(testPool)

	const text = "FIR-2154 free reminder regression"

	// A free reminder (anchor 'none', no conversation/project), already due.
	var reminderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO cerebro_reminder
			(workspace_id, user_id, creator_id, recipient_type, recipient_id,
			 remind_at, text, anchor_type, status)
		VALUES ($1, $2, $2, 'member', $2,
			 NOW() - interval '1 minute', $3, 'none', 'pending')
		RETURNING id
	`, testWorkspaceID, testUserID, text).Scan(&reminderID); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM cerebro_reminder WHERE id = $1`, reminderID)
		testPool.Exec(bg, `DELETE FROM inbox_item WHERE recipient_id = $1 AND title = $2`, testUserID, text)
	})

	// Fire the sweep (bus=nil: we assert the persisted inbox row, not the event).
	tickCerebroReminders(ctx, cerebro, nil)

	// The reminder is claimed as 'fired'.
	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM cerebro_reminder WHERE id = $1`, reminderID).Scan(&status); err != nil {
		t.Fatalf("read reminder status: %v", err)
	}
	if status != "fired" {
		t.Fatalf("reminder status = %q, want \"fired\"", status)
	}

	// ...and exactly one standalone, unread, issue-less reminder row exists.
	var read, issueNull bool
	if err := testPool.QueryRow(ctx, `
		SELECT read, issue_id IS NULL
		FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1
		  AND type = 'reminder' AND route = 'inbox' AND title = $2
	`, testUserID, text).Scan(&read, &issueNull); err != nil {
		t.Fatalf("expected one reminder inbox row for the fired free reminder, got: %v", err)
	}
	if read {
		t.Fatalf("reminder inbox row is read, want unread")
	}
	if !issueNull {
		t.Fatalf("reminder inbox row issue_id is not NULL, want NULL for a free reminder")
	}
}

// FIR-2278: a reminder anchored to an issue/comment must ALSO land a standalone,
// unread `reminder` inbox row (linked to the issue), not merely re-surface the
// issue as a generic manually-added row. Before the fix the anchored path never
// created a reminder-typed row, so an issue/comment reminder never appeared in
// the inbox's Reminders section — the bug Jesper hit ("får ingen reminders").
func TestCerebroReminderSweeper_IssueAnchoredReminderSurfacesInInbox(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	cerebro := cerebrodb.New(testPool)

	const text = "FIR-2278 issue-anchored reminder regression"
	issueID := createTestIssue(t, testWorkspaceID, testUserID)

	// An issue-anchored reminder (anchor 'issue', conversation = the issue),
	// already due.
	var reminderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO cerebro_reminder
			(workspace_id, user_id, creator_id, recipient_type, recipient_id,
			 remind_at, text, anchor_type, conversation_id, status)
		VALUES ($1, $2, $2, 'member', $2,
			 NOW() - interval '1 minute', $3, 'issue', $4, 'pending')
		RETURNING id
	`, testWorkspaceID, testUserID, text, issueID).Scan(&reminderID); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM cerebro_reminder WHERE id = $1`, reminderID)
		testPool.Exec(bg, `DELETE FROM inbox_item WHERE recipient_id = $1 AND title = $2`, testUserID, text)
	})

	tickCerebroReminders(ctx, cerebro, nil)

	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM cerebro_reminder WHERE id = $1`, reminderID).Scan(&status); err != nil {
		t.Fatalf("read reminder status: %v", err)
	}
	if status != "fired" {
		t.Fatalf("reminder status = %q, want \"fired\"", status)
	}

	// Exactly one unread `reminder` inbox row exists, linked to the source issue.
	var read bool
	var rowIssueID string
	if err := testPool.QueryRow(ctx, `
		SELECT read, issue_id
		FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1
		  AND type = 'reminder' AND route = 'inbox' AND title = $2
	`, testUserID, text).Scan(&read, &rowIssueID); err != nil {
		t.Fatalf("expected one reminder inbox row for the fired issue reminder, got: %v", err)
	}
	if read {
		t.Fatalf("reminder inbox row is read, want unread")
	}
	if rowIssueID != issueID {
		t.Fatalf("reminder inbox row issue_id = %q, want the source issue %q", rowIssueID, issueID)
	}
}

// FIR-2278: firing records the created inbox row on cerebro_reminder
// .fired_inbox_item_id, and that id can archive the row. This is the chain the
// done/snooze/delete handlers rely on to clean a fired reminder out of the
// inbox — before the fix the row was orphaned because the relation points from
// reminder to inbox_item, not the other way.
func TestCerebroReminderSweeper_FiredInboxItemIsArchivable(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	cerebro := cerebrodb.New(testPool)

	const text = "FIR-2278 fired-inbox cleanup link"
	var reminderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO cerebro_reminder
			(workspace_id, user_id, creator_id, recipient_type, recipient_id,
			 remind_at, text, anchor_type, status)
		VALUES ($1, $2, $2, 'member', $2,
			 NOW() - interval '1 minute', $3, 'none', 'pending')
		RETURNING id
	`, testWorkspaceID, testUserID, text).Scan(&reminderID); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM cerebro_reminder WHERE id = $1`, reminderID)
		testPool.Exec(bg, `DELETE FROM inbox_item WHERE recipient_id = $1 AND title = $2`, testUserID, text)
	})

	tickCerebroReminders(ctx, cerebro, nil)

	// The fired reminder recorded the inbox row it created.
	var firedInboxItemID pgtype.UUID
	if err := testPool.QueryRow(ctx,
		`SELECT fired_inbox_item_id FROM cerebro_reminder WHERE id = $1`, reminderID).
		Scan(&firedInboxItemID); err != nil {
		t.Fatalf("read fired_inbox_item_id: %v", err)
	}
	if !firedInboxItemID.Valid {
		t.Fatalf("fired_inbox_item_id is NULL, want the surfaced inbox row id")
	}

	// Archiving by that id (what done/snooze/delete now do) clears the row.
	if _, err := db.New(testPool).ArchiveInboxItem(ctx, firedInboxItemID); err != nil {
		t.Fatalf("ArchiveInboxItem: %v", err)
	}
	var archived bool
	if err := testPool.QueryRow(ctx,
		`SELECT archived FROM inbox_item WHERE id = $1`, firedInboxItemID).Scan(&archived); err != nil {
		t.Fatalf("read archived: %v", err)
	}
	if !archived {
		t.Fatalf("fired reminder inbox row not archived after cleanup")
	}
}
