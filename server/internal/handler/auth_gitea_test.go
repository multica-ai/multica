package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

func TestGiteaRedirectURIUsesConfiguredValue(t *testing.T) {
	t.Setenv("GITEA_REDIRECT_URI", "http://multica.local/auth/callback")

	got, err := giteaRedirectURI()
	if err != nil {
		t.Fatalf("giteaRedirectURI returned error: %v", err)
	}
	if got != "http://multica.local/auth/callback" {
		t.Fatalf("redirect URI = %q", got)
	}
}

func TestGiteaRedirectURIUsesExactConfiguredValue(t *testing.T) {
	t.Setenv("GITEA_REDIRECT_URI", "http://multica.local/auth/callback")

	t.Setenv("GITEA_ISSUER_URL", "http://gitea.local")
	if got, err := giteaRedirectURI(); err != nil || got != "http://multica.local/auth/callback" {
		t.Fatalf("configured redirect URI = (%q, %v)", got, err)
	}
}

func TestGiteaBackendURLUsesIssuer(t *testing.T) {
	t.Setenv("GITEA_ISSUER_URL", "http://gitea.local/")

	if got := giteaBackendURL("/api/v1/user"); got != "http://gitea.local/api/v1/user" {
		t.Fatalf("backend URL = %q", got)
	}
}

func TestGiteaStartUsesServerConfiguredRedirectAndPKCE(t *testing.T) {
	t.Setenv("GITEA_ISSUER_URL", "https://gitea.local/")
	t.Setenv("GITEA_CLIENT_ID", "client-id")
	t.Setenv("GITEA_CLIENT_SECRET", "client-secret")
	t.Setenv("GITEA_REDIRECT_URI", "https://multica.local/auth/callback")

	req := httptest.NewRequest(http.MethodGet, "/auth/gitea?state=platform%3Adesktop%2Cnext%3A%2Finbox&redirect_uri=https%3A%2F%2Fevil.local", nil)
	recorder := httptest.NewRecorder()
	(&Handler{}).GiteaStart(recorder, req)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusFound)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := location.Query()
	if location.Host != "gitea.local" || location.Path != "/login/oauth/authorize" {
		t.Fatalf("authorization endpoint = %s", location)
	}
	if query.Get("redirect_uri") != "https://multica.local/auth/callback" {
		t.Fatalf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		t.Fatalf("missing PKCE parameters: %v", query)
	}
	if query.Get("state") == "" || query.Get("state") == "platform:desktop,next:/inbox" {
		t.Fatalf("state was not server-issued: %q", query.Get("state"))
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.OAuthStateCookieName || !cookies[0].HttpOnly {
		t.Fatalf("OAuth state cookie = %#v", cookies)
	}
}

func TestGiteaIssuerRejectsQueryAndUnsupportedScheme(t *testing.T) {
	for _, issuer := range []string{"http://gitea.local?tenant=1", "file:///tmp/gitea"} {
		t.Run(issuer, func(t *testing.T) {
			t.Setenv("GITEA_ISSUER_URL", issuer)
			if _, err := giteaIssuerURL(); err == nil {
				t.Fatal("expected invalid issuer URL to fail")
			}
		})
	}
}

func TestGiteaOAuthStateBindsCookieAndPKCEVerifier(t *testing.T) {
	t.Setenv("JWT_SECRET", "gitea-oauth-test-secret")
	state, cookieValue, wantVerifier, err := newGiteaOAuthState("platform:desktop,next:%2Finbox")
	if err != nil {
		t.Fatalf("newGiteaOAuthState: %v", err)
	}
	if giteaPKCEChallenge(wantVerifier) == "" {
		t.Fatal("expected PKCE challenge")
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/gitea", nil)
	req.AddCookie(&http.Cookie{Name: auth.OAuthStateCookieName, Value: cookieValue})
	gotVerifier, err := validateGiteaOAuthState(req, state)
	if err != nil {
		t.Fatalf("validateGiteaOAuthState: %v", err)
	}
	if gotVerifier != wantVerifier {
		t.Fatalf("verifier = %q, want %q", gotVerifier, wantVerifier)
	}

	if _, err := validateGiteaOAuthState(req, state+"tampered"); err == nil {
		t.Fatal("expected tampered state to fail")
	}
}
