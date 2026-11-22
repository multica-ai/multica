package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

// Regression tests for the guest boundaries found in review of #10766
// (MUL-9420). Each test fails on the code before the fix.

// --- Plugins ---------------------------------------------------------------

func pluginProblemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var problem publicapiv1.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem %s: %v", rec.Body.String(), err)
	}
	return problem.Code
}

// A guest reaches no Plugin endpoint, through a surface session or a callback
// token: neither the unshared project nor a write to its shared project.
func TestPluginActionRefusesGuests(t *testing.T) {
	f := newGuestFixture(t)
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write", "comments:read", "comments:write"})

	asGuest := func(method, path, issue string, body any) *http.Request {
		req := pluginActionRequest(method, path, installationID, body, map[string]string{"issue_ref": issue})
		req.Header.Set("X-User-ID", f.guest)
		return req
	}

	read := httptest.NewRecorder()
	testHandler.GetPluginIssue(read, asGuest(http.MethodGet, "/v1/issues/"+f.otherIssue, f.otherIssue, nil))
	if read.Code != http.StatusForbidden || pluginProblemCode(t, read) != "guest_not_allowed" {
		t.Fatalf("guest plugin read of an unshared issue = %d %s, want 403 guest_not_allowed", read.Code, read.Body.String())
	}

	var before string
	dbfx.QueryRow(t, `SELECT title FROM issue WHERE id = $1`, f.sharedIssue).Scan(&before)
	patch := httptest.NewRecorder()
	testHandler.PatchPluginIssue(patch, asGuest(http.MethodPatch, "/v1/issues/"+f.sharedIssue, f.sharedIssue,
		map[string]any{"title": "renamed by a guest"}))
	if patch.Code != http.StatusForbidden {
		t.Fatalf("guest plugin patch of its shared issue = %d %s, want 403", patch.Code, patch.Body.String())
	}
	var after string
	dbfx.QueryRow(t, `SELECT title FROM issue WHERE id = $1`, f.sharedIssue).Scan(&after)
	if after != before {
		t.Fatalf("title = %q after a refused guest patch, want %q", after, before)
	}

	withCallbackTokens(t)
	token := issueCallbackToken(t, installationID, service.HookActor{Type: "member", ID: parseUUID(f.guest)})
	cb := httptest.NewRecorder()
	testHandler.GetPluginIssue(cb, callbackRequest(token, http.MethodGet, "/v1/issues/"+f.otherIssue, nil,
		map[string]string{"issue_ref": f.otherIssue}))
	if cb.Code != http.StatusForbidden || pluginProblemCode(t, cb) != "guest_not_allowed" {
		t.Fatalf("guest callback read = %d %s, want 403 guest_not_allowed", cb.Code, cb.Body.String())
	}
}

// A call acting as a member reaches only the private projects that member is
// in, as the ordinary issue endpoint does.
func TestPluginActionHidesPrivateProjectsFromMembers(t *testing.T) {
	f := newGuestFixture(t)
	installationID := installPluginForAction(t, []string{"issues:read"})
	outsider := createPlainMember(t, f.marker+"-plugin-outsider@multica.test")

	get := func(userID string) int {
		req := pluginActionRequest(http.MethodGet, "/v1/issues/"+f.secretIssue, installationID, nil, map[string]string{"issue_ref": f.secretIssue})
		req.Header.Set("X-User-ID", userID)
		rec := httptest.NewRecorder()
		testHandler.GetPluginIssue(rec, req)
		return rec.Code
	}
	if code := get(outsider); code != http.StatusNotFound {
		t.Fatalf("member outside the private project: plugin read = %d, want 404", code)
	}
	if code := get(testUserID); code != http.StatusOK {
		t.Fatalf("member of the private project: plugin read = %d, want 200", code)
	}
}

// --- Agents, runs, attachments and mentions -------------------------------

// A guest is shown the agents it meets in its projects, not the directory.
func TestGuestAgentListHasOnlyAgentsInItsProjects(t *testing.T) {
	f := newGuestFixture(t)
	met := dbfx.Agent(t, f.marker+" met", "")
	elsewhere := dbfx.Agent(t, f.marker+" elsewhere", "")
	archived := dbfx.Agent(t, f.marker+" archived", "", testutil.Cols{"archived_at": time.Now()})
	dbfx.Comment(t, f.sharedIssue, "agent reply", testutil.Cols{"author_type": "agent", "author_id": met})
	dbfx.Comment(t, f.otherIssue, "agent reply", testutil.Cols{"author_type": "agent", "author_id": elsewhere})

	for _, path := range []string{"/api/agents", "/api/agents?include_archived=true"} {
		var agents []AgentResponse
		guestRequest(t, testHandler.ListAgents, f.guest, http.MethodGet, path, nil).Want(http.StatusOK).JSON(&agents)
		seen := map[string]bool{}
		for _, a := range agents {
			seen[a.ID] = true
		}
		if !seen[met] {
			t.Errorf("%s: guest misses the agent that replied in its project", path)
		}
		if seen[elsewhere] || seen[archived] {
			t.Errorf("%s: guest sees agents outside its projects: %v", path, seen)
		}
	}
}

// A guest learns that an agent is working on its issue, not where or how.
func TestGuestActiveTaskHasNoRunDetails(t *testing.T) {
	f := newGuestFixture(t)
	runtime := dbfx.Runtime(t, f.marker+" runtime")
	agent := dbfx.Agent(t, f.marker+" worker", runtime)
	dbfx.Task(t, agent, testutil.Cols{
		"issue_id": f.sharedIssue, "status": "running", "runtime_id": runtime,
		"work_dir": "/Users/alice/work/secret-repo", "handoff_note": "use the staging credentials",
	})

	get := func(userID string) string {
		return guestRequest(t, testHandler.GetActiveTaskForIssue, userID, http.MethodGet,
			"/api/issues/"+f.sharedIssue+"/active-task", nil, "id", f.sharedIssue).Want(http.StatusOK).Body.String()
	}
	guest := get(f.guest)
	for _, leak := range []string{"secret-repo", "staging credentials", "work_dir", "handoff_note"} {
		if strings.Contains(guest, leak) {
			t.Errorf("guest active-task carries %q: %s", leak, guest)
		}
	}
	if !strings.Contains(guest, agent) || !strings.Contains(guest, f.sharedIssue) {
		t.Errorf("guest active-task lost the agent or issue: %s", guest)
	}
	if member := get(testUserID); !strings.Contains(member, "staging credentials") {
		t.Errorf("member active-task lost the handoff note: %s", member)
	}
}

func uploadToComment(t *testing.T, userID, commentID string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "note.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("\x89PNG\r\n\x1a\nrest-of-bytes"))
	writer.WriteField("comment_id", commentID)
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.UploadFile(rec, req)
	return rec
}

// A guest attaches files only to its own comments.
func TestGuestUploadsOnlyToItsOwnComments(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = origStorage })
	f := newGuestFixture(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE uploader_id = $1`, f.guest)
	})
	others := dbfx.Comment(t, f.sharedIssue, "member comment", testutil.Cols{"author_type": "member", "author_id": testUserID})
	own := dbfx.Comment(t, f.sharedIssue, "guest comment", testutil.Cols{"author_type": "member", "author_id": f.guest})

	if rec := uploadToComment(t, f.guest, others); rec.Code != http.StatusForbidden {
		t.Errorf("guest upload to someone else's comment = %d %s, want 403", rec.Code, rec.Body.String())
	}
	if rec := uploadToComment(t, f.guest, own); rec.Code != http.StatusOK {
		t.Errorf("guest upload to its own comment = %d %s, want 200", rec.Code, rec.Body.String())
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE comment_id = $1`, others); n != 0 {
		t.Errorf("someone else's comment has %d guest attachments, want 0", n)
	}
}

func uploadDraftToIssue(t *testing.T, userID, issueID string) AttachmentResponse {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "draft.png")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("\x89PNG\r\n\x1a\nrest-of-bytes"))
	writer.WriteField("issue_id", issueID)
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.UploadFile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload draft = %d %s, want 200", rec.Code, rec.Body.String())
	}
	var att AttachmentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &att); err != nil {
		t.Fatal(err)
	}
	return att
}

func attachmentBinding(t *testing.T, id string) (issueID, commentID *string) {
	t.Helper()
	dbfx.QueryRow(t, `SELECT issue_id::text, comment_id::text FROM attachment WHERE id = $1`, id).Scan(&issueID, &commentID)
	return issueID, commentID
}

// A guest's comment draft upload does not touch the issue: it stays unbound
// until the guest's comment (new or edited) takes it. A member's upload still
// attaches to the issue at once.
func TestGuestDraftUploadBindsOnlyWithItsComment(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = origStorage })
	f := newGuestFixture(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE issue_id = $1 OR uploader_id = $2`, f.sharedIssue, f.guest)
	})
	revision := func() int64 {
		var rev int64
		dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id = $1`, f.sharedIssue).Scan(&rev)
		return rev
	}

	before := revision()
	draft := uploadDraftToIssue(t, f.guest, f.sharedIssue)
	if issueID, commentID := attachmentBinding(t, draft.ID); issueID != nil || commentID != nil {
		t.Fatalf("guest draft bound to issue %v comment %v before any comment, want unbound", issueID, commentID)
	}
	if got := revision(); got != before {
		t.Fatalf("issue revision %d -> %d from a guest draft upload, want unchanged", before, got)
	}
	var listed []AttachmentResponse
	guestRequest(t, testHandler.ListAttachments, testUserID, http.MethodGet, "/api/issues/"+f.sharedIssue+"/attachments", nil, "id", f.sharedIssue).
		Want(http.StatusOK).JSON(&listed)
	for _, a := range listed {
		if a.ID == draft.ID {
			t.Fatal("the issue's attachments list a guest draft before its comment is sent")
		}
	}

	var comment CommentResponse
	guestRequest(t, testHandler.CreateComment, f.guest, http.MethodPost, "/api/issues/"+f.sharedIssue+"/comments",
		CreateCommentRequest{Content: "with a file", AttachmentIDs: []string{draft.ID}}, "id", f.sharedIssue).
		Want(http.StatusCreated).JSON(&comment)
	if issueID, commentID := attachmentBinding(t, draft.ID); issueID == nil || *issueID != f.sharedIssue || commentID == nil || *commentID != comment.ID {
		t.Fatalf("guest draft after its comment: issue %v comment %v, want the issue and the comment", issueID, commentID)
	}

	// Editing its comment takes a new draft the same way.
	second := uploadDraftToIssue(t, f.guest, f.sharedIssue)
	guestRequest(t, testHandler.UpdateComment, f.guest, http.MethodPut, "/api/comments/"+comment.ID,
		map[string]any{"content": "with two files", "attachment_ids": []string{draft.ID, second.ID}}, "commentId", comment.ID).
		Want(http.StatusOK)
	if issueID, commentID := attachmentBinding(t, second.ID); issueID == nil || *issueID != f.sharedIssue || commentID == nil || *commentID != comment.ID {
		t.Fatalf("guest edit draft: issue %v comment %v, want the issue and the comment", issueID, commentID)
	}

	// Removing both files from its comment takes them off the issue too: they
	// go back to being the guest's drafts.
	guestRequest(t, testHandler.UpdateComment, f.guest, http.MethodPut, "/api/comments/"+comment.ID,
		map[string]any{"content": "no files", "attachment_ids": []string{}}, "commentId", comment.ID).
		Want(http.StatusOK)
	for _, id := range []string{draft.ID, second.ID} {
		if issueID, commentID := attachmentBinding(t, id); issueID != nil || commentID != nil {
			t.Fatalf("guest file removed from its comment: issue %v comment %v, want unbound", issueID, commentID)
		}
	}
	guestRequest(t, testHandler.ListAttachments, testUserID, http.MethodGet, "/api/issues/"+f.sharedIssue+"/attachments", nil, "id", f.sharedIssue).
		Want(http.StatusOK).JSON(&listed)
	for _, a := range listed {
		if a.ID == draft.ID || a.ID == second.ID {
			t.Fatal("the issue still lists a file the guest removed from its comment")
		}
	}

	// A member's upload attaches to the issue as before, and stays there when
	// removed from the member's comment.
	memberUpload := uploadDraftToIssue(t, testUserID, f.sharedIssue)
	if issueID, _ := attachmentBinding(t, memberUpload.ID); issueID == nil || *issueID != f.sharedIssue {
		t.Fatalf("member upload issue = %v, want bound to the issue", issueID)
	}
	var memberComment CommentResponse
	guestRequest(t, testHandler.CreateComment, testUserID, http.MethodPost, "/api/issues/"+f.sharedIssue+"/comments",
		CreateCommentRequest{Content: "member file", AttachmentIDs: []string{memberUpload.ID}}, "id", f.sharedIssue).
		Want(http.StatusCreated).JSON(&memberComment)
	guestRequest(t, testHandler.UpdateComment, testUserID, http.MethodPut, "/api/comments/"+memberComment.ID,
		map[string]any{"content": "member file removed", "attachment_ids": []string{}}, "commentId", memberComment.ID).
		Want(http.StatusOK)
	if issueID, commentID := attachmentBinding(t, memberUpload.ID); issueID == nil || *issueID != f.sharedIssue || commentID != nil {
		t.Fatalf("member file removed from its comment: issue %v comment %v, want kept on the issue", issueID, commentID)
	}
}

// waitForLockWaiters blocks until at least n other backends of this database
// wait on a lock.
func waitForLockWaiters(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d backends waiting on a lock, want %d", waiting, n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Publishing a comment and editing another one with the same draft take the
// draft and the issue in the same order, so they never deadlock: the issue
// lock is held while the edit and then the publish queue behind it.
func TestCommentEditAndCreateWithSameDraftDoNotDeadlock(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = origStorage })
	f := newGuestFixture(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE uploader_id = $1`, f.guest)
	})
	var existing CommentResponse
	guestRequest(t, testHandler.CreateComment, f.guest, http.MethodPost, "/api/issues/"+f.sharedIssue+"/comments",
		CreateCommentRequest{Content: "to be edited"}, "id", f.sharedIssue).Want(http.StatusCreated).JSON(&existing)
	draft := uploadDraftToIssue(t, f.guest, f.sharedIssue)

	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM issue WHERE id = $1 FOR UPDATE`, f.sharedIssue); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var edit, create *testutil.Response
	wg.Add(2)
	go func() {
		defer wg.Done()
		edit = guestRequest(t, testHandler.UpdateComment, f.guest, http.MethodPut, "/api/comments/"+existing.ID,
			map[string]any{"content": "edited with the draft", "attachment_ids": []string{draft.ID}}, "commentId", existing.ID)
	}()
	waitForLockWaiters(t, 1)
	go func() {
		defer wg.Done()
		create = guestRequest(t, testHandler.CreateComment, f.guest, http.MethodPost, "/api/issues/"+f.sharedIssue+"/comments",
			CreateCommentRequest{Content: "published with the draft", AttachmentIDs: []string{draft.ID}}, "id", f.sharedIssue)
	}()
	waitForLockWaiters(t, 2)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	for name, resp := range map[string]*testutil.Response{"edit": edit, "create": create} {
		if resp.Code >= http.StatusInternalServerError {
			t.Errorf("%s = %d %s, want no server error", name, resp.Code, resp.Body.String())
		}
	}
	if edit.Code != http.StatusOK {
		t.Fatalf("edit = %d %s, want 200: it queued first", edit.Code, edit.Body.String())
	}
	// The edit took the draft; the publish is refused instead of failing.
	if create.Code != http.StatusConflict {
		t.Fatalf("create = %d %s, want 409 for a draft the edit already took", create.Code, create.Body.String())
	}
	if _, commentID := attachmentBinding(t, draft.ID); commentID == nil || *commentID != existing.ID {
		t.Fatalf("draft comment = %v, want the edited comment", commentID)
	}
}

// Editing a comment applies the same mention rule as creating one.
func TestGuestCommentMentionRuleCoversCreateAndEdit(t *testing.T) {
	f := newGuestFixture(t)
	agent := dbfx.Agent(t, f.marker+" mentioned", "")
	agentMention := fmt.Sprintf("[@a](mention://agent/%s) please", agent)
	allMention := "[@All](mention://all/all) heads up"

	create := func(content string) *testutil.Response {
		return guestRequest(t, testHandler.CreateComment, f.guest, http.MethodPost,
			"/api/issues/"+f.sharedIssue+"/comments", CreateCommentRequest{Content: content}, "id", f.sharedIssue)
	}
	create(allMention).Want(http.StatusForbidden)

	var comment CommentResponse
	create("first draft").Want(http.StatusCreated).JSON(&comment)
	edit := func(content string) *testutil.Response {
		return guestRequest(t, testHandler.UpdateComment, f.guest, http.MethodPut,
			"/api/comments/"+comment.ID, map[string]any{"content": content}, "commentId", comment.ID)
	}
	edit(agentMention).Want(http.StatusForbidden)
	edit(allMention).Want(http.StatusForbidden)
	edit("second draft").Want(http.StatusOK)

	var content string
	dbfx.QueryRow(t, `SELECT content FROM comment WHERE id = $1`, comment.ID).Scan(&content)
	if content != "second draft" {
		t.Fatalf("comment content = %q, want the last allowed edit", content)
	}
}

// A guest deletes its own comment through the path that keeps replies.
func TestGuestMayDeleteOwnCommentKeepingReplies(t *testing.T) {
	f := newGuestFixture(t)
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	rec := guestRequest(t, ok, f.guest, http.MethodDelete, "/api/comments/"+f.sharedIssue+"/keep-replies", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("guest DELETE keep-replies = %d, want the route allowed", rec.Code)
	}
}

// A guest takes back an Unsubscribe & archive from its Inbox, on an issue in
// its project only.
func TestGuestUndoesItsUnsubscribe(t *testing.T) {
	f := newGuestFixture(t)
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM issue_subscriber WHERE issue_id IN ($1, $2)`, f.sharedIssue, f.otherIssue)
	})
	dbfx.Exec(t, `INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason) VALUES ($1, 'member', $2, 'commenter')`,
		f.sharedIssue, f.guest)
	post := func(h http.HandlerFunc, issue, suffix string) *testutil.Response {
		t.Helper()
		return guestRequest(t, h, f.guest, http.MethodPost, "/api/issues/"+issue+suffix, nil, "id", issue)
	}

	post(testHandler.UnsubscribeFromIssue, f.sharedIssue, "/unsubscribe").Want(http.StatusOK)
	var body map[string]any
	post(testHandler.UndoUnsubscribeFromIssue, f.sharedIssue, "/unsubscribe/undo").Want(http.StatusOK).JSON(&body)
	if body["reason"] != "commenter" {
		t.Fatalf("guest undo answered %v, want its commenter subscription back", body)
	}
	post(testHandler.UndoUnsubscribeFromIssue, f.otherIssue, "/unsubscribe/undo").Want(http.StatusNotFound)
}

// A guest's Unsubscribe & archive may take the sub-issues too, for itself
// only, and on an issue in its project only.
func TestGuestUnsubscribesItselfFromASubtree(t *testing.T) {
	f := newGuestFixture(t)
	child := dbfx.Issue(t, f.marker+" child", testutil.Cols{"project_id": f.shared, "parent_issue_id": f.sharedIssue})
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM issue_subscriber WHERE issue_id IN ($1, $2)`, f.sharedIssue, child)
	})
	for _, issue := range []string{f.sharedIssue, child} {
		for _, user := range []string{f.guest, testUserID} {
			dbfx.Exec(t, `INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason) VALUES ($1, 'member', $2, 'commenter')`, issue, user)
		}
	}
	subscribed := func(issue, user string) bool {
		t.Helper()
		var n int
		dbfx.QueryRow(t, `SELECT count(*) FROM issue_subscriber
			WHERE issue_id = $1 AND user_type = 'member' AND user_id = $2 AND unsubscribed_at IS NULL`, issue, user).Scan(&n)
		return n > 0
	}
	post := func(issue string, body any) *testutil.Response {
		t.Helper()
		return guestRequest(t, testHandler.UnsubscribeFromIssueSubtree, f.guest, http.MethodPost,
			"/api/issues/"+issue+"/unsubscribe/subtree", body, "id", issue)
	}

	post(f.sharedIssue, map[string]string{"user_id": testUserID, "user_type": "member"}).Want(http.StatusForbidden)
	if !subscribed(f.sharedIssue, testUserID) || !subscribed(child, testUserID) {
		t.Fatal("a guest's subtree unsubscribe took a member's subscriptions")
	}
	post(f.otherIssue, nil).Want(http.StatusNotFound)
	post(f.sharedIssue, nil).Want(http.StatusOK)
	if subscribed(f.sharedIssue, f.guest) || subscribed(child, f.guest) {
		t.Fatal("the guest is still subscribed to the tree it left")
	}
}

// A guest subscribes and unsubscribes itself only: subscribing a member
// notifies them, and unsubscribing one hides replies and mentions from them.
// A member may still change anyone's subscription.
func TestGuestChangesOnlyItsOwnSubscription(t *testing.T) {
	f := newGuestFixture(t)
	agent := dbfx.Agent(t, f.marker+" watcher", "")
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM issue_subscriber WHERE issue_id = $1`, f.sharedIssue)
	})
	subscribed := func(userType, user string) bool {
		t.Helper()
		var n int
		dbfx.QueryRow(t, `SELECT count(*) FROM issue_subscriber
			WHERE issue_id = $1 AND user_type = $2 AND user_id = $3 AND unsubscribed_at IS NULL`,
			f.sharedIssue, userType, user).Scan(&n)
		return n > 0
	}
	post := func(h http.HandlerFunc, suffix string, body any) *testutil.Response {
		t.Helper()
		return guestRequest(t, h, f.guest, http.MethodPost, "/api/issues/"+f.sharedIssue+suffix, body, "id", f.sharedIssue)
	}
	target := func(userType, user string) map[string]string {
		return map[string]string{"user_id": user, "user_type": userType}
	}

	post(testHandler.SubscribeToIssue, "/subscribe", target("member", testUserID)).Want(http.StatusForbidden)
	post(testHandler.SubscribeToIssue, "/subscribe", target("agent", agent)).Want(http.StatusForbidden)
	if subscribed("member", testUserID) || subscribed("agent", agent) {
		t.Fatal("a guest subscribed someone else")
	}

	dbfx.Exec(t, `INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason) VALUES ($1, 'member', $2, 'commenter')`,
		f.sharedIssue, testUserID)
	post(testHandler.UnsubscribeFromIssue, "/unsubscribe", target("member", testUserID)).Want(http.StatusForbidden)
	if !subscribed("member", testUserID) {
		t.Fatal("a guest unsubscribed a member")
	}

	post(testHandler.SubscribeToIssue, "/subscribe", target("member", f.guest)).Want(http.StatusOK)
	if !subscribed("member", f.guest) {
		t.Fatal("the guest could not subscribe itself")
	}
	post(testHandler.UnsubscribeFromIssue, "/unsubscribe", target("member", strings.ToUpper(f.guest))).Want(http.StatusOK)
	if subscribed("member", f.guest) {
		t.Fatal("the guest could not unsubscribe itself")
	}

	asMember := withURLParams(newRequest(http.MethodPost, "/api/issues/"+f.sharedIssue+"/subscribe",
		target("member", f.guest)), "id", f.sharedIssue)
	testutil.Call(t, inboxWorkspaceHandler(testHandler.SubscribeToIssue), asMember).Want(http.StatusOK)
	if !subscribed("member", f.guest) {
		t.Fatal("a member could not subscribe someone else")
	}
}

// A guest settles the auto-archive prompt its Inbox shows it.
func TestGuestResolvesAutoArchivePrompt(t *testing.T) {
	f := newGuestFixture(t)
	guestRequest(t, testHandler.ResolveAutoArchivePrompt, f.guest, http.MethodPost,
		"/api/notification-preferences/auto-archive-prompt",
		map[string]any{"preferences": map[string]string{"auto_archive_completed": "all"}}).
		Want(http.StatusOK)
}

// --- Local search after losing access --------------------------------------

func searchChangesAs(t *testing.T, userID, cursor string) *testutil.Response {
	t.Helper()
	req := asUser(searchIndexRequest(http.MethodPost, "/api/search-index/changes", testWorkspaceID,
		map[string]any{"cursor": cursor, "limit": 1}), userID)
	return testutil.Call(t, testHandler.ListSearchIndexChanges, req)
}

func searchCursorAs(t *testing.T, userID string) string {
	t.Helper()
	var manifest SearchIndexManifestResponse
	req := asUser(searchIndexRequest(http.MethodGet, "/api/search-index/manifest", testWorkspaceID, nil), userID)
	testutil.Call(t, testHandler.GetSearchIndexManifest, req).Want(http.StatusOK).JSON(&manifest)
	return manifest.Cursor
}

// A member made a guest, and a guest losing a project, must rebuild a local
// copy built before: nothing it can no longer see was rewritten, so sync
// alone would keep it.
func TestSearchIndexRebuildsAfterGuestAccessNarrows(t *testing.T) {
	f := newGuestFixture(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member_search_access WHERE workspace_id = $1`, testWorkspaceID)
	})
	member := createPlainMember(t, f.marker+"-search@multica.test")
	var memberID string
	dbfx.QueryRow(t, `SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, member).Scan(&memberID)

	memberCursor := searchCursorAs(t, member)
	ownerCursor := searchCursorAs(t, testUserID)
	searchChangesAs(t, member, memberCursor).Want(http.StatusOK)

	demote := withURLParams(newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID,
		UpdateMemberRequest{Role: "guest", ProjectIDs: []string{f.shared}}), "id", testWorkspaceID, "memberId", memberID)
	testutil.Call(t, testHandler.UpdateMember, demote).Want(http.StatusOK)

	searchChangesAs(t, member, memberCursor).Want(http.StatusGone)
	// Nobody else's copy is affected.
	searchChangesAs(t, testUserID, ownerCursor).Want(http.StatusOK)
	// A copy built after the demotion syncs normally.
	rebuilt := searchCursorAs(t, member)
	searchChangesAs(t, member, rebuilt).Want(http.StatusOK)

	// Re-sending the same projects changes nothing.
	path := "/api/workspaces/" + testWorkspaceID + "/members/" + memberID + "/projects"
	setProjects := func(ids ...string) {
		t.Helper()
		req := withURLParams(newRequest(http.MethodPut, path, SetMemberProjectsRequest{ProjectIDs: ids}),
			"id", testWorkspaceID, "memberId", memberID)
		testutil.Call(t, testHandler.SetMemberProjects, req).Want(http.StatusOK)
	}
	setProjects(f.shared)
	searchChangesAs(t, member, rebuilt).Want(http.StatusOK)

	// Sharing another project: its existing issues and comments were not
	// rewritten, so only a rebuild delivers them.
	setProjects(f.shared, f.other)
	searchChangesAs(t, member, rebuilt).Want(http.StatusGone)

	// Taking one away.
	beforeNarrow := searchCursorAs(t, member)
	setProjects(f.shared)
	searchChangesAs(t, member, beforeNarrow).Want(http.StatusGone)

	// Becoming a member: every project and project-less issue is new to it.
	beforePromotion := searchCursorAs(t, member)
	promote := withURLParams(newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID+"/members/"+memberID,
		UpdateMemberRequest{Role: "member"}), "id", testWorkspaceID, "memberId", memberID)
	testutil.Call(t, testHandler.UpdateMember, promote).Want(http.StatusOK)
	searchChangesAs(t, member, beforePromotion).Want(http.StatusGone)
}

// --- Plan and rollout gate on every guest addition --------------------------

// Project member management shares a project with a guest only when guests
// can be added; members, and guests already in the project, are unaffected.
func TestProjectSharingWithGuestFollowsThePlanAndFlag(t *testing.T) {
	f := newGuestFixture(t)
	addTo := func(project string, ids ...string) *testutil.Response {
		req := withURLParam(newRequest(http.MethodPost, "/api/projects/"+project+"/members", map[string]any{"user_ids": ids}), "id", project)
		return testutil.Call(t, testHandler.AddProjectMembers, req)
	}
	update := func(project string, body map[string]any) *testutil.Response {
		req := withURLParam(newRequest(http.MethodPut, "/api/projects/"+project, body), "id", project)
		return testutil.Call(t, testHandler.UpdateProject, req)
	}

	useGuestsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionEnforce})
	wantGuestsLocked(t, addTo(f.other, f.guest))
	wantGuestsLocked(t, update(f.secret, map[string]any{"member_ids": []string{f.guest}}))
	if got := guestProjectIDs(t, f.guest); fmt.Sprint(got) != fmt.Sprint([]string{f.shared}) {
		t.Fatalf("guest projects after refused shares = %v, want only shared", got)
	}
	// The guest's existing project stays, and re-sending it adds nothing.
	addTo(f.shared, f.guest).Want(http.StatusOK)
	// Members are not guests.
	member := createPlainMember(t, f.marker+"-share-member@multica.test")
	update(f.secret, map[string]any{"member_ids": []string{member}}).Want(http.StatusOK)

	// Cloud before the commercial launch: guests are not released yet.
	useGuestsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionOff})
	withFeatureFlag(t, testHandler, featureflags.BillingWorkspaceSubscriptions, false)
	resp := addTo(f.other, f.guest)
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "workspace_guests_disabled") {
		t.Fatalf("share with a guest while guests are off = %d %s, want 403 workspace_guests_disabled", resp.Code, resp.Body.String())
	}
	if got := guestProjectIDs(t, f.guest); fmt.Sprint(got) != fmt.Sprint([]string{f.shared}) {
		t.Fatalf("guest projects with guests off = %v, want only shared", got)
	}
	// The existing guest keeps reading its project.
	if rec := guestRequest(t, testHandler.GetIssue, f.guest, http.MethodGet, "/api/issues/"+f.sharedIssue, nil, "id", f.sharedIssue); rec.Code != http.StatusOK {
		t.Fatalf("existing guest GET shared issue with guests off = %d, want 200", rec.Code)
	}
}

// A guest invitation sent while guests could be added is not accepted after
// the plan or the rollout switch stops allowing them; it stays pending.
func TestGuestInvitationAcceptanceFollowsThePlanAndFlag(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	f := newGuestFixture(t)
	email := f.marker + "-late@multica.test"
	invitee := dbfx.User(t, "Late Guest", email)
	dbfx.Cleanup(t, `DELETE FROM member WHERE user_id = $1`, invitee)

	var inv InvitationResponse
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members",
		CreateMemberRequest{Email: email, Role: "guest", ProjectIDs: []string{f.shared}}), "id", testWorkspaceID)
	testutil.Call(t, testHandler.CreateInvitation, req).Want(http.StatusCreated).JSON(&inv)

	accept := func() *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodPost, "/api/invitations/"+inv.ID+"/accept", nil), invitee), "id", inv.ID)
		return testutil.Call(t, testHandler.AcceptInvitation, req)
	}
	wantPending := func(label string) {
		t.Helper()
		var status string
		dbfx.QueryRow(t, `SELECT status FROM workspace_invitation WHERE id = $1`, inv.ID).Scan(&status)
		if status != "pending" {
			t.Fatalf("%s: invitation status = %q, want pending", label, status)
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, invitee); n != 0 {
			t.Fatalf("%s: invitee became a member", label)
		}
	}

	useGuestsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionEnforce})
	wantGuestsLocked(t, accept())
	wantPending("plan without guests")

	useGuestsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionOff})
	withFeatureFlag(t, testHandler, featureflags.BillingWorkspaceSubscriptions, false)
	if resp := accept(); resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "workspace_guests_disabled") {
		t.Fatalf("accept with guests off = %d %s, want 403 workspace_guests_disabled", resp.Code, resp.Body.String())
	}
	wantPending("guests off")

	useGuestsFeature(t, entitlement.FeatureDecision{Action: entitlement.ActionObserve})
	accept().Want(http.StatusOK)
	if got := guestAttempts(t); got.observed != 1 || got.lastOperation != "accept_invitation" {
		t.Fatalf("guest attempts after an observed acceptance = %+v, want one observed accept_invitation", got)
	}
}

// --- Concurrent role changes -----------------------------------------------

// raceRoleChange holds the target's member row locked, starts the request,
// waits until the request blocks on that lock (it has read the target's
// role by then), commits the concurrent change, and returns the response.
func raceRoleChange(t *testing.T, memberID, concurrentRole string, request func() *testutil.Response) *testutil.Response {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM member WHERE id = $1 FOR UPDATE`, memberID); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var resp *testutil.Response
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp = request()
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !blockedOnMember(t, memberID) {
		if time.Now().After(deadline) {
			t.Fatal("request never blocked on the member row")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := tx.Exec(ctx, `UPDATE member SET role = $2 WHERE id = $1`, memberID, concurrentRole); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	return resp
}

// blockedOnMember reports whether another backend waits for the lock on
// this member's row.
func blockedOnMember(t *testing.T, memberID string) bool {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM pg_stat_activity a
		WHERE a.wait_event_type = 'Lock' AND a.pid <> pg_backend_pid()
		  AND a.query ILIKE '%member%'
		  AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid = a.pid AND NOT l.granted)`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func wantRoleChanged(t *testing.T, resp *testutil.Response) {
	t.Helper()
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "member_role_changed") {
		t.Fatalf("stale role change = %d %s, want 409 member_role_changed", resp.Code, resp.Body.String())
	}
}

func memberRole(t *testing.T, memberID string) string {
	t.Helper()
	var role string
	dbfx.QueryRow(t, `SELECT role FROM member WHERE id = $1`, memberID).Scan(&role)
	return role
}

// A request decided on "member" must not write over a demotion that
// committed first: making the guest an admin that way would skip the seat.
func TestRoleChangeDecidedOnStaleRoleIsRefused(t *testing.T) {
	f := newGuestFixture(t)
	member := createPlainMember(t, f.marker+"-race@multica.test")
	var memberID string
	dbfx.QueryRow(t, `SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, member).Scan(&memberID)
	path := "/api/workspaces/" + testWorkspaceID + "/members/" + memberID
	patch := func(body UpdateMemberRequest) func() *testutil.Response {
		return func() *testutil.Response {
			req := withURLParams(newRequest(http.MethodPatch, path, body), "id", testWorkspaceID, "memberId", memberID)
			return testutil.Call(t, testHandler.UpdateMember, req)
		}
	}

	// member -> admin, while a demotion to guest commits first.
	wantRoleChanged(t, raceRoleChange(t, memberID, "guest", patch(UpdateMemberRequest{Role: "admin"})))
	if role := memberRole(t, memberID); role != "guest" {
		t.Fatalf("role = %q, want the committed guest", role)
	}

	// guest -> member, while another promotion commits first: the stale
	// request must not take a second seat or rewrite the role.
	wantRoleChanged(t, raceRoleChange(t, memberID, "member", patch(UpdateMemberRequest{Role: "member"})))
	if role := memberRole(t, memberID); role != "member" {
		t.Fatalf("role = %q, want the committed member", role)
	}

	// member -> guest, while a promotion to admin commits first.
	wantRoleChanged(t, raceRoleChange(t, memberID, "admin",
		patch(UpdateMemberRequest{Role: "guest", ProjectIDs: []string{f.shared}})))
	if role := memberRole(t, memberID); role != "admin" {
		t.Fatalf("role = %q, want the committed admin", role)
	}

	// Setting a guest's projects after it was made a member leaves the
	// member's projects alone.
	dbfx.Exec(t, `UPDATE member SET role = 'guest' WHERE id = $1`, memberID)
	addProjectMemberRow(t, f.other, member)
	setProjects := func() *testutil.Response {
		req := withURLParams(newRequest(http.MethodPut, path+"/projects", SetMemberProjectsRequest{ProjectIDs: []string{f.shared}}),
			"id", testWorkspaceID, "memberId", memberID)
		return testutil.Call(t, testHandler.SetMemberProjects, req)
	}
	wantRoleChanged(t, raceRoleChange(t, memberID, "member", setProjects))
	if got := guestProjectIDs(t, member); fmt.Sprint(got) != fmt.Sprint([]string{f.other}) {
		t.Fatalf("member projects = %v, want untouched", got)
	}
}

// An unbound draft joins only its uploader's comment. A guest's file taken
// off its comment cannot be adopted by another guest (or a member) who
// learned its id, on create or on edit.
func TestDraftAttachmentJoinsOnlyItsUploadersComment(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = origStorage })
	f := newGuestFixture(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE uploader_id = $1`, f.guest)
	})
	other := createPlainMember(t, f.marker+"-other-guest@multica.test")
	dbfx.Exec(t, `UPDATE member SET role = 'guest' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, other)
	addProjectMemberRow(t, f.other, other)

	// The guest attaches a file and takes it off again: an unbound draft.
	draft := uploadDraftToIssue(t, f.guest, f.sharedIssue)
	var own CommentResponse
	guestRequest(t, testHandler.CreateComment, f.guest, http.MethodPost, "/api/issues/"+f.sharedIssue+"/comments",
		CreateCommentRequest{Content: "with a file", AttachmentIDs: []string{draft.ID}}, "id", f.sharedIssue).
		Want(http.StatusCreated).JSON(&own)
	guestRequest(t, testHandler.UpdateComment, f.guest, http.MethodPut, "/api/comments/"+own.ID,
		map[string]any{"content": "file removed", "attachment_ids": []string{}}, "commentId", own.ID).Want(http.StatusOK)

	wantUnbound := func(label string) {
		t.Helper()
		if issueID, commentID := attachmentBinding(t, draft.ID); issueID != nil || commentID != nil {
			t.Fatalf("%s: draft bound to issue %v comment %v, want still unbound", label, issueID, commentID)
		}
	}
	for _, adopter := range []string{other, testUserID} {
		issue := f.otherIssue
		guestRequest(t, testHandler.CreateComment, adopter, http.MethodPost, "/api/issues/"+issue+"/comments",
			CreateCommentRequest{Content: "adopting", AttachmentIDs: []string{draft.ID}}, "id", issue).
			Want(http.StatusConflict)
		wantUnbound("create by another user")

		var theirs CommentResponse
		guestRequest(t, testHandler.CreateComment, adopter, http.MethodPost, "/api/issues/"+issue+"/comments",
			CreateCommentRequest{Content: "plain"}, "id", issue).Want(http.StatusCreated).JSON(&theirs)
		guestRequest(t, testHandler.UpdateComment, adopter, http.MethodPut, "/api/comments/"+theirs.ID,
			map[string]any{"content": "adopting on edit", "attachment_ids": []string{draft.ID}}, "commentId", theirs.ID).
			Want(http.StatusConflict)
		wantUnbound("edit by another user")
	}

	// Its uploader can still use it.
	guestRequest(t, testHandler.UpdateComment, f.guest, http.MethodPut, "/api/comments/"+own.ID,
		map[string]any{"content": "file back", "attachment_ids": []string{draft.ID}}, "commentId", own.ID).Want(http.StatusOK)
	if _, commentID := attachmentBinding(t, draft.ID); commentID == nil || *commentID != own.ID {
		t.Fatalf("draft comment = %v, want back on its uploader's comment", commentID)
	}
}

// When a comment published with a draft wins the race, an edit asking for
// the same draft is refused and rolled back, not saved without the file.
func TestCommentEditLosingDraftToPublishIsRefused(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = origStorage })
	f := newGuestFixture(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE uploader_id = $1`, f.guest)
	})
	var existing CommentResponse
	guestRequest(t, testHandler.CreateComment, f.guest, http.MethodPost, "/api/issues/"+f.sharedIssue+"/comments",
		CreateCommentRequest{Content: "before the edit"}, "id", f.sharedIssue).Want(http.StatusCreated).JSON(&existing)
	draft := uploadDraftToIssue(t, f.guest, f.sharedIssue)

	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM issue WHERE id = $1 FOR UPDATE`, f.sharedIssue); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var create, edit *testutil.Response
	wg.Add(2)
	go func() {
		defer wg.Done()
		create = guestRequest(t, testHandler.CreateComment, f.guest, http.MethodPost, "/api/issues/"+f.sharedIssue+"/comments",
			CreateCommentRequest{Content: "published with the draft", AttachmentIDs: []string{draft.ID}}, "id", f.sharedIssue)
	}()
	waitForLockWaiters(t, 1) // the publish holds the draft and waits for the issue
	go func() {
		defer wg.Done()
		edit = guestRequest(t, testHandler.UpdateComment, f.guest, http.MethodPut, "/api/comments/"+existing.ID,
			map[string]any{"content": "edited with the draft", "attachment_ids": []string{draft.ID}}, "commentId", existing.ID)
	}()
	waitForLockWaiters(t, 2) // the edit waits for the draft
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d %s, want 201: it queued first", create.Code, create.Body.String())
	}
	if edit.Code != http.StatusConflict {
		t.Fatalf("edit = %d %s, want 409 for a draft the publish already took", edit.Code, edit.Body.String())
	}
	var content string
	dbfx.QueryRow(t, `SELECT content FROM comment WHERE id = $1`, existing.ID).Scan(&content)
	if content != "before the edit" {
		t.Fatalf("edited comment content = %q, want the refused edit rolled back", content)
	}
}

// An installed client edits without expected_revision or content_base. When
// such an edit also sets attachments and one is no longer available, it is
// refused as a whole: the runs queued from the old body stay queued, and the
// request does not hang on its own transaction's issue lock.
func TestLegacyCommentEditWithUnavailableAttachmentKeepsQueuedRun(t *testing.T) {
	f := newGuestFixture(t)
	runtime := dbfx.Runtime(t, f.marker+" legacy runtime")
	agent := dbfx.Agent(t, f.marker+" legacy agent", runtime)
	comment := dbfx.Comment(t, f.sharedIssue, "please look", testutil.Cols{"author_type": "member", "author_id": testUserID})
	task := dbfx.Task(t, agent, testutil.Cols{
		"issue_id": f.sharedIssue, "runtime_id": runtime, "trigger_comment_id": comment,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := withURLParam(newRequest(http.MethodPut, "/api/comments/"+comment,
		map[string]any{"content": "please look again", "attachment_ids": []string{uuid.NewString()}}), "commentId", comment)
	resp := testutil.Call(t, testHandler.UpdateComment, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, chi.RouteContext(req.Context()))))
	if ctx.Err() != nil {
		t.Fatalf("legacy edit did not finish within 10s (status %d)", resp.Code)
	}
	if resp.Code != http.StatusConflict {
		t.Fatalf("legacy edit with an unavailable attachment = %d %s, want 409", resp.Code, resp.Body.String())
	}
	var status, content string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, task).Scan(&status)
	if status != "queued" {
		t.Fatalf("run status after the refused edit = %q, want still queued", status)
	}
	dbfx.QueryRow(t, `SELECT content FROM comment WHERE id = $1`, comment).Scan(&content)
	if content != "please look" {
		t.Fatalf("comment content = %q, want unchanged", content)
	}
}
