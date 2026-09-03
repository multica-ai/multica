package handler

import (
	"context"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestListInboxItems_CapsUnboundedActiveList is the regression guard for GH
// #6527: the active-inbox query had no LIMIT, so a recipient with a large
// backlog got every sibling row back in one multi-MB / multi-second response.
// ListInboxItems now caps at 200 rows the same way ListArchivedInboxItems does.
// The cap must keep the NEWEST rows (dropping the oldest), because the
// deduplicated UI renders one row per issue carrying that group's newest item.
func TestListInboxItems_CapsUnboundedActiveList(t *testing.T) {
	if testPool == nil || testHandler == nil {
		t.Skip("requires DB")
	}
	ctx := context.Background()

	const total = 210
	base := time.Now().Add(-time.Duration(total) * time.Minute)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM inbox_item WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND title LIKE 'cap-test-%'`,
			testWorkspaceID, testUserID)
	})
	// Distinct created_at values, oldest first, so the newest carry the highest
	// index. issue_id is NULL so every row groups on its own id — 210 rows are
	// 210 groups and none can be collapsed by the UI's per-issue dedup, which
	// isolates the raw LIMIT under test.
	for i := 0; i < total; i++ {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, title, created_at)
			VALUES ($1, 'member', $2, 'mention', 'info', $3, $4)
		`, testWorkspaceID, testUserID,
			// title encodes the age index for the newest-retained assertion.
			"cap-test-"+padIndex(i), base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("insert inbox row %d: %v", i, err)
		}
	}

	items, err := testHandler.Queries.ListInboxItems(ctx, db.ListInboxItemsParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("ListInboxItems: %v", err)
	}

	if len(items) != 200 {
		t.Fatalf("ListInboxItems returned %d rows, want the 200-row cap", len(items))
	}
	// Newest-first: the first row must be the highest index (total-1), and the
	// oldest 10 rows (indices 0-9) must have been dropped, not the newest.
	if got := items[0].Title; got != "cap-test-"+padIndex(total-1) {
		t.Errorf("first row = %q, want newest %q", got, "cap-test-"+padIndex(total-1))
	}
	if got := items[len(items)-1].Title; got != "cap-test-"+padIndex(total-200) {
		t.Errorf("last kept row = %q, want %q (oldest 10 dropped)", got, "cap-test-"+padIndex(total-200))
	}
}

// padIndex zero-pads to 3 digits so lexical title comparison is unambiguous and
// the assertions read against the numeric age index directly.
func padIndex(i int) string {
	digits := []byte{'0' + byte(i/100%10), '0' + byte(i/10%10), '0' + byte(i%10)}
	return string(digits)
}
