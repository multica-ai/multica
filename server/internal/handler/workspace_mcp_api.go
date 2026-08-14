package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WorkspaceMcpServerSummary is the intentionally non-secret inventory of one
// shared server. Never add `url`, `command`, `args`, `headers`, or `env` here:
// upstream #6062 asks for shared secrets to be write-only, and every one of
// those fields routinely carries a token (a Composio-style session URL is a
// bearer credential on its own). Mirrors the daemon's own
// runtimeLocalMcpServerSummary, which draws the same line for the same reason.
type WorkspaceMcpServerSummary struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Enabled   bool   `json:"enabled"`
}

// WorkspaceMcpConfigResponse is the read shape of the shared MCP
// configuration. There is deliberately NO document field and no
// `_redacted` flag: the stored document is never echoed back to anyone,
// regardless of role, so the topology an admin needs for auditing is not
// coupled to reading the credentials back out.
//
// Editing therefore goes through the per-server upsert/delete endpoints
// rather than a client-side read-modify-write.
type WorkspaceMcpConfigResponse struct {
	WorkspaceID string                      `json:"workspace_id"`
	Servers     []WorkspaceMcpServerSummary `json:"servers"`
	ServerCount int                         `json:"server_count"`
}

// mcpTransportOf classifies a server entry for display. Mirrors the
// front-end's mcpTransport (mcp-config-model.ts) so the same entry is
// labelled identically wherever it is listed.
func mcpTransportOf(entry json.RawMessage) string {
	var e struct {
		Type    string          `json:"type"`
		Command json.RawMessage `json:"command"`
		URL     json.RawMessage `json:"url"`
	}
	if err := json.Unmarshal(entry, &e); err != nil {
		return "unknown"
	}
	switch strings.ToLower(strings.TrimSpace(e.Type)) {
	case "local", "stdio":
		return "stdio"
	case "sse":
		return "sse"
	case "remote", "http", "streamable-http":
		return "http"
	}
	if len(e.Command) > 0 {
		return "stdio"
	}
	if len(e.URL) > 0 {
		return "http"
	}
	return "unknown"
}

// mcpServerEnabled reports whether an entry is active. Matches the front-end
// rule: enabled unless `enabled:false` or `disabled:true`.
func mcpServerEnabled(entry json.RawMessage) bool {
	var e struct {
		Enabled  *bool `json:"enabled"`
		Disabled *bool `json:"disabled"`
	}
	if err := json.Unmarshal(entry, &e); err != nil {
		return true
	}
	if e.Enabled != nil && !*e.Enabled {
		return false
	}
	if e.Disabled != nil && *e.Disabled {
		return false
	}
	return true
}

// buildWorkspaceMcpConfigResponse summarizes the stored document. A malformed
// document yields an empty inventory rather than an error: the read path is
// used by settings screens that must still render, and the resolver already
// fails soft on the same input.
func buildWorkspaceMcpConfigResponse(ws db.Workspace) WorkspaceMcpConfigResponse {
	resp := WorkspaceMcpConfigResponse{
		WorkspaceID: uuidToString(ws.ID),
		Servers:     []WorkspaceMcpServerSummary{},
	}
	raw := json.RawMessage(ws.McpConfig)
	if !hasManagedJSON(raw) {
		return resp
	}
	doc, _, err := parseMcpDocument(raw)
	if err != nil {
		return resp
	}
	servers, err := unmarshalServerMap(doc["mcpServers"])
	if err != nil {
		return resp
	}
	for name, entry := range servers {
		resp.Servers = append(resp.Servers, WorkspaceMcpServerSummary{
			Name:      name,
			Transport: mcpTransportOf(entry),
			Enabled:   mcpServerEnabled(entry),
		})
	}
	sort.Slice(resp.Servers, func(i, j int) bool { return resp.Servers[i].Name < resp.Servers[j].Name })
	resp.ServerCount = len(resp.Servers)
	return resp
}

// GetWorkspaceMcpConfig returns the non-secret inventory of shared servers.
// Member-visible: the agent settings view shows what an agent inherits, and
// there is nothing secret in the payload to gate.
func (h *Handler) GetWorkspaceMcpConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, buildWorkspaceMcpConfigResponse(ws))
}

// requireWorkspaceMcpWriter resolves the caller and enforces the write gate.
// The router already restricts these routes to owner/admin, but an agent actor
// running under an owner's PAT would satisfy that gate: handing every agent in
// the workspace a new MCP server has to stay a human admin action.
func (h *Handler) requireWorkspaceMcpWriter(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot modify the workspace MCP configuration")
		return false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return false
	}
	return true
}

// UpdateWorkspaceMcpConfigRequest carries the whole shared document. The field
// is a raw message so an absent key (nothing to do) stays distinguishable from
// an explicit `null` (clear the workspace layer) — the same three-state
// contract the agent column uses.
type UpdateWorkspaceMcpConfigRequest struct {
	McpConfig json.RawMessage `json:"mcp_config"`
}

// UpdateWorkspaceMcpConfig replaces the whole shared document. This is the
// declarative path (`multica workspace mcp set`) for callers that keep the
// document in a file; the UI edits one server at a time through the routes
// below, because it cannot read the current document back.
func (h *Handler) UpdateWorkspaceMcpConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return
	}

	var req UpdateWorkspaceMcpConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(bytes.TrimSpace(req.McpConfig)) == 0 {
		writeError(w, http.StatusBadRequest, "mcp_config is required; pass null to clear the workspace configuration")
		return
	}

	var (
		ws  db.Workspace
		err error
	)
	if bytes.Equal(bytes.TrimSpace(req.McpConfig), []byte("null")) {
		ws, err = h.Queries.ClearWorkspaceMcpConfig(r.Context(), idUUID)
	} else {
		if verr := validateWorkspaceMcpConfig(req.McpConfig); verr != nil {
			writeError(w, http.StatusBadRequest, verr.Error())
			return
		}
		ws, err = h.Queries.SetWorkspaceMcpConfig(r.Context(), db.SetWorkspaceMcpConfigParams{
			ID:        idUUID,
			McpConfig: append([]byte(nil), req.McpConfig...),
		})
	}
	if err != nil {
		slog.Warn("update workspace mcp_config failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace mcp_config")
		return
	}

	h.writeWorkspaceMcpConfigResult(w, r, ws, "replaced")
}

// UpsertWorkspaceMcpServer adds or replaces ONE shared server by name. This is
// what the settings UI uses: since the stored document is never echoed back,
// the client cannot do a read-modify-write, so the merge happens server-side
// under a row lock.
func (h *Handler) UpsertWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "serverName"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "server name is required")
		return
	}

	var entry json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Validate the entry as a one-server document so the shape rules (and the
	// no-input-echo error contract) stay in one place.
	probe, err := json.Marshal(map[string]any{"mcpServers": map[string]json.RawMessage{name: entry}})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate server entry")
		return
	}
	if verr := validateWorkspaceMcpConfig(probe); verr != nil {
		writeError(w, http.StatusBadRequest, verr.Error())
		return
	}

	ws, ok := h.mutateWorkspaceMcpServers(w, r, idUUID, workspaceID, func(servers map[string]json.RawMessage) error {
		servers[name] = entry
		return nil
	})
	if !ok {
		return
	}
	h.writeWorkspaceMcpConfigResult(w, r, ws, "server_upserted")
}

// DeleteWorkspaceMcpServer removes ONE shared server by name. Removing the
// last one clears the column, so agents fall back to their own configuration
// rather than inheriting an empty managed document.
func (h *Handler) DeleteWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "serverName"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "server name is required")
		return
	}

	ws, ok := h.mutateWorkspaceMcpServers(w, r, idUUID, workspaceID, func(servers map[string]json.RawMessage) error {
		if _, exists := servers[name]; !exists {
			return errWorkspaceMcpServerNotFound
		}
		delete(servers, name)
		return nil
	})
	if !ok {
		return
	}
	h.writeWorkspaceMcpConfigResult(w, r, ws, "server_deleted")
}

// errWorkspaceMcpServerNotFound turns a mutate callback into a 404 without the
// callback needing the ResponseWriter.
var errWorkspaceMcpServerNotFound = &workspaceMcpError{status: http.StatusNotFound, message: "shared MCP server not found"}

type workspaceMcpError struct {
	status  int
	message string
}

func (e *workspaceMcpError) Error() string { return e.message }

// mutateWorkspaceMcpServers applies a per-server edit under a row lock so two
// admins editing different servers at the same time cannot lose one another's
// write — the read-modify-write the client can no longer do itself.
func (h *Handler) mutateWorkspaceMcpServers(
	w http.ResponseWriter,
	r *http.Request,
	idUUID pgtype.UUID,
	workspaceID string,
	apply func(servers map[string]json.RawMessage) error,
) (db.Workspace, bool) {
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("workspace mcp server edit: begin tx failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace mcp_config")
		return db.Workspace{}, false
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	current, err := qtx.LockWorkspaceMcpConfig(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return db.Workspace{}, false
	}

	servers := map[string]json.RawMessage{}
	if hasManagedJSON(current) {
		doc, _, derr := parseMcpDocument(current)
		if derr != nil {
			writeError(w, http.StatusConflict, "the stored workspace MCP configuration is malformed; replace it with PUT /mcp-config")
			return db.Workspace{}, false
		}
		parsed, perr := unmarshalServerMap(doc["mcpServers"])
		if perr != nil {
			writeError(w, http.StatusConflict, "the stored workspace MCP configuration is malformed; replace it with PUT /mcp-config")
			return db.Workspace{}, false
		}
		servers = parsed
	}

	if aerr := apply(servers); aerr != nil {
		var wErr *workspaceMcpError
		if errors.As(aerr, &wErr) {
			writeError(w, wErr.status, wErr.message)
		} else {
			writeError(w, http.StatusBadRequest, aerr.Error())
		}
		return db.Workspace{}, false
	}

	var ws db.Workspace
	if len(servers) == 0 {
		// Removing the last shared server means "nothing shared" — clear the
		// column instead of storing an empty document, so the resolver's
		// "workspace declares no servers" path is reached the same way whether
		// the admin deleted servers one by one or cleared the whole layer.
		ws, err = qtx.ClearWorkspaceMcpConfig(r.Context(), idUUID)
	} else {
		var doc []byte
		doc, err = json.Marshal(map[string]any{"mcpServers": servers})
		if err == nil {
			ws, err = qtx.SetWorkspaceMcpConfig(r.Context(), db.SetWorkspaceMcpConfigParams{ID: idUUID, McpConfig: doc})
		}
	}
	if err != nil {
		slog.Warn("workspace mcp server edit failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace mcp_config")
		return db.Workspace{}, false
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("workspace mcp server edit: commit failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace mcp_config")
		return db.Workspace{}, false
	}
	return ws, true
}

// writeWorkspaceMcpConfigResult logs the change and returns the inventory.
// Server names and count only — never the document — so neither the log nor
// the response becomes a second copy of the workspace's credentials.
func (h *Handler) writeWorkspaceMcpConfigResult(w http.ResponseWriter, r *http.Request, ws db.Workspace, action string) {
	resp := buildWorkspaceMcpConfigResponse(ws)
	slog.Info("workspace mcp_config updated", append(logger.RequestAttrs(r),
		"workspace_id", uuidToString(ws.ID), "action", action, "server_count", resp.ServerCount)...)
	writeJSON(w, http.StatusOK, resp)
}
