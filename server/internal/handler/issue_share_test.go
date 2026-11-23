package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueSharePermissionsAndLifecycle(t *testing.T) {
	guest := newGuestFixture(t)
	creator := createPlainMember(t, guest.marker+"-creator@multica.test")
	assignee := createPlainMember(t, guest.marker+"-assignee@multica.test")
	otherMember := createPlainMember(t, guest.marker+"-member@multica.test")
	admin := createPlainMember(t, guest.marker+"-admin@multica.test")
	recipient := dbfx.User(t, "Outside reviewer", guest.marker+"-recipient@multica.test")
	dbfx.Exec(t, "UPDATE member SET role = 'admin' WHERE workspace_id = $1 AND user_id = $2", testWorkspaceID, admin)

	const title = "Review the release checklist"
	const description = "The release candidate is ready for review."
	issue := dbfx.Issue(t, title, testutil.Cols{
		"creator_type": "member", "creator_id": creator,
		"assignee_type": "member", "assignee_id": assignee,
		"description": description, "project_id": guest.shared,
	})
	dbfx.Cleanup(t, "DELETE FROM issue_share WHERE issue_id = $1", issue)

	manage := func(handler http.HandlerFunc, method, user, issueID, token string) *testutil.Response {
		req := withGuestCapability(asUser(newRequest(method, "/api/issues/"+issueID+"/share", nil), user))
		req.Header.Set(issueShareTokenHeader, token)
		return testutil.Call(t, inboxWorkspaceHandler(handler), withURLParam(req, "id", issueID))
	}
	read := func(user, token string) *testutil.Response {
		req := asUser(newRequest(http.MethodGet, "/api/share/issue", nil), user)
		req.Header.Set(issueShareTokenHeader, token)
		return testutil.Call(t, testHandler.GetSharedIssue, req)
	}

	var before issueShareResponse
	manage(testHandler.GetIssueShare, http.MethodGet, creator, issue, "").Want(http.StatusOK).JSON(&before)
	if before.Token != nil {
		t.Fatal("reading the sharing state created a link")
	}
	manage(testHandler.CreateIssueShare, http.MethodPost, otherMember, issue, "").Want(http.StatusForbidden)
	manage(testHandler.CreateIssueShare, http.MethodPost, guest.guest, issue, "").Want(http.StatusForbidden)
	manage(testHandler.CreateIssueShare, http.MethodPost, recipient, issue, "").Want(http.StatusNotFound)
	manage(testHandler.CreateIssueShare, http.MethodPost, admin, guest.secretIssue, "").Want(http.StatusNotFound)

	var share issueShareResponse
	manage(testHandler.CreateIssueShare, http.MethodPost, creator, issue, "").Want(http.StatusOK).JSON(&share)
	if share.Token == nil || !validIssueShareToken(*share.Token) {
		t.Fatal("creation did not return a share token")
	}
	token := *share.Token
	for _, user := range []string{assignee, admin, testUserID} {
		var existing issueShareResponse
		manage(testHandler.CreateIssueShare, http.MethodPost, user, issue, "").Want(http.StatusOK).JSON(&existing)
		if existing.Token == nil || *existing.Token != token {
			t.Fatal("creating an existing share did not preserve its URL")
		}
	}

	manage(testHandler.GetIssueShare, http.MethodGet, otherMember, issue, "").Want(http.StatusForbidden)
	manage(testHandler.RevokeIssueShare, http.MethodDelete, otherMember, issue, token).Want(http.StatusForbidden)
	var shared map[string]any
	response := read(recipient, token).Want(http.StatusOK).JSON(&shared)
	if shared["title"] != title || shared["description"] != description || len(shared) != 5 {
		t.Fatalf("unexpected shared fields: %#v", shared)
	}
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("shared response cache control = %q", got)
	}
	read(recipient, issue).Want(http.StatusNotFound)

	manage(testHandler.RevokeIssueShare, http.MethodDelete, assignee, issue, token).Want(http.StatusNoContent)
	read(recipient, token).Want(http.StatusNotFound)

	var regenerated issueShareResponse
	manage(testHandler.CreateIssueShare, http.MethodPost, testUserID, issue, "").Want(http.StatusOK).JSON(&regenerated)
	if regenerated.Token == nil || *regenerated.Token == token {
		t.Fatal("regenerating a revoked share reused its URL")
	}
	manage(testHandler.RevokeIssueShare, http.MethodDelete, creator, issue, token).Want(http.StatusConflict)
	read(recipient, token).Want(http.StatusNotFound)
	read(recipient, *regenerated.Token).Want(http.StatusOK)
}
