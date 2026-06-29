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
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/runtime"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// flagAPIConnectionTools mirrors runtime.flagAPIConnectionTools — the workspace
// feature flag that exposes api-type connection endpoints as agent tools.
// Default OFF: a missing override resolves to false, so no workspace is affected
// until an admin turns it on.
const flagAPIConnectionTools = "cerebro_api_connection_tools"

// dispatchTimeout caps a single server-side endpoint dispatch.
const dispatchTimeout = 45 * time.Second

// AgentResolver resolves an agent's workspace + runtime + owner from its id.
// Satisfied by the QueriesAgentResolver adapter over *db.Queries.GetAgent; kept
// as a seam so the handler is unit-testable without a database.
type AgentResolver interface {
	ResolveAgent(ctx context.Context, agentID pgtype.UUID) (workspaceID, runtimeID, ownerID pgtype.UUID, err error)
}

// Handler exposes api-type connection endpoints to the Multica MCP server.
type Handler struct {
	pool   *pgxpool.Pool
	conns  *connections.Store
	policy *toolpolicy.Store
	flags  *cerebrodb.Queries
	agents AgentResolver
	client *http.Client
}

// NewHandler wires the handler from the shared pool, the cerebro queries (for the
// feature flag), and an agent resolver. The connection store and tool-policy
// store are both built over the pool — ConnectionEndpointEffective needs the pool
// (it reads the per-connection default and the policy table), so this uses
// toolpolicy.NewStore, NOT NewStoreFromQueries.
func NewHandler(pool *pgxpool.Pool, cerebroQueries *cerebrodb.Queries, agents AgentResolver) *Handler {
	return &Handler{
		pool:   pool,
		conns:  connections.New(pool),
		policy: toolpolicy.NewStore(pool),
		flags:  cerebroQueries,
		agents: agents,
		client: &http.Client{Timeout: dispatchTimeout},
	}
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
	for _, t := range allowed {
		descs = append(descs, toolDescriptor{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	writeJSON(w, http.StatusOK, listResponse{Tools: descs})
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
	// tool may run. allowedTools already filters to SettingAllow, so a tool found
	// in this set is, by construction, an allowed endpoint for this agent.
	for _, t := range h.allowedTools(r, ident) {
		if t.Name() != req.Tool {
			continue
		}
		result, err := t.Call(r.Context(), req.Arguments)
		if err != nil {
			// The endpoint call itself failed (upstream HTTP error, bad params,
			// timeout). Surface as a 502 with the redacted message Call already
			// produced — never a 200 with a hidden error.
			writeError(w, http.StatusBadGateway, err.Error())
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

// allowedTools builds every api-type connection endpoint tool for the workspace
// and keeps only those the calling agent is granted (SettingAllow). When the
// feature flag is off, or there are no enabled connections, it returns an empty
// slice. Any discovery error is logged and swallowed — the list endpoint must
// fail OPEN to an empty list, never break the caller's MCP tool loop.
func (h *Handler) allowedTools(r *http.Request, ident agentIdentity) []*runtime.APIConnectionTool {
	if !h.flagEnabled(r, ident.workspaceID) {
		return nil
	}
	conns, err := h.conns.ListEnabled(r.Context(), ident.workspaceID)
	if err != nil {
		slog.Warn("connection-tools: list enabled connections failed",
			"workspace_id", util.UUIDToString(ident.workspaceID), "error", err)
		return nil
	}
	built := runtime.BuildAPIConnectionTools(conns, h.client)

	var out []*runtime.APIConnectionTool
	for _, t := range built {
		api, ok := t.(*runtime.APIConnectionTool)
		if !ok {
			continue
		}
		setting, _, err := h.policy.ConnectionEndpointEffective(
			r.Context(),
			ident.workspaceID, ident.runtimeID, ident.agentID, ident.ownerID,
			api.ConnectionName(), api.Method(), api.Path(),
		)
		if err != nil {
			// Fail closed on this endpoint — a resolve error must not expose a
			// gate that fronts the secrets box.
			slog.Warn("connection-tools: endpoint gate resolve failed — skipping",
				"workspace_id", util.UUIDToString(ident.workspaceID),
				"agent_id", util.UUIDToString(ident.agentID),
				"connection", api.ConnectionName(), "method", api.Method(), "path", api.Path(),
				"error", err)
			continue
		}
		if setting == toolpolicy.SettingAllow {
			out = append(out, api)
		}
	}
	return out
}

// flagEnabled reports whether cerebro_api_connection_tools is on for the
// workspace. Default OFF: a missing override or a DB error resolves to false.
func (h *Handler) flagEnabled(r *http.Request, workspaceID pgtype.UUID) bool {
	if h.flags == nil {
		return false
	}
	enabled, err := h.flags.GetCerebroFeatureFlag(r.Context(), cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     flagAPIConnectionTools,
	})
	if err != nil {
		return false
	}
	return enabled
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
