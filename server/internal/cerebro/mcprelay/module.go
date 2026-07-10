package mcprelay

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// Env keys that configure the relay. When either is unset the module is not
// built (ok=false) and the daemon falls back to the direct injection path —
// local runtimes simply keep their pre-FIR-1563 (broken-for-internal) behavior
// rather than getting an unusable relay URL.
const (
	envSecret  = "CEREBRO_MCP_RELAY_SECRET" // HMAC secret for token signing.
	envBaseURL = "CEREBRO_MCP_RELAY_URL"    // public base, e.g. https://Multica.firtal.com/mcp-relay
)

// Module bundles the relay pieces. It also satisfies
// handler.WorkspaceConnectionsInjector so the router can swap it in for the raw
// connections store with no other change: when this module is the injector,
// every internal connection injected into a local runtime is rewritten to a
// relay URL. Single construction entry point, matching the Agent Vault module.
type Module struct {
	Signer  *Signer
	Relay   *Relay
	store   *connections.Store
	baseURL string
}

// NewModule builds the relay from environment config + the DB pool. Returns
// ok=false when the relay is not configured so callers skip mounting cleanly.
func NewModule(pool *pgxpool.Pool) (*Module, bool) {
	secret := strings.TrimSpace(os.Getenv(envSecret))
	base := strings.TrimSpace(os.Getenv(envBaseURL))
	if secret == "" || base == "" {
		return nil, false
	}
	signer := NewSigner(secret)
	store := connections.New(pool)
	return &Module{
		Signer:  signer,
		Relay:   NewRelay(signer, store, chainToolPolicy{store: toolpolicy.NewStore(pool)}),
		store:   store,
		baseURL: strings.TrimRight(base, "/"),
	}, true
}

// chainToolPolicy adapts toolpolicy.Store to the relay's toolPolicy seam. It
// resolves the verdict for the actor the relay token carries through the same
// Workspace › Runtime › Agent › Group › User › OnBehalfOf chain the admin screen
// and DisallowedMCPTools use (ConnectionToolVerdicts → Table). A workspace-scoped
// token (Mint) carries no actor, so the TableQuery has only WorkspaceID and the
// resolve collapses to the workspace scope — the first slice's behavior. An
// actor-scoped token (MintFor at claim) additionally honors an agent-level or
// member-level Deny, closing the leak for CLIs that ignore --disallowedTools
// (FIR-2441 fix-list #2 follow-up). Deny is the only blocking verdict;
// Allow/Ask/unknown pass, so this can only ever tighten.
type chainToolPolicy struct {
	store *toolpolicy.Store
}

func (p chainToolPolicy) ToolDenied(ctx context.Context, workspaceID pgtype.UUID, actor ConnActor, connName, toolName string) (bool, error) {
	verdicts, err := p.store.ConnectionToolVerdicts(ctx, toolpolicy.TableQuery{
		WorkspaceID:  workspaceID,
		RuntimeID:    actor.RuntimeID,
		AgentID:      actor.AgentID,
		UserID:       actor.OwnerID,
		OnBehalfOfID: actor.OnBehalfOfID,
	})
	if err != nil {
		return false, err
	}
	for _, v := range verdicts {
		if v.Connection == connName && v.Tool == toolName {
			return v.Setting == toolpolicy.SettingDeny, nil
		}
	}
	return false, nil
}

// BuildMCPConfig satisfies handler.WorkspaceConnectionsInjector. It defers to
// the connections store but supplies THIS module as the URL rewriter, so an
// internal connection is injected as a relay URL + workspace-scoped token rather
// than its real internal URL + secret (FIR-1563). It carries no actor, so it is
// the right builder for the agent-less runtime tool-scan; the claim path prefers
// BuildMCPConfigForActor so the minted token also carries the actor.
func (m *Module) BuildMCPConfig(ctx context.Context, workspaceID pgtype.UUID) json.RawMessage {
	return m.store.BuildMCPConfigRelayed(ctx, workspaceID, m)
}

// BuildMCPConfigForActor is BuildMCPConfig with the claim's actor bound into
// every minted relay token, so the relay resolves an agent- or member-level Deny
// server-side at call time — even for a CLI that ignores --disallowedTools
// (FIR-2441 fix-list #2 follow-up). The legacy claim path (flag off) calls this
// via an optional-interface assertion, so no handler seam changes; when the relay
// is unconfigured the assertion misses and the caller falls back to the direct
// injector unchanged.
func (m *Module) BuildMCPConfigForActor(ctx context.Context, workspaceID, runtimeID, agentID, ownerID, initiatorID pgtype.UUID) json.RawMessage {
	if m == nil {
		return nil
	}
	return m.store.BuildMCPConfigRelayed(ctx, workspaceID, actorRewriter{
		mod: m,
		actor: ConnActor{
			RuntimeID:    runtimeID,
			AgentID:      agentID,
			OwnerID:      ownerID,
			OnBehalfOfID: initiatorID,
		},
	})
}

// actorRewriter is a per-claim connections.MCPURLRewriter that mints
// actor-scoped relay tokens. It exists so BuildMCPConfigForActor can thread the
// claim's actor into the token without changing the shared MCPURLRewriter
// interface (which the workspace-scoped Module.RelayEntry also satisfies).
type actorRewriter struct {
	mod   *Module
	actor ConnActor
}

// RelayEntry mirrors Module.RelayEntry's internal-only gate but mints with the
// actor via MintFor. The verdict-carrying token lets the relay honor the full
// policy chain for this actor at call time.
func (a actorRewriter) RelayEntry(workspaceID, connectionName, connectionURL string, internal bool) (url, bearer string, ok bool) {
	if a.mod == nil || (!internal && !isInternalHost(connectionURL)) {
		return "", "", false
	}
	token, err := a.mod.Signer.MintFor(workspaceID, connectionName, a.actor)
	if err != nil {
		return "", "", false
	}
	return a.mod.baseURL + "/" + connectionName, token, true
}

// RelayEntry implements connections.MCPURLRewriter. For an internal connection
// it returns the public relay URL the local runtime should call plus a freshly
// minted, connection-scoped token. ok=false leaves the direct entry untouched
// (e.g. a public connection the local runtime can already reach). A connection
// counts as internal when its flag is set OR its host is unresolvable off the
// Sliplane network (a *.internal host) — belt-and-suspenders so the fix holds
// even if the flag was never set on the row.
func (m *Module) RelayEntry(workspaceID, connectionName, connectionURL string, internal bool) (url, bearer string, ok bool) {
	if m == nil || (!internal && !isInternalHost(connectionURL)) {
		return "", "", false
	}
	token, err := m.Signer.Mint(workspaceID, connectionName)
	if err != nil {
		return "", "", false
	}
	return m.baseURL + "/" + connectionName, token, true
}

// isInternalHost reports whether a URL points at a Sliplane-internal host that a
// local runtime cannot resolve (e.g. customer-service-mcp.internal:3000).
func isInternalHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return strings.HasSuffix(host, ".internal") || host == "internal"
}

// compile-time checks: the module is both a URL rewriter and a connections
// injector, so swapping it in at the router needs no interface gymnastics.
var (
	_ connections.MCPURLRewriter = (*Module)(nil)
	_ connections.MCPURLRewriter = actorRewriter{}
)
