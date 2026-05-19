package handler

// CEREBRO-PATCH(runtime-tools-admin-handler): JEH-1710 — unified runtime tool
// inventory and access-control admin API.
//
// Endpoints:
//
//   GET    /api/runtimes/{runtimeId}/tools                       — list tools
//   PATCH  /api/runtimes/{runtimeId}/tools/{toolName}            — toggle enabled
//   GET    /api/runtimes/{runtimeId}/tool-grants                 — list grants
//   POST   /api/runtimes/{runtimeId}/tools/{toolName}/groups/{groupId}  — grant group
//   DELETE /api/runtimes/{runtimeId}/tools/{toolName}/groups/{groupId}  — revoke group
//   POST   /api/runtimes/{runtimeId}/tools/{toolName}/users/{userId}    — grant user
//   DELETE /api/runtimes/{runtimeId}/tools/{toolName}/users/{userId}    — revoke user
//
//   GET    /api/agents/{id}/tool-overrides                       — list overrides
//   PUT    /api/agents/{id}/tool-overrides/{toolName}            — set override
//   DELETE /api/agents/{id}/tool-overrides/{toolName}            — clear override
//
// Auth: workspace owner/admin only — same threat model as the runtime
// tools_config endpoint. Mutating runtime tool grants effectively governs
// what every agent on the runtime can do.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// RuntimeToolsAdminService is the seam the cerebro runtimetools service
// implements. The handler package never imports the cerebrodb generated
// package directly — that would create an import cycle.
type RuntimeToolsAdminService interface {
	ListTools(ctx context.Context, runtimeID pgtype.UUID) ([]RuntimeToolView, error)
	SetEnabled(ctx context.Context, runtimeID pgtype.UUID, toolName string, enabled bool) (RuntimeToolView, error)

	ListGroupGrants(ctx context.Context, runtimeID pgtype.UUID) ([]RuntimeToolGroupGrantView, error)
	AddGroupGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, groupID, grantedBy pgtype.UUID) error
	RemoveGroupGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, groupID pgtype.UUID) error

	ListUserGrants(ctx context.Context, runtimeID pgtype.UUID) ([]RuntimeToolUserGrantView, error)
	AddUserGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, userID, grantedBy pgtype.UUID) error
	RemoveUserGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, userID pgtype.UUID) error

	ListAgentOverrides(ctx context.Context, agentID pgtype.UUID) ([]AgentToolOverrideView, error)
	UpsertAgentOverride(ctx context.Context, agentID pgtype.UUID, toolName string, enabled bool, updatedBy pgtype.UUID) (AgentToolOverrideView, error)
	DeleteAgentOverride(ctx context.Context, agentID pgtype.UUID, toolName string) error
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

// RuntimeToolGroupGrantView is one row in the runtime-tool group whitelist.
type RuntimeToolGroupGrantView struct {
	RuntimeID string `json:"runtime_id"`
	ToolName  string `json:"tool_name"`
	GroupID   string `json:"group_id"`
	GroupName string `json:"group_name"`
	GrantedAt string `json:"granted_at"`
}

// RuntimeToolUserGrantView is one row in the runtime-tool user whitelist.
type RuntimeToolUserGrantView struct {
	RuntimeID     string `json:"runtime_id"`
	ToolName      string `json:"tool_name"`
	UserID        string `json:"user_id"`
	UserName      string `json:"user_name"`
	UserEmail     string `json:"user_email"`
	UserAvatarURL string `json:"user_avatar_url,omitempty"`
	GrantedAt     string `json:"granted_at"`
}

// AgentToolOverrideView is one row in the per-agent override table.
type AgentToolOverrideView struct {
	AgentID   string `json:"agent_id"`
	ToolName  string `json:"tool_name"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt string `json:"updated_at"`
}

// SetRuntimeToolsAdmin wires the service into the handler. Called after
// handler.New() so the upstream constructor signature stays clean.
func (h *Handler) SetRuntimeToolsAdmin(svc RuntimeToolsAdminService) {
	h.runtimeToolsAdmin = svc
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

// SetRuntimeToolEnabled handles PATCH /api/runtimes/{runtimeId}/tools/{toolName}.
func (h *Handler) SetRuntimeToolEnabled(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	rtID, _, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	toolName := chi.URLParam(r, "toolName")
	if toolName == "" {
		writeError(w, http.StatusBadRequest, "tool name is required")
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	tool, err := h.runtimeToolsAdmin.SetEnabled(r.Context(), rtID, toolName, *body.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "set enabled: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tool)
}

// ListRuntimeToolGrants handles GET /api/runtimes/{runtimeId}/tool-grants.
// Returns both group and user grants in one response keyed by tool name.
func (h *Handler) ListRuntimeToolGrants(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	rtID, _, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	groups, err := h.runtimeToolsAdmin.ListGroupGrants(r.Context(), rtID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list group grants: "+err.Error())
		return
	}
	users, err := h.runtimeToolsAdmin.ListUserGrants(r.Context(), rtID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list user grants: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group_grants": groups,
		"user_grants":  users,
	})
}

// AddRuntimeToolGroupGrant handles
// POST /api/runtimes/{runtimeId}/tools/{toolName}/groups/{groupId}.
func (h *Handler) AddRuntimeToolGroupGrant(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	rtID, userID, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	toolName := chi.URLParam(r, "toolName")
	groupIDStr := chi.URLParam(r, "groupId")
	if toolName == "" || groupIDStr == "" {
		writeError(w, http.StatusBadRequest, "tool name and group id are required")
		return
	}
	if err := h.runtimeToolsAdmin.AddGroupGrant(r.Context(), rtID, toolName, parseUUID(groupIDStr), parseUUID(userID)); err != nil {
		writeError(w, http.StatusInternalServerError, "add group grant: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveRuntimeToolGroupGrant handles
// DELETE /api/runtimes/{runtimeId}/tools/{toolName}/groups/{groupId}.
func (h *Handler) RemoveRuntimeToolGroupGrant(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	rtID, _, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	toolName := chi.URLParam(r, "toolName")
	groupIDStr := chi.URLParam(r, "groupId")
	if toolName == "" || groupIDStr == "" {
		writeError(w, http.StatusBadRequest, "tool name and group id are required")
		return
	}
	if err := h.runtimeToolsAdmin.RemoveGroupGrant(r.Context(), rtID, toolName, parseUUID(groupIDStr)); err != nil {
		writeError(w, http.StatusInternalServerError, "remove group grant: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddRuntimeToolUserGrant handles
// POST /api/runtimes/{runtimeId}/tools/{toolName}/users/{userId}.
func (h *Handler) AddRuntimeToolUserGrant(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	rtID, grantorID, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	toolName := chi.URLParam(r, "toolName")
	userIDStr := chi.URLParam(r, "userId")
	if toolName == "" || userIDStr == "" {
		writeError(w, http.StatusBadRequest, "tool name and user id are required")
		return
	}
	if err := h.runtimeToolsAdmin.AddUserGrant(r.Context(), rtID, toolName, parseUUID(userIDStr), parseUUID(grantorID)); err != nil {
		writeError(w, http.StatusInternalServerError, "add user grant: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveRuntimeToolUserGrant handles
// DELETE /api/runtimes/{runtimeId}/tools/{toolName}/users/{userId}.
func (h *Handler) RemoveRuntimeToolUserGrant(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	rtID, _, ok := h.loadRuntimeForAdmin(w, r)
	if !ok {
		return
	}
	toolName := chi.URLParam(r, "toolName")
	userIDStr := chi.URLParam(r, "userId")
	if toolName == "" || userIDStr == "" {
		writeError(w, http.StatusBadRequest, "tool name and user id are required")
		return
	}
	if err := h.runtimeToolsAdmin.RemoveUserGrant(r.Context(), rtID, toolName, parseUUID(userIDStr)); err != nil {
		writeError(w, http.StatusInternalServerError, "remove user grant: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListAgentToolOverrides handles GET /api/agents/{id}/tool-overrides.
func (h *Handler) ListAgentToolOverrides(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	overrides, err := h.runtimeToolsAdmin.ListAgentOverrides(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list overrides: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, overrides)
}

// PutAgentToolOverride handles PUT /api/agents/{id}/tool-overrides/{toolName}.
func (h *Handler) PutAgentToolOverride(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	toolName := chi.URLParam(r, "toolName")
	if toolName == "" {
		writeError(w, http.StatusBadRequest, "tool name is required")
		return
	}

	// Agent overrides modify runtime-level access, so require the same
	// owner/admin gate as the runtime endpoints.
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(agent.WorkspaceID), "agent not found")
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can change tool overrides")
		return
	}

	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, errEOF()) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	override, err := h.runtimeToolsAdmin.UpsertAgentOverride(r.Context(), agent.ID, toolName, *body.Enabled, member.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upsert override: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, override)
}

// DeleteAgentToolOverride handles DELETE /api/agents/{id}/tool-overrides/{toolName}.
func (h *Handler) DeleteAgentToolOverride(w http.ResponseWriter, r *http.Request) {
	if !h.requireToolsAdmin(w) {
		return
	}
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	toolName := chi.URLParam(r, "toolName")
	if toolName == "" {
		writeError(w, http.StatusBadRequest, "tool name is required")
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(agent.WorkspaceID), "agent not found")
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can change tool overrides")
		return
	}
	if err := h.runtimeToolsAdmin.DeleteAgentOverride(r.Context(), agent.ID, toolName); err != nil {
		writeError(w, http.StatusInternalServerError, "delete override: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// errEOF is a small helper to compare to io.EOF without importing io here
// (handler files in this package shouldn't reach for io for one comparison).
func errEOF() error { return jsonErrEOF }

var jsonErrEOF = errors.New("EOF")
