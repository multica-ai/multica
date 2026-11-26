package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DaemonAuth resolves a workspace token the way Auth does, so a daemon
// started with one is confined to the same workspace.
func TestDaemonAuth_WorkspaceToken(t *testing.T) {
	queries := db.New(&fakeTaskTokenDB{t: t})
	mw := DaemonAuth(queries, nil, nil, nil)

	rec, seen := serveWithWorkspaceToken(t, mw, httptest.NewRequest("GET", "/api/daemon/workspaces", nil))
	if rec.Code != http.StatusOK || !seen.called {
		t.Fatalf("status = %d, called = %v (body %s)", rec.Code, seen.called, rec.Body)
	}
	if seen.userID != ttUserID || !seen.bound || seen.boundWorkspace != ttWorkspaceA {
		t.Errorf("user %q, bound (%q, %v); want owner %q bound to %q", seen.userID, seen.boundWorkspace, seen.bound, ttUserID, ttWorkspaceA)
	}
	if seen.daemonAuthPath != DaemonAuthPathWorkspaceToken {
		t.Errorf("daemon auth path = %q, want %q", seen.daemonAuthPath, DaemonAuthPathWorkspaceToken)
	}
	// Not a daemon token: the handlers must authorize it as a user, by
	// membership and runtime ownership.
	if seen.daemonWorkspace != "" {
		t.Errorf("daemon workspace = %q; a workspace token must not read as a daemon token", seen.daemonWorkspace)
	}

	req := httptest.NewRequest("GET", "/api/daemon/workspaces/"+ttWorkspaceB+"/repos?workspace_id="+ttWorkspaceB, nil)
	if rec, seen := serveWithWorkspaceToken(t, mw, req); rec.Code != http.StatusForbidden || seen.called {
		t.Fatalf("selector for workspace B: status %d, called %v; want 403", rec.Code, seen.called)
	}
}
