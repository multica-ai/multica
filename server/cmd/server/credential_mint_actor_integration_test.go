package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

// TestCredentialRoutesRejectTaskToken exercises the real Auth -> router ->
// handler chain for MUL-8154: an agent's mat_ task token authenticates as the
// runtime owner, and must not be able to mint a session JWT or manage the
// owner's PATs. The client also forges X-Actor-Source to prove Auth's
// server-set stamp wins.
func TestCredentialRoutesRejectTaskToken(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load integration-test agent: %v", err)
	}
	taskID := ensureAgentTask(t, agentID)
	matToken := mintAgentTaskToken(t, agentID, taskID, testUserID)

	// A human PAT for the same user, so the revoke route has a real target
	// and we can assert it survives the rejected request.
	humanPAT, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatalf("generate pat: %v", err)
	}
	var patID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO personal_access_token (user_id, name, token_hash, token_prefix)
		VALUES ($1, 'mul-8154-target', $2, $3) RETURNING id::text
	`, testUserID, auth.HashToken(humanPAT), humanPAT[:12]).Scan(&patID); err != nil {
		t.Fatalf("insert pat: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE user_id = $1 AND name IN ('mul-8154-target', 'mul-8154-escalation', 'mul-8154-human')`, testUserID)
	})

	countPATs := func() int {
		var n int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM personal_access_token WHERE user_id = $1 AND NOT revoked`, testUserID).Scan(&n); err != nil {
			t.Fatalf("count pats: %v", err)
		}
		return n
	}

	do := func(t *testing.T, method, path, bearer string, body any) *http.Response {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				t.Fatalf("encode body: %v", err)
			}
		}
		req, err := http.NewRequest(method, testServer.URL+path, &buf)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Actor-Source", "member")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("perform request: %v", err)
		}
		return resp
	}

	before := countPATs()
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{name: "cli-token", method: http.MethodPost, path: "/api/cli-token"},
		{name: "create pat", method: http.MethodPost, path: "/api/tokens", body: map[string]any{"name": "mul-8154-escalation"}},
		{name: "list pats", method: http.MethodGet, path: "/api/tokens"},
		{name: "revoke pat", method: http.MethodDelete, path: "/api/tokens/" + patID},
		{name: "renew current", method: http.MethodPost, path: "/api/tokens/current/renew"},
	} {
		t.Run("mat_ "+tc.name, func(t *testing.T) {
			resp := do(t, tc.method, tc.path, matToken, tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
		})
	}
	if after := countPATs(); after != before {
		t.Fatalf("active PAT count changed %d -> %d; rejected requests must have no side effect", before, after)
	}

	// Human credentials keep working through the same chain.
	t.Run("jwt cli-token", func(t *testing.T) {
		resp := do(t, http.MethodPost, "/api/cli-token", testToken, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})
	t.Run("jwt create pat", func(t *testing.T) {
		resp := do(t, http.MethodPost, "/api/tokens", testToken, map[string]any{"name": "mul-8154-human"})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201", resp.StatusCode)
		}
	})
	t.Run("mul_ list pats", func(t *testing.T) {
		resp := do(t, http.MethodGet, "/api/tokens", humanPAT, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})
	t.Run("mul_ renew current", func(t *testing.T) {
		resp := do(t, http.MethodPost, "/api/tokens/current/renew", humanPAT, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})
}
