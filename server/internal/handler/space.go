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

var errSpaceKeyFrozen = errors.New("space identifier cannot be changed after issues have been created")

type SpaceResponse struct {
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
	// spaces, ordered by SortOrder (per-user fractional position; the first
	// space doubles as the issue-creation default when no context applies).
	IsMember  bool    `json:"is_member"`
	SortOrder float64 `json:"sort_order"`
}

func spaceToResponse(t db.WorkspaceSpace) SpaceResponse {
	return SpaceResponse{
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

type CreateSpaceRequest struct {
	Name        string  `json:"name"`
	Key         string  `json:"key"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	// MemberIDs invites workspace members into the new space alongside the
	// creator (who always joins as lead). Minimal v1 membership loop —
	// there is no separate join/leave API yet, so a space is only visible in
	// its members' sidebars.
	MemberIDs []string `json:"member_ids"`
}

type UpdateSpaceRequest struct {
	Name        *string `json:"name"`
	Key         *string `json:"key"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
}

func (h *Handler) ListSpaces(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListWorkspaceSpacesForUser(r.Context(), db.ListWorkspaceSpacesForUserParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list spaces")
		return
	}
	resp := make([]SpaceResponse, len(rows))
	for i, row := range rows {
		resp[i] = spaceToResponse(row.WorkspaceSpace)
		resp[i].IsMember = row.IsMember
		resp[i].SortOrder = row.MemberSortOrder
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": resp, "total": len(resp)})
}

func (h *Handler) CreateSpace(w http.ResponseWriter, r *http.Request) {
	var req CreateSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	key := normalizeSpaceKey(req.Key)
	if key == "" {
		key = defaultSpaceKeyFromSlug(req.Name)
	}
	if !validSpaceKey(key) {
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
		writeError(w, http.StatusInternalServerError, "failed to create space")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	space, err := qtx.CreateWorkspaceSpace(r.Context(), db.CreateWorkspaceSpaceParams{
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
			writeError(w, http.StatusBadRequest, "space identifier is invalid or already used")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create space")
		return
	}
	// The creator always joins as lead — a space invisible in its creator's
	// own sidebar would be unreachable (sidebar shows joined spaces only).
	creatorSort, err := addSpaceMember(r.Context(), qtx, wsUUID, space.ID, creatorUUID, "lead")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add space members")
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
		if _, err := addSpaceMember(r.Context(), qtx, wsUUID, space.ID, uid, "member"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add space members")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create space")
		return
	}

	resp := spaceToResponse(space)
	resp.IsMember = true
	resp.SortOrder = creatorSort
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"space": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// addSpaceMember appends the user to the space at the end of their personal
// space order and returns the assigned sort position.
func addSpaceMember(ctx context.Context, q *db.Queries, wsUUID, spaceID, userID pgtype.UUID, role string) (float64, error) {
	sort, err := q.NextSpaceMemberSortOrder(ctx, db.NextSpaceMemberSortOrderParams{
		WorkspaceID: wsUUID,
		UserID:      userID,
	})
	if err != nil {
		return 0, err
	}
	if err := q.AddWorkspaceSpaceMember(ctx, db.AddWorkspaceSpaceMemberParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
		UserID:      userID,
		Role:        role,
		SortOrder:   sort,
	}); err != nil {
		return 0, err
	}
	return sort, nil
}

type UpdateSpaceMembershipRequest struct {
	SortOrder *float64 `json:"sort_order"`
}

type ReplaceSpaceMembersRequest struct {
	MemberIDs []string `json:"member_ids"`
}

// ReplaceSpaceMembers sets a space's member list wholesale. Anyone in the
// workspace may configure any space's members — membership only drives the
// sidebar and personal defaults, never access, so there is no extra
// permission layer. Kept rows are untouched (their sort_order and role
// survive); added members land at the end of their own personal order. An
// empty list is rejected: zero members means "archive the space", which the
// client performs explicitly via DELETE /api/spaces/{id} after a confirm.
func (h *Handler) ReplaceSpaceMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	spaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "space id")
	if !ok {
		return
	}
	var req ReplaceSpaceMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.MemberIDs) == 0 {
		writeError(w, http.StatusBadRequest, "empty membership archives the space — archive it instead")
		return
	}
	if _, err := service.ValidateActiveSpace(r.Context(), h.Queries, wsUUID, spaceID); err != nil {
		if !writeSpaceResolveError(w, err) {
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
		writeError(w, http.StatusInternalServerError, "failed to update space members")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	current, err := qtx.ListWorkspaceSpaceMembers(r.Context(), db.ListWorkspaceSpaceMembersParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load space members")
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
		if _, err := addSpaceMember(r.Context(), qtx, wsUUID, spaceID, uid, "member"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update space members")
			return
		}
	}
	for _, m := range current {
		if _, keep := next[m.UserID]; keep {
			continue
		}
		if _, err := qtx.RemoveWorkspaceSpaceMember(r.Context(), db.RemoveWorkspaceSpaceMemberParams{
			WorkspaceID: wsUUID,
			SpaceID:     spaceID,
			UserID:      m.UserID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update space members")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update space members")
		return
	}
	h.listSpaceMembersResponse(w, r, wsUUID, spaceID)
}

type SpaceMemberResponse struct {
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	Role      string  `json:"role"`
	CreatedAt string  `json:"created_at"`
}

// ListSpaceMembers lists a space's members with user display data. Membership
// is configured wholesale via ReplaceSpaceMembers.
func (h *Handler) ListSpaceMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	spaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "space id")
	if !ok {
		return
	}
	h.listSpaceMembersResponse(w, r, wsUUID, spaceID)
}

// listSpaceMembersResponse writes the members payload shared by the GET and
// the PUT (replace) endpoints.
func (h *Handler) listSpaceMembersResponse(w http.ResponseWriter, r *http.Request, wsUUID, spaceID pgtype.UUID) {
	rows, err := h.Queries.ListWorkspaceSpaceMembersWithUser(r.Context(), db.ListWorkspaceSpaceMembersWithUserParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list space members")
		return
	}
	resp := make([]SpaceMemberResponse, len(rows))
	for i, row := range rows {
		resp[i] = SpaceMemberResponse{
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

// UpdateSpaceMembership updates the caller's own membership row — currently
// just sort_order, the per-user sidebar position. Fractional: a drag sends
// the midpoint of the drop slot's neighbors, so single-row updates suffice.
func (h *Handler) UpdateSpaceMembership(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	spaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "space id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req UpdateSpaceMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SortOrder == nil {
		writeError(w, http.StatusBadRequest, "sort_order is required")
		return
	}
	m, err := h.Queries.UpdateSpaceMemberSortOrder(r.Context(), db.UpdateSpaceMemberSortOrderParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
		UserID:      parseUUID(userID),
		SortOrder:   *req.SortOrder,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "you are not a member of this space")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"space_id":   uuidToString(m.SpaceID),
		"sort_order": m.SortOrder,
	})
}

func (h *Handler) UpdateSpace(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	spaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "space id")
	if !ok {
		return
	}
	var req UpdateSpaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.UpdateWorkspaceSpaceParams{
		ID:          spaceID,
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
		key := normalizeSpaceKey(*req.Key)
		if !validSpaceKey(key) {
			writeError(w, http.StatusBadRequest, "identifier must match ^[A-Z][A-Z0-9]{0,6}$")
			return
		}
		// Key changes are admin-only: the key is the workspace-wide issue
		// identifier namespace, and the legacy workspace issue_prefix path
		// (admin-gated at the router) funnels into the same row — both
		// doors must carry the same gate.
		current, err := h.Queries.GetWorkspaceSpace(r.Context(), db.GetWorkspaceSpaceParams{
			ID:          spaceID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "space not found")
			return
		}
		if current.Key != key {
			member, ok := ctxMember(r.Context())
			if !ok || !roleAllowed(member.Role, "owner", "admin") {
				writeError(w, http.StatusForbidden, "only workspace admins can change the space key")
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

	space, err := updateWorkspaceSpaceLocked(r.Context(), qtx, params)
	if err != nil {
		if errors.Is(err, errSpaceKeyFrozen) {
			writeError(w, http.StatusConflict, "space identifier cannot be changed after issues have been created")
			return
		}
		if isUniqueViolation(err) || isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "space identifier is invalid or already used")
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "space not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update space")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit space update")
		return
	}
	resp := spaceToResponse(space)
	userID := requestUserID(r)
	resp = h.withCallerMembership(r.Context(), resp, space.ID, userID)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"space": resp})
	writeJSON(w, http.StatusOK, resp)
}

// withCallerMembership stamps the caller's membership view onto a single-space
// response so mutations don't clobber is_member/sort_order in the client's
// list cache (the list endpoint always carries them).
func (h *Handler) withCallerMembership(ctx context.Context, resp SpaceResponse, spaceID pgtype.UUID, userID string) SpaceResponse {
	m, err := h.Queries.GetWorkspaceSpaceMember(ctx, db.GetWorkspaceSpaceMemberParams{
		SpaceID: spaceID,
		UserID:  parseUUID(userID),
	})
	if err == nil {
		resp.IsMember = true
		resp.SortOrder = m.SortOrder
	}
	return resp
}

func updateWorkspaceSpaceLocked(ctx context.Context, qtx *db.Queries, params db.UpdateWorkspaceSpaceParams) (db.WorkspaceSpace, error) {
	locked, err := qtx.LockWorkspaceSpaceForKeyUpdate(ctx, db.LockWorkspaceSpaceForKeyUpdateParams{
		ID:          params.ID,
		WorkspaceID: params.WorkspaceID,
	})
	if err != nil {
		return db.WorkspaceSpace{}, err
	}
	if params.Key.Valid && params.Key.String != locked.Key && locked.IssueCounter > 0 {
		return db.WorkspaceSpace{}, errSpaceKeyFrozen
	}
	return qtx.UpdateWorkspaceSpace(ctx, params)
}

func (h *Handler) ArchiveSpace(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	spaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "space id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// Archiving is admin-only, matching the destructive-op convention
	// (project delete, squad delete). Everything else on a space stays
	// member-open — membership is not a permission layer.
	if member, ok := ctxMember(r.Context()); !ok || !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace admins can archive a space")
		return
	}
	// Block archiving a Space that still drives live autopilots — the SQL only
	// guards the default space, so without this an archived Space would leave
	// active autopilots pointing at a Space that can no longer receive work.
	// Existing issues are intentionally NOT a blocker: the default space always
	// has issues, and archived-space issues stay readable.
	activeAutopilots, err := h.Queries.CountActiveAutopilotsBySpace(r.Context(), db.CountActiveAutopilotsBySpaceParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate space usage")
		return
	}
	if activeAutopilots > 0 {
		writeError(w, http.StatusConflict, "cannot archive a space used by active autopilots")
		return
	}
	space, err := h.Queries.ArchiveWorkspaceSpace(r.Context(), db.ArchiveWorkspaceSpaceParams{
		ID:          spaceID,
		WorkspaceID: wsUUID,
		ArchivedBy:  parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "space cannot be archived")
		return
	}
	resp := h.withCallerMembership(r.Context(), spaceToResponse(space), space.ID, userID)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"space": resp})
	writeJSON(w, http.StatusOK, resp)
}
