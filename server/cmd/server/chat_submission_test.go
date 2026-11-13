package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func submissionSession(t *testing.T) (string, string) {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent WHERE workspace_id=$1 AND name='Integration Test Agent'`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	resp := authRequest(t, "POST", "/api/chat/sessions", map[string]any{"agent_id": agentID})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create status=%d body=%s", resp.StatusCode, body)
	}
	var session struct {
		ID string `json:"id"`
	}
	readJSON(t, resp, &session)
	cleanupSubmissionSession(t, testToken, testWorkspaceID, session.ID)
	return session.ID, agentID
}

func submissionSend(t *testing.T, sessionID string, body any) map[string]any {
	t.Helper()
	resp := authRequest(t, "POST", "/api/chat/sessions/"+sessionID+"/messages", body)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("send status=%d body=%s", resp.StatusCode, raw)
	}
	var sent map[string]any
	readJSON(t, resp, &sent)
	return sent
}

func TestChatSubmissionNormalControl(t *testing.T) {
	sessionID, agentID := submissionSession(t)
	first := submissionSend(t, sessionID, map[string]any{"content": "first normal turn"})
	second := submissionSend(t, sessionID, map[string]any{"content": "second normal turn"})
	if first["queued"] != false || second["queued"] != true || first["task_id"] == second["task_id"] {
		t.Fatalf("queue contract first=%v second=%v", first, second)
	}
	resp := authRequest(t, "GET", "/api/chat/sessions/"+sessionID+"/messages", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d", resp.StatusCode)
	}
	var history []map[string]any
	readJSON(t, resp, &history)
	if len(history) != 1 {
		t.Fatalf("visible history count=%d", len(history))
	}
	agentToken := mintAgentTaskToken(t, agentID, first["task_id"].(string), testUserID)
	submissionStatus(t, testServer.URL, agentToken, testWorkspaceID, "POST", "/api/chat/sessions/"+sessionID+"/messages", map[string]any{"content": "task token turn"}, http.StatusCreated)
	fx := workspaceFixture(t)
	attachmentID := fx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "chat_session_id": sessionID,
		"uploader_type": "member", "uploader_id": testUserID,
		"filename": "offline.txt", "url": "https://example.invalid/offline.txt", "content_type": "text/plain", "size_bytes": 1,
	})
	attached := submissionSend(t, sessionID, map[string]any{"content": "attached turn", "attachment_ids": []string{attachmentID}})
	bound, ok := attached["attachment_ids"].([]any)
	if !ok || len(bound) != 1 || bound[0] != attachmentID {
		t.Fatalf("attachment response=%v", attached)
	}
	var messageID string
	fx.QueryRow(t, `SELECT chat_message_id::text FROM attachment WHERE id=$1`, attachmentID).Scan(&messageID)
	if messageID != attached["message_id"] {
		t.Fatalf("attachment bound to %s", messageID)
	}
	otherID := fx.User(t, "Submission outsider", uuid.NewString()+"@example.test")
	fx.Member(t, testWorkspaceID, otherID, "member")
	otherToken, err := generateTestJWT(otherID, "outsider@example.test", "Outsider")
	if err != nil {
		t.Fatal(err)
	}
	checkDenied := func(token, workspace string) {
		t.Helper()
		req, err := http.NewRequest("POST", testServer.URL+"/api/chat/sessions/"+sessionID+"/messages", bytes.NewBufferString(`{"content":"denied"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Workspace-ID", workspace)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			t.Fatalf("denial status=%d", resp.StatusCode)
		}
	}
	checkDenied(otherToken, testWorkspaceID)
	foreignWorkspace := fx.Workspace(t, "Foreign submission", "submission-"+uuid.NewString())
	fx.Member(t, foreignWorkspace, testUserID, "owner")
	checkDenied(testToken, foreignWorkspace)
	fx.Exec(t, `UPDATE agent_runtime SET visibility='public' WHERE id=(SELECT runtime_id FROM agent WHERE id=$1)`, agentID)
	fx.Exec(t, `UPDATE agent SET visibility='private', permission_mode='private', owner_id=$2 WHERE id=$1`, agentID, otherID)
	checkDenied(testToken, testWorkspaceID)
	fx.Exec(t, `UPDATE agent SET visibility='workspace', owner_id=$2 WHERE id=$1`, agentID, testUserID)
	fx.Exec(t, `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, otherID)
	checkDenied(otherToken, testWorkspaceID)
}

type submissionLostResponse struct{ transport http.RoundTripper }

func (rt submissionLostResponse) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	return nil, errors.New("synthetic loss after server committed response")
}

func TestChatSubmissionResponseLossReplay(t *testing.T) {
	sessionID, _ := submissionSession(t)
	body := map[string]any{"content": "response loss turn", "submission_id": uuid.NewString()}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", testServer.URL+"/api/chat/sessions/"+sessionID+"/messages", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: submissionLostResponse{http.DefaultTransport}}
	if _, err := client.Do(req); err == nil {
		t.Fatal("expected synthetic response loss")
	}
	var firstMessage, firstTask string
	if err := testPool.QueryRow(context.Background(), `SELECT id::text, task_id::text FROM chat_message WHERE chat_session_id=$1 AND role='user'`, sessionID).Scan(&firstMessage, &firstTask); err != nil {
		t.Fatal(err)
	}
	replay := submissionSend(t, sessionID, body)
	resp := authRequest(t, "GET", "/api/chat/sessions/"+sessionID+"/messages", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d", resp.StatusCode)
	}
	var history []map[string]any
	readJSON(t, resp, &history)
	var messages, tasks int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM chat_message WHERE chat_session_id=$1 AND role='user'`, sessionID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	t.Logf("original message=%s task=%s replay=%v messages=%d tasks=%d history=%d", firstMessage, firstTask, replay, messages, tasks, len(history))
	if replay["message_id"] != firstMessage || replay["task_id"] != firstTask || messages != 1 || tasks != 1 || len(history) != 1 {
		t.Fatalf("accepted submission must replay original identity once; messages=%d tasks=%d history=%d", messages, tasks, len(history))
	}
}
