package connections

// Tests for the connection test/discover probe: the Streamable HTTP MCP
// handshake (initialized notification + protocol-version echo), the legacy
// HTTP+SSE MCP transport fallback, and explicit OpenAPI spec input (direct
// URL / uploaded document) for API connections.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Streamable HTTP MCP -----------------------------------------------------

// TestMCP_StreamableHTTP_FullHandshake verifies the probe sends initialize,
// notifications/initialized, then tools/list — echoing the server's negotiated
// protocol version and session ID on the follow-up requests.
func TestMCP_StreamableHTTP_FullHandshake(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var toolsListProtoHeader, toolsListSession string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rpc struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&rpc)
		mu.Lock()
		methods = append(methods, rpc.Method)
		mu.Unlock()

		switch rpc.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-123")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"t","version":"1"}}}`)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			mu.Lock()
			toolsListProtoHeader = r.Header.Get("MCP-Protocol-Version")
			toolsListSession = r.Header.Get("Mcp-Session-Id")
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo","description":"echoes"}]}}`)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{
		URL: srv.URL, Type: TypeMCPHTTP,
	})

	if !result.Reachable {
		t.Fatalf("expected reachable, got %+v", result)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "echo" {
		t.Fatalf("expected [echo], got %+v", result.Tools)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"initialize", "notifications/initialized", "tools/list"}
	if len(methods) != 3 || methods[0] != want[0] || methods[1] != want[1] || methods[2] != want[2] {
		t.Fatalf("expected call order %v, got %v", want, methods)
	}
	if toolsListProtoHeader != "2025-03-26" {
		t.Fatalf("expected negotiated MCP-Protocol-Version header 2025-03-26, got %q", toolsListProtoHeader)
	}
	if toolsListSession != "sess-123" {
		t.Fatalf("expected session header sess-123, got %q", toolsListSession)
	}
}

// TestMCP_StreamableHTTP_SSEResponse verifies tools/list responses wrapped in
// SSE framing (Content-Type: text/event-stream) are unwrapped.
func TestMCP_StreamableHTTP_SSEResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rpc struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&rpc)
		switch rpc.Method {
		case "initialize":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n\n")
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"lookup\"}]}}\n\n")
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{URL: srv.URL, Type: TypeMCPHTTP})
	if !result.Reachable || len(result.Tools) != 1 || result.Tools[0].Name != "lookup" {
		t.Fatalf("expected [lookup] via SSE framing, got %+v", result)
	}
}

// --- Legacy HTTP+SSE MCP transport --------------------------------------------

// newLegacySSEServer builds an httptest server speaking the deprecated
// HTTP+SSE transport: GET / opens the stream and announces /message; POSTs to
// /message return 202 and their JSON-RPC responses are pushed onto the stream.
func newLegacySSEServer(t *testing.T) *httptest.Server {
	t.Helper()
	responses := make(chan string, 8)
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			// The legacy transport rejects POSTs on the base URL — this 405 is
			// exactly what triggers the probe's fallback.
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "event: endpoint\ndata: /message?sessionId=abc\n\n")
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-responses:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
				flusher.Flush()
			}
		}
	})

	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		var rpc struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&rpc)
		switch rpc.Method {
		case "initialize":
			responses <- `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"legacy","version":"1"}}}`
		case "tools/list":
			responses <- `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"legacy_tool","description":"old transport"}]}}`
		}
		w.WriteHeader(http.StatusAccepted)
	})

	return httptest.NewServer(mux)
}

// TestMCP_LegacySSEFallback verifies a server that only speaks the deprecated
// HTTP+SSE transport (405 on POST) is still discovered via the fallback.
func TestMCP_LegacySSEFallback(t *testing.T) {
	srv := newLegacySSEServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := doTestConnection(ctx, testConnectionRequest{URL: srv.URL, Type: TypeMCPHTTP})

	if !result.Reachable {
		t.Fatalf("expected reachable via legacy SSE, got %+v", result)
	}
	if result.Error != "" {
		t.Fatalf("expected no error, got %q", result.Error)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "legacy_tool" {
		t.Fatalf("expected [legacy_tool], got %+v", result.Tools)
	}
}

// TestMCP_FallbackKeepsStreamableError verifies that when neither transport
// works, the streamable-HTTP attempt's diagnostics are preserved.
func TestMCP_FallbackKeepsStreamableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{URL: srv.URL, Type: TypeMCPHTTP})
	if !result.Reachable || result.StatusCode != http.StatusNotFound {
		t.Fatalf("expected reachable with 404 diagnostics, got %+v", result)
	}
	if !strings.Contains(result.Error, "initialize") {
		t.Fatalf("expected initialize error preserved, got %q", result.Error)
	}
}

// --- Explicit OpenAPI spec input (api type) ------------------------------------

// TestAPI_SpecURL verifies an explicit spec URL is fetched and parsed even when
// it lives on a different path than any well-known candidate.
func TestAPI_SpecURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/internal/docs/spec-v1.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, openAPIv3JSON)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{
		URL: srv.URL, Type: TypeAPI, SpecURL: srv.URL + "/internal/docs/spec-v1.json",
	})
	if !result.Reachable {
		t.Fatalf("expected reachable, got %+v", result)
	}
	if len(result.Endpoints) != 2 || result.Endpoints[0].Path != "/orders" {
		t.Fatalf("expected 2 endpoints from spec URL, got %+v", result.Endpoints)
	}
	if result.Error != "" {
		t.Fatalf("expected no error, got %q", result.Error)
	}
}

// TestAPI_SpecURL_FetchFailureIsReported verifies an explicit spec URL that
// cannot be fetched produces an explicit error (unlike best-effort probing).
func TestAPI_SpecURL_FetchFailureIsReported(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{
		URL: srv.URL, Type: TypeAPI, SpecURL: srv.URL + "/missing.json",
	})
	if !result.Reachable {
		t.Fatalf("base URL should still be reachable, got %+v", result)
	}
	if len(result.Endpoints) != 0 {
		t.Fatalf("expected no endpoints, got %+v", result.Endpoints)
	}
	if !strings.Contains(result.Error, "spec URL") {
		t.Fatalf("expected explicit spec URL error, got %q", result.Error)
	}
}

// TestAPI_SpecContentUpload verifies an uploaded document (JSON or YAML) is
// parsed without any network fetch — even when the API itself is unreachable.
func TestAPI_SpecContentUpload(t *testing.T) {
	result := doTestConnection(context.Background(), testConnectionRequest{
		// Closed port: the API is down, but the uploaded spec must still parse.
		URL: "http://127.0.0.1:1", Type: TypeAPI, SpecContent: swaggerV2YAML,
	})
	if result.Reachable {
		t.Fatalf("expected unreachable base URL, got %+v", result)
	}
	if len(result.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints from uploaded YAML, got %+v", result.Endpoints)
	}
	if result.Endpoints[0].Path != "/health" || result.Endpoints[1].Path != "/users" {
		t.Fatalf("unexpected endpoint paths: %+v", result.Endpoints)
	}
}

// TestAPI_SpecContentUpload_BadDocument verifies a non-spec upload gets an
// explicit error instead of silently returning nothing.
func TestAPI_SpecContentUpload_BadDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{
		URL: srv.URL, Type: TypeAPI, SpecContent: `{"hello":"world"}`,
	})
	if len(result.Endpoints) != 0 {
		t.Fatalf("expected no endpoints, got %+v", result.Endpoints)
	}
	if !strings.Contains(result.Error, "uploaded document") {
		t.Fatalf("expected explicit upload error, got %q", result.Error)
	}
}

// TestAPI_SpecContentTakesPrecedenceOverSpecURL verifies uploaded content wins
// over a simultaneously provided spec URL.
func TestAPI_SpecContentTakesPrecedenceOverSpecURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{
		URL: srv.URL, Type: TypeAPI,
		SpecURL:     srv.URL + "/would-404.json",
		SpecContent: openAPIv3JSON,
	})
	if len(result.Endpoints) != 2 {
		t.Fatalf("expected endpoints from uploaded content, got %+v", result.Endpoints)
	}
	if result.Error != "" {
		t.Fatalf("expected no error (content wins), got %q", result.Error)
	}
}

// TestAPI_ProbeAuthRejectionIsReported verifies that when well-known spec
// probing hits a 401, the result says the credential was rejected instead of
// pretending no spec exists (FIR-2640).
func TestAPI_ProbeAuthRejectionIsReported(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{
		URL: srv.URL, Type: TypeAPI, AuthConfig: AuthConfig{BearerToken: "bad-key"},
	})
	if !result.Reachable {
		t.Fatalf("base URL should be reachable, got %+v", result)
	}
	if len(result.Endpoints) != 0 {
		t.Fatalf("expected no endpoints, got %+v", result.Endpoints)
	}
	if !strings.Contains(result.Error, "rejected the connection's credential (HTTP 401)") {
		t.Fatalf("expected credential-rejection error, got %q", result.Error)
	}
}

// TestAPI_ProbeAuthRejectionFallsBackToPublicDocs verifies that a rejected
// credential falls back to the API's anonymous documentation with an explicit
// warning, instead of leaving the endpoint list empty (FIR-2640).
func TestAPI_ProbeAuthRejectionFallsBackToPublicDocs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openAPIv3JSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{
		URL: srv.URL, Type: TypeAPI, AuthConfig: AuthConfig{BearerToken: "bad-key"},
	})
	if len(result.Endpoints) != 2 {
		t.Fatalf("expected fallback to the 2 public endpoints, got %+v", result.Endpoints)
	}
	if !strings.Contains(result.Error, "credential was rejected (HTTP 401") ||
		!strings.Contains(result.Error, "public endpoints") {
		t.Fatalf("expected rejected-credential warning with fallback note, got %q", result.Error)
	}
}

// TestAPI_SpecURLAuthRejectionFallsBackToPublicDoc mirrors the fallback for an
// explicitly provided spec URL.
func TestAPI_SpecURLAuthRejectionFallsBackToPublicDoc(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/spec.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openAPIv3JSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	result := doTestConnection(context.Background(), testConnectionRequest{
		URL: srv.URL, Type: TypeAPI, SpecURL: srv.URL + "/spec.json",
		AuthConfig: AuthConfig{BearerToken: "bad-key"},
	})
	if len(result.Endpoints) != 2 {
		t.Fatalf("expected fallback to the 2 public endpoints, got %+v", result.Endpoints)
	}
	if !strings.Contains(result.Error, "rejected the connection's credential (HTTP 401") ||
		!strings.Contains(result.Error, "public document") {
		t.Fatalf("expected rejected-credential warning with fallback note, got %q", result.Error)
	}
}
