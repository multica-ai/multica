package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestBuildDaemonWebSocketIdentitySeedsBatchRuntimeLeases(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	const daemonID = "lease-daemon"
	runtimeIDs := []string{
		dbfx.Runtime(t, "WS lease runtime 1", testutil.Cols{
			"workspace_id": testWorkspaceID,
			"daemon_id":    daemonID,
			"provider":     "ws-lease-1",
			"device_info":  "WS lease runtime 1",
		}),
		dbfx.Runtime(t, "WS lease runtime 2", testutil.Cols{
			"workspace_id": testWorkspaceID,
			"daemon_id":    daemonID,
			"provider":     "ws-lease-2",
			"device_info":  "WS lease runtime 2",
		}),
	}
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/ws", nil, testWorkspaceID, daemonID)
	req.Header.Set("X-Client-Version", "0.9.0")
	w := httptest.NewRecorder()

	identity, ok := testHandler.buildDaemonWebSocketIdentity(w, req, runtimeIDs, "")
	if !ok {
		t.Fatalf("buildDaemonWebSocketIdentity rejected valid runtimes: %d %s", w.Code, w.Body.String())
	}
	if identity.DaemonID != daemonID || identity.WorkspaceID != testWorkspaceID {
		t.Fatalf("identity scope = daemon %q workspace %q", identity.DaemonID, identity.WorkspaceID)
	}
	if identity.ClientVersion != "0.9.0" {
		t.Fatalf("client version = %q, want 0.9.0", identity.ClientVersion)
	}
	if len(identity.RuntimeLeases) != len(runtimeIDs) {
		t.Fatalf("runtime leases = %d, want %d", len(identity.RuntimeLeases), len(runtimeIDs))
	}
	for _, runtimeID := range runtimeIDs {
		lease := identity.RuntimeLeases[runtimeID]
		if lease == nil {
			t.Fatalf("missing lease for runtime %s", runtimeID)
		}
		state := lease.Snapshot()
		if state.WorkspaceID != testWorkspaceID || state.Status != "online" || !state.LastSeenAtValid {
			t.Fatalf("lease %s = %+v", runtimeID, state)
		}
		if time.Since(state.LastSeenAt) > time.Minute {
			t.Fatalf("lease %s last_seen_at unexpectedly stale: %s", runtimeID, state.LastSeenAt)
		}
	}
}

func TestBuildDaemonWebSocketIdentityFailsClosedForMissingRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/ws", nil, testWorkspaceID, "lease-daemon")
	w := httptest.NewRecorder()

	if _, ok := testHandler.buildDaemonWebSocketIdentity(w, req, []string{uuid.NewString()}, ""); ok {
		t.Fatal("missing runtime unexpectedly authorized")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestBuildDaemonWebSocketIdentityRejectsWrongDaemon(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "WS lease daemon scope", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "owner-daemon",
		"device_info":  "WS lease daemon scope",
	})
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/ws", nil, testWorkspaceID, "other-daemon")
	w := httptest.NewRecorder()

	if _, ok := testHandler.buildDaemonWebSocketIdentity(w, req, []string{runtimeID}, ""); ok {
		t.Fatal("runtime owned by another daemon unexpectedly authorized")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestBuildDaemonWebSocketIdentityRejectsCrossWorkspaceRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	workspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "WS Lease Other Workspace",
		"slug":         "ws-lease-" + uuid.NewString(),
		"description":  "Cross-workspace lease test",
		"issue_prefix": "HWL",
	})
	runtimeID := dbfx.Runtime(t, "WS lease cross workspace", testutil.Cols{
		"workspace_id": workspaceID,
		"daemon_id":    "lease-daemon",
		"device_info":  "WS lease cross workspace",
	})
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/ws", nil, testWorkspaceID, "lease-daemon")
	w := httptest.NewRecorder()

	if _, ok := testHandler.buildDaemonWebSocketIdentity(w, req, []string{runtimeID}, ""); ok {
		t.Fatal("cross-workspace runtime unexpectedly authorized")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}
