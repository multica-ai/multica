package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Project workflow API (MUL-7420).
//
// Reading workflows is open to any member: every client needs them to render
// a project's board and to filter status choices. Creating, editing and
// deleting a workflow is owner/admin only, like the status catalog it builds
// on. Choosing which workflow a project uses is a project edit, open to the
// same members who can edit the project.

// requireProjectWorkflowsV1 gates adopting workflows behind the
// project_workflows_v1 flag. Reads, edits of existing workflows and switching
// a project back to Default stay open, so turning the flag off never strands
// a project on a workflow nobody can maintain.
func (h *Handler) requireProjectWorkflowsV1(w http.ResponseWriter, r *http.Request) bool {
	if featureflags.ProjectWorkflowsV1Enabled(r.Context(), h.FeatureFlags) {
		return true
	}
	writeFeatureDisabled(w, "project_workflows_disabled", "Project workflows are not enabled")
	return false
}

type IssueWorkflowHandlerPayload struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type IssueWorkflowStepPayload struct {
	StatusKey     string                      `json:"status_key"`
	Handler       IssueWorkflowHandlerPayload `json:"handler"`
	Instructions  string                      `json:"instructions"`
	NextStatusKey string                      `json:"next_status_key,omitempty"`
	BackStatusKey string                      `json:"back_status_key,omitempty"`
}

type IssueWorkflowResponse struct {
	ID               string                     `json:"id"`
	WorkspaceID      string                     `json:"workspace_id"`
	Name             string                     `json:"name"`
	Description      string                     `json:"description"`
	InitialStatusKey string                     `json:"initial_status_key"`
	Steps            []IssueWorkflowStepPayload `json:"steps"`
	ProjectIDs       []string                   `json:"project_ids"`
	CreatedAt        string                     `json:"created_at"`
	UpdatedAt        string                     `json:"updated_at"`
}

// StatusMappingRequirement names a status some issues are on that the target
// workflow does not list. SuggestedStatusKey is the server's default choice.
type StatusMappingRequirement struct {
	StatusKey          string `json:"status_key"`
	IssueCount         int64  `json:"issue_count"`
	SuggestedStatusKey string `json:"suggested_status_key"`
}

// StatusIssueCount is a status some issues are on that the target workflow
// also lists, so they keep it.
type StatusIssueCount struct {
	StatusKey  string `json:"status_key"`
	IssueCount int64  `json:"issue_count"`
}

type workflowMappingPlan struct {
	Required       []StatusMappingRequirement `json:"required"`
	Unchanged      []StatusIssueCount         `json:"unchanged"`
	TotalIssues    int64                      `json:"total_issues"`
	AffectedIssues int64                      `json:"affected_issues"`
}

func stepsToPayload(steps []issueworkflow.Step) []IssueWorkflowStepPayload {
	out := make([]IssueWorkflowStepPayload, len(steps))
	for i, s := range steps {
		out[i] = IssueWorkflowStepPayload{
			StatusKey:     s.StatusKey,
			Handler:       IssueWorkflowHandlerPayload{Type: s.Handler.Type, ID: s.Handler.ID},
			Instructions:  s.Instructions,
			NextStatusKey: s.NextStatusKey,
			BackStatusKey: s.BackStatusKey,
		}
	}
	return out
}

func payloadToSteps(in []IssueWorkflowStepPayload) []issueworkflow.Step {
	out := make([]issueworkflow.Step, len(in))
	for i, s := range in {
		out[i] = issueworkflow.Step{
			StatusKey:     s.StatusKey,
			Handler:       issueworkflow.Handler{Type: s.Handler.Type, ID: s.Handler.ID},
			Instructions:  s.Instructions,
			NextStatusKey: s.NextStatusKey,
			BackStatusKey: s.BackStatusKey,
		}
	}
	return out
}

func issueWorkflowToResponse(row db.IssueWorkflow, def issueworkflow.Definition, projectIDs []string) IssueWorkflowResponse {
	if projectIDs == nil {
		projectIDs = []string{}
	}
	return IssueWorkflowResponse{
		ID:               uuidToString(row.ID),
		WorkspaceID:      uuidToString(row.WorkspaceID),
		Name:             row.Name,
		Description:      row.Description,
		InitialStatusKey: row.InitialStatusKey,
		Steps:            stepsToPayload(def.Steps),
		ProjectIDs:       projectIDs,
		CreatedAt:        timestampToString(row.CreatedAt),
		UpdatedAt:        timestampToString(row.UpdatedAt),
	}
}

// projectIDsByWorkflow groups the workspace's projects by the workflow they
// use, so a list response can say where each workflow applies.
func (h *Handler) projectIDsByWorkflow(ctx context.Context, wsUUID pgtype.UUID) (map[string][]string, error) {
	projects, err := h.Queries.ListProjects(ctx, db.ListProjectsParams{WorkspaceID: wsUUID})
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, p := range projects {
		if p.WorkflowID.Valid {
			key := uuidToString(p.WorkflowID)
			out[key] = append(out[key], uuidToString(p.ID))
		}
	}
	return out, nil
}

// workflowStatusCatalog loads the workspace catalog keyed by status key,
// including archived rows so validation can say why a key is unusable.
func (h *Handler) workflowStatusCatalog(ctx context.Context, q *db.Queries, wsUUID pgtype.UUID) (map[string]issueworkflow.CatalogEntry, error) {
	if err := issuestatus.Ensure(ctx, q, wsUUID); err != nil {
		return nil, err
	}
	rows, err := q.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{WorkspaceID: wsUUID, IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	out := make(map[string]issueworkflow.CatalogEntry, len(rows))
	for _, row := range rows {
		// The lifecycle category, not the legacy wire value: a switch maps
		// in_progress onto in_review because both are "started".
		category, _ := issuestatus.ParseCategory(row.Category)
		out[row.Key] = issueworkflow.CatalogEntry{
			Key:      row.Key,
			Name:     row.Name,
			Category: category,
			Archived: row.ArchivedAt.Valid,
		}
	}
	return out, nil
}

func (h *Handler) ListIssueWorkflows(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	rows, err := h.Queries.ListIssueWorkflows(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("ListIssueWorkflows failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	byWorkflow, err := h.projectIDsByWorkflow(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("ListIssueWorkflows projects failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	resp := make([]IssueWorkflowResponse, 0, len(rows))
	for _, row := range rows {
		def, err := issueworkflow.Decode(row)
		if err != nil {
			slog.Warn("ListIssueWorkflows decode failed", append(logger.RequestAttrs(r), "workflow_id", uuidToString(row.ID), "error", err)...)
			continue
		}
		resp = append(resp, issueWorkflowToResponse(row, def, byWorkflow[uuidToString(row.ID)]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": resp, "total": len(resp)})
}

func (h *Handler) GetIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	row, err := h.Queries.GetIssueWorkflow(r.Context(), db.GetIssueWorkflowParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	h.writeIssueWorkflow(w, r, http.StatusOK, row)
}

func (h *Handler) writeIssueWorkflow(w http.ResponseWriter, r *http.Request, status int, row db.IssueWorkflow) {
	def, err := issueworkflow.Decode(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read workflow")
		return
	}
	byWorkflow, err := h.projectIDsByWorkflow(r.Context(), row.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read workflow")
		return
	}
	writeJSON(w, status, issueWorkflowToResponse(row, def, byWorkflow[uuidToString(row.ID)]))
}

type IssueWorkflowWriteRequest struct {
	Name             *string                     `json:"name"`
	Description      *string                     `json:"description"`
	InitialStatusKey *string                     `json:"initial_status_key"`
	Steps            *[]IssueWorkflowStepPayload `json:"steps"`
	// StatusMapping moves issues off statuses an edit removes, keyed by the
	// removed status. Required for every removed status that issues of the
	// workflow's projects still use.
	StatusMapping map[string]string `json:"status_mapping,omitempty"`
	// DryRun validates the edit and returns the mapping plan without writing.
	DryRun bool `json:"dry_run,omitempty"`
}

// validateWorkflowHandlers checks that every concrete handler exists in the
// workspace and that the saving admin may hand work to it — the same gate an
// assignment passes.
func (h *Handler) validateWorkflowHandlers(ctx context.Context, r *http.Request, workspaceID string, steps []issueworkflow.Step) (int, string) {
	for _, s := range steps {
		switch s.Handler.Type {
		case issueworkflow.HandlerAgent, issueworkflow.HandlerSquad, issueworkflow.HandlerMember:
			id, err := parseUUIDString(s.Handler.ID)
			if err != nil {
				return http.StatusBadRequest, fmt.Sprintf("step %q has an invalid handler id", s.StatusKey)
			}
			if status, msg := h.validateAssigneePair(ctx, r, workspaceID,
				pgtype.Text{String: s.Handler.Type, Valid: true}, id); status != 0 {
				return status, fmt.Sprintf("step %q: %s", s.StatusKey, msg)
			}
		}
	}
	return 0, ""
}

func parseUUIDString(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil || !id.Valid {
		return pgtype.UUID{}, errors.New("invalid uuid")
	}
	return id, nil
}

func (h *Handler) CreateIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	if !h.requireProjectWorkflowsV1(w, r) {
		return
	}
	var req IssueWorkflowWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var name, description, initial string
	var steps []issueworkflow.Step
	if req.Name != nil {
		name = *req.Name
	}
	if req.Description != nil {
		description = *req.Description
	}
	if req.InitialStatusKey != nil {
		initial = *req.InitialStatusKey
	}
	if req.Steps != nil {
		steps = payloadToSteps(*req.Steps)
	}
	name, description, initial, steps = issueworkflow.Normalize(name, description, initial, steps)
	catalog, err := h.workflowStatusCatalog(r.Context(), h.Queries, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue statuses")
		return
	}
	if err := issueworkflow.Validate(name, description, initial, steps, catalog); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_workflow", err.Error())
		return
	}
	if status, msg := h.validateWorkflowHandlers(r.Context(), r, workspaceID, steps); status != 0 {
		writeErrorCode(w, status, "invalid_workflow", msg)
		return
	}
	raw, err := issueworkflow.EncodeSteps(steps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	row, err := h.Queries.CreateIssueWorkflow(r.Context(), db.CreateIssueWorkflowParams{
		WorkspaceID:      wsUUID,
		Name:             name,
		Description:      description,
		InitialStatusKey: initial,
		Steps:            raw,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErrorCode(w, http.StatusConflict, "workflow_name_taken", "a workflow with this name already exists")
			return
		}
		slog.Warn("CreateIssueWorkflow failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	h.publishIssueWorkflowChanged(workspaceID, member, "created")
	h.writeIssueWorkflow(w, r, http.StatusCreated, row)
}

// mappingPlan computes which statuses issues of projectIDs are on that
// allowed() rejects, with a suggested target for each.
func mappingPlan(counts []db.CountProjectIssuesByStatusRow, allowed func(string) bool, suggest func(string) string) workflowMappingPlan {
	plan := workflowMappingPlan{Required: []StatusMappingRequirement{}, Unchanged: []StatusIssueCount{}}
	for _, c := range counts {
		plan.TotalIssues += c.IssueCount
		if allowed(c.Status) {
			plan.Unchanged = append(plan.Unchanged, StatusIssueCount{StatusKey: c.Status, IssueCount: c.IssueCount})
			continue
		}
		plan.AffectedIssues += c.IssueCount
		plan.Required = append(plan.Required, StatusMappingRequirement{
			StatusKey:          c.Status,
			IssueCount:         c.IssueCount,
			SuggestedStatusKey: suggest(c.Status),
		})
	}
	sort.Slice(plan.Required, func(i, j int) bool { return plan.Required[i].StatusKey < plan.Required[j].StatusKey })
	return plan
}

// checkMapping verifies a mapping covers every required status with a target
// the destination accepts. It returns the first problem as a message.
func checkMapping(plan workflowMappingPlan, mapping map[string]string, targetAllowed func(string) bool) string {
	for _, req := range plan.Required {
		target, ok := mapping[req.StatusKey]
		if !ok || target == "" {
			return fmt.Sprintf("choose a destination status for the %d issue(s) on %q", req.IssueCount, req.StatusKey)
		}
		if !targetAllowed(target) {
			return fmt.Sprintf("destination status %q for %q is not part of the target workflow", target, req.StatusKey)
		}
	}
	return ""
}

func writeMappingRequired(w http.ResponseWriter, plan workflowMappingPlan, msg string) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": msg,
		"code":  "workflow_status_mapping_required",
		"plan":  plan,
	})
}

// remapIssues applies a validated mapping inside the caller's transaction.
// An issue it moves into a done or closed status ends its wakeups, as any
// status write does; the returned runs are for the caller's post-commit
// broadcast.
func remapIssues(ctx context.Context, qtx *db.Queries, wsUUID pgtype.UUID, projectIDs []pgtype.UUID, plan workflowMappingPlan, mapping map[string]string) ([]db.Issue, []db.AgentTaskQueue, error) {
	var changed []db.Issue
	var cancelled []db.AgentTaskQueue
	for _, req := range plan.Required {
		rows, err := qtx.RemapProjectIssueStatus(ctx, db.RemapProjectIssueStatusParams{
			WorkspaceID: wsUUID,
			ProjectIds:  projectIDs,
			FromStatus:  req.StatusKey,
			ToStatus:    mapping[req.StatusKey],
		})
		if err != nil {
			return nil, nil, err
		}
		for _, issue := range rows {
			stopped, err := service.StopClosedIssueWakeups(ctx, qtx, issue)
			if err != nil {
				return nil, nil, err
			}
			cancelled = append(cancelled, stopped...)
		}
		changed = append(changed, rows...)
	}
	return changed, cancelled, nil
}

func (h *Handler) UpdateIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	var req IssueWorkflowWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	// The exclusive catalog lock keeps a status archive from racing the step
	// validation below, and serializes concurrent edits of one workspace's
	// workflows.
	if err := qtx.LockIssueStatusCatalog(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	current, err := qtx.GetIssueWorkflow(ctx, db.GetIssueWorkflowParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	currentDef, err := issueworkflow.Decode(current)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read workflow")
		return
	}
	name, description, initial, steps := current.Name, current.Description, current.InitialStatusKey, currentDef.Steps
	if req.Name != nil {
		name = *req.Name
	}
	if req.Description != nil {
		description = *req.Description
	}
	if req.InitialStatusKey != nil {
		initial = *req.InitialStatusKey
	}
	if req.Steps != nil {
		steps = payloadToSteps(*req.Steps)
	}
	name, description, initial, steps = issueworkflow.Normalize(name, description, initial, steps)
	catalog, err := h.workflowStatusCatalog(ctx, qtx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue statuses")
		return
	}
	if err := issueworkflow.Validate(name, description, initial, steps, catalog); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_workflow", err.Error())
		return
	}
	if req.Steps != nil {
		if status, msg := h.validateWorkflowHandlers(ctx, r, workspaceID, steps); status != 0 {
			writeErrorCode(w, status, "invalid_workflow", msg)
			return
		}
	}

	next := issueworkflow.Definition{ID: current.ID, Name: name, InitialStatusKey: initial, Steps: steps}
	projects, err := qtx.ListProjectsUsingIssueWorkflow(ctx, db.ListProjectsUsingIssueWorkflowParams{WorkspaceID: wsUUID, WorkflowID: current.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	projectIDs := make([]pgtype.UUID, len(projects))
	for i, p := range projects {
		projectIDs[i] = p.ID
	}
	plan := workflowMappingPlan{Required: []StatusMappingRequirement{}, Unchanged: []StatusIssueCount{}}
	if len(projectIDs) > 0 {
		counts, err := qtx.CountProjectIssuesByStatus(ctx, db.CountProjectIssuesByStatusParams{WorkspaceID: wsUUID, ProjectIds: projectIDs})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save workflow")
			return
		}
		categoryOf := func(key string) string { return catalog[key].Category }
		plan = mappingPlan(counts, next.Has, func(key string) string { return issueworkflow.MoveTarget(next, key, categoryOf) })
	}
	if req.DryRun {
		writeJSON(w, http.StatusOK, map[string]any{"dry_run": true, "plan": plan})
		return
	}
	if msg := checkMapping(plan, req.StatusMapping, next.Has); msg != "" {
		writeMappingRequired(w, plan, msg)
		return
	}
	raw, err := issueworkflow.EncodeSteps(steps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	row, err := qtx.UpdateIssueWorkflow(ctx, db.UpdateIssueWorkflowParams{
		ID:               current.ID,
		WorkspaceID:      wsUUID,
		Name:             name,
		Description:      description,
		InitialStatusKey: initial,
		Steps:            raw,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErrorCode(w, http.StatusConflict, "workflow_name_taken", "a workflow with this name already exists")
			return
		}
		slog.Warn("UpdateIssueWorkflow failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	changed, cancelledWakeups, err := remapIssues(ctx, qtx, wsUUID, projectIDs, plan, req.StatusMapping)
	if err != nil {
		slog.Warn("UpdateIssueWorkflow remap failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to move issues to the new statuses")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	// Remapping is an administrative move: snapshots only, so it neither
	// notifies subscribers per issue nor looks like a handoff.
	h.publishIssueSnapshots(ctx, changed, "member", uuidToString(member.UserID))
	h.broadcastCancelledWakeups(ctx, wsUUID, cancelledWakeups)
	h.publishIssueWorkflowChanged(workspaceID, member, "updated")
	h.writeIssueWorkflow(w, r, http.StatusOK, row)
}

func (h *Handler) DeleteIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockIssueStatusCatalog(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}
	projects, err := qtx.ListProjectsUsingIssueWorkflow(ctx, db.ListProjectsUsingIssueWorkflowParams{WorkspaceID: wsUUID, WorkflowID: idUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}
	if len(projects) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":         fmt.Sprintf("%d project(s) still use this workflow; switch them to another workflow first", len(projects)),
			"code":          "workflow_in_use",
			"project_count": len(projects),
		})
		return
	}
	n, err := qtx.DeleteIssueWorkflow(ctx, db.DeleteIssueWorkflowParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}
	h.publishIssueWorkflowChanged(workspaceID, member, "deleted")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) publishIssueWorkflowChanged(workspaceID string, actor db.Member, action string) {
	h.publish(protocol.EventIssueWorkflowChanged, workspaceID, "member", uuidToString(actor.UserID), map[string]any{
		"action": action,
	})
}

type SetProjectWorkflowRequest struct {
	// WorkflowID is the workflow to use; null switches back to the workspace
	// Default workflow.
	WorkflowID    *string           `json:"workflow_id"`
	StatusMapping map[string]string `json:"status_mapping,omitempty"`
	DryRun        bool              `json:"dry_run,omitempty"`
}

// SetProjectWorkflow switches the workflow a project uses. Issues on statuses
// the new workflow does not list move to the statuses the caller maps them to,
// in the same transaction. The move is administrative: it neither reassigns
// issues nor starts runs.
func (h *Handler) SetProjectWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return
	}
	var req SetProjectWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var targetID pgtype.UUID
	if req.WorkflowID != nil && *req.WorkflowID != "" {
		if !h.requireProjectWorkflowsV1(w, r) {
			return
		}
		id, ok := parseUUIDOrBadRequest(w, *req.WorkflowID, "workflow_id")
		if !ok {
			return
		}
		targetID = id
	}
	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change workflow")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	// Shared catalog lock: a workflow edit (exclusive) cannot change the
	// target's steps between the census and the remap below.
	if err := qtx.LockIssueStatusCatalogShared(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change workflow")
		return
	}
	project, err := qtx.LockProjectForWorkflowChange(ctx, db.LockProjectForWorkflowChangeParams{ID: projectUUID, WorkspaceID: wsUUID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to change workflow")
		return
	}
	var target *issueworkflow.Definition
	if targetID.Valid {
		row, err := qtx.GetIssueWorkflow(ctx, db.GetIssueWorkflowParams{ID: targetID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusBadRequest, "workflow not found in this workspace")
			return
		}
		def, err := issueworkflow.Decode(row)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read workflow")
			return
		}
		target = &def
	}
	catalog, err := h.workflowStatusCatalog(ctx, qtx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue statuses")
		return
	}
	// The Default workflow accepts every catalog status, archived included:
	// issues already on an archived status keep it, as they always have.
	allowed := func(key string) bool {
		if target == nil {
			_, ok := catalog[key]
			return ok
		}
		return target.Has(key)
	}
	targetAccepts := func(key string) bool {
		if target == nil {
			entry, ok := catalog[key]
			return ok && !entry.Archived
		}
		return target.Has(key)
	}
	counts, err := qtx.CountProjectIssuesByStatus(ctx, db.CountProjectIssuesByStatusParams{WorkspaceID: wsUUID, ProjectIds: []pgtype.UUID{project.ID}})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change workflow")
		return
	}
	categoryOf := func(key string) string { return catalog[key].Category }
	plan := mappingPlan(counts, allowed, func(key string) string {
		if target == nil {
			return issuestatus.Todo
		}
		return issueworkflow.MoveTarget(*target, key, categoryOf)
	})
	if req.DryRun {
		writeJSON(w, http.StatusOK, map[string]any{"dry_run": true, "plan": plan})
		return
	}
	if msg := checkMapping(plan, req.StatusMapping, targetAccepts); msg != "" {
		writeMappingRequired(w, plan, msg)
		return
	}
	updated, err := qtx.SetProjectWorkflow(ctx, db.SetProjectWorkflowParams{ID: project.ID, WorkspaceID: wsUUID, WorkflowID: targetID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change workflow")
		return
	}
	changed, cancelledWakeups, err := remapIssues(ctx, qtx, wsUUID, []pgtype.UUID{project.ID}, plan, req.StatusMapping)
	if err != nil {
		slog.Warn("SetProjectWorkflow remap failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to move issues to the new statuses")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change workflow")
		return
	}
	actorID := uuidToString(member.UserID)
	h.publishIssueSnapshots(ctx, changed, "member", actorID)
	h.broadcastCancelledWakeups(ctx, wsUUID, cancelledWakeups)
	resp := projectToResponse(updated)
	resp.IssueCount, resp.DoneCount = h.loadProjectIssueStats(ctx, wsUUID, updated.ID)
	resp.ResourceCount = h.loadProjectResourceCount(ctx, updated.ID)
	h.publish(protocol.EventProjectUpdated, workspaceID, "member", actorID, map[string]any{"project": resp})
	writeJSON(w, http.StatusOK, map[string]any{
		"project":        resp,
		"updated_issues": len(changed),
		"plan":           plan,
	})
}

// PreviewIssueWorkflowBrief renders the brief a step's handler would receive,
// for a workflow being edited. It takes the unsaved definition so the editor
// can preview before saving. Project-relative handlers are named generically
// because a workflow may serve several projects.
func (h *Handler) PreviewIssueWorkflowBrief(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	var req struct {
		IssueWorkflowWriteRequest
		StatusKey string `json:"status_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var name, initial string
	var steps []issueworkflow.Step
	if req.Name != nil {
		name = *req.Name
	}
	if req.InitialStatusKey != nil {
		initial = *req.InitialStatusKey
	}
	if req.Steps != nil {
		steps = payloadToSteps(*req.Steps)
	}
	name, _, initial, steps = issueworkflow.Normalize(name, "", initial, steps)
	def := issueworkflow.Definition{Name: name, InitialStatusKey: initial, Steps: steps}
	if !def.Has(req.StatusKey) {
		writeError(w, http.StatusBadRequest, "status_key is not a step of this workflow")
		return
	}
	resolver := issuestatus.NewResolver(wsUUID)
	brief := issueworkflow.Brief(issueworkflow.BriefInput{
		Workflow:        def,
		StatusKey:       req.StatusKey,
		IssueIdentifier: h.getIssuePrefix(r.Context(), wsUUID) + "-123",
		StatusName: func(key string) string {
			return resolver.Name(r.Context(), h.Queries, key)
		},
		HandlerName: func(step issueworkflow.Step) string {
			switch step.Handler.Type {
			case issueworkflow.HandlerAgent, issueworkflow.HandlerSquad, issueworkflow.HandlerMember:
				id, err := parseUUIDString(step.Handler.ID)
				if err != nil {
					return step.Handler.Type
				}
				return h.actorDisplayName(r.Context(), wsUUID, step.Handler.Type, id)
			case issueworkflow.HandlerProjectLead:
				return "the project lead"
			case issueworkflow.HandlerCreator:
				return "the issue creator"
			}
			return ""
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{"brief": brief})
}

// WorkflowHandoffRun is an active run of the agent or squad a handoff takes
// the issue from.
type WorkflowHandoffRun struct {
	TaskID    string  `json:"task_id"`
	AgentID   string  `json:"agent_id"`
	Status    string  `json:"status"`
	StartedAt *string `json:"started_at"`
}

type WorkflowHandoffPreviewResponse struct {
	Handoff              bool                 `json:"handoff"`
	WorkflowName         string               `json:"workflow_name,omitempty"`
	FromStatus           string               `json:"from_status"`
	ToStatus             string               `json:"to_status"`
	HandlerType          string               `json:"handler_type,omitempty"`
	HandlerID            string               `json:"handler_id,omitempty"`
	PreviousAssigneeType *string              `json:"previous_assignee_type"`
	PreviousAssigneeID   *string              `json:"previous_assignee_id"`
	PreviousRuns         []WorkflowHandoffRun `json:"previous_runs"`
	Brief                string               `json:"brief,omitempty"`
}

// PreviewWorkflowHandoff says what moving an issue to a status would do under
// its project's workflow, for the confirmation shown before a handoff: who
// the issue would go to, which runs of its current agent are still active,
// and the brief the new handler would receive. It writes nothing.
func (h *Handler) PreviewWorkflowHandoff(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}
	resp := WorkflowHandoffPreviewResponse{
		FromStatus:           issue.Status,
		ToStatus:             status,
		PreviousAssigneeType: textToPtr(issue.AssigneeType),
		PreviousAssigneeID:   uuidToPtr(issue.AssigneeID),
		PreviousRuns:         []WorkflowHandoffRun{},
	}
	params := db.UpdateIssueParams{
		ProjectID:    issue.ProjectID,
		Status:       pgtype.Text{String: status, Valid: true},
		AssigneeType: issue.AssigneeType,
		AssigneeID:   issue.AssigneeID,
	}
	hand, err := h.applyIssueWorkflow(r.Context(), issue, &params, true, false, nil)
	if err != nil {
		if writeIssueWorkflowError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve project workflow")
		return
	}
	if def, _, err := issueworkflow.ForProject(r.Context(), h.Queries, issue.WorkspaceID, issue.ProjectID); err == nil && def != nil {
		resp.WorkflowName = def.Name
	}
	if hand == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Handoff = true
	resp.HandlerType = params.AssigneeType.String
	resp.HandlerID = uuidToString(params.AssigneeID)
	next := issue
	next.Status = status
	next.AssigneeType = params.AssigneeType
	next.AssigneeID = params.AssigneeID
	resp.Brief = h.workflowHandoffNote(r.Context(), next, hand)
	if issue.AssigneeType != params.AssigneeType || issue.AssigneeID != params.AssigneeID {
		for _, task := range h.assigneeActiveRuns(r.Context(), issue) {
			started := task.StartedAt
			if !started.Valid {
				started = task.CreatedAt
			}
			resp.PreviousRuns = append(resp.PreviousRuns, WorkflowHandoffRun{
				TaskID:    uuidToString(task.ID),
				AgentID:   uuidToString(task.AgentID),
				Status:    task.Status,
				StartedAt: timestampToPtr(started),
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
