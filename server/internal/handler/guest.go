package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// maxGuestProjects bounds one request; a guest with more projects than this
// is better served as a member.
const maxGuestProjects = 100

// guestsEnabled writes a 403 when new guests are switched off.
func (h *Handler) guestsEnabled(w http.ResponseWriter, r *http.Request) bool {
	if featureflags.WorkspaceGuestsEnabled(r.Context(), h.FeatureFlags) {
		return true
	}
	writeFeatureDisabled(w, "workspace_guests_disabled", "guests are not enabled")
	return false
}

// isGuest reports whether userID is a guest of the workspace. A failed lookup
// reads as not a guest: callers use it for a clearer error, while access
// itself is enforced by projectaccess and the agent invocation gate.
func (h *Handler) isGuest(r *http.Request, workspaceID, userID string) bool {
	if m, ok := ctxMember(r.Context()); ok && uuidToString(m.WorkspaceID) == workspaceID && uuidToString(m.UserID) == userID {
		return projectaccess.IsGuestRole(m.Role)
	}
	m, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	return err == nil && projectaccess.IsGuestRole(m.Role)
}

// mentionsAgent reports whether content @-mentions an agent or a squad, which
// would ask an agent to run.
func mentionsAgent(content string) bool {
	for _, m := range util.ParseMentions(content) {
		if m.Type == "agent" || m.Type == "squad" {
			return true
		}
	}
	return false
}

// guestAgentView is all of an agent a guest is shown: enough to render its
// name and avatar on the comments it wrote.
func guestAgentView(a AgentResponse) AgentResponse {
	return AgentResponse{
		ID:                    a.ID,
		WorkspaceID:           a.WorkspaceID,
		Name:                  a.Name,
		AvatarURL:             a.AvatarURL,
		ArchivedAt:            a.ArchivedAt,
		RuntimeConfig:         map[string]any{},
		ConversationStarters:  []AgentConversationStarter{},
		CustomArgs:            []string{},
		InvocationTargets:     []AgentInvocationTargetDTO{},
		Skills:                []AgentSkillSummary{},
		DisabledRuntimeSkills: []DisabledRuntimeSkill{},
	}
}

// parseGuestProjectIDs validates the projects a guest may see: at least one,
// each a project of this workspace.
func (h *Handler) parseGuestProjectIDs(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, raw []string) ([]pgtype.UUID, bool) {
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "a guest needs at least one project")
		return nil, false
	}
	if len(raw) > maxGuestProjects {
		writeError(w, http.StatusBadRequest, "too many projects")
		return nil, false
	}
	seen := map[[16]byte]bool{}
	ids := make([]pgtype.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := util.ParseUUID(s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid project id")
			return nil, false
		}
		if seen[id.Bytes] {
			continue
		}
		seen[id.Bytes] = true
		ids = append(ids, id)
	}
	found, err := h.Queries.ListWorkspaceProjectIDs(r.Context(), db.ListWorkspaceProjectIDsParams{
		WorkspaceID: workspaceID, ProjectIds: ids,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load projects")
		return nil, false
	}
	// The requester can only share a project they can see themselves.
	hidden, ok := h.requestHiddenProjects(w, r, uuidToString(workspaceID))
	if !ok {
		return nil, false
	}
	visible := 0
	for _, id := range found {
		if hidden.CanSeeProject(id) {
			visible++
		}
	}
	if visible != len(ids) {
		writeError(w, http.StatusBadRequest, "project not found")
		return nil, false
	}
	return ids, true
}

// setGuestProjects makes ids exactly the projects userID is a member of.
func setGuestProjects(r *http.Request, q *db.Queries, workspaceID, userID, addedBy pgtype.UUID, ids []pgtype.UUID) error {
	if _, err := q.RemoveUserFromProjectsExcept(r.Context(), db.RemoveUserFromProjectsExceptParams{
		WorkspaceID: workspaceID, UserID: userID, KeepProjectIds: ids,
	}); err != nil {
		return err
	}
	_, err := q.AddUserToProjects(r.Context(), db.AddUserToProjectsParams{
		WorkspaceID: workspaceID, UserID: userID, ProjectIds: ids, AddedBy: addedBy,
	})
	return err
}

// beginMemberSeat takes a seat for a guest becoming a member the way an
// invitation and its acceptance do: reserve, then consume, under one token
// that stands in for the invitation. The caller moves the intent to confirm
// in the role-change transaction. It returns uuid.Nil when seats are not
// managed. A crash between the steps leaves a reservation with no
// invitation, which the outbox worker releases.
func (h *Handler) beginMemberSeat(ctx context.Context, workspaceID, userID uuid.UUID) (uuid.UUID, error) {
	if !h.seatCapacityEnabled() {
		return uuid.Nil, nil
	}
	token := uuid.New()
	if err := h.reserveInvitationCapacity(ctx, workspaceID, token, time.Now().Add(guestSeatReservation)); err != nil {
		return uuid.Nil, err
	}
	active, err := h.beginCapacityConsume(ctx, workspaceID, token, token, userID)
	if err != nil {
		h.compensateCapacityIntent(ctx, token)
		return uuid.Nil, err
	}
	if !active {
		return uuid.Nil, nil
	}
	return token, nil
}

const guestSeatReservation = time.Hour

// demoteMemberToGuest revokes what a member holds that a guest may not (see
// revokeAndDemoteToGuest) and releases the member's seat.
func (h *Handler) demoteMemberToGuest(w http.ResponseWriter, r *http.Request, requester, target db.Member, projects []pgtype.UUID) (db.Member, bool) {
	ctx := r.Context()
	result, err := h.revokeAndDemoteToGuest(ctx, target.WorkspaceID, target.UserID, target.ID, requester.UserID, projects)
	if err != nil {
		slog.Warn("demote member to guest failed", append(logger.RequestAttrs(r), "error", err, "member_id", uuidToString(target.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update member")
		return db.Member{}, false
	}
	h.settleMemberCapacityRelease(ctx, uuid.UUID(target.WorkspaceID.Bytes), uuid.UUID(target.ID.Bytes))
	wsID := uuidToString(target.WorkspaceID)
	logRevocation(result, wsID, uuidToString(target.UserID))
	h.publishRevocation(ctx, result, wsID, "member", uuidToString(requester.UserID))
	h.notifyDaemonWorkspacesChanged(uuidToString(target.UserID))
	demoted := target
	demoted.Role = projectaccess.RoleGuest
	return demoted, true
}

// promoteGuest gives a guest a member role, taking a seat for it.
func (h *Handler) promoteGuest(w http.ResponseWriter, r *http.Request, target db.Member, role string) (db.Member, bool) {
	ctx := r.Context()
	workspaceID := uuid.UUID(target.WorkspaceID.Bytes)
	token, err := h.beginMemberSeat(ctx, workspaceID, uuid.UUID(target.UserID.Bytes))
	if err != nil {
		writeSeatCapacityError(w, err)
		return db.Member{}, false
	}
	fail := func() (db.Member, bool) {
		if token != uuid.Nil {
			h.compensateCapacityIntent(ctx, token)
		}
		writeError(w, http.StatusInternalServerError, "failed to update member")
		return db.Member{}, false
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return fail()
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	updated, err := qtx.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{ID: target.ID, Role: role})
	if err != nil {
		return fail()
	}
	if token != uuid.Nil {
		if err := transitionCapacityIntentToConfirm(ctx, qtx, token, uuid.UUID(target.ID.Bytes), seatcapacity.ActionConsumeInvitation); err != nil {
			_ = tx.Rollback(ctx)
			return fail()
		}
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return fail()
	}
	if token != uuid.Nil {
		h.confirmCapacityIntent(ctx, workspaceID, token, uuid.UUID(target.ID.Bytes))
	}
	return updated, true
}

type SetMemberProjectsRequest struct {
	ProjectIDs []string `json:"project_ids"`
}

// SetMemberProjects replaces the projects a guest can see.
// PUT /api/workspaces/{id}/members/{memberId}/projects
func (h *Handler) SetMemberProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	memberUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "member id")
	if !ok {
		return
	}
	target, err := h.Queries.GetMember(r.Context(), memberUUID)
	if err != nil || target.WorkspaceID != requester.WorkspaceID {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if !projectaccess.IsGuestRole(target.Role) {
		writeError(w, http.StatusBadRequest, "only a guest's projects are set here")
		return
	}
	var req SetMemberProjectsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ids, ok := h.parseGuestProjectIDs(w, r, target.WorkspaceID, req.ProjectIDs)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update projects")
		return
	}
	defer tx.Rollback(r.Context())
	if err := setGuestProjects(r, h.Queries.WithTx(tx), target.WorkspaceID, target.UserID, requester.UserID, ids); err != nil {
		slog.Warn("set guest projects failed", append(logger.RequestAttrs(r), "error", err, "member_id", uuidToString(target.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update projects")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update projects")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), target.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load member")
		return
	}
	resp := h.memberWithUserResponse(target, user)
	resp.ProjectIDs = uuidsToStrings(ids)
	h.publish(protocol.EventMemberUpdated, uuidToString(target.WorkspaceID), "member", requestUserID(r), map[string]any{
		"member": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}
