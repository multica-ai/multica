package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestLoginRejectsUnverifiedIdentityWithoutReplacingSelection(t *testing.T) {
	for _, name := range []string{"issuer", "audience", "nonce", "expired", "missing-expiry", "missing-subject", "signature", "algorithm"} {
		t.Run(name, func(t *testing.T) {
			f := testManager(t)
			f.login(t, "client-existing", "subject-existing")
			attempt, err := f.m.BeginLogin(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			defer attempt.Close()
			u, _ := url.Parse(attempt.Authorization().URL)
			claims := jwt.MapClaims{
				"iss": Issuer, "aud": "client-new", "sub": "subject-new",
				"exp": time.Now().Add(time.Hour).Unix(), "nonce": u.Query().Get("nonce"),
			}
			var key any = f.key
			var method jwt.SigningMethod = jwt.SigningMethodRS256
			switch name {
			case "issuer":
				claims["iss"] = "https://unrelated.example.test"
			case "audience":
				claims["aud"] = "another-issued-client"
			case "nonce":
				claims["nonce"] = "another-login-attempt"
			case "expired":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "missing-expiry":
				delete(claims, "exp")
			case "missing-subject":
				delete(claims, "sub")
			case "signature":
				key, err = rsa.GenerateKey(rand.Reader, 2048)
				if err != nil {
					t.Fatal(err)
				}
			case "algorithm":
				method, key = jwt.SigningMethodHS256, []byte("fixture-signing-key")
			}
			id := jwt.NewWithClaims(method, claims)
			id.Header["kid"] = "fixture"
			signed, err := id.SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			f.tokens = map[string]any{"access_token": "fixture-access", "refresh_token": "fixture-refresh", "id_token": signed, "expires_in": 3600, "scope": DirectScope, "token_type": "Bearer"}
			_, err = f.m.CompleteLogin(context.Background(), attempt, url.Values{
				"state": {u.Query().Get("state")}, "code": {"fixture-code"}, "client_id": {"client-new"},
			})
			if err == nil {
				t.Fatal("accepted unverified identity")
			}
			status, err := f.m.Status()
			if err != nil || status == nil || status.ClientID != "client-existing" {
				t.Fatal("failed sign-in replaced the active registration")
			}
		})
	}
}

func TestReturningCallbackCannotChangeIssuedClient(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-existing", "subject-existing")
	attempt, err := f.m.BeginLogin(context.Background(), "client-existing")
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	u, _ := url.Parse(attempt.Authorization().URL)
	before := len(f.forms)
	_, err = f.m.CompleteLogin(context.Background(), attempt, url.Values{
		"state": {u.Query().Get("state")}, "code": {"fixture-code"}, "client_id": {"client-other"},
	})
	if err == nil || len(f.forms) != before {
		t.Fatal("changed callback client must be rejected before code exchange")
	}
}

func TestGrantedScopesOverrideAuthorizationCallbackScopes(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-existing", "subject-existing")
	attempt, err := f.m.BeginLogin(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	u, _ := url.Parse(attempt.Authorization().URL)
	f.tokens = f.response(t, "client-identity", "subject-identity", u.Query().Get("nonce"), "openid email")
	account, err := f.m.CompleteLogin(context.Background(), attempt, url.Values{
		"state": {u.Query().Get("state")}, "code": {"fixture-code"}, "client_id": {"client-identity"}, "scope": {DirectScope},
	})
	if err != nil || !account.SignedIn || account.PlanEnabled {
		t.Fatalf("identity-only login state: %+v, %v", account, err)
	}
	status, err := f.m.Status()
	if err != nil || status == nil || status.ClientID != "client-existing" {
		t.Fatal("identity-only registration replaced a usable active account")
	}
	if err := f.m.Select(context.Background(), "client-identity"); err == nil {
		t.Fatal("identity-only registration can be selected for inference")
	}
}

func TestTransientRefreshFailurePreservesCredentials(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-existing", "subject-existing")
	s, err := f.m.load()
	if err != nil {
		t.Fatal(err)
	}
	s.Registrations["client-existing"].ExpiresAt = time.Now().Add(-time.Minute)
	if err := f.m.save(s); err != nil {
		t.Fatal(err)
	}
	f.tokenStatus, f.tokens = http.StatusServiceUnavailable, map[string]any{"error": "temporarily_unavailable"}
	if _, err := f.m.AccessToken(context.Background(), "client-existing"); err == nil {
		t.Fatal("temporary refresh failure returned a token")
	}
	s, err = f.m.load()
	if err != nil || s.Registrations["client-existing"].RefreshToken == "" {
		t.Fatal("temporary failure erased refresh credentials")
	}
	f.tokenStatus = http.StatusOK
	f.tokens = f.response(t, "client-existing", "subject-existing", "", DirectScope)
	if _, err := f.m.AccessToken(context.Background(), "client-existing"); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshRetainsOmittedRefreshTokenAndGrantedScopes(t *testing.T) {
	f := testManager(t)
	f.login(t, "client-existing", "subject-existing")
	s, err := f.m.load()
	if err != nil {
		t.Fatal(err)
	}
	s.Registrations["client-existing"].ExpiresAt = time.Now().Add(-time.Minute)
	if err := f.m.save(s); err != nil {
		t.Fatal(err)
	}
	f.tokens = map[string]any{"access_token": "refreshed-access", "expires_in": 3600, "token_type": "Bearer"}
	token, err := f.m.AccessToken(context.Background(), "client-existing")
	if err != nil || token.AccessToken != "refreshed-access" || !hasScope(token.Scopes, DirectScope) {
		t.Fatalf("non-rotating refresh: %v, %v", token, err)
	}
	s, err = f.m.load()
	if err != nil || s.Registrations["client-existing"].RefreshToken != "refresh-client-existing" {
		t.Fatal("omitted refresh token erased the existing refresh credential")
	}
	s.Registrations["client-existing"].ExpiresAt = time.Now().Add(-time.Minute)
	if err := f.m.save(s); err != nil {
		t.Fatal(err)
	}
	f.tokens["scope"] = "openid email"
	if _, err := f.m.AccessToken(context.Background(), "client-existing"); err == nil {
		t.Fatal("explicit loss of plan permission still allowed inference")
	}
}

func TestTokenDiagnosticsDoNotSerializeBearerMaterial(t *testing.T) {
	token := Token{AccessToken: "opaque-fixture-bearer"}
	encoded, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{string(encoded), fmt.Sprint(token), fmt.Sprintf("%+v", token), fmt.Sprintf("%#v", token)} {
		if strings.Contains(text, token.AccessToken) {
			t.Fatal("token diagnostics disclose bearer material")
		}
	}
}

func TestLoopbackLoginRejectsWrongStateThenAcceptsValidCallback(t *testing.T) {
	f := testManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	callbackClient := &http.Client{Timeout: time.Second}
	account, err := f.m.Login(ctx, LoginOptions{Authorize: func(auth Authorization) error {
		authorize, err := url.Parse(auth.URL)
		if err != nil {
			return err
		}
		callback, err := url.Parse(authorize.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		if callback.Scheme != "http" || callback.Hostname() != "127.0.0.1" || callback.Path != "/auth/callback" {
			return fmt.Errorf("callback is not the expected loopback URI")
		}
		f.tokens = f.response(t, "client-loopback", "subject-loopback", authorize.Query().Get("nonce"), DirectScope)
		query := url.Values{"state": {"wrong-state"}, "code": {"fixture-code"}, "client_id": {"client-loopback"}}
		callback.RawQuery = query.Encode()
		bad, err := callbackClient.Get(callback.String())
		if err != nil {
			return err
		}
		bad.Body.Close()
		if bad.StatusCode < 400 {
			return fmt.Errorf("wrong-state callback was accepted")
		}
		query.Set("state", authorize.Query().Get("state"))
		callback.RawQuery = query.Encode()
		good, err := callbackClient.Get(callback.String())
		if err != nil {
			return err
		}
		defer good.Body.Close()
		body, err := io.ReadAll(good.Body)
		if err != nil {
			return err
		}
		if good.StatusCode != http.StatusOK || strings.Contains(string(body), "fixture-code") {
			return fmt.Errorf("callback failed or exposed its authorization code")
		}
		return nil
	}})
	if err != nil || account.ClientID != "client-loopback" || !account.PlanEnabled {
		t.Fatalf("loopback sign-in: %+v, %v", account, err)
	}
}

func TestCancelledLoginClosesLoopbackListener(t *testing.T) {
	f := testManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var address string
	_, err := f.m.Login(ctx, LoginOptions{Authorize: func(auth Authorization) error {
		u, _ := url.Parse(auth.URL)
		callback, _ := url.Parse(u.Query().Get("redirect_uri"))
		address = callback.Host
		cancel()
		return nil
	}})
	if err == nil || address == "" {
		t.Fatal("cancelled login did not stop its attempt")
	}
	conn, dialErr := net.DialTimeout("tcp", address, time.Second)
	if conn != nil {
		conn.Close()
	}
	if dialErr == nil {
		t.Fatal("cancelled login left its callback listener open")
	}
}
