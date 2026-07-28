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

// FIR-3918: a reminder created by snoozing an inbox row must NOT add a second
// row when it fires. The snoozed row was only hidden (muted_until) and comes
// back on its own carrying the reminder mark, so the sweeper re-surfaces it
// unread instead of creating a standalone copy — before the fix the user got the
// same message twice: the resurfaced row plus a new `reminder` row.
func TestCerebroReminderSweeper_SnoozedInboxRowIsTheReminder(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	cerebro := cerebrodb.New(testPool)

	const text = "FIR-3918 snoozed row is the reminder"
	issueID := createTestIssue(t, testWorkspaceID, testUserID)

	// The row the user snoozed: read, muted until a moment ago (so it has just
	// resurfaced), still in the inbox.
	var sourceItemID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id,
			type, severity, issue_id, title, route, read, archived, muted_until)
		VALUES ($1, 'member', $2, 'mentioned', 'info', $3, $4, 'inbox', true, false,
			NOW() - interval '1 minute')
		RETURNING id
	`, testWorkspaceID, testUserID, issueID, text).Scan(&sourceItemID); err != nil {
		t.Fatalf("insert source inbox row: %v", err)
	}

	var reminderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO cerebro_reminder
			(workspace_id, user_id, creator_id, recipient_type, recipient_id,
			 remind_at, text, anchor_type, conversation_id, source_inbox_item_id, status)
		VALUES ($1, $2, $2, 'member', $2,
			 NOW() - interval '1 minute', $3, 'issue', $4, $5, 'pending')
		RETURNING id
	`, testWorkspaceID, testUserID, text, issueID, sourceItemID).Scan(&reminderID); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM cerebro_reminder WHERE id = $1`, reminderID)
		testPool.Exec(bg, `DELETE FROM inbox_item WHERE recipient_id = $1 AND issue_id = $2`, testUserID, issueID)
	})

	tickCerebroReminders(ctx, cerebro, nil)

	// No standalone reminder row was created for this issue.
	var reminderRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1
		  AND issue_id = $2 AND type = 'reminder'
	`, testUserID, issueID).Scan(&reminderRows); err != nil {
		t.Fatalf("count reminder rows: %v", err)
	}
	if reminderRows != 0 {
		t.Fatalf("standalone reminder rows = %d, want 0 — the snoozed row IS the reminder", reminderRows)
	}

	// The snoozed row came back unread, so the reminder actually rings.
	var read bool
	if err := testPool.QueryRow(ctx,
		`SELECT read FROM inbox_item WHERE id = $1`, sourceItemID).Scan(&read); err != nil {
		t.Fatalf("read source row: %v", err)
	}
	if read {
		t.Fatalf("snoozed row is still read, want unread when its reminder fires")
	}
}

// FIR-3918: the user archived the snoozed row before the reminder fired. The
// existing FIR-394 step un-archives the conversation's rows first, so the row is
// back by the time we look — and it can carry the reminder like any other
// snoozed row. Still exactly one row, not the row plus a standalone copy.
func TestCerebroReminderSweeper_ArchivedSourceIsRestoredAsTheReminder(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	cerebro := cerebrodb.New(testPool)

	const text = "FIR-3918 archived source falls back"
	issueID := createTestIssue(t, testWorkspaceID, testUserID)

	var sourceItemID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id,
			type, severity, issue_id, title, route, read, archived, muted_until)
		VALUES ($1, 'member', $2, 'mentioned', 'info', $3, $4, 'inbox', true, true,
			NOW() - interval '1 minute')
		RETURNING id
	`, testWorkspaceID, testUserID, issueID, text).Scan(&sourceItemID); err != nil {
		t.Fatalf("insert source inbox row: %v", err)
	}

	var reminderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO cerebro_reminder
			(workspace_id, user_id, creator_id, recipient_type, recipient_id,
			 remind_at, text, anchor_type, conversation_id, source_inbox_item_id, status)
		VALUES ($1, $2, $2, 'member', $2,
			 NOW() - interval '1 minute', $3, 'issue', $4, $5, 'pending')
		RETURNING id
	`, testWorkspaceID, testUserID, text, issueID, sourceItemID).Scan(&reminderID); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM cerebro_reminder WHERE id = $1`, reminderID)
		testPool.Exec(bg, `DELETE FROM inbox_item WHERE recipient_id = $1 AND issue_id = $2`, testUserID, issueID)
	})

	tickCerebroReminders(ctx, cerebro, nil)

	// The archived source is back, unread, and it is the ONLY row: no standalone
	// reminder copy was added on top of it.
	var read, archived bool
	if err := testPool.QueryRow(ctx,
		`SELECT read, archived FROM inbox_item WHERE id = $1`, sourceItemID).
		Scan(&read, &archived); err != nil {
		t.Fatalf("read source row: %v", err)
	}
	if archived {
		t.Fatalf("source row still archived, want restored when its reminder fires")
	}
	if read {
		t.Fatalf("restored row is read, want unread when its reminder fires")
	}

	var rows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1 AND issue_id = $2
	`, testUserID, issueID).Scan(&rows); err != nil {
		t.Fatalf("count inbox rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("inbox rows for the issue = %d, want 1 — the restored row IS the reminder", rows)
	}
}

// FIR-3918 fallback: the link must not be trusted blindly. If it points at a row
// that is not the recipient's own, the sweeper ignores it and creates the
// standalone reminder row — a bad link may never cost a reminder, and it may
// never mark someone else's inbox row unread.
func TestCerebroReminderSweeper_ForeignSourceFallsBackToStandaloneRow(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	cerebro := cerebrodb.New(testPool)

	const text = "FIR-3918 foreign source falls back"
	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	otherUserID := createTestUser(t, "fir3918-other@example.com")

	// An inbox row belonging to somebody else on the same issue.
	var foreignItemID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id,
			type, severity, issue_id, title, route, read, archived)
		VALUES ($1, 'member', $2, 'mentioned', 'info', $3, $4, 'inbox', true, false)
		RETURNING id
	`, testWorkspaceID, otherUserID, issueID, text).Scan(&foreignItemID); err != nil {
		t.Fatalf("insert foreign inbox row: %v", err)
	}

	var reminderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO cerebro_reminder
			(workspace_id, user_id, creator_id, recipient_type, recipient_id,
			 remind_at, text, anchor_type, conversation_id, source_inbox_item_id, status)
		VALUES ($1, $2, $2, 'member', $2,
			 NOW() - interval '1 minute', $3, 'issue', $4, $5, 'pending')
		RETURNING id
	`, testWorkspaceID, testUserID, text, issueID, foreignItemID).Scan(&reminderID); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM cerebro_reminder WHERE id = $1`, reminderID)
		testPool.Exec(bg, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
		testPool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})

	tickCerebroReminders(ctx, cerebro, nil)

	// The recipient got their own standalone reminder row...
	var read bool
	if err := testPool.QueryRow(ctx, `
		SELECT read FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1
		  AND issue_id = $2 AND type = 'reminder' AND title = $3
	`, testUserID, issueID, text).Scan(&read); err != nil {
		t.Fatalf("expected a standalone reminder row for a foreign source link, got: %v", err)
	}
	if read {
		t.Fatalf("standalone reminder row is read, want unread")
	}

	// ...and the other member's row was left alone.
	var foreignRead bool
	if err := testPool.QueryRow(ctx,
		`SELECT read FROM inbox_item WHERE id = $1`, foreignItemID).Scan(&foreignRead); err != nil {
		t.Fatalf("read foreign row: %v", err)
	}
	if !foreignRead {
		t.Fatalf("another member's inbox row was marked unread, want untouched")
	}
}
