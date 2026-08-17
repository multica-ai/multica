package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Mixed-version contract for subtree unsubscribe (MUL-5483 review round 6,
// finding 1).
//
// Web/desktop staging deploys automatically when a PR merges; the backend is
// deployed by hand. A new client therefore routinely runs against a server
// that predates whatever it just learned to send. Subtree unsubscribe was
// originally a `subtree: true` FIELD on POST /unsubscribe — and a field is
// exactly the wrong carrier for a capability across that skew, because Go's
// json.Decode ignores unknown fields: the old server would unsubscribe the
// root, answer 200, and the user would be told the whole tree was muted while
// every existing and future child kept notifying them.
//
// Moving it to its own ROUTE converts that silent wrong answer into a 404 the
// client can surface. These tests pin both halves of that contract.

// TestUnsubscribeEndpoints_SubtreeIsASeparateRoute pins the shape the fix
// depends on: the shared endpoint must have NO subtree behavior of its own, so
// that an old server and a new server answer a plain /unsubscribe identically.
func TestUnsubscribeEndpoints_SubtreeIsASeparateRoute(t *testing.T) {
	parentID, childID := createSubscriberTreeFixture(t)

	subscribeTo(t, parentID)
	subscribeTo(t, childID)

	// A body field named "subtree" must be inert on the shared endpoint. If it
	// ever regains meaning here, an old backend and a new backend disagree
	// about what the same request does — which is the whole defect.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+parentID+"/unsubscribe", map[string]any{
		"subtree": true,
	})
	req = withURLParam(req, "id", parentID)
	testHandler.UnsubscribeFromIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UnsubscribeFromIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if isSubscriberOf(t, parentID) {
		t.Fatal("the plain endpoint must still unsubscribe the issue it targets")
	}
	if !isSubscriberOf(t, childID) {
		t.Fatal("a 'subtree' body field must be INERT on POST /unsubscribe: " +
			"if it works here, an older backend silently ignoring it is indistinguishable from success")
	}
}

// TestUnsubscribeEndpoints_SubtreeRouteLeavesDescendants is the positive half:
// the dedicated route does reach descendants, so the capability really did
// move rather than disappear.
func TestUnsubscribeEndpoints_SubtreeRouteLeavesDescendants(t *testing.T) {
	parentID, childID := createSubscriberTreeFixture(t)

	subscribeTo(t, parentID)
	subscribeTo(t, childID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+parentID+"/unsubscribe/subtree", nil)
	req = withURLParam(req, "id", parentID)
	testHandler.UnsubscribeFromIssueSubtree(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UnsubscribeFromIssueSubtree: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if isSubscriberOf(t, parentID) {
		t.Fatal("subtree unsubscribe must leave the root issue")
	}
	if isSubscriberOf(t, childID) {
		t.Fatal("subtree unsubscribe must leave descendants too")
	}
}

// --- fixtures ---

// createSubscriberTreeFixture builds a parent issue with one child and returns
// both ids, cleaning up after the test.
func createSubscriberTreeFixture(t *testing.T) (parentID, childID string) {
	t.Helper()

	create := func(title string, parent *string) string {
		w := httptest.NewRecorder()
		body := map[string]any{"title": title}
		if parent != nil {
			body["parent_issue_id"] = *parent
		}
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue(%s): expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
		var issue IssueResponse
		json.NewDecoder(w.Body).Decode(&issue)
		return issue.ID
	}

	parentID = create("subtree endpoint parent", nil)
	childID = create("subtree endpoint child", &parentID)

	t.Cleanup(func() {
		for _, id := range []string{childID, parentID} {
			w := httptest.NewRecorder()
			req := newRequest("DELETE", "/api/issues/"+id, nil)
			req = withURLParam(req, "id", id)
			testHandler.DeleteIssue(w, req)
		}
	})
	return parentID, childID
}

func subscribeTo(t *testing.T, issueID string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.SubscribeToIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SubscribeToIssue(%s): expected 200, got %d: %s", issueID, w.Code, w.Body.String())
	}
}

func isSubscriberOf(t *testing.T, issueID string) bool {
	t.Helper()
	subscribed, err := testHandler.Queries.IsIssueSubscriber(t.Context(), db.IsIssueSubscriberParams{
		IssueID:  parseUUID(issueID),
		UserType: "member",
		UserID:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("IsIssueSubscriber(%s): %v", issueID, err)
	}
	return subscribed
}
