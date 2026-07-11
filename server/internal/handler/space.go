package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type SpaceResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	Name         string  `json:"name"`
	Key          string  `json:"key"`
	Icon         *string `json:"icon"`
	Context      string  `json:"context"`
	IssueCounter int32   `json:"issue_counter"`
	IsDefault    bool    `json:"is_default"`
	Visibility   string  `json:"visibility"`
	ArchivedAt   *string `json:"archived_at"`
	CreatedBy    *string `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	// Requesting user's independent access and preference view. Membership
	// grants collaboration; pin/follow/reorder never do.
	IsMember   bool    `json:"is_member"`
	MemberRole *string `json:"member_role"`
	IsPinned   bool    `json:"is_pinned"`
	IsFollowed bool    `json:"is_followed"`
	SortOrder  float64 `json:"sort_order"`
}

type SpaceActivityResponse struct {
	ID             string          `json:"id"`
	ActorType      string          `json:"actor_type"`
	ActorID        string          `json:"actor_id"`
	ActorName      string          `json:"actor_name"`
	ActorAvatarURL string          `json:"actor_avatar_url,omitempty"`
	Action         string          `json:"action"`
	Details        json.RawMessage `json:"details"`
	CreatedAt      string          `json:"created_at"`
}

func spaceToResponse(t db.WorkspaceSpace) SpaceResponse {
	return SpaceResponse{
		ID:           uuidToString(t.ID),
		WorkspaceID:  uuidToString(t.WorkspaceID),
		Name:         t.Name,
		Key:          t.Key,
		Icon:         textToPtr(t.Icon),
		Context:      t.Context,
		IssueCounter: t.IssueCounter,
		IsDefault:    t.IsDefault,
		Visibility:   t.Visibility,
		ArchivedAt:   timestampToPtr(t.ArchivedAt),
		CreatedBy:    uuidToPtr(t.CreatedBy),
		CreatedAt:    timestampToString(t.CreatedAt),
		UpdatedAt:    timestampToString(t.UpdatedAt),
	}
}

func validSpaceRole(role string) bool {
	return role == "lead" || role == "admin" || role == "member" || role == "guest"
}

// taskTokenAllowedSpaces resolves and revalidates the authoritative Space
// allow-list stamped by Auth for a running Agent. Most tasks still carry one
// Space; an All-spaces Chat may carry several. Removing the Agent from a Space
// shrinks this set immediately without waiting for token expiry.
//
// Return values are (spaceIDs, isTaskToken, ok). An empty task-token set fails
// closed for every Space-backed endpoint.
func (h *Handler) taskTokenAllowedSpaces(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID) ([]pgtype.UUID, bool, bool) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		return nil, false, true
	}
	if r.Header.Get("X-Workspace-ID") != uuidToString(workspaceID) {
		writeError(w, http.StatusForbidden, "task token is not authorized for this workspace")
		return nil, true, false
	}
	rawIDs := strings.Split(r.Header.Get("X-Space-IDs"), ",")
	if len(rawIDs) == 1 && strings.TrimSpace(rawIDs[0]) == "" {
		rawIDs = nil
	}
	if len(rawIDs) == 0 && r.Header.Get("X-Space-ID") != "" {
		rawIDs = []string{r.Header.Get("X-Space-ID")}
	}
	if len(rawIDs) == 0 {
		writeError(w, http.StatusForbidden, "this task has no Space data access")
		return nil, true, false
	}
	agentID, err := util.ParseUUID(r.Header.Get("X-Agent-ID"))
	if err != nil {
		writeError(w, http.StatusForbidden, "invalid task identity")
		return nil, true, false
	}
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusForbidden, "invalid task identity")
		return nil, true, false
	}

	seen := make(map[string]struct{}, len(rawIDs))
	allowed := make([]pgtype.UUID, 0, len(rawIDs))
	isAllSpacesChat := r.Header.Get("X-Space-ID") == ""
	for _, raw := range rawIDs {
		spaceID, parseErr := util.ParseUUID(strings.TrimSpace(raw))
		if parseErr != nil {
			continue
		}
		key := uuidToString(spaceID)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if !service.AgentAvailableInSpace(r.Context(), h.Queries, agent, workspaceID, spaceID) {
			continue
		}
		// All-spaces Chat is the intersection of Agent Availability and the
		// initiating member's collaboration access. The task token's user_id is
		// that initiator for Chat runs, so membership removal also revokes access.
		if isAllSpacesChat {
			collaborates, collabErr := h.Queries.CanCollaborateInWorkspaceSpace(r.Context(), db.CanCollaborateInWorkspaceSpaceParams{
				WorkspaceID: workspaceID,
				ID:          spaceID,
				UserID:      parseUUID(requestUserID(r)),
			})
			if collabErr != nil || !collaborates {
				continue
			}
		}
		allowed = append(allowed, spaceID)
	}
	if len(allowed) == 0 {
		writeError(w, http.StatusForbidden, "agent no longer has access to this Chat context")
		return nil, true, false
	}
	return allowed, true, true
}

func (h *Handler) requireTaskTokenSpace(w http.ResponseWriter, r *http.Request, workspaceID, targetSpaceID pgtype.UUID) (bool, bool) {
	allowedSpaceIDs, isTaskToken, ok := h.taskTokenAllowedSpaces(w, r, workspaceID)
	if !isTaskToken {
		return false, true
	}
	if !ok {
		return true, false
	}
	for _, allowed := range allowedSpaceIDs {
		if allowed == targetSpaceID {
			return true, true
		}
	}
	if targetSpaceID.Valid {
		writeError(w, http.StatusForbidden, "task token cannot access another Space")
	}
	return true, false
}

// taskTokenSpaceFilter keeps list/search endpoints on one concrete Space. A
// single-Space task gets its bound Space automatically. An All-spaces Chat
// must name a Space explicitly, which lets the Agent iterate the visible
// contexts without any unfiltered workspace-wide query becoming a data leak.
func (h *Handler) taskTokenSpaceFilter(w http.ResponseWriter, r *http.Request, workspaceID, requested pgtype.UUID) (pgtype.UUID, bool) {
	allowedSpaceIDs, isTaskToken, ok := h.taskTokenAllowedSpaces(w, r, workspaceID)
	if !ok {
		return pgtype.UUID{}, false
	}
	if !isTaskToken {
		return requested, true
	}
	if requested.Valid {
		for _, allowed := range allowedSpaceIDs {
			if requested == allowed {
				return requested, true
			}
		}
		writeError(w, http.StatusForbidden, "task token cannot access another Space")
		return pgtype.UUID{}, false
	}
	if len(allowedSpaceIDs) == 1 {
		return allowedSpaceIDs[0], true
	}
	writeError(w, http.StatusBadRequest, "space_id is required when this Chat can access multiple Spaces")
	return pgtype.UUID{}, false
}

func (h *Handler) requireSpaceView(w http.ResponseWriter, r *http.Request, workspaceID, spaceID pgtype.UUID) bool {
	if isTaskToken, ok := h.requireTaskTokenSpace(w, r, workspaceID, spaceID); isTaskToken {
		return ok
	}
	allowed, err := h.Queries.CanViewWorkspaceSpace(r.Context(), db.CanViewWorkspaceSpaceParams{
		WorkspaceID: workspaceID,
		ID:          spaceID,
		UserID:      parseUUID(requestUserID(r)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check space access")
		return false
	}
	if !allowed {
		writeError(w, http.StatusNotFound, "space not found")
		return false
	}
	return true
}

func (h *Handler) requireSpaceCollaboration(w http.ResponseWriter, r *http.Request, workspaceID, spaceID pgtype.UUID) bool {
	if isTaskToken, ok := h.requireTaskTokenSpace(w, r, workspaceID, spaceID); isTaskToken {
		return ok
	}
	allowed, err := h.Queries.CanCollaborateInWorkspaceSpace(r.Context(), db.CanCollaborateInWorkspaceSpaceParams{
		WorkspaceID: workspaceID,
		ID:          spaceID,
		UserID:      parseUUID(requestUserID(r)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check space access")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "join this space before changing its work")
		return false
	}
	return true
}

func (h *Handler) requireSpaceManagement(w http.ResponseWriter, r *http.Request, workspaceID, spaceID pgtype.UUID) bool {
	if r.Header.Get("X-Actor-Source") == "task_token" {
		writeError(w, http.StatusForbidden, "Space management is only available to human actors")
		return false
	}
	allowed, err := h.Queries.CanManageWorkspaceSpace(r.Context(), db.CanManageWorkspaceSpaceParams{
		WorkspaceID: workspaceID,
		ID:          spaceID,
		UserID:      parseUUID(requestUserID(r)),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check space access")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "space lead or admin access is required")
		return false
	}
	return true
}

// requireActiveSpaceMutation keeps an archived Space read-only while still
// allowing requireSpaceManagement to authorize the explicit restore path.
func (h *Handler) requireActiveSpaceMutation(w http.ResponseWriter, r *http.Request, workspaceID, spaceID pgtype.UUID) bool {
	space, err := h.Queries.GetWorkspaceSpace(r.Context(), db.GetWorkspaceSpaceParams{
		ID:          spaceID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "space not found")
		return false
	}
	if space.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "restore the Space before changing it")
		return false
	}
	return true
}

type CreateSpaceRequest struct {
	Name       string  `json:"name"`
	Key        string  `json:"key"`
	Icon       *string `json:"icon"`
	Visibility *string `json:"visibility"`
	Context    *string `json:"context"`
	// MemberIDs invites workspace members into the new space alongside the
	// creator (who always joins as lead). Open Spaces can also be joined later;
	// Private Spaces remain invitation-only.
	MemberIDs []string `json:"member_ids"`
}

type UpdateSpaceRequest struct {
	Name       *string `json:"name"`
	Key        *string `json:"key"`
	Icon       *string `json:"icon"`
	Visibility *string `json:"visibility"`
	Context    *string `json:"context"`
}

func (h *Handler) ListSpaces(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	allowedSpaceIDs, isTaskToken, ok := h.taskTokenAllowedSpaces(w, r, wsUUID)
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
	resp := make([]SpaceResponse, 0, len(rows))
	allowedSet := make(map[string]struct{}, len(allowedSpaceIDs))
	for _, id := range allowedSpaceIDs {
		allowedSet[uuidToString(id)] = struct{}{}
	}
	for _, row := range rows {
		if isTaskToken {
			if _, allowed := allowedSet[uuidToString(row.WorkspaceSpace.ID)]; !allowed {
				continue
			}
		}
		item := spaceToResponse(row.WorkspaceSpace)
		item.IsMember = row.IsMember
		item.MemberRole = textToPtr(row.MemberRole)
		item.IsPinned = row.IsPinned
		item.IsFollowed = row.IsFollowed
		item.SortOrder = row.MemberSortOrder
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": resp, "total": len(resp)})
}

// ListSpaceActivity exposes the Space governance trail in Space Settings.
// Visibility follows the Space itself: open Spaces are visible to Workspace
// members, while Private Space activity remains restricted to its members.
func (h *Handler) ListSpaceActivity(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	spaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "space id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetWorkspaceSpace(r.Context(), db.GetWorkspaceSpaceParams{
		ID: spaceID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}
	if !h.requireSpaceView(w, r, wsUUID, spaceID) {
		return
	}

	rows, err := h.Queries.ListSpaceLifecycleActivities(r.Context(), db.ListSpaceLifecycleActivitiesParams{
		WorkspaceID: wsUUID,
		SpaceID:     uuidToString(spaceID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list Space activity")
		return
	}
	activities := make([]SpaceActivityResponse, 0, len(rows))
	for _, row := range rows {
		activities = append(activities, SpaceActivityResponse{
			ID:             uuidToString(row.ID),
			ActorType:      row.ActorType.String,
			ActorID:        uuidToString(row.ActorID),
			ActorName:      row.ActorName,
			ActorAvatarURL: row.ActorAvatarUrl,
			Action:         row.Action,
			Details:        row.Details,
			CreatedAt:      timestampToString(row.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": activities})
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
		writeError(w, http.StatusBadRequest, "identifier must match ^[A-Z][A-Z0-9]{0,6}$ and must not be a reserved word (e.g. NEW)")
		return
	}
	visibility := "open"
	if req.Visibility != nil {
		visibility = strings.ToLower(strings.TrimSpace(*req.Visibility))
	}
	if visibility != "open" && visibility != "private" {
		writeError(w, http.StatusBadRequest, "visibility must be 'open' or 'private'")
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
		Icon:        ptrToText(req.Icon),
		Visibility:  pgtype.Text{String: visibility, Valid: true},
		Context:     ptrToText(req.Context),
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
	leadRole := "lead"
	resp.MemberRole = &leadRole
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

// ReplaceSpaceMembers sets a space's member list wholesale. Space leads/admins
// and workspace admins may manage it. Kept roles survive; added members join
// with the member role. At least one lead/admin must remain.
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
	if !h.requireSpaceManagement(w, r, wsUUID, spaceID) {
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
	remainingManagers := 0
	for _, m := range current {
		currentSet[m.UserID] = struct{}{}
		if _, keep := next[m.UserID]; keep && (m.Role == "lead" || m.Role == "admin") {
			remainingManagers++
		}
	}
	if remainingManagers == 0 {
		writeError(w, http.StatusConflict, "a space must keep at least one lead or admin")
		return
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
	if !h.requireSpaceView(w, r, wsUUID, spaceID) {
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

func (h *Handler) JoinSpace(w http.ResponseWriter, r *http.Request) {
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
	space, err := h.Queries.GetWorkspaceSpace(r.Context(), db.GetWorkspaceSpaceParams{
		ID:          spaceID,
		WorkspaceID: wsUUID,
	})
	if err != nil || space.ArchivedAt.Valid {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}
	if space.Visibility != "open" {
		writeError(w, http.StatusForbidden, "private spaces are invitation-only")
		return
	}
	userUUID := parseUUID(userID)
	if _, err := h.Queries.GetWorkspaceSpaceMember(r.Context(), db.GetWorkspaceSpaceMemberParams{
		SpaceID: spaceID,
		UserID:  userUUID,
	}); err == nil {
		writeJSON(w, http.StatusOK, h.withCallerMembership(r.Context(), spaceToResponse(space), spaceID, userID))
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join space")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := addSpaceMember(r.Context(), qtx, wsUUID, spaceID, userUUID, "member"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join space")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join space")
		return
	}
	resp := h.withCallerMembership(r.Context(), spaceToResponse(space), spaceID, userID)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"space": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) LeaveSpace(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	spaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "space id")
	if !ok {
		return
	}
	if !h.requireActiveSpaceMutation(w, r, wsUUID, spaceID) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID := parseUUID(userID)
	membership, err := h.Queries.GetWorkspaceSpaceMember(r.Context(), db.GetWorkspaceSpaceMemberParams{
		SpaceID: spaceID,
		UserID:  userUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "you are not a member of this space")
		return
	}
	if membership.Role == "lead" || membership.Role == "admin" {
		count, err := h.Queries.CountWorkspaceSpaceManagers(r.Context(), db.CountWorkspaceSpaceManagersParams{
			WorkspaceID: wsUUID,
			SpaceID:     spaceID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to leave space")
			return
		}
		if count <= 1 {
			writeError(w, http.StatusConflict, "assign another space lead or admin before leaving")
			return
		}
	}
	if _, err := h.Queries.RemoveWorkspaceSpaceMember(r.Context(), db.RemoveWorkspaceSpaceMemberParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
		UserID:      userUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to leave space")
		return
	}
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"space_id": uuidToString(spaceID)})
	w.WriteHeader(http.StatusNoContent)
}

type UpdateSpaceMemberRoleRequest struct {
	Role string `json:"role"`
}

type UpdateSpacePreferenceRequest struct {
	IsPinned   *bool    `json:"is_pinned"`
	IsFollowed *bool    `json:"is_followed"`
	SortOrder  *float64 `json:"sort_order"`
}

type SpacePreferenceResponse struct {
	SpaceID    string  `json:"space_id"`
	IsPinned   bool    `json:"is_pinned"`
	IsFollowed bool    `json:"is_followed"`
	SortOrder  float64 `json:"sort_order"`
}

func (h *Handler) UpdateSpaceMemberRole(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	spaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "space id")
	if !ok {
		return
	}
	if !h.requireSpaceManagement(w, r, wsUUID, spaceID) {
		return
	}
	if !h.requireActiveSpaceMutation(w, r, wsUUID, spaceID) {
		return
	}
	targetUserID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "userId"), "user id")
	if !ok {
		return
	}
	var req UpdateSpaceMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	if !validSpaceRole(req.Role) {
		writeError(w, http.StatusBadRequest, "role must be 'lead', 'admin', 'member', or 'guest'")
		return
	}
	current, err := h.Queries.GetWorkspaceSpaceMember(r.Context(), db.GetWorkspaceSpaceMemberParams{
		SpaceID: spaceID,
		UserID:  targetUserID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "space member not found")
		return
	}
	if (current.Role == "lead" || current.Role == "admin") && req.Role != "lead" && req.Role != "admin" {
		count, err := h.Queries.CountWorkspaceSpaceManagers(r.Context(), db.CountWorkspaceSpaceManagersParams{
			WorkspaceID: wsUUID,
			SpaceID:     spaceID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update space role")
			return
		}
		if count <= 1 {
			writeError(w, http.StatusConflict, "a space must keep at least one lead or admin")
			return
		}
	}
	updated, err := h.Queries.UpdateWorkspaceSpaceMemberRole(r.Context(), db.UpdateWorkspaceSpaceMemberRoleParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
		UserID:      targetUserID,
		Role:        req.Role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update space role")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"space_id": uuidToString(updated.SpaceID),
		"user_id":  uuidToString(updated.UserID),
		"role":     updated.Role,
	})
}

func currentSpacePreference(
	ctx context.Context,
	q *db.Queries,
	workspaceID, spaceID, userID pgtype.UUID,
) (db.WorkspaceSpacePreference, error) {
	pref, err := q.GetWorkspaceSpacePreference(ctx, db.GetWorkspaceSpacePreferenceParams{
		WorkspaceID: workspaceID,
		SpaceID:     spaceID,
		UserID:      userID,
	})
	if err == nil {
		return pref, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.WorkspaceSpacePreference{}, err
	}

	sortOrder, err := q.NextSpacePreferenceSortOrder(ctx, db.NextSpacePreferenceSortOrderParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return db.WorkspaceSpacePreference{}, err
	}
	if membership, membershipErr := q.GetWorkspaceSpaceMember(ctx, db.GetWorkspaceSpaceMemberParams{
		SpaceID: spaceID,
		UserID:  userID,
	}); membershipErr == nil {
		sortOrder = membership.SortOrder
	}
	return db.WorkspaceSpacePreference{
		WorkspaceID: workspaceID,
		SpaceID:     spaceID,
		UserID:      userID,
		SortOrder:   sortOrder,
	}, nil
}

// UpdateSpacePreference changes personal navigation/notification state only.
// It never joins the Space and therefore never grants collaboration access.
func (h *Handler) UpdateSpacePreference(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireSpaceView(w, r, wsUUID, spaceID) {
		return
	}

	var req UpdateSpacePreferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IsPinned == nil && req.IsFollowed == nil && req.SortOrder == nil {
		writeError(w, http.StatusBadRequest, "at least one preference field is required")
		return
	}

	space, err := h.Queries.GetWorkspaceSpace(r.Context(), db.GetWorkspaceSpaceParams{
		ID:          spaceID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}
	if space.ArchivedAt.Valid && ((req.IsPinned != nil && *req.IsPinned) || (req.IsFollowed != nil && *req.IsFollowed)) {
		writeError(w, http.StatusConflict, "archived spaces cannot be pinned or followed")
		return
	}

	userUUID := parseUUID(userID)
	pref, err := currentSpacePreference(r.Context(), h.Queries, wsUUID, spaceID, userUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load space preferences")
		return
	}
	if req.IsPinned != nil {
		pref.IsPinned = *req.IsPinned
	}
	if req.IsFollowed != nil {
		pref.IsFollowed = *req.IsFollowed
	}
	if req.SortOrder != nil {
		pref.SortOrder = *req.SortOrder
	}
	pref, err = h.Queries.UpsertWorkspaceSpacePreference(r.Context(), db.UpsertWorkspaceSpacePreferenceParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
		UserID:      userUUID,
		IsPinned:    pref.IsPinned,
		IsFollowed:  pref.IsFollowed,
		SortOrder:   pref.SortOrder,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update space preferences")
		return
	}
	writeJSON(w, http.StatusOK, SpacePreferenceResponse{
		SpaceID:    uuidToString(pref.SpaceID),
		IsPinned:   pref.IsPinned,
		IsFollowed: pref.IsFollowed,
		SortOrder:  pref.SortOrder,
	})
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
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update space order")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	m, err := qtx.UpdateSpaceMemberSortOrder(r.Context(), db.UpdateSpaceMemberSortOrderParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
		UserID:      parseUUID(userID),
		SortOrder:   *req.SortOrder,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "you are not a member of this space")
		return
	}
	pref, err := currentSpacePreference(r.Context(), qtx, wsUUID, spaceID, parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update space order")
		return
	}
	if _, err := qtx.UpsertWorkspaceSpacePreference(r.Context(), db.UpsertWorkspaceSpacePreferenceParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
		UserID:      parseUUID(userID),
		IsPinned:    pref.IsPinned,
		IsFollowed:  pref.IsFollowed,
		SortOrder:   m.SortOrder,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update space order")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update space order")
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
	if !h.requireSpaceManagement(w, r, wsUUID, spaceID) {
		return
	}
	if !h.requireActiveSpaceMutation(w, r, wsUUID, spaceID) {
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
			writeError(w, http.StatusBadRequest, "identifier must match ^[A-Z][A-Z0-9]{0,6}$ and must not be a reserved word (e.g. NEW)")
			return
		}
		params.Key = pgtype.Text{String: key, Valid: true}
	}
	if req.Icon != nil {
		params.Icon = pgtype.Text{String: *req.Icon, Valid: true}
	}
	if req.Visibility != nil {
		visibility := strings.ToLower(strings.TrimSpace(*req.Visibility))
		if visibility != "open" && visibility != "private" {
			writeError(w, http.StatusBadRequest, "visibility must be 'open' or 'private'")
			return
		}
		current, err := h.Queries.GetWorkspaceSpace(r.Context(), db.GetWorkspaceSpaceParams{
			ID:          spaceID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "space not found")
			return
		}
		if current.IsDefault && visibility == "private" {
			writeError(w, http.StatusConflict, "the workspace default space must remain open")
			return
		}
		params.Visibility = pgtype.Text{String: visibility, Valid: true}
	}
	if req.Context != nil {
		params.Context = pgtype.Text{String: *req.Context, Valid: true}
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

// withCallerMembership stamps the caller's membership and personal preference
// view onto a single-Space response so mutations do not clobber Sidebar state.
func (h *Handler) withCallerMembership(ctx context.Context, resp SpaceResponse, spaceID pgtype.UUID, userID string) SpaceResponse {
	userUUID := parseUUID(userID)
	m, err := h.Queries.GetWorkspaceSpaceMember(ctx, db.GetWorkspaceSpaceMemberParams{
		SpaceID: spaceID,
		UserID:  userUUID,
	})
	if err == nil {
		resp.IsMember = true
		resp.MemberRole = &m.Role
		resp.SortOrder = m.SortOrder
	}
	pref, err := h.Queries.GetWorkspaceSpacePreference(ctx, db.GetWorkspaceSpacePreferenceParams{
		WorkspaceID: parseUUID(resp.WorkspaceID),
		SpaceID:     spaceID,
		UserID:      userUUID,
	})
	if err == nil {
		resp.IsPinned = pref.IsPinned
		resp.IsFollowed = pref.IsFollowed
		resp.SortOrder = pref.SortOrder
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
	// When the identifier changes on a space that already holds issues, every
	// existing OLDKEY-N would stop resolving (identifiers are derived from the
	// space key at read time). Record an alias per issue under the old key
	// first, so GitHub/CLI/link references keep landing on the issue.
	if params.Key.Valid && params.Key.String != locked.Key && locked.IssueCounter > 0 {
		if err := qtx.BackfillSpaceKeyAliases(ctx, db.BackfillSpaceKeyAliasesParams{
			WorkspaceID:   params.WorkspaceID,
			SpaceID:       params.ID,
			SpaceKeyLower: strings.ToLower(locked.Key),
		}); err != nil {
			return db.WorkspaceSpace{}, fmt.Errorf("backfill identifier aliases: %w", err)
		}
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
	if !h.requireSpaceManagement(w, r, wsUUID, spaceID) {
		return
	}
	space, err := h.Queries.GetWorkspaceSpace(r.Context(), db.GetWorkspaceSpaceParams{
		ID:          spaceID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}
	if space.IsDefault {
		writeError(w, http.StatusConflict, "change the workspace default space before archiving this space")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive space")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// No space is permanently protected — instead every workspace must always
	// keep at least one active Space. FOR UPDATE locks every active Space row
	// for this workspace so a concurrent archive on a different Space in the
	// same workspace serializes behind this one; without the lock, two
	// concurrent archives of the last two Spaces could both read "2 active"
	// and both proceed, leaving zero.
	active, err := qtx.ListActiveWorkspaceSpacesForUpdate(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate space usage")
		return
	}
	if len(active) <= 1 {
		writeError(w, http.StatusConflict, "cannot archive the last active space in a workspace")
		return
	}
	// A Space owns its operating units. Archive Squads without transferring
	// assignments, and pause only currently-active Autopilots. Lifecycle marker
	// columns let restore distinguish these changes from resources that users
	// had already archived or paused themselves.
	archivedSquadCount, err := qtx.ArchiveSquadsBySpace(r.Context(), db.ArchiveSquadsBySpaceParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
		ArchivedBy:  parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive Space Squads")
		return
	}
	pausedAutopilotCount, err := qtx.PauseActiveAutopilotsBySpace(r.Context(), db.PauseActiveAutopilotsBySpaceParams{
		WorkspaceID: wsUUID,
		SpaceID:     spaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to pause Space Autopilots")
		return
	}

	space, err = qtx.ArchiveWorkspaceSpace(r.Context(), db.ArchiveWorkspaceSpaceParams{
		ID:          spaceID,
		WorkspaceID: wsUUID,
		ArchivedBy:  parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "space cannot be archived")
		return
	}
	auditDetails, _ := json.Marshal(map[string]any{
		"space_id":               uuidToString(space.ID),
		"space_name":             space.Name,
		"archived_squad_count":   archivedSquadCount,
		"paused_autopilot_count": pausedAutopilotCount,
	})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: wsUUID, IssueID: pgtype.UUID{},
		ActorType: pgtype.Text{String: "member", Valid: true}, ActorID: parseUUID(userID),
		Action: "space_archived", Details: auditDetails,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit space archive")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive space")
		return
	}
	resp := h.withCallerMembership(r.Context(), spaceToResponse(space), space.ID, userID)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"space": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RestoreSpace(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireSpaceManagement(w, r, wsUUID, spaceID) {
		return
	}
	current, err := h.Queries.GetWorkspaceSpace(r.Context(), db.GetWorkspaceSpaceParams{
		ID: spaceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "space not found")
		return
	}
	if !current.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "space is not archived")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore space")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	space, err := qtx.RestoreWorkspaceSpace(r.Context(), db.RestoreWorkspaceSpaceParams{
		ID: spaceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "space cannot be restored")
		return
	}
	restoredSquadCount, err := qtx.RestoreSquadsArchivedBySpace(r.Context(), db.RestoreSquadsArchivedBySpaceParams{
		WorkspaceID: wsUUID, SpaceID: spaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore Space Squads")
		return
	}
	pausedCount, err := qtx.CountAutopilotsPausedBySpace(r.Context(), db.CountAutopilotsPausedBySpaceParams{
		WorkspaceID: wsUUID, SpaceID: spaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect Space Autopilots")
		return
	}
	auditDetails, _ := json.Marshal(map[string]any{
		"space_id":                         uuidToString(space.ID),
		"space_name":                       space.Name,
		"restored_squad_count":             restoredSquadCount,
		"autopilots_awaiting_confirmation": pausedCount,
	})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: wsUUID, IssueID: pgtype.UUID{},
		ActorType: pgtype.Text{String: "member", Valid: true}, ActorID: parseUUID(userID),
		Action: "space_restored", Details: auditDetails,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit space restore")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore space")
		return
	}
	resp := h.withCallerMembership(r.Context(), spaceToResponse(space), space.ID, userID)
	h.publish(protocol.EventWorkspaceUpdated, workspaceID, "member", userID, map[string]any{"space": resp})
	writeJSON(w, http.StatusOK, map[string]any{
		"space":                  resp,
		"paused_autopilot_count": pausedCount,
	})
}

func (h *Handler) ResumeSpaceAutopilots(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	spaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "space id")
	if !ok {
		return
	}
	if !h.requireSpaceManagement(w, r, wsUUID, spaceID) {
		return
	}
	space, err := h.Queries.GetWorkspaceSpace(r.Context(), db.GetWorkspaceSpaceParams{
		ID: spaceID, WorkspaceID: wsUUID,
	})
	if err != nil || space.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "restore the Space before resuming Autopilots")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resume Space Autopilots")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	count, err := qtx.ResumeAutopilotsPausedBySpace(r.Context(), db.ResumeAutopilotsPausedBySpaceParams{
		WorkspaceID: wsUUID, SpaceID: spaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resume Space Autopilots")
		return
	}
	auditDetails, _ := json.Marshal(map[string]any{
		"space_id":                uuidToString(space.ID),
		"space_name":              space.Name,
		"resumed_autopilot_count": count,
	})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: wsUUID, IssueID: pgtype.UUID{},
		ActorType: pgtype.Text{String: "member", Valid: true}, ActorID: parseUUID(userID),
		Action: "space_autopilots_resumed", Details: auditDetails,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to audit Autopilot resume")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resume Space Autopilots")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resumed_autopilot_count": count})
}
