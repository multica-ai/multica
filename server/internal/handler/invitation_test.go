package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const memberAddTestEmail = "member-add-test@multica.ai"

func TestCreateMember_BlocksDuplicateMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	userID := parseUUID(testUserID)
	_, _ = testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, parseUUID(testWorkspaceID), userID)

	// Seed a second user and add them once.
	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Member Add Test', $1)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, memberAddTestEmail).Scan(&otherUserID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`,
			parseUUID(testWorkspaceID), otherUserID)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = $1`, memberAddTestEmail)
	})

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: memberAddTestEmail,
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first add: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	req2 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: memberAddTestEmail,
		Role:  "member",
	})
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	testHandler.CreateMember(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second add: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestCreateMember_ReturnsMemberShape(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const email = "member-shape-test@multica.ai"
	ctx := context.Background()
	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Shape Test', $1)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, email).Scan(&otherUserID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`,
			parseUUID(testWorkspaceID), otherUserID)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = $1`, email)
	})

	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/members", CreateMemberRequest{
		Email: email,
		Role:  "member",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp MemberWithUserResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Email != email || resp.Role != "member" || resp.ID == "" {
		t.Fatalf("unexpected member response: %+v", resp)
	}
}