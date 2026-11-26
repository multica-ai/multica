package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// A daemon started with a workspace token serves that workspace only: it is
// offered no other workspace, and is refused if it names one.
func TestDaemon_WorkspaceTokenServesOnlyItsWorkspace(t *testing.T) {
	f := newWorkspaceTokenFixture(t)
	daemonAuth := middleware.DaemonAuth(testHandler.Queries, nil, nil, nil)
	call := func(h http.HandlerFunc, req *http.Request) *testutil.Response {
		req.Header.Set("Authorization", "Bearer "+f.token)
		return testutil.Call(t, daemonAuth(h).ServeHTTP, req)
	}

	var listed []DaemonWorkspaceResponse
	call(testHandler.ListDaemonWorkspaces, testutil.JSONRequest(http.MethodGet, "/api/daemon/workspaces", nil)).Want(http.StatusOK).JSON(&listed)
	if len(listed) != 1 || listed[0].ID != f.bound {
		t.Fatalf("daemon workspaces = %+v, want only %s", listed, f.bound)
	}

	access := func(workspaceID string) int {
		return call(func(w http.ResponseWriter, r *http.Request) {
			if testHandler.requireDaemonWorkspaceAccess(w, r, workspaceID) {
				w.WriteHeader(http.StatusOK)
			}
		}, testutil.JSONRequest(http.MethodGet, "/api/daemon/anything", nil)).Code
	}
	if got := access(f.bound); got != http.StatusOK {
		t.Fatalf("bound workspace: status %d, want 200", got)
	}
	if got := access(f.other); got != http.StatusNotFound {
		t.Fatalf("other workspace: status %d, want 404", got)
	}
}

// Daemon RPCs arrive over the WebSocket and are replayed as in-process
// requests that carry no Authorization header. The replay must keep the
// connection's workspace token confinement: the owner's runtime in another
// workspace is not this connection's to claim for.
func TestDaemonRPC_WorkspaceTokenConnectionStaysConfined(t *testing.T) {
	f := newWorkspaceTokenFixture(t)
	runtime := func(workspaceID string) string {
		return dbfx.Runtime(t, "Workspace token runtime", testutil.Cols{"workspace_id": workspaceID, "owner_id": f.userID})
	}
	inBound, inOther := runtime(f.bound), runtime(f.other)

	recheck := func(identity daemonws.ClientIdentity) []string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"daemon_id": "workspace-token-daemon", "max_tasks": 1,
			"runtime_ids":               []string{inBound, inOther},
			"force_recheck_runtime_ids": []string{inBound, inOther},
		})
		status, raw, err := testHandler.DaemonRPCHandler(context.Background(), identity, "tasks.claim", body)
		if err != nil || status != http.StatusOK {
			t.Fatalf("tasks.claim: status %d, err %v: %s", status, err, raw)
		}
		// The handler acknowledges a forced recheck only for runtimes the
		// caller is authorized for.
		var resp struct {
			Rechecked []string `json:"force_rechecked_runtime_ids"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("decode tasks.claim response: %v: %s", err, raw)
		}
		sort.Strings(resp.Rechecked)
		return resp.Rechecked
	}

	// Control: the owner's account-wide credential reaches both runtimes.
	both := []string{inBound, inOther}
	sort.Strings(both)
	if got := recheck(daemonws.ClientIdentity{UserID: f.userID}); strings.Join(got, ",") != strings.Join(both, ",") {
		t.Fatalf("account-wide connection authorized %v, want %v", got, both)
	}

	confined := recheck(daemonws.ClientIdentity{UserID: f.userID, WorkspaceTokenWorkspaceID: f.bound})
	if len(confined) != 1 || confined[0] != inBound {
		t.Fatalf("workspace token connection authorized %v, want only %s", confined, inBound)
	}
}

// The connection identity records the confinement at connect time, which is
// what the replay above relies on.
func TestDaemonWebSocketIdentity_RecordsWorkspaceToken(t *testing.T) {
	f := newWorkspaceTokenFixture(t)
	var identity daemonws.ClientIdentity
	req := testutil.JSONRequest(http.MethodGet, "/api/daemon/ws", nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	middleware.DaemonAuth(testHandler.Queries, nil, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, _ = testHandler.buildDaemonWebSocketIdentity(w, r, nil, requestUserID(r))
	})).ServeHTTP(httptest.NewRecorder(), req)

	if identity.UserID != f.userID || identity.WorkspaceTokenWorkspaceID != f.bound {
		t.Fatalf("identity = user %q workspace token %q, want %q / %q", identity.UserID, identity.WorkspaceTokenWorkspaceID, f.userID, f.bound)
	}
}
