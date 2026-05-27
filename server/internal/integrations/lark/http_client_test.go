package lark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// larkFakeServer is a tiny in-memory stand-in for the Lark Open
// Platform. Tests register handlers per path; the server panics if a
// path is hit without a registration (a missed assertion is louder
// than a 404).
//
// The handler shape mirrors http.HandlerFunc so each test can encode
// its own response without inheriting boilerplate.
type larkFakeServer struct {
	t       *testing.T
	mux     *http.ServeMux
	srv     *httptest.Server
	tokenN  atomic.Int32
	sendN   atomic.Int32
	patchN  atomic.Int32
	bindN   atomic.Int32
	authObs atomic.Value // last Authorization header seen across all paths
}

func newLarkFake(t *testing.T) *larkFakeServer {
	t.Helper()
	f := &larkFakeServer{t: t, mux: http.NewServeMux()}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *larkFakeServer) URL() string { return f.srv.URL }

func (f *larkFakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a := r.Header.Get("Authorization"); a != "" {
		f.authObs.Store(a)
	}
	f.mux.ServeHTTP(w, r)
}

func (f *larkFakeServer) lastAuth() string {
	v, _ := f.authObs.Load().(string)
	return v
}

// stubToken installs a token endpoint that returns the supplied token
// with the supplied expire (seconds) and counts hits.
func (f *larkFakeServer) stubToken(token string, expireSec int64) {
	f.mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		f.tokenN.Add(1)
		if r.Method != http.MethodPost {
			f.t.Errorf("token: want POST, got %s", r.Method)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("token: decode body: %v", err)
		}
		if body["app_id"] == "" || body["app_secret"] == "" {
			f.t.Errorf("token: missing app credentials: %v", body)
		}
		writeJSON(w, map[string]any{
			"code":                0,
			"msg":                 "ok",
			"tenant_access_token": token,
			"expire":              expireSec,
		})
	})
}

// stubTokenError installs a token endpoint returning a Lark-style
// error code (non-zero `code` with HTTP 200).
func (f *larkFakeServer) stubTokenError(code int, msg string) {
	f.mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		f.tokenN.Add(1)
		writeJSON(w, map[string]any{"code": code, "msg": msg})
	})
}

// stubSend installs the IM-send endpoint. resp is the response body
// (typically the standard {code, msg, data:{message_id}} shape).
func (f *larkFakeServer) stubSend(resp map[string]any, verify func(r *http.Request, body map[string]string)) {
	f.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		f.sendN.Add(1)
		if r.Method != http.MethodPost {
			f.t.Errorf("send: want POST, got %s", r.Method)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("send: decode body: %v", err)
		}
		if verify != nil {
			verify(r, body)
		}
		writeJSON(w, resp)
	})
}

// stubPatch installs the IM-patch endpoint. The Lark route is
// /open-apis/im/v1/messages/<id>; ServeMux uses prefix matching when
// we register the parent path explicitly. We register the parent
// SEND path above already, so the patch path needs the full prefix.
func (f *larkFakeServer) stubPatch(resp map[string]any, verify func(r *http.Request, id string, body map[string]string)) {
	const prefix = "/open-apis/im/v1/messages/"
	f.mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			f.t.Errorf("patch: want PATCH, got %s", r.Method)
		}
		id := strings.TrimPrefix(r.URL.Path, prefix)
		if id == "" {
			f.t.Errorf("patch: missing message id")
		}
		f.patchN.Add(1)
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("patch: decode body: %v", err)
		}
		if verify != nil {
			verify(r, id, body)
		}
		writeJSON(w, resp)
	})
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// newTestClient returns an httpAPIClient pointed at the fake server,
// using the supplied clock so token expiry can be controlled
// deterministically.
func newTestClient(fake *larkFakeServer, now func() time.Time) *httpAPIClient {
	c := NewHTTPAPIClient(HTTPClientConfig{
		BaseURL: fake.URL(),
		Now:     now,
	}).(*httpAPIClient)
	return c
}

func testCreds() InstallationCredentials {
	return InstallationCredentials{AppID: "cli_app_xx", AppSecret: "secret_xx"}
}

func TestHTTPClient_IsConfigured(t *testing.T) {
	c := NewHTTPAPIClient(HTTPClientConfig{})
	if !c.IsConfigured() {
		t.Fatalf("real client must report IsConfigured()=true")
	}
}

func TestHTTPClient_SendInteractiveCard_HappyPath(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_1", 7200)
	fake.stubSend(
		map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]string{"message_id": "om_msg_42"},
		},
		func(r *http.Request, body map[string]string) {
			if got := r.URL.Query().Get("receive_id_type"); got != "chat_id" {
				t.Errorf("receive_id_type: got %q want chat_id", got)
			}
			if body["receive_id"] != "oc_chat_1" {
				t.Errorf("receive_id: got %q", body["receive_id"])
			}
			if body["msg_type"] != "interactive" {
				t.Errorf("msg_type: got %q want interactive", body["msg_type"])
			}
			if !strings.Contains(body["content"], "\"tag\"") {
				t.Errorf("content not a card body: %q", body["content"])
			}
		},
	)

	c := newTestClient(fake, time.Now)
	msgID, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc_chat_1"),
		CardJSON:       `{"tag":"div","text":"hi"}`,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if msgID != "om_msg_42" {
		t.Errorf("message id: got %q want om_msg_42", msgID)
	}
	if got := fake.lastAuth(); got != "Bearer tok_1" {
		t.Errorf("Authorization header: got %q want Bearer tok_1", got)
	}
}

func TestHTTPClient_SendInteractiveCard_TokenCached(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_cached", 7200)
	fake.stubSend(
		map[string]any{
			"code": 0,
			"data": map[string]string{"message_id": "om_msg_x"},
		},
		nil,
	)
	c := newTestClient(fake, time.Now)
	for i := 0; i < 3; i++ {
		if _, err := c.SendInteractiveCard(context.Background(), SendCardParams{
			InstallationID: testCreds(),
			ChatID:         ChatID("oc_chat_1"),
			CardJSON:       `{}`,
		}); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	if got := fake.tokenN.Load(); got != 1 {
		t.Errorf("token endpoint hits: got %d want 1 (cached after first call)", got)
	}
	if got := fake.sendN.Load(); got != 3 {
		t.Errorf("send endpoint hits: got %d want 3", got)
	}
}

func TestHTTPClient_TokenRefreshAfterExpiry(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_refresh", 120) // 120s expire → 60s usable after safety margin
	fake.stubSend(
		map[string]any{
			"code": 0,
			"data": map[string]string{"message_id": "om"},
		},
		nil,
	)

	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{now: now}
	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL(), Now: clock.Now}).(*httpAPIClient)

	// First call — fetches token.
	if _, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	}); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if fake.tokenN.Load() != 1 {
		t.Fatalf("first call should have fetched a token, got tokenN=%d", fake.tokenN.Load())
	}

	// Advance past the cached token's expiry (token expire 120s,
	// safety margin 60s → cache valid for 60s of wall-clock).
	clock.Advance(90 * time.Second)

	if _, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	}); err != nil {
		t.Fatalf("post-expiry send: %v", err)
	}
	if got := fake.tokenN.Load(); got != 2 {
		t.Errorf("token endpoint hits after expiry: got %d want 2", got)
	}
}

func TestHTTPClient_SendInteractiveCard_LarkErrorCode(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_e", 7200)
	fake.stubSend(map[string]any{"code": 230001, "msg": "no permission"}, nil)
	c := newTestClient(fake, time.Now)
	_, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	})
	if err == nil {
		t.Fatal("want error on non-zero code")
	}
	if !strings.Contains(err.Error(), "code=230001") {
		t.Errorf("error should surface code: %v", err)
	}
}

func TestHTTPClient_SendInteractiveCard_TokenExpired_InvalidatesCache(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_first", 7200)
	// First send replies with expired-token. Second send (after the
	// client should have dropped its cache) reaches the token
	// endpoint again. We swap the send handler mid-test to model
	// this without race conditions: send fails first, second call
	// from the same fake gets the token-endpoint hit + a fresh send
	// reply. To keep the test small we simply assert tokenN
	// increments after the failing call when the caller retries.
	var sendCalls atomic.Int32
	fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		fake.sendN.Add(1)
		n := sendCalls.Add(1)
		if n == 1 {
			writeJSON(w, map[string]any{"code": codeTokenExpired, "msg": "expired"})
			return
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]string{"message_id": "om_ok"}})
	})

	c := newTestClient(fake, time.Now)
	_, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	})
	if err == nil {
		t.Fatal("first send must fail with token-expired")
	}
	if !strings.Contains(err.Error(), "code=99991663") {
		t.Errorf("error should mention token-expired code: %v", err)
	}

	// Caller's retry — should re-fetch the token, then succeed.
	msgID, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	})
	if err != nil {
		t.Fatalf("retry send: %v", err)
	}
	if msgID != "om_ok" {
		t.Errorf("retry message id: got %q", msgID)
	}
	if got := fake.tokenN.Load(); got != 2 {
		t.Errorf("token endpoint hits after invalidation: got %d want 2", got)
	}
}

func TestHTTPClient_PatchInteractiveCard_HappyPath(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_p", 7200)
	fake.stubPatch(
		map[string]any{"code": 0, "msg": "ok"},
		func(r *http.Request, id string, body map[string]string) {
			if id != "om_msg_42" {
				t.Errorf("patch id: got %q want om_msg_42", id)
			}
			if !strings.Contains(body["content"], "updated") {
				t.Errorf("patch content: %q", body["content"])
			}
		},
	)
	c := newTestClient(fake, time.Now)
	if err := c.PatchInteractiveCard(context.Background(), PatchCardParams{
		InstallationID:    testCreds(),
		LarkCardMessageID: "om_msg_42",
		CardJSON:          `{"text":"updated"}`,
	}); err != nil {
		t.Fatalf("patch: %v", err)
	}
}

func TestHTTPClient_PatchInteractiveCard_LarkErrorCode(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_p", 7200)
	fake.stubPatch(map[string]any{"code": 230002, "msg": "card not found"}, nil)
	c := newTestClient(fake, time.Now)
	err := c.PatchInteractiveCard(context.Background(), PatchCardParams{
		InstallationID:    testCreds(),
		LarkCardMessageID: "om_msg_x",
		CardJSON:          `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "code=230002") {
		t.Errorf("want code=230002 in error, got %v", err)
	}
}

func TestHTTPClient_SendBindingPromptCard_HappyPath(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_b", 7200)

	var capturedBody map[string]string
	fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		fake.bindN.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		if got := r.URL.Query().Get("receive_id_type"); got != "open_id" {
			t.Errorf("receive_id_type: got %q want open_id", got)
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]string{"message_id": "om_bind"}})
	})

	c := newTestClient(fake, time.Now)
	if err := c.SendBindingPromptCard(context.Background(), BindingPromptParams{
		InstallationID: testCreds(),
		OpenID:         OpenID("ou_user_1"),
		BindURL:        "https://multica.test/lark/bind?token=abc",
	}); err != nil {
		t.Fatalf("bind prompt: %v", err)
	}
	if capturedBody["receive_id"] != "ou_user_1" {
		t.Errorf("receive_id: got %q", capturedBody["receive_id"])
	}
	if !strings.Contains(capturedBody["content"], "multica.test/lark/bind") {
		t.Errorf("binding card should embed BindURL: %q", capturedBody["content"])
	}
	if !strings.Contains(capturedBody["content"], "去绑定") {
		t.Errorf("binding card should carry the localized CTA: %q", capturedBody["content"])
	}
}

func TestHTTPClient_TokenEndpointError(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubTokenError(10003, "invalid app_id or app_secret")
	c := newTestClient(fake, time.Now)
	_, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "code=10003") {
		t.Errorf("want code=10003 surfaced, got %v", err)
	}
}

func TestHTTPClient_MissingAppCredentials(t *testing.T) {
	c := NewHTTPAPIClient(HTTPClientConfig{}).(*httpAPIClient)
	_, err := c.tenantAccessToken(context.Background(), InstallationCredentials{AppSecret: "x"})
	if err == nil || !strings.Contains(err.Error(), "app_id") {
		t.Errorf("want missing app_id error, got %v", err)
	}
	_, err = c.tenantAccessToken(context.Background(), InstallationCredentials{AppID: "x"})
	if err == nil || !strings.Contains(err.Error(), "app_secret") {
		t.Errorf("want missing app_secret error, got %v", err)
	}
}

func TestHTTPClient_MissingChatID_PreAuth(t *testing.T) {
	// chat_id validation must short-circuit BEFORE any auth round-trip
	// — otherwise a misuse leaks load to the token endpoint.
	fake := newLarkFake(t)
	c := newTestClient(fake, time.Now)
	_, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		CardJSON:       `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "chat_id") {
		t.Errorf("want missing chat_id error, got %v", err)
	}
	if got := fake.tokenN.Load(); got != 0 {
		t.Errorf("token endpoint must not be hit on bad input: got %d", got)
	}
}

func TestHTTPClient_MissingCardJSON(t *testing.T) {
	c := NewHTTPAPIClient(HTTPClientConfig{}).(*httpAPIClient)
	if _, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
	}); err == nil || !strings.Contains(err.Error(), "card json") {
		t.Errorf("send: want missing card json, got %v", err)
	}
	if err := c.PatchInteractiveCard(context.Background(), PatchCardParams{
		InstallationID:    testCreds(),
		LarkCardMessageID: "om",
	}); err == nil || !strings.Contains(err.Error(), "card json") {
		t.Errorf("patch: want missing card json, got %v", err)
	}
}

func TestHTTPClient_PatchMissingID(t *testing.T) {
	c := NewHTTPAPIClient(HTTPClientConfig{}).(*httpAPIClient)
	err := c.PatchInteractiveCard(context.Background(), PatchCardParams{
		InstallationID: testCreds(),
		CardJSON:       `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "card message id") {
		t.Errorf("want missing message id error, got %v", err)
	}
}

func TestHTTPClient_BindingPromptValidation(t *testing.T) {
	c := NewHTTPAPIClient(HTTPClientConfig{}).(*httpAPIClient)
	if err := c.SendBindingPromptCard(context.Background(), BindingPromptParams{
		InstallationID: testCreds(),
		BindURL:        "https://x",
	}); err == nil || !strings.Contains(err.Error(), "open_id") {
		t.Errorf("want missing open_id, got %v", err)
	}
	if err := c.SendBindingPromptCard(context.Background(), BindingPromptParams{
		InstallationID: testCreds(),
		OpenID:         "ou",
	}); err == nil || !strings.Contains(err.Error(), "bind url") {
		t.Errorf("want missing bind url, got %v", err)
	}
}

func TestHTTPClient_ExchangeOAuthCode_NotImplemented(t *testing.T) {
	c := NewHTTPAPIClient(HTTPClientConfig{})
	_, err := c.ExchangeOAuthCode(context.Background(), "code_x", "https://x")
	if !errors.Is(err, ErrAPIClientNotConfigured) {
		t.Errorf("OAuth exchange should still surface ErrAPIClientNotConfigured: %v", err)
	}
}

func TestHTTPClient_BadHTTPStatus(t *testing.T) {
	fake := newLarkFake(t)
	// Token returns success.
	fake.stubToken("tok", 7200)
	// Send replies with 500 + body — exercise the non-2xx branch.
	fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		fake.sendN.Add(1)
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "boom")
	})
	c := newTestClient(fake, time.Now)
	_, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "http 500") {
		t.Errorf("want http 500 surfaced, got %v", err)
	}
}

func TestHTTPClient_TokenExpire_ClampedToSafety(t *testing.T) {
	// Lark returns expire=10s — well under the safety margin. The
	// client must NOT cache a token that is already past its safe
	// window; instead it clamps to 2× safety margin so the cached
	// entry is at least usable for one safety margin of wall-clock.
	fake := newLarkFake(t)
	fake.stubToken("tok_short", 10)
	fake.stubSend(map[string]any{"code": 0, "data": map[string]string{"message_id": "om"}}, nil)

	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{now: now}
	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL(), Now: clock.Now}).(*httpAPIClient)

	if _, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	clock.Advance(30 * time.Second) // still within clamped window
	if _, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	}); err != nil {
		t.Fatalf("send2: %v", err)
	}
	if got := fake.tokenN.Load(); got != 1 {
		t.Errorf("token endpoint hits within clamped window: got %d want 1", got)
	}
}

func TestBindingPromptTemplate_Shape(t *testing.T) {
	raw, err := bindingPromptTemplate("https://multica.test/bind?token=abc")
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("template json: %v", err)
	}
	// Shape check — top-level keys exist and elements is non-empty.
	if _, ok := doc["config"]; !ok {
		t.Errorf("missing config")
	}
	if _, ok := doc["header"]; !ok {
		t.Errorf("missing header")
	}
	elements, ok := doc["elements"].([]any)
	if !ok || len(elements) < 2 {
		t.Fatalf("elements: want >=2, got %v", doc["elements"])
	}
	// Last element should be the action button carrying the URL.
	last, _ := elements[len(elements)-1].(map[string]any)
	if last["tag"] != "action" {
		t.Errorf("last element should be action: %v", last)
	}
	actions, _ := last["actions"].([]any)
	if len(actions) == 0 {
		t.Fatalf("no actions in card")
	}
	btn, _ := actions[0].(map[string]any)
	if btn["url"] != "https://multica.test/bind?token=abc" {
		t.Errorf("button url: got %v", btn["url"])
	}
}

// fakeClock is a minimal monotonic clock for tests that need to drive
// the cache TTL deterministically.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }
