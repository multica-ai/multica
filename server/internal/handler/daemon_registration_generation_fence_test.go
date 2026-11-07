package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestDaemonRegister_FreshHandlerStaleGenerationReturnsConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	daemonID := "fresh-generation-" + uuid.NewString()
	runtimeID := dbfx.Runtime(t, "fresh generation runtime", testutil.Cols{
		"daemon_id": daemonID,
		"provider":  "codex",
		"owner_id":  testUserID,
	})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET metadata = '{"owner_generation":"g2"}'::jsonb WHERE id = $1`, runtimeID); err != nil {
		t.Fatal(err)
	}

	h := *testHandler
	h.LivenessStore = nil
	h.runtimeOwnerGate = &localRuntimeOwnerGate{}
	w := httptest.NewRecorder()
	h.DaemonRegister(w, newRequestAs(testUserID, http.MethodPost, "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    daemonID,
		"runtimes": []map[string]any{{
			"name":             "stale",
			"type":             "codex",
			"status":           "online",
			"owner_generation": "g1",
		}},
	}))
	if w.Code != http.StatusConflict {
		t.Fatalf("fresh handler stale registration = %d: %s, want 409", w.Code, w.Body.String())
	}
	key := runtimeOwnerGateKey(testWorkspaceID, daemonID, "codex", "")
	if h.runtimeOwnerGate.current(key, "g1") {
		t.Fatal("stale registration advanced the owner gate")
	}
}

func TestDaemonRegister_FailedRegistrationLeavesOwnerGateUnchanged(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	daemonID := "failed-generation-" + uuid.NewString()
	profileID := uuid.NewString()
	h := *testHandler
	h.LivenessStore = nil
	h.runtimeOwnerGate = &localRuntimeOwnerGate{}
	w := httptest.NewRecorder()
	h.DaemonRegister(w, newRequestAs(testUserID, http.MethodPost, "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    daemonID,
		"failed_profiles": []map[string]any{{
			"profile_id":       profileID,
			"owner_generation": "g7",
			"reason":           "missing command",
		}},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("failed registration = %d: %s, want 200", w.Code, w.Body.String())
	}
	key := runtimeOwnerGateKey(testWorkspaceID, daemonID, "", profileID)
	if h.runtimeOwnerGate.current(key, "g7") {
		t.Fatal("failed registration advanced the owner gate")
	}
}
