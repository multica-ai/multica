package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

// TestDaemonAuth_DaemonTokenCacheHit pins the daemon-token cache short-circuit:
// when the cache holds an entry for an mdt_ token, DaemonAuth must skip the DB
// lookup. nil queries would otherwise nil-deref on a miss.
func TestDaemonAuth_DaemonTokenCacheHit(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewDaemonTokenCache(rdb)
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}

	const rawToken = "mdt_cache_hit_test_token"
	hash := auth.HashToken(rawToken)
	cache.Set(context.Background(), hash, auth.DaemonTokenIdentity{
		WorkspaceID: "ws-cached",
		DaemonID:    "daemon-cached",
	}, auth.AuthCacheTTL)

	var gotWS, gotDaemon, gotPath string
	mw := DaemonAuth(nil, nil, cache) // nil queries — only safe on cache hit
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotWS = DaemonWorkspaceIDFromContext(r.Context())
		gotDaemon = DaemonIDFromContext(r.Context())
		gotPath = DaemonAuthPathFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on cache hit, got %d: %s", w.Code, w.Body.String())
	}
	if gotWS != "ws-cached" || gotDaemon != "daemon-cached" {
		t.Fatalf("expected (ws-cached, daemon-cached), got (%q, %q)", gotWS, gotDaemon)
	}
	if gotPath != DaemonAuthPathDaemonToken {
		t.Fatalf("expected auth path %q, got %q", DaemonAuthPathDaemonToken, gotPath)
	}
}

// TestDaemonAuth_PATCacheHit pins the PAT-fallback short-circuit. Production
// daemon traffic today uses mul_ PATs (mdt_ minting isn't wired up yet), so
// this is the cache hit that actually matters for /api/daemon/* DB load.
func TestDaemonAuth_PATCacheHit(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewPATCache(rdb)
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}

	const rawToken = "mul_daemon_pat_cache_hit_test"
	hash := auth.HashToken(rawToken)
	cache.Set(context.Background(), hash, "cached-user-id", auth.AuthCacheTTL)

	var gotUserID, gotPath string
	mw := DaemonAuth(nil, cache, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = r.Header.Get("X-User-ID")
		gotPath = DaemonAuthPathFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotUserID != "cached-user-id" {
		t.Fatalf("expected cached X-User-ID, got %q", gotUserID)
	}
	if gotPath != DaemonAuthPathPAT {
		t.Fatalf("expected auth path %q, got %q", DaemonAuthPathPAT, gotPath)
	}
}

func TestDaemonAuth_MissingAuth(t *testing.T) {
	mw := DaemonAuth(nil, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called")
	}))
	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestDaemonAuth_StripsClientSuppliedActorSource mirrors the
// TestAuth_StripsClientSuppliedActorSource invariant for the daemon
// auth path: a client supplying X-Actor-Source must NOT leak that
// header through to the handler. Required for parity between the
// two middlewares — the regular Auth path strips at the top, and we
// added the same strip in DaemonAuth so account-level guards (e.g.
// handler.RequireHumanActor) can trust the header regardless of
// which auth chain a request arrived on.
//
// We exercise an mdt_ token with an attempted forged X-Actor-Source.
// On the mdt_ path no actor-source stamp is added (daemon tokens
// aren't a "machine credential" in the billing sense — they're a
// runtime-bound proof for the daemon API itself), so a clean strip
// leaves the header empty downstream.
func TestDaemonAuth_StripsClientSuppliedActorSource(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewDaemonTokenCache(rdb)

	const rawToken = "mdt_strip_test"
	hash := auth.HashToken(rawToken)
	cache.Set(context.Background(), hash, auth.DaemonTokenIdentity{
		WorkspaceID: "ws-1",
		DaemonID:    "daemon-1",
	}, auth.AuthCacheTTL)

	var gotActorSource string
	mw := DaemonAuth(nil, nil, cache)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotActorSource = r.Header.Get("X-Actor-Source")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	// Forged value the client tries to smuggle in.
	req.Header.Set("X-Actor-Source", "task_token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotActorSource != "" {
		t.Fatalf("X-Actor-Source must be cleared on the mdt_ path, got %q", gotActorSource)
	}
}

// TestDaemonAuth_StripsForgedAgentIdentityHeaders mirrors
// TestAuth_StripsForgedAgentIdentityHeaders on the daemon chain, so a handler
// reachable through either middleware gets the same guarantee. The daemon's
// own endpoints carry task ids in the request body, never in these headers,
// so the strip costs the daemon nothing. MUL-3428.
func TestDaemonAuth_StripsForgedAgentIdentityHeaders(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewDaemonTokenCache(rdb)

	const rawToken = "mdt_agent_header_strip_test"
	hash := auth.HashToken(rawToken)
	cache.Set(context.Background(), hash, auth.DaemonTokenIdentity{
		WorkspaceID: "ws-1",
		DaemonID:    "daemon-1",
	}, auth.AuthCacheTTL)

	var gotAgentID, gotTaskID string
	mw := DaemonAuth(nil, nil, cache)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgentID = r.Header.Get("X-Agent-ID")
		gotTaskID = r.Header.Get("X-Task-ID")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	req.Header.Set("X-Agent-ID", "11111111-1111-1111-1111-111111111111")
	req.Header.Set("X-Task-ID", "22222222-2222-2222-2222-222222222222")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotAgentID != "" || gotTaskID != "" {
		t.Fatalf("agent identity headers must be cleared on the mdt_ path, got agent=%q task=%q", gotAgentID, gotTaskID)
	}
}

// TestDaemonAuth_StripsClientSuppliedUserID: a daemon token carries no user,
// so the user identity a handler reads must be empty on the mdt_ path rather
// than whatever the client sent. The PAT branch still stamps its own user.
func TestDaemonAuth_StripsClientSuppliedUserID(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewDaemonTokenCache(rdb)

	const rawToken = "mdt_user_header_strip_test"
	cache.Set(context.Background(), auth.HashToken(rawToken), auth.DaemonTokenIdentity{
		WorkspaceID: "ws-1",
		DaemonID:    "daemon-1",
		OwnerID:     "33333333-3333-3333-3333-333333333333",
	}, auth.AuthCacheTTL)
	const rawPAT = "mul_user_header_strip_test"
	patCache := auth.NewPATCache(rdb)
	patCache.Set(context.Background(), auth.HashToken(rawPAT), "44444444-4444-4444-4444-444444444444", auth.AuthCacheTTL)

	var gotUserID string
	mw := DaemonAuth(nil, patCache, cache)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = r.Header.Get("X-User-ID")
		w.WriteHeader(http.StatusOK)
	}))

	for _, tc := range []struct{ token, want string }{
		{rawToken, ""},
		{rawPAT, "44444444-4444-4444-4444-444444444444"},
	} {
		gotUserID = "unset"
		req := httptest.NewRequest("GET", "/api/daemon/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("X-User-ID", "11111111-1111-1111-1111-111111111111")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", tc.token, w.Code, w.Body.String())
		}
		if gotUserID != tc.want {
			t.Fatalf("%s: X-User-ID = %q, want %q", tc.token, gotUserID, tc.want)
		}
	}
}

func TestDaemonAuth_InvalidMDT_NilQueries(t *testing.T) {
	mw := DaemonAuth(nil, nil, nil) // no caches, no DB
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called")
	}))
	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer mdt_unknown")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestDaemonAuth_RetiredMCNRejected mirrors TestAuth_RetiredMCNRejected on
// the daemon chain.
func TestDaemonAuth_RetiredMCNRejected(t *testing.T) {
	mw := DaemonAuth(nil, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called for an mcn_ token")
	}))
	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer mcn_anything")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
