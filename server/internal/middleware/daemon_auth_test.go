package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
	mw := DaemonAuth(nil, nil, cache, nil) // nil queries — only safe on cache hit
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
	mw := DaemonAuth(nil, cache, nil, nil)
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
	mw := DaemonAuth(nil, nil, nil, nil)
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

func TestDaemonAuth_InvalidMDT_NilQueries(t *testing.T) {
	mw := DaemonAuth(nil, nil, nil, nil) // no caches, no DB
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

// TestDaemonAuth_MCN_NoVerifierConfigured pins the fail-closed
// behaviour when MULTICA_CLOUD_FLEET_URL is empty: an mcn_ token MUST
// be rejected at the prefix branch with 401, not silently fall
// through to the mul_/JWT paths (an mcn_ string would never match a
// valid PAT or JWT, but failing closed makes the contract explicit).
func TestDaemonAuth_MCN_NoVerifierConfigured(t *testing.T) {
	mw := DaemonAuth(nil, nil, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when verifier is unconfigured")
	}))
	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer mcn_anything")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no verifier, got %d", w.Code)
	}
}

// TestDaemonAuth_MCN_ValidTokenSetsUserID confirms that on a successful
// Fleet verify, DaemonAuth surfaces owner_id as X-User-ID and tags the
// auth path as cloud_pat for telemetry. We use a stub Fleet here
// (no Redis) so the test runs without external services.
func TestDaemonAuth_MCN_ValidTokenSetsUserID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"valid": true,
			"owner_id": "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001",
			"instance_id": "i-01"
		}`))
	}))
	defer srv.Close()

	verifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{FleetBaseURL: srv.URL})

	var gotUser, gotPath string
	mw := DaemonAuth(nil, nil, nil, verifier)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("X-User-ID")
		gotPath = DaemonAuthPathFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer mcn_some_token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotUser != "01972f7e-7e8d-77ef-a13d-1b0ce3e9c001" {
		t.Errorf("expected owner_id propagated as X-User-ID, got %q", gotUser)
	}
	if gotPath != DaemonAuthPathCloudPAT {
		t.Errorf("expected auth path %q, got %q", DaemonAuthPathCloudPAT, gotPath)
	}
}

// TestDaemonAuth_MCN_FleetSaysInvalid confirms that a valid:false
// Fleet response maps to 401 (not 503) — the token IS known to be bad,
// retrying won't help.
func TestDaemonAuth_MCN_FleetSaysInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":false,"reason":"token_revoked"}`))
	}))
	defer srv.Close()

	verifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	mw := DaemonAuth(nil, nil, nil, verifier)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when fleet says invalid")
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer mcn_revoked")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", w.Code)
	}
}

// TestDaemonAuth_MCN_FleetUnreachable confirms the Unavailable branch
// emits 503 — the daemon must distinguish "your token is bad" (401, drop
// it) from "cloud is down" (503, retry later) so a brief outage doesn't
// invalidate everyone's PAT.
func TestDaemonAuth_MCN_FleetUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	verifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	mw := DaemonAuth(nil, nil, nil, verifier)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when fleet is unavailable")
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer mcn_x")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when fleet is unavailable, got %d", w.Code)
	}
}


// TestDaemonAuth_MCN_OwnerNotInLocalDB pins the new owner-existence
// guard end-to-end through the middleware. Cloud verifies the token
// successfully and returns an owner_id that does not exist in our
// local user table — DaemonAuth must reject with 401 (not 503) and
// MUST NOT call the next handler with a phantom X-User-ID.
func TestDaemonAuth_MCN_OwnerNotInLocalDB(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Owner_id is syntactically valid but not seeded in the DB.
		_, _ = w.Write([]byte(`{
			"valid": true,
			"owner_id": "00000000-0000-0000-0000-0000000feed1",
			"instance_id": "i-99"
		}`))
	}))
	defer srv.Close()

	verifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{FleetBaseURL: srv.URL})

	mw := DaemonAuth(queries, nil, nil, verifier)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when owner_id has no local user")
	}))

	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer mcn_phantom_owner")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when local user is missing, got %d", w.Code)
	}
}
