package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newRequestForUser(userID, method, path string, body any) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
	return req
}

func createWorkspaceMember(t *testing.T, workspaceID, role, label string) string {
	t.Helper()

	slugPart := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	email := fmt.Sprintf("%s-%s@multica.ai", slugPart, label)
	name := fmt.Sprintf("Subscriber %s", label)

	var userID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, name, email).Scan(&userID); err != nil {
		t.Fatalf("createWorkspaceMember user: %v", err)
	}

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
	`, workspaceID, userID, role); err != nil {
		t.Fatalf("createWorkspaceMember member: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return userID
}

func createExternalMember(t *testing.T, label string) string {
	t.Helper()

	slugPart := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	email := fmt.Sprintf("%s-%s@multica.ai", slugPart, label)
	workspaceSlug := fmt.Sprintf("%s-%s", slugPart, label)

	var userID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "External Subscriber", email).Scan(&userID); err != nil {
		t.Fatalf("createExternalMember user: %v", err)
	}

	var workspaceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "External Subscriber Workspace", workspaceSlug, "External subscriber test workspace", "EXT").Scan(&workspaceID); err != nil {
		t.Fatalf("createExternalMember workspace: %v", err)
	}

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, workspaceID, userID); err != nil {
		t.Fatalf("createExternalMember member: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return userID
}

func getWorkspaceAgentID(t *testing.T, workspaceID string) string {
	t.Helper()

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id
		FROM agent
		WHERE workspace_id = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, workspaceID).Scan(&agentID); err != nil {
		t.Fatalf("getWorkspaceAgentID: %v", err)
	}

	return agentID
}

func TestSubscriberAPI(t *testing.T) {
	ctx := context.Background()

	// Helper: create an issue for subscriber tests
	createIssue := func(t *testing.T) string {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title": "Subscriber test issue",
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var issue IssueResponse
		json.NewDecoder(w.Body).Decode(&issue)
		return issue.ID
	}

	// Helper: delete an issue
	deleteIssue := func(t *testing.T, issueID string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/issues/"+issueID, nil)
		req = withURLParam(req, "id", issueID)
		testHandler.DeleteIssue(w, req)
	}

	t.Run("Subscribe", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]bool
		json.NewDecoder(w.Body).Decode(&resp)
		if !resp["subscribed"] {
			t.Fatal("SubscribeToIssue: expected subscribed=true")
		}

		// Verify in DB
		subscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber: %v", err)
		}
		if !subscribed {
			t.Fatal("expected user to be subscribed in DB")
		}
	})

	t.Run("SubscribeIdempotent", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		// Subscribe first time
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue (1st): expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Subscribe second time — should also succeed
		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue (2nd): expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("ListSubscribers", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		// Subscribe first
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// List
		w = httptest.NewRecorder()
		req = newRequest("GET", "/api/issues/"+issueID+"/subscribers", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.ListIssueSubscribers(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssueSubscribers: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var subscribers []SubscriberResponse
		json.NewDecoder(w.Body).Decode(&subscribers)
		if len(subscribers) == 0 {
			t.Fatal("ListIssueSubscribers: expected at least 1 subscriber")
		}
		found := false
		for _, s := range subscribers {
			if s.UserID == testUserID && s.UserType == "member" && s.Reason == "manual" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ListIssueSubscribers: expected to find test user subscriber, got %+v", subscribers)
		}
	})

	t.Run("Unsubscribe", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		// Subscribe first
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Unsubscribe
		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/issues/"+issueID+"/unsubscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.UnsubscribeFromIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UnsubscribeFromIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]bool
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["subscribed"] {
			t.Fatal("UnsubscribeFromIssue: expected subscribed=false")
		}

		// Verify in DB
		subscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber: %v", err)
		}
		if subscribed {
			t.Fatal("expected user to NOT be subscribed in DB")
		}
	})

	t.Run("ListAfterUnsubscribe", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		// Subscribe
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)

		// Unsubscribe
		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/issues/"+issueID+"/unsubscribe", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.UnsubscribeFromIssue(w, req)

		// List should be empty
		w = httptest.NewRecorder()
		req = newRequest("GET", "/api/issues/"+issueID+"/subscribers", nil)
		req = withURLParam(req, "id", issueID)
		testHandler.ListIssueSubscribers(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssueSubscribers: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var subscribers []SubscriberResponse
		json.NewDecoder(w.Body).Decode(&subscribers)
		if len(subscribers) != 0 {
			t.Fatalf("ListIssueSubscribers: expected 0 subscribers after unsubscribe, got %d", len(subscribers))
		}
	})

	t.Run("SubscribeOtherMemberRequiresAdmin", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		callerID := createWorkspaceMember(t, testWorkspaceID, "member", "caller-member")
		targetUserID := createWorkspaceMember(t, testWorkspaceID, "member", "target-member")

		w := httptest.NewRecorder()
		req := newRequestForUser(callerID, "POST", "/api/issues/"+issueID+"/subscribe", map[string]any{
			"user_id":   targetUserID,
			"user_type": "member",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("SubscribeToIssue: expected 403 for non-admin managing another member, got %d: %s", w.Code, w.Body.String())
		}

		subscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(targetUserID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber: %v", err)
		}
		if subscribed {
			t.Fatal("expected target member to remain unsubscribed")
		}
	})

	t.Run("SubscribeOtherMemberAllowedForOwner", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		targetUserID := createWorkspaceMember(t, testWorkspaceID, "member", "owner-target-member")

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", map[string]any{
			"user_id":   targetUserID,
			"user_type": "member",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToIssue: expected 200 for owner managing same-workspace member, got %d: %s", w.Code, w.Body.String())
		}

		subscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(targetUserID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber: %v", err)
		}
		if !subscribed {
			t.Fatal("expected owner-managed member subscription to be stored")
		}
	})

	t.Run("SubscribeForeignMemberRejected", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		foreignUserID := createExternalMember(t, "foreign-member")

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/subscribe", map[string]any{
			"user_id":   foreignUserID,
			"user_type": "member",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("SubscribeToIssue: expected 404 for foreign member target, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("SubscribeAgentRequiresAdmin", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		callerID := createWorkspaceMember(t, testWorkspaceID, "member", "agent-caller")
		agentID := getWorkspaceAgentID(t, testWorkspaceID)

		w := httptest.NewRecorder()
		req := newRequestForUser(callerID, "POST", "/api/issues/"+issueID+"/subscribe", map[string]any{
			"user_id":   agentID,
			"user_type": "agent",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.SubscribeToIssue(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("SubscribeToIssue: expected 403 for non-admin managing an agent subscriber, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("UnsubscribeOtherMemberRequiresAdmin", func(t *testing.T) {
		issueID := createIssue(t)
		defer deleteIssue(t, issueID)

		callerID := createWorkspaceMember(t, testWorkspaceID, "member", "unsubscribe-caller")
		targetUserID := createWorkspaceMember(t, testWorkspaceID, "member", "unsubscribe-target")

		if err := testHandler.Queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(targetUserID),
			Reason:   "manual",
		}); err != nil {
			t.Fatalf("AddIssueSubscriber: %v", err)
		}

		w := httptest.NewRecorder()
		req := newRequestForUser(callerID, "POST", "/api/issues/"+issueID+"/unsubscribe", map[string]any{
			"user_id":   targetUserID,
			"user_type": "member",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.UnsubscribeFromIssue(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("UnsubscribeFromIssue: expected 403 for non-admin managing another member, got %d: %s", w.Code, w.Body.String())
		}

		subscribed, err := testHandler.Queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  parseUUID(issueID),
			UserType: "member",
			UserID:   parseUUID(targetUserID),
		})
		if err != nil {
			t.Fatalf("IsIssueSubscriber: %v", err)
		}
		if !subscribed {
			t.Fatal("expected target member subscription to remain present")
		}
	})
}
