package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WorkspaceMcpServerResponse is the intentionally non-secret shape of one
// workspace MCP server. Never add `url`, `command`, `args`, `headers`, or
// `env`: upstream #6062 asks for shared secrets to be write-only, and every
// one of those fields routinely carries a token (a Composio-style session URL
// is a bearer credential on its own). Mirrors the daemon's own
// runtimeLocalMcpServerSummary, which draws the same line for the same reason.
//
// `Enabled` is only meaningful on the agent-scoped list, where it reflects the
// binding's toggle; it is omitted on the workspace library listing.
type WorkspaceMcpServerResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Transport   string `json:"transport"`
	Enabled     *bool  `json:"enabled,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// mcpTransportOf classifies a server entry for display, and — because the
// client uses it to decide whether the guided form may edit an entry — it must
// not be lossy about an explicit `type`.
//
// An unrecognised explicit type is returned VERBATIM rather than inferred from
// the presence of a `url`. Reporting `{"type":"websocket","url":"wss://…"}` as
// "http" would tell the settings UI the form can express it, and saving from
// that form rewrites the entry to `type: "http"` — silently changing the
// protocol. Only an entry with NO explicit type is inferred from command / url,
// which is lossless because the form writes that same shape back.
func mcpTransportOf(entry json.RawMessage) string {
	var e struct {
		Type    string          `json:"type"`
		Command json.RawMessage `json:"command"`
		URL     json.RawMessage `json:"url"`
	}
	if err := json.Unmarshal(entry, &e); err != nil {
		return "unknown"
	}
	if declared := strings.ToLower(strings.TrimSpace(e.Type)); declared != "" {
		switch declared {
		case "local", "stdio":
			return "stdio"
		case "remote", "http", "streamable-http":
			return "http"
		}
		// `sse` and anything a newer client invents land here unchanged. The
		// response schema keeps transport a free string for exactly this.
		return declared
	}
	if len(e.Command) > 0 {
		return "stdio"
	}
	if len(e.URL) > 0 {
		return "http"
	}
	return "unknown"
}

func workspaceMcpServerToResponse(server db.WorkspaceMcpServer) WorkspaceMcpServerResponse {
	return WorkspaceMcpServerResponse{
		ID:          uuidToString(server.ID),
		WorkspaceID: uuidToString(server.WorkspaceID),
		Name:        server.Name,
		Transport:   mcpTransportOf(server.Config),
		CreatedAt:   timestampToString(server.CreatedAt),
		UpdatedAt:   timestampToString(server.UpdatedAt),
	}
}

// ListWorkspaceMcpServers returns the workspace's MCP library. Member-visible:
// the payload carries no credential material, and an agent owner needs to see
// what is available to add.
func (h *Handler) ListWorkspaceMcpServers(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	servers, err := h.Queries.ListWorkspaceMcpServers(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace MCP servers")
		return
	}
	resp := make([]WorkspaceMcpServerResponse, 0, len(servers))
	for _, server := range servers {
		resp = append(resp, workspaceMcpServerToResponse(server))
	}
	writeJSON(w, http.StatusOK, resp)
}

// requireWorkspaceMcpWriter enforces the write gate. The router already
// restricts these routes to owner/admin, but an agent actor running under an
// owner's PAT would satisfy that gate: adding a shared MCP server has to stay a
// human admin action.
func (h *Handler) requireWorkspaceMcpWriter(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot modify the workspace MCP servers")
		return false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return false
	}
	return true
}

// WorkspaceMcpServerRequest carries one server entry. `config` is the object
// that sits under a name in `mcpServers`; it is write-only and never returned.
type WorkspaceMcpServerRequest struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

// CreateWorkspaceMcpServer adds a server to the workspace library. It is bound
// to NO agent — the library entry has to be added to an agent before it does
// anything, which is the whole shape of this feature.
func (h *Handler) CreateWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return
	}

	var req WorkspaceMcpServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateWorkspaceMcpServerName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateWorkspaceMcpServerEntry(req.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the MCP server")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Join the workspace teardown fence (#5219): this table has no FK, so
	// without the shared lock a create committing after DeleteWorkspace swept
	// would leave a row pointing at a workspace that no longer exists.
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), idUUID); err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	server, err := qtx.CreateWorkspaceMcpServer(r.Context(), db.CreateWorkspaceMcpServerParams{
		WorkspaceID: idUUID,
		Name:        name,
		Config:      append([]byte(nil), req.Config...),
		CreatedBy:   parseUUID(requestUserID(r)),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an MCP server with this name already exists in the workspace")
			return
		}
		slog.Warn("create workspace mcp server failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create the MCP server")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the MCP server")
		return
	}
	// Name only — never the entry — so the audit trail cannot become a second
	// copy of the workspace's credentials.
	slog.Info("workspace mcp server created", append(logger.RequestAttrs(r),
		"workspace_id", workspaceID, "server_id", uuidToString(server.ID), "name", server.Name)...)
	writeJSON(w, http.StatusCreated, workspaceMcpServerToResponse(server))
}

// UpdateWorkspaceMcpServer replaces one library entry. Renaming is safe here:
// bindings key off the id, so an agent that uses this server keeps using it.
func (h *Handler) UpdateWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}

	var req WorkspaceMcpServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.UpdateWorkspaceMcpServerParams{ID: serverUUID, WorkspaceID: idUUID}
	if name := strings.TrimSpace(req.Name); name != "" {
		if err := validateWorkspaceMcpServerName(name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if len(req.Config) > 0 {
		if err := validateWorkspaceMcpServerEntry(req.Config); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Config = append([]byte(nil), req.Config...)
	}

	server, err := h.Queries.UpdateWorkspaceMcpServer(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an MCP server with this name already exists in the workspace")
			return
		}
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	slog.Info("workspace mcp server updated", append(logger.RequestAttrs(r),
		"workspace_id", workspaceID, "server_id", uuidToString(server.ID), "name", server.Name)...)
	writeJSON(w, http.StatusOK, workspaceMcpServerToResponse(server))
}

// DeleteWorkspaceMcpServer removes a library entry and every binding to it, in
// one transaction — the binding table carries no FK, so an orphaned row would
// otherwise keep pointing at a server that no longer exists.
func (h *Handler) DeleteWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Exclusive row lock first: an assignment writer holds this row FOR SHARE
	// while it inserts, so taking it here means a concurrent add either lands
	// before this transaction (and is swept below) or waits and then finds the
	// server gone. Without it the add could insert after the sweep, leaving a
	// binding to a server that no longer exists.
	if _, err := qtx.LockWorkspaceMcpServerForUpdate(r.Context(), db.LockWorkspaceMcpServerForUpdateParams{
		ID:          serverUUID,
		WorkspaceID: idUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}

	rows, err := qtx.DeleteWorkspaceMcpServer(r.Context(), db.DeleteWorkspaceMcpServerParams{
		ID:          serverUUID,
		WorkspaceID: idUUID,
	})
	if err != nil {
		slog.Warn("delete workspace mcp server failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if err := qtx.DeleteAgentMcpServersByServer(r.Context(), serverUUID); err != nil {
		slog.Warn("sweep agent mcp bindings failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	slog.Info("workspace mcp server deleted", append(logger.RequestAttrs(r),
		"workspace_id", workspaceID, "server_id", uuidToString(serverUUID))...)
	w.WriteHeader(http.StatusNoContent)
}

// ListAgentMcpServers returns the workspace servers bound to this agent, with
// each binding's toggle state.
func (h *Handler) ListAgentMcpServers(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListAgentMcpServers(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list the agent's MCP servers")
		return
	}
	resp := make([]WorkspaceMcpServerResponse, 0, len(rows))
	for _, row := range rows {
		enabled := row.Enabled
		resp = append(resp, WorkspaceMcpServerResponse{
			ID:          uuidToString(row.ID),
			WorkspaceID: uuidToString(row.WorkspaceID),
			Name:        row.Name,
			Transport:   mcpTransportOf(row.Config),
			Enabled:     &enabled,
			CreatedAt:   timestampToString(row.CreatedAt),
			UpdatedAt:   timestampToString(row.UpdatedAt),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// requireAgentMcpWriter resolves the agent and enforces who may change its
// bindings. Same rule as the agent's own mcp_config: the agent owner or a
// workspace owner/admin, never an agent actor.
func (h *Handler) requireAgentMcpWriter(w http.ResponseWriter, r *http.Request) (db.Agent, bool) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Agent{}, false
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot modify MCP server assignments")
		return db.Agent{}, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Agent{}, false
	}
	if !canViewAgentSecrets(agent, requestUserID(r), member.Role) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Agent{}, false
	}
	return agent, true
}

// AddAgentMcpServerRequest names the library entry to attach.
type AddAgentMcpServerRequest struct {
	ServerID string `json:"server_id"`
}

// AddAgentMcpServer gives one workspace MCP server to this agent. This is the
// only way a library entry reaches an agent.
func (h *Handler) AddAgentMcpServer(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.requireAgentMcpWriter(w, r)
	if !ok {
		return
	}
	var req AddAgentMcpServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.ServerID), "server_id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add the MCP server")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Same fence as every other workspace-scoped insert (#5219): the binding
	// has no FK, so a write committing after DeleteWorkspace swept would
	// outlive the agent and the server it names.
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	// Scope check AND the delete protocol in one statement: the row must
	// belong to this agent's workspace — an id from another workspace would
	// bind across the tenant boundary — and holding it FOR SHARE serializes
	// this insert against DeleteWorkspaceMcpServer's sweep.
	if _, err := qtx.LockWorkspaceMcpServerForShare(r.Context(), db.LockWorkspaceMcpServerForShareParams{
		ID:          serverUUID,
		WorkspaceID: agent.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "MCP server not found in this workspace")
		return
	}
	if err := qtx.AddAgentMcpServer(r.Context(), db.AddAgentMcpServerParams{
		AgentID:  agent.ID,
		ServerID: serverUUID,
	}); err != nil {
		slog.Warn("add agent mcp server failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to add the MCP server")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add the MCP server")
		return
	}
	slog.Info("agent mcp server added", append(logger.RequestAttrs(r),
		"agent_id", uuidToString(agent.ID), "server_id", uuidToString(serverUUID))...)
	h.writeAgentMcpServers(w, r, agent.ID)
}

// SetAgentMcpServerEnabledRequest carries the per-agent toggle.
type SetAgentMcpServerEnabledRequest struct {
	Enabled *bool `json:"enabled"`
}

// SetAgentMcpServerEnabled flips one binding's toggle without dropping it, so
// turning a server back on does not mean finding it again.
func (h *Handler) SetAgentMcpServerEnabled(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.requireAgentMcpWriter(w, r)
	if !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}
	var req SetAgentMcpServerEnabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	rows, err := h.Queries.SetAgentMcpServerEnabled(r.Context(), db.SetAgentMcpServerEnabledParams{
		AgentID:  agent.ID,
		ServerID: serverUUID,
		Enabled:  *req.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "this MCP server is not assigned to the agent")
		return
	}
	slog.Info("agent mcp server toggled", append(logger.RequestAttrs(r),
		"agent_id", uuidToString(agent.ID), "server_id", uuidToString(serverUUID), "enabled", *req.Enabled)...)
	h.writeAgentMcpServers(w, r, agent.ID)
}

// RemoveAgentMcpServer takes a workspace server away from this agent. The
// library entry itself is untouched.
func (h *Handler) RemoveAgentMcpServer(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.requireAgentMcpWriter(w, r)
	if !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}
	rows, err := h.Queries.RemoveAgentMcpServer(r.Context(), db.RemoveAgentMcpServerParams{
		AgentID:  agent.ID,
		ServerID: serverUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove the MCP server")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "this MCP server is not assigned to the agent")
		return
	}
	slog.Info("agent mcp server removed", append(logger.RequestAttrs(r),
		"agent_id", uuidToString(agent.ID), "server_id", uuidToString(serverUUID))...)
	h.writeAgentMcpServers(w, r, agent.ID)
}

// writeAgentMcpServers returns the agent's binding list after a mutation, so
// the client never has to guess the resulting state.
func (h *Handler) writeAgentMcpServers(w http.ResponseWriter, r *http.Request, agentID pgtype.UUID) {
	rows, err := h.Queries.ListAgentMcpServers(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list the agent's MCP servers")
		return
	}
	resp := make([]WorkspaceMcpServerResponse, 0, len(rows))
	for _, row := range rows {
		enabled := row.Enabled
		resp = append(resp, WorkspaceMcpServerResponse{
			ID:          uuidToString(row.ID),
			WorkspaceID: uuidToString(row.WorkspaceID),
			Name:        row.Name,
			Transport:   mcpTransportOf(row.Config),
			Enabled:     &enabled,
			CreatedAt:   timestampToString(row.CreatedAt),
			UpdatedAt:   timestampToString(row.UpdatedAt),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
