package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

// TestDeviceApprovalRoutesRejectTaskToken runs the real Auth -> router ->
// handler chain: an agent's mat_ task token authenticates as the runtime
// owner, and must not be able to inspect or approve a device code (which
// would hand the owner's login to the requesting machine). The browser
// session (JWT) still approves. The client forges X-Actor-Source to prove
// Auth's server-set stamp wins, as in TestCredentialRoutesRejectTaskToken.
func TestDeviceApprovalRoutesRejectTaskToken(t *testing.T) {
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

	do := func(t *testing.T, path, bearer string, body any, out any) int {
		t.Helper()
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, testServer.URL+path, &buf)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Actor-Source", "member")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("perform request: %v", err)
		}
		defer resp.Body.Close()
		if out != nil {
			_ = json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}

	var start struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	if code := do(t, "/auth/device/start", "", map[string]string{"client_name": "build-runner-01"}, &start); code != http.StatusOK {
		t.Fatalf("start status = %d", code)
	}
	hash := auth.HashToken(start.DeviceCode)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM device_authorization WHERE device_code_hash = $1`, hash)
	})
	status := func() string {
		var s string
		if err := testPool.QueryRow(ctx, `SELECT status FROM device_authorization WHERE device_code_hash = $1`, hash).Scan(&s); err != nil {
			t.Fatalf("load authorization: %v", err)
		}
		return s
	}

	body := map[string]string{"user_code": start.UserCode}
	for _, path := range []string{"/api/device/inspect", "/api/device/approve"} {
		t.Run("mat_ "+path, func(t *testing.T) {
			if code := do(t, path, matToken, body, nil); code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", code)
			}
			if got := status(); got != "pending" {
				t.Fatalf("authorization status = %q, want pending", got)
			}
		})
	}

	t.Run("jwt approve", func(t *testing.T) {
		if code := do(t, "/api/device/inspect", testToken, body, nil); code != http.StatusOK {
			t.Fatalf("inspect status = %d, want 200", code)
		}
		if code := do(t, "/api/device/approve", testToken, body, nil); code != http.StatusOK {
			t.Fatalf("approve status = %d, want 200", code)
		}
		if got := status(); got != "approved" {
			t.Fatalf("authorization status = %q, want approved", got)
		}
	})
}
