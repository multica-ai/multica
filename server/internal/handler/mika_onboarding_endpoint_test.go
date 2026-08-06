package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// startMikaOnboarding drives the endpoint the way the router does: session id
// as a chi URL param plus the workspace/member context the middleware sets.
func startMikaOnboarding(t *testing.T, sessionID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest("POST", "/api/chat/sessions/"+sessionID+"/onboarding", body)
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.StartMikaOnboarding(w, req)
	return w
}

// markAsMika stamps the system_key that identifies the workspace's built-in
// agent. The onboarding endpoint gates on this rather than the display name,
// so a fixture merely named "Mika" is not enough.
func markAsMika(t *testing.T, agentID string) string {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET system_key = $2 WHERE id = $1`, agentID, service.MikaSystemKey,
	); err != nil {
		t.Fatalf("mark agent as mika: %v", err)
	}
	return agentID
}

func decodeStartMikaOnboarding(t *testing.T, w *httptest.ResponseRecorder) startMikaOnboardingResponse {
	t.Helper()
	var resp startMikaOnboardingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode start onboarding response: %v", err)
	}
	return resp
}

func countSessionTasks(t *testing.T, sessionID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE chat_session_id = $1`, sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("count session tasks: %v", err)
	}
	return count
}

// cleanupSessionTasks drops the tasks the endpoint enqueued so the shared
// handler fixture does not leak queue rows into later tests.
func cleanupSessionTasks(t *testing.T, sessionID string) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, sessionID)
	})
}

// TestStartMikaOnboarding_EnqueuesOneHiddenKickoff covers the endpoint's whole
// reason to exist: one product-authored opening turn that the runtime receives
// as a normal chat input batch but the member never sees as a bubble.
func TestStartMikaOnboarding_EnqueuesOneHiddenKickoff(t *testing.T) {
	agentID := markAsMika(t, createHandlerTestAgent(t, "Mika", nil))
	sessionID := createHandlerTestChatSession(t, agentID)
	cleanupSessionTasks(t, sessionID)

	w := startMikaOnboarding(t, sessionID, map[string]any{"language": "en"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeStartMikaOnboarding(t, w)
	if !resp.Started || resp.TaskID == "" {
		t.Fatalf("expected a started kickoff with a task id, got %+v", resp)
	}

	var role, content, kind string
	if err := testPool.QueryRow(context.Background(),
		`SELECT role, content, message_kind FROM chat_message WHERE chat_session_id = $1`, sessionID,
	).Scan(&role, &content, &kind); err != nil {
		t.Fatalf("load kickoff message: %v", err)
	}
	if role != "user" {
		t.Fatalf("kickoff must be stored as a user input batch, got role %q", role)
	}
	if kind != protocol.ChatMessageKindOnboardingKickoff {
		t.Fatalf("kickoff message_kind = %q, want %q", kind, protocol.ChatMessageKindOnboardingKickoff)
	}
	if content == "" {
		t.Fatal("kickoff must carry the product-authored prompt for the runtime")
	}

	// The member-facing transcript stays empty: the kickoff is carrier, not
	// conversation.
	listReq := withChatTestWorkspaceCtx(t, withURLParam(
		newRequest("GET", "/api/chat/sessions/"+sessionID+"/messages", nil),
		"sessionId", sessionID,
	))
	listW := httptest.NewRecorder()
	testHandler.ListChatMessages(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list messages: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var visible []ChatMessageResponse
	if err := json.Unmarshal(listW.Body.Bytes(), &visible); err != nil {
		t.Fatalf("decode visible messages: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("expected the kickoff to be hidden from the transcript, got %d message(s)", len(visible))
	}
}

// TestStartMikaOnboarding_IsIdempotent is the retry / double-submit guarantee
// the handler documents: a second call must not enqueue a second opening turn.
func TestStartMikaOnboarding_IsIdempotent(t *testing.T) {
	agentID := markAsMika(t, createHandlerTestAgent(t, "Mika", nil))
	sessionID := createHandlerTestChatSession(t, agentID)
	cleanupSessionTasks(t, sessionID)

	first := startMikaOnboarding(t, sessionID, map[string]any{"language": "zh"})
	if first.Code != http.StatusCreated {
		t.Fatalf("first call: expected 201, got %d: %s", first.Code, first.Body.String())
	}
	if tasks := countSessionTasks(t, sessionID); tasks != 1 {
		t.Fatalf("after first call: expected 1 task, got %d", tasks)
	}

	second := startMikaOnboarding(t, sessionID, map[string]any{"language": "zh"})
	if second.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d: %s", second.Code, second.Body.String())
	}
	if resp := decodeStartMikaOnboarding(t, second); resp.Started {
		t.Fatalf("second call must report started=false, got %+v", resp)
	}
	if tasks := countSessionTasks(t, sessionID); tasks != 1 {
		t.Fatalf("after retry: expected still 1 task, got %d", tasks)
	}

	var messages int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE chat_session_id = $1`, sessionID,
	).Scan(&messages); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if messages != 1 {
		t.Fatalf("after retry: expected still 1 kickoff message, got %d", messages)
	}
}

func TestStartMikaOnboarding_RejectsBadInput(t *testing.T) {
	mikaID := markAsMika(t, createHandlerTestAgent(t, "Mika", nil))
	mikaSession := createHandlerTestChatSession(t, mikaID)
	cleanupSessionTasks(t, mikaSession)

	otherID := createHandlerTestAgent(t, "Not Mika", nil)
	otherSession := createHandlerTestChatSession(t, otherID)
	cleanupSessionTasks(t, otherSession)

	tests := []struct {
		name      string
		sessionID string
		body      any
	}{
		{"unsupported language", mikaSession, map[string]any{"language": "fr"}},
		{"missing language", mikaSession, map[string]any{}},
		{"agent without the mika system_key", otherSession, map[string]any{"language": "en"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := startMikaOnboarding(t, tc.sessionID, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}

	if tasks := countSessionTasks(t, mikaSession); tasks != 0 {
		t.Fatalf("rejected requests must not enqueue work, got %d task(s)", tasks)
	}
	if tasks := countSessionTasks(t, otherSession); tasks != 0 {
		t.Fatalf("rejected requests must not enqueue work, got %d task(s)", tasks)
	}
}
