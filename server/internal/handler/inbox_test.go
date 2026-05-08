package handler

import (
	"context"
	"sort"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestArchiveAllReadInbox_RespectsDedupByIssue (PUL-39) verifies that
// "Archive all read" archives groups (one issue, or one standalone item)
// whose newest non-archived inbox_item is read — matching the inbox UI's
// dedup-by-issue-newest semantic.
//
// The previous implementation archived per row (`SET archived = true WHERE
// read = true`). For an issue with mixed read/unread events that dedup'd to a
// read representative, that hid the read row but exposed an older unread
// sibling, flipping the issue from "read" to "unread" in the list. This test
// pins the corrected behavior so a regression cannot reintroduce the bug.
func TestArchiveAllReadInbox_RespectsDedupByIssue(t *testing.T) {
	ctx := context.Background()

	// Create four issues. Each tests a different scenario; using distinct
	// issues keeps the assertions independent.
	type issueRow struct {
		name    string
		status  string
		issueID string
	}
	issues := []issueRow{
		{name: "all-read"},        // every inbox_item is read → fully archive
		{name: "all-unread"},      // every inbox_item is unread → leave alone
		{name: "mixed-newest-read"},   // mix; newest is read → fully archive
		{name: "mixed-newest-unread"}, // mix; newest is unread → leave alone
	}
	// Distinct `number` per issue avoids collision with the
	// uq_issue_workspace_number unique constraint (default is 0). The exact
	// values don't matter for the test; just need each to be unique within
	// the workspace.
	for i := range issues {
		err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number)
			VALUES ($1, $2, 'todo', 'medium', $3, 'member', $4)
			RETURNING id
		`, testWorkspaceID, "PUL-39 inbox test "+issues[i].name, testUserID, 90000+i).Scan(&issues[i].issueID)
		if err != nil {
			t.Fatalf("setup: insert issue %s: %v", issues[i].name, err)
		}
	}
	t.Cleanup(func() {
		for _, iss := range issues {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, iss.issueID)
		}
	})

	// Insert inbox_items with explicit created_at so dedup-by-newest is
	// deterministic. created_at deltas are seconds, but ordering only cares
	// about strict ordering.
	insert := func(label, issueID string, read bool, ageSeconds int) string {
		var id string
		var issueArg any
		if issueID == "" {
			issueArg = nil
		} else {
			issueArg = issueID
		}
		err := testPool.QueryRow(ctx, `
			INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, issue_id, title, read, archived, created_at)
			VALUES ($1, 'member', $2, 'comment', $3, $4, $5, false, now() - ($6::int * interval '1 second'))
			RETURNING id
		`, testWorkspaceID, testUserID, issueArg, "PUL-39 "+label, read, ageSeconds).Scan(&id)
		if err != nil {
			t.Fatalf("setup: insert inbox_item %s: %v", label, err)
		}
		return id
	}

	// Track insertion ids by stable label so the assertions stay readable.
	itemIDs := map[string]string{}

	// Issue "all-read": two rows, both read. Both should be archived.
	itemIDs["all-read.older"] = insert("all-read.older", issues[0].issueID, true, 200)
	itemIDs["all-read.newer"] = insert("all-read.newer", issues[0].issueID, true, 100)

	// Issue "all-unread": two rows, both unread. Neither should be archived.
	itemIDs["all-unread.older"] = insert("all-unread.older", issues[1].issueID, false, 200)
	itemIDs["all-unread.newer"] = insert("all-unread.newer", issues[1].issueID, false, 100)

	// Issue "mixed-newest-read": three rows, newest is read. ALL should be
	// archived (including the older unread one) — this is the PUL-39 case
	// where the previous per-row SQL would have left the unread sibling
	// behind, flipping the issue from "read" to "unread" in the inbox UI.
	itemIDs["mixed-newest-read.oldest-read"] = insert("mixed-newest-read.oldest-read", issues[2].issueID, true, 300)
	itemIDs["mixed-newest-read.middle-unread"] = insert("mixed-newest-read.middle-unread", issues[2].issueID, false, 200)
	itemIDs["mixed-newest-read.newest-read"] = insert("mixed-newest-read.newest-read", issues[2].issueID, true, 100)

	// Issue "mixed-newest-unread": three rows, newest is unread. NOTHING
	// should be archived — the inbox shows this issue as unread, the user
	// hasn't dismissed it.
	itemIDs["mixed-newest-unread.oldest-read"] = insert("mixed-newest-unread.oldest-read", issues[3].issueID, true, 300)
	itemIDs["mixed-newest-unread.middle-read"] = insert("mixed-newest-unread.middle-read", issues[3].issueID, true, 200)
	itemIDs["mixed-newest-unread.newest-unread"] = insert("mixed-newest-unread.newest-unread", issues[3].issueID, false, 100)

	// Standalone read item (issue_id IS NULL): archive.
	itemIDs["standalone.read"] = insert("standalone.read", "", true, 100)

	// Standalone unread item (issue_id IS NULL): leave alone.
	itemIDs["standalone.unread"] = insert("standalone.unread", "", false, 100)

	t.Cleanup(func() {
		for _, id := range itemIDs {
			testPool.Exec(ctx, `DELETE FROM inbox_item WHERE id = $1`, id)
		}
	})

	// Run the SUT.
	count, err := testHandler.Queries.ArchiveAllReadInbox(ctx, db.ArchiveAllReadInboxParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		RecipientID: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("ArchiveAllReadInbox: %v", err)
	}

	wantArchived := []string{
		"all-read.older",
		"all-read.newer",
		"mixed-newest-read.oldest-read",
		"mixed-newest-read.middle-unread", // archived even though unread, because group's newest is read
		"mixed-newest-read.newest-read",
		"standalone.read",
	}
	wantUntouched := []string{
		"all-unread.older",
		"all-unread.newer",
		"mixed-newest-unread.oldest-read",
		"mixed-newest-unread.middle-read",
		"mixed-newest-unread.newest-unread",
		"standalone.unread",
	}

	if int(count) != len(wantArchived) {
		t.Errorf("ArchiveAllReadInbox affected %d rows, want %d", count, len(wantArchived))
	}

	// Verify per-row state.
	gotArchived := []string{}
	gotUntouched := []string{}
	for label, id := range itemIDs {
		var archived bool
		var read bool
		if err := testPool.QueryRow(ctx, `SELECT archived, read FROM inbox_item WHERE id = $1`, id).Scan(&archived, &read); err != nil {
			t.Fatalf("query inbox_item %s: %v", label, err)
		}
		if archived {
			gotArchived = append(gotArchived, label)
		} else {
			gotUntouched = append(gotUntouched, label)
		}
		// Critical: archive_all_read must NEVER mutate the read flag. If a row
		// was read=true before, it must still be read=true. The original PUL-39
		// bug was reported as "read messages become unread" — that was a UI
		// dedup artifact, not an actual mutation, but pinning this invariant
		// prevents a future regression from causing the same symptom for real.
		// Find the original read state from the test setup:
		expectRead := false
		for _, label2 := range []string{
			"all-read.older", "all-read.newer",
			"mixed-newest-read.oldest-read", "mixed-newest-read.newest-read",
			"mixed-newest-unread.oldest-read", "mixed-newest-unread.middle-read",
			"standalone.read",
		} {
			if label == label2 {
				expectRead = true
				break
			}
		}
		if read != expectRead {
			t.Errorf("inbox_item %s: read=%v, want %v (archive_all_read must not change the read flag)", label, read, expectRead)
		}
	}
	sort.Strings(gotArchived)
	sort.Strings(gotUntouched)
	sort.Strings(wantArchived)
	sort.Strings(wantUntouched)
	if !equalStringSlices(gotArchived, wantArchived) {
		t.Errorf("archived rows mismatch:\n  got:  %v\n  want: %v", gotArchived, wantArchived)
	}
	if !equalStringSlices(gotUntouched, wantUntouched) {
		t.Errorf("untouched rows mismatch:\n  got:  %v\n  want: %v", gotUntouched, wantUntouched)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
