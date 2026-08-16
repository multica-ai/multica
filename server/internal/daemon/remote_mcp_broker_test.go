package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
	"github.com/multica-ai/multica/server/pkg/remotemcp/remotemcptest"
)

func TestRemoteMCPProxyFiltersToolsAndInjectsCredential(t *testing.T) {
	fixture := remotemcptest.NewServer()
	defer fixture.Close()
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	canonical, err := canonicalRemoteMCPJSON(schema)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := url.Parse(fixture.URL)
	proxy := &remoteMCPProxy{
		taskID: "task", endpoint: endpoint, client: fixture.Client(), path: "/capability",
		credentialHeaders: http.Header{"Authorization": []string{"Bearer " + remotemcptest.Credential}},
		semaphore:         make(chan struct{}, remoteMCPMaxConcurrency), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		connection: pluginruntime.RemoteMCPConnection{
			InstallationID: "installation", ContributionKey: "fixture",
			ApprovedTools: []pluginruntime.RemoteMCPTool{{
				Name: "fixture.read", Description: "pinned", InputSchema: canonical,
				SchemaDigest: plugincontract.DigestBytes(canonical), Risk: "read",
			}},
		},
	}

	request := httptest.NewRequest(http.MethodPost, "/capability", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "fixture.write") || !strings.Contains(response.Body.String(), "fixture.read") {
		t.Fatalf("filtered tools/list = %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "pinned") {
		t.Fatalf("tools/list did not preserve reviewed description: %s", response.Body.String())
	}

	writeSchema, err := canonicalRemoteMCPJSON(json.RawMessage(`{
		"type":"object",
		"required":["value"],
		"properties":{"value":{"type":"string"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	proxy.connection.ApprovedTools = append(proxy.connection.ApprovedTools, pluginruntime.RemoteMCPTool{
		Name: "fixture.write", InputSchema: writeSchema, SchemaDigest: plugincontract.DigestBytes(writeSchema), Risk: "write",
	})
	writeRequest := httptest.NewRequest(http.MethodPost, "/capability", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"fixture.write","arguments":{"value":"broker-write"}}}`))
	writeResponse := httptest.NewRecorder()
	proxy.ServeHTTP(writeResponse, writeRequest)
	if writes := fixture.Writes(); len(writes) != 1 || writes[0] != "broker-write" {
		t.Fatalf("fixture writes = %#v, response = %s", writes, writeResponse.Body.String())
	}
}

func TestRemoteMCPProxyRejectsUnapprovedToolWithoutCallingUpstream(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore:  make(chan struct{}, remoteMCPMaxConcurrency),
		connection: pluginruntime.RemoteMCPConnection{ApprovedTools: []pluginruntime.RemoteMCPTool{{Name: "allowed"}}},
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"denied","arguments":{"secret":"not logged"}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if called || !strings.Contains(response.Body.String(), "not approved") {
		t.Fatalf("called=%v response=%s", called, response.Body.String())
	}
}

func TestRemoteMCPProxyPreservesAcceptedNotificationStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("upstream method = %s", request.Method)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
	}

	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
	))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("notification response body = %q, want empty", response.Body.String())
	}
}

func TestRemoteMCPProxyRechecksCredentialBeforeUpstreamCall(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
		connection: pluginruntime.RemoteMCPConnection{
			ContributionID: "contribution", CredentialHeader: "Authorization",
			ApprovedTools: []pluginruntime.RemoteMCPTool{{Name: "allowed"}},
		},
		resolveCredential: func(context.Context, string) (http.Header, error) {
			return nil, errors.New("revoked")
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if called || !strings.Contains(response.Body.String(), "revoked or unavailable") {
		t.Fatalf("called=%v response=%s", called, response.Body.String())
	}
}

func TestRemoteMCPProviderMatrixAndConfigMerge(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "hermes", "qoder"} {
		if !providerSupportsRemoteMCPBroker(provider) {
			t.Fatalf("provider %s must support Remote MCP", provider)
		}
	}
	if providerSupportsRemoteMCPBroker("deveco") {
		t.Fatal("unsupported provider was accepted")
	}
	merged, err := mergeTaskRemoteMCPConfig(
		json.RawMessage(`{"mcpServers":{"agent":{"command":"agent"}}}`),
		json.RawMessage(`{"mcpServers":{"plugin":{"type":"http","url":"http://127.0.0.1/mcp"}}}`),
	)
	if err != nil || !strings.Contains(string(merged), `"agent"`) || !strings.Contains(string(merged), `"plugin"`) {
		t.Fatalf("merge = %s, %v", merged, err)
	}
}

func TestDecodeRemoteMCPSSEData(t *testing.T) {
	raw, err := decodeRemoteMCPSSEData("text/event-stream; charset=utf-8", []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1}\n\n"))
	if err != nil || string(raw) != `{"jsonrpc":"2.0","id":1}` {
		t.Fatalf("decodeRemoteMCPSSEData = %q, %v", raw, err)
	}
}
