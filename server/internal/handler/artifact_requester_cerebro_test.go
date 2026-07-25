// FIR-3778 — an agent that writes a document for a person must record THAT
// person as the requester, and that person must then be able to save it.
//
// The default handler fixture user is a workspace 'owner', which passes the
// admin branch of every gate, so these tests create plain 'member' users: the
// assertions would be meaningless against an owner.
package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// createHandlerTestMember inserts an extra user with the plain 'member' role in
// the fixture workspace, so a test can distinguish the authenticated caller,
// the human an agent acted for, and an unrelated colleague.
func createHandlerTestMember(t *testing.T, email, name string) string {
	t.Helper()

	var userID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, name, email).Scan(&userID); err != nil {
		t.Fatalf("failed to create handler test member: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("failed to add handler test member to workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

// createArtifactAsAgentFor runs CreateArtifact as the given agent on a task
// delegated by delegatedUserID, and returns the decoded response.
func createArtifactAsAgentFor(t *testing.T, agentID, taskID, title string) struct {
	ID              string  `json:"id"`
	AuthorType      string  `json:"author_type"`
	RequesterUserID *string `json:"requester_user_id"`
} {
	t.Helper()

	req := newRequest("POST", "/api/artifacts", map[string]any{
		"kind":  "note",
		"title": title,
		"body":  "first line",
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	rec := httptest.NewRecorder()
	testHandler.CreateArtifact(rec, req)

	if rec.Code != 200 && rec.Code != 201 {
		t.Fatalf("create artifact = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID              string  `json:"id"`
		AuthorType      string  `json:"author_type"`
		RequesterUserID *string `json:"requester_user_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM artifact WHERE id = $1`, resp.ID)
	})
	return resp
}

// TestCreateArtifactStampsDelegatedRequester proves the artifact records the
// human the agent acted for — the originating task's original_user_id — and not
// the authenticated caller, who for a real agent run is the runtime owner.
func TestCreateArtifactStampsDelegatedRequester(t *testing.T) {
	if testPool == nil {
		t.Skip("no test DB")
	}

	delegatedUserID := createHandlerTestMember(t, "fir3778-delegated@multica.ai", "FIR-3778 Delegated")
	agentID := createHandlerTestAgent(t, "FIR-3778 Requester Agent", nil)
	taskID := createHandlerTestDelegatedTaskForAgent(t, agentID, delegatedUserID)

	resp := createArtifactAsAgentFor(t, agentID, taskID, "Checklist")

	if resp.AuthorType != "agent" {
		t.Fatalf("author_type = %q, want agent", resp.AuthorType)
	}
	if resp.RequesterUserID == nil {
		t.Fatalf("requester_user_id is null, want the delegating human %s", delegatedUserID)
	}
	if *resp.RequesterUserID == testUserID {
		t.Fatalf("requester_user_id = the authenticated caller (%s); it must be the human who asked (%s)",
			testUserID, delegatedUserID)
	}
	if *resp.RequesterUserID != delegatedUserID {
		t.Fatalf("requester_user_id = %s, want %s", *resp.RequesterUserID, delegatedUserID)
	}
}

// TestCreateArtifactFallsBackToCallerWhenTaskHasNoDelegator keeps the previous
// behaviour for an agent run that traces back to no member: the authenticated
// user stays the requester rather than the column going null.
func TestCreateArtifactFallsBackToCallerWhenTaskHasNoDelegator(t *testing.T) {
	if testPool == nil {
		t.Skip("no test DB")
	}

	agentID := createHandlerTestAgent(t, "FIR-3778 Fallback Agent", nil)
	taskID := createHandlerTestTaskForAgent(t, agentID) // no original_user_id

	resp := createArtifactAsAgentFor(t, agentID, taskID, "Fallback checklist")

	if resp.RequesterUserID == nil {
		t.Fatalf("requester_user_id is null, want the authenticated caller %s", testUserID)
	}
	if *resp.RequesterUserID != testUserID {
		t.Fatalf("requester_user_id = %s, want the authenticated caller %s", *resp.RequesterUserID, testUserID)
	}
}

// TestUpdateArtifactAllowsRequesterNotStrangers proves the human an agent wrote
// a document for may save it, while an unrelated colleague still may not.
// Both users are plain members, so neither passes the admin branch.
func TestUpdateArtifactAllowsRequesterNotStrangers(t *testing.T) {
	if testPool == nil {
		t.Skip("no test DB")
	}

	requesterID := createHandlerTestMember(t, "fir3778-requester@multica.ai", "FIR-3778 Requester")
	strangerID := createHandlerTestMember(t, "fir3778-stranger@multica.ai", "FIR-3778 Stranger")
	agentID := createHandlerTestAgent(t, "FIR-3778 Update Agent", nil)
	taskID := createHandlerTestDelegatedTaskForAgent(t, agentID, requesterID)

	doc := createArtifactAsAgentFor(t, agentID, taskID, "Checklist to edit")

	save := func(userID, body string) *httptest.ResponseRecorder {
		req := withURLParam(newRequest("PUT", "/api/artifacts/"+doc.ID, map[string]any{
			"body": body,
		}), "id", doc.ID)
		req.Header.Set("X-User-ID", userID)
		rec := httptest.NewRecorder()
		testHandler.UpdateArtifact(rec, req)
		return rec
	}

	if rec := save(requesterID, "edited by the requester"); rec.Code != 200 {
		t.Errorf("requester save = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := save(strangerID, "edited by a stranger"); rec.Code != 403 {
		t.Errorf("stranger save = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}
