package grouppermissions

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler wires the permissions service to chi routes. Permission *editing*
// is admin-only (governance call). Reading is open to any workspace member,
// matching the existing groups handler's listing semantics.
type Handler struct {
	Service  *Service
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries
}

// New is the canonical constructor; router.go calls it once per process.
func New(cerebro *cerebrodb.Queries, upstream *db.Queries, bus *events.Bus) *Handler {
	return &Handler{
		Service:  NewService(cerebro, bus),
		Cerebro:  cerebro,
		Upstream: upstream,
	}
}

// --- request/response types -------------------------------------------------

type capabilityResponse struct {
	GroupID    string  `json:"group_id"`
	Capability string  `json:"capability"`
	GrantedBy  *string `json:"granted_by"`
	GrantedAt  string  `json:"granted_at"`
}

type runtimeAccessResponse struct {
	GroupID   string  `json:"group_id"`
	RuntimeID string  `json:"runtime_id"`
	GrantedBy *string `json:"granted_by"`
	GrantedAt string  `json:"granted_at"`
}

type agentAccessResponse struct {
	GroupID   string  `json:"group_id"`
	AgentID   string  `json:"agent_id"`
	GrantedBy *string `json:"granted_by"`
	GrantedAt string  `json:"granted_at"`
}

type projectGroupResponse struct {
	ProjectID string  `json:"project_id"`
	GroupID   string  `json:"group_id"`
	AddedBy   *string `json:"added_by"`
	AddedAt   string  `json:"added_at"`
}

type setCapabilityRequest struct {
	Capability string `json:"capability"`
}

type runtimeRefRequest struct {
	RuntimeID string `json:"runtime_id"`
}

type agentRefRequest struct {
	AgentID string `json:"agent_id"`
}

type groupRefRequest struct {
	GroupID string `json:"group_id"`
}

func capabilityToResponse(row cerebrodb.CerebroGroupCapability) capabilityResponse {
	return capabilityResponse{
		GroupID:    util.UUIDToString(row.GroupID),
		Capability: row.Capability,
		GrantedBy:  util.UUIDToPtr(row.GrantedBy),
		GrantedAt:  util.TimestampToString(row.GrantedAt),
	}
}

func runtimeAccessToResponse(row cerebrodb.CerebroGroupRuntimeAccess) runtimeAccessResponse {
	return runtimeAccessResponse{
		GroupID:   util.UUIDToString(row.GroupID),
		RuntimeID: util.UUIDToString(row.RuntimeID),
		GrantedBy: util.UUIDToPtr(row.GrantedBy),
		GrantedAt: util.TimestampToString(row.GrantedAt),
	}
}

func agentAccessToResponse(row cerebrodb.CerebroGroupAgentAccess) agentAccessResponse {
	return agentAccessResponse{
		GroupID:   util.UUIDToString(row.GroupID),
		AgentID:   util.UUIDToString(row.AgentID),
		GrantedBy: util.UUIDToPtr(row.GrantedBy),
		GrantedAt: util.TimestampToString(row.GrantedAt),
	}
}

func projectGroupToResponse(row cerebrodb.CerebroProjectGroupMember) projectGroupResponse {
	return projectGroupResponse{
		ProjectID: util.UUIDToString(row.ProjectID),
		GroupID:   util.UUIDToString(row.GroupID),
		AddedBy:   util.UUIDToPtr(row.AddedBy),
		AddedAt:   util.TimestampToString(row.AddedAt),
	}
}

// --- capability endpoints ---------------------------------------------------

// ListCapabilities — GET /api/groups/{id}/capabilities. Any workspace member
// can read; we still resolve the group through the workspace member context
// so cross-workspace lookups 404.
func (h *Handler) ListCapabilities(w http.ResponseWriter, r *http.Request) {
	_, group, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	rows, err := h.Service.ListCapabilities(r.Context(), group.ID)
	if err != nil {
		h.serverError(w, r, "list capabilities", err)
		return
	}
	resp := make([]capabilityResponse, len(rows))
	for i, row := range rows {
		resp[i] = capabilityToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// SetCapability — POST /api/groups/{id}/capabilities. Admin/owner only.
// Body: {"capability": "create_runtime" | "create_agent"}.
func (h *Handler) SetCapability(w http.ResponseWriter, r *http.Request) {
	member, group, ok := h.requireAdminAndGroup(w, r)
	if !ok {
		return
	}

	var req setCapabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !IsKnownCapability(req.Capability) {
		writeError(w, http.StatusBadRequest, "unknown capability")
		return
	}

	row, err := h.Service.SetCapability(r.Context(), group.WorkspaceID, member.UserID, group.ID, req.Capability)
	if err != nil {
		h.serverError(w, r, "set capability", err)
		return
	}
	writeJSON(w, http.StatusCreated, capabilityToResponse(row))
}

// RemoveCapability — DELETE /api/groups/{id}/capabilities/{capability}.
func (h *Handler) RemoveCapability(w http.ResponseWriter, r *http.Request) {
	member, group, ok := h.requireAdminAndGroup(w, r)
	if !ok {
		return
	}
	capability := chi.URLParam(r, "capability")
	if !IsKnownCapability(capability) {
		writeError(w, http.StatusBadRequest, "unknown capability")
		return
	}
	if err := h.Service.RemoveCapability(r.Context(), group.WorkspaceID, member.UserID, group.ID, capability); err != nil {
		h.serverError(w, r, "remove capability", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- runtime allowlist endpoints --------------------------------------------

func (h *Handler) ListRuntimes(w http.ResponseWriter, r *http.Request) {
	_, group, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	rows, err := h.Service.ListRuntimes(r.Context(), group.ID)
	if err != nil {
		h.serverError(w, r, "list group runtimes", err)
		return
	}
	resp := make([]runtimeAccessResponse, len(rows))
	for i, row := range rows {
		resp[i] = runtimeAccessToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// AddRuntime — POST /api/groups/{id}/runtimes. Validates that the runtime
// is in the same workspace; a cross-workspace add should 400, not silently
// create a dangling allowlist row.
func (h *Handler) AddRuntime(w http.ResponseWriter, r *http.Request) {
	member, group, ok := h.requireAdminAndGroup(w, r)
	if !ok {
		return
	}
	var req runtimeRefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runtimeID, err := util.ParseUUID(req.RuntimeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	rt, err := h.Upstream.GetAgentRuntime(r.Context(), runtimeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "runtime not found")
		return
	}
	if rt.WorkspaceID != group.WorkspaceID {
		writeError(w, http.StatusBadRequest, "runtime is in a different workspace")
		return
	}
	row, err := h.Service.AddRuntime(r.Context(), group.WorkspaceID, member.UserID, group.ID, runtimeID)
	if err != nil {
		h.serverError(w, r, "add group runtime", err)
		return
	}
	writeJSON(w, http.StatusCreated, runtimeAccessToResponse(row))
}

// RemoveRuntime — DELETE /api/groups/{id}/runtimes/{runtimeId}.
func (h *Handler) RemoveRuntime(w http.ResponseWriter, r *http.Request) {
	member, group, ok := h.requireAdminAndGroup(w, r)
	if !ok {
		return
	}
	runtimeID, err := util.ParseUUID(chi.URLParam(r, "runtimeId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	if err := h.Service.RemoveRuntime(r.Context(), group.WorkspaceID, member.UserID, group.ID, runtimeID); err != nil {
		h.serverError(w, r, "remove group runtime", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- agent allowlist endpoints ----------------------------------------------

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	_, group, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	rows, err := h.Service.ListAgents(r.Context(), group.ID)
	if err != nil {
		h.serverError(w, r, "list group agents", err)
		return
	}
	resp := make([]agentAccessResponse, len(rows))
	for i, row := range rows {
		resp[i] = agentAccessToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// AddAgent — POST /api/groups/{id}/agents.
func (h *Handler) AddAgent(w http.ResponseWriter, r *http.Request) {
	member, group, ok := h.requireAdminAndGroup(w, r)
	if !ok {
		return
	}
	var req agentRefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentID, err := util.ParseUUID(req.AgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	a, err := h.Upstream.GetAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "agent not found")
		return
	}
	if a.WorkspaceID != group.WorkspaceID {
		writeError(w, http.StatusBadRequest, "agent is in a different workspace")
		return
	}
	row, err := h.Service.AddAgent(r.Context(), group.WorkspaceID, member.UserID, group.ID, agentID)
	if err != nil {
		h.serverError(w, r, "add group agent", err)
		return
	}
	writeJSON(w, http.StatusCreated, agentAccessToResponse(row))
}

// RemoveAgent — DELETE /api/groups/{id}/agents/{agentId}.
func (h *Handler) RemoveAgent(w http.ResponseWriter, r *http.Request) {
	member, group, ok := h.requireAdminAndGroup(w, r)
	if !ok {
		return
	}
	agentID, err := util.ParseUUID(chi.URLParam(r, "agentId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	if err := h.Service.RemoveAgent(r.Context(), group.WorkspaceID, member.UserID, group.ID, agentID); err != nil {
		h.serverError(w, r, "remove group agent", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- project group-access endpoints -----------------------------------------

// ListProjectGroups — GET /api/projects/{id}/group-access.
func (h *Handler) ListProjectGroups(w http.ResponseWriter, r *http.Request) {
	_, project, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	rows, err := h.Service.ListProjectGroups(r.Context(), project.ID)
	if err != nil {
		h.serverError(w, r, "list project groups", err)
		return
	}
	resp := make([]projectGroupResponse, len(rows))
	for i, row := range rows {
		resp[i] = projectGroupToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// AddProjectGroup — POST /api/projects/{id}/group-access. Admin/owner only.
func (h *Handler) AddProjectGroup(w http.ResponseWriter, r *http.Request) {
	member, project, ok := h.requireAdminAndProject(w, r)
	if !ok {
		return
	}
	var req groupRefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	groupID, err := util.ParseUUID(req.GroupID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group_id")
		return
	}
	g, err := h.Cerebro.GetCerebroGroup(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "group not found")
		return
	}
	if g.WorkspaceID != project.WorkspaceID {
		writeError(w, http.StatusBadRequest, "group is in a different workspace")
		return
	}
	row, err := h.Service.AddProjectGroup(r.Context(), project.WorkspaceID, member.UserID, project.ID, groupID)
	if err != nil {
		h.serverError(w, r, "add project group", err)
		return
	}
	writeJSON(w, http.StatusCreated, projectGroupToResponse(row))
}

// RemoveProjectGroup — DELETE /api/projects/{id}/group-access/{groupId}.
func (h *Handler) RemoveProjectGroup(w http.ResponseWriter, r *http.Request) {
	member, project, ok := h.requireAdminAndProject(w, r)
	if !ok {
		return
	}
	groupID, err := util.ParseUUID(chi.URLParam(r, "groupId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group_id")
		return
	}
	if err := h.Service.RemoveProjectGroup(r.Context(), project.WorkspaceID, member.UserID, project.ID, groupID); err != nil {
		h.serverError(w, r, "remove project group", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ----------------------------------------------------------------

// loadGroup resolves the {id} URL param to a cerebro_group row in the
// caller's workspace. Returns ok=false (and writes a 404) if either the
// group doesn't exist or belongs to a different workspace.
func (h *Handler) loadGroup(w http.ResponseWriter, r *http.Request) (db.Member, cerebrodb.CerebroGroup, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return db.Member{}, cerebrodb.CerebroGroup{}, false
	}
	groupID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return db.Member{}, cerebrodb.CerebroGroup{}, false
	}
	group, err := h.Cerebro.GetCerebroGroup(r.Context(), groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "group not found")
			return db.Member{}, cerebrodb.CerebroGroup{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load group")
		return db.Member{}, cerebrodb.CerebroGroup{}, false
	}
	if !uuidEqual(group.WorkspaceID, member.WorkspaceID) {
		writeError(w, http.StatusNotFound, "group not found")
		return db.Member{}, cerebrodb.CerebroGroup{}, false
	}
	return member, group, true
}

// requireAdminAndGroup gates write endpoints behind owner/admin role.
func (h *Handler) requireAdminAndGroup(w http.ResponseWriter, r *http.Request) (db.Member, cerebrodb.CerebroGroup, bool) {
	member, group, ok := h.loadGroup(w, r)
	if !ok {
		return db.Member{}, cerebrodb.CerebroGroup{}, false
	}
	if !isAdminRole(member.Role) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Member{}, cerebrodb.CerebroGroup{}, false
	}
	return member, group, true
}

// loadProject resolves the {id} URL param to a project row in the caller's
// workspace. Mirrors loadGroup so the project group-access handlers stay
// symmetric with the group-side handlers.
func (h *Handler) loadProject(w http.ResponseWriter, r *http.Request) (db.Member, db.Project, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return db.Member{}, db.Project{}, false
	}
	projectID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id")
		return db.Member{}, db.Project{}, false
	}
	project, err := h.Upstream.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          projectID,
		WorkspaceID: member.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Member{}, db.Project{}, false
	}
	return member, project, true
}

func (h *Handler) requireAdminAndProject(w http.ResponseWriter, r *http.Request) (db.Member, db.Project, bool) {
	member, project, ok := h.loadProject(w, r)
	if !ok {
		return db.Member{}, db.Project{}, false
	}
	if !isAdminRole(member.Role) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Member{}, db.Project{}, false
	}
	return member, project, true
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.Error("group permissions request failed", append(logger.RequestAttrs(r), "op", op, "error", err)...)
	writeError(w, http.StatusInternalServerError, op+" failed")
}

func isAdminRole(role string) bool {
	return role == "owner" || role == "admin"
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
