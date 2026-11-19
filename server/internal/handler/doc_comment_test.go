package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestDocCommentsDiscussNotifyAndResolve(t *testing.T) {
	doc := createDocForTest(t)
	t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM doc_comment WHERE doc_id = $1", doc.ID) })
	stamp := time.Now().UnixNano()
	other := dbfx.User(t, "Li Ran", fmt.Sprintf("li-ran-%d@example.test", stamp))
	dbfx.Member(t, testWorkspaceID, other, "member")
	third := dbfx.User(t, "Wang Xue", fmt.Sprintf("wang-xue-%d@example.test", stamp))
	dbfx.Member(t, testWorkspaceID, third, "member")
	guest := dbfx.User(t, "Zhou Kai", fmt.Sprintf("zhou-kai-%d@example.test", stamp))
	dbfx.Member(t, testWorkspaceID, guest, "guest")
	outsider := dbfx.User(t, "Chen Yu", fmt.Sprintf("chen-yu-%d@example.test", stamp))
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM inbox_item WHERE recipient_id = ANY($1::uuid[])", []string{other, third, guest, outsider, testUserID})
	})

	comments := func() []DocCommentResponse {
		t.Helper()
		var out struct {
			Comments []DocCommentResponse `json:"comments"`
		}
		testutil.Call(t, testHandler.ListDocComments, withURLParam(newRequest("GET", "/comments", nil), "id", doc.ID)).Want(http.StatusOK).JSON(&out)
		return out.Comments
	}
	post := func(body map[string]any, as string) *http.Request {
		req := withURLParam(newRequest("POST", "/comments", body), "id", doc.ID)
		if as != "" {
			req.Header.Set("X-User-ID", as)
		}
		return req
	}
	inboxFor := func(user, kind string) int {
		return dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE recipient_id = $1 AND type = $2 AND details->>'doc_id' = $3", user, kind, doc.ID)
	}

	// A discussion needs the text it is about.
	testutil.Call(t, testHandler.CreateDocComment, post(map[string]any{"content": "Why?"}, "")).Want(http.StatusBadRequest)

	var root DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, post(map[string]any{
		"content":       "Can index-only migrations rehearse the same day? [@Li Ran](mention://member/" + other + ") " +
			"[@Zhou Kai](mention://member/" + guest + ") [@Chen Yu](mention://member/" + outsider + ")",
		"anchor_quote":  "Original",
		"anchor_prefix": "# First\n\n",
		"anchor_suffix": "",
	}, "")).Want(http.StatusCreated).JSON(&root)
	if root.ParentID != nil || root.AnchorQuote != "Original" || root.AuthorID != testUserID {
		t.Fatalf("root comment: %+v", root)
	}
	if inboxFor(other, "doc_mentioned") != 1 || inboxFor(testUserID, "doc_mentioned")+inboxFor(testUserID, "doc_comment") != 0 {
		t.Fatal("a mention notifies the mentioned member, never the author")
	}
	// Guests cannot open docs and outsiders are not in the workspace; neither
	// may read the doc's text in their inbox.
	if inboxFor(guest, "doc_mentioned")+inboxFor(outsider, "doc_mentioned") != 0 {
		t.Fatal("a mention must not notify a guest or a non-member")
	}

	// A reply joins the discussion; answering a reply still lands on the root.
	var reply DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, post(map[string]any{"content": "Yes, with CONCURRENTLY.", "parent_id": root.ID}, other)).Want(http.StatusCreated).JSON(&reply)
	var nested DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, post(map[string]any{"content": "Thanks", "parent_id": reply.ID}, third)).Want(http.StatusCreated).JSON(&nested)
	if reply.ParentID == nil || *reply.ParentID != root.ID || nested.ParentID == nil || *nested.ParentID != root.ID {
		t.Fatalf("replies must hang off the root: %+v %+v", reply, nested)
	}
	// Participants hear about later replies; the third member was neither
	// mentioned nor in the discussion when the first reply came.
	if inboxFor(testUserID, "doc_comment") != 2 || inboxFor(other, "doc_comment") != 1 || inboxFor(third, "doc_comment") != 0 {
		t.Fatalf("participant notifications: author=%d other=%d third=%d",
			inboxFor(testUserID, "doc_comment"), inboxFor(other, "doc_comment"), inboxFor(third, "doc_comment"))
	}
	if got := comments(); len(got) != 3 || got[0].ID != root.ID {
		t.Fatalf("list: %+v", got)
	}

	// Anyone can resolve and reopen; replies cannot be resolved.
	resolve := func(id, action string, as string) *http.Request {
		req := withURLParams(newRequest("POST", "/"+action, nil), "id", doc.ID, "docCommentId", id)
		if as != "" {
			req.Header.Set("X-User-ID", as)
		}
		return req
	}
	var resolved DocCommentResponse
	testutil.Call(t, testHandler.ResolveDocComment, resolve(root.ID, "resolve", third)).Want(http.StatusOK).JSON(&resolved)
	if resolved.ResolvedAt == nil || resolved.ResolvedByID == nil || *resolved.ResolvedByID != third {
		t.Fatalf("resolve: %+v", resolved)
	}
	testutil.Call(t, testHandler.ResolveDocComment, resolve(reply.ID, "resolve", "")).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ReopenDocComment, resolve(root.ID, "reopen", "")).Want(http.StatusOK).JSON(&resolved)
	if resolved.ResolvedAt != nil {
		t.Fatalf("reopen: %+v", resolved)
	}

	// Only the author deletes; deleting the root removes the discussion.
	testutil.Call(t, testHandler.DeleteDocComment, withURLParams(newRequest("DELETE", "/", nil), "id", doc.ID, "docCommentId", root.ID)).Want(http.StatusNoContent)
	if got := comments(); len(got) != 0 {
		t.Fatalf("deleting the root must remove replies: %+v", got)
	}
	var again DocCommentResponse
	testutil.Call(t, testHandler.CreateDocComment, post(map[string]any{"content": "x", "anchor_quote": "Original"}, other)).Want(http.StatusCreated).JSON(&again)
	testutil.Call(t, testHandler.DeleteDocComment, withURLParams(newRequest("DELETE", "/", nil), "id", doc.ID, "docCommentId", again.ID)).Want(http.StatusForbidden)

	// Deleting the doc removes its discussions.
	testutil.Call(t, testHandler.DeleteDoc, withURLParam(newRequest("DELETE", "/api/docs", nil), "id", doc.ID)).Want(http.StatusNoContent)
	if n := dbfx.Count(t, "SELECT count(*) FROM doc_comment WHERE doc_id = $1", doc.ID); n != 0 {
		t.Fatalf("doc delete left %d comments", n)
	}
}
