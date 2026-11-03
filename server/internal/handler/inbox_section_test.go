package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type sectionFixture struct {
	t           *testing.T
	workspaceID string
}

func newSectionFixture(t *testing.T, name string) sectionFixture {
	t.Helper()
	workspaceID := dbfx.Workspace(t, name, "inbox-section-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
	return sectionFixture{t: t, workspaceID: workspaceID}
}

func (f sectionFixture) issue(title string) string {
	f.t.Helper()
	return dbfx.Issue(f.t, title, testutil.Cols{"workspace_id": f.workspaceID})
}

// item inserts one active notification for the test user. actorType "" leaves
// actor_type NULL, as system-authored rows have it.
func (f sectionFixture) item(issueID, notifType, actorType, title string, read bool, createdAt testutil.Raw) string {
	f.t.Helper()
	cols := testutil.Cols{
		"workspace_id":   f.workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           notifType,
		"severity":       "info",
		"issue_id":       issueID,
		"title":          title,
		"read":           read,
		"created_at":     createdAt,
	}
	if actorType != "" {
		cols["actor_type"] = actorType
	}
	return dbfx.Insert(f.t, "inbox_item", cols)
}

// routed inserts an unread notification whose section was chosen by the
// recipient's routing preferences when it was created.
func (f sectionFixture) routed(issueID, notifType, actorType, title, section string, createdAt testutil.Raw) string {
	f.t.Helper()
	id := f.item(issueID, notifType, actorType, title, false, createdAt)
	dbfx.Exec(f.t, `UPDATE inbox_item SET section = $2 WHERE id = $1`, id, section)
	return id
}

func (f sectionFixture) unreadSummary() int64 {
	f.t.Helper()
	var summary []InboxWorkspaceUnreadResponse
	testutil.Call(f.t, testHandler.UnreadInboxSummary,
		inboxRequest(http.MethodGet, "/api/inbox/unread-summary", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&summary)
	for _, row := range summary {
		if row.WorkspaceID == f.workspaceID {
			return row.Count
		}
	}
	return 0
}

func (f sectionFixture) archived(title string) bool {
	f.t.Helper()
	return dbfx.Count(f.t,
		"SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND title = $2 AND archived = true",
		f.workspaceID, title) == 1
}

func (f sectionFixture) read(title string) bool {
	f.t.Helper()
	return dbfx.Count(f.t,
		"SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND title = $2 AND read = true",
		f.workspaceID, title) == 1
}

func TestListInboxReturnsSection(t *testing.T) {
	f := newSectionFixture(t, "Inbox section field")
	issueID := f.issue("Section field")
	f.item(issueID, "mentioned", "member", "mention", false, "now() - interval '4 minutes'")
	f.item(issueID, "status_changed", "agent", "agent status", false, "now() - interval '3 minutes'")
	f.item(issueID, "new_comment", "agent", "agent comment", false, "now() - interval '2 minutes'")
	f.item(issueID, "new_comment", "member", "member comment", false, "now() - interval '1 minute'")

	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&items)

	want := map[string]string{
		"mention":        "action",
		"agent status":   "activity",
		"agent comment":  "activity",
		"member comment": "action",
	}
	if len(items) != len(want) {
		t.Fatalf("items = %d, want %d: %+v", len(items), len(want), items)
	}
	for _, item := range items {
		if item.Section != want[item.Title] {
			t.Errorf("%s: section = %q, want %q", item.Title, item.Section, want[item.Title])
		}
	}
}

func TestUnreadCountsIgnoreActivity(t *testing.T) {
	f := newSectionFixture(t, "Inbox unread sections")
	// Only routine activity, all stored unread: nothing to count.
	quietIssue := f.issue("Agent finished")
	f.item(quietIssue, "status_changed", "agent", "quiet status", false, "now() - interval '3 minutes'")
	f.item(quietIssue, "new_comment", "agent", "quiet comment", false, "now() - interval '2 minutes'")
	// An unread mention followed by newer activity: the activity must neither
	// count nor decide the group's state.
	mentionIssue := f.issue("Mentioned")
	f.item(mentionIssue, "mentioned", "member", "mention", false, "now() - interval '3 minutes'")
	f.item(mentionIssue, "status_changed", "agent", "later status", true, "now() - interval '1 minute'")
	// A person commenting without a mention still wants the recipient.
	commentIssue := f.issue("Member comment")
	f.item(commentIssue, "new_comment", "member", "member comment", false, "now() - interval '1 minute'")
	// Read action rows never count.
	readIssue := f.issue("Already handled")
	f.item(readIssue, "issue_assigned", "member", "handled", true, "now() - interval '1 minute'")

	var summary []InboxWorkspaceUnreadResponse
	testutil.Call(t, testHandler.UnreadInboxSummary,
		inboxRequest(http.MethodGet, "/api/inbox/unread-summary", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&summary)
	var grouped int64
	for _, row := range summary {
		if row.WorkspaceID == f.workspaceID {
			grouped = row.Count
		}
	}
	if grouped != 2 {
		t.Fatalf("unread summary = %d, want 2 (mention + member comment): %+v", grouped, summary)
	}

	var raw struct {
		Count int64 `json:"count"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.CountUnreadInbox),
		inboxRequest(http.MethodGet, "/api/inbox/unread-count", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&raw)
	if raw.Count != 2 {
		t.Fatalf("unread count = %d, want 2", raw.Count)
	}
}

func TestArchiveAllReadInboxGroupsBySection(t *testing.T) {
	f := newSectionFixture(t, "Archive read by section")
	issueID := f.issue("Mixed sections")
	f.item(issueID, "mentioned", "member", "unread mention", false, "now() - interval '3 minutes'")
	f.item(issueID, "status_changed", "agent", "newer activity", false, "now() - interval '1 minute'")
	readIssue := f.issue("Read action")
	f.item(readIssue, "issue_assigned", "member", "read assignment", true, "now() - interval '1 minute'")

	testutil.Call(t, inboxWorkspaceHandler(testHandler.ArchiveAllReadInbox),
		inboxRequest(http.MethodPost, "/api/inbox/archive-all-read", f.workspaceID)).
		Want(http.StatusOK)

	if !f.archived("newer activity") {
		t.Error("activity counts as read and should be archived")
	}
	if f.archived("unread mention") {
		t.Error("an unread action row was archived alongside the issue's activity")
	}
	if !f.archived("read assignment") {
		t.Error("a read action group should be archived")
	}
}

func TestArchiveInboxItemScopesActivityToSection(t *testing.T) {
	f := newSectionFixture(t, "Archive item by section")
	issueID := f.issue("Archive scope")
	mentionID := f.item(issueID, "mentioned", "member", "mention", false, "now() - interval '3 minutes'")
	activityID := f.item(issueID, "status_changed", "agent", "status", false, "now() - interval '2 minutes'")
	f.item(issueID, "new_comment", "agent", "agent comment", false, "now() - interval '1 minute'")

	testutil.Call(t, inboxWorkspaceHandler(testHandler.ArchiveInboxItem),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+activityID+"/archive", f.workspaceID), "id", activityID)).
		Want(http.StatusOK)

	if !f.archived("status") || !f.archived("agent comment") {
		t.Fatal("archiving an activity row should clear the issue's activity")
	}
	if f.archived("mention") {
		t.Fatal("archiving an activity row archived the issue's mention")
	}

	testutil.Call(t, inboxWorkspaceHandler(testHandler.ArchiveInboxItem),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+mentionID+"/archive", f.workspaceID), "id", mentionID)).
		Want(http.StatusOK)
	if !f.archived("mention") {
		t.Fatal("archiving an action row should archive it")
	}
}

func TestMarkIssueInboxRead(t *testing.T) {
	f := newSectionFixture(t, "Mark issue inbox read")
	issueID := f.issue("Opened from Work")
	otherIssueID := f.issue("Not opened")
	f.item(issueID, "mentioned", "member", "mention", false, "now() - interval '3 minutes'")
	f.item(issueID, "status_changed", "agent", "status", false, "now() - interval '2 minutes'")
	f.item(otherIssueID, "issue_assigned", "member", "other", false, "now() - interval '1 minute'")

	call := func(id string) *testutil.Response {
		return testutil.Call(t, inboxWorkspaceHandler(testHandler.MarkIssueInboxRead),
			withURLParam(inboxRequest(http.MethodPost, "/api/inbox/issues/"+id+"/read", f.workspaceID), "issueId", id))
	}

	var resp struct {
		Count int64 `json:"count"`
	}
	call(issueID).Want(http.StatusOK).JSON(&resp)
	if resp.Count != 2 {
		t.Fatalf("count = %d, want 2", resp.Count)
	}
	if !f.read("mention") || !f.read("status") {
		t.Fatal("every row on the opened issue should be read")
	}
	if f.read("other") {
		t.Fatal("another issue's notification was marked read")
	}

	call(issueID).Want(http.StatusOK).JSON(&resp)
	if resp.Count != 0 {
		t.Fatalf("second call count = %d, want 0", resp.Count)
	}

	foreignWorkspaceID := dbfx.Workspace(t, "Foreign inbox issue", "inbox-section-foreign-"+uuid.NewString())
	foreignIssueID := dbfx.Issue(t, "Foreign", testutil.Cols{"workspace_id": foreignWorkspaceID})
	call(foreignIssueID).Want(http.StatusNotFound)
}

func TestStoredSectionOverridesDefault(t *testing.T) {
	f := newSectionFixture(t, "Routed sections")
	statusIssue := f.issue("Status routed to action")
	f.routed(statusIssue, "status_changed", "agent", "routed status", "action", "now() - interval '2 minutes'")
	mentionIssue := f.issue("Mention routed to activity")
	f.routed(mentionIssue, "mentioned", "member", "routed mention", "activity", "now() - interval '1 minute'")

	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&items)
	want := map[string]string{"routed status": "action", "routed mention": "activity"}
	for _, item := range items {
		if item.Section != want[item.Title] {
			t.Errorf("%s: section = %q, want %q", item.Title, item.Section, want[item.Title])
		}
	}

	if got := f.unreadSummary(); got != 1 {
		t.Fatalf("unread summary = %d, want 1 (the status routed to action)", got)
	}

	testutil.Call(t, inboxWorkspaceHandler(testHandler.ArchiveAllReadInbox),
		inboxRequest(http.MethodPost, "/api/inbox/archive-all-read", f.workspaceID)).
		Want(http.StatusOK)
	if !f.archived("routed mention") {
		t.Error("a mention routed to activity counts as read and should be archived")
	}
	if f.archived("routed status") {
		t.Error("an unread status routed to action was archived")
	}
}

// Clients from before the Inbox sections group rows once per issue across
// sections and mark only the newest row read through the item endpoint. That
// must still settle the issue, or an older unread mention keeps the badge lit
// with nothing left in their list to open.
func TestMarkInboxReadSettlesIssueForLegacyClients(t *testing.T) {
	f := newSectionFixture(t, "Legacy inbox read")
	issueID := f.issue("Mention then status")
	otherIssueID := f.issue("Untouched")
	f.item(issueID, "mentioned", "member", "older unread mention", false, "now() - interval '3 minutes'")
	activityID := f.item(issueID, "status_changed", "agent", "newer status", false, "now() - interval '2 minutes'")
	f.item(otherIssueID, "issue_assigned", "member", "other issue", false, "now() - interval '1 minute'")

	testutil.Call(t, inboxWorkspaceHandler(testHandler.MarkInboxRead),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+activityID+"/read", f.workspaceID), "id", activityID)).
		Want(http.StatusOK)

	if !f.read("older unread mention") || !f.read("newer status") {
		t.Fatal("reading one row should settle every row of its issue")
	}
	if f.read("other issue") {
		t.Fatal("reading one row marked another issue's notification read")
	}
	if got := f.unreadSummary(); got != 1 {
		t.Fatalf("unread summary = %d, want 1 (the other issue)", got)
	}
}

func TestArchivedInboxTreatsActivityAsRead(t *testing.T) {
	f := newSectionFixture(t, "Archived activity is read")
	activityIssue := f.issue("Archived activity")
	actionIssue := f.issue("Archived mention")
	for _, cols := range []testutil.Cols{
		{"issue_id": activityIssue, "type": "status_changed", "actor_type": "agent", "title": "activity"},
		{"issue_id": actionIssue, "type": "mentioned", "actor_type": "member", "title": "mention"},
	} {
		cols["workspace_id"] = f.workspaceID
		cols["recipient_type"] = "member"
		cols["recipient_id"] = testUserID
		cols["severity"] = "info"
		cols["read"] = false
		cols["archived"] = true
		dbfx.Insert(t, "inbox_item", cols)
	}

	var facets archivedInboxFacetsResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.GetArchivedInboxFacets),
		inboxRequest(http.MethodGet, "/api/inbox/archived/facets", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&facets)
	if facets.UnreadCount != 1 {
		t.Fatalf("archived unread count = %d, want 1", facets.UnreadCount)
	}

	var page archivedInboxPageResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage),
		inboxRequest(http.MethodGet, "/api/inbox/archived/page?unread_only=true", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&page)
	if len(page.Items) != 1 || page.Items[0].Title != "mention" {
		t.Fatalf("unread archived page = %+v, want only the mention", page.Items)
	}
}
