package runtime

import (
	"context"
	"errors"
	"log/slog"
	"testing"

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
	verdicts map[string]toolpolicy.Setting
	err      error
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
