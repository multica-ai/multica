package main

import (
	"context"
	"net/http"
	"testing"
)

// Scan install creates and binds a bot, so it is gated where the manual install
// and the revoke are: workspace owners and admins. An agent's owner who is a
// plain member is refused at the route, the same as for connecting a bot by
// hand, and could not disconnect one it had created anyway.
//
// Through NewRouter, because the gate is the route group the handlers are
// registered in: a handler test would call past it.
//
// REVERSE VERIFICATION: register begin and status in the member group again,
// and the member's requests reach the handlers instead of a 403.
func TestWecomScanInstallIsForWorkspaceAdmins(t *testing.T) {
	ctx := context.Background()

	var memberID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Scan Agent Owner', 'scan-agent-owner@multica.test')
		RETURNING id`).Scan(&memberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberID)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, memberID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		SELECT $1, 'Scan Owner Agent', '', 'cloud', '{}'::jsonb, id, 'workspace', 1, $2
		FROM agent_runtime WHERE workspace_id = $1 LIMIT 1
		RETURNING id`, testWorkspaceID, memberID).Scan(&agentID); err != nil {
		t.Fatalf("create the member's agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO wecom_install_session (request_key_hash, workspace_id, agent_id, initiator_user_id, status, error_reason)
		VALUES ('scan-permission', $1, $2, $3, 'error', 'expired')
		RETURNING id::text`, testWorkspaceID, agentID, testUserID).Scan(&sessionID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM wecom_install_session WHERE id = $1`, sessionID)
	})
	memberToken, err := generateTestJWT(memberID, "scan-agent-owner@multica.test", "Scan Agent Owner")
	if err != nil {
		t.Fatalf("member token: %v", err)
	}

	base := testServer.URL + "/api/workspaces/" + testWorkspaceID + "/wecom/install/"
	do := func(t *testing.T, token, method, url string) int {
		t.Helper()
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		req.Header.Set("Idempotency-Key", "scan-permission-"+method)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, url, err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	begin := base + "begin?agent_id=" + agentID
	status := base + sessionID

	t.Run("the agent's owner, a plain member, is refused", func(t *testing.T) {
		if got := do(t, memberToken, http.MethodPost, begin); got != http.StatusForbidden {
			t.Errorf("begin = %d, want 403", got)
		}
		if got := do(t, memberToken, http.MethodGet, status); got != http.StatusForbidden {
			t.Errorf("status = %d, want 403", got)
		}
	})

	// Past the gate the handler answers: 503 when this server has no scan
	// install configured, otherwise the begin's 202 and the session's 200.
	t.Run("a workspace admin reaches the handlers", func(t *testing.T) {
		if got := do(t, testToken, http.MethodPost, begin); got != http.StatusAccepted && got != http.StatusServiceUnavailable {
			t.Errorf("begin = %d, want 202, or 503 with scan install off", got)
		}
		if got := do(t, testToken, http.MethodGet, status); got != http.StatusOK && got != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 200, or 503 with scan install off", got)
		}
	})
}
