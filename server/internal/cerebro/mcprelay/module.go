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
		Relay:   NewRelay(signer, store),
		store:   store,
		baseURL: strings.TrimRight(base, "/"),
	}, true
}

// BuildMCPConfig satisfies handler.WorkspaceConnectionsInjector. It defers to
// the connections store but supplies THIS module as the URL rewriter, so an
// internal connection is injected as a relay URL + scoped token rather than its
// real internal URL + secret (FIR-1563).
func (m *Module) BuildMCPConfig(ctx context.Context, workspaceID pgtype.UUID) json.RawMessage {
	return m.store.BuildMCPConfigRelayed(ctx, workspaceID, m)
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
)
