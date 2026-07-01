package runtime

// connection_tool_resolver.go (FIR-2441 Fase 2 — Shadow) folds BOTH connection
// types behind ONE resolver so, after the later Flip, every consumer (claim,
// brief, runtime-scan, cloud gateway) reads a single answer instead of the two
// that drift today:
//
//   - api connections resolve through the EXISTING APIConnectionResolver
//     (FIR-2388, api_connection_resolver.go) — reused verbatim as an internal
//     sub-component (Category A). It is NOT rebuilt.
//   - mcp_http connections resolve their per-tool verdict through the same
//     toolpolicy Table() the admin screen and DeniedConnectionTools read
//     (Category B), so "listed == callable" holds for MCP tools too.
//
// Resolve is SHADOW-ONLY for now: it computes ResolvedConnectionTools alongside
// the live paths (daemon.go BuildMCPConfig + DisallowedMCPTools, the gateway,
// the runtime-scan) but decides nothing. The old path still grants access. The
// Flip (Fase 3) points the four consumers at this output; the Contract (Fase 4)
// deletes the independent BuildMCPConfig / DisallowedMCPTools decisions.
//
// The whole feature stays behind the default-off cerebro_api_connection_tools
// workspace flag — with the flag off Resolve returns the zero value.
//
// Two adversarial findings are load-bearing here (see the FIR-2441 adversarial
// artifact, all five verified TRUE against the code):
//
//   - Lone #1: the mcp relay (mcprelay/relay.go) enforces NO per-tool policy —
//     it validates only the signed token, connection name and workspace. So an
//     Ask/Deny verdict CANNOT be honoured once a tool is exposed as a raw relay
//     entry. Therefore raw relay entries are Allow-ONLY: a connection is exposed
//     as a raw MCP server only when EVERY discovered tool on it resolves to
//     Allow. If any tool is Ask or Deny the whole connection is withheld from
//     the raw relay list (it needs the policy-aware dispatch/proxy path). This
//     is Tine's mixed-verdict sharpening: an Allow tool must never drag its
//     Ask/Deny siblings in as a raw relay entry, because a relay entry exposes
//     the whole server, not one tool.
//   - Lone #2: the conflict rule is Deny > Allow > Ask > default_access
//     evaluated across the WHOLE scope stack — not "most-specific scope wins".
//     That precedence lives in toolpolicy (ConnectionEndpointEffective and
//     Table/Resolve), which this resolver delegates to, so a workspace-Deny
//     correctly caps an agent-Allow on a secrets connection.

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// ConnectionIdentity is the resolved actor a connection resolve is gated for.
// It mirrors APIConnectionIdentity but is the type all four consumers will pass
// post-Flip.
//
// Two user fields feed Lone #3 (on_behalf_of): OwnerID is the agent's owner and
// InitiatorID is the human the work is performed for. The user layer of the tool
// policy is gated on the INTERSECTION (most restrictive) of the two, computed
// once by userCeiling() below and used identically by BOTH sub-resolvers
// (apiIdentity for the api half, tableQuery for the mcp_http half). Because the
// intersection lives on the identity — not at each callsite — every consumer the
// Flip points here (agent-brief, cloud call-time, local call-time, runtime-scan)
// gets the SAME fail-closed rule by construction and cannot re-implement it as
// "most permissive" and leak a grant between the owner and the initiator.
//
// InitiatorID is optional: when it is not set (Valid == false) there is no
// delegation in play and the ceiling is simply OwnerID, exactly like the api-only
// APIConnectionIdentity — so nothing changes for the non-delegated path. When it
// IS set and differs from OwnerID, the intersection of two distinct single-user
// sets is empty, so the user layer is emptied (invalid UUID → no user-scoped
// grant matches). That is the fail-closed choice: delegation can only ever
// narrow access, never widen it. Modelling on_behalf_of as its own policy layer
// (so a rule can name the initiator explicitly) is a later slice.
type ConnectionIdentity struct {
	WorkspaceID pgtype.UUID
	RuntimeID   pgtype.UUID
	AgentID     pgtype.UUID
	OwnerID     pgtype.UUID
	// InitiatorID is the human the work is performed for (on_behalf_of). Zero
	// (Valid == false) means no delegation — the ceiling is OwnerID.
	InitiatorID pgtype.UUID
}

// userCeiling is the single user identity the tool-policy user layer is gated on:
// the fail-closed intersection of the agent owner and the work initiator (Lone
// #3). No initiator → the owner. Owner == initiator → that user. Owner and
// initiator both set but different → an invalid UUID, which matches no user-layer
// row, so a personal grant on either party never leaks to the other.
func (id ConnectionIdentity) userCeiling() pgtype.UUID {
	if !id.InitiatorID.Valid {
		return id.OwnerID
	}
	if id.OwnerID.Valid && id.OwnerID.Bytes == id.InitiatorID.Bytes {
		return id.OwnerID
	}
	// Owner != initiator (or owner unset): the intersection is empty. Return the
	// zero UUID so the user layer is fail-closed (no user-scoped grant applies).
	return pgtype.UUID{}
}

func (id ConnectionIdentity) apiIdentity() APIConnectionIdentity {
	return APIConnectionIdentity{
		WorkspaceID: id.WorkspaceID,
		RuntimeID:   id.RuntimeID,
		AgentID:     id.AgentID,
		OwnerID:     id.userCeiling(),
	}
}

func (id ConnectionIdentity) tableQuery() toolpolicy.TableQuery {
	return toolpolicy.TableQuery{
		WorkspaceID: id.WorkspaceID,
		RuntimeID:   id.RuntimeID,
		AgentID:     id.AgentID,
		UserID:      id.userCeiling(),
	}
}

// ConnectionCapability is one resolved connection tool the agent may use, with
// the verdict that admitted it — Allow or Ask. A Deny (or an unresolved policy
// error) never appears here; those tools are dropped, exactly like the api-only
// APIConnectionResolver.
type ConnectionCapability struct {
	// Name is the callable tool name (the MCP tool, or the synthesized api
	// endpoint tool name).
	Name string
	// Type is connections.TypeAPI or connections.TypeMCPHTTP.
	Type string
	// Connection is the workspace connection the tool belongs to.
	Connection string
	// Verdict is SettingAllow or SettingAsk.
	Verdict toolpolicy.Setting
}

// PolicyEvaluation is the Observe record for one connection tool: the verdict it
// resolved to and what the resolver did with it. It is what powers "Explain
// access" — every tool that was listed, dropped, or withheld from the raw relay
// leaves a trace.
type PolicyEvaluation struct {
	Connection string
	Tool       string
	Type       string
	Verdict    toolpolicy.Setting
	// Outcome is one of the evaluationOutcome* constants below.
	Outcome string
	// Reason is a short human-readable explanation of the outcome.
	Reason string
}

// Evaluation outcomes for PolicyEvaluation.Outcome.
const (
	// evaluationListed: tool is offered to the agent (Allow or Ask).
	evaluationListed = "listed"
	// evaluationDenied: tool dropped by a Deny (or a fail-closed policy error).
	evaluationDenied = "denied"
	// evaluationRelayWithheld: an mcp_http connection was NOT exposed as a raw
	// relay entry because it is not Allow-only (Lone #1). Its Allow tools remain
	// in Tools but there is no raw MCP server for them this shadow pass — the
	// policy-aware path handles them.
	evaluationRelayWithheld = "relay_withheld"
)

// ResolvedConnectionTools is the single output every consumer reads post-Flip.
type ResolvedConnectionTools struct {
	// Tools is every admitted connection tool (api + mcp_http) with its verdict.
	// It is the flat verdict view for Observe/"Explain access": a ConnectionCapability
	// deliberately carries no callable handle, so it cannot feed a consumer that must
	// dispatch or describe a tool.
	Tools []ConnectionCapability
	// APITools is every admitted api-connection endpoint tool with its CALLABLE
	// *APIConnectionTool handle and verdict — the shape the three api consumers need
	// but ConnectionCapability omits by design: the cloud executor registers
	// v.Tool with the task registry, the local handler calls v.Tool.Call, and the
	// claim brief reads v.Tool.Description(). Populated verbatim from the reused
	// APIConnectionResolver.ListForAgent (Category A), so the Flip can point each
	// consumer at this field instead of the resolver directly. Empty when the api
	// half is unwired or the flag is off.
	APITools []APIConnectionToolVerdict
	// MCPServers is a {"mcpServers":{...}} document of the raw relay entries for
	// the Allow-only mcp_http connections, ready to merge into --mcp-config. Nil
	// when no connection qualifies. Ask/Deny connections are absent by design.
	MCPServers json.RawMessage
	// Deny is the --disallowedTools tokens for every Deny-verdict mcp_http tool
	// ("mcp__<connection>__<tool>"), derived from the SAME verdict table as the
	// rest of this output.
	Deny []string
	// Evaluations is the Observe trace: one entry per tool considered.
	Evaluations []PolicyEvaluation
}

// mcpToolVerdictResolver resolves the effective verdict of every mcp_http
// connection tool for a chain context. Satisfied in production by
// *toolpolicy.Store; a fake in tests keeps the resolver DB-free.
type mcpToolVerdictResolver interface {
	ConnectionToolVerdicts(ctx context.Context, in toolpolicy.TableQuery) ([]toolpolicy.ConnectionToolVerdict, error)
}

// mcpEntryBuilder builds the raw --mcp-config relay entry for one connection.
// Satisfied in production by connections.MCPServerEntry (via defaultMCPEntry);
// a fake in tests avoids depending on relay wiring.
type mcpEntryBuilder func(c connections.Connection) map[string]any

// ConnectionToolResolver is the one resolver spanning both connection types. It
// is stateless beyond its store handles, so every consumer building one gets
// identical semantics — the whole point of collapsing the two paths.
type ConnectionToolResolver struct {
	api      *APIConnectionResolver
	conns    apiConnectionLister
	verdicts mcpToolVerdictResolver
	flags    apiConnectionFlagChecker
	entryFor mcpEntryBuilder
	logger   *slog.Logger
}

// NewConnectionToolResolver builds the resolver. api is the reused Category-A
// resolver (may be nil when api connections are not wired — Resolve then simply
// yields no api tools). A nil verdicts or conns makes the mcp_http half yield
// nothing. A nil entryFor defaults to the direct-URL connections.MCPServerEntry.
// A nil logger defaults to slog.Default.
func NewConnectionToolResolver(
	api *APIConnectionResolver,
	conns apiConnectionLister,
	verdicts mcpToolVerdictResolver,
	flags apiConnectionFlagChecker,
	entryFor mcpEntryBuilder,
	logger *slog.Logger,
) *ConnectionToolResolver {
	if logger == nil {
		logger = slog.Default()
	}
	if entryFor == nil {
		entryFor = defaultMCPEntry
	}
	return &ConnectionToolResolver{
		api:      api,
		conns:    conns,
		verdicts: verdicts,
		flags:    flags,
		entryFor: entryFor,
		logger:   logger,
	}
}

// defaultMCPEntry is the production relay-entry builder: the direct-URL entry a
// cloud runtime uses. The Flip wires the relay rewriter for local runtimes.
func defaultMCPEntry(c connections.Connection) map[string]any {
	return connections.MCPServerEntry(c, nil)
}

// Resolve computes the unified connection-tool answer for one actor. It is
// flag-gated and additive: with the feature off, an incomplete identity, or the
// stores unwired, it returns the zero value so it can never break a caller's
// tool loop. Fail-closed everywhere policy could not be verified — this fronts
// the secrets box.
func (r *ConnectionToolResolver) Resolve(ctx context.Context, ident ConnectionIdentity) ResolvedConnectionTools {
	var out ResolvedConnectionTools
	if r == nil || !ident.WorkspaceID.Valid || !ident.AgentID.Valid {
		return out
	}
	if !r.flagEnabled(ctx, ident.WorkspaceID) {
		return out
	}

	// --- Category A: api connections (REUSE APIConnectionResolver verbatim) ---
	if r.api != nil {
		for _, v := range r.api.ListForAgent(ctx, ident.apiIdentity()) {
			// Carry the callable handle for the api consumers (executor/handler/brief)…
			out.APITools = append(out.APITools, v)
			// …and mirror it into the flat verdict view for Observe.
			out.Tools = append(out.Tools, ConnectionCapability{
				Name:       v.Tool.Name(),
				Type:       connections.TypeAPI,
				Connection: v.Tool.ConnectionName(),
				Verdict:    v.Verdict,
			})
			out.Evaluations = append(out.Evaluations, PolicyEvaluation{
				Connection: v.Tool.ConnectionName(),
				Tool:       v.Tool.Name(),
				Type:       connections.TypeAPI,
				Verdict:    v.Verdict,
				Outcome:    evaluationListed,
				Reason:     "api endpoint admitted by APIConnectionResolver",
			})
		}
	}

	// --- Category B: mcp_http connections ---
	r.resolveMCP(ctx, ident, &out)
	return out
}

// resolveMCP folds the mcp_http half into out. It reads every enabled
// connection once and the per-tool verdict table once (both fail closed): a
// discovery or resolve error withholds the whole mcp_http half rather than
// letting an unverified tool through.
func (r *ConnectionToolResolver) resolveMCP(ctx context.Context, ident ConnectionIdentity, out *ResolvedConnectionTools) {
	if r.conns == nil || r.verdicts == nil {
		return
	}
	conns, err := r.conns.ListEnabled(ctx, ident.WorkspaceID)
	if err != nil {
		r.logger.Warn("connection tool resolver: list enabled connections failed (mcp withheld)",
			"workspace_id", util.UUIDToString(ident.WorkspaceID), "error", err)
		return
	}
	verdicts, err := r.verdicts.ConnectionToolVerdicts(ctx, ident.tableQuery())
	if err != nil {
		// Fail closed: an unresolved verdict table must not expose a raw MCP
		// server whose per-tool policy could not be verified.
		r.logger.Warn("connection tool resolver: mcp verdict resolve failed (mcp withheld, fail closed)",
			"workspace_id", util.UUIDToString(ident.WorkspaceID),
			"agent_id", util.UUIDToString(ident.AgentID), "error", err)
		return
	}
	verdictByTool := make(map[string]toolpolicy.Setting, len(verdicts))
	for _, v := range verdicts {
		verdictByTool[v.Connection+"\x00"+v.Tool] = v.Setting
	}

	servers := map[string]any{}
	for _, c := range conns {
		if c.Type != connections.TypeMCPHTTP || len(c.Tools) == 0 {
			continue
		}
		allowOnly := true // becomes false on the first non-Allow tool
		for _, t := range c.Tools {
			// A tool present on the connection but absent from the verdict table
			// could not be resolved — fail closed to Deny.
			setting, ok := verdictByTool[c.Name+"\x00"+t.Name]
			if !ok {
				setting = toolpolicy.SettingDeny
			}
			switch setting {
			case toolpolicy.SettingAllow:
				out.Tools = append(out.Tools, ConnectionCapability{
					Name:       t.Name,
					Type:       connections.TypeMCPHTTP,
					Connection: c.Name,
					Verdict:    toolpolicy.SettingAllow,
				})
				out.Evaluations = append(out.Evaluations, mcpEval(c.Name, t.Name, toolpolicy.SettingAllow, evaluationListed, "mcp tool allowed"))
			case toolpolicy.SettingAsk:
				allowOnly = false
				out.Tools = append(out.Tools, ConnectionCapability{
					Name:       t.Name,
					Type:       connections.TypeMCPHTTP,
					Connection: c.Name,
					Verdict:    toolpolicy.SettingAsk,
				})
				out.Evaluations = append(out.Evaluations, mcpEval(c.Name, t.Name, toolpolicy.SettingAsk, evaluationListed, "mcp tool requires approval — not a raw relay entry"))
			default: // Deny (explicit, or fail-closed for an unresolved tool)
				allowOnly = false
				out.Deny = append(out.Deny, toolpolicy.MCPToolToken(c.Name, t.Name))
				out.Evaluations = append(out.Evaluations, mcpEval(c.Name, t.Name, toolpolicy.SettingDeny, evaluationDenied, "mcp tool denied"))
			}
		}
		// Lone #1 + Tine's mixed-verdict sharpening: a raw relay entry exposes the
		// WHOLE server (the relay enforces no per-tool policy), so it is emitted
		// only when EVERY tool on the connection is Allow. A single Ask/Deny tool
		// withholds the connection from the raw relay list — its Allow siblings
		// must not drag the Ask/Deny tools in.
		if allowOnly {
			servers[c.Name] = r.entryFor(c)
		} else {
			out.Evaluations = append(out.Evaluations, PolicyEvaluation{
				Connection: c.Name,
				Type:       connections.TypeMCPHTTP,
				Outcome:    evaluationRelayWithheld,
				Reason:     "connection has a non-Allow tool — withheld from raw relay (Allow-only rule)",
			})
		}
	}
	if len(servers) > 0 {
		if b, err := json.Marshal(map[string]any{"mcpServers": servers}); err == nil {
			out.MCPServers = b
		} else {
			r.logger.Warn("connection tool resolver: marshal mcp servers failed",
				"workspace_id", util.UUIDToString(ident.WorkspaceID), "error", err)
		}
	}
}

func mcpEval(conn, tool string, verdict toolpolicy.Setting, outcome, reason string) PolicyEvaluation {
	return PolicyEvaluation{
		Connection: conn,
		Tool:       tool,
		Type:       connections.TypeMCPHTTP,
		Verdict:    verdict,
		Outcome:    outcome,
		Reason:     reason,
	}
}

// flagEnabled reports whether the default-off cerebro_api_connection_tools flag
// is on for the workspace. A nil flag handle, a missing override, or a DB error
// all resolve to false, mirroring APIConnectionResolver.flagEnabled.
func (r *ConnectionToolResolver) flagEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
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
