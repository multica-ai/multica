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

// Nothing is written when a snooze runs out, so the summary says when the
// next one does and the badge re-reads it then.
func TestInboxSummaryReportsTheNextSnoozeToRunOut(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox next wake")
	if got := f.summary().NextWakeAt; got != nil {
		t.Fatalf("next_wake_at = %q with nothing snoozed, want none", *got)
	}

	soon := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	later := soon.Add(24 * time.Hour)
	f.row(f.issue("Back later"), "mentioned", testutil.Cols{"section": "needs_you", "snoozed_until": later})
	// Only followed, but its row is still out of the counts until then.
	f.row(f.issue("Back soon"), "new_comment", testutil.Cols{"section": "updates", "snoozed_until": soon})
	// Not coming back: already back, or archived.
	f.row(f.issue("Already back"), "mentioned", testutil.Cols{"section": "needs_you", "snoozed_until": time.Now().Add(-time.Hour)})
	f.row(f.issue("Archived"), "mentioned", testutil.Cols{"section": "needs_you", "archived": true, "snoozed_until": soon.Add(-time.Minute)})

	got := f.summary()
	if got.NextWakeAt == nil {
		t.Fatalf("next_wake_at missing, want %s", soon)
	}
	at, err := time.Parse(time.RFC3339, *got.NextWakeAt)
	if err != nil || !at.Equal(soon) {
		t.Errorf("next_wake_at = %q, want %s", *got.NextWakeAt, soon.Format(time.RFC3339))
	}
	if got.NeedsYou != 1 {
		t.Errorf("needs_you = %d, want only the issue already back", got.NeedsYou)
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

func (f inboxSectionsFixture) archiveUpdates(body any) *testutil.Response {
	f.t.Helper()
	req := testutil.WithHeaders(
		testutil.JSONRequest(http.MethodPost, "/api/inbox/archive-updates", body),
		"X-User-ID", testUserID,
		"X-Workspace-ID", f.workspaceID,
	)
	return testutil.Call(f.t, inboxWorkspaceHandler(testHandler.ArchiveInboxUpdates), req)
}

func (f inboxSectionsFixture) archivedRows(ids ...string) int {
	f.t.Helper()
	return dbfx.Count(f.t, `SELECT count(*) FROM inbox_item WHERE id = ANY($1::uuid[]) AND archived = true`, ids)
}

// The Inbox drops an update once it has been read (MUL-9215). Only what is
// still a read update goes: an issue that needs the member, one they snoozed,
// and one that became unread again all stay.
func TestArchiveInboxUpdatesArchivesReadUpdates(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox archive read updates")
	older := time.Now().Add(-time.Hour)

	read := f.issue("Read update")
	readRows := []string{
		f.row(read, "new_comment", testutil.Cols{"section": "updates", "read": true, "created_at": older}),
		f.row(read, "status_changed", testutil.Cols{"section": "updates", "read": true}),
	}
	// The Inbox shows an issue as read by its newest row, so an older unread
	// sibling does not keep it.
	readByNewest := f.issue("Read by its newest row")
	readByNewestRows := []string{
		f.row(readByNewest, "new_comment", testutil.Cols{"section": "updates", "created_at": older}),
		f.row(readByNewest, "priority_changed", testutil.Cols{"section": "updates", "read": true}),
	}
	// A row from before sections were stored is an update once it is read.
	legacyRead := f.row(f.issue("Legacy read"), "mentioned", testutil.Cols{"read": true})
	issueless := f.row(nil, "quick_create_done", testutil.Cols{"section": "updates", "read": true})

	unread := f.issue("Unread again")
	unreadRows := []string{
		f.row(unread, "new_comment", testutil.Cols{"section": "updates", "read": true, "created_at": older}),
		f.row(unread, "new_comment", testutil.Cols{"section": "updates"}),
	}
	waiting := f.issue("Waiting on the member")
	waitingRows := []string{
		f.row(waiting, "mentioned", testutil.Cols{"section": "needs_you", "read": true, "created_at": older}),
		f.row(waiting, "status_changed", testutil.Cols{"section": "updates", "read": true}),
	}
	legacyUnread := f.row(f.issue("Legacy unread mention"), "mentioned", nil)
	snoozedIssue := f.issue("Snoozed update")
	snoozed := f.row(snoozedIssue, "new_comment", testutil.Cols{
		"section": "updates", "read": true, "snoozed_until": time.Now().Add(time.Hour),
	})
	notListed := f.row(f.issue("Not listed"), "new_comment", testutil.Cols{"section": "updates", "read": true})
	// Someone else's row on a listed issue is not the caller's to archive.
	otherID := dbfx.User(t, "Other reader", "inbox-archive-updates-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, f.workspaceID, otherID, "member")
	othersRow := f.row(read, "new_comment", testutil.Cols{"section": "updates", "read": true, "recipient_id": otherID})

	var published []map[string]any
	testHandler.Bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) {
		if e.WorkspaceID != f.workspaceID {
			return
		}
		if payload, ok := e.Payload.(map[string]any); ok {
			published = append(published, payload)
		}
	})

	var resp struct {
		Count int64 `json:"count"`
	}
	f.archiveUpdates(map[string]any{
		"read_only": true,
		"groups": []string{
			read, readByNewest, inboxRowIssue(t, legacyRead), issueless,
			unread, waiting, inboxRowIssue(t, legacyUnread), snoozedIssue,
		},
	}).Want(http.StatusOK).JSON(&resp)

	if resp.Count != 6 {
		t.Errorf("count = %d, want the six rows of the four read updates", resp.Count)
	}
	archived := append(append([]string{legacyRead, issueless}, readRows...), readByNewestRows...)
	if got := f.archivedRows(archived...); got != len(archived) {
		t.Errorf("archived read updates = %d, want %d", got, len(archived))
	}
	kept := append(append([]string{legacyUnread, snoozed, notListed, othersRow}, unreadRows...), waitingRows...)
	if got := f.archivedRows(kept...); got != 0 {
		t.Errorf("archived %d rows that are not the caller's read updates, want 0", got)
	}
	if len(published) != 1 || published[0]["recipient_id"] != testUserID || published[0]["count"] != int64(6) {
		t.Errorf("published %+v, want one inbox:batch-archived for the caller's six rows", published)
	}

	// Nothing left to archive changes no row and announces nothing.
	f.archiveUpdates(map[string]any{"read_only": true, "groups": []string{read, unread, waiting}}).
		Want(http.StatusOK).JSON(&resp)
	if resp.Count != 0 || len(published) != 1 {
		t.Errorf("second call: count = %d with %d events, want 0 and no new event", resp.Count, len(published))
	}
}

// inboxRowIssue is the issue a seeded row belongs to: the key the Inbox
// names its group by.
func inboxRowIssue(t *testing.T, itemID string) string {
	t.Helper()
	var issueID string
	dbfx.QueryRow(t, `SELECT issue_id FROM inbox_item WHERE id = $1`, itemID).Scan(&issueID)
	return issueID
}

// Clearing "Updates" archives the unread ones too, and still nothing that
// needs the member.
func TestArchiveInboxUpdatesClearsUnreadUpdates(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox clear updates")
	unreadIssue := f.issue("Unread update")
	unread := f.row(unreadIssue, "new_comment", testutil.Cols{"section": "updates"})
	waitingIssue := f.issue("Waiting on the member")
	waiting := f.row(waitingIssue, "new_comment", testutil.Cols{"section": "needs_you"})
	groupMentionIssue := f.issue("Group mention filed as an update")
	groupMention := f.row(groupMentionIssue, "mentioned", testutil.Cols{"section": "updates"})

	var resp struct {
		Count int64 `json:"count"`
	}
	f.archiveUpdates(map[string]any{"groups": []string{unreadIssue, waitingIssue, groupMentionIssue}}).
		Want(http.StatusOK).JSON(&resp)
	if resp.Count != 2 {
		t.Errorf("count = %d, want the two updates", resp.Count)
	}
	if got := f.archivedRows(unread, groupMention); got != 2 {
		t.Errorf("archived updates = %d, want 2", got)
	}
	if got := f.archivedRows(waiting); got != 0 {
		t.Errorf("the issue that needs the member was archived")
	}
	if f.summary().NeedsYou != 1 {
		t.Errorf("needs_you = %d, want the waiting issue still counted", f.summary().NeedsYou)
	}
}

func TestArchiveInboxUpdatesRejectsBadInput(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox archive updates input")

	var resp struct {
		Count int64 `json:"count"`
	}
	f.archiveUpdates(map[string]any{"groups": []string{}}).Want(http.StatusOK).JSON(&resp)
	if resp.Count != 0 {
		t.Errorf("count = %d for no groups, want 0", resp.Count)
	}
	f.archiveUpdates(map[string]any{"groups": []string{"not-a-uuid"}}).Want(http.StatusBadRequest)
	tooMany := make([]string, inboxArchiveUpdatesMaxGroups+1)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}
	f.archiveUpdates(map[string]any{"groups": tooMany}).Want(http.StatusBadRequest)
}

// A restored update comes back unread, or the Inbox would drop it again as a
// read update. An issue that needs the member keeps the state it had.
func TestUnarchiveInboxItemRestoresAnUpdateAsUnread(t *testing.T) {
	f := newInboxSectionsFixture(t, "Inbox unarchive update")
	older := time.Now().Add(-time.Hour)
	unarchive := func(id string) InboxItemResponse {
		t.Helper()
		var item InboxItemResponse
		testutil.Call(t, inboxWorkspaceHandler(testHandler.UnarchiveInboxItem),
			withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+id+"/unarchive", f.workspaceID), "id", id)).
			Want(http.StatusOK).
			JSON(&item)
		return item
	}
	isRead := func(id string) bool {
		t.Helper()
		var read bool
		dbfx.QueryRow(t, `SELECT read FROM inbox_item WHERE id = $1`, id).Scan(&read)
		return read
	}

	update := f.issue("Archived update")
	updateOlder := f.row(update, "new_comment", testutil.Cols{"section": "updates", "read": true, "archived": true, "created_at": older})
	updateNewest := f.row(update, "status_changed", testutil.Cols{"section": "updates", "read": true, "archived": true})
	// Restored from the older row: the one the Inbox renders is still the newest.
	if got := unarchive(updateOlder); got.Archived {
		t.Fatalf("the restored row is still archived: %+v", got)
	}
	if isRead(updateNewest) {
		t.Errorf("the restored update's newest row stayed read")
	}
	if !isRead(updateOlder) {
		t.Errorf("an older row of the restored update became unread")
	}
	var resp struct {
		Count int64 `json:"count"`
	}
	f.archiveUpdates(map[string]any{"read_only": true, "groups": []string{update}}).Want(http.StatusOK).JSON(&resp)
	if resp.Count != 0 {
		t.Errorf("the restored update was archived again as read")
	}

	waiting := f.issue("Archived while waiting")
	waitingRow := f.row(waiting, "mentioned", testutil.Cols{"section": "needs_you", "read": true, "archived": true})
	if got := unarchive(waitingRow); !got.Read {
		t.Errorf("an issue that needs the member came back unread: %+v", got)
	}
}
