package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Unsubscribing leaves an opt-out tombstone row behind, so "subscribed" must
// mean an active row — through the real unsubscribe endpoints, for the table
// filter both ways round and for the id list clients filter with.
func TestSubscribedRelationIgnoresUnsubscribed(t *testing.T) {
	suffix := time.Now().UnixNano()
	// Through CreateIssue: fixture-numbered issues would collide with the
	// workspace issue counter the subtree fixture advances.
	create := func(title string, parent string) string {
		body := map[string]any{"title": fmt.Sprintf("subscribed %s %d", title, suffix)}
		if parent != "" {
			body["parent_issue_id"] = parent
		}
		var issue IssueResponse
		testutil.Call(t, testHandler.CreateIssue,
			newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body),
		).Want(http.StatusCreated).JSON(&issue)
		t.Cleanup(func() {
			testHandler.DeleteIssue(httptest.NewRecorder(),
				withURLParam(newRequest(http.MethodDelete, "/api/issues/"+issue.ID, nil), "id", issue.ID))
		})
		return issue.ID
	}
	kept := create("kept", "")
	left := create("left", "")
	parentID := create("parent", "")
	childID := create("child", parentID)
	for _, id := range []string{kept, left, parentID, childID} {
		subscribeTo(t, id)
	}

	req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+left+"/unsubscribe", nil), "id", left)
	testutil.Call(t, testHandler.UnsubscribeFromIssue, req).Want(http.StatusOK)
	w := httptest.NewRecorder()
	testHandler.UnsubscribeFromIssueSubtree(w,
		withURLParam(newRequest(http.MethodPost, "/api/issues/"+parentID+"/unsubscribe/subtree", nil), "id", parentID))
	if w.Code != http.StatusOK {
		t.Fatalf("UnsubscribeFromIssueSubtree: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	fixture := map[string]struct{}{kept: {}, left: {}, parentID: {}, childID: {}}
	assertRows := func(name string, filters issueTableFiltersRequest, want ...string) {
		t.Helper()
		got := issueTableFixtureRows(t, fixture, filters)
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: ids = %v, want %v", name, got, want)
		}
	}
	assertRows("subscribed", issueTableFiltersRequest{Relations: []string{"subscribed"}}, kept)
	assertRows("not subscribed",
		issueTableFiltersRequest{Relations: []string{"subscribed"}, Negate: []string{"relations"}},
		left, parentID, childID)

	ids := subscribedIssueIDs(t)
	for _, id := range []string{left, parentID, childID} {
		if _, ok := ids[id]; ok {
			t.Fatalf("unsubscribed issue %s is still listed as subscribed", id)
		}
	}
	if _, ok := ids[kept]; !ok {
		t.Fatalf("subscribed issue %s is missing from the list", kept)
	}
}

// The id list covers the caller's own member subscriptions in this workspace
// only.
func TestListSubscribedIssueIDsIsScopedToCallerAndWorkspace(t *testing.T) {
	suffix := time.Now().UnixNano()
	otherUser := dbfx.User(t, fmt.Sprintf("sub other %d", suffix), fmt.Sprintf("sub-other-%d@example.com", suffix))
	otherWorkspace := dbfx.Workspace(t, fmt.Sprintf("sub ws %d", suffix), fmt.Sprintf("sub-ws-%d", suffix))
	mine := dbfx.Issue(t, fmt.Sprintf("sub mine %d", suffix))
	theirs := dbfx.Issue(t, fmt.Sprintf("sub theirs %d", suffix))
	elsewhere := dbfx.Issue(t, fmt.Sprintf("sub elsewhere %d", suffix), testutil.Cols{"workspace_id": otherWorkspace})
	subscribe := func(issueID, userID string) {
		dbfx.InsertNoID(t, "issue_subscriber", testutil.Cols{
			"issue_id": issueID, "user_type": "member", "user_id": userID, "reason": "manual",
		}, "issue_id = $1", issueID)
	}
	subscribe(mine, testUserID)
	subscribe(theirs, otherUser)
	subscribe(elsewhere, testUserID)

	ids := subscribedIssueIDs(t)
	if _, ok := ids[mine]; !ok {
		t.Fatalf("own subscription %s is missing", mine)
	}
	for _, id := range []string{theirs, elsewhere} {
		if _, ok := ids[id]; ok {
			t.Fatalf("issue %s is not the caller's subscription in this workspace", id)
		}
	}
}

func subscribedIssueIDs(t *testing.T) map[string]struct{} {
	t.Helper()
	var out struct {
		IssueIDs []string `json:"issue_ids"`
	}
	req := newRequest(http.MethodGet, "/api/issues/subscribed", nil)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	testutil.Call(t, testHandler.ListSubscribedIssueIDs, req).Want(http.StatusOK).JSON(&out)
	ids := make(map[string]struct{}, len(out.IssueIDs))
	for _, id := range out.IssueIDs {
		ids[id] = struct{}{}
	}
	return ids
}
