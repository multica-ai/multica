package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// seedInboxRows writes count active rows for recipient, one millisecond apart,
// the newest at base and titled row-0000.
func seedInboxRows(t *testing.T, workspaceID, recipientID string, count int, base time.Time) {
	t.Helper()
	dbfx.Exec(t, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, title, created_at)
		SELECT $1, 'member', $2, 'system', 'info', 'row-' || lpad(i::text, 4, '0'),
		       $3::timestamptz - i * interval '1 millisecond'
		FROM generate_series(0, $4::int - 1) AS i
	`, workspaceID, recipientID, base, count)
}

func TestListInboxReturnsNewestRowsUpToTheLimit(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Inbox limit list", "inbox-limit-list-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit+3, time.Now().UTC().Add(-time.Minute))
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)

	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox", workspaceID)).
		Want(http.StatusOK).
		JSON(&items)

	if len(items) != inbox.ActiveLimit {
		t.Fatalf("inbox items = %d, want %d", len(items), inbox.ActiveLimit)
	}
	if items[0].Title != "row-0000" || items[len(items)-1].Title != "row-1999" {
		t.Fatalf("list spans %q..%q, want the newest rows newest first", items[0].Title, items[len(items)-1].Title)
	}
}

// The trimmer archives the recipient's shown rows beyond the newest
// ActiveLimit, row by row, and leaves snoozed rows and other inboxes alone.
func TestInboxTrimmerArchivesRowsBeyondTheLimit(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Inbox limit trim", "inbox-limit-trim-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	otherID := dbfx.User(t, "Other inbox owner", "inbox-limit-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, workspaceID, otherID, "member")
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)

	base := time.Now().UTC().Add(-time.Minute)
	extra := 150 // beyond the trimmer's slack
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit+extra, base)
	seedInboxRows(t, workspaceID, otherID, inbox.ActiveLimit+extra, base)
	issueID := dbfx.Issue(t, "Snoozed issue", testutil.Cols{"workspace_id": workspaceID})
	snoozedID := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id":   workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           "new_comment",
		"severity":       "info",
		"issue_id":       issueID,
		"title":          "snoozed",
		"created_at":     base.Add(-time.Hour),
		"snoozed_until":  time.Now().Add(time.Hour),
		"snoozed_at":     time.Now(),
	})

	bus := events.New()
	var announced []events.Event
	bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) { announced = append(announced, e) })
	trimmer := inbox.NewTrimmer(testHandler.Queries, bus)
	trimmer.Mark(inbox.Recipient{WorkspaceID: parseUUID(workspaceID), RecipientType: "member", RecipientID: parseUUID(testUserID)})
	trimmer.Flush(context.Background())

	var active, archived int
	var oldestActive string
	dbfx.QueryRow(t, `
		SELECT count(*) FILTER (WHERE NOT archived), count(*) FILTER (WHERE archived),
		       (SELECT title FROM inbox_item WHERE workspace_id = $1 AND recipient_id = $2
		          AND NOT archived AND snoozed_until IS NULL ORDER BY created_at LIMIT 1)
		FROM inbox_item
		WHERE workspace_id = $1 AND recipient_id = $2 AND snoozed_until IS NULL
	`, workspaceID, testUserID).Scan(&active, &archived, &oldestActive)
	if active != inbox.ActiveLimit || archived != extra || oldestActive != "row-1999" {
		t.Fatalf("active=%d archived=%d oldest kept=%q; want %d newest kept, %d archived",
			active, archived, oldestActive, inbox.ActiveLimit, extra)
	}

	var snoozedArchived bool
	dbfx.QueryRow(t, `SELECT archived FROM inbox_item WHERE id = $1`, snoozedID).Scan(&snoozedArchived)
	if snoozedArchived {
		t.Fatal("a snoozed row was archived")
	}

	var otherActive int
	dbfx.QueryRow(t, `SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND recipient_id = $2 AND NOT archived`,
		workspaceID, otherID).Scan(&otherActive)
	if otherActive != inbox.ActiveLimit+extra {
		t.Fatalf("unmarked inbox active rows = %d, want untouched %d", otherActive, inbox.ActiveLimit+extra)
	}

	if len(announced) != 1 {
		t.Fatalf("batch-archived events = %d, want 1", len(announced))
	}
	if payload := announced[0].Payload.(map[string]any); payload["recipient_id"] != testUserID || payload["count"] != int64(extra) {
		t.Fatalf("event payload = %v", payload)
	}
}
