package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/dwickyfp/wallts/server/internal/util"
	db "github.com/dwickyfp/wallts/server/pkg/db/generated"
	"github.com/dwickyfp/wallts/server/pkg/protocol"
)

type FailoverGroupResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Strategy    string `json:"strategy"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type FailoverGroupMemberResponse struct {
	RuntimeID string `json:"runtime_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Priority  int32  `json:"priority"`
}

type FailoverGroupDetail struct {
	ID       string                        `json:"id"`
	Name     string                        `json:"name"`
	Strategy string                        `json:"strategy"`
	Members  []FailoverGroupMemberResponse `json:"members"`
}

type FailoverStatusResponse struct {
	RuntimeID     string               `json:"runtime_id"`
	RuntimeName   string               `json:"runtime_name"`
	Priority      int32                `json:"priority"`
	FailoverGroup *FailoverGroupDetail `json:"failover_group,omitempty"`
}

func failoverGroupToResponse(g db.RuntimeFailoverGroup) FailoverGroupResponse {
	return FailoverGroupResponse{
		ID:          uuidToString(g.ID),
		WorkspaceID: uuidToString(g.WorkspaceID),
		Name:        g.Name,
		Strategy:    g.Strategy,
		CreatedAt:   timestampToString(g.CreatedAt),
		UpdatedAt:   timestampToString(g.UpdatedAt),
	}
}

func (h *Handler) ListFailoverGroups(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "not found"); !ok {
		return
	}
	groups, err := h.Queries.ListFailoverGroups(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failover groups")
		return
	}
	resp := make([]FailoverGroupResponse, len(groups))
	for i, g := range groups {
		resp[i] = failoverGroupToResponse(g)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateFailoverGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "not found"); !ok {
		return
	}
	var req struct {
		Name     string `json:"name"`
		Strategy string `json:"strategy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	strategy := strings.TrimSpace(req.Strategy)
	if strategy == "" {
		strategy = "priority"
	}
	if strategy != "priority" && strategy != "round-robin" && strategy != "least-loaded" {
		writeError(w, http.StatusBadRequest, "strategy must be 'priority', 'round-robin', or 'least-loaded'")
		return
	}
	group, err := h.Queries.CreateFailoverGroup(r.Context(), db.CreateFailoverGroupParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
		Strategy:    strategy,
	})
	if err != nil {
		slog.Error("CreateFailoverGroup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create failover group")
		return
	}
	writeJSON(w, http.StatusCreated, failoverGroupToResponse(group))
}

func (h *Handler) DeleteFailoverGroup(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupId")
	groupUUID, ok := parseUUIDOrBadRequest(w, groupID, "group_id")
	if !ok {
		return
	}
	group, err := h.Queries.GetFailoverGroup(r.Context(), groupUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "failover group not found")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(group.WorkspaceID), "not found"); !ok {
		return
	}
	runtimes, err := h.Queries.ListRuntimesByFailoverGroup(r.Context(), groupUUID)
	if err == nil {
		for _, rt := range runtimes {
			if _, err := h.Queries.UpdateRuntimeFailoverGroup(r.Context(), db.UpdateRuntimeFailoverGroupParams{
				FailoverGroupID: pgtype.UUID{},
				ID:              rt.ID,
			}); err != nil {
				slog.Warn("failed to unlink runtime from failover group",
					"runtime_id", uuidToString(rt.ID), "error", err)
			}
		}
	}
	if err := h.Queries.DeleteFailoverGroup(r.Context(), groupUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete failover group")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SetRuntimePriority(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "you can only edit your own runtimes")
		return
	}
	var body struct {
		Priority        *int32  `json:"priority"`
		FailoverGroupID *string `json:"failover_group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	updated := rt
	if body.Priority != nil {
		updated, err = h.Queries.UpdateRuntimePriority(r.Context(), db.UpdateRuntimePriorityParams{
			Priority: *body.Priority,
			ID:       runtimeUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update priority")
			return
		}
	}
	if body.FailoverGroupID != nil {
		var groupID pgtype.UUID
		if *body.FailoverGroupID != "" {
			gid, perr := util.ParseUUID(*body.FailoverGroupID)
			if perr != nil {
				writeError(w, http.StatusBadRequest, "invalid failover_group_id")
				return
			}
			group, gerr := h.Queries.GetFailoverGroup(r.Context(), gid)
			if gerr != nil {
				writeError(w, http.StatusNotFound, "failover group not found")
				return
			}
			if uuidToString(group.WorkspaceID) != uuidToString(rt.WorkspaceID) {
				writeError(w, http.StatusBadRequest, "failover group belongs to a different workspace")
				return
			}
			groupID = gid
		}
		updated, err = h.Queries.UpdateRuntimeFailoverGroup(r.Context(), db.UpdateRuntimeFailoverGroupParams{
			FailoverGroupID: groupID,
			ID:              runtimeUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update failover group")
			return
		}
	}
	h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "member",
		uuidToString(member.UserID), map[string]any{"action": "update"})
	writeJSON(w, http.StatusOK, runtimeToResponse(updated))
}

func (h *Handler) GetRuntimeFailoverStatus(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}
	resp := FailoverStatusResponse{
		RuntimeID:   uuidToString(rt.ID),
		RuntimeName: rt.Name,
		Priority:    rt.Priority,
	}
	if rt.FailoverGroupID.Valid {
		group, gerr := h.Queries.GetFailoverGroup(r.Context(), rt.FailoverGroupID)
		if gerr == nil {
			members, _ := h.Queries.ListRuntimesByFailoverGroup(r.Context(), rt.FailoverGroupID)
			memberResp := make([]FailoverGroupMemberResponse, len(members))
			for i, m := range members {
				memberResp[i] = FailoverGroupMemberResponse{
					RuntimeID: uuidToString(m.ID),
					Name:      m.Name,
					Status:    m.Status,
					Priority:  m.Priority,
				}
			}
			resp.FailoverGroup = &FailoverGroupDetail{
				ID:       uuidToString(group.ID),
				Name:     group.Name,
				Strategy: group.Strategy,
				Members:  memberResp,
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
