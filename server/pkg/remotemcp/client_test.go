package remotemcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
