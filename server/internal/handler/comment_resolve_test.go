package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// resolveCommentHTTP drives the POST /api/comments/{id}/resolve handler and
// returns the decoded response. Mirrors the resolve path the web/desktop client
// hits.
func resolveCommentHTTP(t *testing.T, commentID string) CommentResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/comments/"+commentID+"/resolve", nil)
	r = withURLParam(r, "commentId", commentID)
	testHandler.ResolveComment(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve %s: status %d: %s", commentID, w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resolve response: %v", err)
	}
	return resp
}

// commentResolved reports whether a comment currently has resolved_at set,
// read straight from the row so the assertion sees committed state.
func commentResolved(t *testing.T, id string) bool {
	t.Helper()
	var resolvedAt *time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT resolved_at FROM comment WHERE id = $1`, id,
	).Scan(&resolvedAt); err != nil {
		t.Fatalf("query resolved_at for %s: %v", id, err)
	}
	return resolvedAt != nil
}

// commentResolvedAt returns the raw resolved_at timestamp (nil when cleared).
func commentResolvedAt(t *testing.T, id string) *time.Time {
	t.Helper()
	var resolvedAt *time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT resolved_at FROM comment WHERE id = $1`, id,
	).Scan(&resolvedAt); err != nil {
		t.Fatalf("query resolved_at for %s: %v", id, err)
	}
	return resolvedAt
}

// commentEventCapture records comment:resolved / comment:unresolved events for a
// single issue. The handler bus has no Unsubscribe, so the closure filters by
// issue id; events for other tests' issues are ignored.
type commentEventCapture struct {
	mu     sync.Mutex
	events []struct {
		Type      string
		CommentID string
	}
}

func captureCommentEvents(t *testing.T, issueID string) *commentEventCapture {
	t.Helper()
	cap := &commentEventCapture{}
	record := func(e events.Event) {
		m, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		c, ok := m["comment"].(CommentResponse)
		if !ok || c.IssueID != issueID {
			return
		}
		cap.mu.Lock()
		cap.events = append(cap.events, struct {
			Type      string
			CommentID string
		}{e.Type, c.ID})
		cap.mu.Unlock()
	}
	testHandler.Bus.Subscribe(protocol.EventCommentResolved, record)
	testHandler.Bus.Subscribe(protocol.EventCommentUnresolved, record)
	return cap
}

func (c *commentEventCapture) countFor(eventType, commentID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if e.Type == eventType && e.CommentID == commentID {
			n++
		}
	}
	return n
}

// resolveTestFixture seeds an issue with two independent threads so the tests
// can prove the single-resolution invariant is thread-scoped, not issue-scoped:
//
//	root1
//	├── a1
//	└── b1
//	root2 (separate thread)
//	└── a2
type resolveTestFixture struct {
	IssueID string
	Root1   string
	A1      string
	B1      string
	Root2   string
	A2      string
}

func newResolveTestFixture(t *testing.T) resolveTestFixture {
	t.Helper()
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, $3)
		RETURNING id
	`, testWorkspaceID, testUserID, "resolve fixture").Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	base := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	insert := func(parent *string, offset time.Duration, body string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $6)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, body, parent, base.Add(offset)).Scan(&id); err != nil {
			t.Fatalf("insert comment %q: %v", body, err)
		}
		return id
	}

	root1 := insert(nil, 0, "root1")
	a1 := insert(&root1, 1*time.Minute, "a1")
	b1 := insert(&root1, 2*time.Minute, "b1")
	root2 := insert(nil, 10*time.Minute, "root2")
	a2 := insert(&root2, 11*time.Minute, "a2")

	return resolveTestFixture{IssueID: issueID, Root1: root1, A1: a1, B1: b1, Root2: root2, A2: a2}
}

// TestResolveComment_ReplacesPriorThreadResolution is the core regression for
// MUL-3180: a thread must have at most one resolved comment, and resolving a new
// one atomically clears the previous resolution (instead of leaving two resolved
// rows that the UI only papered over).
func TestResolveComment_ReplacesPriorThreadResolution(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newResolveTestFixture(t)
	cap := captureCommentEvents(t, fx.IssueID)

	// Resolve a1 → it is the resolution.
	resolveCommentHTTP(t, fx.A1)
	if !commentResolved(t, fx.A1) {
		t.Fatalf("a1 should be resolved after first resolve")
	}

	// Resolve b1 → b1 becomes the resolution and a1 is cleared in the same write.
	resolveCommentHTTP(t, fx.B1)
	if !commentResolved(t, fx.B1) {
		t.Fatalf("b1 should be resolved")
	}
	if commentResolved(t, fx.A1) {
		t.Fatalf("a1 should have been cleared when b1 was resolved (single-resolution invariant)")
	}

	// The cleared sibling must broadcast comment:unresolved so granular realtime
	// consumers drop the stale resolution; b1 must broadcast comment:resolved.
	if got := cap.countFor(protocol.EventCommentUnresolved, fx.A1); got != 1 {
		t.Fatalf("expected exactly 1 comment:unresolved for a1, got %d", got)
	}
	if got := cap.countFor(protocol.EventCommentResolved, fx.B1); got != 1 {
		t.Fatalf("expected exactly 1 comment:resolved for b1, got %d", got)
	}
}

// TestResolveComment_ScopedToThread proves the clear never reaches across into a
// sibling thread on the same issue.
func TestResolveComment_ScopedToThread(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newResolveTestFixture(t)

	resolveCommentHTTP(t, fx.B1)   // thread 1 resolution
	resolveCommentHTTP(t, fx.A2)   // thread 2 resolution — must NOT touch thread 1
	if !commentResolved(t, fx.B1) {
		t.Fatalf("b1 (thread 1) must stay resolved when a separate thread is resolved")
	}
	if !commentResolved(t, fx.A2) {
		t.Fatalf("a2 (thread 2) should be resolved")
	}

	// Resolving the root of thread 1 overrides the reply resolution (override
	// works in both directions: reply→reply and reply→root).
	resolveCommentHTTP(t, fx.Root1)
	if !commentResolved(t, fx.Root1) {
		t.Fatalf("root1 should be resolved")
	}
	if commentResolved(t, fx.B1) {
		t.Fatalf("b1 should be cleared when root1 becomes the resolution")
	}
	if !commentResolved(t, fx.A2) {
		t.Fatalf("a2 (other thread) must remain resolved throughout")
	}
}

// TestResolveComment_ReResolveIsIdempotent pins the COALESCE idempotency +
// event suppression: re-resolving the current resolution keeps its original
// timestamp and emits no second comment:resolved event.
func TestResolveComment_ReResolveIsIdempotent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newResolveTestFixture(t)
	cap := captureCommentEvents(t, fx.IssueID)

	resolveCommentHTTP(t, fx.A1)
	first := commentResolvedAt(t, fx.A1)
	if first == nil {
		t.Fatalf("a1 should be resolved")
	}

	resolveCommentHTTP(t, fx.A1) // re-resolve same comment
	second := commentResolvedAt(t, fx.A1)
	if second == nil || !second.Equal(*first) {
		t.Fatalf("re-resolve must keep the original resolved_at (got %v, want %v)", second, first)
	}
	if got := cap.countFor(protocol.EventCommentResolved, fx.A1); got != 1 {
		t.Fatalf("re-resolve no-op must not emit a second comment:resolved (got %d)", got)
	}
}
