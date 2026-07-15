package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	guuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// --- fakes for the three resolver seams (no DB) -----------------------------

type fakeConnLister struct {
	conns []connections.Connection
	err   error
}

func (f fakeConnLister) ListEnabled(ctx context.Context, workspaceID pgtype.UUID) ([]connections.Connection, error) {
	return f.conns, f.err
}

type fakeEndpointPolicy struct {
	// verdicts is keyed "<conn> <METHOD> <path>"; a missing key resolves to Deny.
	verdicts     map[string]toolpolicy.Setting
	toolVerdicts map[string]toolpolicy.Setting
	err          error
}

func (f fakeEndpointPolicy) ConnectionToolEffective(_ context.Context, _, _, _, _ pgtype.UUID, toolName string) (toolpolicy.Setting, string, error) {
	if f.err != nil {
		return toolpolicy.SettingDeny, "", f.err
	}
	if setting, ok := f.toolVerdicts[toolName]; ok {
		return setting, "c", nil
	}
	return toolpolicy.SettingDeny, "", nil
}

func (f fakeEndpointPolicy) ConnectionEndpointEffective(ctx context.Context, workspaceID, runtimeID, agentID, userID, onBehalfOfID pgtype.UUID, connName, method, path string) (toolpolicy.Setting, string, error) {
	if f.err != nil {
		return toolpolicy.SettingDeny, connName, f.err
	}
	if s, ok := f.verdicts[connName+" "+method+" "+path]; ok {
		return s, connName, nil
	}
	return toolpolicy.SettingDeny, connName, nil
}

type fakeFlag struct {
	on  bool
	err error
}

func (f fakeFlag) GetCerebroFeatureFlag(ctx context.Context, params cerebrodb.GetCerebroFeatureFlagParams) (bool, error) {
	return f.on, f.err
}

func resolverTestConns() []connections.Connection {
	return []connections.Connection{{
		Name: "c", Type: connections.TypeAPI, URL: "http://c.internal", Enabled: true,
		EndpointPermissions: []connections.EndpointPermission{
			{Path: "/allow", Methods: []string{"GET"}},
			{Path: "/ask", Methods: []string{"GET"}},
			{Path: "/deny", Methods: []string{"GET"}},
		},
	}}
}

func resolverIdent() APIConnectionIdentity {
	return APIConnectionIdentity{
		WorkspaceID: gateTestUUID(1),
		RuntimeID:   gateTestUUID(2),
		AgentID:     gateTestUUID(3),
		OwnerID:     gateTestUUID(4),
	}
}

// The core semantic: Allow and Ask endpoints are listed (with their verdict), a
// Deny endpoint is dropped — the one filter every surface shares.
func TestResolverIncludesAllowAndAskDropsDeny(t *testing.T) {
	r := NewAPIConnectionResolver(
		fakeConnLister{conns: resolverTestConns()},
		fakeEndpointPolicy{verdicts: map[string]toolpolicy.Setting{
			"c GET /allow": toolpolicy.SettingAllow,
			"c GET /ask":   toolpolicy.SettingAsk,
			"c GET /deny":  toolpolicy.SettingDeny,
		}},
		fakeFlag{on: true},
		slog.Default(),
	)

	got := r.ListForAgent(context.Background(), resolverIdent())

	verdictByName := map[string]toolpolicy.Setting{}
	for _, v := range got {
		verdictByName[v.Tool.Name()] = v.Verdict
	}
	if len(verdictByName) != 2 {
		t.Fatalf("expected 2 tools (allow+ask), got %d: %v", len(verdictByName), verdictByName)
	}
	if verdictByName["c__get_allow"] != toolpolicy.SettingAllow {
		t.Errorf("c__get_allow verdict = %q, want allow", verdictByName["c__get_allow"])
	}
	if verdictByName["c__get_ask"] != toolpolicy.SettingAsk {
		t.Errorf("c__get_ask verdict = %q, want ask", verdictByName["c__get_ask"])
	}
	if _, present := verdictByName["c__get_deny"]; present {
		t.Errorf("c__get_deny must be dropped, but it was listed")
	}
}

func TestAppConnectionCallAppliesConnectionAndHumanCeilings(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	connectionID := guuid.MustParse("11111111-1111-4111-8111-111111111111")
	conn := resolverTestConns()[0]
	conn.ID, conn.URL = connectionID.String(), upstream.URL
	resolver := NewAPIConnectionResolver(
		fakeConnLister{conns: []connections.Connection{conn}},
		fakeEndpointPolicy{verdicts: map[string]toolpolicy.Setting{"c GET /allow": toolpolicy.SettingAllow}},
		fakeFlag{on: true}, slog.Default(),
	)
	result, err := resolver.CallForApp(context.Background(), guuid.New(), guuid.New(), connectionID, "c__get_allow", map[string]any{})
	if err != nil || result != `{"ok":true}` {
		t.Fatalf("allowed app connection call = %q, %v", result, err)
	}
	if _, err := resolver.CallForApp(context.Background(), guuid.New(), guuid.New(), guuid.New(), "c__get_allow", map[string]any{}); !errors.Is(err, ErrAppConnectionDenied) {
		t.Fatalf("different connection was not denied: %v", err)
	}
}

func TestAppConnectionCallSupportsApprovedMCPToolAndHumanCeiling(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if request.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "session")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"structuredContent":{"ok":true}}}`))
	}))
	defer upstream.Close()

	connectionID := guuid.MustParse("22222222-2222-4222-8222-222222222222")
	conn := connections.Connection{
		ID: connectionID.String(), Name: "c", Type: connections.TypeMCPHTTP,
		URL: upstream.URL, Enabled: true, Tools: []connections.Tool{{Name: "format"}},
	}
	resolver := NewAPIConnectionResolver(
		fakeConnLister{conns: []connections.Connection{conn}},
		fakeEndpointPolicy{toolVerdicts: map[string]toolpolicy.Setting{"format": toolpolicy.SettingAllow}},
		fakeFlag{on: true}, slog.Default(),
	)
	result, err := resolver.CallForApp(context.Background(), guuid.New(), guuid.New(), connectionID, "format", map[string]any{"value": "x"})
	if err != nil || !strings.Contains(result, `"ok":true`) {
		t.Fatalf("allowed MCP app connection call = %q, %v", result, err)
	}

	resolver.policy = fakeEndpointPolicy{toolVerdicts: map[string]toolpolicy.Setting{"format": toolpolicy.SettingAsk}}
	if _, err := resolver.CallForApp(context.Background(), guuid.New(), guuid.New(), connectionID, "format", map[string]any{}); !errors.Is(err, ErrAppConnectionDenied) {
		t.Fatalf("MCP Ask must fail closed for an app call: %v", err)
	}
}

// With the feature flag off the resolver returns nothing, even when every
// endpoint would otherwise be Allow — the whole feature stays behind the flag.
func TestResolverEmptyWhenFlagOff(t *testing.T) {
	r := NewAPIConnectionResolver(
		fakeConnLister{conns: resolverTestConns()},
		fakeEndpointPolicy{verdicts: map[string]toolpolicy.Setting{
			"c GET /allow": toolpolicy.SettingAllow,
			"c GET /ask":   toolpolicy.SettingAllow,
			"c GET /deny":  toolpolicy.SettingAllow,
		}},
		fakeFlag{on: false},
		slog.Default(),
	)

	if got := r.ListForAgent(context.Background(), resolverIdent()); got != nil {
		t.Fatalf("flag off must return nil, got %d tools", len(got))
	}
}

// A per-endpoint policy error drops that endpoint (fail closed): this gate fronts
// the secrets box, so a tool whose verdict could not be verified is never listed.
func TestResolverFailClosedOnPolicyError(t *testing.T) {
	r := NewAPIConnectionResolver(
		fakeConnLister{conns: resolverTestConns()},
		fakeEndpointPolicy{err: errors.New("db down")},
		fakeFlag{on: true},
		slog.Default(),
	)

	if got := r.ListForAgent(context.Background(), resolverIdent()); got != nil {
		t.Fatalf("policy error must drop every endpoint (fail closed), got %d tools", len(got))
	}
}

// A discovery error resolves to an empty list, never a broken tool loop.
func TestResolverEmptyOnDiscoveryError(t *testing.T) {
	r := NewAPIConnectionResolver(
		fakeConnLister{err: errors.New("list failed")},
		fakeEndpointPolicy{},
		fakeFlag{on: true},
		slog.Default(),
	)
	if got := r.ListForAgent(context.Background(), resolverIdent()); got != nil {
		t.Fatalf("discovery error must return nil, got %d tools", len(got))
	}
}

// Unwired stores (feature not enabled) and an incomplete identity both resolve to
// nil without touching the flag or policy.
func TestResolverNilSafe(t *testing.T) {
	if got := (*APIConnectionResolver)(nil).ListForAgent(context.Background(), resolverIdent()); got != nil {
		t.Fatalf("nil resolver must return nil")
	}
	r := NewAPIConnectionResolver(nil, nil, fakeFlag{on: true}, slog.Default())
	if got := r.ListForAgent(context.Background(), resolverIdent()); got != nil {
		t.Fatalf("unwired stores must return nil")
	}
	full := NewAPIConnectionResolver(fakeConnLister{conns: resolverTestConns()}, fakeEndpointPolicy{}, fakeFlag{on: true}, slog.Default())
	if got := full.ListForAgent(context.Background(), APIConnectionIdentity{WorkspaceID: gateTestUUID(1)}); got != nil {
		t.Fatalf("identity without an agent id must return nil")
	}
}

func TestResolverPersonalizedDiscoveryFiltersGrantsAndKeepsAsk(t *testing.T) {
	conn := resolverTestConns()[0]
	conn.ID = "11111111-1111-4111-8111-111111111111"
	conn.AuthConfig.OnBehalfOf = &connections.OnBehalfOfConfig{Enabled: true}
	conn.EndpointPermissions = append(conn.EndpointPermissions,
		connections.EndpointPermission{Path: "/not-configured", Methods: []string{"GET"}},
	)
	r := NewAPIConnectionResolver(
		fakeConnLister{conns: []connections.Connection{conn}},
		fakeEndpointPolicy{verdicts: map[string]toolpolicy.Setting{
			"c GET /allow": toolpolicy.SettingAllow,
			"c GET /ask":   toolpolicy.SettingAsk,
			"c GET /deny":  toolpolicy.SettingAllow,
		}},
		fakeFlag{on: true}, slog.Default(),
	)
	var principal string
	r.discover = func(_ context.Context, _ *http.Client, _ connections.Connection, gotPrincipal string) ([]connections.EndpointPermission, error) {
		principal = gotPrincipal
		granted, denied := true, false
		return []connections.EndpointPermission{
			{Path: "/allow", Methods: []string{"GET"}, Summary: "Allowed source", Granted: &granted},
			{Path: "/ask", Methods: []string{"GET"}, Summary: "Ask source"},
			{Path: "/deny", Methods: []string{"GET"}, Summary: "Denied source", Granted: &denied},
			{Path: "/dynamic-only", Methods: []string{"GET"}, Summary: "Not configured", Granted: &granted},
		}, nil
	}

	got := r.ListForAgent(context.Background(), resolverIdent())
	if principal != "agent:03030303-0303-0303-0303-030303030303" {
		t.Fatalf("principal = %q", principal)
	}
	if len(got) != 2 {
		t.Fatalf("expected granted + identity-blind configured endpoints, got %d: %+v", len(got), got)
	}
	byName := map[string]APIConnectionToolVerdict{}
	for _, tool := range got {
		byName[tool.Tool.Name()] = tool
	}
	if !strings.HasPrefix(byName["c__get_allow"].Tool.Description(), "Allowed source.") {
		t.Fatalf("dynamic summary missing: %q", byName["c__get_allow"].Tool.Description())
	}
	if byName["c__get_ask"].Verdict != toolpolicy.SettingAsk {
		t.Fatalf("Ask verdict was not preserved: %+v", byName["c__get_ask"])
	}
	if _, ok := byName["c__get_deny"]; ok {
		t.Fatal("explicitly ungranted endpoint must be dropped")
	}
	if _, ok := byName["c__get_dynamic_only"]; ok {
		t.Fatal("endpoint absent from the admin catalogue must not be introduced")
	}
}

func TestResolverPersonalizedDiscoveryErrorFailsClosed(t *testing.T) {
	conn := resolverTestConns()[0]
	conn.AuthConfig.OnBehalfOf = &connections.OnBehalfOfConfig{Enabled: true}
	r := NewAPIConnectionResolver(
		fakeConnLister{conns: []connections.Connection{conn}},
		fakeEndpointPolicy{verdicts: map[string]toolpolicy.Setting{"c GET /allow": toolpolicy.SettingAllow}},
		fakeFlag{on: true}, slog.Default(),
	)
	r.discover = func(context.Context, *http.Client, connections.Connection, string) ([]connections.EndpointPermission, error) {
		return nil, errors.New("spec unavailable")
	}
	if got := r.ListForAgent(context.Background(), resolverIdent()); got != nil {
		t.Fatalf("personalized discovery error must fail closed, got %d tools", len(got))
	}
}

func TestResolverPersonalizedDiscoveryUsesShortCache(t *testing.T) {
	conn := resolverTestConns()[0]
	conn.ID = "11111111-1111-4111-8111-111111111111"
	conn.AuthConfig.OnBehalfOf = &connections.OnBehalfOfConfig{Enabled: true}
	r := NewAPIConnectionResolver(
		fakeConnLister{conns: []connections.Connection{conn}},
		fakeEndpointPolicy{verdicts: map[string]toolpolicy.Setting{"c GET /allow": toolpolicy.SettingAllow}},
		fakeFlag{on: true}, slog.Default(),
	)
	calls := 0
	r.discover = func(context.Context, *http.Client, connections.Connection, string) ([]connections.EndpointPermission, error) {
		calls++
		granted := true
		return []connections.EndpointPermission{{Path: "/allow", Methods: []string{"GET"}, Granted: &granted}}, nil
	}

	_ = r.ListForAgent(context.Background(), resolverIdent())
	_ = r.ListForAgent(context.Background(), resolverIdent())
	if calls != 1 {
		t.Fatalf("expected one personalized spec fetch inside the short cache window, got %d", calls)
	}
}
