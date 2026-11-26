package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// workspaceTokenSeen is what the handler behind an auth middleware saw of a
// request made with the fixture workspace token.
type workspaceTokenSeen struct {
	called                          bool
	userID, workspaceID, source     string
	boundWorkspace                  string
	bound, isPAT                    bool
	daemonAuthPath, daemonWorkspace string
}

// serveWithWorkspaceToken sends req through mw with the fixture workspace
// token, which is bound to workspace A.
func serveWithWorkspaceToken(t *testing.T, mw func(http.Handler) http.Handler, req *http.Request) (*httptest.ResponseRecorder, workspaceTokenSeen) {
	t.Helper()
	var seen workspaceTokenSeen
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.called = true
		seen.userID = r.Header.Get("X-User-ID")
		seen.workspaceID = r.Header.Get("X-Workspace-ID")
		seen.source = r.Header.Get("X-Actor-Source")
		seen.boundWorkspace, seen.bound = BoundWorkspace(r)
		seen.isPAT = IsPersonalAccessToken(r.Context())
		seen.daemonAuthPath = DaemonAuthPathFromContext(r.Context())
		seen.daemonWorkspace = DaemonWorkspaceIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req.Header.Set("Authorization", "Bearer "+wtTestToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, seen
}

// A workspace token authenticates as its owner, carries its workspace, and
// stays a personal access token rather than a machine credential.
func TestAuth_WorkspaceTokenIdentity(t *testing.T) {
	queries := db.New(&fakeTaskTokenDB{t: t})
	rec, seen := serveWithWorkspaceToken(t, Auth(queries, nil, nil, nil), httptest.NewRequest("GET", "/api/issues", nil))

	if rec.Code != http.StatusOK || !seen.called {
		t.Fatalf("status = %d, called = %v (body %s)", rec.Code, seen.called, rec.Body)
	}
	if seen.userID != ttUserID {
		t.Errorf("X-User-ID = %q, want the token owner %q", seen.userID, ttUserID)
	}
	if seen.workspaceID != ttWorkspaceA {
		t.Errorf("X-Workspace-ID = %q, want the bound workspace %q", seen.workspaceID, ttWorkspaceA)
	}
	if !seen.bound || seen.boundWorkspace != ttWorkspaceA {
		t.Errorf("BoundWorkspace = (%q, %v), want (%q, true)", seen.boundWorkspace, seen.bound, ttWorkspaceA)
	}
	if !seen.isPAT {
		t.Error("a workspace token must count as a personal access token")
	}
	if seen.source != "" {
		t.Errorf("X-Actor-Source = %q; a workspace token is its owner's credential, not a machine's", seen.source)
	}
}

// An mwt_ string that matches no workspace token row is refused; it is never
// retried as an account-wide token or a session.
func TestAuth_UnknownWorkspaceTokenIsRefused(t *testing.T) {
	queries := db.New(&fakeTaskTokenDB{t: t})
	called := false
	handler := Auth(queries, nil, nil, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	req := httptest.NewRequest("GET", "/api/issues", nil)
	req.Header.Set("Authorization", "Bearer mwt_no_such_token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("status = %d, called = %v; want 401 and no handler", rec.Code, called)
	}
}

// A cache hit must return the workspace together with the owner: the token is
// exactly as confined as on a database read. Queries are nil, so a miss here
// would fail instead of authenticating.
func TestAuth_WorkspaceTokenCacheHitKeepsBinding(t *testing.T) {
	cache := auth.NewPATCache(newRedisTestClient(t))
	cache.SetWorkspaceToken(context.Background(), auth.HashToken(wtTestToken),
		auth.WorkspaceTokenIdentity{UserID: ttUserID, WorkspaceID: ttWorkspaceA}, auth.AuthCacheTTL)

	rec, seen := serveWithWorkspaceToken(t, Auth(nil, cache, nil, nil), httptest.NewRequest("GET", "/api/issues", nil))
	if rec.Code != http.StatusOK || !seen.bound || seen.boundWorkspace != ttWorkspaceA || seen.userID != ttUserID {
		t.Fatalf("cache hit: status %d, bound (%q, %v), user %q", rec.Code, seen.boundWorkspace, seen.bound, seen.userID)
	}

	req := httptest.NewRequest("GET", "/api/issues", nil)
	req.Header.Set("X-Workspace-ID", ttWorkspaceB)
	if rec, seen := serveWithWorkspaceToken(t, Auth(nil, cache, nil, nil), req); rec.Code != http.StatusForbidden || seen.called {
		t.Fatalf("cache hit naming workspace B: status %d, called %v; want 403", rec.Code, seen.called)
	}
}

// The account-wide entry of a token is never read back for a workspace token:
// the two kinds live in separate key spaces.
func TestAuth_WorkspaceTokenIgnoresAccountWideCacheEntry(t *testing.T) {
	cache := auth.NewPATCache(newRedisTestClient(t))
	cache.Set(context.Background(), auth.HashToken(wtTestToken), ttUserID, auth.AuthCacheTTL)

	queries := db.New(&fakeTaskTokenDB{t: t})
	rec, seen := serveWithWorkspaceToken(t, Auth(queries, cache, nil, nil), httptest.NewRequest("GET", "/api/issues", nil))
	if rec.Code != http.StatusOK || !seen.bound || seen.boundWorkspace != ttWorkspaceA {
		t.Fatalf("status %d, bound (%q, %v); want the binding read from the database", rec.Code, seen.boundWorkspace, seen.bound)
	}
}

func TestRefuseWorkspaceToken(t *testing.T) {
	queries := db.New(&fakeTaskTokenDB{t: t})
	gate := func(next http.Handler) http.Handler {
		return Auth(queries, nil, nil, nil)(RefuseWorkspaceToken(next))
	}

	rec, seen := serveWithWorkspaceToken(t, gate, httptest.NewRequest("POST", "/api/tokens", nil))
	if rec.Code != http.StatusForbidden || seen.called {
		t.Fatalf("workspace token: status %d, called %v; want 403 before the handler", rec.Code, seen.called)
	}
	if !strings.Contains(rec.Body.String(), errWorkspaceTokenAccountRoute) {
		t.Fatalf("body = %s, want %q", rec.Body, errWorkspaceTokenAccountRoute)
	}

	// Control: every other credential passes.
	called := false
	handler := gate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest("POST", "/api/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+generateToken(validClaims(), auth.JWTSecret()))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Fatal("a session must pass RefuseWorkspaceToken")
	}
}

// A workspace named in the path is not a selector Auth can see, so the
// workspace middleware compares it with the binding itself. The fake database
// fails the test on a membership lookup: the refusal must come first.
func TestWorkspaceMiddleware_WorkspaceTokenPathMismatch(t *testing.T) {
	queries := db.New(&fakeTaskTokenDB{t: t})
	router := chi.NewRouter()
	router.Use(Auth(queries, nil, nil, nil))
	router.With(RequireWorkspaceMemberFromURL(queries, "id")).Get("/api/workspaces/{id}", func(http.ResponseWriter, *http.Request) {
		t.Error("handler ran for a workspace the token is not bound to")
	})

	req := httptest.NewRequest("GET", "/api/workspaces/"+ttWorkspaceB, nil)
	req.Header.Set("Authorization", "Bearer "+wtTestToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), boundWorkspaceToken.mismatch()) {
		t.Fatalf("status %d, body %s; want 403 %q", rec.Code, rec.Body, boundWorkspaceToken.mismatch())
	}
}

// Handlers outside the workspace middleware resolve the workspace through
// ResolveWorkspaceIDFromRequest, which must answer with the binding.
func TestResolveWorkspaceIDFromRequest_WorkspaceToken(t *testing.T) {
	queries := db.New(&fakeTaskTokenDB{t: t})
	var resolved string
	handler := Auth(queries, nil, nil, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		resolved = ResolveWorkspaceIDFromRequest(r, queries)
	}))
	req := httptest.NewRequest("POST", "/api/upload-file", nil)
	req.Header.Set("Authorization", "Bearer "+wtTestToken)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if resolved != ttWorkspaceA {
		t.Fatalf("resolved workspace = %q, want the bound workspace %q", resolved, ttWorkspaceA)
	}
}
