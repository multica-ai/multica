package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/projectplan"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type createProjectPlanRequest struct {
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Attributes  json.RawMessage `json:"attributes"`
}

type createProjectPlanFromIssueRequest struct {
	SourceIssueID string          `json:"source_issue_id"`
	Kind          string          `json:"kind"`
	Attributes    json.RawMessage `json:"attributes"`
}

type projectPlanPatchRequest struct {
	Title       *string          `json:"title"`
	Description *string          `json:"description"`
	Kind        *string          `json:"kind"`
	Attributes  *json.RawMessage `json:"attributes"`
}

func (r projectPlanPatchRequest) patch() projectplan.PlanPatch {
	return projectplan.PlanPatch{
		Title: r.Title, Description: r.Description, Kind: r.Kind, Attributes: r.Attributes,
	}
}

type createProjectPlanPhaseRequest struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Attributes  json.RawMessage `json:"attributes"`
	Position    int32           `json:"position"`
}

type updateProjectPlanPhaseRequest struct {
	Title       *string          `json:"title"`
	Description *string          `json:"description"`
	Attributes  *json.RawMessage `json:"attributes"`
	Position    *int32           `json:"position"`
}

type createProjectPlanPartRequest struct {
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	AcceptanceCriteria string          `json:"acceptance_criteria"`
	Attributes         json.RawMessage `json:"attributes"`
	Position           int32           `json:"position"`
}

type updateProjectPlanPartRequest struct {
	Title              *string          `json:"title"`
	Description        *string          `json:"description"`
	AcceptanceCriteria *string          `json:"acceptance_criteria"`
	Attributes         *json.RawMessage `json:"attributes"`
	Position           *int32           `json:"position"`
}

type reorderProjectPlanItemsRequest struct {
	OrderedIDs []string `json:"ordered_ids"`
}

type projectPlanWriteContext struct {
	project db.Project
	actor   projectplan.Actor
}

type projectPlanResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	ProjectID     string          `json:"project_id"`
	Version       int32           `json:"version"`
	Kind          string          `json:"kind"`
	Origin        string          `json:"origin"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Attributes    json.RawMessage `json:"attributes"`
	SourceIssueID *string         `json:"source_issue_id"`
	Superseded    bool            `json:"superseded"`
	SupersededAt  *string         `json:"superseded_at"`
	CreatedByType string          `json:"created_by_type"`
	CreatedByID   string          `json:"created_by_id"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

type projectPlanPhaseResponse struct {
	ID            string          `json:"id"`
	ProjectPlanID string          `json:"project_plan_id"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Attributes    json.RawMessage `json:"attributes"`
	Position      int32           `json:"position"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

type projectPlanPartResponse struct {
	ID                 string          `json:"id"`
	ProjectPlanID      string          `json:"project_plan_id"`
	ProjectPlanPhaseID string          `json:"project_plan_phase_id"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	AcceptanceCriteria string          `json:"acceptance_criteria"`
	Attributes         json.RawMessage `json:"attributes"`
	Position           int32           `json:"position"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
}

type projectPlanIssueLinkResponse struct {
	ID                  string `json:"id"`
	ProjectPlanID       string `json:"project_plan_id"`
	ProjectPlanPartID   string `json:"project_plan_part_id"`
	IssueID             string `json:"issue_id"`
	IssueNumberSnapshot int32  `json:"issue_number_snapshot"`
	IssueTitleSnapshot  string `json:"issue_title_snapshot"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

type projectPlanDeleteImpactResponse struct {
	MembershipRows     int64 `json:"membership_rows"`
	LiveIssuesUnlinked int64 `json:"live_issues_unlinked"`
}

// GetActiveProjectPlan returns the complete read model for a project's active
// plan. A project without an active plan is a normal 404 empty state.
func (h *Handler) GetActiveProjectPlan(w http.ResponseWriter, r *http.Request) {
	if !h.projectPlanReadsEnabled(r) {
		writeError(w, http.StatusNotFound, "project plan not found")
		return
	}
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	overview, err := projectplan.NewReader(h.Queries).ReadActive(
		r.Context(), project.WorkspaceID, project.ID,
	)
	h.writeProjectPlanRead(w, r, overview, err)
}

// GetProjectPlan returns a retained plan version with live issue state.
func (h *Handler) GetProjectPlan(w http.ResponseWriter, r *http.Request) {
	if !h.projectPlanReadsEnabled(r) {
		writeError(w, http.StatusNotFound, "project plan not found")
		return
	}
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	planID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "planId"), "plan id")
	if !ok {
		return
	}

	overview, err := projectplan.NewReader(h.Queries).Read(
		r.Context(), project.WorkspaceID, project.ID, planID,
	)
	h.writeProjectPlanRead(w, r, overview, err)
}

func (h *Handler) projectPlanReadsEnabled(r *http.Request) bool {
	return featureflags.ProjectPlansEnabled(r.Context(), h.FeatureFlags)
}

func (h *Handler) writeProjectPlanRead(
	w http.ResponseWriter,
	r *http.Request,
	overview projectplan.Overview,
	err error,
) {
	if err == nil {
		writeJSON(w, http.StatusOK, overview)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project plan not found")
		return
	}
	slog.Error("project plan read failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "failed to read project plan")
}

func (h *Handler) projectPlanWriter(w http.ResponseWriter, r *http.Request) (projectPlanWriteContext, bool) {
	if !h.projectPlanReadsEnabled(r) {
		writeError(w, http.StatusNotFound, "project plan not found")
		return projectPlanWriteContext{}, false
	}
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return projectPlanWriteContext{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return projectPlanWriteContext{}, false
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(project.WorkspaceID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return projectPlanWriteContext{}, false
	}
	return projectPlanWriteContext{
		project: project,
		actor:   projectplan.Actor{Type: actorType, ID: actorUUID},
	}, true
}

func (h *Handler) projectPlanForWrite(
	w http.ResponseWriter,
	r *http.Request,
) (projectPlanWriteContext, pgtype.UUID, bool) {
	writeContext, ok := h.projectPlanWriter(w, r)
	if !ok {
		return projectPlanWriteContext{}, pgtype.UUID{}, false
	}
	planID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "planId"), "plan id")
	if !ok {
		return projectPlanWriteContext{}, pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetProjectPlanForRead(r.Context(), db.GetProjectPlanForReadParams{
		ID: planID, ProjectID: writeContext.project.ID, WorkspaceID: writeContext.project.WorkspaceID,
	}); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("project plan authorization lookup failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load project plan")
			return projectPlanWriteContext{}, pgtype.UUID{}, false
		}
		writeError(w, http.StatusNotFound, "project plan not found")
		return projectPlanWriteContext{}, pgtype.UUID{}, false
	}
	return writeContext, planID, true
}

func decodeProjectPlanRequest(w http.ResponseWriter, r *http.Request, request any) bool {
	if err := json.NewDecoder(r.Body).Decode(request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (h *Handler) writeProjectPlanError(w http.ResponseWriter, r *http.Request, err error) {
	var domainErr *projectplan.Error
	if !errors.As(err, &domainErr) {
		slog.Error("project plan write failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "project plan write failed")
		return
	}

	status := http.StatusInternalServerError
	switch domainErr.Kind {
	case projectplan.ErrorDisabled, projectplan.ErrorNotFound:
		status = http.StatusNotFound
	case projectplan.ErrorInvalid:
		status = http.StatusBadRequest
	case projectplan.ErrorNotActive,
		projectplan.ErrorActivePlanExists,
		projectplan.ErrorVersionConflict,
		projectplan.ErrorPositionConflict,
		projectplan.ErrorIssueAlreadyLinked:
		status = http.StatusConflict
	case projectplan.ErrorUnavailable:
		slog.Error("project plan write unavailable", append(logger.RequestAttrs(r), "error", err)...)
	}
	writeError(w, status, domainErr.Message)
}

// CreateProjectPlan creates a manually-authored active plan.
func (h *Handler) CreateProjectPlan(w http.ResponseWriter, r *http.Request) {
	writeContext, ok := h.projectPlanWriter(w, r)
	if !ok {
		return
	}
	var request createProjectPlanRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	plan, err := h.ProjectPlanService.CreateManual(r.Context(), projectplan.CreateManualParams{
		WorkspaceID: writeContext.project.WorkspaceID,
		ProjectID:   writeContext.project.ID,
		Kind:        request.Kind,
		Title:       request.Title,
		Description: request.Description,
		Attributes:  request.Attributes,
		CreatedBy:   writeContext.actor,
	})
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectPlanToResponse(plan))
}

// CreateProjectPlanFromIssue snapshots an issue into a new active plan.
func (h *Handler) CreateProjectPlanFromIssue(w http.ResponseWriter, r *http.Request) {
	writeContext, ok := h.projectPlanWriter(w, r)
	if !ok {
		return
	}
	var request createProjectPlanFromIssueRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	sourceIssueID, ok := parseUUIDOrBadRequest(w, request.SourceIssueID, "source_issue_id")
	if !ok {
		return
	}
	plan, err := h.ProjectPlanService.CreateFromIssue(r.Context(), projectplan.CreateFromIssueParams{
		WorkspaceID:   writeContext.project.WorkspaceID,
		ProjectID:     writeContext.project.ID,
		SourceIssueID: sourceIssueID,
		Kind:          request.Kind,
		Attributes:    request.Attributes,
		CreatedBy:     writeContext.actor,
	})
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectPlanToResponse(plan))
}

// UpdateProjectPlan applies a partial update to an active plan.
func (h *Handler) UpdateProjectPlan(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	var request projectPlanPatchRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	plan, err := h.ProjectPlanService.UpdatePlan(
		r.Context(), writeContext.project.WorkspaceID, planID, request.patch(),
	)
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectPlanToResponse(plan))
}

// SupersedeProjectPlan atomically replaces an active plan with its next version.
func (h *Handler) SupersedeProjectPlan(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	var request projectPlanPatchRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	plan, err := h.ProjectPlanService.Supersede(r.Context(), projectplan.SupersedeParams{
		WorkspaceID: writeContext.project.WorkspaceID,
		PlanID:      planID,
		Patch:       request.patch(),
		CreatedBy:   writeContext.actor,
	})
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectPlanToResponse(plan))
}

// AddProjectPlanPhase appends a phase at the requested position.
func (h *Handler) AddProjectPlanPhase(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	var request createProjectPlanPhaseRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	phase, err := h.ProjectPlanService.AddPhase(r.Context(), projectplan.CreatePhaseParams{
		WorkspaceID: writeContext.project.WorkspaceID,
		PlanID:      planID,
		Title:       request.Title,
		Description: request.Description,
		Attributes:  request.Attributes,
		Position:    request.Position,
	})
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectPlanPhaseToResponse(phase))
}

// UpdateProjectPlanPhase applies a partial update to a phase.
func (h *Handler) UpdateProjectPlanPhase(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	phaseID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "phaseId"), "phase id")
	if !ok {
		return
	}
	var request updateProjectPlanPhaseRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	phase, err := h.ProjectPlanService.UpdatePhase(
		r.Context(), writeContext.project.WorkspaceID, planID, phaseID,
		projectplan.PhasePatch{
			Title: request.Title, Description: request.Description,
			Attributes: request.Attributes, Position: request.Position,
		},
	)
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectPlanPhaseToResponse(phase))
}

// ReorderProjectPlanPhases replaces the complete phase ordering.
func (h *Handler) ReorderProjectPlanPhases(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	var request reorderProjectPlanItemsRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	orderedIDs, ok := parseUUIDSliceOrBadRequest(w, request.OrderedIDs, "ordered_ids")
	if !ok {
		return
	}
	if err := h.ProjectPlanService.ReorderPhases(
		r.Context(), writeContext.project.WorkspaceID, planID, orderedIDs,
	); err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteProjectPlanPhase removes a phase and its contained structure.
func (h *Handler) DeleteProjectPlanPhase(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	phaseID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "phaseId"), "phase id")
	if !ok {
		return
	}
	if err := h.ProjectPlanService.DeletePhase(
		r.Context(), writeContext.project.WorkspaceID, planID, phaseID,
	); err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddProjectPlanPart adds a part to a phase.
func (h *Handler) AddProjectPlanPart(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	phaseID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "phaseId"), "phase id")
	if !ok {
		return
	}
	var request createProjectPlanPartRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	part, err := h.ProjectPlanService.AddPart(r.Context(), projectplan.CreatePartParams{
		WorkspaceID:        writeContext.project.WorkspaceID,
		PlanID:             planID,
		PhaseID:            phaseID,
		Title:              request.Title,
		Description:        request.Description,
		AcceptanceCriteria: request.AcceptanceCriteria,
		Attributes:         request.Attributes,
		Position:           request.Position,
	})
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectPlanPartToResponse(part))
}

// UpdateProjectPlanPart applies a partial update to a part.
func (h *Handler) UpdateProjectPlanPart(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	partID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "partId"), "part id")
	if !ok {
		return
	}
	var request updateProjectPlanPartRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	part, err := h.ProjectPlanService.UpdatePart(
		r.Context(), writeContext.project.WorkspaceID, planID, partID,
		projectplan.PartPatch{
			Title: request.Title, Description: request.Description,
			AcceptanceCriteria: request.AcceptanceCriteria,
			Attributes:         request.Attributes, Position: request.Position,
		},
	)
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectPlanPartToResponse(part))
}

// ReorderProjectPlanParts replaces the complete ordering inside one phase.
func (h *Handler) ReorderProjectPlanParts(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	phaseID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "phaseId"), "phase id")
	if !ok {
		return
	}
	var request reorderProjectPlanItemsRequest
	if !decodeProjectPlanRequest(w, r, &request) {
		return
	}
	orderedIDs, ok := parseUUIDSliceOrBadRequest(w, request.OrderedIDs, "ordered_ids")
	if !ok {
		return
	}
	if err := h.ProjectPlanService.ReorderParts(
		r.Context(), writeContext.project.WorkspaceID, planID, phaseID, orderedIDs,
	); err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteProjectPlanPart removes a part and its links.
func (h *Handler) DeleteProjectPlanPart(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	partID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "partId"), "part id")
	if !ok {
		return
	}
	if err := h.ProjectPlanService.DeletePart(
		r.Context(), writeContext.project.WorkspaceID, planID, partID,
	); err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LinkProjectPlanIssue links a project issue to a part.
func (h *Handler) LinkProjectPlanIssue(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	partID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "partId"), "part id")
	if !ok {
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue id")
	if !ok {
		return
	}
	link, err := h.ProjectPlanService.LinkIssue(
		r.Context(), writeContext.project.WorkspaceID, planID, partID, issueID,
	)
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectPlanIssueLinkToResponse(link))
}

// UnlinkProjectPlanIssue removes an issue link from a part.
func (h *Handler) UnlinkProjectPlanIssue(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	partID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "partId"), "part id")
	if !ok {
		return
	}
	issueID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue id")
	if !ok {
		return
	}
	if err := h.ProjectPlanService.UnlinkIssue(
		r.Context(), writeContext.project.WorkspaceID, planID, partID, issueID,
	); err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetProjectPlanDeleteImpact previews the rows affected by deleting a plan.
func (h *Handler) GetProjectPlanDeleteImpact(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	impact, err := h.ProjectPlanService.DeleteImpact(
		r.Context(), writeContext.project.WorkspaceID, planID,
	)
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectPlanDeleteImpactToResponse(impact))
}

// DeleteProjectPlan removes a plan and returns the committed impact.
func (h *Handler) DeleteProjectPlan(w http.ResponseWriter, r *http.Request) {
	writeContext, planID, ok := h.projectPlanForWrite(w, r)
	if !ok {
		return
	}
	impact, err := h.ProjectPlanService.DeletePlan(
		r.Context(), writeContext.project.WorkspaceID, planID,
	)
	if err != nil {
		h.writeProjectPlanError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectPlanDeleteImpactToResponse(impact))
}

func projectPlanToResponse(plan db.ProjectPlan) projectPlanResponse {
	return projectPlanResponse{
		ID: uuidToString(plan.ID), WorkspaceID: uuidToString(plan.WorkspaceID),
		ProjectID: uuidToString(plan.ProjectID), Version: plan.Version, Kind: plan.Kind,
		Origin: plan.Origin, Title: plan.Title, Description: plan.Description,
		Attributes: json.RawMessage(plan.Attributes), SourceIssueID: uuidToPtr(plan.SourceIssueID),
		Superseded: plan.SupersededAt.Valid, SupersededAt: timestampToPtr(plan.SupersededAt),
		CreatedByType: plan.CreatedByType, CreatedByID: uuidToString(plan.CreatedByID),
		CreatedAt: timestampToString(plan.CreatedAt), UpdatedAt: timestampToString(plan.UpdatedAt),
	}
}

func projectPlanPhaseToResponse(phase db.ProjectPlanPhase) projectPlanPhaseResponse {
	return projectPlanPhaseResponse{
		ID: uuidToString(phase.ID), ProjectPlanID: uuidToString(phase.ProjectPlanID),
		Title: phase.Title, Description: phase.Description, Attributes: json.RawMessage(phase.Attributes),
		Position: phase.Position, CreatedAt: timestampToString(phase.CreatedAt),
		UpdatedAt: timestampToString(phase.UpdatedAt),
	}
}

func projectPlanPartToResponse(part db.ProjectPlanPart) projectPlanPartResponse {
	return projectPlanPartResponse{
		ID: uuidToString(part.ID), ProjectPlanID: uuidToString(part.ProjectPlanID),
		ProjectPlanPhaseID: uuidToString(part.ProjectPlanPhaseID), Title: part.Title,
		Description: part.Description, AcceptanceCriteria: part.AcceptanceCriteria,
		Attributes: json.RawMessage(part.Attributes), Position: part.Position,
		CreatedAt: timestampToString(part.CreatedAt), UpdatedAt: timestampToString(part.UpdatedAt),
	}
}

func projectPlanIssueLinkToResponse(link db.ProjectPlanPartIssue) projectPlanIssueLinkResponse {
	return projectPlanIssueLinkResponse{
		ID: uuidToString(link.ID), ProjectPlanID: uuidToString(link.ProjectPlanID),
		ProjectPlanPartID: uuidToString(link.ProjectPlanPartID), IssueID: uuidToString(link.IssueID),
		IssueNumberSnapshot: link.IssueNumberSnapshot, IssueTitleSnapshot: link.IssueTitleSnapshot,
		CreatedAt: timestampToString(link.CreatedAt), UpdatedAt: timestampToString(link.UpdatedAt),
	}
}

func projectPlanDeleteImpactToResponse(impact projectplan.DeleteImpact) projectPlanDeleteImpactResponse {
	return projectPlanDeleteImpactResponse{
		MembershipRows: impact.MembershipRows, LiveIssuesUnlinked: impact.LiveIssuesUnlinked,
	}
}
