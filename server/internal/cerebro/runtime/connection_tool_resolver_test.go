package runtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/handler"
)

// fakeMCPVerdicts satisfies mcpToolVerdictResolver without a DB. verdicts is
// keyed "<conn>\x00<tool>"; a missing key resolves to Deny (fail closed) in the
// resolver, mirroring production.
type fakeMCPVerdicts struct {
	verdicts []toolpolicy.ConnectionToolVerdict
	err      error
}

func (f fakeMCPVerdicts) ConnectionToolVerdicts(ctx context.Context, in toolpolicy.TableQuery) ([]toolpolicy.ConnectionToolVerdict, error) {
	return f.verdicts, f.err
}

// fakeEntry is a deterministic relay-entry builder that avoids depending on the
// real connections.MCPServerEntry / relay wiring.
func fakeEntry(c connections.Connection) map[string]any {
	return map[string]any{"type": "http", "url": "relay://" + c.Name}
}

func mixedVerdictConns() []connections.Connection {
	return []connections.Connection{
		// api connection — proves Category A reuse (APIConnectionResolver).
		{
			Name: "apiconn", Type: connections.TypeAPI, URL: "http://api.internal", Enabled: true,
			EndpointPermissions: []connections.EndpointPermission{
				{Path: "/allow", Methods: []string{"GET"}},
			},
		},
		// mcp_http, Allow-only → gets a raw relay entry.
		{
			Name: "alpha", Type: connections.TypeMCPHTTP, URL: "http://alpha.internal", Enabled: true,
			Tools: []connections.Tool{{Name: "a1"}, {Name: "a2"}},
		},
		// mcp_http, Allow + Ask → mixed → NO raw relay entry.
		{
			Name: "beta", Type: connections.TypeMCPHTTP, URL: "http://beta.internal", Enabled: true,
			Tools: []connections.Tool{{Name: "b1"}, {Name: "b2"}},
		},
		// mcp_http, Allow + Deny → mixed → NO raw relay entry; Deny tool listed.
		{
			Name: "gamma", Type: connections.TypeMCPHTTP, URL: "http://gamma.internal", Enabled: true,
			Tools: []connections.Tool{{Name: "g1"}, {Name: "g2"}},
		},
	}
}

func newMixedResolver() *ConnectionToolResolver {
	conns := fakeConnLister{conns: mixedVerdictConns()}
	// Category A: reuse the real APIConnectionResolver over the same connection
	// lister (it filters to api-type internally) with a fake endpoint policy.
	api := NewAPIConnectionResolver(
		conns,
		fakeEndpointPolicy{verdicts: map[string]toolpolicy.Setting{
			"apiconn GET /allow": toolpolicy.SettingAllow,
		}},
		fakeFlag{on: true},
		slog.Default(),
	)
	mcp := fakeMCPVerdicts{verdicts: []toolpolicy.ConnectionToolVerdict{
		{Connection: "alpha", Tool: "a1", Setting: toolpolicy.SettingAllow},
		{Connection: "alpha", Tool: "a2", Setting: toolpolicy.SettingAllow},
		{Connection: "beta", Tool: "b1", Setting: toolpolicy.SettingAllow},
		{Connection: "beta", Tool: "b2", Setting: toolpolicy.SettingAsk},
		{Connection: "gamma", Tool: "g1", Setting: toolpolicy.SettingAllow},
		{Connection: "gamma", Tool: "g2", Setting: toolpolicy.SettingDeny},
	}}
	return NewConnectionToolResolver(api, conns, mcp, fakeFlag{on: true}, fakeEntry, slog.Default())
}

func mixedIdent() ConnectionIdentity {
	return ConnectionIdentity{
		WorkspaceID: gateTestUUID(1),
		RuntimeID:   gateTestUUID(2),
		AgentID:     gateTestUUID(3),
		OwnerID:     gateTestUUID(4),
	}
}

// The core Fase-2 semantic (Lone #1 + Tine's mixed-verdict sharpening):
// Allow-only mcp_http connections become raw relay entries; a connection with
// any Ask/Deny tool does NOT — its Allow siblings must not drag the non-Allow
// tools in. Deny tools are dropped from Tools and surface only as --disallowedTools.
func TestConnectionToolResolverMixedVerdicts(t *testing.T) {
	got := newMixedResolver().Resolve(context.Background(), mixedIdent())

	// --- Tools: Allow + Ask listed, Deny dropped; api tool present (reuse) ---
	type key struct{ conn, tool string }
	toolVerdict := map[key]toolpolicy.Setting{}
	var apiTools int
	for _, c := range got.Tools {
		toolVerdict[key{c.Connection, c.Name}] = c.Verdict
		if c.Type == connections.TypeAPI {
			apiTools++
			if c.Connection != "apiconn" {
				t.Errorf("unexpected api connection %q", c.Connection)
			}
			if c.Verdict != toolpolicy.SettingAllow {
				t.Errorf("api tool verdict = %q, want allow", c.Verdict)
			}
		}
	}
	if apiTools == 0 {
		t.Error("Category A reuse broken: no api-connection tool in output")
	}
	wantAllow := []key{{"alpha", "a1"}, {"alpha", "a2"}, {"beta", "b1"}, {"gamma", "g1"}}
	for _, k := range wantAllow {
		if toolVerdict[k] != toolpolicy.SettingAllow {
			t.Errorf("tool %v verdict = %q, want allow", k, toolVerdict[k])
		}
	}
	if toolVerdict[key{"beta", "b2"}] != toolpolicy.SettingAsk {
		t.Errorf("beta.b2 verdict = %q, want ask", toolVerdict[key{"beta", "b2"}])
	}
	if _, listed := toolVerdict[key{"gamma", "g2"}]; listed {
		t.Error("gamma.g2 is Deny and must NOT appear in Tools")
	}

	// --- MCPServers: Allow-only connection (alpha) only ---
	servers := parseMCPServers(t, got.MCPServers)
	if _, ok := servers["alpha"]; !ok {
		t.Error("alpha is Allow-only and must be a raw relay entry")
	}
	if _, ok := servers["beta"]; ok {
		t.Error("beta has an Ask tool and must NOT be a raw relay entry")
	}
	if _, ok := servers["gamma"]; ok {
		t.Error("gamma has a Deny tool and must NOT be a raw relay entry")
	}
	if _, ok := servers["apiconn"]; ok {
		t.Error("apiconn is an api connection, never an mcp relay entry")
	}

	// --- Deny: only the Deny mcp tool, as a --disallowedTools token ---
	if len(got.Deny) != 1 || got.Deny[0] != toolpolicy.MCPToolToken("gamma", "g2") {
		t.Errorf("Deny = %v, want exactly [%s]", got.Deny, toolpolicy.MCPToolToken("gamma", "g2"))
	}

	// --- Evaluations: relay_withheld recorded for the two mixed connections ---
	withheld := map[string]bool{}
	for _, e := range got.Evaluations {
		if e.Outcome == evaluationRelayWithheld {
			withheld[e.Connection] = true
		}
	}
	if !withheld["beta"] || !withheld["gamma"] {
		t.Errorf("relay_withheld evaluations = %v, want beta and gamma", withheld)
	}
	if withheld["alpha"] {
		t.Error("alpha is Allow-only and must not be withheld from the relay")
	}
}

// FIR-2441 (the Flip, slice 1): Resolve carries the CALLABLE *APIConnectionTool
// handle for every admitted api endpoint in APITools — the shape the three api
// consumers (executor/handler/brief) need and that ConnectionCapability omits.
// APITools must mirror what the reused APIConnectionResolver.ListForAgent returns
// (same tools, same verdicts) so the Flip can point a consumer at this field
// instead of the resolver directly with no change in behavior.
func TestConnectionToolResolverAPIToolsCarryCallableHandle(t *testing.T) {
	ident := mixedIdent()
	got := newMixedResolver().Resolve(context.Background(), ident)

	if len(got.APITools) == 0 {
		t.Fatal("APITools is empty: the callable api handle is not carried through Resolve")
	}

	// Parity: APITools must equal what ListForAgent returns directly (same tool
	// identity + verdict), proving Resolve reuses the resolver verbatim.
	direct := newMixedResolver().api.ListForAgent(context.Background(), ident.apiIdentity())
	if len(got.APITools) != len(direct) {
		t.Fatalf("APITools count = %d, want %d (parity with ListForAgent)", len(got.APITools), len(direct))
	}
	for _, v := range got.APITools {
		if v.Tool == nil {
			t.Fatal("APITools carries a nil *APIConnectionTool handle")
		}
		if v.Tool.ConnectionName() != "apiconn" {
			t.Errorf("unexpected api connection %q, want apiconn", v.Tool.ConnectionName())
		}
		if v.Verdict != toolpolicy.SettingAllow {
			t.Errorf("api tool verdict = %q, want allow", v.Verdict)
		}
		// The handle must be usable — the brief reads Name()+Description(), the
		// executor/handler dispatch it. A blank name would be an unusable handle.
		if v.Tool.Name() == "" {
			t.Error("callable handle has an empty Name()")
		}
	}
}

// The brief adapter is the first consumer moved onto Resolve. It must map the
// carried api handles to the handler brief shape, and — like the old direct
// path — return nil when the flag is off, so the router wiring swap is reversible.
func TestConnectionToolResolverBriefAdapter(t *testing.T) {
	ident := handler.CerebroAPIConnectionBriefIdentity{
		WorkspaceID: gateTestUUID(1),
		RuntimeID:   gateTestUUID(2),
		AgentID:     gateTestUUID(3),
		OwnerID:     gateTestUUID(4),
	}

	briefs := newMixedResolver().APIConnectionToolsForBrief(context.Background(), ident)
	if len(briefs) == 0 {
		t.Fatal("brief adapter returned no api tools with the flag on")
	}
	for _, b := range briefs {
		if b.Name == "" {
			t.Error("brief tool has an empty Name")
		}
		if b.Verdict != string(toolpolicy.SettingAllow) {
			t.Errorf("brief tool verdict = %q, want allow", b.Verdict)
		}
	}

	// Flag off ⇒ nil, identical to the api-only resolver it replaces.
	conns := fakeConnLister{conns: mixedVerdictConns()}
	apiOff := NewAPIConnectionResolver(conns,
		fakeEndpointPolicy{verdicts: map[string]toolpolicy.Setting{"apiconn GET /allow": toolpolicy.SettingAllow}},
		fakeFlag{on: false}, slog.Default())
	off := NewConnectionToolResolver(apiOff, conns, fakeMCPVerdicts{}, fakeFlag{on: false}, fakeEntry, slog.Default())
	if got := off.APIConnectionToolsForBrief(context.Background(), ident); got != nil {
		t.Errorf("flag off must yield nil brief tools, got %v", got)
	}
}

// A tool present on the connection but missing from the verdict table fails
// closed to Deny — it is neither listed nor a relay entry.
func TestConnectionToolResolverUnresolvedToolFailsClosed(t *testing.T) {
	conns := fakeConnLister{conns: []connections.Connection{{
		Name: "solo", Type: connections.TypeMCPHTTP, URL: "http://solo.internal", Enabled: true,
		Tools: []connections.Tool{{Name: "known"}, {Name: "missing"}},
	}}}
	// Only "known" has a verdict; "missing" is absent → fail closed to Deny.
	mcp := fakeMCPVerdicts{verdicts: []toolpolicy.ConnectionToolVerdict{
		{Connection: "solo", Tool: "known", Setting: toolpolicy.SettingAllow},
	}}
	r := NewConnectionToolResolver(nil, conns, mcp, fakeFlag{on: true}, fakeEntry, slog.Default())

	got := r.Resolve(context.Background(), mixedIdent())

	servers := parseMCPServers(t, got.MCPServers)
	if _, ok := servers["solo"]; ok {
		t.Error("solo has an unresolved (fail-closed Deny) tool and must not be a raw relay entry")
	}
	if len(got.Deny) != 1 || got.Deny[0] != toolpolicy.MCPToolToken("solo", "missing") {
		t.Errorf("Deny = %v, want the unresolved tool denied", got.Deny)
	}
}

// The feature is flag-gated: with the flag off, Resolve returns the zero value
// so it can never break a caller's tool loop.
func TestConnectionToolResolverFlagOffReturnsEmpty(t *testing.T) {
	conns := fakeConnLister{conns: mixedVerdictConns()}
	mcp := fakeMCPVerdicts{verdicts: []toolpolicy.ConnectionToolVerdict{
		{Connection: "alpha", Tool: "a1", Setting: toolpolicy.SettingAllow},
	}}
	r := NewConnectionToolResolver(nil, conns, mcp, fakeFlag{on: false}, fakeEntry, slog.Default())

	got := r.Resolve(context.Background(), mixedIdent())
	if len(got.Tools) != 0 || got.MCPServers != nil || len(got.Deny) != 0 || len(got.Evaluations) != 0 {
		t.Errorf("flag off must yield the zero value, got %+v", got)
	}
}

// Lone #3 + FIR-2441 (on_behalf_of): the two sub-resolvers gate the initiator
// differently because their engines differ, and the rule for each lives on
// ConnectionIdentity so no callsite can re-implement it:
//   - mcp_http (tableQuery, tighten-only): the user layer is the fail-closed
//     userCeiling intersection of owner and initiator, AND the initiator is the
//     on_behalf_of layer.
//   - api endpoints (apiIdentity, default-deny GRANT path): the user layer is the
//     REAL owner (never emptied — that would diverge from the call-time guard) and
//     the initiator is ONLY the tighten-only on_behalf_of layer.
//
// Either way the initiator can only ever narrow access (Deny), never widen it. The
// mcp_http user-layer ceiling per case: no initiator → owner; owner == initiator →
// that shared user; owner != initiator → empty (a personal grant never leaks). The
// api half always keeps the real owner plus a Deny-only on_behalf_of layer.
func TestConnectionIdentityOnBehalfOfIntersection(t *testing.T) {
	owner := gateTestUUID(10)
	initiator := gateTestUUID(11)
	var empty pgtype.UUID

	cases := []struct {
		name        string
		owner       pgtype.UUID
		initiator   pgtype.UUID
		wantCeiling pgtype.UUID // the mcp_http user-layer ceiling
	}{
		{"no initiator falls back to owner", owner, empty, owner},
		{"self-delegation keeps the shared user", owner, owner, owner},
		{"owner != initiator fails closed to empty", owner, initiator, empty},
		{"unset owner with initiator fails closed", empty, initiator, empty},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := ConnectionIdentity{
				WorkspaceID: gateTestUUID(1),
				RuntimeID:   gateTestUUID(2),
				AgentID:     gateTestUUID(3),
				OwnerID:     c.owner,
				InitiatorID: c.initiator,
			}
			// api half: the user layer stays the REAL owner (matching the cloud
			// call-time guard), and the initiator is carried as the Deny-only
			// on_behalf_of layer — NOT folded into an emptied ceiling.
			if got := id.apiIdentity().OwnerID; got != c.owner {
				t.Errorf("api half OwnerID = %v, want owner %v", got, c.owner)
			}
			if got := id.apiIdentity().OnBehalfOfID; got != c.initiator {
				t.Errorf("api half OnBehalfOfID = %v, want initiator %v", got, c.initiator)
			}
			// mcp half: the user layer is the fail-closed userCeiling, and the
			// initiator is also carried as the on_behalf_of layer.
			if got := id.tableQuery().UserID; got != c.wantCeiling {
				t.Errorf("mcp half UserID = %v, want %v", got, c.wantCeiling)
			}
			if got := id.tableQuery().OnBehalfOfID; got != c.initiator {
				t.Errorf("mcp half OnBehalfOfID = %v, want initiator %v", got, c.initiator)
			}
			// When the mcp ceiling fails closed, it must be an INVALID uuid (no row
			// matches), not merely a different-but-valid user.
			if c.wantCeiling == empty && id.userCeiling().Valid {
				t.Errorf("expected fail-closed invalid ceiling, got a valid uuid")
			}
		})
	}
}

func parseMCPServers(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	if raw == nil {
		return map[string]any{}
	}
	var doc struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("MCPServers is not valid JSON: %v", err)
	}
	return doc.MCPServers
}
