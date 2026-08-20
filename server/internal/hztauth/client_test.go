package hztauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func testClient(t *testing.T, internalURL string) *Client {
	t.Helper()
	client, err := New(Config{
		PublicURL: "http://192.168.1.10:8080", InternalURL: internalURL,
		FrontendURL: "http://192.168.1.20:3000", ClientSecret: "client-secret-at-least-32-characters",
		FlowSecret: "flow-secret-at-least-32-characters", Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestFlowRoundTripAndAuthorizeURL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client := testClient(t, "http://127.0.0.1:1")
	flow, err := client.NewFlow("/acme/issues", now)
	if err != nil {
		t.Fatalf("NewFlow: %v", err)
	}
	parsed, err := client.ParseFlow(flow.Cookie, flow.State, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ParseFlow: %v", err)
	}
	if parsed.Next != "/acme/issues" || parsed.Verifier != flow.Verifier {
		t.Fatalf("unexpected parsed flow: %+v", parsed)
	}
	authorize, err := url.Parse(client.AuthorizeURL(flow))
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	if authorize.Host != "192.168.1.10:8080" || authorize.Query().Get("client_id") != "multica" || authorize.Query().Get("redirect_uri") != "http://192.168.1.20:3000/auth/hzt/callback" || authorize.Query().Get("code_challenge") != flow.Challenge || authorize.Query().Get("state") != flow.State {
		t.Fatalf("unexpected authorize URL: %s", authorize.String())
	}
}

func TestFlowRejectsTamperingStateAndExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client := testClient(t, "http://127.0.0.1:1")
	flow, err := client.NewFlow("", now)
	if err != nil {
		t.Fatalf("NewFlow: %v", err)
	}
	for name, testCase := range map[string]struct {
		cookie string
		state  string
		at     time.Time
	}{
		"tampered": {flow.Cookie + "x", flow.State, now},
		"state":    {flow.Cookie, "wrong-state", now},
		"expired":  {flow.Cookie, flow.State, now.Add(11 * time.Minute)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := client.ParseFlow(testCase.cookie, testCase.state, testCase.at); err == nil {
				t.Fatal("expected flow rejection")
			}
		})
	}
}

func TestExchangePostsServerCredentialsAndParsesIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sso/token" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["client_id"] != "multica" || body["redirect_uri"] != "http://192.168.1.20:3000/auth/hzt/callback" || body["client_secret"] != "client-secret-at-least-32-characters" || body["code_verifier"] != "verifier" {
			t.Fatalf("unexpected token request: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":300,"user":{"id":"hzt-id","username":"admin","email":"admin@example.com","displayName":"Admin","role":"admin","roles":[{"slug":"admin"}]}}`))
	}))
	defer server.Close()

	identity, err := testClient(t, server.URL).Exchange(context.Background(), "code", "verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if identity.ID != "hzt-id" || identity.Email == nil || *identity.Email != "admin@example.com" || len(identity.Roles) != 1 {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}
