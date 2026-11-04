package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AssignSquadProjectRequest names the project a squad takes. StatusMapping
// answers a 409 workflow_status_mapping_required: where each status the SDLC
// workflow lacks moves its issues.
type AssignSquadProjectRequest struct {
	ProjectID     string            `json:"project_id"`
	StatusMapping map[string]string `json:"status_mapping,omitempty"`
}

// AssignSquadProject gives a project to a squad, which shows it in its room. The
// project takes the SDLC Auto workflow, built on the squad's SDLC agents. A
// squad holds one project, so its previous one is unassigned. The workflow
// switch uses the same status mapping as a project's workflow change, in one
// transaction. A project already on a different workflow is refused: its
// workflow is changed from the project's settings.
func (h *Handler) AssignSquadProject(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageSquad(member, squad) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if squad.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "squad is archived")
		return
	}
	var req AssignSquadProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	wsUUID := squad.WorkspaceID
	ctx := r.Context()

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign project")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockIssueStatusCatalogShared(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign project")
		return
	}
	project, err := qtx.LockProjectForWorkflowChange(ctx, db.LockProjectForWorkflowChangeParams{ID: projectUUID, WorkspaceID: wsUUID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to assign project")
		return
	}

	sdlc, err := h.initiativeService().EnsureSDLCWorkflow(ctx, qtx, wsUUID, squad.ID)
	if err != nil {
		if errors.Is(err, service.ErrBuildNeedsSDLCSquad) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "build_squad_not_sdlc", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to set up the SDLC workflow")
		return
	}

	var changed []db.Issue
	var cancelled []db.AgentTaskQueue
	workflowChanges := !project.WorkflowID.Valid || uuidToString(project.WorkflowID) != uuidToString(sdlc.ID)
	if project.WorkflowID.Valid && workflowChanges {
		writeErrorCode(w, http.StatusConflict, "project_workflow_set",
			"this project already uses another workflow; change it in the project's settings first")
		return
	}
	var plan workflowMappingPlan
	if workflowChanges {
		target, err := issueworkflow.Decode(sdlc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read workflow")
			return
		}
		catalog, err := h.workflowStatusCatalog(ctx, qtx, wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load issue statuses")
			return
		}
		counts, err := qtx.CountProjectIssuesByStatus(ctx, db.CountProjectIssuesByStatusParams{WorkspaceID: wsUUID, ProjectIds: []pgtype.UUID{project.ID}})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to assign project")
			return
		}
		categoryOf := func(key string) string { return catalog[key].Category }
		plan = mappingPlan(counts, target.Has, func(key string) string {
			return issueworkflow.MoveTarget(target, key, categoryOf)
		})
		if msg := checkMapping(plan, req.StatusMapping, target.Has); msg != "" {
			writeMappingRequired(w, plan, msg)
			return
		}
		if _, err := qtx.SetProjectWorkflow(ctx, db.SetProjectWorkflowParams{ID: project.ID, WorkspaceID: wsUUID, WorkflowID: sdlc.ID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to assign project")
			return
		}
		changed, cancelled, err = remapIssues(ctx, qtx, wsUUID, []pgtype.UUID{project.ID}, plan, req.StatusMapping)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to move issues to the new statuses")
			return
		}
	}

	unassigned, err := qtx.ClearSquadFromOtherProjects(ctx, db.ClearSquadFromOtherProjectsParams{WorkspaceID: wsUUID, SquadID: squad.ID, ID: project.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign project")
		return
	}
	assigned, err := qtx.SetProjectSquad(ctx, db.SetProjectSquadParams{ID: project.ID, WorkspaceID: wsUUID, SquadID: squad.ID})
	if err != nil {
		slog.Warn("AssignSquadProject set squad failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to assign project")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign project")
		return
	}

	actorID := uuidToString(member.UserID)
	h.publishIssueSnapshots(ctx, changed, "member", actorID)
	h.broadcastCancelledWakeups(ctx, wsUUID, cancelled)
	for _, p := range unassigned {
		h.publishProjectWorkflowUpdate(ctx, workspaceID, member, p)
	}
	resp := h.publishProjectWorkflowUpdate(ctx, workspaceID, member, assigned)
	writeJSON(w, http.StatusOK, map[string]any{
		"project":        resp,
		"updated_issues": len(changed),
		"plan":           plan,
	})
}
