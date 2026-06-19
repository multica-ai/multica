package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomerServiceMCPToolCallsRemoteServer(t *testing.T) {
	var gotAuth string
	var gotCFJWT string
	var gotName string
	var gotArgs map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCFJWT = r.Header.Get("Cf-Access-Jwt-Assertion")

		var body struct {
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotName = body.Params.Name
		gotArgs = body.Params.Arguments

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`event: message
data: {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Hej fra kundeservice"}]}}

`))
	}))
	defer srv.Close()

	tool := &CustomerServiceMCPTool{
		name:        "draft_reply",
		description: "test",
		inputSchema: objectSchema([]string{"message"}, map[string]any{"message": stringProp("msg")}),
		cfg: customerServiceMCPConfig{
			URL:         srv.URL,
			BearerToken: "secret",
		},
		client: srv.Client(),
	}

	out, err := tool.Call(context.Background(), map[string]any{"message": "Min pakke mangler"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out != "Hej fra kundeservice" {
		t.Fatalf("output = %q, want remote text", out)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want bearer", gotAuth)
	}
	if gotCFJWT != "internal" {
		t.Fatalf("Cf-Access-Jwt-Assertion = %q, want internal fallback", gotCFJWT)
	}
	if gotName != "draft_reply" {
		t.Fatalf("tool name = %q, want draft_reply", gotName)
	}
	if gotArgs["message"] != "Min pakke mangler" {
		t.Fatalf("message arg = %v", gotArgs["message"])
	}
}

func TestCustomerServiceMCPToolsAreRegisteredAndInMetadata(t *testing.T) {
	reg := NewDefaultRegistry(nil, nil, ToolContext{})

	required := []string{
		"draft_reply",
		"lookup_order",
		"check_delivery_estimate",
		"draft_reply_from_conversation",
		"execute_db_tool",
		"list_db_tools",
		"mcp_data_status",
	}
	for _, name := range required {
		if !reg.hasTool(name) {
			t.Fatalf("%s was not registered", name)
		}
	}

	found := map[string]bool{}
	for _, meta := range AllBuiltinToolMeta() {
		for _, name := range required {
			if meta.Name != name {
				continue
			}
			found[meta.Name] = meta.Status == ToolStatusNewlyImplemented
		}
	}
	for _, name := range required {
		if !found[name] {
			t.Fatalf("%s missing from metadata or wrong status: %#v", name, found)
		}
	}
}

func TestCustomerServiceMCPToolReturnsStructuredContentWhenNoText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[],"structuredContent":{"orderNumber":"123456","status":"shipped"}}}`))
	}))
	defer srv.Close()

	tool := &CustomerServiceMCPTool{
		name:        "lookup_order",
		description: "test",
		inputSchema: objectSchema([]string{"orderNumber"}, map[string]any{"orderNumber": stringProp("order")}),
		cfg: customerServiceMCPConfig{
			URL:         srv.URL,
			BearerToken: "secret",
		},
		client: srv.Client(),
	}

	out, err := tool.Call(context.Background(), map[string]any{"orderNumber": "123456"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output was not JSON: %v\n%s", err, out)
	}
	if got["orderNumber"] != "123456" || got["status"] != "shipped" {
		t.Fatalf("structured output = %#v", got)
	}
}

func TestCustomerServiceMCPToolRequiresBearerToken(t *testing.T) {
	tool := &CustomerServiceMCPTool{name: "draft_reply", cfg: customerServiceMCPConfig{URL: "http://example.test/mcp"}}
	if _, err := tool.Call(context.Background(), map[string]any{"message": "hej"}); err == nil {
		t.Fatal("expected missing bearer token error")
	}
}
