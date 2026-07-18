package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// withDraftRestoreParams sets both chi URL params in one route context —
// chaining withURLParam would replace the context and drop the first param.
func withDraftRestoreParams(req *http.Request, sessionID, restoreID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionId", sessionID)
	rctx.URLParams.Add("restoreId", restoreID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// seedDraftRestore inserts a chat_draft_restore row the way the deferred
// finalize tx does: id is the deleted user message's id, attachments already
// detached (chat_message_id NULL).
func seedDraftRestore(t *testing.T, sessionID, content string, attachmentIDs []string) string {
	t.Helper()
	restoreID := uuid.NewString()
	ids := "{}"
	if len(attachmentIDs) > 0 {
		ids = "{"
		for i, id := range attachmentIDs {
			if i > 0 {
				ids += ","
			}
			ids += id
		}
		ids += "}"
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO chat_draft_restore (id, chat_session_id, task_id, content, attachment_ids)
		VALUES ($1, $2, $3, $4, $5::uuid[])
	`, restoreID, sessionID, uuid.NewString(), content, ids); err != nil {
		t.Fatalf("seed draft restore: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_draft_restore WHERE id = $1`, restoreID)
	})
	return restoreID
}

func seedDetachedChatAttachment(t *testing.T, sessionID string) string {
	t.Helper()
	var attachmentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (
			workspace_id, chat_session_id, uploader_type, uploader_id,
			filename, url, content_type, size_bytes
		)
		VALUES ($1, $2, 'member', $3, 'notes.txt', 'https://files.test/notes.txt', 'text/plain', 12)
		RETURNING id
	`, testWorkspaceID, sessionID, testUserID).Scan(&attachmentID); err != nil {
		t.Fatalf("seed detached attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID)
	})
	return attachmentID
}

// The recovery path the chat:cancel_finalized broadcast cannot guarantee: a
// creator whose client missed the event fetches the pending restore, gets the
// prompt content plus re-bindable attachments, and consumes it idempotently.
func TestChatDraftRestores_CreatorFetchesAndConsumes(t *testing.T) {
	agentID := createHandlerTestAgent(t, "DraftRestoreAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	attachmentID := seedDetachedChatAttachment(t, sessionID)
	restoreID := seedDraftRestore(t, sessionID, "run the thing", []string{attachmentID})

	listReq := withURLParam(newRequest(http.MethodGet, "/api/chat/sessions/"+sessionID+"/draft-restores", nil), "sessionId", sessionID)
	listReq = withChatTestWorkspaceCtx(t, listReq)
	listW := httptest.NewRecorder()
	testHandler.ListChatDraftRestores(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var resp ChatDraftRestoresResponse
	if err := json.Unmarshal(listW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Restores) != 1 {
		t.Fatalf("restores = %d, want 1", len(resp.Restores))
	}
	got := resp.Restores[0]
	if got.ID != restoreID {
		t.Errorf("id = %q, want %q", got.ID, restoreID)
	}
	if got.Content != "run the thing" {
		t.Errorf("content = %q, want %q", got.Content, "run the thing")
	}
	if len(got.Attachments) != 1 || got.Attachments[0].ID != attachmentID {
		t.Fatalf("attachments = %+v, want the seeded attachment", got.Attachments)
	}
	if got.Attachments[0].Filename != "notes.txt" {
		t.Errorf("attachment filename = %q, want notes.txt", got.Attachments[0].Filename)
	}

	// Consume twice: both must be 204 (idempotent), and the row must be gone.
	for i := 0; i < 2; i++ {
		delReq := withDraftRestoreParams(newRequest(http.MethodDelete, "/api/chat/sessions/"+sessionID+"/draft-restores/"+restoreID, nil), sessionID, restoreID)
		delReq = withChatTestWorkspaceCtx(t, delReq)
		delW := httptest.NewRecorder()
		testHandler.ConsumeChatDraftRestore(delW, delReq)
		if delW.Code != http.StatusNoContent {
			t.Fatalf("consume call %d: expected 204, got %d: %s", i+1, delW.Code, delW.Body.String())
		}
	}

	listW = httptest.NewRecorder()
	testHandler.ListChatDraftRestores(listW, withChatTestWorkspaceCtx(t, withURLParam(newRequest(http.MethodGet, "/api/chat/sessions/"+sessionID+"/draft-restores", nil), "sessionId", sessionID)))
	if listW.Code != http.StatusOK {
		t.Fatalf("relist: expected 200, got %d", listW.Code)
	}
	resp = ChatDraftRestoresResponse{}
	if err := json.Unmarshal(listW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode relist: %v", err)
	}
	if len(resp.Restores) != 0 {
		t.Errorf("restores after consume = %d, want 0", len(resp.Restores))
	}
}

// Draft restores hold the creator's private prompt: another workspace member
// must not be able to read or consume them.
func TestChatDraftRestores_NonCreatorForbidden(t *testing.T) {
	agentID := createHandlerTestAgent(t, "DraftRestoreOtherAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	restoreID := seedDraftRestore(t, sessionID, "secret prompt", nil)

	var otherUserID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ('Draft Restore Other', $1)
		RETURNING id
	`, fmt.Sprintf("draft-restore-other-%d@multica.ai", time.Now().UnixNano())).Scan(&otherUserID); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID); err != nil {
		t.Fatalf("create other member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, otherUserID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})

	listReq := withURLParam(newRequest(http.MethodGet, "/api/chat/sessions/"+sessionID+"/draft-restores", nil), "sessionId", sessionID)
	listReq = withChatTestWorkspaceCtx(t, listReq)
	listReq.Header.Set("X-User-ID", otherUserID)
	listW := httptest.NewRecorder()
	testHandler.ListChatDraftRestores(listW, listReq)
	if listW.Code != http.StatusForbidden {
		t.Fatalf("list as non-creator: expected 403, got %d: %s", listW.Code, listW.Body.String())
	}

	delReq := withDraftRestoreParams(newRequest(http.MethodDelete, "/api/chat/sessions/"+sessionID+"/draft-restores/"+restoreID, nil), sessionID, restoreID)
	delReq = withChatTestWorkspaceCtx(t, delReq)
	delReq.Header.Set("X-User-ID", otherUserID)
	delW := httptest.NewRecorder()
	testHandler.ConsumeChatDraftRestore(delW, delReq)
	if delW.Code != http.StatusForbidden {
		t.Fatalf("consume as non-creator: expected 403, got %d: %s", delW.Code, delW.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM chat_draft_restore WHERE id = $1`, restoreID).Scan(&count); err != nil {
		t.Fatalf("count restore: %v", err)
	}
	if count != 1 {
		t.Errorf("restore row must survive a non-creator consume attempt, count = %d", count)
	}
}

// chat_draft_restore has no chat_session FK (MUL-3515), so nothing but
// DeleteChatSession's own transaction prunes an unconsumed restore.
func TestDeleteChatSession_PrunesDraftRestores(t *testing.T) {
	agentID := createHandlerTestAgent(t, "DraftRestorePruneAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	seedDraftRestore(t, sessionID, "unconsumed prompt", nil)

	req := withURLParam(newRequest(http.MethodDelete, "/api/chat/sessions/"+sessionID, nil), "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()
	testHandler.DeleteChatSession(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_draft_restore WHERE chat_session_id = $1`, sessionID).Scan(&count); err != nil {
		t.Fatalf("count restores: %v", err)
	}
	if count != 0 {
		t.Errorf("draft restores must be pruned with their session, count = %d", count)
	}
}

func countDraftRestores(t *testing.T, sessionID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_draft_restore WHERE chat_session_id = $1`, sessionID).Scan(&count); err != nil {
		t.Fatalf("count restores: %v", err)
	}
	return count
}

// DeleteChatSession is not the only way a chat_session dies: it also cascades
// from agent (migration 033). Hard-deleting a runtime's archived agents drops
// their sessions silently, and with no FK on chat_draft_restore the pending
// restores — each holding the user's prompt — would be stranded forever.
func TestDeleteAgentRuntime_PrunesDraftRestoresOfCascadedSessions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := seedIsolatedRuntime(t, "Runtime With Draft Restore")
	archivedAgent := seedAgentOnRuntime(t, runtimeID, "Archived Agent With Draft Restore", true)
	sessionID := createHandlerTestChatSession(t, archivedAgent)
	seedDraftRestore(t, sessionID, "prompt stranded by the agent cascade", nil)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, "/api/runtimes/"+runtimeID, nil), "runtimeId", runtimeID)
	testHandler.DeleteAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteAgentRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if agentExists(t, archivedAgent) {
		t.Fatal("archived agent should have been deleted — the cascade under test never ran")
	}
	if n := countDraftRestores(t, sessionID); n != 0 {
		t.Errorf("draft restores of a cascade-deleted session must be pruned, count = %d", n)
	}
}

// The workspace cascade, same class. DeleteWorkspace runs outside a transaction
// (its atomicity comes from being one multi-CTE statement), so the prune has to
// live inside that statement — this is what guards it.
func TestDeleteWorkspace_PrunesDraftRestoresOfCascadedSessions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	const slug = "handler-tests-delete-draft-restores"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, '')
		RETURNING id
	`, "Handler Test Draft Restore Cascade", slug).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Draft Restore Cascade Runtime', 'cloud', 'isolated_test', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, wsID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Draft Restore Cascade Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, wsID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, 'Draft Restore Cascade Session', 'active')
		RETURNING id
	`, wsID, agentID, testUserID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	seedDraftRestore(t, sessionID, "prompt stranded by the workspace cascade", nil)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+wsID, nil), "id", wsID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if n := countDraftRestores(t, sessionID); n != 0 {
		t.Errorf("draft restores of a workspace-cascaded session must be pruned, count = %d", n)
	}
}
