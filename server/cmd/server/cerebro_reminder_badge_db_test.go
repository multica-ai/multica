package main

// CEREBRO-PATCH(badge-reminder-standalone): FIR-2278 DB regression test. The OS
// app badge (CountUnreadInboxForUserAllWorkspaces) must count a fired reminder
// as its own row, not fold it into its issue's group — otherwise the badge
// disagrees with the frontend inbox, which now treats reminders as standalone.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCountUnreadInboxBadge_ReminderCountsStandalone(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()

	// A dedicated recipient UUID so the cross-workspace count is isolated from
	// other fixtures' inbox rows. The badge query filters on recipient_id only,
	// so no user row is needed.
	const recipient = "00000000-0000-0000-0000-0000000f2278"
	issueID := createTestIssue(t, testWorkspaceID, testUserID)

	// On the same issue: a fired reminder row AND a newer ordinary comment row,
	// both unread, inbox-routed.
	for _, row := range []struct{ typ, title string }{
		{"reminder", "FIR-2278 badge reminder"},
		{"new_comment", "FIR-2278 badge comment"},
	} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO inbox_item
				(workspace_id, recipient_type, recipient_id, type, severity,
				 issue_id, title, route, read, archived)
			VALUES ($1, 'member', $2, $3, 'info', $4, $5, 'inbox', false, false)
		`, testWorkspaceID, recipient, row.typ, issueID, row.title); err != nil {
			t.Fatalf("insert inbox_item %s: %v", row.typ, err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM inbox_item WHERE recipient_id = $1`, recipient)
	})

	var rid pgtype.UUID
	if err := rid.Scan(recipient); err != nil {
		t.Fatalf("parse recipient uuid: %v", err)
	}
	count, err := db.New(testPool).CountUnreadInboxForUserAllWorkspaces(ctx, rid)
	if err != nil {
		t.Fatalf("CountUnreadInboxForUserAllWorkspaces: %v", err)
	}
	// Standalone reminder → 2 (reminder + issue group). The old issue-only dedup
	// would have collapsed both into 1.
	if count != 2 {
		t.Fatalf("badge count = %d, want 2 (reminder standalone + issue row)", count)
	}
}
