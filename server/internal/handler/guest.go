package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
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

// WorkspaceGuestsConfigKey is the client config key that tells Web, Desktop
// and mobile whether this deployment offers adding guests (guestsReleased).
// It is derived, not a flag of its own.
const WorkspaceGuestsConfigKey = "workspace_guests"

// guestsReleased reports whether this deployment offers adding guests. The
// deployment mode comes from configuration, not from whether the Cloud plan
// client came up:
//
//   - No Cloud URL (self-host): always. There are no Cloud plans.
//   - A Cloud URL: guests are part of the commercial launch. They are offered
//     once billing_workspace_subscriptions is on, and the workspace's plan
//     then decides through the guests paid feature (requireGuestAddition).
//     When the plan client is unavailable (a malformed Cloud URL), nothing
//     could apply the plan, so adding guests is refused rather than allowed.
func (h *Handler) guestsReleased(ctx context.Context) bool {
	if strings.TrimSpace(h.cfg.CloudURL) == "" {
		return true
	}
	if h.Features == nil {
		return false
	}
	return featureflags.BillingWorkspaceSubscriptionsEnabled(ctx, h.FeatureFlags)
}

// guestsEnabled writes a 403 when this deployment does not offer guests yet.
func (h *Handler) guestsEnabled(w http.ResponseWriter, r *http.Request) bool {
	if h.guestsReleased(r.Context()) {
		return true
	}
	writeFeatureDisabled(w, "workspace_guests_disabled", "guests are not enabled")
	return false
}

// requireGuestAddition admits a write that adds a guest or widens one:
// guests must be released on this deployment (guestsReleased) and included in
// the workspace's plan (the guests paid feature). Removing a guest, narrowing
// its projects and promoting it are never restricted, and existing guests
// keep their access after a downgrade or while guests are not released.
func (h *Handler) requireGuestAddition(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, operation string) (featureAdmission, bool) {
	if !h.guestsEnabled(w, r) {
		return featureAdmission{}, false
	}
	return h.requireFeatureAddition(w, r, workspaceID, entitlement.FeatureGuests, operation)
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

// guestMentionForbidden reports whether content carries a mention a guest may
// not write: an agent or a squad, which would ask an agent to run, or @all,
// which would notify the whole workspace. Creating and editing a comment
// apply the same rule.
func guestMentionForbidden(content string) bool {
	for _, m := range util.ParseMentions(content) {
		if m.Type == "agent" || m.Type == "squad" || m.IsMentionAll() {
			return true
		}
	}
	return false
}

const guestMentionForbiddenMessage = "guests cannot mention agents, squads or everyone"

// refuseGuestMention writes a 403 when a guest member writes a comment with a
// mention guestMentionForbidden rejects.
func (h *Handler) refuseGuestMention(w http.ResponseWriter, r *http.Request, workspaceID, userID, actorType, content string) bool {
	if actorType != "member" || !h.isGuest(r, workspaceID, userID) || !guestMentionForbidden(content) {
		return false
	}
	writeError(w, http.StatusForbidden, guestMentionForbiddenMessage)
	return true
}

// guestTaskView is all of a run a guest is shown: that an agent is working on
// the issue and since when, without its working directory, handoff, trigger
// or result.
func guestTaskView(full AgentTaskResponse) AgentTaskResponse {
	view := restrictedTaskResponse(full)
	view.IssueID = full.IssueID
	return view
}

// guestAgentIDs is the set of agents a guest meets in its projects.
func (h *Handler) guestAgentIDs(ctx context.Context, member db.Member) (map[[16]byte]bool, error) {
	ids, err := h.Queries.ListAgentIDsSeenByGuest(ctx, db.ListAgentIDsSeenByGuestParams{
		WorkspaceID: member.WorkspaceID, UserID: member.UserID,
	})
	if err != nil {
		return nil, err
	}
	seen := make(map[[16]byte]bool, len(ids))
	for _, id := range ids {
		if id.Valid {
			seen[id.Bytes] = true
		}
	}
	return seen, nil
}

// requireGuestProjectAddition admits adding userIDs to a project when that
// shares the project with a guest who is not a member of it yet: guests must
// be switched on and in the plan, as for every other guest addition. projectID
// is invalid for a project being created. Adding only members, or guests
// already in the project, is never restricted here.
func (h *Handler) requireGuestProjectAddition(w http.ResponseWriter, r *http.Request, workspaceID, projectID pgtype.UUID, userIDs []pgtype.UUID) (featureAdmission, bool) {
	if len(userIDs) == 0 {
		return featureAdmission{}, true
	}
	if !projectID.Valid {
		projectID = projectaccess.NoProject
	}
	n, err := h.Queries.CountGuestsNotInProject(r.Context(), db.CountGuestsNotInProjectParams{
		WorkspaceID: workspaceID, UserIds: userIDs, ProjectID: projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check project members")
		return featureAdmission{}, false
	}
	if n == 0 {
		return featureAdmission{}, true
	}
	return h.requireGuestAddition(w, r, workspaceID, "add_projects")
}

// errMemberRoleChanged reports that a member's role changed between the read
// a role change was decided on and the transaction that applies it.
var errMemberRoleChanged = errors.New("member role changed")

func writeMemberRoleChanged(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]string{
		"error": "the member's role changed while this request was in flight; reload and try again",
		"code":  "member_role_changed",
	})
}

// lockMemberRole locks the member row in the transaction and checks it still
// has the role the change was decided on. Concurrent changes to one member
// then apply in turn, and a request decided on a stale role is refused
// instead of skipping the checks the current role needs (a guest becoming a
// member takes a seat; a member becoming a guest gives up its access).
func lockMemberRole(ctx context.Context, q *db.Queries, memberID pgtype.UUID, expectedRole string) (db.Member, error) {
	m, err := q.GetMemberForUpdate(ctx, memberID)
	if err != nil {
		return db.Member{}, err
	}
	if m.Role != expectedRole {
		return db.Member{}, errMemberRoleChanged
	}
	return m, nil
}

// writeMemberUpdateError maps a role-change failure to its response.
func writeMemberUpdateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errMemberRoleChanged):
		writeMemberRoleChanged(w)
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "member not found")
	default:
		writeError(w, http.StatusInternalServerError, "failed to update member")
	}
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

// setGuestProjects makes ids exactly the projects userID is a member of. When
// that adds or takes away a project, the guest's local search copy is made to
// rebuild: nothing in those projects changed, so sync alone would neither
// drop what it lost nor deliver what it gained.
func setGuestProjects(r *http.Request, q *db.Queries, workspaceID, userID, addedBy pgtype.UUID, ids []pgtype.UUID) error {
	removed, err := q.RemoveUserFromProjectsExcept(r.Context(), db.RemoveUserFromProjectsExceptParams{
		WorkspaceID: workspaceID, UserID: userID, KeepProjectIds: ids,
	})
	if err != nil {
		return err
	}
	added, err := q.AddUserToProjects(r.Context(), db.AddUserToProjectsParams{
		WorkspaceID: workspaceID, UserID: userID, ProjectIds: ids, AddedBy: addedBy,
	})
	if err != nil {
		return err
	}
	if removed == 0 && len(added) == 0 {
		return nil
	}
	return markSearchAccessChanged(r.Context(), q, workspaceID, userID)
}

// markSearchAccessChanged records, in the caller's transaction, that what
// userID can see changed without the content changing (MUL-9420). Local
// search copies built before the transaction then rebuild under the new
// access; see ListSearchIndexChanges.
func markSearchAccessChanged(ctx context.Context, q *db.Queries, workspaceID, userID pgtype.UUID) error {
	return q.MarkSearchAccessChanged(ctx, db.MarkSearchAccessChangedParams{
		WorkspaceID: workspaceID, UserID: userID,
	})
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
	result, err := h.revokeAndDemoteToGuest(ctx, target.WorkspaceID, target.UserID, target.ID, requester.UserID, projects, target.Role)
	if err != nil {
		slog.Warn("demote member to guest failed", append(logger.RequestAttrs(r), "error", err, "member_id", uuidToString(target.ID))...)
		writeMemberUpdateError(w, err)
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
	// A promoted guest takes a member slot like a new invitation does, so a
	// full Free workspace refuses it the same way.
	memberLimit, err := h.memberLimitFor(ctx, target.WorkspaceID, role, false)
	if err != nil {
		if errors.Is(err, errMemberLimitReached) {
			writeMemberLimitReached(w)
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update member")
		}
		return db.Member{}, false
	}
	token, err := h.beginMemberSeat(ctx, workspaceID, uuid.UUID(target.UserID.Bytes))
	if err != nil {
		writeSeatCapacityError(w, err)
		return db.Member{}, false
	}
	compensate := func() {
		if token != uuid.Nil {
			h.compensateCapacityIntent(ctx, token)
		}
	}
	fail := func() (db.Member, bool) {
		compensate()
		writeError(w, http.StatusInternalServerError, "failed to update member")
		return db.Member{}, false
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return fail()
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := memberLimit.lock(ctx, qtx); err != nil {
		return fail()
	}
	// Re-check under the row lock that the target is still a guest: a
	// concurrent change that made it a member already took its seat.
	if _, err := lockMemberRole(ctx, qtx, target.ID, projectaccess.RoleGuest); err != nil {
		_ = tx.Rollback(ctx)
		compensate()
		writeMemberUpdateError(w, err)
		return db.Member{}, false
	}
	limitAdmission, err := memberLimit.admit(ctx, qtx)
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, errMemberLimitReached) {
			limitAdmission.record(ctx)
			compensate()
			writeMemberLimitReached(w)
			return db.Member{}, false
		}
		return fail()
	}
	updated, err := qtx.UpdateMemberRole(ctx, db.UpdateMemberRoleParams{ID: target.ID, Role: role})
	if err != nil {
		return fail()
	}
	// The new member sees projects and project-less content its local
	// search copy never received as a guest.
	if err := markSearchAccessChanged(ctx, qtx, target.WorkspaceID, target.UserID); err != nil {
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
	limitAdmission.record(ctx)
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
	added, err := h.Queries.CountProjectsUserIsNotMemberOf(r.Context(), db.CountProjectsUserIsNotMemberOfParams{
		ProjectIds: ids, WorkspaceID: target.WorkspaceID, UserID: target.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update projects")
		return
	}
	// Only sharing another project widens the guest; narrowing never is.
	var admission featureAdmission
	if added > 0 {
		if admission, ok = h.requireGuestAddition(w, r, target.WorkspaceID, "add_projects"); !ok {
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update projects")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	// The target must still be a guest: a member made one meanwhile keeps
	// its own projects, which this would otherwise overwrite.
	if _, err := lockMemberRole(r.Context(), qtx, target.ID, projectaccess.RoleGuest); err != nil {
		writeMemberUpdateError(w, err)
		return
	}
	if err := setGuestProjects(r, qtx, target.WorkspaceID, target.UserID, requester.UserID, ids); err != nil {
		slog.Warn("set guest projects failed", append(logger.RequestAttrs(r), "error", err, "member_id", uuidToString(target.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update projects")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update projects")
		return
	}
	admission.admitted(r.Context())

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
