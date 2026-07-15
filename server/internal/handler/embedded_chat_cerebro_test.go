package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateEmbeddedChatSessionUsesVerifiedMemberAndAPIKind(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	if _, err := testPool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS cerebro_chat_session_context (
			chat_session_id uuid PRIMARY KEY REFERENCES chat_session(id) ON DELETE CASCADE,
			kind text NOT NULL CHECK (kind IN ('api')),
			source text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		t.Fatal(err)
	}

	verifiedUser := createWorkspaceMemberUser(t, "member")
	agentID := createHandlerTestAgent(t, "Embedded Owner Agent", []byte(`null`))
	// A public agent lets a normal verified member start the conversation.
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET visibility = 'workspace' WHERE id = $1`, agentID); err != nil {
		t.Fatal(err)
	}
	cleanupChatSessionsForAgent(t, agentID)

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodPost, "/api/cerebro/embedded-chat/sessions", verifiedUser, map[string]any{
		"agent_id": agentID, "title": "Finance report",
	})
	testHandler.CreateEmbeddedChatSession(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status: want 201 got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var creatorID, kind, source string
	if err := testPool.QueryRow(context.Background(), `
		SELECT cs.creator_id, ec.kind, ec.source
		FROM chat_session cs
		JOIN cerebro_chat_session_context ec ON ec.chat_session_id = cs.id
		WHERE cs.id = $1
	`, body.ID).Scan(&creatorID, &kind, &source); err != nil {
		t.Fatal(err)
	}
	if creatorID != verifiedUser || kind != "api" || source != "finance" {
		t.Fatalf("creator/kind/source = %s/%s/%s", creatorID, kind, source)
	}
}
