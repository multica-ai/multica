package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Private projects (MUL-8472, MUL-9284). Only project members see one; a
// workspace role grants no visibility. Among the people who can see it:
//
//   - change visibility or lead, add/remove members: a workspace owner/admin,
//     or the project's access manager (canManageProjectAccess)
//   - ordinary project members: read-only membership, no self-removal
//   - agents: none of these; an agent's access is derived from humans

type ProjectMemberResponse struct {
	ProjectID     string  `json:"project_id"`
	UserID        string  `json:"user_id"`
	Name          string  `json:"name"`
	Email         string  `json:"email"`
	AvatarURL     *string `json:"avatar_url"`
	AddedBy       *string `json:"added_by"`
	CreatedAt     string  `json:"created_at"`
	WorkspaceRole string  `json:"workspace_role,omitempty"`
}

type AddProjectMembersRequest struct {
	UserIDs []string `json:"user_ids"`
}

// projectAccessUpdate is the validated, not yet written, access side of a
// project update: a visibility switch and the member rows it needs.
type projectAccessUpdate struct {
	visibility string // target visibility; "" leaves it unchanged
	addIDs     []pgtype.UUID
	// privateAddition names how the update adds to private-project
	// configuration ("make_private", "add_members"), or is "" when it does
	// not. It decides whether the private_projects paid feature applies.
	privateAddition string
}

func (u projectAccessUpdate) empty() bool { return u.visibility == "" && len(u.addIDs) == 0 }

func (h *Handler) requireHumanProjectAccessActor(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot change project access")
		return false
	}
	return true
}

// canManageProjectAccess reports whether member may change who sees project:
// its visibility, its member list and, on a private project, its lead.
// Callers have already established that the member can see the project, which
// on a private one means being a project member.
//
// Workspace owners and admins may. On a private project so may its access
// manager (GetProjectAccessManager): the lead when that is a person, otherwise
// the creator, otherwise the longest-standing member. The lead of a private
// project may be an agent or nobody, so without that fallback a project whose
// list holds no owner or admin would have nobody able to manage it.
//
// Any member may set the lead of a workspace-visible project, so leading one
// grants nothing; making it private takes an owner/admin.
func (h *Handler) canManageProjectAccess(ctx context.Context, member db.Member, project db.Project) (bool, error) {
	if projectaccess.IsManagerRole(member.Role) {
		return true, nil
	}
	manager, err := h.projectAccessManager(ctx, project)
	if err != nil {
		return false, err
	}
	return manager.Valid && manager == member.UserID, nil
}

// projectAccessManager is the member who manages a private project's access
// besides workspace owners and admins. Invalid for a workspace-visible
// project, and for a private one with no members.
func (h *Handler) projectAccessManager(ctx context.Context, project db.Project) (pgtype.UUID, error) {
	if projectVisibilityOrDefault(project.Visibility) != projectaccess.VisibilityPrivate {
		return pgtype.UUID{}, nil
	}
	manager, err := h.Queries.GetProjectAccessManager(ctx, db.GetProjectAccessManagerParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, nil
	}
	return manager, err
}

// requireProjectAccessManager answers 403 with denied unless the caller may
// manage the project's access.
func (h *Handler) requireProjectAccessManager(w http.ResponseWriter, r *http.Request, member db.Member, project db.Project, denied string) bool {
	ok, err := h.canManageProjectAccess(r.Context(), member, project)
	if err != nil {
		if writeQueryCancellation(w, r, err) {
			return false
		}
		slog.Warn("resolve project access manager failed", append(logger.RequestAttrs(r), "project_id", uuidToString(project.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to resolve project access")
		return false
	}
	if !ok {
		writeError(w, http.StatusForbidden, denied)
		return false
	}
	return true
}

func parseProjectMemberIDs(w http.ResponseWriter, raw []string, field string) ([]pgtype.UUID, bool) {
	ids := make([]pgtype.UUID, 0, len(raw))
	seen := map[pgtype.UUID]struct{}{}
	for _, s := range raw {
		id, ok := parseUUIDOrBadRequest(w, s, field)
		if !ok {
			return nil, false
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, true
}

// prepareProjectAccessUpdate validates the access side of a project update
// before anything is written. Three things change who can see a project:
//
//   - a visibility switch (requested = nil leaves it alone), which takes a
//     human who manages the project's access;
//   - member_ids on a project that is or stays private, which adds them as
//     POST /members would and takes the same human;
//   - on a private project, a new member lead who is not yet a project member.
//     A member lead must be a project member, so the update adds them, which
//     is granting access: it takes a human who could add members directly.
//     Agents never grant access, here or anywhere else.
//
// member_ids is never dropped silently: on a project that is or becomes
// workspace-visible, naming anyone not already a project member is a 400.
//
// newLeadType / newLeadID are the lead the project will have after the update.
func (h *Handler) prepareProjectAccessUpdate(w http.ResponseWriter, r *http.Request, project db.Project, requested *string, rawMemberIDs []string, newLeadType pgtype.Text, newLeadID pgtype.UUID, leadChanged bool) (projectAccessUpdate, bool) {
	workspaceID := uuidToString(project.WorkspaceID)
	current := projectVisibilityOrDefault(project.Visibility)
	memberLead := newLeadType.Valid && newLeadType.String == "member" && newLeadID.Valid

	// A person who leads a private project manages its access, so changing
	// the lead is an access change: naming a person hands them the member
	// list, and clearing it or naming an agent hands the list to whoever is
	// next in line. On a workspace-visible project the lead is an ordinary
	// field anyone can set, as it was before private projects existed.
	if leadChanged && current == projectaccess.VisibilityPrivate {
		if !h.requireHumanProjectAccessActor(w, r, workspaceID) {
			return projectAccessUpdate{}, false
		}
		member, ok := h.workspaceMember(w, r, workspaceID)
		if !ok {
			return projectAccessUpdate{}, false
		}
		if !h.requireProjectAccessManager(w, r, member, project, "only workspace admins and the member who manages this project's access can change the project lead") {
			return projectAccessUpdate{}, false
		}
	}

	if requested != nil && *requested != current {
		target := *requested
		if !projectaccess.ValidVisibility(target) {
			writeError(w, http.StatusBadRequest, "invalid visibility: must be one of workspace, private")
			return projectAccessUpdate{}, false
		}
		if !h.requireHumanProjectAccessActor(w, r, workspaceID) {
			return projectAccessUpdate{}, false
		}
		member, ok := h.workspaceMember(w, r, workspaceID)
		if !ok {
			return projectAccessUpdate{}, false
		}
		if !h.requireProjectAccessManager(w, r, member, project, "only workspace admins and the member who manages this project's access can change project visibility") {
			return projectAccessUpdate{}, false
		}
		update := projectAccessUpdate{visibility: target}
		if target == projectaccess.VisibilityPrivate {
			ids, ok := parseProjectMemberIDs(w, rawMemberIDs, "member_ids")
			if !ok {
				return projectAccessUpdate{}, false
			}
			// The caller and a member lead always keep access to a project
			// they make private. A lead this request did not name may have
			// left the workspace since; there is nobody to add then, and
			// the project goes on naming them as any project would.
			ids = append(ids, member.UserID)
			if memberLead {
				present := true
				if !leadChanged {
					var err error
					if present, err = h.isWorkspaceMemberUser(r, project.WorkspaceID, newLeadID); err != nil {
						writeError(w, http.StatusInternalServerError, "failed to check the project lead")
						return projectAccessUpdate{}, false
					}
				}
				if present {
					ids = append(ids, newLeadID)
				}
			}
			update.addIDs = ids
			update.privateAddition = privateProjectOpMakePrivate
		} else if !h.requireExistingProjectMembers(w, r, project, rawMemberIDs) {
			return projectAccessUpdate{}, false
		}
		return update, true
	}

	if current != projectaccess.VisibilityPrivate {
		if !h.requireExistingProjectMembers(w, r, project, rawMemberIDs) {
			return projectAccessUpdate{}, false
		}
		return projectAccessUpdate{}, true
	}

	var update projectAccessUpdate
	if len(rawMemberIDs) > 0 {
		if !h.requireHumanProjectAccessActor(w, r, workspaceID) {
			return projectAccessUpdate{}, false
		}
		ids, ok := parseProjectMemberIDs(w, rawMemberIDs, "member_ids")
		if !ok {
			return projectAccessUpdate{}, false
		}
		member, ok := h.workspaceMember(w, r, workspaceID)
		if !ok {
			return projectAccessUpdate{}, false
		}
		if !h.requireProjectAccessManager(w, r, member, project, "only workspace admins and the member who manages this project's access can add members") {
			return projectAccessUpdate{}, false
		}
		update.addIDs = ids
	}
	if leadChanged && memberLead && !slices.Contains(update.addIDs, newLeadID) {
		isMember, err := h.isProjectMember(r, project, newLeadID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check project membership")
			return projectAccessUpdate{}, false
		}
		if !isMember {
			update.addIDs = append(update.addIDs, newLeadID)
		}
	}
	// The web app's access dialog writes back the current member list, so only
	// someone who is not a member yet adds to the private project.
	if len(update.addIDs) > 0 {
		grows, err := h.projectMembersGrow(r, project, update.addIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list project members")
			return projectAccessUpdate{}, false
		}
		if grows {
			update.privateAddition = privateProjectOpAddMembers
		}
	}
	return update, true
}

// Private-project operations restricted by the private_projects paid feature.
// Everything else on a private project stays allowed under the restriction:
// removing members, making it workspace-visible, deleting or archiving it,
// changing the lead to an existing member, an agent or nobody, editing any
// other field, and the workspace owner's recovery join (JoinPrivateProject),
// which must keep a project with no members reachable.
const (
	privateProjectOpCreate      = "create"
	privateProjectOpMakePrivate = "make_private"
	privateProjectOpAddMembers  = "add_members"
)

// projectMembersGrow reports whether adding ids to project would add anyone
// who is not a member yet.
func (h *Handler) projectMembersGrow(r *http.Request, project db.Project, ids []pgtype.UUID) (bool, error) {
	existing, err := h.Queries.ListProjectMemberUserIDs(r.Context(), db.ListProjectMemberUserIDsParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if !slices.Contains(existing, id) {
			return true, nil
		}
	}
	return false, nil
}

// requireExistingProjectMembers answers 400 unless every id in member_ids is
// already a project member. A workspace-visible project gains no members from
// an update; the web app's access dialog sends the current list when it makes
// a project public, which stays accepted.
func (h *Handler) requireExistingProjectMembers(w http.ResponseWriter, r *http.Request, project db.Project, rawMemberIDs []string) bool {
	if len(rawMemberIDs) == 0 {
		return true
	}
	ids, ok := parseProjectMemberIDs(w, rawMemberIDs, "member_ids")
	if !ok {
		return false
	}
	existing, err := h.Queries.ListProjectMemberUserIDs(r.Context(), db.ListProjectMemberUserIDsParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project members")
		return false
	}
	for _, id := range ids {
		if !slices.Contains(existing, id) {
			writeError(w, http.StatusBadRequest, "member_ids only adds members to a private project")
			return false
		}
	}
	return true
}

// writeProjectUpdate stores params and, in the same transaction, a prepared
// access update, so a rejected member list leaves the project untouched.
func (h *Handler) writeProjectUpdate(w http.ResponseWriter, r *http.Request, params db.UpdateProjectParams, access projectAccessUpdate, actorUserID string) (db.Project, bool) {
	ctx := r.Context()
	if access.empty() {
		project, err := h.Queries.UpdateProject(ctx, params)
		if err != nil {
			h.writeProjectWriteError(w, r, err, "update")
			return db.Project{}, false
		}
		return project, true
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return db.Project{}, false
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	project, err := qtx.UpdateProject(ctx, params)
	if err != nil {
		h.writeProjectWriteError(w, r, err, "update")
		return db.Project{}, false
	}
	if len(access.addIDs) > 0 {
		inserted, err := qtx.AddProjectMembers(ctx, db.AddProjectMembersParams{
			ProjectID:   project.ID,
			WorkspaceID: project.WorkspaceID,
			UserIds:     access.addIDs,
			AddedBy:     parseUUIDOrInvalid(actorUserID),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add project members")
			return db.Project{}, false
		}
		if !h.allProjectMembersExist(w, r, qtx, project, access.addIDs, inserted) {
			return db.Project{}, false
		}
	}
	if access.visibility != "" {
		project, err = qtx.UpdateProjectVisibility(ctx, db.UpdateProjectVisibilityParams{
			ID:          project.ID,
			WorkspaceID: project.WorkspaceID,
			Visibility:  access.visibility,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update project visibility")
			return db.Project{}, false
		}
	}
	if err := touchProjectSearchIndex(r, qtx, project); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update search index")
		return db.Project{}, false
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project update")
		return db.Project{}, false
	}
	if access.visibility != "" {
		slog.Info("project visibility changed", append(logger.RequestAttrs(r),
			"project_id", uuidToString(project.ID), "visibility", project.Visibility)...)
	}
	if len(access.addIDs) > 0 {
		h.publishProjectMembersChanged(project, parseUUIDOrInvalid(actorUserID))
	}
	return project, true
}

// allProjectMembersExist rejects a member list naming users outside the
// workspace. AddProjectMembers skips them (and existing members) silently, so
// anything not inserted must already be a member to count as valid.
func (h *Handler) allProjectMembersExist(w http.ResponseWriter, r *http.Request, q *db.Queries, project db.Project, requested []pgtype.UUID, inserted []db.ProjectMember) bool {
	if len(inserted) == len(requested) {
		return true
	}
	existing, err := q.ListProjectMemberUserIDs(r.Context(), db.ListProjectMemberUserIDsParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project members")
		return false
	}
	present := make(map[pgtype.UUID]struct{}, len(existing))
	for _, id := range existing {
		present[id] = struct{}{}
	}
	for _, id := range requested {
		if _, ok := present[id]; !ok {
			writeError(w, http.StatusBadRequest, "user "+uuidToString(id)+" is not a member of this workspace")
			return false
		}
	}
	return true
}

func parseUUIDOrInvalid(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

// loadVisibleProject loads {id} for a project sub-resource and 404s when the
// caller cannot see it.
func (h *Handler) loadVisibleProject(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	if !h.requireProjectVisible(w, r, workspaceID, project.ID, http.StatusNotFound, "project not found") {
		return db.Project{}, false
	}
	return project, true
}

func (h *Handler) listProjectMemberResponses(r *http.Request, project db.Project) ([]ProjectMemberResponse, error) {
	rows, err := h.Queries.ListProjectMembers(r.Context(), db.ListProjectMembersParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	resp := make([]ProjectMemberResponse, len(rows))
	for i, row := range rows {
		resp[i] = ProjectMemberResponse{
			ProjectID: uuidToString(row.ProjectID),
			UserID:    uuidToString(row.UserID),
			Name:      row.UserName,
			Email:     row.UserEmail,
			AvatarURL: textToPtr(row.UserAvatarUrl),
			AddedBy:   uuidToPtr(row.AddedBy),
			CreatedAt: timestampToString(row.CreatedAt),
		}
	}
	return resp, nil
}

// ListProjectMembers — GET /api/projects/{id}/members
func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadVisibleProject(w, r)
	if !ok {
		return
	}
	resp, err := h.listProjectMemberResponses(r, project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project members")
		return
	}
	// manager_id names the member who manages access besides workspace owners
	// and admins, so a client can show who that is and whether it is the
	// caller without repeating the rule.
	manager, err := h.projectAccessManager(r.Context(), project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve project access")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": resp, "total": len(resp), "manager_id": uuidToPtr(manager)})
}

// AddProjectMembers — POST /api/projects/{id}/members {"user_ids": [...]}
func (h *Handler) AddProjectMembers(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadVisibleProject(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(project.WorkspaceID)
	if !h.requireHumanProjectAccessActor(w, r, workspaceID) {
		return
	}
	var req AddProjectMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ids, ok := parseProjectMemberIDs(w, req.UserIDs, "user_ids")
	if !ok {
		return
	}
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "user_ids is required")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !h.requireProjectAccessManager(w, r, member, project, "only workspace admins and the member who manages this project's access can add members") {
		return
	}
	// Sharing the project with a guest who is not in it yet is a guest
	// addition (MUL-9420), whatever the project's visibility.
	guestAdmission, ok := h.requireGuestProjectAddition(w, r, project.WorkspaceID, project.ID, ids)
	if !ok {
		return
	}
	var admission featureAdmission
	if projectVisibilityOrDefault(project.Visibility) == projectaccess.VisibilityPrivate {
		grows, err := h.projectMembersGrow(r, project, ids)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list project members")
			return
		}
		if grows {
			if admission, ok = h.requireFeatureAddition(w, r, project.WorkspaceID, entitlement.FeaturePrivateProjects, privateProjectOpAddMembers); !ok {
				return
			}
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	inserted, err := qtx.AddProjectMembers(r.Context(), db.AddProjectMembersParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
		UserIds:     ids,
		AddedBy:     member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add project members")
		return
	}
	if !h.allProjectMembersExist(w, r, qtx, project, ids, inserted) {
		return
	}
	if err := touchProjectSearchIndex(r, qtx, project); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update search index")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project members")
		return
	}
	admission.admitted(r.Context())
	guestAdmission.admitted(r.Context())
	h.publishProjectMembersChanged(project, member.UserID)
	h.ListProjectMembers(w, r)
}

// RemoveProjectMember — DELETE /api/projects/{id}/members/{userId}
func (h *Handler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadVisibleProject(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(project.WorkspaceID)
	if !h.requireHumanProjectAccessActor(w, r, workspaceID) {
		return
	}
	target, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "userId"), "user id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !h.requireProjectAccessManager(w, r, member, project, "only workspace admins and the member who manages this project's access can remove project members") {
		return
	}
	if project.LeadType.Valid && project.LeadType.String == "member" && project.LeadID == target {
		writeError(w, http.StatusConflict, "change the project lead before removing the current lead")
		return
	}
	removed, err := h.Queries.DeleteProjectMember(r.Context(), db.DeleteProjectMemberParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
		UserID:      target,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove project member")
		return
	}
	if removed == 0 {
		writeError(w, http.StatusNotFound, "project member not found")
		return
	}
	if err := touchProjectSearchIndex(r, h.Queries, project); err != nil {
		slog.Warn("touch project search index failed", append(logger.RequestAttrs(r), "project_id", uuidToString(project.ID), "error", err)...)
	}
	h.publishProjectMembersChanged(project, member.UserID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) isWorkspaceMemberUser(r *http.Request, workspaceID, userID pgtype.UUID) (bool, error) {
	_, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (h *Handler) isProjectMember(r *http.Request, project db.Project, userID pgtype.UUID) (bool, error) {
	ids, err := h.Queries.ListProjectMemberUserIDs(r.Context(), db.ListProjectMemberUserIDsParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == userID {
			return true, nil
		}
	}
	return false, nil
}

func (h *Handler) publishProjectMembersChanged(project db.Project, actorUserID pgtype.UUID) {
	h.publish(protocol.EventProjectMembersChanged, uuidToString(project.WorkspaceID), "member", uuidToString(actorUserID), map[string]any{
		"project_id": uuidToString(project.ID),
	})
}

// touchProjectSearchIndex re-announces a project's issues and comments to
// local search sync, so every client re-pulls them under its current access:
// upserts where it can see them, deletions where it cannot.
func touchProjectSearchIndex(r *http.Request, q *db.Queries, project db.Project) error {
	return q.TouchProjectSearchIndexEntries(r.Context(), db.TouchProjectSearchIndexEntriesParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
}
