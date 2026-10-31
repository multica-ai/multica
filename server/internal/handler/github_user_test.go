package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func setAppURLForTest(t *testing.T, origin string) {
	t.Helper()
	previous := testHandler.cfg.AppURL
	testHandler.cfg.AppURL = origin
	t.Cleanup(func() { testHandler.cfg.AppURL = previous })
}

func getMyGitHubAccount(t *testing.T) GitHubAccountResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.GetMyGitHubAccount(w, newRequest(http.MethodGet, "/api/me/github", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp GitHubAccountResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return resp
}

func TestGetMyGitHubAccount(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	previous := testHandler.GitHubUser
	testHandler.GitHubUser = nil
	if got := getMyGitHubAccount(t); got.Configured || got.Connected || got.Login != nil {
		t.Fatalf("unconfigured = %+v", got)
	}
	testHandler.GitHubUser = previous

	api := newFakeGitHubAPI(t)
	svc := installGitHubUserService(t, api)
	if got := getMyGitHubAccount(t); !got.Configured || got.Connected || got.Login != nil {
		t.Fatalf("not connected = %+v", got)
	}
	connectTestGitHubUser(t, svc)
	got := getMyGitHubAccount(t)
	if !got.Configured || !got.Connected || got.Login == nil || *got.Login != "octocat" ||
		got.AvatarURL == nil || *got.AvatarURL != "https://avatars.example/u/777" {
		t.Fatalf("connected = %+v", got)
	}

	req := newRequest(http.MethodGet, "/api/me/github", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	w := httptest.NewRecorder()
	testHandler.GetMyGitHubAccount(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent actor: status = %d", w.Code)
	}
}

func TestConnectMyGitHubAccount(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	connect := func(returnTo string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.ConnectMyGitHubAccount(w, newRequest(http.MethodGet, "/api/me/github/connect?return_to="+url.QueryEscape(returnTo), nil))
		return w
	}

	previous := testHandler.GitHubUser
	testHandler.GitHubUser = nil
	expectErrorCode(t, connect("/"), http.StatusServiceUnavailable, "github_user_auth_not_configured")
	testHandler.GitHubUser = previous

	svc := installGitHubUserService(t, newFakeGitHubAPI(t))
	previousPublic := testHandler.cfg.PublicURL
	testHandler.cfg.PublicURL = "https://api.multica.test"
	t.Cleanup(func() { testHandler.cfg.PublicURL = previousPublic })

	w := connect("/acme/issues/MUL-7?tab=activity")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp GitHubAccountConnectResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	u, err := url.Parse(resp.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if !strings.HasSuffix(u.Path, "/login/oauth/authorize") || q.Get("client_id") != testGitHubClientID ||
		q.Get("redirect_uri") != "https://api.multica.test/api/github/user/callback" {
		t.Fatalf("authorize url = %s", resp.URL)
	}
	state, err := svc.VerifyState(q.Get("state"))
	if err != nil || state.UserID != testUserID || state.ReturnTo != "/acme/issues/MUL-7?tab=activity" {
		t.Fatalf("state = %+v err=%v", state, err)
	}

	for _, bad := range []string{
		"https://evil.example/steal",
		"//evil.example/steal",
		`/\evil.example`,
		"javascript:alert(1)",
		"acme/issues",
		"/acme\n/issues",
		"/" + strings.Repeat("a", 2048),
	} {
		if w := connect(bad); w.Code != http.StatusBadRequest {
			t.Errorf("return_to %q: status = %d, want 400", bad, w.Code)
		}
	}
	if w := connect(""); w.Code != http.StatusOK {
		t.Fatalf("empty return_to must default to the app root: %d", w.Code)
	}
}

func TestGitHubUserCallback(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	api := newFakeGitHubAPI(t)
	svc := installGitHubUserService(t, api)
	setAppURLForTest(t, "https://app.multica.test")

	callback := func(query url.Values) string {
		t.Helper()
		w := httptest.NewRecorder()
		// Public route: the browser arrives from GitHub without Multica headers.
		req := httptest.NewRequest(http.MethodGet, "/api/github/user/callback?"+query.Encode(), nil)
		testHandler.GitHubUserCallback(w, req)
		if w.Code != http.StatusFound {
			t.Fatalf("status = %d: %s", w.Code, w.Body.String())
		}
		return w.Header().Get("Location")
	}
	connected := func() bool {
		var n int
		dbfx.QueryRow(t, `SELECT count(*) FROM github_user_connection WHERE user_id = $1`, testUserID).Scan(&n)
		return n == 1
	}
	state := func(returnTo string) string {
		s, err := svc.SignState(testUserID, returnTo)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("missing state", func(t *testing.T) {
		if loc := callback(url.Values{"code": {"good-code"}}); loc != "https://app.multica.test/?github_account_error=missing_params" {
			t.Fatalf("location = %s", loc)
		}
	})
	t.Run("tampered state", func(t *testing.T) {
		if loc := callback(url.Values{"code": {"good-code"}, "state": {state("/x") + "x"}}); loc != "https://app.multica.test/?github_account_error=invalid_state" {
			t.Fatalf("location = %s", loc)
		}
		if connected() {
			t.Fatal("an invalid state must not connect anything")
		}
	})
	t.Run("user denied", func(t *testing.T) {
		loc := callback(url.Values{"error": {"access_denied"}, "state": {state("/acme/issues/MUL-7")}})
		if loc != "https://app.multica.test/acme/issues/MUL-7?github_account_error=access_denied" {
			t.Fatalf("location = %s", loc)
		}
	})
	t.Run("hands the code to the web app without connecting", func(t *testing.T) {
		signed := state("/acme/issues/MUL-7?tab=activity#prs")
		loc := callback(url.Values{"code": {"good-code"}, "state": {signed}})
		want := "https://app.multica.test/acme/issues/MUL-7?" + url.Values{
			"github_account_code":  {"good-code"},
			"github_account_state": {signed},
			"tab":                  {"activity"},
		}.Encode() + "#prs"
		if loc != want {
			t.Fatalf("location = %s (want %s)", loc, want)
		}
		if connected() {
			t.Fatal("the public callback must leave connecting to the signed-in web app")
		}
	})
	t.Run("not configured", func(t *testing.T) {
		previous := testHandler.GitHubUser
		testHandler.GitHubUser = nil
		defer func() { testHandler.GitHubUser = previous }()
		if loc := callback(url.Values{"code": {"good-code"}}); loc != "https://app.multica.test/?github_account_error=not_configured" {
			t.Fatalf("location = %s", loc)
		}
	})
}

func TestCompleteMyGitHubAccount(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	complete := func(code, state string) *httptest.ResponseRecorder {
		t.Helper()
		body := map[string]string{"code": code, "state": state}
		w := httptest.NewRecorder()
		testHandler.CompleteMyGitHubAccount(w, newRequest(http.MethodPost, "/api/me/github/complete", body))
		return w
	}
	connected := func() bool {
		var n int
		dbfx.QueryRow(t, `SELECT count(*) FROM github_user_connection WHERE user_id = $1`, testUserID).Scan(&n)
		return n == 1
	}

	previous := testHandler.GitHubUser
	testHandler.GitHubUser = nil
	expectErrorCode(t, complete("good-code", "x"), http.StatusServiceUnavailable, "github_user_auth_not_configured")
	testHandler.GitHubUser = previous

	svc := installGitHubUserService(t, newFakeGitHubAPI(t))
	sign := func(userID string) string {
		s, err := svc.SignState(userID, "/acme/issues/MUL-7")
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("missing fields", func(t *testing.T) {
		if w := complete("", sign(testUserID)); w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("tampered state", func(t *testing.T) {
		expectErrorCode(t, complete("good-code", sign(testUserID)+"x"), http.StatusBadRequest, "github_account_invalid_state")
	})
	t.Run("state signed for another user", func(t *testing.T) {
		expectErrorCode(t, complete("good-code", sign("00000000-0000-0000-0000-0000000000aa")),
			http.StatusForbidden, "github_account_state_mismatch")
		if connected() {
			t.Fatal("a link started by someone else must not attach this GitHub account")
		}
	})
	t.Run("bad code", func(t *testing.T) {
		expectErrorCode(t, complete("stolen", sign(testUserID)), http.StatusBadGateway, "github_account_exchange_failed")
		if connected() {
			t.Fatal("a rejected code must not connect")
		}
	})
	t.Run("success", func(t *testing.T) {
		w := complete("good-code", sign(testUserID))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", w.Code, w.Body.String())
		}
		var resp GitHubAccountResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if !resp.Connected || resp.Login == nil || *resp.Login != "octocat" || !connected() {
			t.Fatalf("resp = %+v connected=%v", resp, connected())
		}
	})
	t.Run("agent actor", func(t *testing.T) {
		req := newRequest(http.MethodPost, "/api/me/github/complete", map[string]string{"code": "good-code", "state": sign(testUserID)})
		req.Header.Set("X-Actor-Source", "task_token")
		w := httptest.NewRecorder()
		testHandler.CompleteMyGitHubAccount(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestDisconnectMyGitHubAccount(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	api := newFakeGitHubAPI(t)
	svc := installGitHubUserService(t, api)
	connectTestGitHubUser(t, svc)

	w := httptest.NewRecorder()
	testHandler.DisconnectMyGitHubAccount(w, newRequest(http.MethodDelete, "/api/me/github", nil))
	expectOK(t, w)
	revokes := api.callsTo("DELETE /applications/" + testGitHubClientID + "/grant")
	if len(revokes) != 1 || !strings.Contains(string(revokes[0].Body), testGitHubUserToken) {
		t.Fatalf("revoke calls = %+v", revokes)
	}
	if got := getMyGitHubAccount(t); got.Connected {
		t.Fatalf("still connected: %+v", got)
	}

	// Revocation failing on GitHub still disconnects locally.
	connectTestGitHubUser(t, svc)
	api.reply("DELETE /applications/"+testGitHubClientID+"/grant", http.StatusBadGateway, `{}`)
	w = httptest.NewRecorder()
	testHandler.DisconnectMyGitHubAccount(w, newRequest(http.MethodDelete, "/api/me/github", nil))
	expectOK(t, w)
	if got := getMyGitHubAccount(t); got.Connected {
		t.Fatalf("still connected after failed revoke: %+v", got)
	}
}

func TestNormalizeGitHubUserReturnTo(t *testing.T) {
	for raw, want := range map[string]string{
		"":                     "/",
		"  ":                   "/",
		"/":                    "/",
		"/acme/issues/MUL-1":   "/acme/issues/MUL-1",
		"/settings?tab=github": "/settings?tab=github",
	} {
		if got, ok := normalizeGitHubUserReturnTo(raw); !ok || got != want {
			t.Errorf("%q -> %q %v, want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"http://x", "//x", `/\x`, "x", "/a\tb", "mailto:a@b"} {
		if _, ok := normalizeGitHubUserReturnTo(raw); ok {
			t.Errorf("%q must be rejected", raw)
		}
	}
}
