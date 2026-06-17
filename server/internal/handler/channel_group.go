package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ChannelGroupResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Position  float64 `json:"position"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
}

func channelGroupToResponse(g db.ChannelGroup) ChannelGroupResponse {
	return ChannelGroupResponse{
		ID:        uuidToString(g.ID),
		Name:      g.Name,
		Position:  g.Position,
		CreatedBy: uuidToPtr(g.CreatedBy),
		CreatedAt: timestampToString(g.CreatedAt),
	}
}

func (h *Handler) ListChannelGroups(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	groups, err := h.Queries.ListChannelGroups(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}
	resp := make([]ChannelGroupResponse, len(groups))
	for i, g := range groups {
		resp[i] = channelGroupToResponse(g)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateChannelGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	maxPos, err := h.Queries.GetMaxChannelGroupPosition(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get position")
		return
	}

	group, err := h.Queries.CreateChannelGroup(r.Context(), db.CreateChannelGroupParams{
		WorkspaceID: wsUUID,
		Name:        body.Name,
		Position:    maxPos + 1,
		CreatedBy:   member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create group")
		return
	}
	writeJSON(w, http.StatusCreated, channelGroupToResponse(group))
}

func (h *Handler) UpdateChannelGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	groupID := chi.URLParam(r, "groupId")
	groupUUID, ok := parseUUIDOrBadRequest(w, groupID, "group id")
	if !ok {
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	group, err := h.Queries.UpdateChannelGroupName(r.Context(), db.UpdateChannelGroupNameParams{
		ID:   groupUUID,
		Name: body.Name,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update group")
		return
	}
	writeJSON(w, http.StatusOK, channelGroupToResponse(group))
}

func (h *Handler) DeleteChannelGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	groupID := chi.URLParam(r, "groupId")
	groupUUID, ok := parseUUIDOrBadRequest(w, groupID, "group id")
	if !ok {
		return
	}

	if err := h.Queries.DeleteChannelGroup(r.Context(), db.DeleteChannelGroupParams{
		ID:          groupUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete group")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateChannelGroupPosition(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	groupID := chi.URLParam(r, "groupId")
	groupUUID, ok := parseUUIDOrBadRequest(w, groupID, "group id")
	if !ok {
		return
	}

	var body struct {
		Position float64 `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if err := h.Queries.UpdateChannelGroupPosition(r.Context(), db.UpdateChannelGroupPositionParams{
		ID:       groupUUID,
		Position: body.Position,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update position")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MoveChannelToGroup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	var body struct {
		ChannelID string  `json:"channel_id"`
		GroupID   *string `json:"group_id"`
		Position  float64 `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ChannelID == "" {
		writeError(w, http.StatusBadRequest, "channel_id is required")
		return
	}

	channelUUID, ok := parseUUIDOrBadRequest(w, body.ChannelID, "channel_id")
	if !ok {
		return
	}

	var groupID pgtype.UUID
	if body.GroupID != nil && *body.GroupID != "" {
		var gOk bool
		groupID, gOk = parseUUIDOrBadRequest(w, *body.GroupID, "group_id")
		if !gOk {
			return
		}
	}

	position := body.Position
	if position == 0 {
		maxPos, err := h.Queries.GetMaxChannelPositionInGroup(r.Context(), db.GetMaxChannelPositionInGroupParams{
			WorkspaceID: wsUUID,
			GroupID:     groupID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get position")
			return
		}
		position = maxPos + 1
	}

	if err := h.Queries.MoveChannelToGroup(r.Context(), db.MoveChannelToGroupParams{
		ID:       channelUUID,
		GroupID:  groupID,
		Position: position,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to move channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
