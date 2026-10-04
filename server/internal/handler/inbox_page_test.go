package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestInboxPagePagesActiveGroups(t *testing.T) {
	ws := dbfx.Workspace(t, "Paged inbox", "inbox-page-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	for range 205 {
		dbfx.Issue(t, "Active", testutil.Cols{"workspace_id": ws})
	}
	// Each group holds an older and a newer active row plus an even newer
	// archived one, which must never represent the group. Every newest active
	// row shares one timestamp, so the ID tie-breaker decides every page boundary.
	created := time.Now().UTC().Truncate(time.Microsecond)
	dbfx.Exec(t, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, archived, created_at)
		SELECT $1, 'member', $2, 'mentioned', 'info', iss.id, row.title, row.archived, $3::timestamptz + row.offset_ms * interval '1 millisecond'
		FROM issue iss
		CROSS JOIN (VALUES ('older', false, -60000), ('newest active', false, 0), ('archived', true, 60000)) AS row(title, archived, offset_ms)
		WHERE iss.workspace_id = $1
	`, ws, testUserID, created)
	// Two archived-only notifications give the archive a second page.
	dbfx.Exec(t, `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, title, archived)
		SELECT $1, 'member', $2, 'mentioned', 'info', 'archived only', true FROM generate_series(1, 2)
	`, ws, testUserID)
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, ws)

	seen := map[string]bool{}
	cursor := ""
	var firstCursor string
	for pageNumber := 0; ; pageNumber++ {
		if pageNumber > 5 {
			t.Fatal("pagination did not terminate")
		}
		var page inboxPageResponse
		path := "/api/inbox/page?limit=50&cursor=" + url.QueryEscape(cursor)
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInboxPage), inboxRequest(http.MethodGet, path, ws)).Want(http.StatusOK).JSON(&page)
		for _, item := range page.Items {
			if item.IssueID == nil || seen[*item.IssueID] {
				t.Fatalf("missing or duplicate group: %+v", item)
			}
			if item.Archived || item.Title != "newest active" {
				t.Fatalf("group not represented by its newest active row: %+v", item)
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

	// Installed clients keep the unbounded, ungrouped array.
	var legacy []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox), inboxRequest(http.MethodGet, "/api/inbox", ws)).Want(http.StatusOK).JSON(&legacy)
	if len(legacy) != 410 {
		t.Fatalf("legacy inbox = %d rows, want every active row (410)", len(legacy))
	}

	var archived archivedInboxPageResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, "/api/inbox/archived/page?limit=1", ws)).Want(http.StatusOK).JSON(&archived)
	if !archived.HasMore || archived.NextCursor == nil || archived.Items[0].Title != "archived only" {
		t.Fatalf("archive must hold only the archived-only groups: %+v", archived)
	}
	for _, query := range []string{"limit=0", "limit=101", "cursor=broken", "group_id=bad", "unread_only=bad", "statuses=a,,b",
		"unread_only=true&cursor=" + firstCursor, "limit=1&cursor=" + *archived.NextCursor} {
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInboxPage), inboxRequest(http.MethodGet, "/api/inbox/page?"+query, ws)).Want(http.StatusBadRequest)
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, "/api/inbox/archived/page?cursor="+firstCursor, ws)).Want(http.StatusBadRequest)
}

func TestInboxPageFiltersLookupAndCommentAnchor(t *testing.T) {
	ws := dbfx.Workspace(t, "Inbox filters", "inbox-filter-page-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	issue := dbfx.Issue(t, "Older matching group", testutil.Cols{"workspace_id": ws, "status": "in_review", "priority": "high"})
	actor := uuid.NewString()
	comment := uuid.NewString()
	old := time.Now().UTC().Add(-time.Hour)
	insert := func(issueID *string, read bool, actorType string, actorID *string, created time.Time, details string, archived bool) string {
		return dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id": ws, "recipient_type": "member", "recipient_id": testUserID,
			"type": "mentioned", "severity": "info", "title": "Inbox filter", "issue_id": issueID,
			"read": read, "actor_type": actorType, "actor_id": actorID, "archived": archived,
			"created_at": created, "details": details,
		})
	}
	insert(&issue, false, "agent", &actor, old.Add(-time.Minute), `{"comment_id":"`+comment+`"}`, false)
	newest := insert(&issue, true, "system", nil, old, `{}`, false)
	// A newer archived anchor belongs to the archive, not to the active group.
	insert(&issue, false, "agent", &actor, old.Add(30*time.Second), `{"comment_id":"`+uuid.NewString()+`"}`, true)
	// Fill the newest page with nonmatching groups, so a local filter would miss the match.
	for range 55 {
		insert(nil, false, "agent", &actor, old.Add(time.Minute), `{}`, false)
	}
	get := func(query string) inboxPageResponse {
		var page inboxPageResponse
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInboxPage), inboxRequest(http.MethodGet, "/api/inbox/page?"+query, ws)).Want(http.StatusOK).JSON(&page)
		return page
	}
	getArchived := func(query string) archivedInboxPageResponse {
		var page archivedInboxPageResponse
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInboxPage), inboxRequest(http.MethodGet, "/api/inbox/archived/page?"+query, ws)).Want(http.StatusOK).JSON(&page)
		return page
	}
	page := get("statuses=in_review&priorities=high&actors=system")
	if len(page.Items) != 1 || page.Items[0].ID != newest {
		t.Fatalf("filter lost older match: %+v", page)
	}
	if string(page.Items[0].Details) != `{"comment_id":"`+comment+`"}` {
		t.Fatalf("comment anchor not taken from the newest active row carrying one: %s", page.Items[0].Details)
	}
	if page.Items[0].IssueStatus == nil || *page.Items[0].IssueStatus != "in_review" || page.Items[0].IssuePriority == nil || *page.Items[0].IssuePriority != "high" {
		t.Fatalf("issue projection missing: %+v", page.Items[0])
	}
	if len(get("group_id="+issue+"&unread_only=true").Items) != 0 {
		t.Fatal("older unread notification matched")
	}
	if len(get("group_id="+issue+"&actors=agent:"+actor).Items) != 0 {
		t.Fatal("older actor matched")
	}
	if len(get("group_id="+issue).Items) != 1 {
		t.Fatal("deep link outside first page did not resolve")
	}
	if len(getArchived("group_id="+issue).Items) != 0 {
		t.Fatal("issue with an active row also appeared in the archive")
	}

	var facets inboxFacetsResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.GetInboxFacets), inboxRequest(http.MethodGet, "/api/inbox/facets?actors=system", ws)).Want(http.StatusOK).JSON(&facets)
	if facets.Statuses["in_review"] != 1 || facets.Priorities["high"] != 1 || facets.Actors["agent:"+actor] != 55 || facets.Actors["system"] != 1 || facets.UnreadCount != 0 {
		t.Fatalf("incorrect facets: %+v", facets)
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.GetInboxFacets), inboxRequest(http.MethodGet, "/api/inbox/facets", ws)).Want(http.StatusOK).JSON(&facets)
	if facets.UnreadCount != 55 {
		t.Fatalf("unread facet counts rendered rows, want 55 unread groups: %+v", facets)
	}

	// Archiving the group moves it from one view to the other.
	dbfx.Exec(t, `UPDATE inbox_item SET archived = true WHERE issue_id = $1`, issue)
	if len(get("group_id="+issue).Items) != 0 || len(getArchived("group_id="+issue).Items) != 1 {
		t.Fatal("archived group still listed as active")
	}
	dbfx.Exec(t, `UPDATE inbox_item SET archived = false WHERE issue_id = $1`, issue)

	// Another recipient and another workspace cannot read this inbox, including lookup.
	otherWS := dbfx.Workspace(t, "Other inbox", "other-inbox-"+uuid.NewString())
	dbfx.Member(t, otherWS, testUserID, "owner")
	var other inboxPageResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInboxPage), inboxRequest(http.MethodGet, "/api/inbox/page?group_id="+issue, otherWS)).Want(http.StatusOK).JSON(&other)
	if len(other.Items) != 0 {
		t.Fatal("cross-workspace inbox leak")
	}
	otherUser := dbfx.User(t, "Other recipient", uuid.NewString()+"@example.test")
	dbfx.Member(t, ws, otherUser, "member")
	req := inboxRequest(http.MethodGet, "/api/inbox/page?group_id="+issue, ws)
	req.Header.Set("X-User-ID", otherUser)
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInboxPage), req).Want(http.StatusOK).JSON(&other)
	if len(other.Items) != 0 {
		t.Fatal("cross-recipient inbox leak")
	}
	for _, handler := range []http.HandlerFunc{testHandler.ListInboxPage, testHandler.GetInboxFacets} {
		req := inboxRequest(http.MethodGet, "/api/inbox/page", ws)
		req.Header.Set("X-User-ID", uuid.NewString())
		testutil.Call(t, inboxWorkspaceHandler(handler), req).Want(http.StatusNotFound)
	}
}

// Archived cursors issued before the active view existed must keep verifying,
// so the archived scope is pinned to the bytes it has always hashed.
func TestInboxPageScopeKeepsArchivedCursorsValid(t *testing.T) {
	filters := inboxFilters{
		WorkspaceID: parseUUID(uuid.NewString()), RecipientID: parseUUID(uuid.NewString()),
		Statuses: []string{"todo"}, Priorities: []string{}, Actors: []string{"system"}, UnreadOnly: true,
	}
	for _, group := range []pgtype.UUID{{}, parseUUID(uuid.NewString())} {
		encoded, err := json.Marshal(struct {
			Filters inboxFilters
			Group   pgtype.UUID
		}{filters, group})
		if err != nil {
			t.Fatal(err)
		}
		archived := inboxPageScope(archivedInboxView, filters, group)
		if want := fmt.Sprintf("%x", sha256.Sum256(encoded)); archived != want {
			t.Fatalf("archived scope changed: got %s, want %s", archived, want)
		}
		if inboxPageScope(activeInboxView, filters, group) == archived {
			t.Fatal("active and archived cursors share a scope")
		}
	}
}
