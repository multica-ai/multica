package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/auth"
)

type fakeOAuthProvider struct {
	id          string
	configured  bool
	redirectURI string
	exchangeErr error
	fetchErr    error
	profile     auth.OAuthProfile
}

func newFakeOAuthProvider(id string) *fakeOAuthProvider {
	return &fakeOAuthProvider{
		id:         id,
		configured: true,
		profile: auth.OAuthProfile{
			Email: "user@example.com",
		},
	}
}

func (f *fakeOAuthProvider) ID() string                                   { return f.id }
func (f *fakeOAuthProvider) Configured() bool                             { return f.configured }
func (f *fakeOAuthProvider) RedirectURI() string                          { return f.redirectURI }
func (f *fakeOAuthProvider) PublicConfig() auth.OAuthProviderPublicConfig { return auth.OAuthProviderPublicConfig{} }

func (f *fakeOAuthProvider) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	if f.exchangeErr != nil {
		return "", f.exchangeErr
	}
	return "fake-access-token", nil
}

func (f *fakeOAuthProvider) FetchProfile(ctx context.Context, accessToken string) (auth.OAuthProfile, error) {
	if f.fetchErr != nil {
		return auth.OAuthProfile{}, f.fetchErr
	}
	return f.profile, nil
}

// postOAuthLogin dispatches through h.OAuthLogin with the chi URL param
// populated, matching how the router mounts the real route.
func postOAuthLogin(provider string, body map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/oauth/"+provider, &buf)
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider", provider)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.OAuthLogin(w, req)
	return w
}

// withFakeProvider swaps a single provider in testHandler.OAuthProviders and
// returns a cleanup fn.
func withFakeProvider(t *testing.T, id string, fp auth.OAuthProvider) func() {
	t.Helper()
	prev, had := testHandler.OAuthProviders[id]
	testHandler.OAuthProviders[id] = fp
	return func() {
		if had {
			testHandler.OAuthProviders[id] = prev
		} else {
			delete(testHandler.OAuthProviders, id)
		}
	}
}

func oauthCleanupUser(t *testing.T, email string) {
	t.Helper()
	testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, strings.ToLower(email))
}

// --- Generic OAuth flow --------------------------------------------------

func TestOAuthLoginCreatesNewUser(t *testing.T) {
	const email = "new-google@multica.ai"
	t.Cleanup(func() { oauthCleanupUser(t, email) })

	fp := newFakeOAuthProvider("google")
	fp.profile = auth.OAuthProfile{
		Email:   email,
		Name:    "Google New User",
		Picture: "https://example.com/avatar.png",
	}
	defer withFakeProvider(t, "google", fp)()

	w := postOAuthLogin("google", map[string]string{
		"code":         "auth-code",
		"redirect_uri": "http://localhost:3000/auth/callback",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LoginResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if resp.User.Email != email {
		t.Fatalf("email: want %q got %q", email, resp.User.Email)
	}
	if resp.User.Name != "Google New User" {
		t.Fatalf("name backfill: got %q", resp.User.Name)
	}
	if resp.User.AvatarURL == nil || *resp.User.AvatarURL != "https://example.com/avatar.png" {
		t.Fatalf("avatar backfill: got %v", resp.User.AvatarURL)
	}
}

func TestOAuthLoginReusesExistingUser(t *testing.T) {
	email := handlerTestEmail

	fp := newFakeOAuthProvider("google")
	fp.profile = auth.OAuthProfile{Email: email, Name: "Handler Test User"}
	defer withFakeProvider(t, "google", fp)()

	w := postOAuthLogin("google", map[string]string{"code": "c", "redirect_uri": "x"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp LoginResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.User.ID == "" {
		t.Fatal("expected user id")
	}
	if resp.User.Name != handlerTestName {
		t.Fatalf("name: want %q got %q", handlerTestName, resp.User.Name)
	}
}

func TestOAuthLoginRejectsAccountWithNoEmail(t *testing.T) {
	fp := newFakeOAuthProvider("google")
	fp.profile = auth.OAuthProfile{Name: "Nameless"}
	defer withFakeProvider(t, "google", fp)()

	w := postOAuthLogin("google", map[string]string{"code": "c", "redirect_uri": "x"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOAuthLoginUnconfiguredReturns503(t *testing.T) {
	fp := newFakeOAuthProvider("google")
	fp.configured = false
	defer withFakeProvider(t, "google", fp)()

	w := postOAuthLogin("google", map[string]string{"code": "c", "redirect_uri": "x"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestOAuthLoginMissingCodeReturns400(t *testing.T) {
	defer withFakeProvider(t, "google", newFakeOAuthProvider("google"))()

	w := postOAuthLogin("google", map[string]string{"redirect_uri": "x"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestOAuthLoginExchangeFailureReturns400(t *testing.T) {
	fp := newFakeOAuthProvider("google")
	fp.exchangeErr = errors.New("bad code")
	defer withFakeProvider(t, "google", fp)()

	w := postOAuthLogin("google", map[string]string{"code": "c", "redirect_uri": "x"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestOAuthLoginUserFetchFailureReturns502(t *testing.T) {
	fp := newFakeOAuthProvider("google")
	fp.fetchErr = errors.New("rate limited")
	defer withFakeProvider(t, "google", fp)()

	w := postOAuthLogin("google", map[string]string{"code": "c", "redirect_uri": "x"})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestOAuthLoginUnknownProviderReturns404(t *testing.T) {
	w := postOAuthLogin("does-not-exist", map[string]string{"code": "c"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
