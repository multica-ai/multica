package main

// FIR-4930 — end-to-end over the real HTTP API and a real database: creating an
// issue with on_behalf_of_user_id must durably attribute it to that human, and a
// target who is not a member of the workspace must be refused before any issue
// exists.

import (
	"context"
	"net/http"
	"testing"
)

// addWorkspaceMember makes an existing user a member of the test workspace and
// returns their user id.
func addWorkspaceMember(t *testing.T, email string) string {
	t.Helper()
	userID := createTestUser(t, email)
	t.Cleanup(func() { cleanupTestUser(t, email) })

	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
		 ON CONFLICT DO NOTHING`,
		testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	return userID
}

func TestCreateIssueWithOnBehalfOf(t *testing.T) {
	appOwnerID := addWorkspaceMember(t, "api-on-behalf-of-owner@multica.ai")

	resp := authRequestWithWorkspace(t, http.MethodPost, "/api/issues", testWorkspaceID, map[string]any{
		"title":                "Deploy review: invoice-warnings",
		"on_behalf_of_user_id": appOwnerID,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create issue: expected 201, got %d", resp.StatusCode)
	}
	var created map[string]any
	readJSON(t, resp, &created)
	issueID, _ := created["id"].(string)
	if issueID == "" {
		t.Fatal("create issue returned no id")
	}
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	// The stamp is durable — it is a column on the issue, not a derived join.
	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(on_behalf_of_user_id::text, '') FROM issue WHERE id = $1`, issueID,
	).Scan(&stored); err != nil {
		t.Fatalf("read stored stamp: %v", err)
	}
	if stored != appOwnerID {
		t.Fatalf("stored on_behalf_of_user_id = %q, want %q", stored, appOwnerID)
	}

	// And it is what the issue reports back as its human origin.
	getResp := authRequestWithWorkspace(t, http.MethodGet, "/api/issues/"+issueID, testWorkspaceID, nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get issue: expected 200, got %d", getResp.StatusCode)
	}
	var fetched map[string]any
	readJSON(t, getResp, &fetched)
	obo, ok := fetched["on_behalf_of"].(map[string]any)
	if !ok {
		t.Fatalf("issue response carries no on_behalf_of: %v", fetched["on_behalf_of"])
	}
	if obo["user_id"] != appOwnerID {
		t.Errorf("on_behalf_of.user_id = %v, want %v", obo["user_id"], appOwnerID)
	}
}

// Fail closed: a user who is not a member of this workspace cannot be stamped,
// and the rejection happens before the issue is created so a bad call leaves
// nothing behind.
func TestCreateIssueWithOnBehalfOf_RejectsNonMember(t *testing.T) {
	outsiderEmail := "api-on-behalf-of-outsider@multica.ai"
	outsiderID := createTestUser(t, outsiderEmail)
	t.Cleanup(func() { cleanupTestUser(t, outsiderEmail) })

	var before int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue WHERE workspace_id = $1`, testWorkspaceID).Scan(&before)

	resp := authRequestWithWorkspace(t, http.MethodPost, "/api/issues", testWorkspaceID, map[string]any{
		"title":                "should never exist",
		"on_behalf_of_user_id": outsiderID,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-member target, got %d", resp.StatusCode)
	}

	var after int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue WHERE workspace_id = $1`, testWorkspaceID).Scan(&after)
	if after != before {
		t.Fatalf("a rejected stamp still created an issue (%d → %d)", before, after)
	}
}

// Correcting a wrong stamp through the update endpoint, and clearing it again.
func TestUpdateIssueOnBehalfOf(t *testing.T) {
	wrongID := addWorkspaceMember(t, "api-on-behalf-of-wrong@multica.ai")
	rightID := addWorkspaceMember(t, "api-on-behalf-of-right@multica.ai")

	resp := authRequestWithWorkspace(t, http.MethodPost, "/api/issues", testWorkspaceID, map[string]any{
		"title":                "Deploy review: wrong owner",
		"on_behalf_of_user_id": wrongID,
	})
	var created map[string]any
	readJSON(t, resp, &created)
	issueID, _ := created["id"].(string)
	if issueID == "" {
		t.Fatal("create issue returned no id")
	}
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	updResp := authRequestWithWorkspace(t, http.MethodPut, "/api/issues/"+issueID, testWorkspaceID, map[string]any{
		"on_behalf_of_user_id": rightID,
	})
	defer updResp.Body.Close()
	if updResp.StatusCode != http.StatusOK {
		t.Fatalf("update issue: expected 200, got %d", updResp.StatusCode)
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(on_behalf_of_user_id::text, '') FROM issue WHERE id = $1`, issueID,
	).Scan(&stored); err != nil {
		t.Fatalf("read stored stamp: %v", err)
	}
	if stored != rightID {
		t.Fatalf("stored on_behalf_of_user_id = %q, want %q", stored, rightID)
	}

	clearResp := authRequestWithWorkspace(t, http.MethodPut, "/api/issues/"+issueID, testWorkspaceID, map[string]any{
		"on_behalf_of_user_id": "",
	})
	defer clearResp.Body.Close()
	if clearResp.StatusCode != http.StatusOK {
		t.Fatalf("clear stamp: expected 200, got %d", clearResp.StatusCode)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(on_behalf_of_user_id::text, '') FROM issue WHERE id = $1`, issueID,
	).Scan(&stored); err != nil {
		t.Fatalf("read stored stamp after clear: %v", err)
	}
	if stored != "" {
		t.Fatalf("expected the stamp to be cleared, still %q", stored)
	}
}
