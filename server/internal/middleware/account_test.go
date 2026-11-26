package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

func staticAccounts(states map[string]auth.AccountState) *auth.AccountStates {
	return auth.NewAccountStates(nil, func(_ context.Context, userID string) (auth.AccountState, error) {
		return states[userID], nil
	})
}

func serveWithAccounts(t *testing.T, accounts *auth.AccountStates, token string) *httptest.ResponseRecorder {
	t.Helper()
	handler := AuthWithAccounts(accounts, nil, nil, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestAuthWithAccounts_SuspendedAccountIsForbidden(t *testing.T) {
	token := generateToken(validClaims(), auth.JWTSecret())
	w := serveWithAccounts(t, staticAccounts(map[string]auth.AccountState{"test-user-id": {Suspended: true}}), token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthWithAccounts_EndedSessionIsUnauthorized(t *testing.T) {
	accounts := staticAccounts(map[string]auth.AccountState{"test-user-id": {TokenVersion: 2}})

	old := validClaims()
	old[auth.TokenVersionClaim] = 1
	if w := serveWithAccounts(t, accounts, generateToken(old, auth.JWTSecret())); w.Code != http.StatusUnauthorized {
		t.Fatalf("older session: expected 401, got %d: %s", w.Code, w.Body.String())
	}

	current := validClaims()
	current[auth.TokenVersionClaim] = 2
	if w := serveWithAccounts(t, accounts, generateToken(current, auth.JWTSecret())); w.Code != http.StatusOK {
		t.Fatalf("current session: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthWithAccounts_UnknownStateRefuses(t *testing.T) {
	accounts := auth.NewAccountStates(nil, func(context.Context, string) (auth.AccountState, error) {
		return auth.AccountState{}, errors.New("db down")
	})
	w := serveWithAccounts(t, accounts, generateToken(validClaims(), auth.JWTSecret()))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDaemonAuthWithAccounts_SuspendedDaemonTokenOwner(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewDaemonTokenCache(rdb)
	const rawToken = "mdt_suspended_owner_test"
	cache.Set(context.Background(), auth.HashToken(rawToken), auth.DaemonTokenIdentity{
		WorkspaceID: "ws", DaemonID: "daemon", OwnerID: "owner-1",
	}, auth.AuthCacheTTL)

	accounts := staticAccounts(map[string]auth.AccountState{"owner-1": {Suspended: true}})
	handler := DaemonAuthWithAccounts(accounts, nil, nil, cache, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called")
	}))
	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDaemonAuthWithAccounts_SuspendedPATOwner(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := auth.NewPATCache(rdb)
	const rawToken = "mul_daemon_suspended_owner_test"
	cache.Set(context.Background(), auth.HashToken(rawToken), "owner-2", auth.AuthCacheTTL)

	accounts := staticAccounts(map[string]auth.AccountState{"owner-2": {Suspended: true}})
	handler := DaemonAuthWithAccounts(accounts, nil, cache, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called")
	}))
	req := httptest.NewRequest("POST", "/api/daemon/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
