package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Autopilot writes are judged as the human the caller acts for, and for an
// agent caller that human is the run's ORIGINATOR (MUL-7108). That makes the
// question "is this really an agent?" load-bearing, so this test pins the
// answer at the router: only a server-issued mat_ task token makes a request an
// agent's. A member replaying an observable (agent_id, task_id) pair — both are
// returned by GET /api/issues/{id}/task-runs — stays themselves (MUL-3428).
//
// The two halves are deliberately mirror images: the SAME live task, whose
// originator may write this autopilot, is reached once by forgery and once by
// the real credential. Only the credential differs, so the opposite outcomes
// can be explained by nothing else.
func TestAutopilotWritesIgnoreForgedAgentIdentity(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)

	var agentID, runtimeID string
	fx.QueryRow(t, `SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`, testWorkspaceID).
		Scan(&agentID, &runtimeID)

	// A live run whose originator is the workspace owner — a writer on the
	// autopilot below. This is the identity a forger wants to borrow.
	taskID := fx.Insert(t, "agent_task_queue", testutil.Cols{
		"agent_id": agentID, "runtime_id": runtimeID, "status": "running", "priority": 0,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
		"originator_source": "direct_human",
	})

	autopilotID := fx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": testWorkspaceID, "title": "forged actor regression",
		"assignee_id": agentID, "assignee_type": "agent", "execution_mode": "run_only",
		"status": "active", "created_by_type": "member", "created_by_id": testUserID,
	})
	fx.Cleanup(t, `DELETE FROM autopilot_rule_version WHERE autopilot_id = $1`, autopilotID)
	fx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id": autopilotID, "kind": "webhook", "enabled": true, "provider": "generic",
		"webhook_token":   fmt.Sprintf("tok_forgery_%d", time.Now().UnixNano()),
		"created_by_type": "member", "created_by_id": testUserID,
	})

	// The forger: a workspace member holding no grant on this autopilot.
	email := fmt.Sprintf("autopilot-forger-%d@multica.test", time.Now().UnixNano())
	outsider := fx.User(t, "Autopilot Forger", email)
	fx.Member(t, testWorkspaceID, outsider, "member")
	outsiderJWT, err := generateTestJWT(outsider, email, "Autopilot Forger")
	if err != nil {
		t.Fatalf("outsider jwt: %v", err)
	}

	do := func(t *testing.T, method, path string, body any, decorate func(*http.Request)) *http.Response {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		} else {
			reader = bytes.NewReader(nil)
		}
		req, err := http.NewRequest(method, testServer.URL+path, reader)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		decorate(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		return resp
	}
	forged := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+outsiderJWT)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		// The forgery: a real, member-readable pair replayed as headers.
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", taskID)
	}

	t.Run("forged agent identity is judged as the member", func(t *testing.T) {
		resp := do(t, "GET", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, nil, forged)
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("detail status = %d, want 200", resp.StatusCode)
		}
		var detail struct {
			Autopilot handler.AutopilotResponse          `json:"autopilot"`
			Triggers  []handler.AutopilotTriggerResponse `json:"triggers"`
		}
		readJSON(t, resp, &detail)
		if detail.Autopilot.CanWrite == nil || *detail.Autopilot.CanWrite {
			t.Errorf("can_write = %t, want false: the forged pair must not lend this member the originator's grant",
				detail.Autopilot.CanWrite != nil && *detail.Autopilot.CanWrite)
		}
		for _, tr := range detail.Triggers {
			if tr.WebhookToken != nil || tr.WebhookPath != nil || tr.WebhookURL != nil {
				t.Error("the forged pair yielded the webhook credential, which is a trigger this member cannot otherwise fire")
			}
		}

		writeResp := do(t, "PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID,
			map[string]any{"status": "paused"}, forged)
		defer writeResp.Body.Close()
		if writeResp.StatusCode != http.StatusForbidden {
			t.Fatalf("update status = %d, want 403 — a member must not write through a borrowed agent identity", writeResp.StatusCode)
		}
	})

	t.Run("a real task token is judged by its originator", func(t *testing.T) {
		// Bound to the OUTSIDER as runtime owner: the machine's owner holds no
		// grant here, so admission can only come from the task's originator.
		token := mintAgentTaskToken(t, agentID, taskID, outsider)
		resp := do(t, "PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID,
			map[string]any{"status": "paused"}, func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+token)
			})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("update status = %d, want 200 — the ordering human may write this autopilot", resp.StatusCode)
		}
	})
}
