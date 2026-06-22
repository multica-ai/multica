package lark

import (
	"context"
	"encoding/json"
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
	reactN  atomic.Int32
	delRN   atomic.Int32
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

// stubReaction installs the IM-reaction-create endpoint.
func (f *larkFakeServer) stubReaction(resp map[string]any, verify func(r *http.Request, id string, body map[string]any)) {
	const suffix = "/reactions"
	f.mux.HandleFunc("/open-apis/im/v1/messages/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, suffix) {
			return // let other handlers match
		}
		if r.Method != http.MethodPost {
			f.t.Errorf("reaction: want POST, got %s", r.Method)
		}
		f.reactN.Add(1)
		rawID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/open-apis/im/v1/messages/"), suffix)
		if rawID == "" {
			f.t.Errorf("reaction: missing message id")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("reaction: decode body: %v", err)
		}
		if verify != nil {
			verify(r, rawID, body)
		}
		writeJSON(w, resp)
	})
}

// stubReactionDelete installs the IM-reaction-delete endpoint.
func (f *larkFakeServer) stubReactionDelete(resp map[string]any, verify func(r *http.Request, msgID string, reactionID string)) {
	const prefix = "/open-apis/im/v1/messages/"
	f.mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			return // let other handlers match
		}
		rest := strings.TrimPrefix(r.URL.Path, prefix)
		parts := strings.Split(rest, "/reactions/")
		if len(parts) != 2 {
			return // not a delete path
		}
		f.delRN.Add(1)
		if parts[0] == "" {
			f.t.Errorf("reaction delete: missing message id")
		}
		if parts[1] == "" {
			f.t.Errorf("reaction delete: missing reaction id")
		}
		if verify != nil {
			verify(r, parts[0], parts[1])
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

// TestHTTPClient_StubReportsNotConfigured pins that the stub never
// claims wired outbound — handlers gate install / management UI on
// this signal.
func TestHTTPClient_StubReportsNotConfigured(t *testing.T) {
	s := NewStubAPIClient(nil)
	if s.IsConfigured() {
		t.Errorf("stub IsConfigured must be false")
	}
}

// TestHTTPClient_SendInteractiveCard_DefaultRendererBodyHasUpdateMulti
// is the send-side half of the must-fix wire check: when the Patcher
// uses NewDefaultRenderer to produce a card and ships it via
// SendInteractiveCard, the actual HTTP body Lark receives must carry
// config.update_multi=true so the card is patchable downstream.
// Without this, the first send succeeds but every subsequent patch
// silently no-ops on Lark's side while local DB status still flips.
func TestHTTPClient_SendInteractiveCard_DefaultRendererBodyHasUpdateMulti(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_um_send", 7200)
	var capturedContent string
	fake.stubSend(
		map[string]any{"code": 0, "data": map[string]string{"message_id": "om_send_um"}},
		func(_ *http.Request, body map[string]string) {
			capturedContent = body["content"]
		},
	)

	r := NewDefaultRenderer()
	render, err := r.Render(RenderInput{Kind: CardKindThinking, AgentName: "TestAgent"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	c := newTestClient(fake, time.Now)
	if _, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc_send_um"),
		CardJSON:       render.JSON,
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	assertCardContentHasUpdateMulti(t, capturedContent)
}

// TestHTTPClient_PatchInteractiveCard_DefaultRendererBodyHasUpdateMulti
// is the patch-side half of the same wire check. Every PatchCardParams
// the Patcher produces goes through the default renderer; the body
// shipped over PATCH /open-apis/im/v1/messages/:id must still carry
// update_multi=true, otherwise Lark refuses to apply the patch to a
// card that was sent with update_multi=true (the two ends must agree).
func TestHTTPClient_PatchInteractiveCard_DefaultRendererBodyHasUpdateMulti(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_um_patch", 7200)
	var capturedContent string
	fake.stubPatch(
		map[string]any{"code": 0, "msg": "ok"},
		func(_ *http.Request, _ string, body map[string]string) {
			capturedContent = body["content"]
		},
	)

	r := NewDefaultRenderer()
	render, err := r.Render(RenderInput{Kind: CardKindRunning, AgentName: "TestAgent"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	c := newTestClient(fake, time.Now)
	if err := c.PatchInteractiveCard(context.Background(), PatchCardParams{
		InstallationID:    testCreds(),
		LarkCardMessageID: "om_patch_um",
		CardJSON:          render.JSON,
	}); err != nil {
		t.Fatalf("patch: %v", err)
	}

	assertCardContentHasUpdateMulti(t, capturedContent)
}

func assertCardContentHasUpdateMulti(t *testing.T, content string) {
	t.Helper()
	if content == "" {
		t.Fatalf("captured content empty — fake server did not receive the request body")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("card content is not valid JSON: %v (raw=%s)", err, content)
	}
	cfg, ok := doc["config"].(map[string]any)
	if !ok {
		t.Fatalf("card content missing config block (raw=%s)", content)
	}
	if v, _ := cfg["update_multi"].(bool); !v {
		t.Fatalf("config.update_multi must be true so the card is patchable on Lark's side; got config=%v (raw=%s)", cfg, content)
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

// TestHTTPClient_SendTextMessage_HappyPath pins the wire shape of the
// plain text outbound used for chat replies + /issue confirmations.
// Path, query, bearer auth, msg_type, and the double-JSON-encoded
// `content` envelope all matter — Lark rejects anything off-spec and
// the failures are silent-but-non-2xx, which is hard to debug
// in production without this kind of contract pin.
func TestHTTPClient_SendTextMessage_HappyPath(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_text", 7200)
	fake.stubSend(
		map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]string{"message_id": "om_text_1"},
		},
		func(r *http.Request, body map[string]string) {
			if r.URL.Path != "/open-apis/im/v1/messages" {
				t.Errorf("path: got %q want /open-apis/im/v1/messages", r.URL.Path)
			}
			if got := r.URL.Query().Get("receive_id_type"); got != "chat_id" {
				t.Errorf("receive_id_type: got %q want chat_id", got)
			}
			if body["receive_id"] != "oc_chat_42" {
				t.Errorf("receive_id: got %q want oc_chat_42", body["receive_id"])
			}
			if body["msg_type"] != "text" {
				t.Errorf("msg_type: got %q want text (NOT interactive — chat replies are plain bubbles)", body["msg_type"])
			}
			// content is a JSON-encoded string Lark requires: the outer
			// HTTP body is JSON, and `content` is another JSON
			// document INSIDE it. Decode and inspect.
			var inner map[string]string
			if err := json.Unmarshal([]byte(body["content"]), &inner); err != nil {
				t.Fatalf("content is not valid inner JSON: %v (raw=%q)", err, body["content"])
			}
			if inner["text"] != "Hello world" {
				t.Errorf("inner content.text: got %q want Hello world", inner["text"])
			}
		},
	)

	c := newTestClient(fake, time.Now)
	msgID, err := c.SendTextMessage(context.Background(), SendTextParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc_chat_42"),
		Text:           "Hello world",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if msgID != "om_text_1" {
		t.Errorf("message id: got %q want om_text_1", msgID)
	}
	if got := fake.lastAuth(); got != "Bearer tok_text" {
		t.Errorf("Authorization header: got %q want Bearer tok_text", got)
	}
}

// TestHTTPClient_SendMarkdownCard_HappyPath pins the wire shape of the
// schema-2.0 card we send for markdown chat replies. The MUST-haves:
// msg_type=interactive (not text), content is a JSON-encoded card
// envelope, the card has `schema: "2.0"` at the top level, and the
// body element is `{tag: "markdown", content: <agent's md verbatim>}`.
// Lark rejects malformed cards with a generic 9499xxxx code that's
// painful to root-cause in production, so we contract-pin every level.
func TestHTTPClient_SendMarkdownCard_HappyPath(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_md", 7200)
	fake.stubSend(
		map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]string{"message_id": "om_md_1"},
		},
		func(r *http.Request, body map[string]string) {
			if r.URL.Path != "/open-apis/im/v1/messages" {
				t.Errorf("path: got %q want /open-apis/im/v1/messages", r.URL.Path)
			}
			if got := r.URL.Query().Get("receive_id_type"); got != "chat_id" {
				t.Errorf("receive_id_type: got %q want chat_id", got)
			}
			if body["msg_type"] != "interactive" {
				t.Errorf("msg_type: got %q want interactive (markdown cards ride the interactive endpoint)", body["msg_type"])
			}
			var card map[string]any
			if err := json.Unmarshal([]byte(body["content"]), &card); err != nil {
				t.Fatalf("content is not valid card JSON: %v (raw=%q)", err, body["content"])
			}
			if card["schema"] != "2.0" {
				t.Errorf("card.schema: got %v want \"2.0\"", card["schema"])
			}
			bodyDoc, _ := card["body"].(map[string]any)
			elements, _ := bodyDoc["elements"].([]any)
			if len(elements) != 1 {
				t.Fatalf("expected exactly one body element; got %d", len(elements))
			}
			el, _ := elements[0].(map[string]any)
			if el["tag"] != "markdown" {
				t.Errorf("element.tag: got %v want \"markdown\"", el["tag"])
			}
			if el["content"] != "# Heading\n- list" {
				t.Errorf("markdown body must be forwarded verbatim; got %q", el["content"])
			}
		},
	)

	c := newTestClient(fake, time.Now)
	msgID, err := c.SendMarkdownCard(context.Background(), SendMarkdownCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc_chat_42"),
		Markdown:       "# Heading\n- list",
	})
	if err != nil {
		t.Fatalf("send markdown card: %v", err)
	}
	if msgID != "om_md_1" {
		t.Errorf("message id: got %q want om_md_1", msgID)
	}
	if got := fake.lastAuth(); got != "Bearer tok_md" {
		t.Errorf("Authorization header: got %q want Bearer tok_md", got)
	}
}

// TestHTTPClient_SendTextMessage_EncodesSpecialCharacters guards the
// inner JSON envelope's escaping. Lark's spec is "content MUST be a
// JSON-encoded string", which means newlines and quotes have to be
// double-escaped — once when we marshal the inner `{"text": ...}`,
// then once more implicitly when the outer body is encoded for the
// HTTP request. Forgetting either pass corrupts the text Lark renders
// (or worse, rejects the message with a body parse error).
func TestHTTPClient_SendTextMessage_EncodesSpecialCharacters(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"multiline", "first line\nsecond line"},
		{"double_quote", `she said "hi"`},
		{"backslash", `path\to\file`},
		{"chinese", "你好，世界 🌏"},
		{"tab_and_newline", "col1\tcol2\nrow2"},
		{"json_lookalike", `{"fake": "json"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newLarkFake(t)
			fake.stubToken("tok", 7200)
			fake.stubSend(
				map[string]any{"code": 0, "data": map[string]string{"message_id": "om_x"}},
				func(r *http.Request, body map[string]string) {
					var inner map[string]string
					if err := json.Unmarshal([]byte(body["content"]), &inner); err != nil {
						t.Fatalf("content envelope not valid JSON after wire-encode round trip: %v (raw=%q)", err, body["content"])
					}
					if inner["text"] != tc.text {
						t.Errorf("text round-trip failed\n  got:  %q\n  want: %q", inner["text"], tc.text)
					}
				},
			)
			c := newTestClient(fake, time.Now)
			if _, err := c.SendTextMessage(context.Background(), SendTextParams{
				InstallationID: testCreds(),
				ChatID:         ChatID("oc_chat_1"),
				Text:           tc.text,
			}); err != nil {
				t.Fatalf("send: %v", err)
			}
		})
	}
}

// TestHTTPClient_SendTextMessage_LarkErrorCode pins the failure path:
// non-zero `code` becomes a wrapped error; a missing message_id even
// with code=0 is still treated as failure (matches the success-card
// path so callers don't have to special-case the response shapes).
func TestHTTPClient_SendTextMessage_LarkErrorCode(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	fake.stubSend(
		map[string]any{
			"code": 234567,
			"msg":  "Permission denied",
		},
		nil,
	)
	c := newTestClient(fake, time.Now)
	_, err := c.SendTextMessage(context.Background(), SendTextParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		Text:           "hi",
	})
	if err == nil {
		t.Fatal("expected error on non-zero Lark code")
	}
	if !strings.Contains(err.Error(), "234567") {
		t.Errorf("error should surface the Lark code; got %v", err)
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

func TestHTTPClient_AddMessageReaction_HappyPath(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_react", 7200)
	fake.stubReaction(map[string]any{"code": 0, "msg": "ok", "data": map[string]string{"reaction_id": "re_42"}}, func(r *http.Request, id string, body map[string]any) {
		if id != "om_user_msg_1" {
			t.Errorf("message id: got %q want om_user_msg_1", id)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok_react" {
			t.Errorf("Authorization=%q want Bearer tok_react", got)
		}
		reactionType, ok := body["reaction_type"].(map[string]any)
		if !ok {
			t.Fatalf("reaction_type missing or wrong shape: %v", body)
		}
		if got := reactionType["emoji_type"]; got != "Typing" {
			t.Errorf("emoji_type=%v want Typing", got)
		}
	})

	c := newTestClient(fake, time.Now)
	reactionID, err := c.AddMessageReaction(context.Background(), AddReactionParams{
		InstallationID: testCreds(),
		MessageID:      "om_user_msg_1",
		EmojiType:      "Typing",
	})
	if err != nil {
		t.Fatalf("AddMessageReaction: %v", err)
	}
	if reactionID != "re_42" {
		t.Errorf("reaction id: got %q want re_42", reactionID)
	}
	if got := fake.reactN.Load(); got != 1 {
		t.Fatalf("reaction endpoint calls=%d want 1", got)
	}
}

func TestHTTPClient_DeleteMessageReaction_HappyPath(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_del", 7200)
	fake.stubReactionDelete(map[string]any{"code": 0, "msg": "ok"}, func(r *http.Request, msgID string, reactionID string) {
		if msgID != "om_user_msg_1" {
			t.Errorf("message id: got %q want om_user_msg_1", msgID)
		}
		if reactionID != "re_42" {
			t.Errorf("reaction id: got %q want re_42", reactionID)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok_del" {
			t.Errorf("Authorization=%q want Bearer tok_del", got)
		}
	})

	c := newTestClient(fake, time.Now)
	if err := c.DeleteMessageReaction(context.Background(), DeleteReactionParams{
		InstallationID: testCreds(),
		MessageID:      "om_user_msg_1",
		ReactionID:     "re_42",
	}); err != nil {
		t.Fatalf("DeleteMessageReaction: %v", err)
	}
	if got := fake.delRN.Load(); got != 1 {
		t.Fatalf("reaction delete endpoint calls=%d want 1", got)
	}
}

func TestHTTPClient_AddMessageReaction_Validation(t *testing.T) {
	c := NewHTTPAPIClient(HTTPClientConfig{}).(*httpAPIClient)
	_, err := c.AddMessageReaction(context.Background(), AddReactionParams{MessageID: "m"})
	if err == nil || !strings.Contains(err.Error(), "missing emoji_type") {
		t.Errorf("want missing emoji_type error, got %v", err)
	}
	_, err = c.AddMessageReaction(context.Background(), AddReactionParams{EmojiType: "Typing"})
	if err == nil || !strings.Contains(err.Error(), "missing message_id") {
		t.Errorf("want missing message_id error, got %v", err)
	}
}

func TestHTTPClient_DeleteMessageReaction_Validation(t *testing.T) {
	c := NewHTTPAPIClient(HTTPClientConfig{}).(*httpAPIClient)
	err := c.DeleteMessageReaction(context.Background(), DeleteReactionParams{ReactionID: "re"})
	if err == nil || !strings.Contains(err.Error(), "missing message_id") {
		t.Errorf("want missing message_id error, got %v", err)
	}
	err = c.DeleteMessageReaction(context.Background(), DeleteReactionParams{MessageID: "m"})
	if err == nil || !strings.Contains(err.Error(), "missing reaction_id") {
		t.Errorf("want missing reaction_id error, got %v", err)
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

// TestHTTPClient_GetBotInfo_HappyPath drives the device-flow follow-up
// step: once RegistrationService has fresh client_id / client_secret
// from /oauth/v1/app/registration, it mints a tenant_access_token and
// asks /open-apis/bot/v3/info for the Bot's per-installation open_id,
// then resolves the bot's union_id via /open-apis/contact/v3/users/
// {open_id}?user_id_type=open_id. Both identifiers are persisted on
// the installation row; the union_id is what the WS decoder uses to
// route inbound @-mentions in multi-bot group chats (MUL-2671). The
// other fields on the bot/v3/info response (display name, avatar,
// IP whitelist) are deliberately dropped on the floor.
func TestHTTPClient_GetBotInfo_HappyPath(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_bi", 7200)
	fake.mux.HandleFunc("/open-apis/bot/v3/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("bot info: want GET, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok_bi" {
			t.Errorf("bot info: Authorization=%q want Bearer tok_bi", got)
		}
		writeJSON(w, map[string]any{
			"code": 0,
			"msg":  "ok",
			"bot": map[string]any{
				"open_id":   "ou_bot_42",
				"app_name":  "PersonalAgent",
				"avatar_url": "https://example/avatar.png",
			},
		})
	})
	fake.mux.HandleFunc("/open-apis/contact/v3/users/ou_bot_42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("contact users: want GET, got %s", r.Method)
		}
		if got := r.URL.Query().Get("user_id_type"); got != "open_id" {
			t.Errorf("contact users: user_id_type=%q want open_id", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok_bi" {
			t.Errorf("contact users: Authorization=%q want Bearer tok_bi", got)
		}
		writeJSON(w, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"user": map[string]any{
					"union_id": "on_bot_42_stable",
				},
			},
		})
	})

	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
	info, err := c.GetBotInfo(context.Background(), testCreds())
	if err != nil {
		t.Fatalf("GetBotInfo: %v", err)
	}
	if string(info.OpenID) != "ou_bot_42" {
		t.Errorf("OpenID: got %q want ou_bot_42", info.OpenID)
	}
	if info.UnionID != "on_bot_42_stable" {
		t.Errorf("UnionID: got %q want on_bot_42_stable", info.UnionID)
	}
}

// TestHTTPClient_GetBotInfo_UnionIDLookupSoftFails covers the case
// where /contact/v3/users returns a non-zero code (e.g. the app's
// contact scope was never approved). The install must still succeed
// with an empty UnionID so the operator can backfill later instead
// of the QR flow failing outright. The decoder transitional fallback
// keeps single-bot installs working in the gap.
func TestHTTPClient_GetBotInfo_UnionIDLookupSoftFails(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_bi_softfail", 7200)
	fake.mux.HandleFunc("/open-apis/bot/v3/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"code": 0,
			"msg":  "ok",
			"bot":  map[string]any{"open_id": "ou_bot_softfail"},
		})
	})
	fake.mux.HandleFunc("/open-apis/contact/v3/users/ou_bot_softfail", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 99991002, "msg": "no permission"})
	})
	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
	info, err := c.GetBotInfo(context.Background(), testCreds())
	if err != nil {
		t.Fatalf("GetBotInfo unexpectedly errored on soft-fail: %v", err)
	}
	if string(info.OpenID) != "ou_bot_softfail" {
		t.Errorf("OpenID: got %q want ou_bot_softfail", info.OpenID)
	}
	if info.UnionID != "" {
		t.Errorf("UnionID: got %q want empty (soft-fail leaves backfill to operator)", info.UnionID)
	}
}

// TestHTTPClient_GetBotInfo_LarkErrorCode surfaces a non-zero Lark
// error code (e.g. 230003 = bot disabled) as a wrapped error so
// RegistrationService can fail the install cleanly instead of
// recording a row with bot_open_id="".
func TestHTTPClient_GetBotInfo_LarkErrorCode(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok_bi_err", 7200)
	fake.mux.HandleFunc("/open-apis/bot/v3/info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 230003, "msg": "bot disabled"})
	})
	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL()})
	_, err := c.GetBotInfo(context.Background(), testCreds())
	if err == nil || !strings.Contains(err.Error(), "code=230003") {
		t.Errorf("want code=230003 surfaced, got %v", err)
	}
}

// TestHTTPClient_GetBotInfo_MissingCredentials short-circuits before
// any HTTP round-trip when the caller hands in zero-value credentials.
// A misuse here should NOT leak load to Lark's token endpoint.
func TestHTTPClient_GetBotInfo_MissingCredentials(t *testing.T) {
	fake := newLarkFake(t)
	c := newTestClient(fake, time.Now)
	if _, err := c.GetBotInfo(context.Background(), InstallationCredentials{}); err == nil ||
		!strings.Contains(err.Error(), "missing app credentials") {
		t.Errorf("want missing credentials error, got %v", err)
	}
	if got := fake.tokenN.Load(); got != 0 {
		t.Errorf("token endpoint must not be hit on bad input: got %d", got)
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
