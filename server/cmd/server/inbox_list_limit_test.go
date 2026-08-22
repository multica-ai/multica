package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestListInboxItemsRespectsLimit pins the LIMIT 200 on ListInboxItems (#6527).
// Without the cap, a heavy inbox returns every non-archived row — multi-MB
// payloads on every mark-read refetch. The archived list is already capped at
// 200 (ListArchivedInboxItems); this test proves the active list is too.
func TestListInboxItemsRespectsLimit(t *testing.T) {
	queries := db.New(testPool)
	ctx := context.Background()

	recipientEmail := "inbox-limit-test@multica.ai"
	recipientID := createTestUser(t, recipientEmail)
	t.Cleanup(func() { cleanupTestUser(t, recipientEmail) })

	// Insert more rows than the LIMIT (205 > 200) so a missing cap would
	// return all 205. Use raw SQL for the bulk insert; each row is a
	// standalone item (no issue_id) so it groups on its own id.
	const insertCount = 205
	for i := 0; i < insertCount; i++ {
		_, err := testPool.Exec(ctx, `
			INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, title)
			VALUES ($1, 'member', $2, 'issue_assigned', 'info', $3)
		`, util.MustParseUUID(testWorkspaceID), util.MustParseUUID(recipientID),
			fmt.Sprintf("limit test item %d", i))
		if err != nil {
			t.Fatalf("insert inbox item %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM inbox_item WHERE recipient_id = $1`, util.MustParseUUID(recipientID))
	})

	items, err := queries.ListInboxItems(ctx, db.ListInboxItemsParams{
		WorkspaceID:   util.MustParseUUID(testWorkspaceID),
		RecipientType: "member",
		RecipientID:   util.MustParseUUID(recipientID),
	})
	if err != nil {
		t.Fatalf("ListInboxItems: %v", err)
	}

	if len(items) != 200 {
		t.Errorf("ListInboxItems returned %d items, want 200 (LIMIT); the query cap from #6527 is missing", len(items))
	}
}
