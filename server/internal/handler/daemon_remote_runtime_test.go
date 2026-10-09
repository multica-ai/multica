package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestDaemonRegisterExecutionMode(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, tc := range []struct {
		name, mode, want string
		status           int
	}{
		{"legacy", "", "local", http.StatusOK},
		{"remote", "cloud", "cloud", http.StatusOK},
		{"invalid", "unknown", "local", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			daemonID := uuid.NewString()
			id := dbfx.Runtime(t, "mode fixture", testutil.Cols{"daemon_id": daemonID, "provider": "qoder_cloud", "runtime_mode": "local"})
			req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
				"workspace_id": testWorkspaceID, "daemon_id": daemonID,
				"runtimes": []map[string]any{{"name": "Qoder Cloud Agent", "type": "qoder_cloud", "runtime_mode": tc.mode, "status": "online"}},
			}, testWorkspaceID, daemonID)
			testutil.Call(t, testHandler.DaemonRegister, req).Want(tc.status)
			var mode string
			dbfx.QueryRow(t, `SELECT runtime_mode FROM agent_runtime WHERE id=$1`, id).Scan(&mode)
			if mode != tc.want {
				t.Fatalf("runtime_mode=%q, want %q", mode, tc.want)
			}
		})
	}
}
