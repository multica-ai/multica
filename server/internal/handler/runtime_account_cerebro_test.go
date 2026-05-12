// CEREBRO-PATCH(runtime-account-cerebro-test): cerebro-only unit test for
// the heartbeat account hook. Net-new fork file in the upstream-zone path.
// Stays in-package (handler) by using a mock RuntimeAccountInvoker so we
// don't need to import the cerebro/runtime tree (which would cycle).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// mockRuntimeAccountInvoker captures the RecordAccount call so the
// heartbeat-hook tests can assert what the upstream handler routed
// through the seam. Returns a configurable record or error.
type mockRuntimeAccountInvoker struct {
	calls   []mockRecordCall
	respond func(call mockRecordCall) (RuntimeAccountRecord, error)
}

type mockRecordCall struct {
	RuntimeID     pgtype.UUID
	WorkspaceID   pgtype.UUID
	Provider      string
	LoginIdentity string
}

func (m *mockRuntimeAccountInvoker) RecordAccount(_ context.Context, runtimeID, workspaceID pgtype.UUID, provider, identity string) (RuntimeAccountRecord, error) {
	call := mockRecordCall{
		RuntimeID:     runtimeID,
		WorkspaceID:   workspaceID,
		Provider:      provider,
		LoginIdentity: identity,
	}
	m.calls = append(m.calls, call)
	if m.respond != nil {
		return m.respond(call)
	}
	return RuntimeAccountRecord{Provider: provider, LoginIdentity: identity}, nil
}

func newHandlerForAccountHook(mock *mockRuntimeAccountInvoker) *Handler {
	return &Handler{RuntimeAccount: mock}
}

// TestRecordHeartbeatAccount_HappyPath pins the JEH-997 contract: a beat
// carrying a fully-populated Account payload routes to RuntimeAccount.Record-
// Account with the runtime + workspace from the resolved runtime row.
func TestRecordHeartbeatAccount_HappyPath(t *testing.T) {
	mock := &mockRuntimeAccountInvoker{}
	h := newHandlerForAccountHook(mock)

	rt := db.AgentRuntime{ID: pgtype.UUID{Valid: true}, WorkspaceID: pgtype.UUID{Valid: true}}
	h.recordHeartbeatAccount(context.Background(), rt, &protocol.DaemonHeartbeatAccount{
		Provider:      "claude",
		LoginIdentity: "sara@firtal.dk",
	})

	if len(mock.calls) != 1 {
		t.Fatalf("RecordAccount calls = %d, want 1", len(mock.calls))
	}
	if got := mock.calls[0]; got.Provider != "claude" || got.LoginIdentity != "sara@firtal.dk" {
		t.Errorf("RecordAccount args mismatch: %+v", got)
	}
}

// TestRecordHeartbeatAccount_NilAccountSkips verifies the "daemon couldn't
// detect an identity yet" branch: nil Account must not call the recorder.
// Without this, every beat with auth-state missing would attempt a DB
// upsert with empty strings (and fail validation server-side).
func TestRecordHeartbeatAccount_NilAccountSkips(t *testing.T) {
	mock := &mockRuntimeAccountInvoker{}
	h := newHandlerForAccountHook(mock)

	h.recordHeartbeatAccount(context.Background(), db.AgentRuntime{}, nil)

	if len(mock.calls) != 0 {
		t.Fatalf("expected no calls for nil Account, got %d", len(mock.calls))
	}
}

// TestRecordHeartbeatAccount_HalfFilledSkips pins the defensive guard for a
// client bug — a daemon that somehow sends Account with empty fields.
// Recording would surface the handler.ErrRuntimeAccount* sentinels and
// log noise on every beat; better to swallow it.
func TestRecordHeartbeatAccount_HalfFilledSkips(t *testing.T) {
	mock := &mockRuntimeAccountInvoker{}
	h := newHandlerForAccountHook(mock)

	cases := []*protocol.DaemonHeartbeatAccount{
		{Provider: "", LoginIdentity: "x"},
		{Provider: "claude", LoginIdentity: ""},
		{Provider: "", LoginIdentity: ""},
	}
	for _, acc := range cases {
		h.recordHeartbeatAccount(context.Background(), db.AgentRuntime{}, acc)
	}
	if len(mock.calls) != 0 {
		t.Fatalf("half-filled Account must skip recorder, got %d calls", len(mock.calls))
	}
}

// TestRecordHeartbeatAccount_NilInvokerSilent pins that the heartbeat path
// is safe even when the cerebro runtime account service was not wired (e.g.
// upstream-only deployments, or before router init completes). The hook
// must be a no-op rather than a nil-pointer panic.
func TestRecordHeartbeatAccount_NilInvokerSilent(t *testing.T) {
	h := &Handler{} // RuntimeAccount intentionally unset
	h.recordHeartbeatAccount(context.Background(), db.AgentRuntime{}, &protocol.DaemonHeartbeatAccount{
		Provider:      "claude",
		LoginIdentity: "x@y.z",
	})
}

// TestRecordHeartbeatAccount_RecorderErrorSwallowed pins the best-effort
// invariant: a DB hiccup inside RecordAccount must not propagate up to
// the heartbeat ack. The daemon retries on the next tick.
func TestRecordHeartbeatAccount_RecorderErrorSwallowed(t *testing.T) {
	mock := &mockRuntimeAccountInvoker{
		respond: func(mockRecordCall) (RuntimeAccountRecord, error) {
			return RuntimeAccountRecord{}, errors.New("transient db failure")
		},
	}
	h := newHandlerForAccountHook(mock)

	// If the call panicked or returned an error, the test would fail; we
	// intentionally call as a void function — that IS the contract.
	h.recordHeartbeatAccount(context.Background(), db.AgentRuntime{}, &protocol.DaemonHeartbeatAccount{
		Provider:      "claude",
		LoginIdentity: "sara@firtal.dk",
	})
}

// TestDaemonHeartbeat_RoutesAccountToInvoker is the JEH-997 integration
// test: an HTTP heartbeat carrying an Account payload must invoke the
// upstream-side recorder seam with the runtime + workspace it resolved
// from the auth context. This pins option A (heartbeat-embedded) as the
// transport — the same Account field travels over both HTTP and WS, and
// the handler unwraps it identically.
func TestDaemonHeartbeat_RoutesAccountToInvoker(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// First, register a runtime so the heartbeat has something to ack.
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "test-daemon-hb-account",
		"device_name":  "test-device",
		"runtimes": []map[string]any{
			{"name": "test-rt-hb-account", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	}, testWorkspaceID, "test-daemon-hb-account")
	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup DaemonRegister: %d: %s", w.Code, w.Body.String())
	}
	var regResp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&regResp)
	runtimeID := regResp["runtimes"].([]any)[0].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// Install a mock invoker so the test does not depend on the cerebro
	// runtime package being wired into testHandler.
	mock := &mockRuntimeAccountInvoker{}
	prev := testHandler.RuntimeAccount
	testHandler.RuntimeAccount = mock
	t.Cleanup(func() { testHandler.RuntimeAccount = prev })

	// Now hit the heartbeat endpoint with the Account payload.
	w = httptest.NewRecorder()
	req = newDaemonTokenRequest("POST", "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
		"account":    map[string]any{"provider": "claude", "login_identity": "sara@firtal.dk"},
	}, testWorkspaceID, "test-daemon-hb-account")
	testHandler.DaemonHeartbeat(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DaemonHeartbeat: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if len(mock.calls) != 1 {
		t.Fatalf("RuntimeAccount.RecordAccount calls = %d, want 1", len(mock.calls))
	}
	got := mock.calls[0]
	if got.Provider != "claude" || got.LoginIdentity != "sara@firtal.dk" {
		t.Errorf("recorder args mismatch: %+v", got)
	}
}

// TestDaemonHeartbeat_OmittedAccountSkipsRecorder verifies that a vanilla
// heartbeat (no Account field) flows through untouched. Daemons that have
// not yet detected an identity must not trigger a recorder call.
func TestDaemonHeartbeat_OmittedAccountSkipsRecorder(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "test-daemon-hb-noaccount",
		"device_name":  "test-device",
		"runtimes": []map[string]any{
			{"name": "test-rt-hb-noaccount", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	}, testWorkspaceID, "test-daemon-hb-noaccount")
	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup DaemonRegister: %d: %s", w.Code, w.Body.String())
	}
	var regResp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&regResp)
	runtimeID := regResp["runtimes"].([]any)[0].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	mock := &mockRuntimeAccountInvoker{}
	prev := testHandler.RuntimeAccount
	testHandler.RuntimeAccount = mock
	t.Cleanup(func() { testHandler.RuntimeAccount = prev })

	w = httptest.NewRecorder()
	req = newDaemonTokenRequest("POST", "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
	}, testWorkspaceID, "test-daemon-hb-noaccount")
	testHandler.DaemonHeartbeat(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DaemonHeartbeat: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(mock.calls) != 0 {
		t.Fatalf("expected no recorder calls when Account omitted, got %d", len(mock.calls))
	}
}
