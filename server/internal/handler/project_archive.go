package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// projectArchivedCode is the machine-readable code for writes that try to
// put new work into an archived project. Clients key their "restore it
// first" copy off it.
const projectArchivedCode = "project_archived"

func writeProjectArchived(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusConflict, projectArchivedCode, "project is archived; restore it before adding issues")
}

// projectIsArchived reports whether a workspace project is archived. A
// project that cannot be read reports false: callers have already validated
// existence and only need the archive bit.
func (h *Handler) projectIsArchived(ctx context.Context, q *db.Queries, projectID, workspaceID pgtype.UUID) bool {
	if !projectID.Valid {
		return false
	}
	project, err := q.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID:          projectID,
		WorkspaceID: workspaceID,
	})
	return err == nil && project.ArchivedAt.Valid
}

// ArchiveProjectResponse is the archived project plus the autopilots the
// archive paused, so callers can tell the user what stopped.
type ArchiveProjectResponse struct {
	ProjectResponse
	PausedAutopilotIDs []string `json:"paused_autopilot_ids"`
}

// loadArchivableProject runs the gate shared by archive and restore: the
// same visibility and member gate as UpdateProject, so anyone who can edit
// the project can archive or restore it.
func (h *Handler) loadArchivableProject(w http.ResponseWriter, r *http.Request) (db.Project, string, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, "", false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Project{}, "", false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, "", false
	}
	if !h.requireProjectVisible(w, r, workspaceID, project.ID, http.StatusNotFound, "project not found") {
		return db.Project{}, "", false
	}
	if _, ok := requireUserID(w, r); !ok {
		return db.Project{}, "", false
	}
	return project, workspaceID, true
}

func (h *Handler) projectResponseWithCounts(ctx context.Context, project db.Project) ProjectResponse {
	resp := projectToResponse(project)
	h.fillProjectIssueStats(ctx, &resp, project)
	resp.ResourceCount = h.loadProjectResourceCount(ctx, project.ID)
	return resp
}

func (h *Handler) ArchiveProject(w http.ResponseWriter, r *http.Request) {
	project, workspaceID, ok := h.loadArchivableProject(w, r)
	if !ok {
		return
	}
	if project.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "project is already archived")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve actor")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive project")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	archived, err := qtx.ArchiveProject(r.Context(), db.ArchiveProjectParams{
		ID:             project.ID,
		WorkspaceID:    project.WorkspaceID,
		ArchivedByType: pgtype.Text{String: actorType, Valid: true},
		ArchivedByID:   actorUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "project is already archived")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive project")
		return
	}
	paused, err := qtx.PauseAutopilotsByArchivedProject(r.Context(), db.PauseAutopilotsByArchivedProjectParams{
		WorkspaceID: project.WorkspaceID,
		ProjectID:   project.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to pause project autopilots")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive project")
		return
	}

	resp := ArchiveProjectResponse{
		ProjectResponse:    h.projectResponseWithCounts(r.Context(), archived),
		PausedAutopilotIDs: make([]string, 0, len(paused)),
	}
	h.publish(protocol.EventProjectUpdated, workspaceID, actorType, actorID, map[string]any{"project": resp.ProjectResponse})
	for _, autopilot := range paused {
		resp.PausedAutopilotIDs = append(resp.PausedAutopilotIDs, uuidToString(autopilot.ID))
		h.publish(protocol.EventAutopilotUpdated, workspaceID, actorType, actorID, map[string]any{
			"autopilot": autopilotToResponse(autopilot, nil),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RestoreProject(w http.ResponseWriter, r *http.Request) {
	project, workspaceID, ok := h.loadArchivableProject(w, r)
	if !ok {
		return
	}
	if !project.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "project is not archived")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)

	// Autopilots paused by the archive stay paused: resuming automation is
	// an explicit decision, not a side effect of restoring.
	restored, err := h.Queries.RestoreProject(r.Context(), db.RestoreProjectParams{
		ID:          project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "project is not archived")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore project")
		return
	}
	resp := h.projectResponseWithCounts(r.Context(), restored)
	h.publish(protocol.EventProjectUpdated, workspaceID, actorType, actorID, map[string]any{"project": resp})
	writeJSON(w, http.StatusOK, resp)
}
