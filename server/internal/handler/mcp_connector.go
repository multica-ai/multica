package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// McpConnectorResponse is the API shape for a single connector in the
// directory. input_schema and mcp_template are passed through as raw JSON:
// input_schema is consumed verbatim by the frontend form renderer, and
// mcp_template is rendered against the user's answers client-side and
// deep-merged into the agent's mcp_config. is_custom lets the UI gate
// edit/delete affordances to workspace-authored rows (global seed rows are
// read-only).
type McpConnectorResponse struct {
	ID          string          `json:"id"`
	WorkspaceID *string         `json:"workspace_id"`
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Icon        *string         `json:"icon"`
	Description *string         `json:"description"`
	Popularity  int32           `json:"popularity"`
	InputSchema json.RawMessage `json:"input_schema"`
	McpTemplate json.RawMessage `json:"mcp_template"`
	IsCustom    bool            `json:"is_custom"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

func mcpConnectorToResponse(c db.McpConnector) McpConnectorResponse {
	inputSchema := json.RawMessage("{}")
	if len(c.InputSchema) > 0 {
		inputSchema = json.RawMessage(c.InputSchema)
	}
	mcpTemplate := json.RawMessage("{}")
	if len(c.McpTemplate) > 0 {
		mcpTemplate = json.RawMessage(c.McpTemplate)
	}
	return McpConnectorResponse{
		ID:          uuidToString(c.ID),
		WorkspaceID: uuidToPtr(c.WorkspaceID),
		Slug:        c.Slug,
		Name:        c.Name,
		Icon:        textToPtr(c.Icon),
		Description: textToPtr(c.Description),
		Popularity:  c.Popularity,
		InputSchema: inputSchema,
		McpTemplate: mcpTemplate,
		// A row is custom (and therefore editable/deletable by an admin)
		// exactly when it carries a workspace_id. Global seed rows have a
		// NULL workspace_id and are never mutable through the API.
		IsCustom:  c.WorkspaceID.Valid,
		CreatedAt: timestampToString(c.CreatedAt),
		UpdatedAt: timestampToString(c.UpdatedAt),
	}
}

// CreateMcpConnectorRequest is the body for authoring a workspace-custom
// connector. input_schema and mcp_template are accepted as raw JSON and
// stored verbatim — the frontend owns their shape (form fields +
// {{placeholder}} substitution template).
type CreateMcpConnectorRequest struct {
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Icon        *string         `json:"icon"`
	Description *string         `json:"description"`
	Popularity  int32           `json:"popularity"`
	InputSchema json.RawMessage `json:"input_schema"`
	McpTemplate json.RawMessage `json:"mcp_template"`
}

// UpdateMcpConnectorRequest is the partial-update body for a workspace-custom
// connector. Every field is a pointer so an omitted key leaves the column
// unchanged (the SQL uses COALESCE(narg, col)).
type UpdateMcpConnectorRequest struct {
	Name        *string          `json:"name"`
	Icon        *string          `json:"icon"`
	Description *string          `json:"description"`
	Popularity  *int32           `json:"popularity"`
	InputSchema *json.RawMessage `json:"input_schema"`
	McpTemplate *json.RawMessage `json:"mcp_template"`
}

// ensureGlobalMcpConnectorsSeeded inserts the embedded curated catalog into
// the global (workspace_id NULL) scope if it has not been seeded yet. It is
// idempotent: it short-circuits when any global row already exists, and each
// insert uses ON CONFLICT DO NOTHING so concurrent first-list requests cannot
// race-duplicate the catalog. Seeding lazily on first list (rather than at
// migrate time) keeps the curated JSON the single source of truth — bumping
// the embedded file ships new connectors without a migration.
func (h *Handler) ensureGlobalMcpConnectorsSeeded(ctx context.Context) error {
	count, err := h.Queries.CountGlobalMcpConnectors(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, c := range mcpConnectorSeed {
		inputSchema := []byte(c.InputSchema)
		if len(inputSchema) == 0 {
			inputSchema = []byte("{}")
		}
		if _, err := h.Queries.InsertGlobalMcpConnector(ctx, db.InsertGlobalMcpConnectorParams{
			Slug:        c.Slug,
			Name:        c.Name,
			Icon:        strToText(c.Icon),
			Description: strToText(c.Description),
			Popularity:  c.Popularity,
			InputSchema: inputSchema,
			McpTemplate: []byte(c.McpTemplate),
		}); err != nil {
			return err
		}
	}
	return nil
}

// ListMcpConnectors returns the connector directory visible to the current
// workspace: every global curated connector plus the workspace's own custom
// connectors, ordered by popularity. Any workspace member may browse the
// directory (read access mirrors ListSkills / ListAgents).
func (h *Handler) ListMcpConnectors(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if err := h.ensureGlobalMcpConnectorsSeeded(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to seed connector catalog")
		return
	}

	connectors, err := h.Queries.ListMcpConnectors(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connectors")
		return
	}

	resp := make([]McpConnectorResponse, 0, len(connectors))
	for _, c := range connectors {
		resp = append(resp, mcpConnectorToResponse(c))
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateMcpConnector authors a workspace-custom connector. Admin-gated:
// only workspace owner/admin may add to the catalog (custom connectors are
// shared workspace-wide, so authoring them is an administrative action).
func (h *Handler) CreateMcpConnector(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req CreateMcpConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Slug == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "slug and name are required")
		return
	}
	if len(req.McpTemplate) == 0 {
		writeError(w, http.StatusBadRequest, "mcp_template is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	inputSchema := []byte(req.InputSchema)
	if len(inputSchema) == 0 {
		inputSchema = []byte("{}")
	}

	created, err := h.Queries.CreateMcpConnector(r.Context(), db.CreateMcpConnectorParams{
		WorkspaceID: wsUUID,
		Slug:        req.Slug,
		Name:        req.Name,
		Icon:        ptrToText(req.Icon),
		Description: ptrToText(req.Description),
		Popularity:  req.Popularity,
		InputSchema: inputSchema,
		McpTemplate: []byte(req.McpTemplate),
		CreatedBy:   member.UserID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a connector with this slug already exists in the workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create connector")
		return
	}

	writeJSON(w, http.StatusCreated, mcpConnectorToResponse(created))
}

// UpdateMcpConnector edits a workspace-custom connector. Admin-gated. The
// underlying query only matches rows with the current workspace_id and a
// non-NULL workspace_id, so a global seed row (or another workspace's row)
// can never be mutated here — a miss surfaces as 404.
func (h *Handler) UpdateMcpConnector(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "connector id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req UpdateMcpConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateMcpConnectorParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
		Name:        ptrToText(req.Name),
		Icon:        ptrToText(req.Icon),
		Description: ptrToText(req.Description),
	}
	if req.Popularity != nil {
		params.Popularity = pgtype.Int4{Int32: *req.Popularity, Valid: true}
	}
	if req.InputSchema != nil {
		params.InputSchema = []byte(*req.InputSchema)
	}
	if req.McpTemplate != nil {
		params.McpTemplate = []byte(*req.McpTemplate)
	}

	updated, err := h.Queries.UpdateMcpConnector(r.Context(), params)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "connector not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update connector")
		return
	}

	writeJSON(w, http.StatusOK, mcpConnectorToResponse(updated))
}

// DeleteMcpConnector removes a workspace-custom connector. Admin-gated. The
// query returns the deleted id (RETURNING id); a missing row means the id
// was a global seed row, belonged to another workspace, or never existed —
// all of which are 404 here, never a silent 204 (guards #1661).
func (h *Handler) DeleteMcpConnector(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "connector id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, err := h.Queries.DeleteMcpConnector(r.Context(), db.DeleteMcpConnectorParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "connector not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete connector")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
