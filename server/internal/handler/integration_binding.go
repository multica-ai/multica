package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type integrationConnectionBindingResponse struct {
	Provider     string   `json:"provider"`
	ConnectionID string   `json:"connection_id"`
	DisplayName  string   `json:"display_name"`
	Status       string   `json:"status"`
	SpaceIDs     []string `json:"space_ids"`
}

func (h *Handler) listIntegrationBindingResponse(r *http.Request, workspaceID pgtype.UUID) ([]integrationConnectionBindingResponse, error) {
	connections, err := h.Queries.ListBindableIntegrationConnections(r.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	bindings, err := h.Queries.ListIntegrationSpaceBindings(r.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	byConnection := make(map[string][]string, len(connections))
	for _, binding := range bindings {
		key := binding.Provider + ":" + uuidToString(binding.ConnectionID)
		byConnection[key] = append(byConnection[key], uuidToString(binding.SpaceID))
	}
	resp := make([]integrationConnectionBindingResponse, len(connections))
	for i, connection := range connections {
		id := uuidToString(connection.ConnectionID)
		spaceIDs := byConnection[connection.Provider+":"+id]
		if spaceIDs == nil {
			spaceIDs = []string{}
		}
		resp[i] = integrationConnectionBindingResponse{
			Provider: connection.Provider, ConnectionID: id,
			DisplayName: connection.DisplayName, Status: connection.Status,
			SpaceIDs: spaceIDs,
		}
	}
	return resp, nil
}

func (h *Handler) ListIntegrationBindings(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	resp, err := h.listIntegrationBindingResponse(r, parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list Integration bindings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connections": resp,
		"can_manage":  roleAllowed(member.Role, "owner", "admin"),
	})
}

func (h *Handler) ReplaceIntegrationBindings(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	provider := chi.URLParam(r, "provider")
	if provider != "github" && provider != "slack" && provider != "feishu" {
		writeError(w, http.StatusBadRequest, "unsupported Integration provider")
		return
	}
	connectionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connectionId"), "connection id")
	if !ok {
		return
	}
	wsUUID := parseUUID(workspaceID)
	exists, err := h.Queries.IntegrationConnectionExists(r.Context(), db.IntegrationConnectionExistsParams{
		Provider: provider, ConnectionID: connectionID, WorkspaceID: wsUUID,
	})
	if err != nil || !exists {
		writeError(w, http.StatusNotFound, "Integration connection not found")
		return
	}
	var req struct {
		SpaceIDs []string `json:"space_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	spaceIDs := make([]pgtype.UUID, 0, len(req.SpaceIDs))
	seen := make(map[pgtype.UUID]struct{}, len(req.SpaceIDs))
	for _, raw := range req.SpaceIDs {
		id, ok := parseUUIDOrBadRequest(w, raw, "space id")
		if !ok {
			return
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		spaceIDs = append(spaceIDs, id)
	}
	if len(spaceIDs) > 0 {
		spaces, err := h.Queries.ListWorkspaceSpacesByIDs(r.Context(), db.ListWorkspaceSpacesByIDsParams{
			WorkspaceID: wsUUID, SpaceIds: spaceIDs,
		})
		if err != nil || len(spaces) != len(spaceIDs) {
			writeError(w, http.StatusBadRequest, "every Space must belong to this workspace")
			return
		}
		for _, space := range spaces {
			if space.ArchivedAt.Valid {
				writeError(w, http.StatusBadRequest, "archived Spaces cannot receive Integration bindings")
				return
			}
		}
	}
	previousBindings, err := h.Queries.ListIntegrationSpaceBindings(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect Integration bindings")
		return
	}
	previousSpaceIDs := make([]string, 0)
	affectedSpaceIDs := make([]string, 0)
	affectedSeen := make(map[string]struct{})
	for _, binding := range previousBindings {
		if binding.Provider != provider || binding.ConnectionID != connectionID {
			continue
		}
		id := uuidToString(binding.SpaceID)
		previousSpaceIDs = append(previousSpaceIDs, id)
		affectedSpaceIDs = append(affectedSpaceIDs, id)
		affectedSeen[id] = struct{}{}
	}
	newSpaceIDs := make([]string, 0, len(spaceIDs))
	for _, spaceID := range spaceIDs {
		id := uuidToString(spaceID)
		newSpaceIDs = append(newSpaceIDs, id)
		if _, exists := affectedSeen[id]; !exists {
			affectedSpaceIDs = append(affectedSpaceIDs, id)
			affectedSeen[id] = struct{}{}
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update Integration bindings")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteIntegrationSpaceBindings(r.Context(), db.DeleteIntegrationSpaceBindingsParams{
		WorkspaceID: wsUUID, Provider: provider, ConnectionID: connectionID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update Integration bindings")
		return
	}
	for _, spaceID := range spaceIDs {
		if err := qtx.CreateIntegrationSpaceBinding(r.Context(), db.CreateIntegrationSpaceBindingParams{
			WorkspaceID: wsUUID, Provider: provider, ConnectionID: connectionID,
			SpaceID: spaceID, CreatedBy: member.UserID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update Integration bindings")
			return
		}
	}
	auditDetails, _ := json.Marshal(map[string]any{
		"provider":           provider,
		"connection_id":      uuidToString(connectionID),
		"previous_space_ids": previousSpaceIDs,
		"space_ids":          newSpaceIDs,
		"affected_space_ids": affectedSpaceIDs,
	})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: wsUUID, IssueID: pgtype.UUID{},
		ActorType: pgtype.Text{String: "member", Valid: true}, ActorID: member.UserID,
		Action: "integration_space_bindings_replaced", Details: auditDetails,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit Integration bindings")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update Integration bindings")
		return
	}
	resp, err := h.listIntegrationBindingResponse(r, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Integration bindings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": resp, "can_manage": true})
}
