package runtime

// api_connection_resolver.go (FIR-2388) is the SINGLE source of truth for
// "which api-type connection endpoint tools does this agent get right now, and
// with what verdict". Before this, three surfaces each filtered the same
// endpoint list their own way and drifted:
//
//   - the Firtal Gateway (cloud) kept Allow+Ask, fail-OPEN on a policy error;
//   - the connectiontools HTTP handler (local `multica mcp serve`) kept only
//     Allow, fail-CLOSED;
//   - the claim-time brief never listed api-connection tools at all, so an agent
//     that HAD an endpoint could not see it in its prompt.
//
// ListForAgent replaces all three filters with one, so "listed" == "callable"
// for every runtime (the whole point of FIR-2388). It lives in the runtime
// package — not connectiontools — on purpose: the cloud executor is in runtime
// and connectiontools already imports runtime, so putting the shared resolver in
// connectiontools would be an import cycle (see the build plan's architecture
// correction). The handler package can't import runtime (runtime imports
// handler), so the brief reaches this through an injected interface — see
// APIConnectionToolsForBrief below.
//
// The whole feature stays behind the default-off cerebro_api_connection_tools
// workspace flag; with the flag off ListForAgent returns nil. Per-endpoint access
// is opt-in default-deny (FIR-2166 "C" v2) — only an explicit Allow (or a
// connection whose default_access is allow/ask) admits an endpoint.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	guuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

var ErrAppConnectionDenied = errors.New("app connection call denied")

// The three seams below are narrow interfaces the resolver depends on, so it is
// unit-testable without a database. In production they are satisfied by the
// pool-backed *connections.Store, *toolpolicy.Store, and *cerebrodb.Queries the
// three consumers already hold.

// apiConnectionLister lists a workspace's enabled connections.
type apiConnectionLister interface {
	ListEnabled(ctx context.Context, workspaceID pgtype.UUID) ([]connections.Connection, error)
}

// endpointPolicyResolver resolves the per-actor verdict for one endpoint.
type endpointPolicyResolver interface {
	ConnectionEndpointEffective(ctx context.Context, workspaceID, runtimeID, agentID, userID, onBehalfOfID pgtype.UUID, connName, method, path string) (toolpolicy.Setting, string, error)
	ConnectionToolEffective(ctx context.Context, workspaceID, runtimeID, agentID, userID pgtype.UUID, toolName string) (toolpolicy.Setting, string, error)
}

// apiConnectionFlagChecker reads the default-off cerebro_api_connection_tools flag.
type apiConnectionFlagChecker interface {
	GetCerebroFeatureFlag(ctx context.Context, params cerebrodb.GetCerebroFeatureFlagParams) (bool, error)
}

type apiEndpointDiscoverer func(context.Context, *http.Client, connections.Connection, string) ([]connections.EndpointPermission, error)

const personalizedDiscoveryTTL = time.Minute

type personalizedDiscoveryCacheEntry struct {
	endpoints []connections.EndpointPermission
	expiresAt time.Time
}

// APIConnectionIdentity is the resolved actor an endpoint is gated for. The owner
// layers feed the Workspace › Runtime › Agent › Group › User chain
// (ConnectionEndpointEffective); a zero UUID simply resolves that layer as absent.
//
// OnBehalfOfID is the delegated task initiator — a distinct, TIGHTEN-ONLY policy
// layer (FIR-2441). On this default-deny grant path an on_behalf_of Deny revokes
// the call but an on_behalf_of Allow/Ask can never grant it, so a member can be
// denied a connection across every agent they drive without ever being able to
// open the secrets box to themselves. Zero (Valid == false) means no delegation.
type APIConnectionIdentity struct {
	WorkspaceID  pgtype.UUID
	RuntimeID    pgtype.UUID
	AgentID      pgtype.UUID
	OwnerID      pgtype.UUID
	OnBehalfOfID pgtype.UUID
}

// APIConnectionToolVerdict pairs an admitted endpoint tool with the per-agent
// verdict that admitted it — SettingAllow or SettingAsk. A Deny (or an
// unresolved policy error) never appears here: those endpoints are dropped.
type APIConnectionToolVerdict struct {
	Tool    *APIConnectionTool
	Verdict toolpolicy.Setting
}

// APIConnectionResolver is the shared resolver. It is stateless beyond its store
// handles and a dispatch http.Client, so every consumer building one via
// NewAPIConnectionResolver gets identical semantics — there is no per-instance
// config to drift.
type APIConnectionResolver struct {
	conns  apiConnectionLister
	policy endpointPolicyResolver
	flags  apiConnectionFlagChecker
	client *http.Client
	logger *slog.Logger
	// discover resolves an identity-aware OpenAPI view. It is a function seam so
	// the resolver's fail-closed filtering is unit-testable without a network.
	discover apiEndpointDiscoverer
	cacheMu  sync.Mutex
	cache    map[string]personalizedDiscoveryCacheEntry
	now      func() time.Time
	// exchanger enables per-person session-key exchange (FIR-2564 fase 2) on
	// the tools this resolver builds. Wired when the flag checker is the
	// cerebrodb query handle (which carries the person-key cache); nil
	// otherwise, which fails exchange-enabled calls closed.
	exchanger *ConnectionSessionExchanger
}

// NewAPIConnectionResolver builds the resolver. A nil conns or policy (feature
// not wired) makes ListForAgent return nil. A nil logger defaults to slog.Default.
func NewAPIConnectionResolver(conns apiConnectionLister, policy endpointPolicyResolver, flags apiConnectionFlagChecker, logger *slog.Logger) *APIConnectionResolver {
	if logger == nil {
		logger = slog.Default()
	}
	r := &APIConnectionResolver{
		conns:  conns,
		policy: policy,
		flags:  flags,
		client: &http.Client{Timeout: apiConnectionToolTimeout},
		logger: logger,
		discover: func(ctx context.Context, client *http.Client, conn connections.Connection, principal string) ([]connections.EndpointPermission, error) {
			return connections.DiscoverAPIEndpointsForPrincipal(ctx, client, conn.URL, conn.AuthConfig, principal)
		},
		cache: make(map[string]personalizedDiscoveryCacheEntry),
		now:   time.Now,
	}
	// The cerebrodb query handle doubles as the person-key cache store; when
	// the flag checker is that handle, per-person session exchange is available.
	if store, ok := flags.(connectionPersonKeyStore); ok {
		r.exchanger = NewConnectionSessionExchanger(store, logger)
	}
	return r
}

// ListForAgent returns the api-connection endpoint tools this agent may use, each
// with its Allow/Ask verdict. The one filter every surface shares:
//
//   - flag off / store not wired / incomplete identity → nil (feature is additive
//     and flag-gated; it must never break the caller's tool loop);
//   - connection discovery error → nil (a transient blip must not break the loop);
//   - per-endpoint policy error → DROP that endpoint (fail CLOSED — this fronts the
//     secrets box, so never list a tool whose verdict could not be verified);
//   - verdict Deny → DROP;
//   - verdict Allow or Ask → INCLUDE (Ask is surfaced as "requires approval", not
//     hidden — the caller decides how to honor Ask at call time).
//
// This is the LIST-time decision. The always-on call-time guards remain the hard
// enforcement: the cloud gateway re-resolves each call (apiEndpointSetting,
// fail-closed) and the local handler dispatches only Allow.
func (r *APIConnectionResolver) ListForAgent(ctx context.Context, ident APIConnectionIdentity) []APIConnectionToolVerdict {
	if r == nil || r.conns == nil || r.policy == nil || !ident.WorkspaceID.Valid || !ident.AgentID.Valid {
		return nil
	}
	if !r.flagEnabled(ctx, ident.WorkspaceID) {
		return nil
	}
	conns, err := r.conns.ListEnabled(ctx, ident.WorkspaceID)
	if err != nil {
		r.logger.Warn("api connection resolver: list enabled connections failed",
			"workspace_id", util.UUIDToString(ident.WorkspaceID), "error", err)
		return nil
	}
	conns = r.connectionsForIdentity(ctx, conns, ident)
	built := buildAPIConnectionTools(conns, r.client, r.exchanger)
	if len(built) == 0 {
		return nil
	}
	out := make([]APIConnectionToolVerdict, 0, len(built))
	for _, t := range built {
		api, ok := t.(*APIConnectionTool)
		if !ok {
			continue
		}
		setting, _, err := r.policy.ConnectionEndpointEffective(
			ctx, ident.WorkspaceID, ident.RuntimeID, ident.AgentID, ident.OwnerID, ident.OnBehalfOfID,
			api.ConnectionName(), api.Method(), api.Path())
		if err != nil {
			// Fail closed: a resolve error must not expose a gate that fronts the
			// secrets box. The endpoint is dropped from the list.
			r.logger.Warn("api connection resolver: endpoint policy resolve failed — dropping (fail closed)",
				"workspace_id", util.UUIDToString(ident.WorkspaceID),
				"agent_id", util.UUIDToString(ident.AgentID),
				"connection", api.ConnectionName(), "method", api.Method(), "path", api.Path(),
				"error", err)
			continue
		}
		if setting == toolpolicy.SettingDeny {
			continue
		}
		out = append(out, APIConnectionToolVerdict{Tool: api, Verdict: setting})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// connectionsForIdentity replaces the static endpoint catalogue on
// identity-aware API connections with the intersection of that catalogue and
// a fresh principal-specific OpenAPI view. The static catalogue remains the
// admin-authored ceiling: discovery can remove or relabel an operation, never
// introduce one. A discovery error drops that connection (fail closed).
func (r *APIConnectionResolver) connectionsForIdentity(ctx context.Context, conns []connections.Connection, ident APIConnectionIdentity) []connections.Connection {
	if r.discover == nil {
		return conns
	}
	principal := "agent:" + util.UUIDToString(ident.AgentID)
	out := make([]connections.Connection, 0, len(conns))
	for _, conn := range conns {
		if conn.Type != connections.TypeAPI || conn.AuthConfig.OnBehalfOf == nil || !conn.AuthConfig.OnBehalfOf.Enabled {
			out = append(out, conn)
			continue
		}
		discovered, err := r.discoverForIdentity(ctx, conn, principal)
		if err != nil {
			r.logger.Warn("api connection resolver: personalized discovery failed — dropping connection (fail closed)",
				"workspace_id", util.UUIDToString(ident.WorkspaceID),
				"agent_id", util.UUIDToString(ident.AgentID),
				"connection", conn.Name, "error", err)
			continue
		}
		conn.EndpointPermissions = intersectEndpointPermissions(conn.EndpointPermissions, discovered)
		out = append(out, conn)
	}
	return out
}

func (r *APIConnectionResolver) discoverForIdentity(ctx context.Context, conn connections.Connection, principal string) ([]connections.EndpointPermission, error) {
	key := strings.Join([]string{
		conn.ID, conn.Name, conn.URL, conn.UpdatedAt.UTC().Format(time.RFC3339Nano), principal,
	}, "|")
	now := r.now()
	r.cacheMu.Lock()
	entry, ok := r.cache[key]
	if ok && now.Before(entry.expiresAt) {
		r.cacheMu.Unlock()
		return entry.endpoints, nil
	}
	delete(r.cache, key)
	r.cacheMu.Unlock()

	endpoints, err := r.discover(ctx, r.client, conn, principal)
	if err != nil {
		return nil, err
	}
	r.cacheMu.Lock()
	r.cache[key] = personalizedDiscoveryCacheEntry{
		endpoints: endpoints,
		expiresAt: now.Add(personalizedDiscoveryTTL),
	}
	r.cacheMu.Unlock()
	return endpoints, nil
}

type endpointMethodKey struct {
	path   string
	method string
}

func intersectEndpointPermissions(configured, discovered []connections.EndpointPermission) []connections.EndpointPermission {
	dynamic := make(map[endpointMethodKey]connections.EndpointPermission)
	for _, ep := range discovered {
		path := normalizeEndpointPath(ep.Path)
		for _, method := range ep.Methods {
			dynamic[endpointMethodKey{path: path, method: strings.ToUpper(strings.TrimSpace(method))}] = ep
		}
	}
	var out []connections.EndpointPermission
	for _, ep := range configured {
		path := normalizeEndpointPath(ep.Path)
		for _, methodRaw := range ep.Methods {
			method := strings.ToUpper(strings.TrimSpace(methodRaw))
			found, ok := dynamic[endpointMethodKey{path: path, method: method}]
			if !ok || (found.Granted != nil && !*found.Granted) {
				continue
			}
			summary := strings.TrimSpace(found.Summary)
			if summary == "" {
				summary = ep.Summary
			}
			out = append(out, connections.EndpointPermission{
				Path: path, Methods: []string{method}, Summary: summary, Granted: found.Granted,
			})
		}
	}
	return out
}

// CallForApp dispatches one endpoint for a published mini app. The apps handler
// has already enforced the app grant ceiling; this second gate applies the
// configured connection policy for the viewing human and keeps credentials
// server-side.
func (r *APIConnectionResolver) CallForApp(ctx context.Context, workspaceID, memberID, connectionID guuid.UUID, toolName string, arguments map[string]any) (string, error) {
	if r == nil || r.conns == nil || r.policy == nil {
		return "", ErrAppConnectionDenied
	}
	ws := pgtype.UUID{Bytes: workspaceID, Valid: true}
	member := pgtype.UUID{Bytes: memberID, Valid: true}
	conns, err := r.conns.ListEnabled(ctx, ws)
	if err != nil {
		return "", ErrAppConnectionDenied
	}
	for _, conn := range conns {
		if conn.ID != connectionID.String() {
			continue
		}
		if conn.Type == connections.TypeMCPHTTP {
			declared := false
			for _, tool := range conn.Tools {
				if tool.Name == toolName {
					declared = true
					break
				}
			}
			if !declared {
				return "", ErrAppConnectionDenied
			}
			setting, _, policyErr := r.policy.ConnectionToolEffective(ctx, ws, pgtype.UUID{}, pgtype.UUID{}, member, toolName)
			if policyErr != nil || setting != toolpolicy.SettingAllow {
				return "", ErrAppConnectionDenied
			}
			raw, callErr := connections.CallMCPTool(ctx, conn.URL, conn.AuthConfig, toolName, arguments)
			if callErr != nil {
				return "", ErrAppConnectionDenied
			}
			return string(raw), nil
		}
		for _, built := range buildAPIConnectionTools([]connections.Connection{conn}, r.client, r.exchanger) {
			api, ok := built.(*APIConnectionTool)
			if !ok || api.Name() != toolName {
				continue
			}
			setting, _, policyErr := r.policy.ConnectionEndpointEffective(ctx, ws, pgtype.UUID{}, pgtype.UUID{}, member, member, api.ConnectionName(), api.Method(), api.Path())
			if policyErr != nil || setting != toolpolicy.SettingAllow {
				return "", ErrAppConnectionDenied
			}
			return api.Call(WithConnectionTriggerMember(ctx, memberID.String()), arguments)
		}
	}
	return "", ErrAppConnectionDenied
}

// flagEnabled reports whether cerebro_api_connection_tools is on for the
// workspace. Default OFF: a nil flag handle, a missing override, or a DB error all
// resolve to false, mirroring the prior per-consumer flag checks.
func (r *APIConnectionResolver) flagEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if r.flags == nil {
		return false
	}
	enabled, err := r.flags.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     flagAPIConnectionTools,
	})
	if err != nil {
		return false
	}
	return enabled
}
