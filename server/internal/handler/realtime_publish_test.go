package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestMutationsPublishWSEvents (PUL-40) is the contract test that pins the
// publish-to-bus chain for the events the realtime UI relies on. The
// listener layer (server/cmd/server/listeners.go) turns each bus event into a
// `BroadcastToWorkspace` fanout, and the frontend (use-realtime-sync.ts)
// turns each fanout into a TanStack Query invalidation. Removing a `h.publish`
// call anywhere in this chain silently breaks "this page shows new content
// without a manual refresh" — the symptom in PUL-40.
//
// This test does NOT cover the listener fanout (see listeners_scope_test.go)
// or the WS upgrade / connection lifecycle. It pins the layer where the
// chain most often regresses: a mutation handler that forgets to publish.
func TestMutationsPublishWSEvents(t *testing.T) {
	ctx := context.Background()

	// Subscribe to the test handler's bus before each subtest. We use a
	// buffered channel size 1: only the first matching event matters; later
	// events drop without blocking the publisher.
	subscribe := func(eventType string) <-chan events.Event {
		ch := make(chan events.Event, 1)
		testHandler.Bus.Subscribe(eventType, func(e events.Event) {
			select {
			case ch <- e:
			default:
				// Channel full — already captured the first event for this
				// subtest. Subsequent events dropped to avoid blocking the
				// publisher. This is fine because tests assert the FIRST
				// event matches; the bus has no Unsubscribe so this listener
				// stays for the rest of the test process.
			}
		})
		return ch
	}

	expect := func(t *testing.T, ch <-chan events.Event, wantType, wantWorkspaceID string) events.Event {
		t.Helper()
		select {
		case e := <-ch:
			if e.Type != wantType {
				t.Errorf("event type: got %q, want %q", e.Type, wantType)
			}
			if e.WorkspaceID != wantWorkspaceID {
				t.Errorf("event workspace_id: got %q, want %q", e.WorkspaceID, wantWorkspaceID)
			}
			if e.Payload == nil {
				t.Errorf("event payload: nil (frontend handlers expect a payload)")
			}
			return e
		case <-time.After(2 * time.Second):
			t.Fatalf("no %q event published within 2s — handler likely missing h.publish() call (PUL-40 regression)", wantType)
			return events.Event{}
		}
	}

	t.Run("CreateIssue publishes issue:created", func(t *testing.T) {
		ch := subscribe(protocol.EventIssueCreated)

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":    "PUL-40 publish-test issue",
			"status":   "todo",
			"priority": "medium",
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var created IssueResponse
		_ = json.Unmarshal(w.Body.Bytes(), &created)
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
		})

		expect(t, ch, protocol.EventIssueCreated, testWorkspaceID)
	})

	t.Run("UpdateIssue (status change) publishes issue:updated", func(t *testing.T) {
		// Set up an issue to update.
		var issueID string
		err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number)
			VALUES ($1, 'PUL-40 update target', 'todo', 'medium', $2, 'member', 91000)
			RETURNING id
		`, testWorkspaceID, testUserID).Scan(&issueID)
		if err != nil {
			t.Fatalf("setup: insert issue: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		})

		ch := subscribe(protocol.EventIssueUpdated)

		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "in_progress"})
		req = withURLParam(req, "id", issueID)
		testHandler.UpdateIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		expect(t, ch, protocol.EventIssueUpdated, testWorkspaceID)
	})

	t.Run("CreateComment publishes comment:created with issue_id in payload", func(t *testing.T) {
		// Set up an issue for the comment.
		var issueID string
		err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number)
			VALUES ($1, 'PUL-40 comment target', 'todo', 'medium', $2, 'member', 91001)
			RETURNING id
		`, testWorkspaceID, testUserID).Scan(&issueID)
		if err != nil {
			t.Fatalf("setup: insert issue: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		})

		ch := subscribe(protocol.EventCommentCreated)

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
			"content": "PUL-40 comment text",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.CreateComment(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
		}

		e := expect(t, ch, protocol.EventCommentCreated, testWorkspaceID)

		// The frontend handler in use-realtime-sync.ts:340 does
		// `if (comment?.issue_id) invalidateTimeline(comment.issue_id)`.
		// If the publish payload loses the comment.issue_id field, the
		// timeline invalidation silently no-ops and the bug returns. Pin
		// the payload shape here so it can't regress.
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			t.Fatalf("comment:created payload: expected map, got %T", e.Payload)
		}
		comment, ok := payload["comment"].(CommentResponse)
		if !ok {
			t.Fatalf("comment:created payload.comment: expected CommentResponse, got %T", payload["comment"])
		}
		if comment.IssueID != issueID {
			t.Errorf("comment:created payload.comment.issue_id: got %q, want %q", comment.IssueID, issueID)
		}
	})
}

