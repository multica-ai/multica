package connectiontools

// FIR-2273 — expose api-type workspace connection endpoints to the Multica MCP
// server (`multica mcp serve`) so a LOCAL runtime (a laptop) can call them
// WITHOUT switching to the Firtal Gateway.
//
// mcp_http connections already reach local runtimes through the FIR-1563 relay;
// this is ONLY for api-type connections (connections.TypeAPI), whose individual
// endpoints are permission-gated. The actual HTTP call to the connection's URL
// is dispatched SERVER-SIDE here, inside Multica, exactly like the Firtal
// Gateway path (runtime.APIConnectionTool.Call): the credential never leaves the
// backend, and because this code runs inside the internal network it can reach
// `.internal` connection URLs directly.
//
// Two AGENT-ONLY endpoints (require the server-set X-Actor-Source == "task_token"
// header, the same authoritative identity the personal-browser gate uses):
//
//	GET  /api/cerebro/connection-tools        — list the endpoints THIS agent is allowed
//	POST /api/cerebro/connection-tools/call   — dispatch one allowed endpoint
//
// Access is default-deny per agent: an endpoint is listed/callable only when the
// workspace feature flag `cerebro_api_connection_tools` is ON AND
// toolpolicy.ConnectionEndpointEffective resolves to SettingAllow for the calling
// agent. SettingAsk and SettingDeny are both "not allowed" here — there is no
// approval inbox on this path, and this fronts the secrets box, so it must never
// proceed on an unresolved ask.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/runtime"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// AgentResolver resolves an agent's workspace + runtime + owner from its id.
// Satisfied by the QueriesAgentResolver adapter over *db.Queries.GetAgent; kept
// as a seam so the handler is unit-testable without a database.
type AgentResolver interface {
	ResolveAgent(ctx context.Context, agentID pgtype.UUID) (workspaceID, runtimeID, ownerID pgtype.UUID, err error)
}

// Handler exposes api-type connection endpoints to the Multica MCP server.
//
// FIR-2441 (the Flip, slice 3): the "which endpoints does this agent get"
// decision now runs through the UNIFIED runtime.ConnectionToolResolver — the same
// resolver the claim brief (slice 1, #2000) and the cloud executor (slice 2,
// #2001) moved onto — reading its api half (Resolve(...).APITools). The api
// sub-component is the reused APIConnectionResolver, so APITools is the same
// Allow+Ask verdict list ListForAgent returned before (Deny and any endpoint
// whose verdict could not be resolved are dropped, fail closed) — identical
// callable handles, same default-off cerebro_api_connection_tools flag gate
// (Resolve yields the zero value with the flag off). Both List and Call read that
// single output, so "listed == callable" holds identically for the local surface.
// This handler still owns only the agent-identity gate (requireAgent) and the
// local dispatch contract; the resolver owns the flag check, endpoint discovery,
// and the per-endpoint policy verdict. The always-on call-time guard inside
// APIConnectionTool.Call is untouched.
type Handler struct {
	agents   AgentResolver
	resolver *runtime.ConnectionToolResolver
}

// NewHandler wires the handler from an agent resolver and the unified connection
// tool resolver. A nil resolver makes the endpoints return an empty tool list
// (feature off), never an error.
func NewHandler(agents AgentResolver, resolver *runtime.ConnectionToolResolver) *Handler {
	return &Handler{agents: agents, resolver: resolver}
}

// --- request/response types -------------------------------------------------

type toolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type listResponse struct {
	Tools []toolDescriptor `json:"tools"`
}

type callRequest struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type callResponse struct {
	Result string `json:"result"`
}

// --- handlers ---------------------------------------------------------------

// List — GET /api/cerebro/connection-tools
//
// Returns one descriptor per api-type connection endpoint the CALLING AGENT is
// allowed to call right now. When the flag is off or nothing is allowed, returns
// an empty list with 200 (not an error) — the MCP tool loop is additive and must
// never break because this workspace has not enabled the feature.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ident, ok := h.requireAgent(w, r)
	if !ok {
		return
	}

	allowed := h.allowedTools(r, ident)
	descs := make([]toolDescriptor, 0, len(allowed))
	for _, v := range allowed {
		descs = append(descs, toolDescriptor{
			Name:        v.Tool.Name(),
			Description: endpointDescription(v),
			InputSchema: v.Tool.InputSchema(),
		})
	}
	writeJSON(w, http.StatusOK, listResponse{Tools: descs})
}

// endpointDescription is the model-facing one-liner for a listed endpoint. An Ask
// endpoint is surfaced (not hidden) with a clear note that it needs approval and
// cannot be auto-dispatched on a local runtime — matching the FIR-2388 rule that
// only Deny is hidden.
func endpointDescription(v runtime.APIConnectionToolVerdict) string {
	desc := v.Tool.Description()
	if v.Verdict != toolpolicy.SettingAllow {
		desc += " Requires human approval — not available on a local runtime; run it on the cloud runtime."
	}
	return desc
}

// Call — POST /api/cerebro/connection-tools/call
//
// Re-lists the caller's allowed tools server-side, finds the one whose Name()
// matches the requested tool, RE-CHECKS the per-agent endpoint gate (the client
// is never trusted for routing/authz), then dispatches the call server-side and
// returns the response string. 403 if the tool does not exist or is not allowed;
// 400 on a bad body.
func (h *Handler) Call(w http.ResponseWriter, r *http.Request) {
	ident, ok := h.requireAgent(w, r)
	if !ok {
		return
	}

	var req callRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Tool) == "" {
		writeError(w, http.StatusBadRequest, "tool is required")
		return
	}

	// Re-resolve the allowed set; the gate is the only source of truth for which
	// tool may run. The set contains Allow AND Ask endpoints (Deny/unresolved are
	// dropped by the resolver), so a match must still be verdict-checked here.
	for _, v := range h.allowedTools(r, ident) {
		if v.Tool.Name() != req.Tool {
			continue
		}
		if v.Verdict != toolpolicy.SettingAllow {
			// Ask cannot be auto-approved on this path — there is no approval inbox
			// on the local MCP surface, and this fronts the secrets box. Fail closed
			// with a clear reason rather than dispatch an unapproved call.
			writeError(w, http.StatusForbidden, "tool requires human approval and cannot be dispatched on a local runtime")
			return
		}
		// FIR-2668: carry the server-resolved agent identity to dispatch so
		// on_behalf_of-enabled connections stamp the agent's delegated identity.
		callCtx := runtime.WithConnectionAgent(r.Context(), util.UUIDToString(ident.agentID))
		result, err := v.Tool.Call(callCtx, req.Arguments)
		if err != nil {
			// The endpoint call itself failed (upstream HTTP error, bad params,
			// timeout). Surface the redacted message Call already produced —
			// never a 200 with a hidden error. NOT 502: Cloudflare replaces an
			// origin's 502 body with its generic "error code: 502" page, so the
			// agent never sees the upstream message (FIR-2441). 424 states it
			// precisely — this request was fine, the upstream it depends on
			// failed — and passes through Cloudflare with the body intact.
			writeError(w, http.StatusFailedDependency, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, callResponse{Result: result})
		return
	}

	// Not found in the allowed set: either the tool does not exist for this
	// workspace, or the agent is not granted it. Both are a 403 (fail closed —
	// do not leak whether the endpoint exists).
	writeError(w, http.StatusForbidden, "tool not allowed")
}

// --- internals --------------------------------------------------------------

// agentIdentity is the authoritative, server-resolved identity of the calling
// agent (from the mat_ task token's server-set headers + the agent row).
type agentIdentity struct {
	workspaceID pgtype.UUID
	agentID     pgtype.UUID
	runtimeID   pgtype.UUID
	ownerID     pgtype.UUID
}

// requireAgent enforces that the caller is an agent (mat_ task token) and
// resolves its runtime + owner, cross-checking the token workspace against the
// agent row. Mirrors handler.AuthorizePersonalBrowser exactly. Fails closed.
func (h *Handler) requireAgent(w http.ResponseWriter, r *http.Request) (agentIdentity, bool) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "connection tools are agent-only")
		return agentIdentity{}, false
	}
	agentID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Agent-ID"), "agent_id")
	if !ok {
		return agentIdentity{}, false
	}
	wsID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Workspace-ID"), "workspace_id")
	if !ok {
		return agentIdentity{}, false
	}
	// Owner (user-ceiling layer). Present on a task token; tolerate absence and
	// fall back to the agent row's owner.
	var ownerID pgtype.UUID
	if uid := strings.TrimSpace(r.Header.Get("X-User-ID")); uid != "" {
		if parsed, err := util.ParseUUID(uid); err == nil {
			ownerID = parsed
		}
	}

	agentWsID, runtimeID, agentOwner, err := h.agents.ResolveAgent(r.Context(), agentID)
	if err != nil {
		slog.Warn("connection-tools: agent lookup failed — failing closed",
			"workspace_id", util.UUIDToString(wsID), "agent_id", util.UUIDToString(agentID), "error", err)
		writeError(w, http.StatusForbidden, "agent lookup failed")
		return agentIdentity{}, false
	}
	if agentWsID != wsID {
		writeError(w, http.StatusForbidden, "agent does not belong to this workspace")
		return agentIdentity{}, false
	}
	if !ownerID.Valid {
		ownerID = agentOwner
	}

	return agentIdentity{
		workspaceID: wsID,
		agentID:     agentID,
		runtimeID:   runtimeID,
		ownerID:     ownerID,
	}, true
}

// allowedTools resolves the api-type connection endpoint tools the calling agent
// may use, each with its Allow/Ask verdict, through the UNIFIED resolver's api
// half (Resolve(...).APITools). When the resolver is not wired, the flag is off,
// or there are no granted endpoints, it returns nil (the list endpoint must fail
// OPEN to an empty list, never break the caller's MCP tool loop). Deny and
// unresolved endpoints are already dropped by the resolver (fail closed); the
// caller verdict-checks Allow vs Ask before dispatch.
//
// InitiatorID is left unset: this local surface carries no separate on_behalf_of
// actor, so the user ceiling is simply the owner (ident.ownerID = the task
// token's X-User-ID, or the agent-row owner fallback) — byte-for-byte the
// identity the old ListForAgent(APIConnectionIdentity{OwnerID: ident.ownerID})
// used, so the swap is parity-preserving.
func (h *Handler) allowedTools(r *http.Request, ident agentIdentity) []runtime.APIConnectionToolVerdict {
	if h.resolver == nil {
		return nil
	}
	return h.resolver.Resolve(r.Context(), runtime.ConnectionIdentity{
		WorkspaceID: ident.workspaceID,
		RuntimeID:   ident.runtimeID,
		AgentID:     ident.agentID,
		OwnerID:     ident.ownerID,
	}).APITools
}

// --- helpers ----------------------------------------------------------------

func parseUUIDOrBadRequest(w http.ResponseWriter, s, field string) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+field)
		return pgtype.UUID{}, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
