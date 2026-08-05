package main

import (
	"context"
	"net/http"
	"testing"
)

// The handler-level tests for ContinueTaskInChat call the handler function
// directly and inject the workspace/member context by hand. That deliberately
// skips everything in front of the handler — route registration, JWT auth, the
// workspace-context middleware — so a route that was never wired, or wired
// outside the authenticated group, would still pass every one of them.
//
// This exercises the real chi router built by NewRouter over HTTP with a real
// bearer token, so it fails if:
//   - POST /api/tasks/{taskId}/continue-in-chat is not registered (404 on a
//     valid task id),
//   - it is registered outside the auth group (an unauthenticated call would
//     succeed instead of 401),
//   - the workspace-context middleware does not populate what the handler reads
//     (which surfaces as a 400 "invalid workspace id" rather than a 2xx).
func TestContinueTaskInChatRouteIsWiredAndAuthenticated(t *testing.T) {
	if testServer == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Own runtime + agent rather than reusing whatever the shared fixture's
	// `agent WHERE workspace_id = ... LIMIT 1` happens to return. Sibling tests in
	// this package create and archive agents in the same workspace, so borrowing
	// an arbitrary row made this test order-dependent: picking up an archived
	// agent turned the expected 201 into a 400. Seed a dedicated pair so the test
	// is hermetic.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Continue Chat Route Runtime', 'cloud', 'continue_chat_route_runtime', 'online', 'route test', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Continue Chat Route Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	// Workspace-invocable, so canInvokeAgent admits the requesting member.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("seed invocation target: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, session_id, work_dir, completed_at)
		VALUES ($1, $2, 'completed', 0, 'sess-route-test', '/work/route-test', now())
		RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create terminal task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	path := "/api/tasks/" + taskID + "/continue-in-chat"

	// Unauthenticated: the route must sit behind auth, not in front of it.
	unauth, err := http.Post(testServer.URL+path, "application/json", nil)
	if err != nil {
		t.Fatalf("unauthenticated request failed: %v", err)
	}
	defer unauth.Body.Close()
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated POST %s = %d, want 401", path, unauth.StatusCode)
	}

	// Authenticated: reaches the handler and creates the continuation.
	resp := authRequest(t, "POST", path, nil)
	var body struct {
		ChatSession struct {
			ID                  string  `json:"id"`
			ContinuedFromTaskID *string `json:"continued_from_task_id"`
		} `json:"chat_session"`
		Reopened       bool `json:"reopened"`
		SessionCarried bool `json:"session_carried"`
		WorkDirCarried bool `json:"work_dir_carried"`
	}
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		t.Fatalf("authenticated POST %s = %d, want 201 (a 404 here means the route is not registered; "+
			"a 400 means the workspace context middleware did not populate what the handler reads)",
			path, resp.StatusCode)
	}
	readJSON(t, resp, &body)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, body.ChatSession.ID)
	})

	if body.ChatSession.ID == "" {
		t.Fatal("response carried no chat session id")
	}
	if !body.SessionCarried || !body.WorkDirCarried {
		t.Errorf("session_carried=%v work_dir_carried=%v, want both true",
			body.SessionCarried, body.WorkDirCarried)
	}
	if body.ChatSession.ContinuedFromTaskID == nil || *body.ChatSession.ContinuedFromTaskID != taskID {
		t.Errorf("continued_from_task_id = %v, want %s", body.ChatSession.ContinuedFromTaskID, taskID)
	}

	// The seeded row must actually carry the resume pointer end to end, not just
	// echo it in the response.
	var sessionID, workDir, storedRuntime string
	if err := testPool.QueryRow(ctx, `
		SELECT session_id, work_dir, runtime_id FROM chat_session WHERE id = $1
	`, body.ChatSession.ID).Scan(&sessionID, &workDir, &storedRuntime); err != nil {
		t.Fatalf("read created chat session: %v", err)
	}
	if sessionID != "sess-route-test" || workDir != "/work/route-test" {
		t.Errorf("stored pointer = (%q, %q), want (sess-route-test, /work/route-test)", sessionID, workDir)
	}
	if storedRuntime != runtimeID {
		t.Errorf("stored runtime_id = %s, want the task's runtime %s", storedRuntime, runtimeID)
	}

	// Second call over the same route reopens rather than forking.
	again := authRequest(t, "POST", path, nil)
	defer again.Body.Close()
	if again.StatusCode != http.StatusOK {
		t.Errorf("second POST %s = %d, want 200 (reopen)", path, again.StatusCode)
	}
}
