package lark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The send path is exercised here through the real HTTP client against a
// fake Lark, not through a fake APIClient: the addressing
// (receive_id_type=open_id), the card envelope, and the Summary wiring
// only exist on the wire. A fake would let all three be wrong and still
// pass.
func TestSendMarkdownCardToOpenIDWireFormat(t *testing.T) {
	var (
		mu       sync.Mutex
		gotPath  string
		gotQuery string
		gotBody  map[string]any
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "tenant_access_token": "t-1", "expire": 7200,
			})
		case "/open-apis/im/v1/messages":
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			gotBody = body
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "ok",
				"data": map[string]any{"message_id": "om_1"},
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: srv.URL})
	id, err := c.SendMarkdownCardToOpenID(context.Background(), SendMarkdownCardToOpenIDParams{
		InstallationID: InstallationCredentials{AppID: "cli_1", AppSecret: "s", Region: RegionFeishu},
		OpenID:         "ou_target",
		Markdown:       "**[任务指派] 实现登录页**",
		Summary:        "实现登录页",
	})
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if id != "om_1" {
		t.Errorf("message id = %q, want om_1", id)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/open-apis/im/v1/messages" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "receive_id_type=open_id") {
		t.Errorf("query = %q, want receive_id_type=open_id", gotQuery)
	}
	if gotBody["receive_id"] != "ou_target" {
		t.Errorf("receive_id = %v, want ou_target", gotBody["receive_id"])
	}
	if gotBody["msg_type"] != "interactive" {
		t.Errorf("msg_type = %v, want interactive", gotBody["msg_type"])
	}
	content, _ := gotBody["content"].(string)
	var card map[string]any
	if err := json.Unmarshal([]byte(content), &card); err != nil {
		t.Fatalf("content is not JSON: %v", err)
	}
	if card["schema"] != "2.0" {
		t.Errorf("card schema = %v, want 2.0", card["schema"])
	}
	cfg, _ := card["config"].(map[string]any)
	summary, _ := cfg["summary"].(map[string]any)
	if summary["content"] != "实现登录页" {
		t.Errorf("summary = %v, want the title", summary["content"])
	}
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]any)
	if len(elements) != 1 {
		t.Fatalf("elements = %d, want 1", len(elements))
	}
	el, _ := elements[0].(map[string]any)
	if el["tag"] != "markdown" {
		t.Errorf("element tag = %v, want markdown", el["tag"])
	}
	if el["content"] != "**[任务指派] 实现登录页**" {
		t.Errorf("element content = %v", el["content"])
	}
}

// A missing open_id must fail before any network call — the binding row
// is the only source of that id, so an empty one means the caller passed
// a malformed binding, not that Lark should be asked.
func TestSendMarkdownCardToOpenIDRejectsEmptyAddress(t *testing.T) {
	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: "http://127.0.0.1:1"})
	_, err := c.SendMarkdownCardToOpenID(context.Background(), SendMarkdownCardToOpenIDParams{
		InstallationID: InstallationCredentials{AppID: "cli_1", AppSecret: "s"},
		Markdown:       "x",
	})
	if err == nil {
		t.Fatal("expected an error for a missing open_id")
	}
	if !strings.Contains(err.Error(), "open_id") {
		t.Errorf("error = %v, want it to name open_id", err)
	}
}
