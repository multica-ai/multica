package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func pinRequest(t *testing.T, h *Handler, id string, pinned bool) CommentResponse {
	t.Helper()
	method, handler := http.MethodPost, h.PinComment
	if !pinned {
		method, handler = http.MethodDelete, h.UnpinComment
	}
	var out CommentResponse
	testutil.Call(t, handler, withURLParam(newRequest(method, "/api/comments/"+id+"/pin", nil), "commentId", id)).Want(http.StatusOK).JSON(&out)
	return out
}

func TestPinCommentProjectionDeletionAndEvents(t *testing.T) {
	if testPool == nil {
		t.Fatal("database required")
	}
	issue := dbfx.Issue(t, "pin projection")
	root := dbfx.Comment(t, issue, "root")
	dbfx.Comment(t, issue, "reply", testutil.Cols{"parent_id": root})
	h := *testHandler
	h.Bus = events.New()
	var received []events.Event
	h.Bus.Subscribe(protocol.EventCommentPinned, func(e events.Event) { received = append(received, e) })
	h.Bus.Subscribe(protocol.EventCommentUnpinned, func(e events.Event) { received = append(received, e) })
	first := pinRequest(t, &h, root, true)
	pinRequest(t, &h, root, true)
	if len(received) != 1 || received[0].WorkspaceID != testWorkspaceID || received[0].ActorID != testUserID {
		t.Fatalf("bad event routing: %+v", received)
	}
	payload := received[0].Payload.(map[string]any)["comment"].(CommentResponse)
	if payload.PinnedAt == nil || payload.Content != "root" || payload.IssueID != issue {
		t.Fatalf("bad event payload: %+v", payload)
	}
	var edited CommentResponse
	testutil.Call(t, h.UpdateComment, withURLParam(newRequest(http.MethodPut, "/api/comments/"+root, map[string]any{"content": "edited"}), "commentId", root)).Want(http.StatusOK).JSON(&edited)
	if edited.PinnedAt == nil || *edited.PinnedAt != *first.PinnedAt {
		t.Fatalf("edit lost pin: %+v", edited)
	}
	var timeline []TimelineEntry
	testutil.Call(t, h.ListTimeline, withURLParam(newRequest(http.MethodGet, "/api/issues/"+issue+"/timeline", nil), "id", issue)).Want(http.StatusOK).JSON(&timeline)
	found := false
	for _, entry := range timeline {
		if entry.ID == root {
			found = true
			if entry.PinnedAt == nil || *entry.PinnedAt != *first.PinnedAt {
				t.Fatalf("timeline lost pin: %+v", entry)
			}
		}
	}
	if !found {
		t.Fatal("missing timeline comment")
	}
	for _, query := range []string{"", "?roots_only=true", "?thread=" + root} {
		var comments []CommentResponse
		testutil.Call(t, h.ListComments, withURLParam(newRequest(http.MethodGet, "/api/issues/"+issue+"/comments"+query, nil), "id", issue)).Want(http.StatusOK).JSON(&comments)
		for _, c := range comments {
			if c.ID == root && c.PinnedAt == nil {
				t.Fatalf("projection %s lost pin", query)
			}
		}
	}
	testutil.Call(t, h.DeleteComment, withURLParam(newRequest(http.MethodDelete, "/api/comments/"+root, nil), "commentId", root)).Want(http.StatusNoContent)
	var pinned bool
	if err := testPool.QueryRow(context.Background(), "SELECT pinned_at IS NOT NULL OR pinned_by_type IS NOT NULL OR pinned_by_id IS NOT NULL FROM comment WHERE id=$1", root).Scan(&pinned); err != nil || pinned {
		t.Fatalf("tombstone retained pin: %v %v", pinned, err)
	}
	for _, handler := range []http.HandlerFunc{h.PinComment, h.UnpinComment} {
		testutil.Call(t, handler, withURLParam(newRequest(http.MethodPost, "/api/comments/"+root+"/pin", nil), "commentId", root)).Want(http.StatusNotFound)
	}
}

func TestPinCommentAccess(t *testing.T) {
	if testPool == nil {
		t.Fatal("database required")
	}
	f := newPrivateProjectFixture(t)
	for _, handler := range []http.HandlerFunc{testHandler.PinComment, testHandler.UnpinComment} {
		req := withURLParam(asUser(newRequest(http.MethodPost, "/api/comments/"+f.secretComment+"/pin", nil), f.outsider), "commentId", f.secretComment)
		testutil.Call(t, handler, req).Want(http.StatusNotFound)
		req = withURLParam(newRequest(http.MethodPost, "/api/comments/"+f.secretComment+"/pin", nil), "commentId", f.secretComment)
		req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000001")
		testutil.Call(t, handler, req).Want(http.StatusNotFound)
	}
}

func TestPinCommentAgentActor(t *testing.T) {
	if testPool == nil {
		t.Fatal("database required")
	}
	issue := dbfx.Issue(t, "agent pin")
	id := dbfx.Comment(t, issue, "root")
	agent := dbfx.Agent(t, "pin actor", dbfx.Runtime(t, "pin actor runtime"))
	task := createHandlerTestTaskForAgentOnIssue(t, agent, issue)
	req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+id+"/pin", nil), "commentId", id)
	req.Header.Set("X-Agent-ID", agent)
	req.Header.Set("X-Task-ID", task)
	var out CommentResponse
	testutil.Call(t, testHandler.PinComment, req).Want(http.StatusOK).JSON(&out)
	if out.PinnedByType == nil || *out.PinnedByType != "agent" || out.PinnedByID == nil || *out.PinnedByID != agent {
		t.Fatalf("wrong agent pin actor: %+v", out)
	}
}

func TestPinCommentConcurrentIdempotency(t *testing.T) {
	if testPool == nil {
		t.Fatal("database required")
	}
	issue := dbfx.Issue(t, "concurrent pin")
	id := dbfx.Comment(t, issue, "root")
	h := *testHandler
	h.Bus = events.New()
	var mu sync.Mutex
	count := 0
	h.Bus.Subscribe(protocol.EventCommentPinned, func(e events.Event) { mu.Lock(); count++; mu.Unlock() })
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() { defer wg.Done(); pinRequest(t, &h, id, true) }()
	}
	wg.Wait()
	if count != 1 {
		t.Fatalf("concurrent no-ops emitted %d events", count)
	}
	pinRequest(t, &h, id, false)
}

func TestPinCommentRootAndReply(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Fatal("database required for pin tests")
	}
	issueID := dbfx.Issue(t, "pin navigation")
	root := dbfx.Comment(t, issueID, "root")
	reply := dbfx.Comment(t, issueID, "reply", testutil.Cols{"parent_id": root})
	for _, id := range []string{root, reply} {
		t.Run(id, func(t *testing.T) {
			request := func(method string, handler http.HandlerFunc) CommentResponse {
				var out CommentResponse
				testutil.Call(t, handler, withURLParam(newRequest(method, "/api/comments/"+id+"/pin", nil), "commentId", id)).Want(http.StatusOK).JSON(&out)
				return out
			}
			first := request(http.MethodPost, testHandler.PinComment)
			if first.PinnedAt == nil || first.PinnedByID == nil || *first.PinnedByID != testUserID {
				t.Fatalf("missing pin actor: %+v", first)
			}
			again := request(http.MethodPost, testHandler.PinComment)
			if again.Revision != first.Revision || *again.PinnedAt != *first.PinnedAt || again.UpdatedAt != first.UpdatedAt {
				t.Fatal("pin is not idempotent")
			}
			unpinned := request(http.MethodDelete, testHandler.UnpinComment)
			if unpinned.PinnedAt != nil || unpinned.PinnedByID != nil || unpinned.Revision != first.Revision+1 || unpinned.UpdatedAt != first.UpdatedAt {
				t.Fatalf("bad unpin: %+v", unpinned)
			}
			noop := request(http.MethodDelete, testHandler.UnpinComment)
			if noop.Revision != unpinned.Revision {
				t.Fatal("unpin no-op bumped revision")
			}
		})
	}
}

func TestPinCommentLimitPerIssue(t *testing.T) {
	if testPool == nil {
		t.Fatal("database required")
	}
	issue := dbfx.Issue(t, "pin limit")
	ids := make([]string, MaxPinnedCommentsPerIssue+2)
	for i := range ids {
		ids[i] = dbfx.Comment(t, issue, "comment")
	}
	for _, id := range ids[:MaxPinnedCommentsPerIssue] {
		pinRequest(t, testHandler, id, true)
	}
	over := ids[MaxPinnedCommentsPerIssue]
	req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+over+"/pin", nil), "commentId", over)
	var body map[string]string
	testutil.Call(t, testHandler.PinComment, req).Want(http.StatusConflict).JSON(&body)
	if body["code"] != "pin_limit_reached" {
		t.Fatalf("limit code = %q", body["code"])
	}
	// Re-pinning an already pinned comment at the limit stays a no-op success.
	pinRequest(t, testHandler, ids[0], true)
	// Freeing a slot lets the next pin through.
	pinRequest(t, testHandler, ids[0], false)
	if got := pinRequest(t, testHandler, over, true); got.PinnedAt == nil {
		t.Fatal("pin after freeing a slot was not applied")
	}
	// A different issue has its own budget.
	other := dbfx.Comment(t, dbfx.Issue(t, "other pins"), "comment")
	if got := pinRequest(t, testHandler, other, true); got.PinnedAt == nil {
		t.Fatal("limit leaked across issues")
	}
}

func TestPinCommentLimitUnderConcurrency(t *testing.T) {
	if testPool == nil {
		t.Fatal("database required")
	}
	issue := dbfx.Issue(t, "concurrent pin limit")
	ids := make([]string, MaxPinnedCommentsPerIssue*2)
	for i := range ids {
		ids[i] = dbfx.Comment(t, issue, "comment")
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+id+"/pin", nil), "commentId", id)
			testHandler.PinComment(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()
	var n int
	if err := testPool.QueryRow(context.Background(), "SELECT count(*) FROM comment WHERE issue_id = $1 AND pinned_at IS NOT NULL", issue).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != MaxPinnedCommentsPerIssue {
		t.Fatalf("concurrent pins left %d pinned, want %d", n, MaxPinnedCommentsPerIssue)
	}
}
