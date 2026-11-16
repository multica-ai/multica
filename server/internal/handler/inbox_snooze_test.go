package handler

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type snoozeFixture struct {
	workspaceID string
	issueID     string
	otherIssue  string
}

func newSnoozeFixture(t *testing.T) snoozeFixture {
	t.Helper()
	workspaceID := dbfx.Workspace(t, "Inbox snooze", "inbox-snooze-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
	return snoozeFixture{
		workspaceID: workspaceID,
		issueID:     dbfx.Issue(t, "Snoozed issue", testutil.Cols{"workspace_id": workspaceID}),
		otherIssue:  dbfx.Issue(t, "Other issue", testutil.Cols{"workspace_id": workspaceID}),
	}
}

func (f snoozeFixture) insert(t *testing.T, issueID, title string, read bool, createdAt time.Time) string {
	t.Helper()
	return dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id":   f.workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           "new_comment",
		"severity":       "info",
		"issue_id":       issueID,
		"title":          title,
		"read":           read,
		"created_at":     createdAt,
	})
}

func (f snoozeFixture) snooze(t *testing.T, itemID string, until time.Time) *testutil.Response {
	t.Helper()
	req := testutil.WithHeaders(
		testutil.JSONRequest(http.MethodPost, "/api/inbox/"+itemID+"/snooze", map[string]any{"until": until.UTC().Format(time.RFC3339)}),
		"X-User-ID", testUserID,
		"X-Workspace-ID", f.workspaceID,
	)
	return testutil.Call(t, inboxWorkspaceHandler(testHandler.SnoozeInboxItem), withURLParam(req, "id", itemID))
}

func (f snoozeFixture) list(t *testing.T) []InboxItemResponse {
	t.Helper()
	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox), inboxRequest(http.MethodGet, "/api/inbox", f.workspaceID)).
		Want(http.StatusOK).JSON(&items)
	return items
}

func (f snoozeFixture) snoozed(t *testing.T) []SnoozedInboxResponse {
	t.Helper()
	var rows []SnoozedInboxResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListSnoozedInbox), inboxRequest(http.MethodGet, "/api/inbox/snoozed", f.workspaceID)).
		Want(http.StatusOK).JSON(&rows)
	return rows
}

func (f snoozeFixture) unreadCount(t *testing.T) int64 {
	t.Helper()
	var resp struct {
		Count int64 `json:"count"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.CountUnreadInbox), inboxRequest(http.MethodGet, "/api/inbox/unread-count", f.workspaceID)).
		Want(http.StatusOK).JSON(&resp)
	return resp.Count
}

func inboxIssueIDs(items []InboxItemResponse) map[string]int {
	ids := map[string]int{}
	for _, item := range items {
		if item.IssueID != nil {
			ids[*item.IssueID]++
		}
	}
	return ids
}

func TestSnoozeInboxTakesTheWholeIssueOutUntilItsTime(t *testing.T) {
	f := newSnoozeFixture(t)
	now := time.Now()
	f.insert(t, f.issueID, "older", false, now.Add(-2*time.Hour))
	newest := f.insert(t, f.issueID, "newest", true, now.Add(-time.Hour))
	f.insert(t, f.otherIssue, "other", false, now.Add(-time.Hour))

	until := now.Add(48 * time.Hour).Truncate(time.Second)
	f.snooze(t, newest, until).Want(http.StatusOK)

	if ids := inboxIssueIDs(f.list(t)); ids[f.issueID] != 0 || ids[f.otherIssue] != 1 {
		t.Fatalf("listed issues = %v, want only the other issue", ids)
	}
	if got := f.unreadCount(t); got != 1 {
		t.Fatalf("unread count = %d, want 1 (the snoozed issue does not count)", got)
	}
	rows := f.snoozed(t)
	if len(rows) != 1 || rows[0].IssueID != f.issueID || rows[0].ItemID != newest || rows[0].Updates != 0 {
		t.Fatalf("snoozed = %+v, want the issue with its newest row and no updates", rows)
	}
	if got, err := time.Parse(time.RFC3339, rows[0].SnoozedUntil); err != nil || !got.Equal(until) {
		t.Fatalf("snoozed_until = %q, want %s", rows[0].SnoozedUntil, until)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1 AND read = false", newest); got != 1 {
		t.Fatal("the newest row stayed read; a snoozed issue comes back unread")
	}

	// Its time passes: back in the list, carrying when it came back.
	dbfx.Exec(t, `UPDATE inbox_item SET snoozed_until = now() - interval '1 minute' WHERE issue_id = $1`, f.issueID)
	items := f.list(t)
	if ids := inboxIssueIDs(items); ids[f.issueID] != 2 {
		t.Fatalf("listed issues after the snooze ran out = %v, want both rows of the issue", ids)
	}
	for _, item := range items {
		if item.IssueID != nil && *item.IssueID == f.issueID && item.SnoozedUntil == nil {
			t.Fatal("a row back from a snooze lost snoozed_until")
		}
	}
	if got := len(f.snoozed(t)); got != 0 {
		t.Fatalf("snoozed after the time passed = %d, want 0", got)
	}
}

func TestSnoozeInboxRejectsTimesOutsideTheWindow(t *testing.T) {
	f := newSnoozeFixture(t)
	item := f.insert(t, f.issueID, "row", false, time.Now())
	f.snooze(t, item, time.Now().Add(-time.Minute)).Want(http.StatusBadRequest)
	f.snooze(t, item, time.Now().Add(inbox.MaxSnooze+time.Hour)).Want(http.StatusBadRequest)
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1 AND snoozed_until IS NOT NULL", item); got != 0 {
		t.Fatal("a rejected snooze was written")
	}
}

func TestUnsnoozeInboxBringsTheIssueBack(t *testing.T) {
	f := newSnoozeFixture(t)
	item := f.insert(t, f.issueID, "row", true, time.Now())
	f.snooze(t, item, time.Now().Add(time.Hour)).Want(http.StatusOK)

	req := testutil.WithHeaders(
		testutil.JSONRequest(http.MethodPost, "/api/inbox/"+item+"/unsnooze", map[string]any{"read": true}),
		"X-User-ID", testUserID,
		"X-Workspace-ID", f.workspaceID,
	)
	testutil.Call(t, inboxWorkspaceHandler(testHandler.UnsnoozeInboxItem), withURLParam(req, "id", item)).Want(http.StatusOK)

	if ids := inboxIssueIDs(f.list(t)); ids[f.issueID] != 1 {
		t.Fatalf("listed issues = %v, want the issue back", ids)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1 AND read = true AND snoozed_until IS NULL AND snoozed_at IS NULL", item); got != 1 {
		t.Fatal("undo did not restore the row as read and unsnoozed")
	}
}

func TestInboxRowsWrittenIntoASnoozeJoinItUnlessTheyWakeIt(t *testing.T) {
	f := newSnoozeFixture(t)
	item := f.insert(t, f.issueID, "row", false, time.Now().Add(-time.Hour))
	f.snooze(t, item, time.Now().Add(24*time.Hour)).Want(http.StatusOK)

	create := func(notifType string, details string) db.InboxItem {
		t.Helper()
		row, inserted, err := inbox.Deliver(context.Background(), testHandler.Queries, nil, inbox.Notification{Params: db.CreateInboxItemParams{
			WorkspaceID:   parseUUID(f.workspaceID),
			RecipientType: "member",
			RecipientID:   parseUUID(testUserID),
			Type:          notifType,
			Severity:      "info",
			IssueID:       parseUUID(f.issueID),
			Title:         "Snoozed issue",
			Details:       []byte(details),
		}})
		if err != nil || !inserted {
			t.Fatalf("create %s: inserted=%v err=%v", notifType, inserted, err)
		}
		return row
	}

	for _, quiet := range []struct{ notifType, details string }{
		{"new_comment", `{}`},
		{"status_changed", `{}`},
		{"mentioned", `{"mention_via":"all"}`},
		{"mentioned", `{"mention_via":"squad"}`},
	} {
		row := create(quiet.notifType, quiet.details)
		if !inbox.IsSnoozed(row, time.Now()) {
			t.Fatalf("%s %s woke the snooze", quiet.notifType, quiet.details)
		}
	}
	rows := f.snoozed(t)
	if len(rows) != 1 || rows[0].Updates != 4 {
		t.Fatalf("snoozed = %+v, want one issue with 4 updates", rows)
	}
	if ids := inboxIssueIDs(f.list(t)); ids[f.issueID] != 0 {
		t.Fatalf("listed issues = %v, want the snoozed issue still out", ids)
	}

	woke := create("mentioned", `{"mention_via":"member"}`)
	if woke.SnoozedUntil.Valid {
		t.Fatal("a mention by name did not wake the snooze")
	}
	if got := len(f.snoozed(t)); got != 0 {
		t.Fatalf("snoozed after the wake = %d, want 0", got)
	}
	if ids := inboxIssueIDs(f.list(t)); ids[f.issueID] != 6 {
		t.Fatalf("listed issues = %v, want all six rows back", ids)
	}

	// Without a snooze a new row is written as before.
	if row := create("new_comment", `{}`); row.SnoozedUntil.Valid {
		t.Fatal("a row on an issue that is not snoozed was written snoozed")
	}
}

func TestInboxBatchActionsSkipSnoozedIssues(t *testing.T) {
	f := newSnoozeFixture(t)
	snoozedItem := f.insert(t, f.issueID, "snoozed", false, time.Now())
	f.insert(t, f.otherIssue, "listed", false, time.Now())
	f.snooze(t, snoozedItem, time.Now().Add(time.Hour)).Want(http.StatusOK)

	testutil.Call(t, inboxWorkspaceHandler(testHandler.MarkAllInboxRead), inboxRequest(http.MethodPost, "/api/inbox/mark-all-read", f.workspaceID)).Want(http.StatusOK)
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1 AND read = false", snoozedItem); got != 1 {
		t.Fatal("mark all read read a snoozed issue")
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ArchiveAllInbox), inboxRequest(http.MethodPost, "/api/inbox/archive-all", f.workspaceID)).Want(http.StatusOK)
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1 AND archived = false", snoozedItem); got != 1 {
		t.Fatal("archive all archived a snoozed issue")
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND archived = false", f.otherIssue); got != 0 {
		t.Fatal("archive all left the listed issue")
	}

	// Viewing the snoozed issue elsewhere leaves it unread.
	testutil.Call(t, inboxWorkspaceHandler(testHandler.MarkIssueInboxRead),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/issues/"+f.issueID+"/read", f.workspaceID), "issueId", f.issueID)).
		Want(http.StatusOK)
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1 AND read = false", snoozedItem); got != 1 {
		t.Fatal("viewing a snoozed issue read it")
	}

	// Archiving it ends the snooze, so a restore does not bring it back hidden.
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ArchiveInboxItem),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+snoozedItem+"/archive", f.workspaceID), "id", snoozedItem)).
		Want(http.StatusOK)
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1 AND archived = true AND snoozed_until IS NULL", snoozedItem); got != 1 {
		t.Fatal("archiving kept the snooze")
	}
}

// subscribeInboxEvents records the inbox events published for one workspace.
// Bus handlers are never unsubscribed in this suite, and Publish is
// synchronous, so the slice is complete once a request returns.
func subscribeInboxEvents(workspaceID string) *[]string {
	var seen []string
	var mu sync.Mutex
	for _, eventType := range []string{
		protocol.EventInboxNew, protocol.EventInboxSnoozed, protocol.EventInboxUnsnoozed, protocol.EventInboxBatchRead,
	} {
		testHandler.Bus.Subscribe(eventType, func(e events.Event) {
			if e.WorkspaceID != workspaceID {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, e.Type)
		})
	}
	return &seen
}

func TestSnoozeInboxRefreshesClientsThatPredateIt(t *testing.T) {
	f := newSnoozeFixture(t)
	item := f.insert(t, f.issueID, "row", false, time.Now())
	seen := subscribeInboxEvents(f.workspaceID)

	f.snooze(t, item, time.Now().Add(time.Hour)).Want(http.StatusOK)
	want := []string{protocol.EventInboxSnoozed, protocol.EventInboxBatchRead}
	if !slices.Equal(*seen, want) {
		t.Fatalf("snooze published %v, want %v", *seen, want)
	}

	// A row written into the snooze is announced as snoozed only: never as new,
	// and without the refresh, since the list does not change.
	*seen = nil
	row, inserted, err := inbox.Deliver(context.Background(), testHandler.Queries, nil, inbox.Notification{Params: db.CreateInboxItemParams{
		WorkspaceID:   parseUUID(f.workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(testUserID),
		Type:          "new_comment",
		Severity:      "info",
		IssueID:       parseUUID(f.issueID),
		Title:         "Snoozed issue",
		Details:       []byte(`{}`),
	}})
	if err != nil || !inserted {
		t.Fatalf("create: inserted=%v err=%v", inserted, err)
	}
	if got := inbox.NewItemEventType(row); got != protocol.EventInboxSnoozed {
		t.Fatalf("a row written into a snooze is announced as %s, want %s", got, protocol.EventInboxSnoozed)
	}

	req := testutil.WithHeaders(
		testutil.JSONRequest(http.MethodPost, "/api/inbox/"+item+"/unsnooze", map[string]any{}),
		"X-User-ID", testUserID,
		"X-Workspace-ID", f.workspaceID,
	)
	testutil.Call(t, inboxWorkspaceHandler(testHandler.UnsnoozeInboxItem), withURLParam(req, "id", item)).Want(http.StatusOK)
	want = []string{protocol.EventInboxUnsnoozed, protocol.EventInboxBatchRead}
	if !slices.Equal(*seen, want) {
		t.Fatalf("unsnooze published %v, want %v", *seen, want)
	}
}

func TestWakesSnooze(t *testing.T) {
	cases := []struct {
		notifType string
		details   string
		want      bool
	}{
		{"issue_assigned", ``, true},
		{"task_failed", `{}`, true},
		{"children_done", `{}`, true},
		{"app_action_requested", `{}`, true},
		{"app_action_reminder", `{}`, true},
		{"mentioned", `{"mention_via":"member"}`, true},
		{"mentioned", `{}`, true},
		{"mentioned", `{"mention_via":"all"}`, false},
		{"mentioned", `{"mention_via":"squad"}`, false},
		{"new_comment", `{}`, false},
		{"status_changed", `{}`, false},
		{"reaction_added", `{}`, false},
	}
	for _, c := range cases {
		if got := inbox.WakesSnooze(c.notifType, []byte(c.details)); got != c.want {
			t.Errorf("WakesSnooze(%s, %s) = %v, want %v", c.notifType, c.details, got, c.want)
		}
	}
}
