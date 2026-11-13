package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// inboxSectionsFixture is one workspace whose inbox rows the caller owns.
type inboxSectionsFixture struct {
	t           *testing.T
	workspaceID string
}

func newInboxSectionsFixture(t *testing.T, name string) inboxSectionsFixture {
	t.Helper()
	workspaceID := dbfx.Workspace(t, name, "inbox-sections-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
	return inboxSectionsFixture{t: t, workspaceID: workspaceID}
}

func (f inboxSectionsFixture) issue(title string) string {
	f.t.Helper()
	return dbfx.Issue(f.t, title, testutil.Cols{"workspace_id": f.workspaceID})
}

// row inserts one of the caller's inbox rows. A nil issue makes it issue-less.
func (f inboxSectionsFixture) row(issueID any, notifType string, over testutil.Cols) string {
	f.t.Helper()
	cols := testutil.Cols{
		"workspace_id":   f.workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           notifType,
		"severity":       "info",
		"title":          notifType,
	}
	if issueID != nil {
		cols["issue_id"] = issueID
	}
	for k, v := range over {
		cols[k] = v
	}
	return dbfx.Insert(f.t, "inbox_item", cols)
}

func (f inboxSectionsFixture) summary() InboxWorkspaceUnreadResponse {
	f.t.Helper()
	var summary []InboxWorkspaceUnreadResponse
	testutil.Call(f.t, inboxWorkspaceHandler(testHandler.UnreadInboxSummary),
		inboxRequest(http.MethodGet, "/api/inbox/unread-summary", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&summary)
	for _, s := range summary {
		if s.WorkspaceID == f.workspaceID {
			return s
		}
	}
	return InboxWorkspaceUnreadResponse{WorkspaceID: f.workspaceID}
}

func (f inboxSectionsFixture) list(query string) []InboxItemResponse {
	f.t.Helper()
	var items []InboxItemResponse
	testutil.Call(f.t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox"+query, f.workspaceID)).
		Want(http.StatusOK).
		JSON(&items)
	return items
}

func (f inboxSectionsFixture) snooze(id string, body any) *testutil.Response {
	f.t.Helper()
	req := testutil.WithHeaders(
		testutil.JSONRequest(http.MethodPost, "/api/inbox/"+id+"/snooze", body),
		"X-User-ID", testUserID,
		"X-Workspace-ID", f.workspaceID,
	)
	return testutil.Call(f.t, inboxWorkspaceHandler(testHandler.SnoozeInboxItem), withURLParam(req, "id", id))
}

func (f inboxSectionsFixture) unsnooze(id string) int64 {
	f.t.Helper()
	var resp struct {
		Count int64 `json:"count"`
	}
	testutil.Call(f.t, inboxWorkspaceHandler(testHandler.UnsnoozeInboxItem),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+id+"/unsnooze", f.workspaceID), "id", id)).
		Want(http.StatusOK).
		JSON(&resp)
	return resp.Count
}

// The badge counts the issues that wait on the member, read or not, and
// nothing they only follow (MUL-9215).
func TestInboxSummaryCountsIssuesThatNeedTheMember(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox needs-you count")
	future := time.Now().Add(2 * time.Hour)
	past := time.Now().Add(-2 * time.Hour)

	// Counted: opening an issue is not handling it, and two rows are one issue.
	opened := f.issue("Opened but not handled")
	f.row(opened, "new_comment", testutil.Cols{"section": "needs_you", "read": true})
	f.row(opened, "agent_completed", testutil.Cols{"section": "needs_you", "read": true})

	// Not counted: the member only follows this one.
	f.row(f.issue("Followed"), "new_comment", testutil.Cols{"section": "updates"})

	// Rows from before sections were stored: an unread one addressed to the
	// member counts, a read one was handled when it was read, and a comment
	// has no recorded relation.
	f.row(f.issue("Legacy unread mention"), "mentioned", nil)
	f.row(f.issue("Legacy read mention"), "mentioned", testutil.Cols{"read": true})
	f.row(f.issue("Legacy comment"), "new_comment", nil)
	// The retired routing wrote other values; they follow the legacy rule.
	f.row(f.issue("Retired routing"), "issue_assigned", testutil.Cols{"section": "action"})

	// Snoozed: out until the time passes, back once it has.
	f.row(f.issue("Snoozed"), "mentioned", testutil.Cols{"section": "needs_you", "snoozed_until": future})
	f.row(f.issue("Snooze over"), "mentioned", testutil.Cols{"section": "needs_you", "snoozed_until": past})

	// Something new addressed to the member brings a snoozed issue back early;
	// an update on it does not.
	wokenEarly := f.issue("Woken early")
	f.row(wokenEarly, "new_comment", testutil.Cols{"section": "needs_you", "snoozed_until": future, "read": true})
	f.row(wokenEarly, "mentioned", testutil.Cols{"section": "needs_you"})
	stillSnoozed := f.issue("Still snoozed")
	f.row(stillSnoozed, "mentioned", testutil.Cols{"section": "needs_you", "snoozed_until": future, "read": true})
	f.row(stillSnoozed, "priority_changed", testutil.Cols{"section": "updates"})

	// Archived rows are gone, and an issue-less notice counts on its own.
	f.row(f.issue("Archived"), "mentioned", testutil.Cols{"section": "needs_you", "archived": true})
	f.row(nil, "autopilot_paused", testutil.Cols{"section": "needs_you"})

	got := f.summary()
	// opened, legacy unread mention, retired routing, snooze over, woken
	// early, issue-less notice.
	if got.NeedsYou != 6 {
		t.Errorf("needs_you = %d, want 6", got.NeedsYou)
	}
}

// Clients that predate the sectioned Inbox show `count` as their badge, so it
// stays the number of unread issues. Only a snooze takes an issue out of it.
func TestInboxSummaryKeepsTheUnreadCount(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox unread count")

	f.row(f.issue("Unread update"), "new_comment", testutil.Cols{"section": "updates"})
	f.row(f.issue("Read and waiting"), "mentioned", testutil.Cols{"section": "needs_you", "read": true})
	f.row(f.issue("Snoozed"), "mentioned", testutil.Cols{"section": "needs_you", "snoozed_until": time.Now().Add(time.Hour)})

	got := f.summary()
	if got.Count != 1 {
		t.Errorf("count = %d, want 1: the unread update", got.Count)
	}
	if got.NeedsYou != 1 {
		t.Errorf("needs_you = %d, want 1: the read row still waiting", got.NeedsYou)
	}

	var unread struct {
		Count int64 `json:"count"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.CountUnreadInbox),
		inboxRequest(http.MethodGet, "/api/inbox/unread-count", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&unread)
	if unread.Count != 1 {
		t.Errorf("unread-count = %d, want 1: a snoozed row is not unread", unread.Count)
	}
}

// A workspace with nothing unread still appears once something waits there.
func TestInboxSummaryListsAWorkspaceWithOnlyReadRowsWaiting(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox read but waiting")
	f.row(f.issue("Read and waiting"), "issue_assigned", testutil.Cols{"section": "needs_you", "read": true})

	got := f.summary()
	if got.Count != 0 || got.NeedsYou != 1 {
		t.Errorf("summary = %+v, want count 0 and needs_you 1", got)
	}
}

func TestListInboxReportsSectionAndHidesSnoozedRowsUnlessAsked(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox list sections")
	until := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)

	waiting := f.row(f.issue("Waiting"), "new_comment", testutil.Cols{"section": "needs_you", "read": true})
	followed := f.row(f.issue("Followed"), "new_comment", testutil.Cols{"section": "updates"})
	legacy := f.row(f.issue("Legacy"), "mentioned", nil)
	snoozed := f.row(f.issue("Snoozed"), "mentioned", testutil.Cols{"section": "needs_you", "snoozed_until": until})
	back := f.row(f.issue("Back"), "mentioned", testutil.Cols{"section": "needs_you", "snoozed_until": time.Now().Add(-time.Hour)})

	byID := func(items []InboxItemResponse) map[string]InboxItemResponse {
		out := make(map[string]InboxItemResponse, len(items))
		for _, item := range items {
			out[item.ID] = item
		}
		return out
	}

	// A client that predates the Snoozed section does not ask for those rows
	// and must not get them: it would show a snoozed issue as plainly unread.
	plain := byID(f.list(""))
	if _, ok := plain[snoozed]; ok {
		t.Errorf("a snoozed row was listed without include_snoozed")
	}
	if _, ok := plain[back]; !ok {
		t.Errorf("a row whose snooze is over was not listed")
	}
	for id, want := range map[string]string{waiting: "needs_you", followed: "updates", legacy: "needs_you"} {
		if got := plain[id].Section; got != want {
			t.Errorf("section of %s = %q, want %q", id, got, want)
		}
	}

	all := byID(f.list("?include_snoozed=true"))
	got, ok := all[snoozed]
	if !ok {
		t.Fatalf("include_snoozed=true did not list the snoozed row")
	}
	if got.SnoozedUntil == nil {
		t.Fatalf("snoozed_until missing on the snoozed row")
	}
	if parsed, err := time.Parse(time.RFC3339, *got.SnoozedUntil); err != nil || !parsed.Equal(until) {
		t.Errorf("snoozed_until = %q, want %s", *got.SnoozedUntil, until.Format(time.RFC3339))
	}
	if all[waiting].SnoozedUntil != nil {
		t.Errorf("snoozed_until = %q on a row that was never snoozed", *all[waiting].SnoozedUntil)
	}
}

func TestSnoozeInboxItemSnoozesTheWholeIssue(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox snooze")
	issueID := f.issue("Snoozed issue")
	older := f.row(issueID, "issue_assigned", testutil.Cols{"section": "needs_you", "read": true})
	newest := f.row(issueID, "new_comment", testutil.Cols{"section": "needs_you", "read": true})
	archived := f.row(issueID, "new_comment", testutil.Cols{"section": "needs_you", "archived": true})
	other := f.row(f.issue("Another issue"), "mentioned", testutil.Cols{"section": "needs_you"})

	published := make(chan map[string]any, 4)
	for _, eventType := range []string{protocol.EventInboxSnoozed, protocol.EventInboxUnsnoozed} {
		testHandler.Bus.Subscribe(eventType, func(e events.Event) {
			payload, ok := e.Payload.(map[string]any)
			if !ok || payload["item_id"] != newest {
				return
			}
			select {
			case published <- map[string]any{"type": e.Type, "recipient_id": payload["recipient_id"]}:
			default:
			}
		})
	}

	until := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	var resp struct {
		Count        int64  `json:"count"`
		SnoozedUntil string `json:"snoozed_until"`
	}
	f.snooze(newest, map[string]string{"until": until.Format(time.RFC3339)}).Want(http.StatusOK).JSON(&resp)
	if resp.Count != 2 {
		t.Fatalf("count = %d, want the issue's two active rows", resp.Count)
	}
	if resp.SnoozedUntil != until.Format(time.RFC3339) {
		t.Errorf("snoozed_until = %q, want %q", resp.SnoozedUntil, until.Format(time.RFC3339))
	}

	snoozedUntil := func(id string) *time.Time {
		t.Helper()
		var at *time.Time
		dbfx.QueryRow(t, `SELECT snoozed_until FROM inbox_item WHERE id = $1`, id).Scan(&at)
		return at
	}
	isRead := func(id string) bool {
		t.Helper()
		var read bool
		dbfx.QueryRow(t, `SELECT read FROM inbox_item WHERE id = $1`, id).Scan(&read)
		return read
	}
	for _, id := range []string{older, newest} {
		if at := snoozedUntil(id); at == nil || !at.Equal(until) {
			t.Errorf("snoozed_until of %s = %v, want %s", id, at, until)
		}
	}
	for _, id := range []string{archived, other} {
		if at := snoozedUntil(id); at != nil {
			t.Errorf("snoozed_until of %s = %v, want it untouched", id, at)
		}
	}
	// The row the Inbox shows comes back as new; its sibling keeps its state.
	if isRead(newest) {
		t.Errorf("the snoozed row stayed read; it should come back unread")
	}
	if !isRead(older) {
		t.Errorf("an older sibling became unread")
	}
	select {
	case got := <-published:
		if got["type"] != protocol.EventInboxSnoozed || got["recipient_id"] != testUserID {
			t.Errorf("published %+v, want inbox:snoozed for the caller", got)
		}
	default:
		t.Errorf("snoozing published no inbox:snoozed event")
	}

	if f.summary().NeedsYou != 1 {
		t.Errorf("needs_you = %d, want 1: only the other issue", f.summary().NeedsYou)
	}

	if got := f.unsnooze(newest); got != 2 {
		t.Fatalf("unsnooze count = %d, want the issue's two rows", got)
	}
	for _, id := range []string{older, newest} {
		if at := snoozedUntil(id); at != nil {
			t.Errorf("snoozed_until of %s = %v after unsnooze, want none", id, at)
		}
	}
	select {
	case got := <-published:
		if got["type"] != protocol.EventInboxUnsnoozed {
			t.Errorf("published %+v, want inbox:unsnoozed", got)
		}
	default:
		t.Errorf("unsnoozing published no inbox:unsnoozed event")
	}
	if got := f.unsnooze(older); got != 0 {
		t.Errorf("unsnoozing an issue that is not snoozed changed %d rows", got)
	}
	if f.summary().NeedsYou != 2 {
		t.Errorf("needs_you = %d after unsnooze, want 2", f.summary().NeedsYou)
	}
}

func TestSnoozeInboxItemRejectsATimeItCannotUse(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox snooze validation")
	id := f.row(f.issue("Issue"), "mentioned", testutil.Cols{"section": "needs_you"})
	archived := f.row(f.issue("Archived issue"), "mentioned", testutil.Cols{"section": "needs_you", "archived": true})

	for name, body := range map[string]any{
		"missing":        map[string]string{},
		"not a time":     map[string]string{"until": "tomorrow"},
		"in the past":    map[string]string{"until": time.Now().Add(-time.Minute).Format(time.RFC3339)},
		"years from now": map[string]string{"until": time.Now().AddDate(3, 0, 0).Format(time.RFC3339)},
	} {
		f.snooze(id, body).Want(http.StatusBadRequest)
		if got := dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE id = $1 AND snoozed_until IS NOT NULL`, id); got != 0 {
			t.Errorf("%s: the row was snoozed", name)
		}
	}
	f.snooze(archived, map[string]string{"until": time.Now().Add(time.Hour).Format(time.RFC3339)}).
		Want(http.StatusConflict)
	f.snooze(uuid.NewString(), map[string]string{"until": time.Now().Add(time.Hour).Format(time.RFC3339)}).
		Want(http.StatusNotFound)
}

// Restoring an archived issue is asking to see it now, so a snooze set before
// the archive does not hide it again.
func TestUnarchiveInboxItemDropsASnooze(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox unarchive snooze")
	issueID := f.issue("Archived while snoozed")
	future := time.Now().Add(6 * time.Hour)
	first := f.row(issueID, "mentioned", testutil.Cols{"section": "needs_you", "archived": true, "snoozed_until": future})
	second := f.row(issueID, "new_comment", testutil.Cols{"section": "needs_you", "archived": true, "snoozed_until": future})

	testutil.Call(t, inboxWorkspaceHandler(testHandler.UnarchiveInboxItem),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+first+"/unarchive", f.workspaceID), "id", first)).
		Want(http.StatusOK)

	if got := dbfx.Count(t,
		`SELECT count(*) FROM inbox_item WHERE id = ANY($1::uuid[]) AND archived = false AND snoozed_until IS NULL`,
		[]string{first, second}); got != 2 {
		t.Errorf("restored rows without a snooze = %d, want 2", got)
	}
	if f.summary().NeedsYou != 1 {
		t.Errorf("needs_you = %d, want the restored issue", f.summary().NeedsYou)
	}
}
