package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---- Response types ----

type RuntimeGroupOverrideResponse struct {
	ID          string  `json:"id"`
	GroupID     string  `json:"group_id"`
	RuntimeID   string  `json:"runtime_id"`
	RuntimeName string  `json:"runtime_name"`
	StartsAt    string  `json:"starts_at"`
	EndsAt      string  `json:"ends_at"`
	CreatedBy   *string `json:"created_by"`
}

type RuntimeGroupResponse struct {
	ID               string                        `json:"id"`
	WorkspaceID      string                        `json:"workspace_id"`
	Name             string                        `json:"name"`
	Description      string                        `json:"description"`
	Runtimes         []AgentRuntimeRef             `json:"runtimes"`
	ActiveOverride   *RuntimeGroupOverrideResponse `json:"active_override"`
	MemberAgentCount int64                         `json:"member_agent_count"`
	CreatedBy        *string                       `json:"created_by"`
	CreatedAt        string                        `json:"created_at"`
	UpdatedAt        string                        `json:"updated_at"`
}

// ---- Request types ----

type CreateRuntimeGroupRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	RuntimeIDs  []string `json:"runtime_ids"`
}

type UpdateRuntimeGroupRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	RuntimeIDs  *[]string `json:"runtime_ids"`
}

type SetRuntimeGroupOverrideRequest struct {
	RuntimeID string    `json:"runtime_id"`
	EndsAt    time.Time `json:"ends_at"`
}

// ---- Helpers ----

func (h *Handler) buildRuntimeGroupResponse(ctx context.Context, g db.RuntimeGroup) (RuntimeGroupResponse, error) {
	members, err := h.Queries.ListRuntimeGroupMembers(ctx, g.ID)
	if err != nil {
		return RuntimeGroupResponse{}, err
	}
	runtimes := make([]AgentRuntimeRef, len(members))
	for i, m := range members {
		runtimes[i] = AgentRuntimeRef{
			ID:          uuidToString(m.RuntimeID),
			Name:        m.RuntimeName,
			Status:      m.RuntimeStatus,
			RuntimeMode: m.RuntimeMode,
			Provider:    m.RuntimeProvider,
			DeviceInfo:  m.RuntimeDeviceInfo,
			OwnerID:     uuidToPtr(m.RuntimeOwnerID),
			LastUsedAt:  timestampToPtr(m.LastUsedAt),
		}
	}

	var override *RuntimeGroupOverrideResponse
	active, err := h.Queries.GetActiveRuntimeGroupOverride(ctx, g.ID)
	if err == nil {
		override = &RuntimeGroupOverrideResponse{
			ID:          uuidToString(active.ID),
			GroupID:     uuidToString(active.GroupID),
			RuntimeID:   uuidToString(active.RuntimeID),
			RuntimeName: active.RuntimeName,
			StartsAt:    timestampToString(active.StartsAt),
			EndsAt:      timestampToString(active.EndsAt),
			CreatedBy:   uuidToPtr(active.CreatedBy),
		}
	}

	count, err := h.Queries.CountAgentsUsingRuntimeGroup(ctx, g.ID)
	if err != nil {
		return RuntimeGroupResponse{}, err
	}

	return RuntimeGroupResponse{
		ID:               uuidToString(g.ID),
		WorkspaceID:      uuidToString(g.WorkspaceID),
		Name:             g.Name,
		Description:      g.Description,
		Runtimes:         runtimes,
		ActiveOverride:   override,
		MemberAgentCount: count,
		CreatedBy:        uuidToPtr(g.CreatedBy),
		CreatedAt:        timestampToString(g.CreatedAt),
		UpdatedAt:        timestampToString(g.UpdatedAt),
	}, nil
}

// ---- Handlers ----

func (h *Handler) ListRuntimeGroups(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	groups, err := h.Queries.ListRuntimeGroupsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runtime groups")
		return
	}
	resp := make([]RuntimeGroupResponse, 0, len(groups))
	for _, g := range groups {
		rg, err := h.buildRuntimeGroupResponse(r.Context(), g)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to build response")
			return
		}
		resp = append(resp, rg)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateRuntimeGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateRuntimeGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	runtimeUUIDs := make([]pgtype.UUID, len(req.RuntimeIDs))
	for i, rid := range req.RuntimeIDs {
		runtimeUUIDs[i] = parseUUID(rid)
	}
	group, err := h.RuntimeGroupService.CreateGroup(
		r.Context(), parseUUID(workspaceID), req.Name, req.Description, parseUUID(userID), runtimeUUIDs,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.buildRuntimeGroupResponse(r.Context(), group)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetRuntimeGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	group, err := h.Queries.GetRuntimeGroupInWorkspace(r.Context(), db.GetRuntimeGroupInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime group not found")
		return
	}
	resp, err := h.buildRuntimeGroupResponse(r.Context(), group)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateRuntimeGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	if _, err := h.Queries.GetRuntimeGroupInWorkspace(r.Context(), db.GetRuntimeGroupInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "runtime group not found")
		return
	}
	var req UpdateRuntimeGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var runtimeUUIDs []pgtype.UUID
	if req.RuntimeIDs != nil {
		runtimeUUIDs = make([]pgtype.UUID, len(*req.RuntimeIDs))
		for i, rid := range *req.RuntimeIDs {
			runtimeUUIDs[i] = parseUUID(rid)
		}
	}
	group, err := h.RuntimeGroupService.UpdateGroup(r.Context(), parseUUID(id), req.Name, req.Description, runtimeUUIDs, parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.buildRuntimeGroupResponse(r.Context(), group)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteRuntimeGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	if _, err := h.Queries.GetRuntimeGroupInWorkspace(r.Context(), db.GetRuntimeGroupInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "runtime group not found")
		return
	}
	if err := h.Queries.DeleteRuntimeGroup(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runtime group")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SetRuntimeGroupOverride(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if _, err := h.Queries.GetRuntimeGroupInWorkspace(r.Context(), db.GetRuntimeGroupInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "runtime group not found")
		return
	}
	var req SetRuntimeGroupOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	if !req.EndsAt.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "ends_at must be in the future")
		return
	}
	endsAt := pgtype.Timestamptz{Time: req.EndsAt, Valid: true}
	_, err := h.RuntimeGroupService.SetOverride(r.Context(), parseUUID(id), parseUUID(req.RuntimeID), endsAt, parseUUID(userID))
	if err != nil {
		if errors.Is(err, service.ErrRuntimeNotGroupMember) {
			writeError(w, http.StatusBadRequest, "runtime is not a member of this group")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to set override")
		return
	}
	group, _ := h.Queries.GetRuntimeGroup(r.Context(), parseUUID(id))
	resp, err := h.buildRuntimeGroupResponse(r.Context(), group)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ClearRuntimeGroupOverride(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	if _, err := h.Queries.GetRuntimeGroupInWorkspace(r.Context(), db.GetRuntimeGroupInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "runtime group not found")
		return
	}
	if err := h.RuntimeGroupService.ClearOverride(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear override")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
