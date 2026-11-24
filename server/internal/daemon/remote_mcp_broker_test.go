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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/remotemcp"
	"github.com/multica-ai/multica/server/pkg/remotemcp/remotemcptest"
)

func TestRemoteMCPServerNameUsesSafeDistinctContributionIdentities(t *testing.T) {
	connections := []remotemcp.Connection{
		{ContributionKey: "example__toolbox", ContributionID: "plugin:12345678-1234-4234-8234-1234567890ab:toolbox"},
		{ContributionKey: "example__toolbox", ContributionID: "plugin:12345678-1234-4234-8234-1234567890ac:toolbox"},
		{ContributionKey: "example__toolbox", ContributionID: "plugin:12345678-1234-4234-8234-1234567890ab:other_hook"},
		{ContributionKey: "Mixed Case/☃", ContributionID: "00000000-0000-0000-0000-000000000001"},
	}
	safeName := regexp.MustCompile(`^[a-z0-9_-]+$`)
	seen := map[string]bool{}
	for _, connection := range connections {
		name := remoteMCPServerName(connection)
		if !safeName.MatchString(name) {
			t.Errorf("Codex-incompatible server name %q for %q", name, connection.ContributionID)
		}
		if seen[name] {
			t.Errorf("distinct contribution identities alias to %q", name)
		}
		seen[name] = true
		if again := remoteMCPServerName(connection); again != name {
			t.Fatalf("server identity changed: %q != %q", again, name)
		}
	}
}

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
		connection: remotemcp.Connection{
			InstallationID: "installation", ContributionKey: "fixture",
			ApprovedTools: []remotemcp.Tool{{
				Name: "fixture.read", Description: "pinned", InputSchema: canonical,
				SchemaDigest: remotemcp.DigestBytes(canonical), Risk: "read",
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
	proxy.connection.ApprovedTools = append(proxy.connection.ApprovedTools, remotemcp.Tool{
		Name: "fixture.write", InputSchema: writeSchema, SchemaDigest: remotemcp.DigestBytes(writeSchema), Risk: "write",
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
		connection: remotemcp.Connection{ApprovedTools: []remotemcp.Tool{{Name: "allowed"}}},
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
	const contributionID = "plugin:12345678-1234-4234-8234-1234567890ab:toolbox"
	called := false
	credentialChecked := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
		connection: remotemcp.Connection{
			ContributionID: contributionID, CredentialHeader: "Authorization",
			ApprovedTools: []remotemcp.Tool{{Name: "allowed"}},
		},
		resolveCredential: func(_ context.Context, id string) (http.Header, error) {
			credentialChecked = true
			if id != contributionID {
				t.Errorf("credential lookup identity = %q, want original %q", id, contributionID)
			}
			return nil, errors.New("revoked")
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if !credentialChecked || called || !strings.Contains(response.Body.String(), "revoked or unavailable") {
		t.Fatalf("credentialChecked=%v called=%v response=%s", credentialChecked, called, response.Body.String())
	}
}

func TestRemoteMCPProviderMatrixAndConfigMerge(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "hermes", "qoder", "mcode"} {
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

func TestRemoteMCPProxyFiltersMultiLineAndNotificationPrefixedSSEToolsList(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	canonical, err := canonicalRemoteMCPJSON(schema)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w,
			"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":1}}\n\n"+
				"event: message\n"+
				"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[\n"+
				"data: {\"name\":\"fixture.read\",\"description\":\"Read\",\"inputSchema\":{\"type\":\"object\",\"properties\":{}}},\n"+
				"data: {\"name\":\"fixture.write\",\"description\":\"Write\",\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}\n"+
				"data: ]}}\n\n")
		w.(http.Flusher).Flush()
		// Keep the stream open, as Streamable HTTP servers may.
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer close(release)
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		taskID: "task", endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		connection: remotemcp.Connection{
			InstallationID: "installation", ContributionKey: "fixture",
			ApprovedTools: []remotemcp.Tool{{
				Name: "fixture.read", Description: "pinned", InputSchema: canonical,
				SchemaDigest: remotemcp.DigestBytes(canonical), Risk: "read",
			}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/capability", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, `"error"`) || strings.Contains(body, "fixture.write") || !strings.Contains(body, "fixture.read") {
		t.Fatalf("filtered SSE tools/list = %s", body)
	}
}
