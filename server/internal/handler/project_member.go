package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Private projects (MUL-8472). Who may do what:
//
//   - change visibility, remove other members: workspace owner/admin, or the
//     project's member lead
//   - add members: the above, or any project member
//   - remove yourself: any project member
//   - agents: none of these; an agent's access is derived from humans
//
// Only switching a project TO private is behind private_projects_v1. Making
// a private project public and managing members keep working with the flag
// off, so turning the flag off never strands an existing private project.

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

// projectVisibilityChange is a validated, not yet applied, visibility switch.
type projectVisibilityChange struct {
	changed   bool
	target    string
	memberIDs []pgtype.UUID
}

func (h *Handler) requireHumanProjectAccessActor(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot change project access")
		return false
	}
	return true
}

// canManageProjectAccess: workspace owner/admin, or the project's member lead.
func canManageProjectAccess(member db.Member, project db.Project) bool {
	if projectaccess.IsManagerRole(member.Role) {
		return true
	}
	return project.LeadType.Valid && project.LeadType.String == "member" && project.LeadID == member.UserID
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

// prepareProjectVisibilityChange validates a requested visibility (nil = not
// requested) against permissions and the rollout flag without writing.
func (h *Handler) prepareProjectVisibilityChange(w http.ResponseWriter, r *http.Request, project db.Project, visibility *string, rawMemberIDs []string) (projectVisibilityChange, bool) {
	if visibility == nil || *visibility == projectVisibilityOrDefault(project.Visibility) {
		return projectVisibilityChange{}, true
	}
	target := *visibility
	if !projectaccess.ValidVisibility(target) {
		writeError(w, http.StatusBadRequest, "invalid visibility: must be one of workspace, private")
		return projectVisibilityChange{}, false
	}
	workspaceID := uuidToString(project.WorkspaceID)
	if !h.requireHumanProjectAccessActor(w, r, workspaceID) {
		return projectVisibilityChange{}, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return projectVisibilityChange{}, false
	}
	if !canManageProjectAccess(member, project) {
		writeError(w, http.StatusForbidden, "only workspace admins and the project lead can change project visibility")
		return projectVisibilityChange{}, false
	}
	if target == projectaccess.VisibilityPrivate && !featureflags.PrivateProjectsV1Enabled(r.Context(), h.FeatureFlags) {
		writeFeatureDisabled(w, "private_projects_disabled", "private projects are not enabled")
		return projectVisibilityChange{}, false
	}
	change := projectVisibilityChange{changed: true, target: target}
	if target == projectaccess.VisibilityPrivate {
		ids, ok := parseProjectMemberIDs(w, rawMemberIDs, "member_ids")
		if !ok {
			return projectVisibilityChange{}, false
		}
		// The caller and a member lead always keep access to a project they
		// make private.
		ids = append(ids, member.UserID)
		if project.LeadType.Valid && project.LeadType.String == "member" && project.LeadID.Valid {
			ids = append(ids, project.LeadID)
		}
		change.memberIDs = ids
	}
	return change, true
}

// applyProjectAccessUpdate writes a prepared visibility change, and keeps a
// private project's member lead in its member list. It returns the project as
// stored afterwards.
func (h *Handler) applyProjectAccessUpdate(w http.ResponseWriter, r *http.Request, project db.Project, change projectVisibilityChange, actorUserID string) (db.Project, bool) {
	ctx := r.Context()
	var addIDs []pgtype.UUID
	if change.changed && change.target == projectaccess.VisibilityPrivate {
		addIDs = change.memberIDs
	} else if !change.changed && project.Visibility == projectaccess.VisibilityPrivate &&
		project.LeadType.Valid && project.LeadType.String == "member" && project.LeadID.Valid {
		addIDs = []pgtype.UUID{project.LeadID}
	}
	if !change.changed && len(addIDs) == 0 {
		return project, true
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return db.Project{}, false
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if len(addIDs) > 0 {
		inserted, err := qtx.AddProjectMembers(ctx, db.AddProjectMembersParams{
			ProjectID:   project.ID,
			WorkspaceID: project.WorkspaceID,
			UserIds:     addIDs,
			AddedBy:     parseUUIDOrInvalid(actorUserID),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add project members")
			return db.Project{}, false
		}
		if change.changed && !h.allProjectMembersExist(w, r, qtx, project, addIDs, inserted) {
			return db.Project{}, false
		}
	}
	if change.changed {
		project, err = qtx.UpdateProjectVisibility(ctx, db.UpdateProjectVisibilityParams{
			ID:          project.ID,
			WorkspaceID: project.WorkspaceID,
			Visibility:  change.target,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update project visibility")
			return db.Project{}, false
		}
		if err := touchProjectSearchIndex(r, qtx, project); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update search index")
			return db.Project{}, false
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project access")
		return db.Project{}, false
	}
	if change.changed {
		slog.Info("project visibility changed", append(logger.RequestAttrs(r),
			"project_id", uuidToString(project.ID), "visibility", project.Visibility)...)
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
	writeJSON(w, http.StatusOK, map[string]any{"members": resp, "total": len(resp)})
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
	if !canManageProjectAccess(member, project) {
		isMember, err := h.isProjectMember(r, project, member.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check project membership")
			return
		}
		if !isMember {
			writeError(w, http.StatusForbidden, "only project members can add members")
			return
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
	if target != member.UserID && !canManageProjectAccess(member, project) {
		writeError(w, http.StatusForbidden, "only workspace admins and the project lead can remove other members")
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
