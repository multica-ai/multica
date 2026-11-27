package remotemcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type staticResolver map[string][]netip.Addr

func (r staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r[host], nil
}

func TestValidatePublicHTTPSEndpoint(t *testing.T) {
	resolver := staticResolver{
		"mcp.example.com": {netip.MustParseAddr("8.8.8.8")},
		"private.example": {netip.MustParseAddr("169.254.169.254")},
	}
	endpoint, err := ValidatePublicHTTPSEndpoint(context.Background(), "https://mcp.example.com/v1/mcp", []string{"mcp.example.com"}, resolver)
	if err != nil || endpoint.Hostname() != "mcp.example.com" {
		t.Fatalf("ValidatePublicHTTPSEndpoint = %v, %v", endpoint, err)
	}
	for _, raw := range []string{
		"http://mcp.example.com/mcp",
		"https://localhost/mcp",
		"https://token@mcp.example.com/mcp",
		"https://private.example/mcp",
	} {
		if _, err := ValidatePublicHTTPSEndpoint(context.Background(), raw, nil, resolver); err == nil {
			t.Fatalf("endpoint %q was accepted", raw)
		}
	}
	if _, err := ValidatePublicHTTPSEndpoint(context.Background(), "https://mcp.example.com/mcp", []string{"other.example.com"}, resolver); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("host policy error = %v", err)
	}
}

func TestHostAllowedWildcardDoesNotMatchApex(t *testing.T) {
	if !hostAllowed("tools.example.com", []string{"*.example.com"}) {
		t.Fatal("wildcard did not match subdomain")
	}
	if hostAllowed("example.com", []string{"*.example.com"}) {
		t.Fatal("wildcard matched apex")
	}
}

func TestSecureHTTPClientRecoversBlackholedHTTP2Connection(t *testing.T) {
	started := make(chan struct{}, 1)
	var interrupted atomic.Int32
	server, connections := testutil.NewBlackholeHTTP2Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("expected HTTP/2, got %s", r.Proto)
		}
		if r.URL.Path == "/blocked" {
			interrupted.Add(1)
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	t.Setenv(DevOriginsEnv, server.URL)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := NewSecureHTTPClient(endpoint)
	testutil.TrustHTTP2Server(t, client, server)
	client.Timeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if _, _, err := call(ctx, client, endpoint, nil, "", nil); err != nil {
		t.Fatal(err)
	}
	conn := <-connections
	defer conn.Close()
	blocked := *endpoint
	blocked.Path = "/blocked"
	done := make(chan error, 1)
	go func() {
		_, _, err := call(ctx, client, &blocked, nil, "", nil)
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("MCP call did not reach server")
	}
	conn.Drop.Store(true)
	if err := <-done; err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected connection loss before request timeout, got %v", err)
	}
	if _, _, err := call(ctx, client, endpoint, nil, "", nil); err != nil {
		t.Fatalf("MCP client reused the dead connection: %v", err)
	}
	select {
	case <-connections:
	default:
		t.Fatal("MCP recovery did not open a new connection")
	}
	if count := interrupted.Load(); count != 1 {
		t.Fatalf("interrupted MCP call was replayed %d times", count)
	}
}

func TestContainsProtocolVersion(t *testing.T) {
	if !containsString([]string{"2025-03-26", "2024-11-05"}, "2024-11-05") {
		t.Fatal("declared protocol version was not found")
	}
	if containsString([]string{"2024-11-05"}, "2025-03-26") {
		t.Fatal("undeclared protocol version was accepted")
	}
}

func TestToolSetDigestIgnoresInputSchemaObjectKeyOrder(t *testing.T) {
	firstSchema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"count":{"type":"number"}}}`)
	secondSchema := json.RawMessage(`{"properties":{"count":{"type":"number"},"query":{"type":"string"}},"type":"object"}`)
	canonical, err := canonicalJSON(firstSchema)
	if err != nil {
		t.Fatal(err)
	}
	schemaDigest := DigestBytes(canonical)

	first, err := ToolSetDigest([]Tool{{
		Name: "search", InputSchema: firstSchema, SchemaDigest: schemaDigest, Risk: "read",
	}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ToolSetDigest([]Tool{{
		Name: "search", InputSchema: secondSchema, SchemaDigest: schemaDigest, Risk: "read",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("digest depends on input schema object key order: %s != %s", first, second)
	}
}

func TestDiscoverHandlesMultiLineAndNotificationPrefixedSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.Header().Set("Mcp-Session-Id", "sess-1")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, ": keepalive\n\n"+
				"id: 0\ndata:\n\n"+
				"event: ping\ndata: ok\n\n"+
				"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"level\":\"info\"}}\n\n"+
				"data: {\"jsonrpc\":\"2.0\",\"id\":\"srv-1\",\"method\":\"ping\",\"params\":{}}\n\n"+
				"event: message\n"+
				"data: {\"jsonrpc\":\"2.0\",\"id\":1,\n"+
				"data: \"result\":{\"protocolVersion\":\"2025-03-26\"}}\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w,
				"event: message\r\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":1}}\r\n\r\n"+
					"data: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"tools\":[]}}\r\n\r\n"+
					"data: {\"jsonrpc\":\"2.0\",\"id\":2,\r\n"+
					"data: \"result\":{\"tools\":[{\"name\":\"search\",\"description\":\"Search docs\",\"inputSchema\":{\"type\":\"object\"}}]}}")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv(DevOriginsEnv, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tools, digest, err := Discover(ctx, server.URL, []string{"127.0.0.1"}, nil, nil)
	if err != nil {
		t.Fatalf("Discover failed on SSE stream: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "search" || digest == "" {
		t.Fatalf("Discover = %+v, digest=%q", tools, digest)
	}
}

func TestCallSurfacesNullIDJSONRPCErrorInSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n"+
				"data: {\"jsonrpc\":\"2.0\",\"id\":null,\"error\":{\"code\":-32600,\"message\":\"Invalid Request\"}}\n\n")
	}))
	defer server.Close()
	t.Setenv(DevOriginsEnv, server.URL)
	endpoint, _ := url.Parse(server.URL)

	_, _, err := call(context.Background(), NewSecureHTTPClient(endpoint), endpoint, nil, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
	})
	if err == nil || !strings.Contains(err.Error(), "Invalid Request") {
		t.Fatalf("call error = %v, want JSON-RPC Invalid Request", err)
	}
}

func TestReadResponseRejectsOversizedSSEStream(t *testing.T) {
	padding := strings.Repeat("a", MaxResponseBytes+128)
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:   io.NopCloser(strings.NewReader(": " + padding + "\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")),
	}
	_, err := ReadResponse(resp, nil)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("ReadResponse error = %v, want ErrResponseTooLarge", err)
	}
}

func TestReadSSEResponseAcceptsLoneCRLineEndingsOnOpenStream(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	go func() {
		// The stream stays open after the event, so the trailing CR must end
		// the event without waiting to see whether a LF follows.
		_, _ = io.WriteString(writer, "event: message\rdata: {\"jsonrpc\":\"2.0\",\r\ndata: \"id\":7,\"result\":{}}\r\r")
	}()
	done := make(chan struct{})
	var raw []byte
	var err error
	go func() {
		raw, err = readSSEResponse(reader, json.RawMessage("7"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readSSEResponse waited for more data after a complete event")
	}
	if err != nil || string(raw) != "{\"jsonrpc\":\"2.0\",\n\"id\":7,\"result\":{}}" {
		t.Fatalf("readSSEResponse = %q, %v", raw, err)
	}
}

func TestReadSSEResponseReportsMissingMatchingResponse(t *testing.T) {
	_, err := readSSEResponse(strings.NewReader("data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{}}\n\n"), json.RawMessage("1"))
	if err == nil || !strings.Contains(err.Error(), "no matching JSON-RPC response") {
		t.Fatalf("readSSEResponse error = %v, want no matching response", err)
	}
}
