package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

// wokeCols is a row whose snooze ran out a minute ago, as SnoozeInboxGroup
// leaves an issue's newest row.
func wokeCols() testutil.Cols {
	wake := time.Now().Add(-time.Minute)
	return testutil.Cols{"snoozed_until": wake, "snoozed_at": wake.Add(-24 * time.Hour), "returned_at": wake}
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
	insertOldInboxRow(t, workspaceID, "woke", wokeCols())
	insertOldInboxRow(t, workspaceID, "restored", testutil.Cols{"returned_at": time.Now()})

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
	wokeID := insertOldInboxRow(t, workspaceID, "woke", wokeCols())
	restoredID := insertOldInboxRow(t, workspaceID, "restored", testutil.Cols{"returned_at": time.Now()})

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

// Restoring a busy issue brings back the restored row on top, but its older
// rows keep their rank, so they do not push the rest of the inbox out.
func TestUnarchiveBusyIssueKeepsOtherRowsInTheInbox(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit busy restore")
	base := time.Now().UTC().Add(-time.Hour)
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit, base, time.Millisecond)
	issueID := dbfx.Issue(t, "Busy archived issue", testutil.Cols{"workspace_id": workspaceID})
	dbfx.Exec(t, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, archived, created_at)
		SELECT $1, 'member', $2, 'new_comment', 'info', $3, 'busy', true, $4::timestamptz - i * interval '1 millisecond'
		FROM generate_series(0, $5::int) AS i
	`, workspaceID, testUserID, issueID, base.Add(-24*time.Hour), inbox.ActiveLimit)
	var newestID string
	dbfx.QueryRow(t, `SELECT id FROM inbox_item WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1`, issueID).Scan(&newestID)

	testutil.Call(t, inboxWorkspaceHandler(testHandler.UnarchiveInboxItem),
		testutil.WithURLParams(inboxRequest(http.MethodPost, "/api/inbox/"+newestID+"/unarchive", workspaceID), "id", newestID)).
		Want(http.StatusOK)
	trimInbox(t, workspaceID)

	plain := dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND issue_id IS NULL AND NOT archived`, workspaceID)
	if plain != inbox.ActiveLimit-1 || isArchived(t, newestID) {
		t.Fatalf("other rows kept=%d, restored row archived=%v; want %d kept beside the restored issue",
			plain, isArchived(t, newestID), inbox.ActiveLimit-1)
	}
}

// Snoozing ranks only the issue's newest row by the wake time; ending the
// snooze early ranks it by now.
func TestSnoozeRanksOnlyTheNewestRowByItsReturn(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit snooze rank")
	issueID := dbfx.Issue(t, "Snoozed busy issue", testutil.Cols{"workspace_id": workspaceID})
	olderID := insertOldInboxRow(t, workspaceID, "older", testutil.Cols{"issue_id": issueID})
	newestID := insertOldInboxRow(t, workspaceID, "newest", testutil.Cols{"issue_id": issueID, "created_at": time.Now().Add(-time.Hour)})

	until := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	if _, err := testHandler.Queries.SnoozeInboxGroup(context.Background(), db.SnoozeInboxGroupParams{
		ID: parseUUID(newestID), SnoozedUntil: pgtype.Timestamptz{Time: until, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	returnedAt := func(id string) *time.Time {
		var at *time.Time
		dbfx.QueryRow(t, `SELECT returned_at FROM inbox_item WHERE id = $1`, id).Scan(&at)
		return at
	}
	if at := returnedAt(newestID); at == nil || !at.Equal(until) || returnedAt(olderID) != nil {
		t.Fatalf("after snooze newest returned_at=%v older=%v; want only the newest at %v", at, returnedAt(olderID), until)
	}

	if _, err := testHandler.Queries.UnsnoozeInboxGroup(context.Background(), db.UnsnoozeInboxGroupParams{
		WorkspaceID: parseUUID(workspaceID), RecipientType: "member", RecipientID: parseUUID(testUserID), IssueID: parseUUID(issueID),
	}); err != nil {
		t.Fatal(err)
	}
	if at := returnedAt(newestID); at == nil || time.Since(*at) > time.Minute || at.After(until) || returnedAt(olderID) != nil {
		t.Fatalf("after unsnooze newest returned_at=%v older=%v; want only the newest, at about now", at, returnedAt(olderID))
	}
}

// Rows in a private project the member cannot see are not shown, so they
// neither count toward the limit nor push out rows the member can see.
func TestInboxTrimmerIgnoresRowsTheMemberCannotSee(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit hidden")
	projectID := dbfx.Project(t, "Private project", testutil.Cols{"workspace_id": workspaceID, "visibility": "private"})
	hiddenIssueID := dbfx.Issue(t, "Hidden issue", testutil.Cols{"workspace_id": workspaceID, "project_id": projectID})
	dbfx.Exec(t, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, created_at)
		SELECT $1, 'member', $2, 'new_comment', 'info', $3, 'hidden', now() - i * interval '1 millisecond'
		FROM generate_series(0, $4::int - 1) AS i
	`, workspaceID, testUserID, hiddenIssueID, inbox.ActiveLimit)
	visibleID := insertOldInboxRow(t, workspaceID, "visible", nil)

	trimInbox(t, workspaceID)

	if isArchived(t, visibleID) {
		t.Fatal("a visible row was archived to make room for rows the member cannot see")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND NOT archived`, hiddenIssueID); n != inbox.ActiveLimit {
		t.Fatalf("hidden rows active = %d, want untouched %d", n, inbox.ActiveLimit)
	}
}

func returnedAtOf(t *testing.T, id string) *time.Time {
	t.Helper()
	var at *time.Time
	dbfx.QueryRow(t, `SELECT returned_at FROM inbox_item WHERE id = $1`, id).Scan(&at)
	return at
}

// A snooze written before returned_at existed (or by an older server during a
// rollout) carries only snoozed_until. The trim gives its issue's newest row
// the wake rank before it archives anything, so the woken issue stays.
func TestInboxTrimmerKeepsSnoozeWrittenWithoutReturn(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit legacy snooze")
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit, time.Now().UTC().Add(-time.Hour), time.Millisecond)
	issueID := dbfx.Issue(t, "Legacy snoozed issue", testutil.Cols{"workspace_id": workspaceID})
	wake := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
	legacy := testutil.Cols{"issue_id": issueID, "snoozed_until": wake, "snoozed_at": wake.Add(-24 * time.Hour)}
	olderID := insertOldInboxRow(t, workspaceID, "legacy-older", legacy)
	legacy["created_at"] = time.Now().Add(-47 * time.Hour)
	newestID := insertOldInboxRow(t, workspaceID, "legacy-newest", legacy)

	trimInbox(t, workspaceID)

	if isArchived(t, newestID) {
		t.Fatal("the woken issue's newest row was archived")
	}
	if at := returnedAtOf(t, newestID); at == nil || !at.Equal(wake) {
		t.Fatalf("newest returned_at = %v, want the wake time %v", at, wake)
	}
	if !isArchived(t, olderID) {
		t.Fatal("the woken issue's older row should rank by its own time and go")
	}
}

// A comment that joined a running snooze is the issue's newest row when the
// snooze ends, so it, not the row that was newest when snoozing, keeps the
// issue's rank.
func TestInboxTrimmerHandsReturnToRowThatJoinedSnooze(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit joined snooze")
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit, time.Now().UTC().Add(-time.Hour), time.Millisecond)
	issueID := dbfx.Issue(t, "Snoozed issue with news", testutil.Cols{"workspace_id": workspaceID})
	wake := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
	snoozedAt := wake.Add(-24 * time.Hour)
	beforeID := insertOldInboxRow(t, workspaceID, "before-snooze", testutil.Cols{
		"issue_id": issueID, "snoozed_until": wake, "snoozed_at": snoozedAt, "returned_at": wake,
	})
	joinedID := insertOldInboxRow(t, workspaceID, "joined-snooze", testutil.Cols{
		"issue_id": issueID, "snoozed_until": wake, "snoozed_at": snoozedAt, "created_at": time.Now().Add(-2 * time.Hour),
	})

	trimInbox(t, workspaceID)

	if isArchived(t, joinedID) {
		t.Fatal("the comment that arrived during the snooze was archived")
	}
	if at := returnedAtOf(t, joinedID); at == nil || !at.Equal(wake) {
		t.Fatalf("joined row returned_at = %v, want the wake time %v", at, wake)
	}
	if returnedAtOf(t, beforeID) != nil || !isArchived(t, beforeID) {
		t.Fatal("the row newest at snooze time kept the issue's rank")
	}
}

// Bulk archives drop the return rank, so a later restore earns it anew on the
// restored row only.
func TestBulkArchiveClearsReturn(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit bulk archive")
	allID := insertOldInboxRow(t, workspaceID, "archive-all", testutil.Cols{"returned_at": time.Now(), "read": true})
	ctx := context.Background()
	if _, err := testHandler.Queries.ArchiveAllReadInbox(ctx, db.ArchiveAllReadInboxParams{
		WorkspaceID: parseUUID(workspaceID), RecipientID: parseUUID(testUserID),
	}); err != nil {
		t.Fatal(err)
	}
	if !isArchived(t, allID) || returnedAtOf(t, allID) != nil {
		t.Fatalf("archive all read: archived=%v returned_at=%v", isArchived(t, allID), returnedAtOf(t, allID))
	}

	dbfx.Exec(t, `UPDATE inbox_item SET archived = false, returned_at = now() WHERE id = $1`, allID)
	if _, err := testHandler.Queries.ArchiveAllInbox(ctx, db.ArchiveAllInboxParams{
		WorkspaceID: parseUUID(workspaceID), RecipientID: parseUUID(testUserID),
	}); err != nil {
		t.Fatal(err)
	}
	if !isArchived(t, allID) || returnedAtOf(t, allID) != nil {
		t.Fatalf("archive all: archived=%v returned_at=%v", isArchived(t, allID), returnedAtOf(t, allID))
	}
}

// The trimmer's startup backfill gives a snooze set before returned_at
// existed its return rank, so the woken issue is in the very first list read
// of a full inbox.
func TestReturnedAtBackfillRanksLegacySnooze(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit backfill")
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit, time.Now().UTC().Add(-time.Hour), time.Millisecond)
	issueID := dbfx.Issue(t, "Legacy snooze", testutil.Cols{"workspace_id": workspaceID})
	wake := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
	legacy := testutil.Cols{"issue_id": issueID, "snoozed_until": wake, "snoozed_at": wake.Add(-24 * time.Hour)}
	olderID := insertOldInboxRow(t, workspaceID, "legacy-older", legacy)
	legacy["created_at"] = time.Now().Add(-47 * time.Hour)
	newestID := insertOldInboxRow(t, workspaceID, "legacy-newest", legacy)

	trimmer := inbox.NewTrimmer(testHandler.Queries, nil)
	if n, err := trimmer.Backfill(context.Background(), parseUUID(workspaceID)); err != nil || n != 1 {
		t.Fatalf("Backfill = %d, %v; want the one snoozed issue ranked", n, err)
	}

	if at := returnedAtOf(t, newestID); at == nil || !at.Equal(wake) || returnedAtOf(t, olderID) != nil {
		t.Fatalf("newest returned_at=%v older=%v; want only the newest at the wake time", at, returnedAtOf(t, olderID))
	}
	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox", workspaceID)).
		Want(http.StatusOK).
		JSON(&items)
	if len(items) == 0 || items[0].ID != newestID {
		t.Fatal("the woken issue is missing from the first read of a full inbox")
	}
}

// Even before its rank is settled, the archive never takes a woken issue's
// newest row on that stale rank.
func TestArchiveInboxOverflowSparesUnsettledReturn(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit archive guard")
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit, time.Now().UTC().Add(-time.Hour), time.Millisecond)
	issueID := dbfx.Issue(t, "Unsettled snooze", testutil.Cols{"workspace_id": workspaceID})
	wake := time.Now().Add(-time.Minute)
	newestID := insertOldInboxRow(t, workspaceID, "unsettled", testutil.Cols{
		"issue_id": issueID, "snoozed_until": wake, "snoozed_at": wake.Add(-24 * time.Hour),
	})

	archived, err := testHandler.Queries.ArchiveInboxOverflow(context.Background(), db.ArchiveInboxOverflowParams{
		WorkspaceID: parseUUID(workspaceID), RecipientType: "member", RecipientID: parseUUID(testUserID),
		Keep: inbox.ActiveLimit, BatchSize: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if isArchived(t, newestID) || archived != 0 {
		t.Fatalf("archived=%d, woken row archived=%v; want the woken issue's newest row spared", archived, isArchived(t, newestID))
	}
}

// A notification that joins a running snooze takes the issue's return rank
// when it is written, not only once the trimmer settles it.
func TestDeliverIntoSnoozeHandsReturnToNewRow(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit join snooze")
	issueID := dbfx.Issue(t, "Snoozed issue", testutil.Cols{"workspace_id": workspaceID})
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	holderID := insertOldInboxRow(t, workspaceID, "holder", testutil.Cols{
		"issue_id": issueID, "snoozed_until": until, "snoozed_at": time.Now(), "returned_at": until,
	})

	item, inserted, err := inbox.Deliver(context.Background(), testHandler.Queries, nil, inbox.Notification{
		Params: db.CreateInboxItemParams{
			WorkspaceID: parseUUID(workspaceID), RecipientType: "member", RecipientID: parseUUID(testUserID),
			Type: "new_comment", Severity: "info", IssueID: parseUUID(issueID), Title: "joined",
			Details: []byte(`{}`),
		},
		IgnorePreferences: true,
	})
	if err != nil || !inserted {
		t.Fatalf("Deliver = inserted %v, err %v", inserted, err)
	}
	joinedID := uuidToString(item.ID)
	if at := returnedAtOf(t, joinedID); at == nil || !at.Equal(until) || returnedAtOf(t, holderID) != nil {
		t.Fatalf("joined returned_at=%v holder=%v; want the rank handed to the joining row", at, returnedAtOf(t, holderID))
	}
}

// NormalizeInboxReturns skips rows another write holds instead of waiting on
// them, and settles them on a later call.
func TestNormalizeInboxReturnsSkipsLockedRows(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit normalize lock")
	issueID := dbfx.Issue(t, "Unsettled snooze", testutil.Cols{"workspace_id": workspaceID})
	wake := time.Now().Add(-time.Minute)
	newestID := insertOldInboxRow(t, workspaceID, "unsettled", testutil.Cols{
		"issue_id": issueID, "snoozed_until": wake, "snoozed_at": wake.Add(-24 * time.Hour),
	})
	params := db.NormalizeInboxReturnsParams{
		WorkspaceID: parseUUID(workspaceID), RecipientType: "member", RecipientID: parseUUID(testUserID), BatchSize: 500,
	}

	ctx := context.Background()
	tx, err := dbfx.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM inbox_item WHERE id = $1 FOR UPDATE`, newestID); err != nil {
		t.Fatal(err)
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	n, err := testHandler.Queries.NormalizeInboxReturns(callCtx, params)
	cancel()
	if rbErr := tx.Rollback(ctx); rbErr != nil {
		t.Fatal(rbErr)
	}
	if err != nil || n != 0 {
		t.Fatalf("normalize under a held lock = %d, %v; want 0 without waiting", n, err)
	}

	if n, err := testHandler.Queries.NormalizeInboxReturns(ctx, params); err != nil || n != 1 {
		t.Fatalf("normalize after the lock = %d, %v; want the row settled", n, err)
	}
	if at := returnedAtOf(t, newestID); at == nil {
		t.Fatal("the woken row still has no return rank")
	}
}

// Two rows joining a snooze hand the rank over in either order and leave it
// on the newer one.
func TestHandInboxReturnToRowInReverseOrder(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit handoff order")
	issueID := dbfx.Issue(t, "Snoozed issue", testutil.Cols{"workspace_id": workspaceID})
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	holderID := insertOldInboxRow(t, workspaceID, "holder", testutil.Cols{
		"issue_id": issueID, "snoozed_until": until, "snoozed_at": time.Now(), "returned_at": until,
	})
	ctx := context.Background()
	create := func(title string) db.InboxItem {
		item, err := testHandler.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID: parseUUID(workspaceID), RecipientType: "member", RecipientID: parseUUID(testUserID),
			Type: "new_comment", Severity: "info", IssueID: parseUUID(issueID), Title: title, Details: []byte(`{}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	hand := func(item db.InboxItem) {
		if err := testHandler.Queries.HandInboxReturnToRow(ctx, db.HandInboxReturnToRowParams{
			WorkspaceID: item.WorkspaceID, RecipientType: item.RecipientType, RecipientID: item.RecipientID,
			IssueID: item.IssueID, KeepID: item.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	a := create("a")
	b := create("b")
	hand(b)
	hand(a)

	if at := returnedAtOf(t, uuidToString(b.ID)); at == nil || !at.Equal(until) {
		t.Fatalf("newer row returned_at = %v, want %v", at, until)
	}
	if returnedAtOf(t, uuidToString(a.ID)) != nil || returnedAtOf(t, holderID) != nil {
		t.Fatal("an older row kept the issue's return rank")
	}
}

// While a duplicate rank is held by another write, the trim archives
// nothing; once it is settled, it keeps exactly the newest ActiveLimit rows.
func TestInboxTrimmerWaitsForHeldDuplicateRank(t *testing.T) {
	workspaceID := limitInboxWorkspace(t, "Inbox limit held rank")
	seedInboxRows(t, workspaceID, testUserID, inbox.ActiveLimit, time.Now().UTC().Add(-time.Hour), time.Millisecond)
	issueID := dbfx.Issue(t, "Duplicate rank", testutil.Cols{"workspace_id": workspaceID})
	now := time.Now()
	staleID := insertOldInboxRow(t, workspaceID, "stale-holder", testutil.Cols{"issue_id": issueID, "returned_at": now})
	insertOldInboxRow(t, workspaceID, "newest", testutil.Cols{
		"issue_id": issueID, "returned_at": now, "created_at": now.Add(-47 * time.Hour),
	})
	plain := func() int {
		return dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND issue_id IS NULL AND NOT archived`, workspaceID)
	}

	ctx := context.Background()
	tx, err := dbfx.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM inbox_item WHERE id = $1 FOR UPDATE`, staleID); err != nil {
		t.Fatal(err)
	}
	trimInbox(t, workspaceID)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n := plain(); n != inbox.ActiveLimit {
		t.Fatalf("plain rows = %d while the rank was held, want all %d kept", n, inbox.ActiveLimit)
	}

	trimInbox(t, workspaceID)
	if n := plain(); n != inbox.ActiveLimit-1 || !isArchived(t, staleID) {
		t.Fatalf("plain rows = %d, stale holder archived=%v; want %d kept beside the issue", n, isArchived(t, staleID), inbox.ActiveLimit-1)
	}
}
