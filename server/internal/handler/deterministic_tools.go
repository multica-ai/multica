package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/detsteps"
	"github.com/multica-ai/multica/server/pkg/dettools"
)

// toolNamePattern constrains a tool name to a valid MCP tool identifier: the
// name is what an agent calls, so it must be a lowercase snake_case token.
var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// reservedToolNames are the built-in compiled tools; a workspace tool may not
// shadow one (the per-task registry refuses the collision anyway, but rejecting
// at author time gives a clear error).
var reservedToolNames = map[string]bool{
	"repo_facts": true, "policy_check": true, "build_probe": true,
	"test_gate": true, "diff_summarize": true, "artifact_emit": true,
}

// DeterministicToolResponse is the API shape for a workspace-authored tool.
type DeterministicToolResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	Enabled     bool    `json:"enabled"`
	CreatedBy   *string `json:"created_by,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func deterministicToolToResponse(t db.DeterministicTool) DeterministicToolResponse {
	return DeterministicToolResponse{
		ID:          uuidToString(t.ID),
		WorkspaceID: uuidToString(t.WorkspaceID),
		Name:        t.Name,
		Description: t.Description,
		Source:      t.Source,
		Enabled:     t.Enabled,
		CreatedBy:   uuidToPtr(t.CreatedBy),
		CreatedAt:   timestampToString(t.CreatedAt),
		UpdatedAt:   timestampToString(t.UpdatedAt),
	}
}

type createDeterministicToolRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Enabled     *bool  `json:"enabled"`
}

type updateDeterministicToolRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Source      *string `json:"source"`
	Enabled     *bool   `json:"enabled"`
}

// validateToolName checks the shared name rules and returns a client-facing
// error message, or "" when valid.
func validateToolName(name string) string {
	if !toolNamePattern.MatchString(name) {
		return "name must be lowercase snake_case (start with a letter; letters, digits, underscores; max 64 chars)"
	}
	if reservedToolNames[name] {
		return "name collides with a built-in tool"
	}
	return ""
}

// ListDeterministicTools returns all workspace-authored tools.
func (h *Handler) ListDeterministicTools(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	tools, err := h.Queries.ListDeterministicToolsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deterministic tools")
		return
	}
	resp := make([]DeterministicToolResponse, len(tools))
	for i, t := range tools {
		resp[i] = deterministicToolToResponse(t)
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateDeterministicTool persists a new workspace-authored tool.
func (h *Handler) CreateDeterministicTool(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req createDeterministicToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if msg := validateToolName(req.Name); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	tool, err := h.Queries.CreateDeterministicTool(r.Context(), db.CreateDeterministicToolParams{
		WorkspaceID: wsUUID,
		Name:        req.Name,
		Description: sanitizeNullBytes(req.Description),
		Source:      sanitizeNullBytes(req.Source),
		Enabled:     enabled,
		CreatedBy:   parseUUID(creatorID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a deterministic tool with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create deterministic tool")
		return
	}
	writeJSON(w, http.StatusCreated, deterministicToolToResponse(tool))
}

// loadDeterministicToolForUser resolves the path id and confirms the caller is a
// member of the owning workspace.
func (h *Handler) loadDeterministicToolForUser(w http.ResponseWriter, r *http.Request) (db.DeterministicTool, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return db.DeterministicTool{}, false
	}
	tool, err := h.Queries.GetDeterministicTool(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "deterministic tool not found")
		return db.DeterministicTool{}, false
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(tool.WorkspaceID), "deterministic tool not found"); !ok {
		return db.DeterministicTool{}, false
	}
	return tool, true
}

// UpdateDeterministicTool edits an existing tool. Any subset of fields may be set.
func (h *Handler) UpdateDeterministicTool(w http.ResponseWriter, r *http.Request) {
	tool, ok := h.loadDeterministicToolForUser(w, r)
	if !ok {
		return
	}

	var req updateDeterministicToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateDeterministicToolParams{ID: tool.ID}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if msg := validateToolName(name); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: sanitizeNullBytes(*req.Description), Valid: true}
	}
	if req.Source != nil {
		params.Source = pgtype.Text{String: sanitizeNullBytes(*req.Source), Valid: true}
	}
	if req.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}

	updated, err := h.Queries.UpdateDeterministicTool(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a deterministic tool with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update deterministic tool")
		return
	}
	writeJSON(w, http.StatusOK, deterministicToolToResponse(updated))
}

// DeleteDeterministicTool removes a tool.
func (h *Handler) DeleteDeterministicTool(w http.ResponseWriter, r *http.Request) {
	tool, ok := h.loadDeterministicToolForUser(w, r)
	if !ok {
		return
	}
	if _, err := h.Queries.DeleteDeterministicTool(r.Context(), tool.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete deterministic tool")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testDeterministicToolRequest is the body for the author/test loop: the Go
// source of a deterministic step plus a sample input the step's Run receives.
type testDeterministicToolRequest struct {
	Source string         `json:"source"`
	Input  map[string]any `json:"input"`
}

// TestDeterministicTool compiles and runs source in the sandboxed interpreter and
// returns the dettools.Result envelope. It always responds 200: a compile error
// or policy failure is a step-level outcome carried inside the envelope, not an
// HTTP error — the only 4xx is a malformed request body or empty source.
func (h *Handler) TestDeterministicTool(w http.ResponseWriter, r *http.Request) {
	var req testDeterministicToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Source) == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}

	// Run in an isolated, killable subprocess (this binary re-exec'd as a step
	// sandbox) rather than in-process, so tenant Go never executes inside the
	// long-lived server process and a runaway step is hard-killed.
	res := detsteps.RunSubprocess(r.Context(), detsteps.SelfBin(), req.Source, req.Input, detsteps.DefaultTimeout)
	writeJSON(w, http.StatusOK, res)
}

// Keep the reserved-name list honest: every name we reject as "built-in" must
// actually be a registered built-in tool, so a renamed/removed compiled tool
// can't silently leave a stale reservation behind.
func init() {
	have := map[string]bool{}
	for _, name := range dettools.AllToolNames() {
		have[name] = true
	}
	for name := range reservedToolNames {
		if !have[name] {
			panic("reservedToolNames lists an unknown built-in tool: " + name)
		}
	}
}
