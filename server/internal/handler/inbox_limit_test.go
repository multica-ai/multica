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

// seedInboxRows writes count active rows for recipient, step apart (zero for
// identical times), the newest at base and titled row-0000.
func seedInboxRows(t *testing.T, workspaceID, recipientID string, count int, base time.Time, step time.Duration) {
	t.Helper()
	dbfx.Exec(t, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, title, created_at)
		SELECT $1, 'member', $2, 'system', 'info', 'row-' || lpad(i::text, 4, '0'),
		       $3::timestamptz - i * $5::interval
		FROM generate_series(0, $4::int - 1) AS i
	`, workspaceID, recipientID, base, count, step.String())
}

func limitInboxWorkspace(t *testing.T, name string) string {
	t.Helper()
	workspaceID := dbfx.Workspace(t, name, "inbox-limit-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
	return workspaceID
}

func insertOldInboxRow(t *testing.T, workspaceID, title string, cols testutil.Cols) string {
	t.Helper()
	row := testutil.Cols{
		"workspace_id":   workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           "system",
		"severity":       "info",
		"title":          title,
		"created_at":     time.Now().Add(-48 * time.Hour),
	}
	for k, v := range cols {
		row[k] = v
	}
	return dbfx.Insert(t, "inbox_item", row)
}

func trimInbox(t *testing.T, workspaceID string) []events.Event {
	t.Helper()
	bus := events.New()
	var announced []events.Event
	bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) { announced = append(announced, e) })
	trimmer := inbox.NewTrimmer(testHandler.Queries, bus)
	trimmer.Mark(inbox.Recipient{WorkspaceID: parseUUID(workspaceID), RecipientType: "member", RecipientID: parseUUID(testUserID)})
	trimmer.Flush(context.Background())
	return announced
}

func isArchived(t *testing.T, id string) bool {
	t.Helper()
	var archived bool
	dbfx.QueryRow(t, `SELECT archived FROM inbox_item WHERE id = $1`, id).Scan(&archived)
	return archived
}

// The list returns the newest ActiveLimit rows, ranking a row that came back
// from a snooze or from the archive by when it came back.
func TestListInboxReturnsNewestRowsUpToTheLimit(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit list")
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit+3, time.Now().UTC().Add(-time.Hour), time.Millisecond)
	insertOldInboxRow(t, workspaceID, "woke", testutil.Cols{
		"snoozed_until": time.Now().Add(-time.Minute),
		"snoozed_at":    time.Now().Add(-24 * time.Hour),
	})
	insertOldInboxRow(t, workspaceID, "restored", testutil.Cols{"restored_at": time.Now()})

	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox", workspaceID)).
		Want(http.StatusOK).
		JSON(&items)

	if len(items) != inbox.ActiveLimit {
		t.Fatalf("inbox items = %d, want %d", len(items), inbox.ActiveLimit)
	}
	if items[0].Title != "restored" || items[1].Title != "woke" || items[2].Title != "row-0000" ||
		items[len(items)-1].Title != "row-1997" {
		t.Fatalf("list = %q, %q, %q .. %q; want the rows that came back on top, then the newest",
			items[0].Title, items[1].Title, items[2].Title, items[len(items)-1].Title)
	}
}

// The trimmer archives the shown rows beyond the newest ActiveLimit, row by
// row, and leaves snoozed rows, rows that came back, and other inboxes alone.
func TestInboxTrimmerArchivesRowsBeyondTheLimit(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit trim")
	otherID := dbfx.User(t, "Other inbox owner", "inbox-limit-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, workspaceID, otherID, "member")

	base := time.Now().UTC().Add(-time.Hour)
	extra := 150
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit+extra, base, time.Millisecond)
	seedInboxRows(t, workspaceID, otherID, inbox.ActiveLimit+extra, base, time.Millisecond)
	issueID := dbfx.Issue(t, "Snoozed issue", testutil.Cols{"workspace_id": workspaceID})
	snoozedID := insertOldInboxRow(t, workspaceID, "snoozed", testutil.Cols{
		"issue_id":      issueID,
		"snoozed_until": time.Now().Add(time.Hour),
		"snoozed_at":    time.Now(),
	})
	wokeID := insertOldInboxRow(t, workspaceID, "woke", testutil.Cols{
		"snoozed_until": time.Now().Add(-time.Minute),
		"snoozed_at":    time.Now().Add(-24 * time.Hour),
	})
	restoredID := insertOldInboxRow(t, workspaceID, "restored", testutil.Cols{"restored_at": time.Now()})

	announced := trimInbox(t, workspaceID)

	var active, archived int
	var oldestKept string
	dbfx.QueryRow(t, `
		SELECT count(*) FILTER (WHERE NOT archived), count(*) FILTER (WHERE archived),
		       (SELECT title FROM inbox_item WHERE workspace_id = $1 AND recipient_id = $2
		          AND NOT archived AND title LIKE 'row-%' ORDER BY created_at LIMIT 1)
		FROM inbox_item
		WHERE workspace_id = $1 AND recipient_id = $2 AND title LIKE 'row-%'
	`, workspaceID, testUserID).Scan(&active, &archived, &oldestKept)
	// The woken and restored rows rank on top, so two more seeded rows go.
	if active != inbox.ActiveLimit-2 || archived != extra+2 || oldestKept != "row-1997" {
		t.Fatalf("seeded rows active=%d archived=%d oldest kept=%q", active, archived, oldestKept)
	}
	if isArchived(t, snoozedID) || isArchived(t, wokeID) || isArchived(t, restoredID) {
		t.Fatal("archived a snoozed row or a row that came back")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND recipient_id = $2 AND NOT archived`,
		workspaceID, otherID); n != inbox.ActiveLimit+extra {
		t.Fatalf("unmarked inbox active rows = %d, want untouched %d", n, inbox.ActiveLimit+extra)
	}
	if len(announced) != 1 {
		t.Fatalf("batch-archived events = %d, want 1", len(announced))
	}
	if payload := announced[0].Payload.(map[string]any); payload["recipient_id"] != testUserID || payload["count"] != int64(extra+2) {
		t.Fatalf("event payload = %v", payload)
	}
}

// Rows written in the same instant are cut by id, the list's tie-break, so
// exactly ActiveLimit stay and they are the ones the list shows.
func TestInboxTrimmerCutsIdenticalTimesByID(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit ties")
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit+201, time.Now().UTC().Add(-time.Hour), 0)

	trimInbox(t, workspaceID)

	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox", workspaceID)).
		Want(http.StatusOK).
		JSON(&items)
	active := dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND NOT archived`, workspaceID)
	if active != inbox.ActiveLimit || len(items) != inbox.ActiveLimit {
		t.Fatalf("active=%d listed=%d, want %d of each", active, len(items), inbox.ActiveLimit)
	}
	listedArchived := dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE id = ANY($1::uuid[]) AND archived`, inboxItemIDs(items))
	if listedArchived != 0 {
		t.Fatalf("%d listed rows were archived; the trim must keep exactly what the list shows", listedArchived)
	}
}

func inboxItemIDs(items []InboxItemResponse) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

// Restoring a notification older than the newest ActiveLimit brings it back
// into the list instead of leaving it in neither list.
func TestUnarchiveRanksOldNotificationAsBack(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit restore")
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit, time.Now().UTC().Add(-time.Hour), time.Millisecond)
	oldID := insertOldInboxRow(t, workspaceID, "restored", testutil.Cols{"archived": true})

	testutil.Call(t, inboxWorkspaceHandler(testHandler.UnarchiveInboxItem),
		testutil.WithURLParams(inboxRequest(http.MethodPost, "/api/inbox/"+oldID+"/unarchive", workspaceID), "id", oldID)).
		Want(http.StatusOK)
	trimInbox(t, workspaceID)

	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox", workspaceID)).
		Want(http.StatusOK).
		JSON(&items)
	if isArchived(t, oldID) || len(items) == 0 || items[0].ID != oldID {
		t.Fatalf("restored row archived=%v, list head=%v; want it back on top", isArchived(t, oldID), items[0].Title)
	}
}
