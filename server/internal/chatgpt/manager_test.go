package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type authFixture struct {
	m               *Manager
	key             *rsa.PrivateKey
	mu              sync.Mutex
	tokens          map[string]any
	tokenStatus     int
	forms           []url.Values
	jwksUnavailable bool
	revokeStatus    int
	revocations     int
	models          map[string]any
}

func testManager(t *testing.T) *authFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &authFixture{key: key, tokenStatus: 200, revokeStatus: 200,
		models: map[string]any{"models": []any{map[string]any{"slug": "z-model", "display_name": "First", "visibility": "list"}, map[string]any{"slug": "hidden", "visibility": "hide"}, map[string]any{"slug": "a-model", "display_name": "Second", "visibility": "list"}}}}
	f.m = NewManager(filepath.Join(t.TempDir(), "chatgpt", "credentials.json"))
	f.m.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		status := 200
		var body any
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			body = map[string]any{"issuer": Issuer, "authorization_endpoint": Issuer + "/api/accounts/authorize", "token_endpoint": Issuer + "/api/accounts/oauth/token", "jwks_uri": Issuer + "/.well-known/jwks.json", "revocation_endpoint": Issuer + "/api/accounts/oauth/revoke"}
		case "/.well-known/jwks.json":
			if f.jwksUnavailable {
				status = 503
				body = map[string]any{"detail": "temporary"}
				break
			}
			body = map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "fixture", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}}
		case "/api/accounts/oauth/token":
			_ = r.ParseForm()
			f.forms = append(f.forms, r.Form)
			status, body = f.tokenStatus, f.tokens
		case "/api/accounts/oauth/revoke":
			_ = r.ParseForm()
			f.forms = append(f.forms, r.Form)
			f.revocations++
			status, body = f.revokeStatus, map[string]any{}
		case "/v1/models":
			body = f.models
		default:
			t.Errorf("unexpected endpoint %s", r.URL)
			status = 404
			body = map[string]any{}
		}
		data, _ := json.Marshal(body)
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
	})}
	return f
}

func (f *authFixture) response(t *testing.T, client, subject, nonce, scopes string) map[string]any {
	t.Helper()
	claims := jwt.MapClaims{"iss": Issuer, "aud": client, "sub": subject, "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix(), "email": "same@example.test"}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "fixture"
	signed, err := token.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"access_token": "access-" + client, "refresh_token": "refresh-" + client, "id_token": signed, "token_type": "Bearer", "expires_in": 3600, "scope": scopes}
}

func (f *authFixture) login(t *testing.T, client, subject string) Account {
	t.Helper()
	a, err := f.m.BeginLogin(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	u, _ := url.Parse(a.Authorization().URL)
	f.tokens = f.response(t, client, subject, u.Query().Get("nonce"), DirectScope+" openid offline_access")
	got, err := f.m.CompleteLogin(context.Background(), a, url.Values{"state": {u.Query().Get("state")}, "code": {"fixture-code"}, "client_id": {client}})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRegistrationPKCEHostAndProtectedStorage(t *testing.T) {
	f := testManager(t)
	a, err := f.m.BeginLogin(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	u, _ := url.Parse(a.Authorization().URL)
	q := u.Query()
	for key, want := range map[string]string{"client_id": "dynamic_agent_client", "agent_name_hint": "Multica", "code_challenge_method": "S256", "resource": Resource} {
		if q.Get(key) != want {
			t.Fatalf("%s = %q, want %q", key, q.Get(key), want)
		}
	}
	if !strings.HasPrefix(q.Get("ext_agent_host_id"), "urn:uuid:") || q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge") == "" {
		t.Fatal("missing PKCE/host fields")
	}
	f.tokens = f.response(t, "client-a", "subject-a", q.Get("nonce"), DirectScope)
	got, err := f.m.CompleteLogin(context.Background(), a, url.Values{"state": {q.Get("state")}, "code": {"code"}, "client_id": {"client-a"}})
	if err != nil || !got.PlanEnabled || !got.Active {
		t.Fatalf("login: %+v %v", got, err)
	}
	st, err := os.Stat(f.m.path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Fatalf("credential permissions: %v", st.Mode())
	}
	if f.forms[0].Get("client_id") != "client-a" || f.forms[0].Get("redirect_uri") != q.Get("redirect_uri") || f.forms[0].Get("code_verifier") == "" {
		t.Fatal("incorrect token exchange")
	}
	b, err := f.m.BeginLogin(context.Background(), "client-a")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	v, _ := url.Parse(b.Authorization().URL)
	if v.Query().Get("ext_agent_host_id") != q.Get("ext_agent_host_id") || v.Query().Get("client_id") != "client-a" || v.Query().Has("agent_name_hint") || !v.Query().Has("id_token_hint") {
		t.Fatal("returning registration not reused")
	}
	if strings.Contains(b.Authorization().DisplayURL, "id_token_hint") {
		t.Fatal("ID token exposed in display URL")
	}
}

func TestInvalidCallbackCannotReplaceActiveAccount(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-a", "subject-a")
	for _, tc := range []struct {
		name     string
		callback url.Values
	}{
		{"state", url.Values{"state": {"wrong"}, "code": {"x"}, "client_id": {"client-b"}}},
		{"issued-client", url.Values{"code": {"x"}, "client_id": {"dynamic_agent_client"}}},
		{"denied", url.Values{"error": {"access_denied"}, "client_id": {"client-b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, e := f.m.BeginLogin(context.Background(), "")
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			u, _ := url.Parse(a.Authorization().URL)
			if !tc.callback.Has("state") {
				tc.callback.Set("state", u.Query().Get("state"))
			}
			if _, err := f.m.CompleteLogin(context.Background(), a, tc.callback); err == nil {
				t.Fatal("accepted invalid callback")
			}
			status, _ := f.m.Status()
			if status.ClientID != "client-a" {
				t.Fatal("changed active account")
			}
		})
	}
}

func TestIdentityScopeAndSeparateRegistrations(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-a", "same-subject")
	f.login(t, "client-b", "same-subject")
	accounts, err := f.m.Accounts()
	if err != nil || len(accounts) != 2 {
		t.Fatalf("accounts %v %v", accounts, err)
	}
	if accounts[0].Label == accounts[1].Label {
		t.Fatal("registration labels must be distinct")
	}
	if err := f.m.Select(context.Background(), "client-a"); err != nil {
		t.Fatal(err)
	}
	a, err := f.m.BeginLogin(context.Background(), "client-a")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	u, _ := url.Parse(a.Authorization().URL)
	f.tokens = f.response(t, "client-a", "wrong-subject", u.Query().Get("nonce"), DirectScope)
	if _, err = f.m.CompleteLogin(context.Background(), a, url.Values{"state": {u.Query().Get("state")}, "code": {"x"}}); err == nil {
		t.Fatal("reconnect changed identity")
	}
	a, err = f.m.BeginLogin(context.Background(), "client-a")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	u, _ = url.Parse(a.Authorization().URL)
	f.tokens = f.response(t, "client-a", "same-subject", u.Query().Get("nonce"), "openid email")
	account, err := f.m.CompleteLogin(context.Background(), a, url.Values{"state": {u.Query().Get("state")}, "code": {"x"}})
	if err != nil || account.PlanEnabled || !account.SignedIn {
		t.Fatalf("identity-only: %+v %v", account, err)
	}
	if _, err = f.m.AccessToken(context.Background(), "client-a"); err == nil {
		t.Fatal("identity-only grant allowed inference")
	}
}

func TestRefreshRotationAndSelectionGate(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-a", "subject-a")
	s, _ := f.m.load()
	s.Registrations["client-a"].ExpiresAt = time.Now().Add(-time.Minute)
	if err := f.m.save(s); err != nil {
		t.Fatal(err)
	}
	f.tokens = f.response(t, "client-a", "subject-a", "", DirectScope)
	f.tokens["access_token"] = "rotated-access"
	f.tokens["refresh_token"] = "rotated-refresh"
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := NewManager(f.m.path)
			other.client = f.m.client
			tok, err := other.AccessToken(context.Background(), "client-a")
			if err != nil || tok.AccessToken != "rotated-access" {
				t.Errorf("refresh: %v %v", tok, err)
			}
		}()
	}
	wg.Wait()
	if len(f.forms) != 2 {
		t.Fatalf("token exchange count=%d want login+one refresh", len(f.forms))
	}
	if f.forms[1].Get("resource") != Resource || f.forms[1].Has("scope") || f.forms[1].Get("refresh_token") != "refresh-client-a" {
		t.Fatal("invalid refresh contract")
	}
	tok, err := f.m.AccessToken(context.Background(), "client-a")
	if err != nil {
		t.Fatal(err)
	}
	f.login(t, "client-b", "subject-b")
	if f.m.AssertActive(context.Background(), tok) == nil {
		t.Fatal("old account token remained active")
	}
}

func TestPendingRotatedTokenSurvivesTemporaryJWKSFailure(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-a", "subject-a")
	s, _ := f.m.load()
	s.Registrations["client-a"].ExpiresAt = time.Now().Add(-time.Minute)
	_ = f.m.save(s)
	f.tokens = f.response(t, "client-a", "subject-a", "", DirectScope)
	f.tokens["refresh_token"] = "replacement"
	f.jwksUnavailable = true
	if _, err := f.m.AccessToken(context.Background(), ""); err == nil {
		t.Fatal("unverified rotation allowed inference")
	}
	f.jwksUnavailable = false
	if _, err := f.m.AccessToken(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(f.forms) != 2 {
		t.Fatal("retried consumed refresh token")
	}
	s, _ = f.m.load()
	if s.Registrations["client-a"].RefreshToken != "replacement" {
		t.Fatal("lost refresh rotation")
	}
}

func TestTerminalRefreshClearsTokensButKeepsRegistration(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-a", "subject-a")
	s, _ := f.m.load()
	s.Registrations["client-a"].ExpiresAt = time.Now().Add(-time.Minute)
	_ = f.m.save(s)
	f.tokenStatus = 400
	f.tokens = map[string]any{"error": "invalid_grant"}
	if _, err := f.m.AccessToken(context.Background(), ""); err == nil {
		t.Fatal("terminal grant accepted")
	}
	accounts, _ := f.m.Accounts()
	if len(accounts) != 1 || accounts[0].SignedIn || accounts[0].ClientID != "client-a" {
		t.Fatalf("registration lost: %+v", accounts)
	}
}

func TestLogoutAndModels(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-a", "subject-a")
	models, err := f.m.Models(context.Background(), "")
	if err != nil || len(models) != 2 || models[0].Slug != "z-model" || models[1].Slug != "a-model" {
		t.Fatalf("catalog: %+v %v", models, err)
	}
	result, err := f.m.Logout(context.Background(), "")
	if err != nil || !result.RevocationConfirmed {
		t.Fatalf("logout: %+v %v", result, err)
	}
	accounts, _ := f.m.Accounts()
	if len(accounts) != 1 || accounts[0].SignedIn || !accounts[0].Active {
		t.Fatal("logout lost retained active registration")
	}
	if _, err := f.m.AccessToken(context.Background(), ""); err == nil {
		t.Fatal("logout token still usable")
	}
	if f.revocations != 1 {
		t.Fatal("no revocation")
	}
	result, err = f.m.Logout(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if f.revocations != 1 {
		t.Fatal("repeated logout revokes twice")
	}
}

func TestErrorDoesNotExposeTokenMaterial(t *testing.T) {
	err := &Error{Code: "invalid_grant", Message: "ChatGPT authorization must be renewed."}
	if !strings.Contains(fmt.Sprint(err), "renewed") {
		t.Fatal("missing actionable error")
	}
	tok := Token{AccessToken: "secret-fixture"}
	raw, _ := json.Marshal(tok)
	if strings.Contains(string(raw), "secret-fixture") || strings.Contains(fmt.Sprint(tok), "secret-fixture") {
		t.Fatal("token exposed")
	}
}

func TestPermanentPendingLoginDoesNotPreventFreshAuthorization(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-a", "subject-a")
	a, err := f.m.BeginLogin(context.Background(), "client-a")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	u, _ := url.Parse(a.Authorization().URL)
	f.tokens = f.response(t, "client-a", "wrong-subject", u.Query().Get("nonce"), DirectScope)
	if _, err = f.m.CompleteLogin(context.Background(), a, url.Values{"state": {u.Query().Get("state")}, "code": {"first"}}); err == nil {
		t.Fatal("accepted wrong returning identity")
	}
	authorizations := 0
	account, err := f.m.Login(context.Background(), LoginOptions{ClientID: "client-a", Authorize: func(auth Authorization) error {
		authorizations++
		u, _ := url.Parse(auth.URL)
		f.tokens = f.response(t, "client-a", "subject-a", u.Query().Get("nonce"), DirectScope)
		callback, _ := url.Parse(u.Query().Get("redirect_uri"))
		callback.RawQuery = url.Values{"state": {u.Query().Get("state")}, "code": {"retry"}}.Encode()
		response, err := http.Get(callback.String())
		if err == nil {
			response.Body.Close()
		}
		return err
	}})
	if err != nil || account.ClientID != "client-a" || authorizations != 1 {
		t.Fatalf("fresh retry: %+v %v, authorizations=%d", account, err, authorizations)
	}
}

func TestLogoutRevokesAndClearsUnverifiedPendingLogin(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			f := testManager(t)
			client := "client-new"
			if existing {
				f.login(t, client, "subject-a")
			}
			selected := ""
			if existing {
				selected = client
			}
			a, err := f.m.BeginLogin(context.Background(), selected)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			u, _ := url.Parse(a.Authorization().URL)
			f.tokens = f.response(t, client, "subject-a", u.Query().Get("nonce"), DirectScope)
			f.tokens["refresh_token"] = "pending-login-refresh"
			f.jwksUnavailable = true
			if _, err = f.m.CompleteLogin(context.Background(), a, url.Values{"state": {u.Query().Get("state")}, "code": {"pending"}, "client_id": {client}}); err == nil {
				t.Fatal("expected temporary validation failure")
			}
			f.jwksUnavailable = false
			if _, err = f.m.Logout(context.Background(), client); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, form := range f.forms {
				if form.Get("token") == "pending-login-refresh" {
					found = true
				}
			}
			if !found {
				t.Fatal("pending login renewable session was not revoked")
			}
			s, err := f.m.load()
			if err != nil {
				t.Fatal(err)
			}
			if len(s.PendingLogins) != 0 || s.Registrations[client] == nil || s.Registrations[client].AccessToken != "" {
				t.Fatal("pending tokens not cleared or issued registration lost")
			}
		})
	}
}

func TestLoginPreservesLaterAccountSelection(t *testing.T) {
	for _, resumePending := range []bool{false, true} {
		t.Run(fmt.Sprint(resumePending), func(t *testing.T) {
			f := testManager(t)
			f.login(t, "client-a", "subject-a")
			f.login(t, "client-b", "subject-b")
			if err := f.m.Select(context.Background(), "client-a"); err != nil {
				t.Fatal(err)
			}
			a, err := f.m.BeginLogin(context.Background(), "client-a")
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			u, _ := url.Parse(a.Authorization().URL)
			f.tokens = f.response(t, "client-a", "subject-a", u.Query().Get("nonce"), DirectScope)
			f.tokens["access_token"] = "new-a-token"
			callback := url.Values{"state": {u.Query().Get("state")}, "code": {"new-a"}}
			if resumePending {
				f.jwksUnavailable = true
				if _, err := f.m.CompleteLogin(context.Background(), a, callback); err == nil {
					t.Fatal("expected pending verification")
				}
				f.jwksUnavailable = false
			}
			if err := f.m.Select(context.Background(), "client-b"); err != nil {
				t.Fatal(err)
			}
			if resumePending {
				_, err = f.m.Login(context.Background(), LoginOptions{ClientID: "client-a", Authorize: func(Authorization) error {
					t.Fatal("already validated pending credentials should not open a browser")
					return nil
				}})
			} else {
				_, err = f.m.CompleteLogin(context.Background(), a, callback)
			}
			var oauthErr *Error
			if !errors.As(err, &oauthErr) || oauthErr.Code != "chatgpt_session_changed" {
				t.Fatalf("expected explicit selection conflict, got %v", err)
			}
			s, err := f.m.load()
			if err != nil || s.ActiveClientID != "client-b" || s.Registrations["client-a"].AccessToken != "new-a-token" || len(s.PendingLogins) != 0 {
				t.Fatal("later selection or verified inactive credentials were lost")
			}
		})
	}
}

func TestAssertActiveRejectsTokenThatExpiredBeforeDispatch(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-a", "subject-a")
	token, err := f.m.AccessToken(context.Background(), "client-a")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := f.m.load()
	s.Registrations["client-a"].ExpiresAt = time.Now().Add(-time.Second)
	if err := f.m.save(s); err != nil {
		t.Fatal(err)
	}
	if err := f.m.AssertActive(context.Background(), token); err == nil {
		t.Fatal("dispatch accepted a known expired token")
	}
}

func TestLogoutWithoutRefreshTokenDoesNotClaimRemoteRevocation(t *testing.T) {
	for _, state := range []string{"verified", "pending", "pending-with-existing"} {
		t.Run(state, func(t *testing.T) {
			f := testManager(t)
			existingID := ""
			if state == "pending-with-existing" {
				f.login(t, "client-a", "subject-a")
				existingID = "client-a"
			}
			a, err := f.m.BeginLogin(context.Background(), existingID)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			u, _ := url.Parse(a.Authorization().URL)
			f.tokens = f.response(t, "client-a", "subject-a", u.Query().Get("nonce"), "openid "+DirectScope)
			delete(f.tokens, "refresh_token")
			f.jwksUnavailable = state != "verified"
			_, err = f.m.CompleteLogin(context.Background(), a, url.Values{"state": {u.Query().Get("state")}, "code": {"short-grant"}, "client_id": {"client-a"}})
			if (err == nil) != (state == "verified") {
				t.Fatalf("unexpected verification result: %v", err)
			}
			f.jwksUnavailable = false
			result, err := f.m.Logout(context.Background(), "client-a")
			if err != nil || result.RevocationConfirmed || !strings.Contains(result.Message, "https://chatgpt.com/settings") {
				t.Fatalf("unconfirmed short-term session logout: %+v %v", result, err)
			}
			s, _ := f.m.load()
			if len(s.PendingLogins) != 0 || s.Registrations["client-a"].AccessToken != "" || s.Registrations["client-a"].IDToken != "" {
				t.Fatal("local tokens remain after logout")
			}
			result, err = f.m.Logout(context.Background(), "client-a")
			if err != nil || !result.RevocationConfirmed {
				t.Fatalf("already signed-out account should be idempotent: %+v %v", result, err)
			}
		})
	}
}
