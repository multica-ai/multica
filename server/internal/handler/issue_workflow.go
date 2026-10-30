package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Project workflow API (MUL-7420).
//
// Reading and creating workflows is open to any member: every client needs
// them to render a project's board, and a new workflow affects no project
// until someone switches one to it. Editing and deleting a workflow, handoffs
// included, is open to its owner (the member who created it) and to workspace
// owners/admins (MUL-8120). The Default workflow has no owner, so only
// owners/admins change it. Which workflow a project uses is open to
// owners/admins and to the project's lead.

func isWorkspaceAdmin(member db.Member) bool {
	return roleAllowed(member.Role, "owner", "admin")
}

// canEditIssueWorkflow reports whether member may edit or delete workflow:
// workspace owners and admins, and the member who owns it.
func canEditIssueWorkflow(member db.Member, workflow db.IssueWorkflow) bool {
	if isWorkspaceAdmin(member) {
		return true
	}
	return workflow.OwnerID.Valid && workflow.OwnerID == member.UserID
}

// canManageProjectWorkflow reports whether member may change a project's own
// workflow settings: owners and admins, and the member who leads the project.
func canManageProjectWorkflow(member db.Member, project db.Project) bool {
	if isWorkspaceAdmin(member) {
		return true
	}
	return project.LeadType.Valid && project.LeadType.String == "member" && project.LeadID == member.UserID
}

type IssueWorkflowHandlerPayload struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

// IssueWorkflowStepPayload is one status of a workflow, in board order, with
// what happens when an issue enters it.
type IssueWorkflowStepPayload struct {
	StatusKey     string                      `json:"status_key"`
	Handler       IssueWorkflowHandlerPayload `json:"handler"`
	Instructions  string                      `json:"instructions"`
	NextStatusKey string                      `json:"next_status_key,omitempty"`
	BackStatusKey string                      `json:"back_status_key,omitempty"`
}

type IssueWorkflowResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
	// OwnerID is the member who owns the workflow; null for Default.
	OwnerID    *string                    `json:"owner_id"`
	Steps      []IssueWorkflowStepPayload `json:"steps"`
	ProjectIDs []string                   `json:"project_ids"`
	CreatedAt  string                     `json:"created_at"`
	UpdatedAt  string                     `json:"updated_at"`
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

func stepToPayload(s issueworkflow.Step) IssueWorkflowStepPayload {
	return IssueWorkflowStepPayload{
		StatusKey:     s.StatusKey,
		Handler:       IssueWorkflowHandlerPayload{Type: s.Handler.Type, ID: s.Handler.ID},
		Instructions:  s.Instructions,
		NextStatusKey: s.NextStatusKey,
		BackStatusKey: s.BackStatusKey,
	}
}

func payloadToStep(statusKey string, in IssueWorkflowStepPayload) issueworkflow.Step {
	return issueworkflow.Step{
		StatusKey:     statusKey,
		Handler:       issueworkflow.Handler{Type: in.Handler.Type, ID: in.Handler.ID},
		Instructions:  in.Instructions,
		NextStatusKey: in.NextStatusKey,
		BackStatusKey: in.BackStatusKey,
	}
}

func issueWorkflowToResponse(row db.IssueWorkflow, def issueworkflow.Definition, projectIDs []string) IssueWorkflowResponse {
	if projectIDs == nil {
		projectIDs = []string{}
	}
	steps := make([]IssueWorkflowStepPayload, len(def.Steps))
	for i, s := range def.Steps {
		steps[i] = stepToPayload(s)
	}
	return IssueWorkflowResponse{
		ID:          uuidToString(row.ID),
		WorkspaceID: uuidToString(row.WorkspaceID),
		Name:        row.Name,
		Description: row.Description,
		IsDefault:   row.IsDefault,
		OwnerID:     uuidToPtr(row.OwnerID),
		Steps:       steps,
		ProjectIDs:  projectIDs,
		CreatedAt:   timestampToString(row.CreatedAt),
		UpdatedAt:   timestampToString(row.UpdatedAt),
	}
}

// projectIDsByWorkflow groups the workspace's projects by the workflow row
// they use, so a list response can say where each workflow applies. Projects
// without a workflow, or pointing at a deleted one, count for Default.
func (h *Handler) projectIDsByWorkflow(ctx context.Context, wsUUID pgtype.UUID, rows []db.IssueWorkflow) (map[string][]string, error) {
	projects, err := h.Queries.ListProjects(ctx, db.ListProjectsParams{WorkspaceID: wsUUID})
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	defaultID := ""
	for _, row := range rows {
		id := uuidToString(row.ID)
		known[id] = true
		if row.IsDefault {
			defaultID = id
		}
	}
	out := map[string][]string{}
	for _, p := range projects {
		key := uuidToString(p.WorkflowID)
		if !p.WorkflowID.Valid || !known[key] {
			key = defaultID
		}
		if key != "" {
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
	// Store Default on the first read so every client sees the same workflow.
	if _, err := issueworkflow.EnsureDefault(r.Context(), h.Queries, wsUUID); err != nil {
		slog.Warn("ListIssueWorkflows ensure default failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	rows, err := h.Queries.ListIssueWorkflows(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("ListIssueWorkflows failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	byWorkflow, err := h.projectIDsByWorkflow(r.Context(), wsUUID, rows)
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
	writeJSON(w, status, h.issueWorkflowResponse(r.Context(), row))
}

func (h *Handler) issueWorkflowResponse(ctx context.Context, row db.IssueWorkflow) IssueWorkflowResponse {
	def, err := issueworkflow.Decode(row)
	if err != nil {
		def = issueworkflow.Definition{}
	}
	rows, err := h.Queries.ListIssueWorkflows(ctx, row.WorkspaceID)
	if err != nil {
		rows = []db.IssueWorkflow{row}
	}
	byWorkflow, err := h.projectIDsByWorkflow(ctx, row.WorkspaceID, rows)
	if err != nil {
		byWorkflow = map[string][]string{}
	}
	return issueWorkflowToResponse(row, def, byWorkflow[uuidToString(row.ID)])
}

// NewWorkflowStatus creates a catalog status as part of a workflow write, so
// a column can be added without the status settings page.
type NewWorkflowStatus struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Color    string `json:"color"`
}

// IssueWorkflowStepInput names a step by an existing status key or by a
// status to create, with its handoff. Next and back statuses must name
// existing statuses.
type IssueWorkflowStepInput struct {
	StatusKey     string                       `json:"status_key,omitempty"`
	NewStatus     *NewWorkflowStatus           `json:"new_status,omitempty"`
	Handler       *IssueWorkflowHandlerPayload `json:"handler,omitempty"`
	Instructions  string                       `json:"instructions,omitempty"`
	NextStatusKey string                       `json:"next_status_key,omitempty"`
	BackStatusKey string                       `json:"back_status_key,omitempty"`
}

type IssueWorkflowWriteRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	// Steps replace the workflow's steps and handoffs. Todo, done and
	// cancelled are added when missing; backlog and blocked are listed only
	// when included. Fixed statuses take their fixed places.
	Steps *[]IssueWorkflowStepInput `json:"steps"`
	// StatusMapping moves issues off statuses an edit removes, keyed by the
	// removed status. Required for every removed status that issues of the
	// workflow's projects still use.
	StatusMapping map[string]string `json:"status_mapping,omitempty"`
	// DryRun validates the edit and returns the mapping plan without writing.
	DryRun bool `json:"dry_run,omitempty"`
}

var newStatusDefaultColor = map[string]string{
	"unstarted": "#94a3b8",
	"started":   "#3b82f6",
	"done":      "#22c55e",
	"closed":    "#64748b",
}

// resolveStepInputs turns step inputs into steps, creating the new statuses
// they name. The caller holds the exclusive catalog lock. A non-empty message
// is a caller error.
// errStatusCreateForbidden reports a workflow write that would add a status
// to the workspace catalog for a caller who may not: statuses are shared by
// every board, so only owners/admins create them (MUL-8120). Reusing an
// existing status, by key or by name, stays open.
var errStatusCreateForbidden = errors.New("only workspace admins can create statuses")

// resolveStepInputs turns the request's steps into workflow steps, creating
// the new statuses they name. allowCreate is false for callers who may not
// add statuses; a step naming one that does not exist yet then fails with
// errStatusCreateForbidden.
func resolveStepInputs(ctx context.Context, qtx *db.Queries, wsUUID pgtype.UUID, inputs []IssueWorkflowStepInput, allowCreate bool) ([]issueworkflow.Step, []db.IssueStatus, string, error) {
	steps := make([]issueworkflow.Step, 0, len(inputs))
	var created []db.IssueStatus
	for _, in := range inputs {
		step := issueworkflow.Step{
			StatusKey:     in.StatusKey,
			Instructions:  in.Instructions,
			NextStatusKey: in.NextStatusKey,
			BackStatusKey: in.BackStatusKey,
		}
		if in.Handler != nil {
			step.Handler = issueworkflow.Handler{Type: in.Handler.Type, ID: in.Handler.ID}
		}
		if in.NewStatus != nil {
			entry, isNew, msg, err := createWorkflowStatus(ctx, qtx, wsUUID, *in.NewStatus, allowCreate)
			if msg != "" || err != nil {
				return nil, nil, msg, err
			}
			if isNew {
				created = append(created, entry)
			}
			step.StatusKey = entry.Key
		}
		steps = append(steps, step)
	}
	return steps, created, "", nil
}

func createWorkflowStatus(ctx context.Context, qtx *db.Queries, wsUUID pgtype.UUID, in NewWorkflowStatus, allowCreate bool) (db.IssueStatus, bool, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return db.IssueStatus{}, false, "status name must be 1-64 characters", nil
	}
	category, ok := issuestatus.ParseCategory(in.Category)
	if !ok {
		category = "started"
	}
	color := newStatusDefaultColor[category]
	if strings.TrimSpace(in.Color) != "" {
		c, err := normalizeColor(in.Color)
		if err != nil {
			return db.IssueStatus{}, false, err.Error(), nil
		}
		color = c
	}
	// A status with this name already exists: reuse it instead of failing,
	// so the same template applied twice lands on the same statuses.
	entries, err := qtx.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{WorkspaceID: wsUUID, IncludeArchived: false})
	if err != nil {
		return db.IssueStatus{}, false, "", err
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name, name) {
			return e, false, "", nil
		}
	}
	if !allowCreate {
		return db.IssueStatus{}, false, "", errStatusCreateForbidden
	}
	entry, msg, err := createIssueStatusEntryLocked(ctx, qtx, wsUUID, category, db.CreateIssueStatusEntryParams{
		WorkspaceID: wsUUID,
		Name:        name,
		Category:    category,
		Color:       color,
	})
	if err != nil && isUniqueViolation(err) {
		return db.IssueStatus{}, false, fmt.Sprintf("a status named %q already exists", name), nil
	}
	return entry, err == nil && msg == "", msg, err
}

// validateHandoffHandlers checks that each concrete handler exists in the
// workspace and that the caller may hand work to it — the same gate an
// assignment passes. Only changed handlers are checked, so an edit never
// fails on a handler someone else picked.
func (h *Handler) validateHandoffHandlers(ctx context.Context, r *http.Request, workspaceID string, steps, previous []issueworkflow.Step) (int, string) {
	before := map[issueworkflow.Handler]bool{}
	for _, s := range previous {
		before[s.Handler] = true
	}
	for _, s := range steps {
		if before[s.Handler] {
			continue
		}
		if status, msg := h.validateHandoffHandler(ctx, r, workspaceID, s); status != 0 {
			return status, msg
		}
	}
	return 0, ""
}

// validateHandoffHandler checks one step's handler like
// validateHandoffHandlers.
func (h *Handler) validateHandoffHandler(ctx context.Context, r *http.Request, workspaceID string, s issueworkflow.Step) (int, string) {
	switch s.Handler.Type {
	case issueworkflow.HandlerAgent, issueworkflow.HandlerSquad, issueworkflow.HandlerMember:
		id, err := parseUUIDString(s.Handler.ID)
		if err != nil {
			return http.StatusBadRequest, "invalid handler id"
		}
		return h.validateAssigneePair(ctx, r, workspaceID, pgtype.Text{String: s.Handler.Type, Valid: true}, id)
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
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	var req IssueWorkflowWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var name, description string
	if req.Name != nil {
		name = *req.Name
	}
	if req.Description != nil {
		description = *req.Description
	}
	var inputs []IssueWorkflowStepInput
	if req.Steps != nil {
		inputs = *req.Steps
	}

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockIssueStatusCatalog(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	// Store Default before creating statuses for this workflow, so they
	// never join Default's snapshot.
	if _, err := issueworkflow.EnsureDefault(ctx, qtx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	steps, createdStatuses, msg, err := resolveStepInputs(ctx, qtx, wsUUID, inputs, isWorkspaceAdmin(member))
	if msg != "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_workflow", msg)
		return
	}
	if errors.Is(err, errStatusCreateForbidden) {
		writeErrorCode(w, http.StatusForbidden, "status_create_forbidden", err.Error())
		return
	}
	if err != nil {
		slog.Warn("CreateIssueWorkflow statuses failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	name, description, steps = issueworkflow.Normalize(name, description, steps)
	catalog, err := h.workflowStatusCatalog(ctx, qtx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue statuses")
		return
	}
	if err := issueworkflow.Validate(name, description, steps, false, catalog); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_workflow", err.Error())
		return
	}
	if status, msg := h.validateHandoffHandlers(ctx, r, workspaceID, steps, nil); status != 0 {
		writeErrorCode(w, status, "invalid_handoff", msg)
		return
	}
	raw, err := issueworkflow.EncodeSteps(steps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	row, err := qtx.CreateIssueWorkflow(ctx, db.CreateIssueWorkflowParams{
		WorkspaceID:      wsUUID,
		Name:             name,
		Description:      description,
		InitialStatusKey: issueworkflow.StartStatus,
		Steps:            raw,
		OwnerID:          member.UserID,
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
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	if len(createdStatuses) > 0 {
		h.publishIssueStatusChanged(workspaceID, member, "created")
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
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
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
	if !canEditIssueWorkflow(member, current) {
		writeError(w, http.StatusForbidden, "only the workflow owner and workspace admins can change this workflow")
		return
	}
	var projects []db.Project
	if current.IsDefault {
		projects, err = qtx.ListProjectsUsingDefaultIssueWorkflow(ctx, wsUUID)
	} else {
		projects, err = qtx.ListProjectsUsingIssueWorkflow(ctx, db.ListProjectsUsingIssueWorkflowParams{WorkspaceID: wsUUID, WorkflowID: current.ID})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow")
		return
	}
	currentSteps, err := issueworkflow.DecodeSteps(current.Steps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read workflow")
		return
	}
	name, description, steps := current.Name, current.Description, currentSteps
	if req.Name != nil && !current.IsDefault {
		name = *req.Name
	}
	if req.Description != nil {
		description = *req.Description
	}
	var createdStatuses []db.IssueStatus
	if req.Steps != nil {
		var msg string
		steps, createdStatuses, msg, err = resolveStepInputs(ctx, qtx, wsUUID, *req.Steps, isWorkspaceAdmin(member))
		if msg != "" {
			writeErrorCode(w, http.StatusBadRequest, "invalid_workflow", msg)
			return
		}
		if errors.Is(err, errStatusCreateForbidden) {
			writeErrorCode(w, http.StatusForbidden, "status_create_forbidden", err.Error())
			return
		}
		if err != nil {
			slog.Warn("UpdateIssueWorkflow statuses failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to save workflow")
			return
		}
	}
	name, description, steps = issueworkflow.Normalize(name, description, steps)
	catalog, err := h.workflowStatusCatalog(ctx, qtx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue statuses")
		return
	}
	if err := issueworkflow.Validate(name, description, steps, current.IsDefault, catalog); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_workflow", err.Error())
		return
	}
	if status, msg := h.validateHandoffHandlers(ctx, r, workspaceID, steps, currentSteps); status != 0 {
		writeErrorCode(w, status, "invalid_handoff", msg)
		return
	}

	// Default accepts every status, so editing it never moves issues; issues
	// on a status it drops stay there and show under hidden columns.
	plan := workflowMappingPlan{Required: []StatusMappingRequirement{}, Unchanged: []StatusIssueCount{}}
	projectIDs := make([]pgtype.UUID, len(projects))
	for i, p := range projects {
		projectIDs[i] = p.ID
	}
	if !current.IsDefault && len(projectIDs) > 0 {
		next := issueworkflow.Definition{ID: current.ID, Name: name, Steps: steps}
		counts, err := qtx.CountProjectIssuesByStatus(ctx, db.CountProjectIssuesByStatusParams{WorkspaceID: wsUUID, ProjectIds: projectIDs})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save workflow")
			return
		}
		categoryOf := func(key string) string { return catalog[key].Category }
		plan = mappingPlan(counts, next.Has, func(key string) string { return issueworkflow.MoveTarget(next, key, categoryOf) })
		if req.DryRun {
			writeJSON(w, http.StatusOK, map[string]any{"dry_run": true, "plan": plan})
			return
		}
		if msg := checkMapping(plan, req.StatusMapping, next.Has); msg != "" {
			writeMappingRequired(w, plan, msg)
			return
		}
	} else if req.DryRun {
		writeJSON(w, http.StatusOK, map[string]any{"dry_run": true, "plan": plan})
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
		InitialStatusKey: issueworkflow.StartStatus,
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
	if len(createdStatuses) > 0 {
		h.publishIssueStatusChanged(workspaceID, member, "created")
	}
	h.publishIssueWorkflowChanged(workspaceID, member, "updated")
	h.writeIssueWorkflow(w, r, http.StatusOK, row)
}

func (h *Handler) DeleteIssueWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
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
	row, err := qtx.GetIssueWorkflow(ctx, db.GetIssueWorkflowParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !canEditIssueWorkflow(member, row) {
		writeError(w, http.StatusForbidden, "only the workflow owner and workspace admins can change this workflow")
		return
	}
	if row.IsDefault {
		writeErrorCode(w, http.StatusBadRequest, "workflow_is_default", "the Default workflow cannot be deleted")
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

// publishProjectWorkflowUpdate announces a project whose workflow settings
// changed and returns the project as the API shapes it.
func (h *Handler) publishProjectWorkflowUpdate(ctx context.Context, workspaceID string, member db.Member, project db.Project) ProjectResponse {
	resp := projectToResponse(project)
	resp.IssueCount, resp.DoneCount = h.loadProjectIssueStats(ctx, project.WorkspaceID, project.ID)
	resp.ResourceCount = h.loadProjectResourceCount(ctx, project.ID)
	h.publish(protocol.EventProjectUpdated, workspaceID, "member", uuidToString(member.UserID), map[string]any{"project": resp})
	return resp
}

// loadProjectForWorkflowWrite resolves the {id} project and checks that the
// caller may change its workflow settings.
func (h *Handler) loadProjectForWorkflowWrite(w http.ResponseWriter, r *http.Request) (db.Project, db.Member, string, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Project{}, db.Member{}, "", false
	}
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return db.Project{}, db.Member{}, "", false
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, db.Member{}, "", false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, db.Member{}, "", false
	}
	if !canManageProjectWorkflow(member, project) {
		writeError(w, http.StatusForbidden, "only workspace admins and the project lead can change this project's workflow")
		return db.Project{}, db.Member{}, "", false
	}
	return project, member, workspaceID, true
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
	loaded, member, workspaceID, ok := h.loadProjectForWorkflowWrite(w, r)
	if !ok {
		return
	}
	wsUUID := loaded.WorkspaceID
	var req SetProjectWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var targetID pgtype.UUID
	if req.WorkflowID != nil && *req.WorkflowID != "" {
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
	project, err := qtx.LockProjectForWorkflowChange(ctx, db.LockProjectForWorkflowChangeParams{ID: loaded.ID, WorkspaceID: wsUUID})
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
		if def.IsDefault {
			// Choosing the Default row is the same as choosing Default.
			targetID = pgtype.UUID{}
		} else {
			target = &def
		}
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
	h.publishIssueSnapshots(ctx, changed, "member", uuidToString(member.UserID))
	h.broadcastCancelledWakeups(ctx, wsUUID, cancelledWakeups)
	resp := h.publishProjectWorkflowUpdate(ctx, workspaceID, member, updated)
	writeJSON(w, http.StatusOK, map[string]any{
		"project":        resp,
		"updated_issues": len(changed),
		"plan":           plan,
	})
}

// SetIssueWorkflowHandoffRequest is a column's handoff. CopyForProjectID,
// when set, leaves the workflow as it is and saves the handoff on a copy of
// it that only that project uses.
type SetIssueWorkflowHandoffRequest struct {
	IssueWorkflowStepPayload
	CopyForProjectID string `json:"copy_for_project_id,omitempty"`
}

// SetIssueWorkflowHandoff saves what a workflow does when an issue enters
// one of its columns: who it is handed to, the instructions their run gets,
// and where the handler moves it next. It applies to every project using the
// workflow, unless the caller asks for a copy for one project: the project
// then switches to the copy in the same transaction. The copy lists the same
// statuses, so no issue moves.
func (h *Handler) SetIssueWorkflowHandoff(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	var req SetIssueWorkflowHandoffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	step := issueworkflow.NormalizeHandoff(payloadToStep(chi.URLParam(r, "status"), req.IssueWorkflowStepPayload))
	var copyFor pgtype.UUID
	if req.CopyForProjectID != "" {
		copyFor, ok = parseUUIDOrBadRequest(w, req.CopyForProjectID, "copy_for_project_id")
		if !ok {
			return
		}
	}
	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save handoff")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockIssueStatusCatalog(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save handoff")
		return
	}
	current, err := qtx.GetIssueWorkflow(ctx, db.GetIssueWorkflowParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !canEditIssueWorkflow(member, current) {
		writeError(w, http.StatusForbidden, "only the workflow owner and workspace admins can change this workflow")
		return
	}
	def, err := issueworkflow.Decode(current)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read workflow")
		return
	}
	if err := issueworkflow.ValidateHandoff(step, def); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_handoff", err.Error())
		return
	}
	if status, msg := h.validateHandoffHandlers(ctx, r, workspaceID, []issueworkflow.Step{step}, def.Steps); status != 0 {
		writeErrorCode(w, status, "invalid_handoff", msg)
		return
	}
	for i, s := range def.Steps {
		if s.StatusKey == step.StatusKey {
			def.Steps[i] = step
		}
	}
	raw, err := issueworkflow.EncodeSteps(def.Steps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save handoff")
		return
	}
	if copyFor.Valid {
		h.saveHandoffOnProjectCopy(w, r, qtx, tx, member, workspaceID, current, raw, copyFor)
		return
	}
	row, err := qtx.UpdateIssueWorkflow(ctx, db.UpdateIssueWorkflowParams{
		ID:               current.ID,
		WorkspaceID:      wsUUID,
		Name:             current.Name,
		Description:      current.Description,
		InitialStatusKey: issueworkflow.StartStatus,
		Steps:            raw,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save handoff")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save handoff")
		return
	}
	h.publishIssueWorkflowChanged(workspaceID, member, "updated")
	h.writeIssueWorkflow(w, r, http.StatusOK, row)
}

// saveHandoffOnProjectCopy finishes SetIssueWorkflowHandoff for a copy:
// steps already carry the new handoff. The caller holds the exclusive catalog
// lock in tx.
func (h *Handler) saveHandoffOnProjectCopy(w http.ResponseWriter, r *http.Request, qtx *db.Queries, tx pgx.Tx, member db.Member, workspaceID string, source db.IssueWorkflow, steps []byte, projectID pgtype.UUID) {
	ctx := r.Context()
	if source.IsDefault {
		writeErrorCode(w, http.StatusBadRequest, "invalid_handoff", "the Default workflow cannot be copied for one project")
		return
	}
	project, err := qtx.LockProjectForWorkflowChange(ctx, db.LockProjectForWorkflowChangeParams{ID: projectID, WorkspaceID: source.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if project.WorkflowID != source.ID {
		writeErrorCode(w, http.StatusConflict, "invalid_handoff", "the project does not use this workflow")
		return
	}
	// The copy switches the project's workflow, which only admins and the
	// project's lead may do.
	if !canManageProjectWorkflow(member, project) {
		writeError(w, http.StatusForbidden, "only workspace admins and the project lead can change this project's workflow")
		return
	}
	existing, err := qtx.ListIssueWorkflows(ctx, source.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save handoff")
		return
	}
	taken := make(map[string]bool, len(existing))
	for _, row := range existing {
		taken[strings.ToLower(row.Name)] = true
	}
	row, err := qtx.CreateIssueWorkflow(ctx, db.CreateIssueWorkflowParams{
		WorkspaceID:      source.WorkspaceID,
		Name:             projectCopyName(source.Name, project.Title, taken),
		Description:      source.Description,
		InitialStatusKey: issueworkflow.StartStatus,
		Steps:            steps,
		OwnerID:          member.UserID,
	})
	if err != nil {
		slog.Warn("SetIssueWorkflowHandoff copy failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save handoff")
		return
	}
	updated, err := qtx.SetProjectWorkflow(ctx, db.SetProjectWorkflowParams{ID: project.ID, WorkspaceID: source.WorkspaceID, WorkflowID: row.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save handoff")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save handoff")
		return
	}
	h.publishIssueWorkflowChanged(workspaceID, member, "created")
	h.publishProjectWorkflowUpdate(ctx, workspaceID, member, updated)
	h.writeIssueWorkflow(w, r, http.StatusCreated, row)
}

// projectCopyName names a workflow copied for one project, "Workflow
// (Project)", numbered when that name is taken and cut to the name limit.
func projectCopyName(workflow, project string, taken map[string]bool) string {
	for n := 1; ; n++ {
		suffix := " (" + project + ")"
		if n > 1 {
			suffix = " (" + project + " " + strconv.Itoa(n) + ")"
		}
		base := []rune(workflow)
		if room := issueworkflow.MaxNameRunes - utf8.RuneCountInString(suffix); room < len(base) {
			if room < 1 {
				suffix = " " + strconv.Itoa(n)
				room = issueworkflow.MaxNameRunes - len(suffix)
			}
			base = base[:room]
		}
		name := strings.TrimSpace(string(base)) + suffix
		if !taken[strings.ToLower(name)] {
			return name
		}
	}
}

// PreviewIssueWorkflowBriefRequest is a handoff being edited. ProjectID, when
// set, names the project the preview is for.
type PreviewIssueWorkflowBriefRequest struct {
	IssueWorkflowStepPayload
	ProjectID string `json:"project_id,omitempty"`
}

// PreviewIssueWorkflowBrief renders the brief a column's handler would
// receive, for a handoff being edited before it is saved.
func (h *Handler) PreviewIssueWorkflowBrief(w http.ResponseWriter, r *http.Request) {
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
	var req PreviewIssueWorkflowBriefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	row, err := h.Queries.GetIssueWorkflow(ctx, db.GetIssueWorkflowParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	def, err := issueworkflow.Decode(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read workflow")
		return
	}
	projectName := ""
	if req.ProjectID != "" {
		if id, err := parseUUIDString(req.ProjectID); err == nil {
			if p, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: id, WorkspaceID: wsUUID}); err == nil {
				projectName = p.Title
			}
		}
	}
	step := issueworkflow.NormalizeHandoff(payloadToStep(req.StatusKey, req.IssueWorkflowStepPayload))
	if !def.Has(step.StatusKey) {
		writeError(w, http.StatusBadRequest, "status_key is not a column of this workflow")
		return
	}
	// The brief names the draft's handler, so an unchecked id would let the
	// caller read the name of a user outside this workspace.
	if status, msg := h.validateHandoffHandlers(ctx, r, workspaceID, []issueworkflow.Step{step}, def.Steps); status != 0 {
		writeError(w, status, msg)
		return
	}
	for i, s := range def.Steps {
		if s.StatusKey == step.StatusKey {
			def.Steps[i] = step
		}
	}
	resolver := issuestatus.NewResolver(wsUUID)
	brief := issueworkflow.Brief(issueworkflow.BriefInput{
		Workflow:        def,
		ProjectName:     projectName,
		StatusKey:       step.StatusKey,
		IssueIdentifier: h.getIssuePrefix(ctx, wsUUID) + "-123",
		StatusName: func(key string) string {
			return resolver.Name(ctx, h.Queries, key)
		},
		HandlerName: func(s issueworkflow.Step) string {
			switch s.Handler.Type {
			case issueworkflow.HandlerAgent, issueworkflow.HandlerSquad, issueworkflow.HandlerMember:
				id, err := parseUUIDString(s.Handler.ID)
				if err != nil {
					return s.Handler.Type
				}
				return h.actorDisplayName(ctx, wsUUID, s.Handler.Type, id)
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
// and the brief the new handler would receive. With project_id it previews
// moving the issue into that project and entering the step it lands on
// (status then optional). It writes nothing.
func (h *Handler) PreviewWorkflowHandoff(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	projectParam := r.URL.Query().Get("project_id")
	if status == "" && projectParam == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}
	params := db.UpdateIssueParams{
		ProjectID:    issue.ProjectID,
		AssigneeType: issue.AssigneeType,
		AssigneeID:   issue.AssigneeID,
	}
	if status != "" {
		params.Status = pgtype.Text{String: status, Valid: true}
	}
	enterOnMove := false
	if projectParam != "" {
		projectID, ok := parseUUIDOrBadRequest(w, projectParam, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: issue.WorkspaceID}); err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		params.ProjectID = projectID
		enterOnMove = true
	}
	hand, err := h.applyIssueWorkflow(r.Context(), issue, &params, status != "", false, enterOnMove, nil)
	if err != nil {
		if writeIssueWorkflowError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve project workflow")
		return
	}
	if params.Status.Valid {
		status = params.Status.String
	} else {
		status = issue.Status
	}
	resp := WorkflowHandoffPreviewResponse{
		FromStatus:           issue.Status,
		ToStatus:             status,
		PreviousAssigneeType: textToPtr(issue.AssigneeType),
		PreviousAssigneeID:   uuidToPtr(issue.AssigneeID),
		PreviousRuns:         []WorkflowHandoffRun{},
	}
	if hand == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.WorkflowName = hand.def.Name
	resp.Handoff = true
	resp.HandlerType = params.AssigneeType.String
	resp.HandlerID = uuidToString(params.AssigneeID)
	next := issue
	next.Status = status
	next.ProjectID = params.ProjectID
	next.AssigneeType = params.AssigneeType
	next.AssigneeID = params.AssigneeID
	resp.Brief = h.workflowHandoffBrief(r.Context(), next, hand)
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
