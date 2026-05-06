package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/localmode"
	"github.com/multica-ai/multica/server/internal/middleware"
)

func TestLocalSessionBootstrapsAndIssuesUsableJWT(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	cleanupLocalSessionFixture(t)
	t.Cleanup(func() { cleanupLocalSessionFixture(t) })

	h := newLocalSessionHandler(true)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/local-session", nil)
	w := httptest.NewRecorder()
	h.LocalSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var resp LoginResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Token == "" {
		t.Fatal("token is empty")
	}
	if resp.User.Email != localmode.LocalUserEmail {
		t.Fatalf("user email = %q, want %q", resp.User.Email, localmode.LocalUserEmail)
	}
	if resp.User.Name != localmode.LocalUserName {
		t.Fatalf("user name = %q, want %q", resp.User.Name, localmode.LocalUserName)
	}
	if resp.User.OnboardedAt == nil {
		t.Fatal("local user should be marked onboarded")
	}

	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(resp.Token, claims, func(*jwt.Token) (any, error) {
		return auth.JWTSecret(), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("issued token did not validate: %v", err)
	}
	if claims["sub"] != resp.User.ID {
		t.Fatalf("token sub claim = %v, want %s", claims["sub"], resp.User.ID)
	}
	if claims["email"] != localmode.LocalUserEmail {
		t.Fatalf("token email claim = %v, want %s", claims["email"], localmode.LocalUserEmail)
	}

	// Token should authenticate against a normal protected route.
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+resp.Token)
	meRec := httptest.NewRecorder()

	mw := middleware.Auth(h.Queries, nil)
	mw(http.HandlerFunc(h.GetMe)).ServeHTTP(meRec, meReq)

	if meRec.Code != http.StatusOK {
		t.Fatalf("/api/me with local-session token: status = %d, body=%s", meRec.Code, meRec.Body.String())
	}
	var me UserResponse
	if err := json.NewDecoder(meRec.Body).Decode(&me); err != nil {
		t.Fatalf("decode /api/me response: %v", err)
	}
	if me.Email != localmode.LocalUserEmail {
		t.Fatalf("/api/me email = %q, want %q", me.Email, localmode.LocalUserEmail)
	}
	if me.ID != resp.User.ID {
		t.Fatalf("/api/me id = %q, want %q", me.ID, resp.User.ID)
	}

	// Idempotent: a second call returns the same user without duplicating rows.
	w2 := httptest.NewRecorder()
	h.LocalSession(w2, httptest.NewRequest(http.MethodPost, "/api/auth/local-session", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("second call status = %d, want 200; body=%s", w2.Code, w2.Body.String())
	}
	var resp2 LoginResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.User.ID != resp.User.ID {
		t.Fatalf("second user id = %q, want %q", resp2.User.ID, resp.User.ID)
	}

	var userCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM "user" WHERE email = $1`, localmode.LocalUserEmail,
	).Scan(&userCount); err != nil {
		t.Fatalf("count local users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("local user rows = %d, want 1", userCount)
	}
}

func TestLocalSessionDisabledOutsideLocalMode(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	h := newLocalSessionHandler(false)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/local-session", nil)
	w := httptest.NewRecorder()
	h.LocalSession(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when local mode is disabled; body=%s", w.Code, w.Body.String())
	}
}

func newLocalSessionHandler(localEnabled bool) *Handler {
	h := *testHandler
	if localEnabled {
		h.LocalMode = localmode.Config{ProductMode: "local"}
	} else {
		h.LocalMode = localmode.Config{}
	}
	h.LocalBootstrapper = localmode.NewBootstrapper(testPool)
	return &h
}

func cleanupLocalSessionFixture(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, localmode.LocalWorkspaceSlug); err != nil {
		t.Fatalf("delete local workspace fixture: %v", err)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, localmode.LocalUserEmail); err != nil {
		t.Fatalf("delete local user fixture: %v", err)
	}
}
