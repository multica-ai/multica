package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/localhost"
)

func TestConversationPreheatSettingPersists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := newConversationPreheat(dir, "token")
	if !p.enabled() {
		t.Fatal("preheat must default to on")
	}
	do := func(method, body string) (int, map[string]bool) {
		req := httptest.NewRequest(method, "http://127.0.0.1/local/conversation-preheat", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		p.handle(rec, req)
		var out map[string]bool
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, out := do(http.MethodPut, `{}`); code != http.StatusBadRequest {
		t.Fatalf("missing value = %d, %v", code, out)
	}
	if code, out := do(http.MethodPut, `{"old_conversations":false}`); code != http.StatusOK || out["old_conversations"] {
		t.Fatalf("put = %d, %v", code, out)
	}
	if p.enabled() {
		t.Fatal("still enabled")
	}
	if newConversationPreheat(dir, "token").enabled() {
		t.Fatal("the choice did not survive a restart")
	}
	if code, out := do(http.MethodGet, ""); code != http.StatusOK || out["old_conversations"] {
		t.Fatalf("get = %d, %v", code, out)
	}
}

func TestConversationPreheatSettingIsOnlyForThisDevicesDesktop(t *testing.T) {
	t.Parallel()

	p := newConversationPreheat(t.TempDir(), "token")
	for name, req := range map[string]*http.Request{
		"no credentials": httptest.NewRequest(http.MethodPut, "http://127.0.0.1/local/conversation-preheat", strings.NewReader(`{"old_conversations":false}`)),
		"not loopback":   withBearer(httptest.NewRequest(http.MethodPut, "http://example.com/local/conversation-preheat", strings.NewReader(`{"old_conversations":false}`)), "token"),
		"wrong token":    withBearer(httptest.NewRequest(http.MethodPut, "http://127.0.0.1/local/conversation-preheat", strings.NewReader(`{"old_conversations":false}`)), "other"),
	} {
		rec := httptest.NewRecorder()
		p.handle(rec, req)
		if rec.Code != http.StatusForbidden || !p.enabled() {
			t.Errorf("%s = %d, enabled %v", name, rec.Code, p.enabled())
		}
	}
	req := withBearer(httptest.NewRequest(http.MethodGet, "http://127.0.0.1/local/conversation-preheat", nil), localhost.RendererToken("token"))
	rec := httptest.NewRecorder()
	p.handle(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("the renderer = %d", rec.Code)
	}
}

func withBearer(r *http.Request, token string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}
