package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var errTeamKeyFrozen = errors.New("team key cannot be changed after issues have been created")

type TeamResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	Name         string  `json:"name"`
	Key          string  `json:"key"`
	Description  string  `json:"description"`
	Icon         *string `json:"icon"`
	IssueCounter int32   `json:"issue_counter"`
	IsDefault    bool    `json:"is_default"`
	ArchivedAt   *string `json:"archived_at"`
	CreatedBy    *string `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func teamToResponse(t db.WorkspaceTeam) TeamResponse {
	return TeamResponse{
		ID:           uuidToString(t.ID),
		WorkspaceID:  uuidToString(t.WorkspaceID),
		Name:         t.Name,
		Key:          t.Key,
		Description:  t.Description,
		Icon:         textToPtr(t.Icon),
		IssueCounter: t.IssueCounter,
		IsDefault:    t.IsDefault,
		ArchivedAt:   timestampToPtr(t.ArchivedAt),
		CreatedBy:    uuidToPtr(t.CreatedBy),
		CreatedAt:    timestampToString(t.CreatedAt),
		UpdatedAt:    timestampToString(t.UpdatedAt),
	}
}

type CreateTeamRequest struct {
	Name        string  `json:"name"`
	Key         string  `json:"key"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
}

type UpdateTeamRequest struct {
	Name        *string `json:"name"`
	Key         *string `json:"key"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
}

func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	teams, err := h.Queries.ListWorkspaceTeams(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list teams")
		return
	}
	resp := make([]TeamResponse, len(teams))
	for i, team := range teams {
		resp[i] = teamToResponse(team)
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": resp, "total": len(resp)})
}

func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	var req CreateTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	key := normalizeTeamKey(req.Key)
	if key == "" {
		key = defaultTeamKeyFromSlug(req.Name)
	}
	if !validTeamKey(key) {
		writeError(w, http.StatusBadRequest, "key must match ^[A-Z][A-Z0-9]{0,6}$")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	team, err := h.Queries.CreateWorkspaceTeam(r.Context(), db.CreateWorkspaceTeamParams{
		WorkspaceID: wsUUID,
		Name:        req.Name,
		Key:         key,
		IsDefault:   false,
		Description: ptrToText(req.Description),
		Icon:        ptrToText(req.Icon),
		CreatedBy:   parseUUID(userID),
	})
	if err != nil {
		if isUniqueViolation(err) || isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "team key is invalid or already used")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create team")
		return
	}
	resp := teamToResponse(team)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"team": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	teamID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "team id")
	if !ok {
		return
	}
	var req UpdateTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.UpdateWorkspaceTeamParams{
		ID:          teamID,
		WorkspaceID: wsUUID,
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Key != nil {
		key := normalizeTeamKey(*req.Key)
		if !validTeamKey(key) {
			writeError(w, http.StatusBadRequest, "key must match ^[A-Z][A-Z0-9]{0,6}$")
			return
		}
		params.Key = pgtype.Text{String: key, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Icon != nil {
		params.Icon = pgtype.Text{String: *req.Icon, Valid: true}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	team, err := updateWorkspaceTeamLocked(r.Context(), qtx, params)
	if err != nil {
		if errors.Is(err, errTeamKeyFrozen) {
			writeError(w, http.StatusConflict, "team key cannot be changed after issues have been created")
			return
		}
		if isUniqueViolation(err) || isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "team key is invalid or already used")
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "team not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update team")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit team update")
		return
	}
	resp := teamToResponse(team)
	userID := requestUserID(r)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"team": resp})
	writeJSON(w, http.StatusOK, resp)
}

func updateWorkspaceTeamLocked(ctx context.Context, qtx *db.Queries, params db.UpdateWorkspaceTeamParams) (db.WorkspaceTeam, error) {
	locked, err := qtx.LockWorkspaceTeamForKeyUpdate(ctx, db.LockWorkspaceTeamForKeyUpdateParams{
		ID:          params.ID,
		WorkspaceID: params.WorkspaceID,
	})
	if err != nil {
		return db.WorkspaceTeam{}, err
	}
	if params.Key.Valid && params.Key.String != locked.Key && locked.IssueCounter > 0 {
		return db.WorkspaceTeam{}, errTeamKeyFrozen
	}
	return qtx.UpdateWorkspaceTeam(ctx, params)
}

func (h *Handler) ArchiveTeam(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	teamID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "team id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// Block archiving a Team that still drives live autopilots — the SQL only
	// guards the default team, so without this an archived Team would leave
	// active autopilots pointing at a Team that can no longer receive work.
	// Existing issues are intentionally NOT a blocker: the default team always
	// has issues, and archived-team issues stay readable.
	activeAutopilots, err := h.Queries.CountActiveAutopilotsByTeam(r.Context(), db.CountActiveAutopilotsByTeamParams{
		WorkspaceID: wsUUID,
		TeamID:      teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate team usage")
		return
	}
	if activeAutopilots > 0 {
		writeError(w, http.StatusConflict, "cannot archive a team used by active autopilots")
		return
	}
	team, err := h.Queries.ArchiveWorkspaceTeam(r.Context(), db.ArchiveWorkspaceTeamParams{
		ID:          teamID,
		WorkspaceID: wsUUID,
		ArchivedBy:  parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "team cannot be archived")
		return
	}
	resp := teamToResponse(team)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"team": resp})
	writeJSON(w, http.StatusOK, resp)
}
