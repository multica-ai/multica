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
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/remotemcp"
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
		connection: remotemcp.Connection{
			InstallationID: "installation", ContributionKey: "fixture", Transport: "http",
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
		connection: remotemcp.Connection{Transport: "http", ApprovedTools: []remotemcp.Tool{{Name: "allowed"}}},
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"denied","arguments":{"secret":"not logged"}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if called || !strings.Contains(response.Body.String(), "not approved") {
		t.Fatalf("called=%v response=%s", called, response.Body.String())
	}
}

func TestRemoteMCPProxyRejectsUnsupportedTransportWithoutCallingUpstream(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	control := managedMCPTestControl()
	proxy := &remoteMCPProxy{
		taskID: "task", provider: "claude", endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency), invocationGate: newManagedMCPInvocationGate(control),
		connection: remotemcp.Connection{
			ContributionKey: "fixture", Transport: "stdio",
			ApprovedTools: []remotemcp.Tool{{
				Name: "allowed", SchemaDigest: control.rule.SchemaDigest,
			}},
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if called || !strings.Contains(response.Body.String(), "transport is not supported") {
		t.Fatalf("called=%v response=%s", called, response.Body.String())
	}
	if got, want := strings.Join(control.stages, ","), "capability,allowlist,approval,consume,started,terminal"; got != want {
		t.Fatalf("gate order = %s, want %s", got, want)
	}
	if control.identity.TransportKind != managedMCPTransportKind || control.identity.ServerKey != "fixture" || control.identity.ToolName != "allowed" {
		t.Fatalf("exact policy identity = %+v", control.identity)
	}
}

type stubRemoteMCPInvocationGate struct {
	beginErr error
}

func (gate *stubRemoteMCPInvocationGate) CheckCapability(string, string) error {
	return gate.beginErr
}

type recordingManagedMCPControlPlane struct {
	stages      []string
	supported   bool
	rule        managedMCPPolicyRule
	ruleErr     error
	approval    managedMCPApproval
	approvalErr error
	consumeErr  error
	startedErr  error
	terminalErr error
	identity    managedMCPToolIdentity
}

func (control *recordingManagedMCPControlPlane) SupportsCapability(string, string) bool {
	control.stages = append(control.stages, "capability")
	return control.supported
}

func (control *recordingManagedMCPControlPlane) LookupRule(_ context.Context, identity managedMCPToolIdentity) (managedMCPPolicyRule, error) {
	control.stages = append(control.stages, "allowlist")
	control.identity = identity
	return control.rule, control.ruleErr
}

func (control *recordingManagedMCPControlPlane) LookupApproval(context.Context, remoteMCPInvocation, managedMCPPolicyRule) (managedMCPApproval, error) {
	control.stages = append(control.stages, "approval")
	return control.approval, control.approvalErr
}

func (control *recordingManagedMCPControlPlane) ConsumeApproval(context.Context, remoteMCPInvocation, managedMCPPolicyRule, managedMCPApproval) error {
	control.stages = append(control.stages, "consume")
	return control.consumeErr
}

func (control *recordingManagedMCPControlPlane) CommitStarted(_ context.Context, _ remoteMCPInvocation, _ managedMCPPolicyRule, grant remoteMCPInvocationGrant) (remoteMCPInvocationGrant, error) {
	control.stages = append(control.stages, "started")
	if grant.InvocationID == "" {
		grant.InvocationID = "018f0000-0000-7000-8000-000000000001"
	}
	return grant, control.startedErr
}

func (control *recordingManagedMCPControlPlane) CommitTerminalAndTaskMessage(context.Context, remoteMCPInvocationGrant, remoteMCPInvocationResult) error {
	control.stages = append(control.stages, "terminal")
	return control.terminalErr
}

func managedMCPTestControl() *recordingManagedMCPControlPlane {
	return &recordingManagedMCPControlPlane{
		supported: true,
		rule: managedMCPPolicyRule{
			SchemaDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Effect:       "require_approval",
		},
		approval: managedMCPApproval{
			Status: "approved",
			Grant: remoteMCPInvocationGrant{
				InvocationID:      "018f0000-0000-7000-8000-000000000001",
				ApprovalRequestID: "018f0000-0000-7000-8000-000000000002",
			},
		},
	}
}

func TestManagedMCPInvocationGateEnforcesRequiredOrder(t *testing.T) {
	control := managedMCPTestControl()
	gate := newManagedMCPInvocationGate(control)
	if err := gate.CheckCapability("claude", managedMCPTransportKind); err != nil {
		t.Fatalf("CheckCapability: %v", err)
	}
	grant, err := gate.Begin(context.Background(), remoteMCPInvocation{
		TaskID: "task", ProviderFamily: "claude", TransportKind: "managed_mcp",
		ServerKey: "fixture", ToolName: "allowed", SchemaDigest: control.rule.SchemaDigest,
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := gate.Finish(context.Background(), grant, remoteMCPInvocationResult{OutcomeCode: "succeeded"}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if got, want := strings.Join(control.stages, ","), "capability,allowlist,approval,consume,started,terminal"; got != want {
		t.Fatalf("gate order = %s, want %s", got, want)
	}
}

func TestRemoteMCPProxyGateFailuresNeverReachUpstream(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*recordingManagedMCPControlPlane)
		message string
	}{
		{name: "deny", mutate: func(control *recordingManagedMCPControlPlane) { control.ruleErr = errors.New("no exact rule") }, message: "denied by policy"},
		{name: "pending", mutate: func(control *recordingManagedMCPControlPlane) { control.approval.Status = "pending" }, message: "approval is pending"},
		{name: "approval denied", mutate: func(control *recordingManagedMCPControlPlane) { control.approval.Status = "denied" }, message: "approval was denied"},
		{name: "expired", mutate: func(control *recordingManagedMCPControlPlane) { control.approval.Status = "expired" }, message: "approval expired"},
		{name: "cancelled", mutate: func(control *recordingManagedMCPControlPlane) { control.approval.Status = "cancelled" }, message: "approval was cancelled"},
		{name: "schema drift", mutate: func(control *recordingManagedMCPControlPlane) {
			control.rule.SchemaDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}, message: "schema changed"},
		{name: "audit failure", mutate: func(control *recordingManagedMCPControlPlane) { control.startedErr = errors.New("audit unavailable") }, message: "audit commit failed"},
		{name: "duplicate consume", mutate: func(control *recordingManagedMCPControlPlane) { control.consumeErr = errRemoteMCPApprovalConsumed }, message: "already consumed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer upstream.Close()
			endpoint, _ := url.Parse(upstream.URL)
			control := managedMCPTestControl()
			test.mutate(control)
			proxy := &remoteMCPProxy{
				taskID: "task", provider: "claude", endpoint: endpoint, client: upstream.Client(), path: "/capability",
				semaphore: make(chan struct{}, remoteMCPMaxConcurrency), invocationGate: newManagedMCPInvocationGate(control),
				connection: remotemcp.Connection{
					ContributionKey: "fixture", Transport: "http",
					ApprovedTools: []remotemcp.Tool{{Name: "allowed", SchemaDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
				},
			}
			request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{"secret":"never-forward"}}}`))
			response := httptest.NewRecorder()
			proxy.ServeHTTP(response, request)
			if got := calls.Load(); got != 0 {
				t.Fatalf("upstream calls = %d, want 0", got)
			}
			if !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("response = %s, want %q", response.Body.String(), test.message)
			}
		})
	}
}

type concurrentManagedMCPControlPlane struct {
	entered  atomic.Int32
	consumed atomic.Bool
	terminal atomic.Int32
	ready    chan struct{}
	release  chan struct{}
}

func (*concurrentManagedMCPControlPlane) SupportsCapability(string, string) bool { return true }
func (*concurrentManagedMCPControlPlane) LookupRule(context.Context, managedMCPToolIdentity) (managedMCPPolicyRule, error) {
	return managedMCPPolicyRule{SchemaDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Effect: "require_approval"}, nil
}
func (*concurrentManagedMCPControlPlane) LookupApproval(context.Context, remoteMCPInvocation, managedMCPPolicyRule) (managedMCPApproval, error) {
	return managedMCPApproval{Status: "approved", Grant: remoteMCPInvocationGrant{InvocationID: "018f0000-0000-7000-8000-000000000001", ApprovalRequestID: "018f0000-0000-7000-8000-000000000002"}}, nil
}
func (control *concurrentManagedMCPControlPlane) ConsumeApproval(context.Context, remoteMCPInvocation, managedMCPPolicyRule, managedMCPApproval) error {
	if control.entered.Add(1) == 2 {
		close(control.ready)
	}
	<-control.release
	if control.consumed.CompareAndSwap(false, true) {
		return nil
	}
	return errRemoteMCPApprovalConsumed
}
func (*concurrentManagedMCPControlPlane) CommitStarted(_ context.Context, _ remoteMCPInvocation, _ managedMCPPolicyRule, grant remoteMCPInvocationGrant) (remoteMCPInvocationGrant, error) {
	return grant, nil
}
func (control *concurrentManagedMCPControlPlane) CommitTerminalAndTaskMessage(context.Context, remoteMCPInvocationGrant, remoteMCPInvocationResult) error {
	control.terminal.Add(1)
	return nil
}

func TestRemoteMCPProxyConcurrentApprovalConsumptionCallsUpstreamExactlyOnce(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	control := &concurrentManagedMCPControlPlane{ready: make(chan struct{}), release: make(chan struct{})}
	proxy := &remoteMCPProxy{
		taskID: "task", provider: "claude", endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency), invocationGate: newManagedMCPInvocationGate(control),
		connection: remotemcp.Connection{
			ContributionKey: "fixture", Transport: "http",
			ApprovedTools: []remotemcp.Tool{{Name: "allowed", SchemaDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		},
	}

	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
			response := httptest.NewRecorder()
			proxy.ServeHTTP(response, request)
			responses <- response
		}()
	}
	<-control.ready
	close(control.release)
	first, second := <-responses, <-responses
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
	if got := control.terminal.Load(); got != 1 {
		t.Fatalf("terminal commits = %d, want 1", got)
	}
	combined := first.Body.String() + second.Body.String()
	if !strings.Contains(combined, "already consumed") || !strings.Contains(combined, `"result"`) {
		t.Fatalf("responses = %s", combined)
	}
	if first.Header().Get("X-Multica-Tool-Invocation-ID") != "018f0000-0000-7000-8000-000000000001" && second.Header().Get("X-Multica-Tool-Invocation-ID") != "018f0000-0000-7000-8000-000000000001" {
		t.Fatal("successful managed tool result did not propagate invocation ID")
	}
}

func (gate *stubRemoteMCPInvocationGate) Begin(context.Context, remoteMCPInvocation) (remoteMCPInvocationGrant, error) {
	return remoteMCPInvocationGrant{}, gate.beginErr
}

func (*stubRemoteMCPInvocationGate) Finish(context.Context, remoteMCPInvocationGrant, remoteMCPInvocationResult) error {
	return nil
}

func TestRemoteMCPProxyRejectsMissingPreTransportCapabilityWithoutCallingUpstream(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		taskID: "task", provider: "claude", endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
		connection: remotemcp.Connection{
			ContributionKey: "fixture", Transport: "http",
			ApprovedTools: []remotemcp.Tool{{Name: "allowed", SchemaDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		},
		invocationGate: &stubRemoteMCPInvocationGate{beginErr: errRemoteMCPCapabilityUnsupported},
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if called || !strings.Contains(response.Body.String(), "capability is unavailable") {
		t.Fatalf("called=%v response=%s", called, response.Body.String())
	}
}

func TestRemoteMCPProxyChecksCapabilityBeforeExactAllowlist(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	control := managedMCPTestControl()
	control.supported = false
	proxy := &remoteMCPProxy{
		taskID: "task", provider: "claude", endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency), invocationGate: newManagedMCPInvocationGate(control),
		connection: remotemcp.Connection{
			ContributionKey: "fixture", Transport: "http",
			ApprovedTools: []remotemcp.Tool{{Name: "different", SchemaDigest: control.rule.SchemaDigest}},
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unapproved","arguments":{}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if called || !strings.Contains(response.Body.String(), "capability is unavailable") {
		t.Fatalf("called=%v response=%s", called, response.Body.String())
	}
	if got, want := strings.Join(control.stages, ","), "capability"; got != want {
		t.Fatalf("gate order = %s, want %s", got, want)
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
	control := managedMCPTestControl()
	proxy := &remoteMCPProxy{
		taskID: "task", provider: "claude", endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency), invocationGate: newManagedMCPInvocationGate(control),
		connection: remotemcp.Connection{
			ContributionID: "contribution", CredentialHeader: "Authorization", Transport: "http",
			ContributionKey: "fixture", ApprovedTools: []remotemcp.Tool{{Name: "allowed", SchemaDigest: control.rule.SchemaDigest}},
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
	if got, want := strings.Join(control.stages, ","), "capability,allowlist,approval,consume,started,terminal"; got != want {
		t.Fatalf("gate order = %s, want %s", got, want)
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

func TestManagedMCPPreTransportCapabilityAdvertisementIsTransportAndProviderScoped(t *testing.T) {
	claude := providerCapabilitiesForRegistration("claude")
	want := managedMCPPreTransportCapability("claude")
	if !strings.Contains(","+claude+",", ","+want+",") {
		t.Fatalf("claude capabilities = %q, missing %q", claude, want)
	}
	if strings.Contains(providerCapabilitiesForRegistration("antigravity"), "managed_mcp:") {
		t.Fatal("antigravity advertised a managed MCP pre-transport capability")
	}
	if managedMCPPreTransportCapability("claude") == managedMCPPreTransportCapability("codex") {
		t.Fatal("managed MCP capability is not provider-family scoped")
	}
}

func TestDecodeRemoteMCPSSEData(t *testing.T) {
	raw, err := decodeRemoteMCPSSEData("text/event-stream; charset=utf-8", []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1}\n\n"))
	if err != nil || string(raw) != `{"jsonrpc":"2.0","id":1}` {
		t.Fatalf("decodeRemoteMCPSSEData = %q, %v", raw, err)
	}
}

func TestRemoteMCPInvocationIdempotencyKeyIsStableAndOpaque(t *testing.T) {
	rawID := json.RawMessage(`"secret-canary-jsonrpc-id"`)
	first := remoteMCPInvocationIdempotencyKey("task", "fixture", "allowed", rawID)
	second := remoteMCPInvocationIdempotencyKey("task", "fixture", "allowed", rawID)
	different := remoteMCPInvocationIdempotencyKey("task", "fixture", "allowed", json.RawMessage(`2`))
	if first != second || first == different {
		t.Fatalf("idempotency keys first=%q second=%q different=%q", first, second, different)
	}
	if strings.Contains(first, "secret-canary") || !strings.HasPrefix(first, "mcp:sha256:") {
		t.Fatalf("idempotency key = %q, want opaque digest", first)
	}
}
