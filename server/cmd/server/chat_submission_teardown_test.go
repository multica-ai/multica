package main

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestChatSubmissionRuntimeUnbindKeepsUserReceipts(t *testing.T) {
	fx := workspaceFixture(t)
	runtimeID := fx.Runtime(t, "Submission runtime teardown", testutil.Cols{"owner_id": testUserID})
	agentID := fx.Agent(t, "Persistent submission agent", runtimeID)
	created := submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", "/api/chat/sessions", map[string]any{"agent_id": agentID}, http.StatusCreated)
	sessionID := created["id"].(string)
	cleanupSubmissionSession(t, testToken, testWorkspaceID, sessionID)
	userBody := map[string]any{"content": "keep accepted user turn", "submission_id": uuid.NewString()}
	accepted := submissionSend(t, sessionID, userBody)
	builder := submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", "/api/agent-builder/sessions", map[string]any{"runtime_id": runtimeID}, http.StatusCreated)
	builderSession := builder["session_id"].(string)
	cleanupSubmissionSession(t, testToken, testWorkspaceID, builderSession)
	submissionSend(t, builderSession, map[string]any{"content": "accepted builder turn", "submission_id": uuid.NewString()})
	// Both active user agents and runtime-local system carriers participate in
	// the same production teardown. The confirmed snapshot contains user agents.
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", "/api/runtimes/"+runtimeID+"/unbind-agents-and-delete", map[string]any{"expected_active_agent_ids": []string{agentID}}, http.StatusOK)
	submissionCounts(t, builderSession, 0, 0, 0)
	if count := fx.Count(t, `SELECT count(*) FROM chat_submission_receipt WHERE chat_session_id=$1`, sessionID); count != 1 {
		t.Fatalf("surviving user session receipt count=%d", count)
	}
	if replay := submissionSend(t, sessionID, userBody); !reflect.DeepEqual(accepted, replay) {
		t.Fatalf("unbinding changed historical acceptance %v", replay)
	}
	var unbound bool
	fx.QueryRow(t, `SELECT runtime_id IS NULL FROM agent WHERE id=$1`, agentID).Scan(&unbound)
	if !unbound {
		t.Fatal("user agent did not survive unbound")
	}
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "DELETE", "/api/chat/sessions/"+sessionID, nil, http.StatusNoContent)
	submissionCounts(t, sessionID, 0, 0, 0)
}

func TestChatSubmissionProfileAndGCTeardown(t *testing.T) {
	for _, path := range []string{"profile", "gc"} {
		t.Run(path, func(t *testing.T) {
			fx := workspaceFixture(t)
			runtimeID := fx.Runtime(t, "Submission "+path+" runtime", testutil.Cols{"owner_id": testUserID})
			var profileID string
			if path == "profile" {
				profile := submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles", map[string]any{
					"display_name": "Submission profile " + uuid.NewString(), "protocol_family": "codex", "command_name": "offline-submission-test",
				}, http.StatusCreated)
				profileID = profile["id"].(string)
				fx.Exec(t, `UPDATE agent_runtime SET profile_id=$2 WHERE id=$1`, runtimeID, profileID)
			}
			builder := submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", "/api/agent-builder/sessions", map[string]any{"runtime_id": runtimeID}, http.StatusCreated)
			sessionID := builder["session_id"].(string)
			cleanupSubmissionSession(t, testToken, testWorkspaceID, sessionID)
			accepted := submissionSend(t, sessionID, map[string]any{"content": "retired builder acceptance", "submission_id": uuid.NewString()})
			submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", "/api/tasks/"+accepted["task_id"].(string)+"/cancel", nil, http.StatusOK)
			submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "PATCH", "/api/chat/sessions/"+sessionID+"/archive", map[string]any{"archived": true}, http.StatusOK)
			fx.Exec(t, `UPDATE agent_runtime SET status='offline', last_seen_at=now()-interval '10 days' WHERE id=$1`, runtimeID)
			if path == "profile" {
				submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "DELETE", "/api/workspaces/"+testWorkspaceID+"/runtime-profiles/"+profileID, nil, http.StatusNoContent)
			} else {
				result, err := gcRuntime(context.Background(), testPool, db.New(testPool), parseUUID(runtimeID))
				if err != nil || !result.deleted {
					t.Fatalf("shared GC teardown result=%+v error=%v", result, err)
				}
			}
			submissionCounts(t, sessionID, 0, 0, 0)
			if count := fx.Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, sessionID); count != 0 {
				t.Fatal("retired builder session survived its parent")
			}
		})
	}
}
