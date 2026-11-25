package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCodebuddyProtocolSummaryResumeQueries(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	const summary = "\n[codebuddy protocol: exit_code=0 stdout_bytes=1400 stderr_bytes=0 event_count=1 invalid_event_count=0 assistant_event_count=0 tool_use_count=0 saw_result=true result_is_error=true result_bytes=0 last_assistant_bytes=0 scanner_error=false]"
	ctx := context.Background()
	for _, tc := range []struct {
		name, reason, raw, wantSession string
	}{
		{"model", "agent_error.model_not_found_or_unavailable", `HTTP 404 {"type":"invalid_request_error","code":"model_not_found"}`, "current"},
		{"auth", "agent_error.provider_auth_or_access", `HTTP 401 {"type":"invalid_request_error","code":"invalid_api_key"}`, "current"},
		{"actual poisoned request", "agent_error.unknown", `HTTP 400 {"type":"invalid_request_error","message":"Could not process image"}`, "healthy"},
	} {
		for _, scope := range []string{"issue", "chat"} {
			t.Run(tc.name+"/"+scope, func(t *testing.T) {
				agentID, runtimeID, _ := createRuntimeGuardAgent(t, ctx)
				column := "issue_id"
				var scopeID string
				if scope == "issue" {
					scopeID = dbfx.Issue(t, "CodeBuddy diagnostic resume")
				} else {
					column = "chat_session_id"
					scopeID = dbfx.ChatSession(t, agentID)
				}
				dbfx.Task(t, agentID, testutil.Cols{
					column: scopeID, "runtime_id": runtimeID, "session_id": "healthy", "status": "completed",
					"completed_at": testutil.Raw("now() - interval '2 minutes'"),
				})
				dbfx.Task(t, agentID, testutil.Cols{
					column: scopeID, "runtime_id": runtimeID, "session_id": "current", "status": "failed",
					"error": tc.raw + summary, "failure_reason": tc.reason,
					"completed_at": testutil.Raw("now() - interval '1 minute'"),
				})
				if scope == "issue" {
					got, err := testHandler.Queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{AgentID: parseUUID(agentID), IssueID: parseUUID(scopeID)})
					if err != nil || got.SessionID.String != tc.wantSession {
						t.Fatalf("issue session=%q, err=%v, want %q", got.SessionID.String, err, tc.wantSession)
					}
				} else {
					got, err := testHandler.Queries.GetLastChatTaskSession(ctx, db.GetLastChatTaskSessionParams{ChatSessionID: parseUUID(scopeID)})
					if err != nil || got.SessionID.String != tc.wantSession {
						t.Fatalf("chat session=%q, err=%v, want %q", got.SessionID.String, err, tc.wantSession)
					}
				}
			})
		}
	}
}
