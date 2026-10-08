package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// refreshSessionToken signs a session for the shared test user.
//
// Lifetimes are fractions of auth.AuthRenewThreshold() rather than literal
// durations: AuthTokenTTL() is cached process-wide behind a sync.Once, so a
// test cannot assume which value it resolved to.
func refreshSessionToken(t *testing.T, remaining time.Duration, sid string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":   testUserID,
		"email": handlerTestEmail,
		"name":  handlerTestName,
		"exp":   time.Now().Add(remaining).Unix(),
		"iat":   time.Now().Add(-time.Hour).Unix(),
	}
	if sid != "" {
		claims["sid"] = sid
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatalf("sign session token: %v", err)
	}
	return signed
}

func bearerRefreshRequest(t *testing.T, token string) *http.Request {
	t.Helper()
	req := newRequest("POST", "/api/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func cookieRefreshRequest(t *testing.T, token string) *http.Request {
	t.Helper()
	req := newRequest("POST", "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: auth.AuthCookieName, Value: token})
	return req
}

func TestRefreshSession_RenewsBearerSessionInsideWindow(t *testing.T) {
	token := refreshSessionToken(t, auth.AuthRenewThreshold()/2, "sid-bearer")

	var resp RefreshSessionResponse
	testutil.Call(t, testHandler.RefreshSession, bearerRefreshRequest(t, token)).
		Want(http.StatusOK).JSON(&resp)

	if !resp.Renewed {
		t.Fatalf("expected renewed=true, got %+v", resp)
	}
	if resp.Token == "" {
		t.Fatal("a bearer client has to receive the new token — it has nowhere else to get it")
	}
	if resp.Token == token {
		t.Error("renewal must produce a different token")
	}
	claims, err := auth.ParseSessionToken(resp.Token)
	if err != nil {
		t.Fatalf("returned token does not parse: %v", err)
	}
	if got, _ := claims["sid"].(string); got != "sid-bearer" {
		t.Errorf("sid = %q, want sid-bearer — the session identity must survive renewal", got)
	}
	if resp.CheckAgainInSeconds <= 0 {
		t.Error("clients need a server-supplied cadence; they must not invent one")
	}
}

// Asking early is a normal answer, not an error: the client polls on a
// cadence and most calls land outside the window.
func TestRefreshSession_OutsideWindowIsANoOp(t *testing.T) {
	token := refreshSessionToken(t, auth.AuthRenewThreshold()+auth.AuthRenewThreshold()/2, "sid-fresh")

	var resp RefreshSessionResponse
	testutil.Call(t, testHandler.RefreshSession, bearerRefreshRequest(t, token)).
		Want(http.StatusOK).JSON(&resp)

	if resp.Renewed {
		t.Error("a session outside the renewal window must not be renewed")
	}
	if resp.Token != "" {
		t.Error("an unrenewed response must not carry a token")
	}
	if resp.ExpiresAt == "" {
		t.Error("the caller still needs to know when the current session ends")
	}
}

// The browser's session is HttpOnly for a reason. Renewing it over this
// endpoint must not hand the JWT back in a body any script can read.
func TestRefreshSession_CookieModeDoesNotLeakTheToken(t *testing.T) {
	token := refreshSessionToken(t, auth.AuthRenewThreshold()/2, "sid-cookie")

	var resp RefreshSessionResponse
	res := testutil.Call(t, testHandler.RefreshSession, cookieRefreshRequest(t, token)).
		Want(http.StatusOK)
	res.JSON(&resp)

	if !resp.Renewed {
		t.Fatalf("expected renewed=true, got %+v", resp)
	}
	if resp.Token != "" {
		t.Error("a cookie-authenticated refresh must not return the JWT in the body")
	}

	var authCookie *http.Cookie
	for _, c := range res.Result().Cookies() {
		if c.Name == auth.AuthCookieName {
			authCookie = c
		}
	}
	if authCookie == nil {
		t.Fatal("cookie mode must receive the renewed session as a Set-Cookie")
	}
	if !authCookie.HttpOnly {
		t.Error("the renewed cookie must stay HttpOnly")
	}
	if authCookie.Value == token {
		t.Error("the renewed cookie must carry a new token")
	}
}

// Every machine credential the auth middleware accepts authenticates here
// too. None of them may be exchanged for an interactive session: that would
// turn a scoped, revocable token into an unscoped, unrevocable one.
func TestRefreshSession_RejectsNonSessionCredentials(t *testing.T) {
	cases := map[string]string{
		"personal access token": "mul_0123456789abcdef0123456789abcdef01234567",
		"agent task token":      "mat_0123456789abcdef0123456789abcdef01234567",
		"cloud node pat":        "mcn_0123456789abcdef0123456789abcdef01234567",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.RefreshSession, bearerRefreshRequest(t, token)).
				Want(http.StatusBadRequest)
		})
	}

	t.Run("no credential at all", func(t *testing.T) {
		testutil.Call(t, testHandler.RefreshSession, newRequest("POST", "/api/auth/refresh", nil)).
			Want(http.StatusBadRequest)
	})
}

// An expired JWT never reaches this handler in production — the middleware
// rejects it — but if it ever did, refresh must not be the thing that brings
// it back to life.
func TestRefreshSession_DoesNotReviveExpiredSession(t *testing.T) {
	expired := refreshSessionToken(t, -time.Minute, "sid-dead")

	testutil.Call(t, testHandler.RefreshSession, bearerRefreshRequest(t, expired)).
		Want(http.StatusBadRequest)
}

// Defense in depth: X-User-ID is set by the middleware from these very
// claims, so a mismatch means a header was forged past it.
func TestRefreshSession_RejectsSubjectMismatch(t *testing.T) {
	claims := jwt.MapClaims{
		"sub":   "00000000-0000-0000-0000-00000000dead",
		"email": handlerTestEmail,
		"exp":   time.Now().Add(auth.AuthRenewThreshold() / 2).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	testutil.Call(t, testHandler.RefreshSession, bearerRefreshRequest(t, signed)).
		Want(http.StatusUnauthorized)
}

// A legacy session (no sid) refreshes and acquires one, which is how clients
// already signed in migrate onto the session-bound CSRF binding.
func TestRefreshSession_MigratesLegacySession(t *testing.T) {
	legacy := refreshSessionToken(t, auth.AuthRenewThreshold()/2, "")

	var resp RefreshSessionResponse
	testutil.Call(t, testHandler.RefreshSession, bearerRefreshRequest(t, legacy)).
		Want(http.StatusOK).JSON(&resp)

	if !resp.Renewed || resp.Token == "" {
		t.Fatalf("a legacy session inside the window must renew, got %+v", resp)
	}
	if auth.SessionIDFromToken(resp.Token) == "" {
		t.Error("the refreshed legacy session must carry a session id")
	}
}
