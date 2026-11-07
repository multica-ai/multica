package handler

import (
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ProjectMoveRequest struct {
	TargetWorkspaceID string `json:"target_workspace_id"`
	PlanHash          string `json:"plan_hash"`
}

func (h *Handler) authorizeProjectMove(w http.ResponseWriter, r *http.Request) (db.Project, ProjectMoveRequest, bool) {
	var req ProjectMoveRequest
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "project moves require a human actor")
		return db.Project{}, req, false
	}
	source := h.resolveWorkspaceID(r)
	if actor, _ := h.resolveActor(r, requestUserID(r), source); actor == "agent" {
		writeError(w, http.StatusForbidden, "project moves require a human actor")
		return db.Project{}, req, false
	}
	member, ok := h.requireWorkspaceRole(w, r, source, "workspace not found", "owner")
	if !ok {
		return db.Project{}, req, false
	}
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, req, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: member.WorkspaceID})
	if err != nil {
		writeError(w, 404, "project not found")
		return db.Project{}, req, false
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return db.Project{}, req, false
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, req.TargetWorkspaceID, "target_workspace_id")
	if !ok {
		return db.Project{}, req, false
	}
	if targetUUID == project.WorkspaceID {
		writeError(w, 400, "target workspace must differ")
		return db.Project{}, req, false
	}
	if _, ok := h.requireWorkspaceRole(w, r, req.TargetWorkspaceID, "workspace not found", "owner", "admin"); !ok {
		return db.Project{}, req, false
	}
	return project, req, true
}

func (h *Handler) PreviewProjectMove(w http.ResponseWriter, r *http.Request) {
	project, req, ok := h.authorizeProjectMove(w, r)
	if !ok {
		return
	}
	var plan ProjectMovePlan
	err := h.runSearchIndexRead(r.Context(), func(q *db.Queries) error {
		var err error
		plan, err = buildProjectMovePlan(r.Context(), q, project, req.TargetWorkspaceID)
		return err
	})
	if err != nil {
		h.writeProjectWriteError(w, r, err, "preview move")
		return
	}
	writeJSON(w, 200, plan)
}
func (h *Handler) MoveProject(w http.ResponseWriter, r *http.Request) {
	project, req, ok := h.authorizeProjectMove(w, r)
	if !ok {
		return
	}
	if len(req.PlanHash) != 64 {
		writeError(w, 400, "a preview plan_hash is required")
		return
	}
	if _, err := hex.DecodeString(req.PlanHash); err != nil {
		writeError(w, 400, "invalid plan_hash")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		h.writeProjectWriteError(w, r, err, "move")
		return
	}
	defer tx.Rollback(r.Context())
	ctx := r.Context()
	q := h.Queries.WithTx(tx)
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		h.writeProjectWriteError(w, r, err, "move")
		return
	}
	// Draft boundary: only this operation participates in the locks below.
	// Ordinary writers are unchanged and can race with the move.
	// Acquire existing catalog guards first, then UUID-ordered workspace
	// counters, the project, and UUID-ordered moving issues. NO KEY UPDATE
	// on counters avoids conflicting with ordinary comment FK KEY SHARE.
	// These locks serialize moves, not every ordinary writer protocol.
	catalogs := []string{uuidToString(project.WorkspaceID), req.TargetWorkspaceID}
	sort.Strings(catalogs)
	// Definition edits lock props:<workspace> before prop:<id>; value writes
	// lock prop:<id> before issue rows. Freeze both catalogs, then their values,
	// before any owner lock, and rebuild the mapping/hash under these guards.
	for _, workspace := range catalogs {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "props:"+workspace); err != nil {
			h.writeProjectWriteError(w, r, err, "move")
			return
		}
	}
	var propertyIDs []string
	for _, workspace := range catalogs {
		properties, err := q.ListIssueProperties(ctx, db.ListIssuePropertiesParams{WorkspaceID: parseUUID(workspace), IncludeArchived: true})
		if err != nil {
			h.writeProjectWriteError(w, r, err, "move")
			return
		}
		for _, property := range properties {
			propertyIDs = append(propertyIDs, uuidToString(property.ID))
		}
	}
	sort.Strings(propertyIDs)
	for _, id := range propertyIDs {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "prop:"+id); err != nil {
			h.writeProjectWriteError(w, r, err, "move")
			return
		}
	}
	for _, workspace := range catalogs {
		if err := q.LockIssueStatusCatalogShared(ctx, parseUUID(workspace)); err != nil {
			h.writeProjectWriteError(w, r, err, "move")
			return
		}
	}
	locked, err := q.LockProjectMoveWorkspaces(ctx, []pgtype.UUID{project.WorkspaceID, parseUUID(req.TargetWorkspaceID)})
	if err != nil || len(locked) != 2 {
		h.writeProjectWriteError(w, r, err, "move")
		return
	}
	if _, err := q.LockProjectForDelete(ctx, db.LockProjectForDeleteParams{ID: project.ID, WorkspaceID: project.WorkspaceID}); err != nil {
		writeError(w, 409, "project changed; preview again")
		return
	}
	if _, err := q.LockProjectMoveIssues(ctx, db.LockProjectMoveIssuesParams{ProjectID: project.ID, SourceWorkspaceID: project.WorkspaceID}); err != nil {
		h.writeProjectWriteError(w, r, err, "move")
		return
	}
	// Re-read memberships after locks rather than trusting middleware or caches.
	for _, gate := range []struct {
		workspace pgtype.UUID
		roles     []string
	}{{project.WorkspaceID, []string{"owner"}}, {parseUUID(req.TargetWorkspaceID), []string{"owner", "admin"}}} {
		member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: gate.workspace, UserID: parseUUID(requestUserID(r))})
		if err != nil {
			writeError(w, 404, "workspace not found")
			return
		}
		if !roleAllowed(member.Role, gate.roles...) {
			writeError(w, 403, "insufficient permissions")
			return
		}
	}
	plan, err := buildProjectMovePlan(ctx, q, project, req.TargetWorkspaceID)
	if err != nil {
		h.writeProjectWriteError(w, r, err, "move")
		return
	}
	if plan.PlanHash != req.PlanHash || len(plan.Blockers) > 0 {
		writeJSON(w, 409, map[string]any{"error": "move plan changed or has blockers; preview again", "plan": plan})
		return
	}
	for _, issue := range plan.Issues {
		if err := assertIssueStatusStillActive(ctx, q, parseUUID(req.TargetWorkspaceID), issue.TargetStatus); err != nil {
			writeError(w, http.StatusConflict, "move status mapping changed; preview again")
			return
		}
	}
	if err := applyProjectMove(ctx, q, plan, requestUserID(r)); err != nil {
		h.writeProjectWriteError(w, r, err, "move")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		h.writeProjectWriteError(w, r, err, "move")
		return
	}
	writeJSON(w, 200, map[string]any{"project_id": plan.ProjectID, "workspace_id": plan.TargetWorkspaceID, "moved": true})
}
