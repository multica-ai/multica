package lark

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Lark reports an invalid / expired tenant_access_token as HTTP 400 with
// the code in the BODY, not as a 2xx envelope:
//
//	http 400: {"code":99991663,"msg":"Invalid access token for authorization..."}
//
// Every test in this file drives that exact shape. It is the shape the
// client used to be blind to (#7611): the transport error short-circuited
// before the body was ever parsed, so the cached token was never dropped
// and every later outbound replayed it until the process restarted.

const invalidTokenMsg = "Invalid access token for authorization. Please make a request with token attached."

// writeLarkStatusError writes the non-2xx + business-code reply Lark
// actually sends.
func writeLarkStatusError(w http.ResponseWriter, status, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"code":%d,"msg":%q}`, code, msg)
}

// stubRotatingToken hands out a different token per mint so a test can
// tell the replayed request apart from the original one. The last entry
// is repeated once the list runs out.
func stubRotatingToken(f *larkFakeServer, tokens ...string) {
	f.mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		n := int(f.tokenN.Add(1))
		tok := tokens[len(tokens)-1]
		if n <= len(tokens) {
			tok = tokens[n-1]
		}
		writeJSON(w, map[string]any{
			"code":                0,
			"msg":                 "ok",
			"tenant_access_token": tok,
			"expire":              7200,
		})
	})
}

func (c *httpAPIClient) cachedTokenValue(appID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[appID]
	if !ok {
		return "", false
	}
	return t.value, true
}

// TestHTTPClient_SendCard_HTTP400TokenExpired_RefreshesAndRetries is the
// direct regression test for #7611: the send that hits the dead token
// must recover by itself, not merely unblock the NEXT send.
func TestHTTPClient_SendCard_HTTP400TokenExpired_RefreshesAndRetries(t *testing.T) {
	fake := newLarkFake(t)
	stubRotatingToken(fake, "tok_dead", "tok_fresh")

	var attempts atomic.Int32
	fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		switch n := attempts.Add(1); n {
		case 1:
			if got := r.Header.Get("Authorization"); got != "Bearer tok_dead" {
				t.Errorf("first attempt auth = %q, want the cached token", got)
			}
			writeLarkStatusError(w, http.StatusBadRequest, codeTenantTokenInvalid, invalidTokenMsg)
		case 2:
			if got := r.Header.Get("Authorization"); got != "Bearer tok_fresh" {
				t.Errorf("retry auth = %q, want the refreshed token", got)
			}
			writeJSON(w, map[string]any{"code": 0, "data": map[string]string{"message_id": "om_ok"}})
		default:
			t.Errorf("send called %d times, want at most 2", n)
		}
	})

	c := newTestClient(fake, time.Now)
	msgID, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	})
	if err != nil {
		t.Fatalf("send must recover from a rejected token: %v", err)
	}
	if msgID != "om_ok" {
		t.Errorf("message id = %q, want om_ok", msgID)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("send attempts = %d, want 2 (original + one replay)", got)
	}
	if got := fake.tokenN.Load(); got != 2 {
		t.Errorf("token mints = %d, want 2 (the dead one was dropped)", got)
	}
	if cached, ok := c.cachedTokenValue(testCreds().AppID); !ok || cached != "tok_fresh" {
		t.Errorf("cache holds %q (present=%v), want tok_fresh", cached, ok)
	}
}

// TestHTTPClient_SendMarkdownCard_HTTP400TokenExpired_StopsAfterOneRetry
// pins the other half of the contract: refreshing must not turn into a
// refresh storm when the fresh token is rejected too.
func TestHTTPClient_SendMarkdownCard_HTTP400TokenExpired_StopsAfterOneRetry(t *testing.T) {
	fake := newLarkFake(t)
	stubRotatingToken(fake, "tok_a", "tok_b", "tok_c")

	var attempts atomic.Int32
	fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		writeLarkStatusError(w, http.StatusBadRequest, codeTenantTokenInvalid, invalidTokenMsg)
	})

	c := newTestClient(fake, time.Now)
	_, err := c.SendMarkdownCard(context.Background(), SendMarkdownCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		Markdown:       "**hi**",
	})
	if err == nil {
		t.Fatal("a permanently rejected token must surface as an error")
	}
	// The wording operators see in their logs (#7611).
	if !strings.Contains(err.Error(), "lark http client: send markdown card: http 400") {
		t.Errorf("error should keep the transport wording: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("send attempts = %d, want exactly 2", got)
	}
	if got := fake.tokenN.Load(); got != 2 {
		t.Errorf("token mints = %d, want exactly 2", got)
	}
}

// TestHTTPClient_AddMessageReaction_HTTP400TokenExpired_RefreshesAndRetries
// covers the typing-indicator path, which is where the reporter saw the
// failure first.
func TestHTTPClient_AddMessageReaction_HTTP400TokenExpired_RefreshesAndRetries(t *testing.T) {
	fake := newLarkFake(t)
	stubRotatingToken(fake, "tok_dead", "tok_fresh")

	var attempts atomic.Int32
	fake.mux.HandleFunc("/open-apis/im/v1/messages/om_1/reactions", func(w http.ResponseWriter, r *http.Request) {
		if n := attempts.Add(1); n == 1 {
			writeLarkStatusError(w, http.StatusBadRequest, codeTenantTokenInvalid, invalidTokenMsg)
			return
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]string{"reaction_id": "re_1"}})
	})

	c := newTestClient(fake, time.Now)
	reactionID, err := c.AddMessageReaction(context.Background(), AddReactionParams{
		InstallationID: testCreds(),
		MessageID:      "om_1",
		EmojiType:      "Typing",
	})
	if err != nil {
		t.Fatalf("add reaction must recover from a rejected token: %v", err)
	}
	if reactionID != "re_1" {
		t.Errorf("reaction id = %q, want re_1", reactionID)
	}
	if got := fake.tokenN.Load(); got != 2 {
		t.Errorf("token mints = %d, want 2", got)
	}
}

// TestHTTPClient_HTTP400NonTokenError_KeepsCachedToken guards the other
// direction: a 400 that is NOT about the token must not drop a healthy
// cache entry or replay the request.
func TestHTTPClient_HTTP400NonTokenError_KeepsCachedToken(t *testing.T) {
	fake := newLarkFake(t)
	stubRotatingToken(fake, "tok_good")

	var attempts atomic.Int32
	fake.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		writeLarkStatusError(w, http.StatusBadRequest, 230001, "bot is not in the chat")
	})

	c := newTestClient(fake, time.Now)
	_, err := c.SendInteractiveCard(context.Background(), SendCardParams{
		InstallationID: testCreds(),
		ChatID:         ChatID("oc"),
		CardJSON:       `{}`,
	})
	if err == nil {
		t.Fatal("a non-token 400 must still be an error")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("send attempts = %d, want 1 (no replay for a non-token failure)", got)
	}
	if got := fake.tokenN.Load(); got != 1 {
		t.Errorf("token mints = %d, want 1 (the cached token is still good)", got)
	}
	if cached, ok := c.cachedTokenValue(testCreds().AppID); !ok || cached != "tok_good" {
		t.Errorf("cache holds %q (present=%v), want the token to survive", cached, ok)
	}
}

// TestHTTPClient_DownloadResource_HTTP400TokenExpired_RefreshesAndRetries
// covers the resource path, which builds its request by hand instead of
// going through doJSON and so needs its own refresh-and-retry.
func TestHTTPClient_DownloadResource_HTTP400TokenExpired_RefreshesAndRetries(t *testing.T) {
	fake := newLarkFake(t)
	stubRotatingToken(fake, "tok_dead", "tok_fresh")

	var attempts atomic.Int32
	fake.mux.HandleFunc("/open-apis/im/v1/messages/om_1/resources/img_1", func(w http.ResponseWriter, r *http.Request) {
		if n := attempts.Add(1); n == 1 {
			writeLarkStatusError(w, http.StatusBadRequest, codeTenantTokenInvalid, invalidTokenMsg)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok_fresh" {
			t.Errorf("retry auth = %q, want the refreshed token", got)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{7, 8, 9})
	})

	c := newTestClient(fake, time.Now)
	got, err := c.DownloadMessageResource(context.Background(), testCreds(), DownloadResourceParams{
		MessageID: "om_1",
		FileKey:   "img_1",
		Type:      "image",
	})
	if err != nil {
		t.Fatalf("download must recover from a rejected token: %v", err)
	}
	if string(got.Data) != string([]byte{7, 8, 9}) {
		t.Errorf("downloaded bytes = %v", got.Data)
	}
	if n := attempts.Load(); n != 2 {
		t.Errorf("download attempts = %d, want 2", n)
	}
}

// TestHTTPClient_InvalidateTokenCache_ForcesRemint covers credential
// rotation: re-registering a Bot issues a new app_secret under the same
// app_id, Lark revokes the old token, and nothing about the cache key
// changes — so the holder has to be told.
func TestHTTPClient_InvalidateTokenCache_ForcesRemint(t *testing.T) {
	fake := newLarkFake(t)
	stubRotatingToken(fake, "tok_first", "tok_second")
	fake.stubSend(map[string]any{"code": 0, "data": map[string]string{"message_id": "om_ok"}}, nil)

	c := newTestClient(fake, time.Now)
	send := func() {
		t.Helper()
		if _, err := c.SendInteractiveCard(context.Background(), SendCardParams{
			InstallationID: testCreds(),
			ChatID:         ChatID("oc"),
			CardJSON:       `{}`,
		}); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	send()
	send()
	if got := fake.tokenN.Load(); got != 1 {
		t.Fatalf("token mints = %d, want 1 while the cached token is alive", got)
	}

	var invalidator TokenCacheInvalidator = c
	invalidator.InvalidateTokenCache(testCreds().AppID)

	send()
	if got := fake.tokenN.Load(); got != 2 {
		t.Errorf("token mints = %d, want 2 after the cache was invalidated", got)
	}
	if got := fake.lastAuth(); got != "Bearer tok_second" {
		t.Errorf("auth after rotation = %q, want the re-minted token", got)
	}
}

// TestIsThreadReplyUnsupported_ReadsNon2xxBodyCode pins the second place
// the non-2xx short-circuit went unnoticed: the thread -> chat fallback
// classifies by business code too, and a code Lark delivered with a 400
// is exactly as definitive as one it delivered with a 200.
func TestIsThreadReplyUnsupported_ReadsNon2xxBodyCode(t *testing.T) {
	if !isThreadReplyUnsupported(&larkAPIStatusError{StatusCode: 400, Code: 230071, Msg: "no reply in thread"}) {
		t.Error("230071 delivered as HTTP 400 should classify as thread-reply-unsupported")
	}
	if isThreadReplyUnsupported(&larkAPIStatusError{StatusCode: 400, Code: codeTenantTokenInvalid, Msg: invalidTokenMsg}) {
		t.Error("a token failure must never trigger the chat-level fallback")
	}
	// A gateway error carries no Lark code: delivery is ambiguous and the
	// fallback could duplicate the reply.
	if isThreadReplyUnsupported(&larkAPIStatusError{StatusCode: 502, Raw: "<html>bad gateway</html>"}) {
		t.Error("a body with no Lark code must not classify")
	}
}
