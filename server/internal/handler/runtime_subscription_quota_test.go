package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRuntimeSubscriptionQuotaOwnerAndRefresh(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "quota test", testutil.Cols{
		"provider": "codex", "runtime_mode": "local", "visibility": "public", "daemon_id": "quota-test-daemon",
	})
	other := dbfx.User(t, "Quota admin", "quota-admin@example.test")
	dbfx.Member(t, testWorkspaceID, other, "admin")
	path := "/api/runtimes/" + runtimeID + "/subscription-quota"
	call := func(handler http.HandlerFunc, req *http.Request, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, want %d: %s", req.Method, req.URL, w.Code, want, w.Body.String())
		}
		return w
	}
	read := func(handler http.HandlerFunc, method, target, status string) protocol.SubscriptionQuotaReport {
		t.Helper()
		req := withURLParam(newRequest(method, target, nil), "runtimeId", runtimeID)
		w := call(handler, req, http.StatusOK)
		var report protocol.SubscriptionQuotaReport
		if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil || report.Status != status {
			t.Fatalf("expected %s quota: %s (%v)", status, w.Body.String(), err)
		}
		return report
	}

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := withURLParam(newRequestAs(other, method, path, nil), "runtimeId", runtimeID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		handler := testHandler.GetRuntimeSubscriptionQuota
		if method == http.MethodPost {
			handler = testHandler.RequestRuntimeSubscriptionQuota
		}
		call(handler, req, http.StatusNotFound)
	}

	read(testHandler.GetRuntimeSubscriptionQuota, http.MethodGet, path, "unsupported")
	read(testHandler.RequestRuntimeSubscriptionQuota, http.MethodPost, path, "unsupported")
	if testHandler.pendingRuntimeSubscriptionQuota(ctx, runtimeID) != "" {
		t.Fatal("an older daemon must not get a quota request")
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata = $1 WHERE id = $2`,
		map[string]any{"capabilities": []string{protocol.DaemonCapabilitySubscriptionQuotaV1}}, runtimeID)

	read(testHandler.RequestRuntimeSubscriptionQuota, http.MethodPost, path, "pending")
	heartbeat := func(supported bool) protocol.DaemonHeartbeatAckPayload {
		t.Helper()
		req := newOwnerDaemonRequest(http.MethodPost, "/api/daemon/heartbeat", DaemonHeartbeatRequest{
			RuntimeID: runtimeID, SupportsSubscriptionQuota: supported,
		})
		w := call(testHandler.DaemonHeartbeat, req, http.StatusOK)
		var ack protocol.DaemonHeartbeatAckPayload
		if err := json.Unmarshal(w.Body.Bytes(), &ack); err != nil {
			t.Fatal(err)
		}
		return ack
	}
	if heartbeat(false).PendingSubscriptionQuota != "" {
		t.Fatal("older daemons must not claim quota work")
	}
	requestID := heartbeat(true).PendingSubscriptionQuota
	if requestID == "" || heartbeat(true).PendingSubscriptionQuota != requestID {
		t.Fatal("a lost heartbeat ACK must leave the same request available")
	}
	read(testHandler.RequestRuntimeSubscriptionQuota, http.MethodPost, path, "pending")

	reportResult := func(id, daemon string, report protocol.SubscriptionQuotaReport, want int) {
		t.Helper()
		req := newDaemonTokenRequest(http.MethodPost, "/result", report, testWorkspaceID, daemon)
		route := chi.NewRouteContext()
		route.URLParams.Add("runtimeId", runtimeID)
		route.URLParams.Add("requestId", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
		call(testHandler.ReportRuntimeSubscriptionQuota, req, want)
	}
	used := 42.0
	snapshot := &protocol.SubscriptionQuotaSnapshot{
		ObservedAt: time.Now().Unix(),
		Primary:    &protocol.SubscriptionQuotaWindow{UsedPercent: &used},
	}
	report := protocol.SubscriptionQuotaReport{Status: protocol.SubscriptionQuotaComplete, Snapshot: snapshot}
	reportResult(requestID, "different-daemon", report, http.StatusNotFound)
	reportResult("expired-id", "quota-test-daemon", report, http.StatusOK)
	read(testHandler.GetRuntimeSubscriptionQuota, http.MethodGet, path, "pending")
	reportResult(requestID, "quota-test-daemon", report, http.StatusOK)
	if heartbeat(true).PendingSubscriptionQuota != "" {
		t.Fatal("completed refresh must stop being delivered")
	}

	got := read(testHandler.RequestRuntimeSubscriptionQuota, http.MethodPost, path+"?if_stale=true", "complete")
	if got.Snapshot == nil || got.Snapshot.Primary == nil || *got.Snapshot.Primary.UsedPercent != used {
		t.Fatalf("snapshot not persisted: %+v", got.Snapshot)
	}
	if _, err := testHandler.quotaStore.update(ctx, runtimeID, func(state runtimeQuotaState) (runtimeQuotaState, bool) {
		state.RequestedAt = time.Now().Add(-10 * time.Second)
		return state, true
	}); err != nil {
		t.Fatal(err)
	}
	read(testHandler.RequestRuntimeSubscriptionQuota, http.MethodPost, path+"?if_stale=true", "complete")
	read(testHandler.RequestRuntimeSubscriptionQuota, http.MethodPost, path, "pending")
	ack, err := testHandler.HandleDaemonWSHeartbeat(ctx, daemonws.ClientIdentity{
		WorkspaceID: testWorkspaceID,
		RuntimeLeases: map[string]*daemonws.RuntimeLease{
			runtimeID: daemonws.NewRuntimeLease(testWorkspaceID, "online", time.Now(), true),
		},
	}, runtimeID, false, true)
	if err != nil || ack == nil || ack.PendingSubscriptionQuota == "" || ack.PendingSubscriptionQuota == requestID {
		t.Fatalf("websocket heartbeat did not claim the new request: %+v (%v)", ack, err)
	}
	reportResult(ack.PendingSubscriptionQuota, "quota-test-daemon", protocol.SubscriptionQuotaReport{
		Status: protocol.SubscriptionQuotaFailed,
	}, http.StatusOK)
	if got := read(testHandler.GetRuntimeSubscriptionQuota, http.MethodGet, path, "failed"); got.Snapshot == nil {
		t.Fatal("temporary failures must preserve the previous snapshot")
	}
	for _, userID := range []string{testUserID, other} {
		req := newRequestAs(userID, http.MethodGet, "/api/runtimes", nil)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		w := call(testHandler.ListAgentRuntimes, req, http.StatusOK)
		var rows []AgentRuntimeResponse
		if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.ID == runtimeID {
				found = true
				if (row.SubscriptionQuota != nil) != (userID == testUserID) {
					t.Fatal("only the runtime owner may see the quota on the list")
				}
			}
		}
		if !found {
			t.Fatal("test runtime missing from list")
		}
	}
}

func TestObserveRuntimeSubscriptionQuota(t *testing.T) {
	for _, provider := range []string{"claude", "dsh", "codex"} {
		t.Run(provider, func(t *testing.T) {
			const daemonID = "quota-observation-daemon"
			runtimeID := dbfx.Runtime(t, "quota observation", testutil.Cols{
				"provider": provider, "runtime_mode": "local", "daemon_id": daemonID,
			})
			snapshot := protocol.SubscriptionQuotaSnapshot{ObservedAt: time.Now().Unix(), Exhausted: true}
			send := func(body protocol.SubscriptionQuotaSnapshot, want int) {
				t.Helper()
				req := withURLParam(newDaemonTokenRequest(http.MethodPost, "/observation", body, testWorkspaceID, daemonID), "runtimeId", runtimeID)
				w := httptest.NewRecorder()
				testHandler.ObserveRuntimeSubscriptionQuota(w, req)
				if w.Code != want {
					t.Fatalf("status = %d, want %d: %s", w.Code, want, w.Body.String())
				}
			}
			if provider == "codex" {
				send(snapshot, http.StatusNotFound)
				return
			}

			send(snapshot, http.StatusNoContent)
			stale := snapshot
			stale.ObservedAt--
			send(stale, http.StatusNoContent)
			req := withURLParam(newRequest(http.MethodGet, "/subscription-quota", nil), "runtimeId", runtimeID)
			w := httptest.NewRecorder()
			testHandler.GetRuntimeSubscriptionQuota(w, req)
			var report protocol.SubscriptionQuotaReport
			if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil || report.Snapshot == nil ||
				report.Snapshot.ObservedAt != snapshot.ObservedAt || !report.Snapshot.Exhausted {
				t.Fatalf("observation missing or overwritten: %s (%v)", w.Body.String(), err)
			}

			w = httptest.NewRecorder()
			testHandler.RequestRuntimeSubscriptionQuota(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("passive runtime accepted refresh request: %d", w.Code)
			}
		})
	}
}
