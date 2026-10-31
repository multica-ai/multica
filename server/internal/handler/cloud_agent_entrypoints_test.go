package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Quick-create is refused for every cloud agent provider before anything is
// persisted. Chat is refused for Claude Managed Agents, whose bridge runs
// issue runs only; Qoder Cloud Agents runs chat turns.
func TestCloudAgentChatAndQuickCreateRefused(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	for _, tc := range []struct {
		provider, name string
		chatRefused    bool
	}{
		{"claude_managed", "Claude Managed Agents", true},
		{"qoder_cloud", "Qoder Cloud Agents", false},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, tc.provider+" entrypoint runtime", testutil.Cols{"provider": tc.provider})
			agentID := dbfx.Agent(t, tc.provider+" entrypoint agent", runtimeID)
			sessionID := createHandlerTestChatSession(t, agentID)

			req := newRequest(http.MethodPost, "/api/chat-sessions/"+sessionID+"/messages", map[string]any{"content": "hello"})
			req = withChatTestWorkspaceCtx(t, withURLParam(req, "sessionId", sessionID))
			w := httptest.NewRecorder()
			testHandler.SendChatMessage(w, req)
			if tc.chatRefused {
				if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), tc.name) {
					t.Fatalf("SendChatMessage = %d %s, want 422 naming %s", w.Code, w.Body.String(), tc.name)
				}
				var messages int
				if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM chat_message WHERE chat_session_id = $1`, sessionID).Scan(&messages); err != nil {
					t.Fatal(err)
				}
				if messages != 0 {
					t.Fatalf("refused send persisted %d messages", messages)
				}
			} else if w.Code == http.StatusUnprocessableEntity {
				t.Fatalf("SendChatMessage refused a chat the bridge can run: %s", w.Body.String())
			}

			status, payload := testHandler.checkQuickCreateDaemonVersion(context.Background(), obsmetrics.RuntimeLookupSourceIssue, parseUUID(runtimeID))
			if status != http.StatusUnprocessableEntity || !strings.Contains(payload["reason"].(string), tc.name) {
				t.Fatalf("quick-create check = %d %v, want 422 naming %s", status, payload, tc.name)
			}
		})
	}
}
