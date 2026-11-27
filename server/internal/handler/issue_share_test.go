package handler

import (
	"fmt"
	"net/http"
	"strings"
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
	const avatar = "https://profiles.example.com/avatar.png"
	dbfx.Exec(t, `UPDATE "user" SET name = 'Release Reviewer', avatar_url = $1 WHERE id = $2`, avatar, creator)

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
		"author_type": "agent", "author_id": agent, "source_task_id": run, "parent_id": memberCommentID,
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
	if shared["title"] != title || shared["description"] != description || len(shared) != 8 {
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
		if !ok || comment["id"] != expected.id ||
			comment["author_type"] != expected.authorType || comment["content"] != expected.content || comment["created_at"] == nil {
			t.Fatalf("unexpected shared comment at %d: %#v", index, comment)
		}
	}
	member := comments[0].(map[string]any)
	if len(member) != 6 || member["author_name"] != "Release Reviewer" || member["author_avatar_url"] != avatar {
		t.Fatalf("unexpected public member attribution: %#v", member)
	}
	privateAgent := comments[1].(map[string]any)
	if len(privateAgent) != 6 || privateAgent["parent_id"] != memberCommentID || privateAgent["source_task_id"] != run ||
		privateAgent["author_name"] != nil || privateAgent["author_avatar_url"] != nil {
		t.Fatalf("unexpected private agent attribution: %#v", privateAgent)
	}
	runs, ok := shared["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("unexpected shared runs: %#v", shared["runs"])
	}
	summary, ok := runs[0].(map[string]any)
	if !ok || len(summary) != 4 || summary["id"] != run || summary["status"] != "completed" ||
		summary["started_at"] == nil || summary["completed_at"] == nil {
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

func TestIssueShareRunMessagesAndDeliverables(t *testing.T) {
	issue := dbfx.Issue(t, "Export review")
	otherIssue := dbfx.Issue(t, "Unshared task")
	recipient := dbfx.User(t, "Share reader", "shared-issue-resources-"+issue+"@multica.test")
	const token = "9a682af94cff2b877f6d293b"
	dbfx.InsertNoID(t, "issue_share", testutil.Cols{
		"issue_id": issue, "workspace_id": testWorkspaceID, "token": token, "created_by": testUserID,
	}, "issue_id = $1", issue)
	agent := dbfx.Agent(t, "Release verifier", "", testutil.Cols{
		"permission_mode": "public_to", "visibility": "workspace", "avatar_url": "emoji:🦊",
	})
	dbfx.Insert(t, "agent_invocation_target", testutil.Cols{
		"agent_id": agent, "target_type": "workspace", "target_id": testWorkspaceID,
	})
	completedRun := func(issueID string) string {
		return dbfx.Task(t, agent, testutil.Cols{
			"issue_id": issueID, "status": "completed", "completed_at": testutil.Raw("now()"),
		})
	}
	run, emptyRun, otherRun := completedRun(issue), completedRun(issue), completedRun(otherIssue)
	dbfx.Exec(t, `INSERT INTO task_message (task_id, seq, type, content)
		SELECT $1, seq, 'text', 'Visible transcript row' FROM generate_series(0, $2::integer) AS seq`, run, sharedIssueMessagePageSize)
	dbfx.Insert(t, "task_message", testutil.Cols{
		"task_id": run, "seq": sharedIssueMessagePageSize + 1, "type": "thinking", "content": "Private reasoning",
	})
	dbfx.Insert(t, "task_message", testutil.Cols{
		"task_id": run, "seq": sharedIssueMessagePageSize + 2, "type": "tool_result",
		"content": "Private tool response", "output": "Private output", "input": testutil.Raw(`'{"credential":"private"}'::jsonb`),
	})
	const contents = "Version two of the release notes"
	store := &mockStorage{}
	store.put("test-share/release.txt", []byte(contents))
	h := &Handler{Queries: testHandler.Queries, Storage: store}
	comment := dbfx.Comment(t, issue, "Release notes are ready", testutil.Cols{
		"author_type": "agent", "author_id": agent, "source_task_id": run,
	})
	otherComment := dbfx.Comment(t, otherIssue, "Unshared file")
	addFile := func(issueID string, commentID any, name, key, typeName, created string) string {
		return dbfx.Insert(t, "attachment", testutil.Cols{
			"workspace_id": testWorkspaceID, "issue_id": issueID, "comment_id": commentID,
			"uploader_type": "member", "uploader_id": testUserID, "filename": name,
			"url": store.ObjectURL(key), "content_type": typeName, "size_bytes": len(contents),
			"created_at": testutil.Raw(created),
		})
	}
	old := addFile(issue, comment, "release.txt", "test-share/old-release.txt", "text/plain; charset=utf-8", "now() - interval '1 minute'")
	file := addFile(issue, comment, "release.txt", "test-share/release.txt", "text/plain", "now()")
	otherFile := addFile(otherIssue, otherComment, "other.txt", "test-share/other.txt", "text/plain", "now()")
	issueInput := addFile(issue, nil, "input.txt", "test-share/input.txt", "text/plain", "now()")
	unknownStorage := addFile(issue, comment, "external.txt", "test-share/external.txt", "text/plain", "now()")
	dbfx.Exec(t, "UPDATE attachment SET url = $1 WHERE id = $2", "https://s3.example.com/test-bucket/test-share/release.txt", unknownStorage)

	request := func(user, shareToken, path string) *http.Request {
		req := newRequest(http.MethodGet, path, nil)
		if user != "" {
			req = asUser(req, user)
		} else {
			req.Header.Del("X-User-ID")
		}
		req.Header.Set(issueShareTokenHeader, shareToken)
		return req
	}
	messages := func(user, shareToken, runID, query string) *testutil.Response {
		path := fmt.Sprintf("/api/share/issue/runs/%s/messages%s", runID, query)
		return testutil.Call(t, h.ListSharedIssueRunMessages, withURLParam(request(user, shareToken, path), "runId", runID))
	}
	download := func(user, shareToken, fileID string) *testutil.Response {
		return testutil.Call(t, h.DownloadSharedIssueDeliverable, withURLParam(request(user, shareToken, "/api/share/issue/deliverables/"+fileID), "attachmentId", fileID))
	}
	var shared sharedIssue
	testutil.Call(t, h.GetSharedIssue, request(recipient, token, "/api/share/issue")).Want(http.StatusOK).JSON(&shared)
	if len(shared.Deliverables) != 1 || shared.Deliverables[0].ID != file || shared.Deliverables[0].Filename != "release.txt" {
		t.Fatalf("unexpected deliverables: %#v", shared.Deliverables)
	}
	if len(shared.Comments) != 1 || shared.Comments[0].AuthorName != "Release verifier" || shared.Comments[0].AuthorAvatarURL != "emoji:🦊" {
		t.Fatalf("unexpected comment attribution: %#v", shared.Comments)
	}
	if len(shared.Runs) != 2 || shared.Runs[0].AgentName != "Release verifier" || shared.Runs[0].AgentAvatarURL != "emoji:🦊" {
		t.Fatalf("unexpected public agent runs: %#v", shared.Runs)
	}

	messages("", token, run, "").Want(http.StatusUnauthorized)
	messages(recipient, "", run, "").Want(http.StatusNotFound)
	messages(recipient, token, otherRun, "").Want(http.StatusNotFound)
	messages(recipient, token, run, "?after=-2").Want(http.StatusBadRequest)
	var page sharedIssueRunMessages
	messages(recipient, token, run, "").Want(http.StatusOK).JSON(&page)
	if len(page.Messages) != sharedIssueMessagePageSize || page.Messages[0].Seq != 0 ||
		page.NextAfter == nil || *page.NextAfter != sharedIssueMessagePageSize-1 {
		t.Fatalf("unexpected first message page: size=%d, next=%v", len(page.Messages), page.NextAfter)
	}
	response := messages(recipient, token, run, fmt.Sprintf("?after=%d", *page.NextAfter)).Want(http.StatusOK)
	response.JSON(&page)
	if len(page.Messages) != 1 || page.Messages[0].Content != "Visible transcript row" || page.NextAfter != nil ||
		strings.Contains(response.Text(), "Private") || strings.Contains(response.Text(), "credential") {
		t.Fatalf("unexpected final message page: %s", response.Text())
	}
	var empty sharedIssueRunMessages
	messages(recipient, token, emptyRun, "").Want(http.StatusOK).JSON(&empty)
	if empty.Messages == nil || len(empty.Messages) != 0 || empty.NextAfter != nil {
		t.Fatalf("unexpected empty message page: %#v", empty)
	}

	download("", token, file).Want(http.StatusUnauthorized)
	download(recipient, "", file).Want(http.StatusNotFound)
	for _, id := range []string{otherFile, old, issueInput, unknownStorage} {
		download(recipient, token, id).Want(http.StatusNotFound)
	}
	downloaded := download(recipient, token, file).Want(http.StatusOK)
	if downloaded.Text() != contents || downloaded.Header().Get("Content-Disposition") != `attachment; filename="release.txt"` ||
		downloaded.Header().Get("Location") != "" || downloaded.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected download: headers=%v body=%q", downloaded.Header(), downloaded.Text())
	}

	revoke := asUser(newRequest(http.MethodDelete, "/api/issues/"+issue+"/share", nil), testUserID)
	revoke.Header.Set(issueShareTokenHeader, token)
	testutil.Call(t, inboxWorkspaceHandler(testHandler.RevokeIssueShare), withURLParam(revoke, "id", issue)).Want(http.StatusNoContent)
	messages(recipient, token, run, "").Want(http.StatusNotFound)
	download(recipient, token, file).Want(http.StatusNotFound)
}
