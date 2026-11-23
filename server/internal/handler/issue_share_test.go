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
	const memberComment = "Please verify the export."
	const agentComment = "Both export checks passed."
	runtime := dbfx.Runtime(t, "Share test runtime", testutil.Cols{"status": "offline"})
	agent := dbfx.Agent(t, "Internal agent name", runtime)
	run := dbfx.Task(t, agent, testutil.Cols{
		"issue_id": issue, "status": "completed",
		"started_at": testutil.Raw("now() - interval '2 minutes'"), "completed_at": testutil.Raw("now()"),
		"result": testutil.Raw(`'{"output":"internal transcript"}'::jsonb`), "handoff_note": "internal handoff",
	})
	memberCommentID := dbfx.Comment(t, issue, memberComment, testutil.Cols{
		"author_id": creator, "created_at": testutil.Raw("now() - interval '3 minutes'"),
	})
	agentCommentID := dbfx.Comment(t, issue, agentComment, testutil.Cols{
		"author_type": "agent", "author_id": agent, "source_task_id": run,
	})
	dbfx.Comment(t, issue, "Deleted comment", testutil.Cols{"deleted_at": testutil.Raw("now()")})
	dbfx.Comment(t, guest.secretIssue, "Comment from another issue")
	otherRun := dbfx.Task(t, agent, testutil.Cols{
		"issue_id": guest.secretIssue, "status": "completed", "completed_at": testutil.Raw("now()"),
	})
	dbfx.Comment(t, issue, "Comment from another run", testutil.Cols{
		"author_type": "agent", "author_id": agent, "source_task_id": otherRun,
	})
	dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": runtime, "status": "running"})
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
	if shared["title"] != title || shared["description"] != description || len(shared) != 7 {
		t.Fatalf("unexpected shared fields: %#v", shared)
	}
	comments, ok := shared["comments"].([]any)
	if !ok || len(comments) != 2 {
		t.Fatalf("unexpected shared comments: %#v", shared["comments"])
	}
	for index, expected := range []struct{ id, authorType, content string }{
		{memberCommentID, "member", memberComment},
		{agentCommentID, "agent", agentComment},
	} {
		comment, ok := comments[index].(map[string]any)
		if !ok || len(comment) != 4 || comment["id"] != expected.id ||
			comment["author_type"] != expected.authorType || comment["content"] != expected.content || comment["created_at"] == nil {
			t.Fatalf("unexpected shared comment at %d: %#v", index, comment)
		}
	}
	runs, ok := shared["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("unexpected shared runs: %#v", shared["runs"])
	}
	summary, ok := runs[0].(map[string]any)
	if !ok || len(summary) != 3 || summary["id"] != run || summary["status"] != "completed" || summary["completed_at"] == nil {
		t.Fatalf("unexpected shared run summary: %#v", summary)
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
