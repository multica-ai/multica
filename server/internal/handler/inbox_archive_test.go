package handler

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestArchivedInboxPageBeyond200(t *testing.T) {
	ws := dbfx.Workspace(t, "Paged archive", "archive-page-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	// Equal timestamps exercise the ID tie-breaker at every page boundary.
	created := time.Now().UTC().Truncate(time.Microsecond)
	for range 205 {
		issue := dbfx.Issue(t, "Archived", testutil.Cols{"workspace_id": ws})
		dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id": ws, "recipient_type": "member", "recipient_id": testUserID,
			"type": "mentioned", "severity": "info", "title": "Archived", "issue_id": issue,
			"archived": true, "created_at": created,
		})
	}
	seen := map[string]bool{}
	cursor := ""
	var firstCursor string
	for pageNumber := 0; ; pageNumber++ {
		if pageNumber > 5 {
			t.Fatal("pagination did not terminate")
		}
		var page archivedInboxPageResponse
		path := "/api/inbox/archived/page?limit=50&cursor=" + url.QueryEscape(cursor)
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, path, ws)).Want(http.StatusOK).JSON(&page)
		for _, item := range page.Items {
			if item.IssueID == nil || seen[*item.IssueID] {
				t.Fatalf("missing or duplicate group: %+v", item)
			}
			seen[*item.IssueID] = true
		}
		if !page.HasMore {
			if page.NextCursor != nil {
				t.Fatal("last page has a cursor")
			}
			break
		}
		if len(page.Items) != 50 || page.NextCursor == nil {
			t.Fatalf("invalid page: %+v", page)
		}
		cursor = *page.NextCursor
		if firstCursor == "" {
			firstCursor = cursor
		}
	}
	if len(seen) != 205 {
		t.Fatalf("got %d groups, want 205", len(seen))
	}
	// Old installed clients retain their capped array response.
	var legacy []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInbox), inboxRequest(http.MethodGet, "/api/inbox/archived", ws)).Want(http.StatusOK).JSON(&legacy)
	if len(legacy) != 200 {
		t.Fatalf("legacy archive = %d, want 200", len(legacy))
	}
	for _, query := range []string{"limit=0", "limit=101", "cursor=broken", "group_id=bad", "unread_only=bad", "statuses=a,,b", "unread_only=true&cursor=" + firstCursor} {
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, "/api/inbox/archived/page?"+query, ws)).Want(http.StatusBadRequest)
	}
}

func TestArchivedInboxMatchedNotificationKeepsItsCommentAnchor(t *testing.T) {
	ws := dbfx.Workspace(t, "Archive anchors", "archive-anchor-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	issue := dbfx.Issue(t, "Anchored", testutil.Cols{"workspace_id": ws})
	c1, c2 := uuid.NewString(), uuid.NewString()
	old := time.Now().UTC().Add(-time.Hour)
	insert := func(kind string, created time.Time, comment string) string {
		return dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id": ws, "recipient_type": "member", "recipient_id": testUserID,
			"type": kind, "severity": "info", "title": "Anchored", "issue_id": issue,
			"archived": true, "created_at": created, "details": `{"comment_id":"` + comment + `"}`,
		})
	}
	mention := insert("mentioned", old, c1)
	newest := insert("new_comment", old.Add(time.Minute), c2)
	get := func(query string) archivedInboxPageResponse {
		var page archivedInboxPageResponse
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, "/api/inbox/archived/page?"+query, ws)).Want(http.StatusOK).JSON(&page)
		return page
	}
	for _, query := range []string{"types=mention", "group_id=" + issue + "&types=mention"} {
		page := get(query)
		if len(page.Items) != 1 || page.Items[0].ID != mention || string(page.Items[0].Details) != `{"comment_id":"`+c1+`"}` {
			t.Fatalf("%s: mention lost its own comment: %+v", query, page.Items)
		}
	}
	if page := get(""); len(page.Items) != 1 || page.Items[0].ID != newest || string(page.Items[0].Details) != `{"comment_id":"`+c2+`"}` {
		t.Fatalf("unfiltered group did not show the newest comment: %+v", page.Items)
	}
}

func TestArchivedInboxFiltersAndLookupUseLatestGroup(t *testing.T) {
	ws := dbfx.Workspace(t, "Archive filters", "archive-filter-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	issue := dbfx.Issue(t, "Older matching group", testutil.Cols{"workspace_id": ws, "status": "in_review", "priority": "high"})
	actor := uuid.NewString()
	comment := uuid.NewString()
	old := time.Now().UTC().Add(-time.Hour)
	insert := func(issueID *string, read bool, actorType string, actorID *string, created time.Time, details string, archived bool) string {
		return dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id": ws, "recipient_type": "member", "recipient_id": testUserID,
			"type": "mentioned", "severity": "info", "title": "Archive filter", "issue_id": issueID,
			"read": read, "actor_type": actorType, "actor_id": actorID, "archived": archived,
			"created_at": created, "details": details,
		})
	}
	insert(&issue, false, "agent", &actor, old.Add(-time.Minute), `{"comment_id":"`+comment+`"}`, true)
	newest := insert(&issue, true, "system", nil, old, `{}`, true)
	// Fill the newest page with nonmatching groups, so a local filter would miss the match.
	for range 55 {
		insert(nil, false, "agent", &actor, old.Add(time.Minute), `{}`, true)
	}
	get := func(query string) archivedInboxPageResponse {
		var page archivedInboxPageResponse
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, "/api/inbox/archived/page?"+query, ws)).Want(http.StatusOK).JSON(&page)
		return page
	}
	page := get("statuses=in_review&priorities=high&actors=system")
	if len(page.Items) != 1 || page.Items[0].ID != newest {
		t.Fatalf("filter lost older match: %+v", page)
	}
	if string(page.Items[0].Details) != `{"comment_id":"`+comment+`"}` {
		t.Fatalf("comment anchor lost: %s", page.Items[0].Details)
	}
	if len(get("group_id="+issue+"&unread_only=true").Items) != 0 {
		t.Fatal("older unread notification matched")
	}
	// A sender filter matches any notification in the group and renders the
	// newest one it matched.
	if byActor := get("group_id=" + issue + "&actors=agent:" + actor); len(byActor.Items) != 1 || byActor.Items[0].ID == newest {
		t.Fatalf("older actor did not select its own notification: %+v", byActor)
	}
	if len(get("group_id="+issue+"&actors=agent:"+actor+"&unread_only=true").Items) != 1 {
		t.Fatal("unread did not test the notification the sender filter selected")
	}
	if len(get("group_id="+issue).Items) != 1 {
		t.Fatal("deep link outside first page did not resolve")
	}
	var facets archivedInboxFacetsResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.GetArchivedInboxFacets), inboxRequest(http.MethodGet, "/api/inbox/archived/facets?actors=system", ws)).Want(http.StatusOK).JSON(&facets)
	if facets.Statuses["in_review"] != 1 || facets.Priorities["high"] != 1 || facets.Actors["agent:"+actor] != 56 || facets.Actors["system"] != 1 || facets.UnreadCount != 0 {
		t.Fatalf("incorrect facets: %+v", facets)
	}
	insert(&issue, false, "system", nil, time.Now(), `{}`, false)
	if len(get("group_id="+issue).Items) != 0 {
		t.Fatal("active issue also appeared in archive")
	}
	// Another recipient and another workspace cannot read this archive, including lookup.
	otherWS := dbfx.Workspace(t, "Other archive", "other-archive-"+uuid.NewString())
	dbfx.Member(t, otherWS, testUserID, "owner")
	var other archivedInboxPageResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, "/api/inbox/archived/page?group_id="+issue, otherWS)).Want(http.StatusOK).JSON(&other)
	if len(other.Items) != 0 {
		t.Fatal("cross-workspace archive leak")
	}
	otherUser := dbfx.User(t, "Other recipient", uuid.NewString()+"@example.test")
	dbfx.Member(t, ws, otherUser, "member")
	req := inboxRequest(http.MethodGet, "/api/inbox/archived/page?group_id="+issue, ws)
	req.Header.Set("X-User-ID", otherUser)
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), req).Want(http.StatusOK).JSON(&other)
	if len(other.Items) != 0 {
		t.Fatal("cross-recipient archive leak")
	}
	insert(&issue, false, "system", nil, time.Now(), `{}`, false)
	if len(get("group_id="+issue).Items) != 0 {
		t.Fatal("active issue also appeared in archive")
	}
	for _, handler := range []http.HandlerFunc{testHandler.ListArchivedInboxPage, testHandler.GetArchivedInboxFacets} {
		req := inboxRequest(http.MethodGet, "/api/inbox/archived/page", ws)
		req.Header.Set("X-User-ID", uuid.NewString())
		testutil.Call(t, inboxWorkspaceHandler(handler), req).Want(http.StatusNotFound)
	}
}

func TestArchivedInboxNotificationAndIssueFilters(t *testing.T) {
	ws := dbfx.Workspace(t, "Archive extended filters", "archive-extended-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	today := time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC) // a Wednesday
	project := dbfx.Project(t, "Filtered project", testutil.Cols{"workspace_id": ws})
	label := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": ws, "name": "bug", "color": "#f00", "resource_type": "issue"})
	tracked := dbfx.Issue(t, "Tracked", testutil.Cols{
		"workspace_id": ws, "project_id": project, "assignee_type": "member", "assignee_id": testUserID,
		"due_date": today.Format(time.DateOnly),
	})
	dbfx.Exec(t, `INSERT INTO issue_to_label (issue_id, label_id) VALUES ($1, $2)`, tracked, label)
	bare := dbfx.Issue(t, "Bare", testutil.Cols{"workspace_id": ws})
	base := time.Now().UTC().Add(-time.Hour)
	insert := func(issueID *string, notifType string, created time.Time) string {
		return dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id": ws, "recipient_type": "member", "recipient_id": testUserID,
			"type": notifType, "severity": "info", "title": notifType, "issue_id": issueID,
			"read": true, "actor_type": "system", "archived": true, "created_at": created,
		})
	}
	mention := insert(&tracked, "mentioned", base)
	change := insert(&tracked, "status_changed", base.Add(time.Minute))
	comment := insert(&bare, "new_comment", base)
	issueless := insert(nil, "quick_create_failed", base)

	ids := func(query string) []string {
		var page archivedInboxPageResponse
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, "/api/inbox/archived/page?"+query, ws)).Want(http.StatusOK).JSON(&page)
		out := make([]string, len(page.Items))
		for i, item := range page.Items {
			out[i] = item.ID
		}
		slices.Sort(out)
		return out
	}
	sorted := func(values ...string) []string {
		slices.Sort(values)
		return values
	}
	day := "today=" + today.Format(time.DateOnly)
	for _, tc := range []struct {
		query string
		want  []string
	}{
		// The type filter picks the matching notification, not the newest one.
		{"types=mention", []string{mention}},
		{"types=comment,mention", sorted(mention, comment)},
		{"types=change&negated=type", sorted(mention, comment, issueless)},
		{"projects=" + project, []string{change}},
		{"projects=none", []string{comment}},
		// Issue filters, negated ones too, never match an issue-less notification.
		{"projects=" + project + "&negated=project", []string{comment}},
		{"assignees=member:" + testUserID, []string{change}},
		{"assignees=none", []string{comment}},
		{"labels=" + label, []string{change}},
		{"labels=" + label + "&negated=label", []string{comment}},
		{"due_dates=today&" + day, []string{change}},
		{"due_dates=this_week,overdue&" + day, []string{change}},
		{"due_dates=none&" + day, []string{comment}},
		{"due_dates=today&negated=dueDate&" + day, []string{comment}},
		{"received_after=" + url.QueryEscape(time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)), []string{}},
		{"types=mention&received_after=" + url.QueryEscape(base.Add(30*time.Second).Format(time.RFC3339Nano)), []string{}},
	} {
		if got := ids(tc.query); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.query, got, tc.want)
		}
	}

	facets := func(query string) archivedInboxFacetsResponse {
		var out archivedInboxFacetsResponse
		testutil.Call(t, inboxWorkspaceHandler(testHandler.GetArchivedInboxFacets), inboxRequest(http.MethodGet, "/api/inbox/archived/facets?"+query, ws)).Want(http.StatusOK).JSON(&out)
		return out
	}
	// Each count ignores its own dimension, and an issue filter drops the
	// issue-less notification from every other count.
	if got := facets("types=mention&projects=none"); got.Types["mention"] != 0 || got.Types["comment"] != 1 || got.Types["system"] != 0 ||
		got.Projects[project] != 1 || got.Projects["none"] != 0 {
		t.Fatalf("facets with type and project filters: %+v", got)
	}
	if got := facets("types=mention"); got.Labels[label] != 1 || got.Assignees["member:"+testUserID] != 1 || got.Projects["none"] != 0 {
		t.Fatalf("facets with a type filter: %+v", got)
	}
	if got := facets(""); got.Types["mention"] != 1 || got.Types["change"] != 1 || got.Types["system"] != 1 || got.Projects["none"] != 1 || got.Assignees["none"] != 1 {
		t.Fatalf("unfiltered facets: %+v", got)
	}

	for _, query := range []string{"types=bogus", "negated=creator", "due_dates=later&" + day, "due_dates=today", "today=03/11/2026", "received_after=yesterday"} {
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, "/api/inbox/archived/page?"+query, ws)).Want(http.StatusBadRequest)
	}
}
