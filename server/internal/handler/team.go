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
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var errTeamKeyFrozen = errors.New("team identifier cannot be changed after issues have been created")

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
	// Requesting user's membership view: the sidebar shows only joined
	// teams, ordered by SortOrder (per-user fractional position; the first
	// team doubles as the issue-creation default when no context applies).
	IsMember  bool    `json:"is_member"`
	SortOrder float64 `json:"sort_order"`
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
	// MemberIDs invites workspace members into the new team alongside the
	// creator (who always joins as lead). Minimal v1 membership loop —
	// there is no separate join/leave API yet, so a team is only visible in
	// its members' sidebars.
	MemberIDs []string `json:"member_ids"`
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
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListWorkspaceTeamsForUser(r.Context(), db.ListWorkspaceTeamsForUserParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list teams")
		return
	}
	resp := make([]TeamResponse, len(rows))
	for i, row := range rows {
		resp[i] = teamToResponse(row.WorkspaceTeam)
		resp[i].IsMember = row.IsMember
		resp[i].SortOrder = row.MemberSortOrder
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
		writeError(w, http.StatusBadRequest, "identifier must match ^[A-Z][A-Z0-9]{0,6}$")
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
	memberIDs := make([]pgtype.UUID, 0, len(req.MemberIDs))
	creatorUUID := parseUUID(userID)
	for _, raw := range req.MemberIDs {
		uid, ok := parseUUIDOrBadRequest(w, raw, "member_ids")
		if !ok {
			return
		}
		if uid == creatorUUID {
			continue // creator joins as lead below
		}
		memberIDs = append(memberIDs, uid)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create team")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	team, err := qtx.CreateWorkspaceTeam(r.Context(), db.CreateWorkspaceTeamParams{
		WorkspaceID: wsUUID,
		Name:        req.Name,
		Key:         key,
		IsDefault:   false,
		Description: ptrToText(req.Description),
		Icon:        ptrToText(req.Icon),
		CreatedBy:   creatorUUID,
	})
	if err != nil {
		if isUniqueViolation(err) || isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "team identifier is invalid or already used")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create team")
		return
	}
	// The creator always joins as lead — a team invisible in its creator's
	// own sidebar would be unreachable (sidebar shows joined teams only).
	creatorSort, err := addTeamMember(r.Context(), qtx, wsUUID, team.ID, creatorUUID, "lead")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add team members")
		return
	}
	for _, uid := range memberIDs {
		if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      uid,
			WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "member_ids must be members of this workspace")
			return
		}
		if _, err := addTeamMember(r.Context(), qtx, wsUUID, team.ID, uid, "member"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add team members")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create team")
		return
	}

	resp := teamToResponse(team)
	resp.IsMember = true
	resp.SortOrder = creatorSort
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"team": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// addTeamMember appends the user to the team at the end of their personal
// team order and returns the assigned sort position.
func addTeamMember(ctx context.Context, q *db.Queries, wsUUID, teamID, userID pgtype.UUID, role string) (float64, error) {
	sort, err := q.NextTeamMemberSortOrder(ctx, db.NextTeamMemberSortOrderParams{
		WorkspaceID: wsUUID,
		UserID:      userID,
	})
	if err != nil {
		return 0, err
	}
	if err := q.AddWorkspaceTeamMember(ctx, db.AddWorkspaceTeamMemberParams{
		WorkspaceID: wsUUID,
		TeamID:      teamID,
		UserID:      userID,
		Role:        role,
		SortOrder:   sort,
	}); err != nil {
		return 0, err
	}
	return sort, nil
}

type UpdateTeamMembershipRequest struct {
	SortOrder *float64 `json:"sort_order"`
}

type ReplaceTeamMembersRequest struct {
	MemberIDs []string `json:"member_ids"`
}

// ReplaceTeamMembers sets a team's member list wholesale. Anyone in the
// workspace may configure any team's members — membership only drives the
// sidebar and personal defaults, never access, so there is no extra
// permission layer. Kept rows are untouched (their sort_order and role
// survive); added members land at the end of their own personal order. An
// empty list is rejected: zero members means "archive the team", which the
// client performs explicitly via DELETE /api/teams/{id} after a confirm.
func (h *Handler) ReplaceTeamMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	teamID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "team id")
	if !ok {
		return
	}
	var req ReplaceTeamMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.MemberIDs) == 0 {
		writeError(w, http.StatusBadRequest, "empty membership archives the team — archive it instead")
		return
	}
	if _, err := service.ValidateActiveTeam(r.Context(), h.Queries, wsUUID, teamID); err != nil {
		if !writeTeamResolveError(w, err) {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	next := make(map[pgtype.UUID]struct{}, len(req.MemberIDs))
	for _, raw := range req.MemberIDs {
		uid, ok := parseUUIDOrBadRequest(w, raw, "member_ids")
		if !ok {
			return
		}
		next[uid] = struct{}{}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update team members")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	current, err := qtx.ListWorkspaceTeamMembers(r.Context(), db.ListWorkspaceTeamMembersParams{
		WorkspaceID: wsUUID,
		TeamID:      teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load team members")
		return
	}
	currentSet := make(map[pgtype.UUID]struct{}, len(current))
	for _, m := range current {
		currentSet[m.UserID] = struct{}{}
	}
	for uid := range next {
		if _, kept := currentSet[uid]; kept {
			continue
		}
		if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      uid,
			WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "member_ids must be members of this workspace")
			return
		}
		if _, err := addTeamMember(r.Context(), qtx, wsUUID, teamID, uid, "member"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update team members")
			return
		}
	}
	for _, m := range current {
		if _, keep := next[m.UserID]; keep {
			continue
		}
		if _, err := qtx.RemoveWorkspaceTeamMember(r.Context(), db.RemoveWorkspaceTeamMemberParams{
			WorkspaceID: wsUUID,
			TeamID:      teamID,
			UserID:      m.UserID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update team members")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update team members")
		return
	}
	h.listTeamMembersResponse(w, r, wsUUID, teamID)
}

type TeamMemberResponse struct {
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	Role      string  `json:"role"`
	CreatedAt string  `json:"created_at"`
}

// ListTeamMembers lists a team's members with user display data. Membership
// is configured wholesale via ReplaceTeamMembers.
func (h *Handler) ListTeamMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	teamID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "team id")
	if !ok {
		return
	}
	h.listTeamMembersResponse(w, r, wsUUID, teamID)
}

// listTeamMembersResponse writes the members payload shared by the GET and
// the PUT (replace) endpoints.
func (h *Handler) listTeamMembersResponse(w http.ResponseWriter, r *http.Request, wsUUID, teamID pgtype.UUID) {
	rows, err := h.Queries.ListWorkspaceTeamMembersWithUser(r.Context(), db.ListWorkspaceTeamMembersWithUserParams{
		WorkspaceID: wsUUID,
		TeamID:      teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list team members")
		return
	}
	resp := make([]TeamMemberResponse, len(rows))
	for i, row := range rows {
		resp[i] = TeamMemberResponse{
			UserID:    uuidToString(row.UserID),
			Name:      row.UserName,
			Email:     row.UserEmail,
			AvatarURL: textToPtr(row.UserAvatarUrl),
			Role:      row.Role,
			CreatedAt: timestampToString(row.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": resp, "total": len(resp)})
}

// UpdateTeamMembership updates the caller's own membership row — currently
// just sort_order, the per-user sidebar position. Fractional: a drag sends
// the midpoint of the drop slot's neighbors, so single-row updates suffice.
func (h *Handler) UpdateTeamMembership(w http.ResponseWriter, r *http.Request) {
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
	var req UpdateTeamMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SortOrder == nil {
		writeError(w, http.StatusBadRequest, "sort_order is required")
		return
	}
	m, err := h.Queries.UpdateTeamMemberSortOrder(r.Context(), db.UpdateTeamMemberSortOrderParams{
		WorkspaceID: wsUUID,
		TeamID:      teamID,
		UserID:      parseUUID(userID),
		SortOrder:   *req.SortOrder,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "you are not a member of this team")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"team_id":    uuidToString(m.TeamID),
		"sort_order": m.SortOrder,
	})
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
			writeError(w, http.StatusBadRequest, "identifier must match ^[A-Z][A-Z0-9]{0,6}$")
			return
		}
		// Key changes are admin-only: the key is the workspace-wide issue
		// identifier namespace, and the legacy workspace issue_prefix path
		// (admin-gated at the router) funnels into the same row — both
		// doors must carry the same gate.
		current, err := h.Queries.GetWorkspaceTeam(r.Context(), db.GetWorkspaceTeamParams{
			ID:          teamID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "team not found")
			return
		}
		if current.Key != key {
			member, ok := ctxMember(r.Context())
			if !ok || !roleAllowed(member.Role, "owner", "admin") {
				writeError(w, http.StatusForbidden, "only workspace admins can change the team key")
				return
			}
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
			writeError(w, http.StatusConflict, "team identifier cannot be changed after issues have been created")
			return
		}
		if isUniqueViolation(err) || isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "team identifier is invalid or already used")
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
	resp = h.withCallerMembership(r.Context(), resp, team.ID, userID)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"team": resp})
	writeJSON(w, http.StatusOK, resp)
}

// withCallerMembership stamps the caller's membership view onto a single-team
// response so mutations don't clobber is_member/sort_order in the client's
// list cache (the list endpoint always carries them).
func (h *Handler) withCallerMembership(ctx context.Context, resp TeamResponse, teamID pgtype.UUID, userID string) TeamResponse {
	m, err := h.Queries.GetWorkspaceTeamMember(ctx, db.GetWorkspaceTeamMemberParams{
		TeamID: teamID,
		UserID: parseUUID(userID),
	})
	if err == nil {
		resp.IsMember = true
		resp.SortOrder = m.SortOrder
	}
	return resp
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
	// Archiving is admin-only, matching the destructive-op convention
	// (project delete, squad delete). Everything else on a team stays
	// member-open — membership is not a permission layer.
	if member, ok := ctxMember(r.Context()); !ok || !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace admins can archive a team")
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
	resp := h.withCallerMembership(r.Context(), teamToResponse(team), team.ID, userID)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"team": resp})
	writeJSON(w, http.StatusOK, resp)
}
