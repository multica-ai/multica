package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestCommentEmit_StatusChangeFiresCommentCreated locks the team-app integration
// contract: posting a status_change comment via Handler.CreateComment must
// publish exactly one protocol.EventCommentCreated event with the comment
// payload carrying type "status_change". The standalone team-app subscriber
// depends on this event firing under this exact name. Story M-PR#2.
func TestCommentEmit_StatusChangeFiresCommentCreated(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialised; DATABASE_URL likely unset")
	}

	// Create the issue first so the issue:created event does not pollute the
	// captured bus. (We subscribe only to comment:created below, but creating
	// before the swap also keeps the handler test fixture independent of any
	// listeners we register.)
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "comment_emit_test issue",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		cleanup := newRequest(http.MethodDelete, "/api/issues/"+issue.ID, nil)
		cleanup = withURLParam(cleanup, "id", issue.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanup)
	})

	// Replace the handler bus with a captured one for the duration of this test.
	var (
		mu       sync.Mutex
		captured []events.Event
	)
	bus := events.New()
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, e)
	})
	origBus := testHandler.Bus
	testHandler.Bus = bus
	t.Cleanup(func() { testHandler.Bus = origBus })

	// Post a status_change comment.
	w = httptest.NewRecorder()
	req = newRequest(http.MethodPost, "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content": "moved to in_progress",
		"type":    "status_change",
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("expected exactly 1 comment:created event, got %d", len(captured))
	}
	evt := captured[0]
	if evt.Type != protocol.EventCommentCreated {
		t.Fatalf("event type: expected %q, got %q", protocol.EventCommentCreated, evt.Type)
	}
	if evt.WorkspaceID != testWorkspaceID {
		t.Fatalf("event workspace_id: expected %q, got %q", testWorkspaceID, evt.WorkspaceID)
	}

	payload, ok := evt.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload: expected map[string]any, got %T", evt.Payload)
	}
	commentVal, ok := payload["comment"]
	if !ok {
		t.Fatalf("payload missing %q key: %#v", "comment", payload)
	}
	comment, ok := commentVal.(CommentResponse)
	if !ok {
		t.Fatalf("payload[\"comment\"]: expected CommentResponse, got %T", commentVal)
	}
	if comment.Type != "status_change" {
		t.Fatalf("comment.Type: expected %q, got %q", "status_change", comment.Type)
	}
	if comment.IssueID != issue.ID {
		t.Fatalf("comment.IssueID: expected %q, got %q", issue.ID, comment.IssueID)
	}
}
