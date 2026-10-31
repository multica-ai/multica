package ghuser

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	svc, _ := newTestService(t, nil, nil, nil)
	token, err := svc.SignState("user-1", "/acme/issues/MUL-1?tab=prs")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.VerifyState(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != "user-1" || got.ReturnTo != "/acme/issues/MUL-1?tab=prs" {
		t.Fatalf("state = %+v", got)
	}
	again, _ := svc.SignState("user-1", "/")
	if again == token {
		t.Fatal("two states for the same user must differ (nonce)")
	}
}

func TestStateRejectsTamperingExpiryAndForeignKeys(t *testing.T) {
	svc, clk := newTestService(t, nil, nil, nil)
	token, err := svc.SignState("user-1", "/")
	if err != nil {
		t.Fatal(err)
	}
	payload, sig, _ := strings.Cut(token, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(payload)
	forged := base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(raw), "user-1", "user-2", 1)))

	other, _ := newTestService(t, nil, nil, nil)
	cases := map[string]struct {
		svc   *Service
		token string
	}{
		"empty":            {svc, ""},
		"no signature":     {svc, payload},
		"swapped user":     {svc, forged + "." + sig},
		"bad signature":    {svc, payload + "." + sig[:len(sig)-2] + "AA"},
		"other deployment": {other, token},
		"garbage":          {svc, "a.b.c"},
	}
	for name, tc := range cases {
		if _, err := tc.svc.VerifyState(tc.token); !errors.Is(err, ErrInvalidState) {
			t.Errorf("%s: err = %v, want ErrInvalidState", name, err)
		}
	}

	clk.Advance(StateTTL + time.Second)
	if _, err := svc.VerifyState(token); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expired state: err = %v", err)
	}
}

func TestStateRequiresConfiguration(t *testing.T) {
	var svc *Service
	if _, err := svc.SignState("u", "/"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("sign on nil service: %v", err)
	}
	if _, err := svc.VerifyState("x.y"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("verify on nil service: %v", err)
	}
}

func TestAuthorizeURL(t *testing.T) {
	svc, _ := newTestService(t, nil, nil, nil)
	u, err := url.Parse(svc.AuthorizeURL("st.sig", "https://api.example/api/github/user/callback"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "github.com" || u.Path != "/login/oauth/authorize" || q.Get("client_id") != testClientID ||
		q.Get("state") != "st.sig" || q.Get("redirect_uri") != "https://api.example/api/github/user/callback" {
		t.Fatalf("authorize url = %s", u)
	}
	if strings.Contains(svc.AuthorizeURL("s", ""), "redirect_uri") {
		t.Fatal("empty redirect URI must be omitted so GitHub uses the registered callback")
	}
}

func TestNewFromEnvRequiresAllSettings(t *testing.T) {
	t.Setenv("GITHUB_APP_CLIENT_ID", testClientID)
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "")
	t.Setenv("MULTICA_GITHUB_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	svc, err := NewFromEnv(nil, nil)
	if err != nil || svc != nil || svc.Configured() {
		t.Fatalf("missing secret: svc=%v err=%v", svc, err)
	}

	t.Setenv("GITHUB_APP_CLIENT_SECRET", testClientSecret)
	svc, err = NewFromEnv(nil, nil)
	if err != nil || !svc.Configured() {
		t.Fatalf("complete config: svc=%v err=%v", svc, err)
	}

	t.Setenv("MULTICA_GITHUB_SECRET_KEY", "too-short")
	if _, err := NewFromEnv(nil, nil); err == nil {
		t.Fatal("a malformed key must be reported")
	}
}
