package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateChatSessionModePersistsAndReturnsCanonicalValue(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatModeAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	for _, tc := range []struct {
		input string
		want  string
	}{{"auto", "auto"}, {"plan", "plan"}, {"build", "build"}, {"research", "research"}, {"review", "review"}, {"default", "build"}} {
		req := newRequest(http.MethodPatch, "/api/chat/sessions/"+sessionID, map[string]any{"mode": tc.input})
		req = withURLParam(req, "sessionId", sessionID)
		req = withChatTestWorkspaceCtx(t, req)
		w := httptest.NewRecorder()
		testHandler.UpdateChatSession(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("mode %q: code=%d body=%s", tc.input, w.Code, strings.TrimSpace(w.Body.String()))
		}
		var resp ChatSessionResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode mode %q: %v", tc.input, err)
		}
		if resp.Mode != tc.want {
			t.Fatalf("mode %q response=%q, want %q", tc.input, resp.Mode, tc.want)
		}
		var stored string
		if err := testPool.QueryRow(context.Background(), `SELECT mode FROM chat_session WHERE id = $1`, sessionID).Scan(&stored); err != nil {
			t.Fatalf("read stored mode: %v", err)
		}
		if stored != tc.want {
			t.Fatalf("mode %q stored=%q, want %q", tc.input, stored, tc.want)
		}
	}
}

func TestUpdateChatSessionRejectsUnknownMode(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatBadModeAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	req := newRequest(http.MethodPatch, "/api/chat/sessions/"+sessionID, map[string]any{"mode": "ship"})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.UpdateChatSession(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s, want 400", w.Code, strings.TrimSpace(w.Body.String()))
	}
}
