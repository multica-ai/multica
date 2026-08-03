package handler

// CEREBRO-PATCH(runtime-tools-admin-handler): JEH-1710 — unified runtime tool
// inventory API. Access authoring is owned by the canonical tool-policy API.
//
// Endpoints:
//
//   GET    /api/runtimes/{runtimeId}/tools                       — list tools
//
// Auth: workspace owner/admin only — same threat model as the runtime
// tools_config endpoint. Mutating runtime tool grants effectively governs
// what every agent on the runtime can do.

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/accessdiagnostics"
)

// RuntimeToolsAdminService is the seam the cerebro runtimetools service
// implements. The handler package never imports the cerebrodb generated
// package directly — that would create an import cycle.
type RuntimeToolsAdminService interface {
	ListTools(ctx context.Context, runtimeID pgtype.UUID) ([]RuntimeToolView, error)
	// CEREBRO-PATCH(runtime-tools-scan-now-local-stamp): FIR-2284 stamp last_scanned_at on a local scan.
	StampScanned(ctx context.Context, runtimeID pgtype.UUID) error
}

// CEREBRO-PATCH(runtime-agnostic-tool-access): TECH-3071 resolves the read-only
// effective access preview for a runtime's tool inventory. It does not mutate
// grants or gate execution.
type RuntimeToolAccessService interface {
	ListEffectiveTools(ctx context.Context, q RuntimeToolAccessQuery) ([]RuntimeToolEffectiveAccessView, error)
}

type RuntimeToolAccessQuery struct {
	WorkspaceID         pgtype.UUID
	RuntimeID           pgtype.UUID
	RuntimeMode         string
	RuntimeProvider     string
	RuntimeCapabilities []byte
	AgentID             pgtype.UUID
	UserID              pgtype.UUID
	// OnBehalfOfID is the delegated member (task initiator) the work is performed
	// for, resolved as the tighten-only on_behalf_of policy layer distinct from
	// UserID (the agent owner) (FIR-2441). Zero when there is no delegation.
	OnBehalfOfID pgtype.UUID
}

func (h *Handler) SetRuntimeToolAccess(svc RuntimeToolAccessService) {
	h.runtimeToolAccess = svc
}

// RuntimeToolView is the wire shape returned by the admin tool endpoints.
// Mirrors runtimetools.Tool but only exposes fields the UI needs and uses
// plain types (no pgtype.X) for stable JSON.
type RuntimeToolView struct {
	ID            string  `json:"id"`
	RuntimeID     string  `json:"runtime_id"`
	Name          string  `json:"name"`
	Source        string  `json:"source"`
	MCPServerName string  `json:"mcp_server_name,omitempty"`
	Description   string  `json:"description,omitempty"`
	Enabled       bool    `json:"enabled"`
	LastScannedAt *string `json:"last_scanned_at,omitempty"`
}

type RuntimeToolEffectiveAccessView struct {
	Descriptor        RuntimeToolDescriptorView        `json:"descriptor"`
	Inventory         RuntimeToolInventoryStateView    `json:"inventory"`
	Policy            RuntimeToolPolicyStateView       `json:"policy"`
	Protocol          RuntimeToolProtocolStateView     `json:"protocol"`
	Credential        RuntimeToolCredentialStateView   `json:"credential"`
	ExposureEffective RuntimeToolExposureEffectiveView `json:"exposure_effective"`
	Layers            map[string]string                `json:"layers,omitempty"`
}

type RuntimeToolDescriptorView struct {
	ToolKey                  string   `json:"tool_key"`
	DisplayName              string   `json:"display_name"`
	Description              string   `json:"description,omitempty"`
	Source                   string   `json:"source"`
	RiskClass                string   `json:"risk_class"`
	Protocols                []string `json:"protocols"`
	RecommendedDefaultPolicy string   `json:"recommended_default_policy"`
}

type RuntimeToolInventoryStateView struct {
	RuntimeID     string `json:"runtime_id"`
	ToolName      string `json:"tool_name"`
	Source        string `json:"source"`
	MCPServerName string `json:"mcp_server_name,omitempty"`
	Enabled       bool   `json:"enabled"`
}

type RuntimeToolPolicyStateView struct {
	Effective string `json:"effective"`
	Reason    string `json:"reason"`
	DecidedBy string `json:"decided_by,omitempty"`
	CappedBy  string `json:"capped_by,omitempty"`
}

type RuntimeToolProtocolStateView struct {
	Effective          string   `json:"effective"`
	RequiredProtocols  []string `json:"required_protocols"`
	RuntimeProtocols   []string `json:"runtime_protocols"`
	SelectedProtocol   string   `json:"selected_protocol,omitempty"`
	SupportsAsk        bool     `json:"supports_ask"`
	UnsupportedMessage string   `json:"unsupported_message,omitempty"`
}

type RuntimeToolCredentialStateView struct {
	Effective string `json:"effective"`
	Reason    string `json:"reason"`
}

type RuntimeToolExposureEffectiveView struct {
	Effective bool   `json:"effective"`
	Reason    string `json:"reason"`
}

// SetRuntimeToolsAdmin wires the service into the handler. Called after
// handler.New() so the upstream constructor signature stays clean.
func (h *Handler) SetRuntimeToolsAdmin(svc RuntimeToolsAdminService) {
	h.runtimeToolsAdmin = svc
}

// CloudRuntimeToolScanner runs an immediate, server-side tool inventory scan for
// cloud (firtal-gateway) runtimes. Those runtimes have no daemon connected to
// the daemon websocket, so the daemon-push "Scan now" path returns 502; this
// seam lets the handler enumerate their built-in tool surface in-process
// instead. Wired in cmd/server (FIR-2284).
type CloudRuntimeToolScanner interface {
	Scan(ctx context.Context, runtimeID, workspaceID pgtype.UUID) error
}

// SetCloudRuntimeToolScanner wires the cloud-runtime scanner into the handler.
func (h *Handler) SetCloudRuntimeToolScanner(s CloudRuntimeToolScanner) {
	h.cloudRuntimeToolScanner = s
}

// loadRuntimeForAdmin is the auth gate for the runtime-tool admin endpoints.
// Resolves the runtime, then requires the caller to be an owner/admin in the
// runtime's workspace.
func (h *Handler) loadRuntimeForAdmin(w http.ResponseWriter, r *http.Request) (pgtype.UUID, string, bool) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return pgtype.UUID{}, "", false
	}
	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found")
	if !ok {
		return pgtype.UUID{}, "", false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can manage runtime tools")
		return pgtype.UUID{}, "", false
	}
	return rt.ID, uuidToString(member.UserID), true
}

// requireToolsAdmin returns true if the seam has been wired. Lets handlers
// 503 cleanly when cerebro is feature-flagged off.
func (h *Handler) requireToolsAdmin(w http.ResponseWriter) bool {
	if h.runtimeToolsAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime tools admin not enabled")
		return false
	}
	return true
}

func (h *Handler) requireRuntimeToolAccess(w http.ResponseWriter) bool {
	if h.runtimeToolAccess == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime tool access preview not enabled")
		return false
	}
	return true
}

// ListRuntimeTools handles GET /api/runtimes/{runtimeId}/tools.
func (h *Handler) ListRuntimeTools(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	rtID, _, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	tools, err := h.runtimeToolsAdmin.ListTools(r.Context(), rtID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list tools: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tools)
}

// CEREBRO-PATCH(runtime-agnostic-tool-access): TECH-3071
// ListRuntimeToolEffectiveAccess handles GET /api/runtimes/{runtimeId}/tools/effective.
// It returns the server-computed read-only access preview for the runtime's tool
// inventory. Query params:
//
//	agent_id optional — include the agent layer and default user_id to the owner.
//	user_id  optional — include the member/user ceiling and runtime grant result.
func (h *Handler) ListRuntimeToolEffectiveAccess(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) || !h.requireRuntimeToolAccess(w) {
		return
	}
	rtID, _, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), rtID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	var agentID pgtype.UUID
	var userID pgtype.UUID
	if raw := r.URL.Query().Get("agent_id"); raw != "" {
		agentID = parseUUID(raw)
		agent, err := h.Queries.GetAgent(r.Context(), agentID)
		if err != nil || !agent.WorkspaceID.Valid || agent.WorkspaceID != rt.WorkspaceID {
			writeError(w, http.StatusBadRequest, "agent does not belong to this workspace")
			return
		}
		if agent.RuntimeID.Valid && agent.RuntimeID != rt.ID {
			writeError(w, http.StatusBadRequest, "agent is not assigned to this runtime")
			return
		}
		userID = agent.OwnerID
	}
	if raw := r.URL.Query().Get("user_id"); raw != "" {
		userID = parseUUID(raw)
	}

	rows, err := h.runtimeToolAccess.ListEffectiveTools(r.Context(), RuntimeToolAccessQuery{
		WorkspaceID:         rt.WorkspaceID,
		RuntimeID:           rt.ID,
		RuntimeMode:         rt.RuntimeMode,
		RuntimeProvider:     rt.Provider,
		RuntimeCapabilities: marshalRuntimeCapabilities(normalizedRuntimeCapabilities(rt.Provider, rt.Capabilities, rt.ToolsConfig)),
		AgentID:             agentID,
		UserID:              userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list effective tool access: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// GetRuntimeAccessDiagnostics handles GET /api/runtimes/{runtimeId}/access-diagnostics.
// It projects provider-probe and MCP tools/list evidence through one read-only
// REST contract used by the app, CLI and MCP. It never authors or enforces access.
func (h *Handler) GetRuntimeAccessDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	rtID, _, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), rtID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	tools, err := h.runtimeToolsAdmin.ListTools(r.Context(), rtID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list runtime access diagnostics: "+err.Error())
		return
	}
	evidence := make([]accessdiagnostics.RuntimeToolEvidence, 0, len(tools))
	for _, tool := range tools {
		row := accessdiagnostics.RuntimeToolEvidence{
			Name:          tool.Name,
			Source:        tool.Source,
			MCPServerName: tool.MCPServerName,
		}
		if tool.LastScannedAt != nil {
			row.LastScannedAt, _ = time.Parse(time.RFC3339Nano, *tool.LastScannedAt)
		}
		evidence = append(evidence, row)
	}
	writeJSON(w, http.StatusOK, accessdiagnostics.BuildRuntimeDiagnostics(accessdiagnostics.RuntimeInput{
		RuntimeID:          uuidToString(rt.ID),
		Provider:           rt.Provider,
		Status:             rt.Status,
		Capabilities:       normalizedRuntimeCapabilities(rt.Provider, rt.Capabilities, rt.ToolsConfig),
		ProviderObservedAt: rt.UpdatedAt.Time,
		Tools:              evidence,
		Now:                time.Now(),
		StaleAfter:         30 * time.Minute,
	}))
}

// CEREBRO-PATCH(runtime-tools-scan-now): FIR-2230 admin-triggered live scan endpoint.
// RequestRuntimeToolScan handles POST /api/.../runtimes/{runtimeId}/tools/scan-now.
// Pushes an immediate MCP tools/list scan request to the runtime's daemon over
// the daemon websocket (FIR-2230), instead of waiting for the next scheduled
// heartbeat scan. The scan itself is async — the daemon reports results back
// through the existing ingest endpoint, so the client refetches the inventory
// shortly after a 202. Returns 502 when no daemon is connected for the runtime,
// so the UI can tell the admin the runtime is offline rather than silently
// dropping the request.
func (h *Handler) RequestRuntimeToolScan(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	rtID, _, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), rtID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	// CEREBRO-PATCH(runtime-tools-scan-now-cloud): FIR-2284 — cloud (firtal-gateway)
	// runtimes run server-side and never connect to the daemon websocket, so the
	// daemon-push path below always 502s for them. Scan them in-process instead.
	if rt.RuntimeMode == "cloud" || rt.Provider == "firtal-gateway" {
		if h.cloudRuntimeToolScanner == nil {
			writeError(w, http.StatusServiceUnavailable, "cloud runtime scanner not enabled")
			return
		}
		if err := h.cloudRuntimeToolScanner.Scan(r.Context(), rt.ID, rt.WorkspaceID); err != nil {
			writeError(w, http.StatusInternalServerError, "cloud runtime scan: "+err.Error())
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if h.DaemonHub == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon websocket not available")
		return
	}
	if h.DaemonHub.RuntimeConnectionCount(uuidToString(rtID)) == 0 {
		writeError(w, http.StatusBadGateway, "runtime daemon is offline")
		return
	}
	h.DaemonHub.RequestToolScan(uuidToString(rtID))
	// CEREBRO-PATCH(runtime-tools-scan-now-snapshot-mirror): FIR-2284 — the daemon
	// push above only scans external MCP servers (tools_config.mcpServers); a
	// local daemon with none reports nothing, so "Scan now" looked like it did
	// nothing. Mirror the runtime's stored built-in snapshot into the register
	// now so the runtime's own tools surface in the unified table immediately;
	// any MCP tools the async scan finds are added on top when it reports back.
	h.persistRuntimeCapabilitySnapshot(r, rt.ID, rt.WorkspaceID, rt.Capabilities)
	// CEREBRO-PATCH(runtime-tools-scan-now-local-stamp): FIR-2284 — the daemon
	// only stamps last_scanned_at on the MCP rows it reports; a local runtime
	// with no MCP servers never updated the "Last scanned" label. Stamp the
	// runtime's existing inventory now so the label flips, mirroring cloud.
	_ = h.runtimeToolsAdmin.StampScanned(r.Context(), rt.ID)
	w.WriteHeader(http.StatusAccepted)
}
