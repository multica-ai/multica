package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	defaultInitiativeListLimit  = 100
	maxInitiativeListLimit      = 200
	defaultInitiativeEventLimit = 50
	maxInitiativeEventLimit     = 200
	// maxInitiativeTitleLength keeps derived titles list-friendly; the full
	// idea text is preserved on the initiative row.
	maxInitiativeTitleLength = 120
)

type InitiativeResponse struct {
	ID                         string          `json:"id"`
	WorkspaceID                string          `json:"workspace_id"`
	Title                      string          `json:"title"`
	Idea                       string          `json:"idea"`
	Constraints                json.RawMessage `json:"constraints"`
	Status                     string          `json:"status"`
	AutonomyLevel              *int16          `json:"autonomy_level"`
	PlanVersion                int32           `json:"plan_version"`
	OrchestratorAgentID        *string         `json:"orchestrator_agent_id"`
	BudgetLimitTokens          *int64          `json:"budget_limit_tokens"`
	BudgetSpentTokens          int64           `json:"budget_spent_tokens"`
	MaxParallelTasks           *int32          `json:"max_parallel_tasks"`
	MaxAttempts                *int32          `json:"max_attempts"`
	StallTimeoutSeconds        *int32          `json:"stall_timeout_seconds"`
	ExternalWaitTimeoutSeconds *int32          `json:"external_wait_timeout_seconds"`
	PausePrevStatus            *string         `json:"pause_prev_status"`
	PauseReason                *string         `json:"pause_reason"`
	NeedsHumanReason           *string         `json:"needs_human_reason"`
	CreatedBy                  string          `json:"created_by"`
	ApprovedBy                 *string         `json:"approved_by"`
	ApprovedAt                 *string         `json:"approved_at"`
	CreatedAt                  string          `json:"created_at"`
	UpdatedAt                  string          `json:"updated_at"`
}

type InitiativeTaskResponse struct {
	ID                   string          `json:"id"`
	InitiativeID         string          `json:"initiative_id"`
	WorkspaceID          string          `json:"workspace_id"`
	PlanVersion          int32           `json:"plan_version"`
	TaskKey              string          `json:"task_key"`
	Title                string          `json:"title"`
	Description          string          `json:"description"`
	Role                 string          `json:"role"`
	DependsOn            []string        `json:"depends_on"`
	AcceptanceCriteria   json.RawMessage `json:"acceptance_criteria"`
	RequiredCapabilities []string        `json:"required_capabilities"`
	State                string          `json:"state"`
	StateReason          *string         `json:"state_reason"`
	Attempt              int32           `json:"attempt"`
	MaxAttempts          *int32          `json:"max_attempts"`
	AssigneeHint         json.RawMessage `json:"assignee_hint"`
	IssueID              *string         `json:"issue_id"`
	Branch               *string         `json:"branch"`
	StallStrikes         int32           `json:"stall_strikes"`
	LastActivityAt       *string         `json:"last_activity_at"`
	CreatedAt            string          `json:"created_at"`
	UpdatedAt            string          `json:"updated_at"`
}

type InitiativeEventResponse struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	InitiativeID string          `json:"initiative_id"`
	TaskID       *string         `json:"task_id"`
	ActorType    string          `json:"actor_type"`
	ActorID      *string         `json:"actor_id"`
	EventType    string          `json:"event_type"`
	Payload      json.RawMessage `json:"payload"`
	CreatedAt    string          `json:"created_at"`
}

type InitiativeBlockerResponse struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	InitiativeID    string          `json:"initiative_id"`
	TaskID          string          `json:"task_id"`
	SourceCommentID *string         `json:"source_comment_id"`
	Category        *string         `json:"category"`
	Status          string          `json:"status"`
	Question        string          `json:"question"`
	Resolution      json.RawMessage `json:"resolution"`
	AnsweredBy      *string         `json:"answered_by"`
	AnsweredAt      *string         `json:"answered_at"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

// InitiativeProgress is the done/total task rollup for the current plan
// version, shown on list rows and the detail header.
type InitiativeProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

func initiativeToResponse(i db.Initiative) InitiativeResponse {
	constraints := json.RawMessage(i.Constraints)
	if len(constraints) == 0 {
		constraints = json.RawMessage("{}")
	}
	return InitiativeResponse{
		ID:                         uuidToString(i.ID),
		WorkspaceID:                uuidToString(i.WorkspaceID),
		Title:                      i.Title,
		Idea:                       i.Idea,
		Constraints:                constraints,
		Status:                     i.Status,
		AutonomyLevel:              int2ToPtr(i.AutonomyLevel),
		PlanVersion:                i.PlanVersion,
		OrchestratorAgentID:        uuidToPtr(i.OrchestratorAgentID),
		BudgetLimitTokens:          int8ToPtr(i.BudgetLimitTokens),
		BudgetSpentTokens:          i.BudgetSpentTokens,
		MaxParallelTasks:           int4ToPtr(i.MaxParallelTasks),
		MaxAttempts:                int4ToPtr(i.MaxAttempts),
		StallTimeoutSeconds:        int4ToPtr(i.StallTimeoutSeconds),
		ExternalWaitTimeoutSeconds: int4ToPtr(i.ExternalWaitTimeoutSeconds),
		PausePrevStatus:            textToPtr(i.PausePrevStatus),
		PauseReason:                textToPtr(i.PauseReason),
		NeedsHumanReason:           textToPtr(i.NeedsHumanReason),
		CreatedBy:                  uuidToString(i.CreatedBy),
		ApprovedBy:                 uuidToPtr(i.ApprovedBy),
		ApprovedAt:                 timestampToPtr(i.ApprovedAt),
		CreatedAt:                  timestampToString(i.CreatedAt),
		UpdatedAt:                  timestampToString(i.UpdatedAt),
	}
}

func initiativeTaskToResponse(t db.InitiativeTask) InitiativeTaskResponse {
	criteria := json.RawMessage(t.AcceptanceCriteria)
	if len(criteria) == 0 {
		criteria = json.RawMessage("[]")
	}
	hint := json.RawMessage(t.AssigneeHint)
	if len(hint) == 0 {
		hint = json.RawMessage("{}")
	}
	capabilities := t.RequiredCapabilities
	if capabilities == nil {
		capabilities = []string{}
	}
	return InitiativeTaskResponse{
		ID:                   uuidToString(t.ID),
		InitiativeID:         uuidToString(t.InitiativeID),
		WorkspaceID:          uuidToString(t.WorkspaceID),
		PlanVersion:          t.PlanVersion,
		TaskKey:              t.TaskKey,
		Title:                t.Title,
		Description:          t.Description,
		Role:                 t.Role,
		DependsOn:            uuidStringsOrEmpty(t.DependsOn),
		AcceptanceCriteria:   criteria,
		RequiredCapabilities: capabilities,
		State:                t.State,
		StateReason:          textToPtr(t.StateReason),
		Attempt:              t.Attempt,
		MaxAttempts:          int4ToPtr(t.MaxAttempts),
		AssigneeHint:         hint,
		IssueID:              uuidToPtr(t.IssueID),
		Branch:               textToPtr(t.Branch),
		StallStrikes:         t.StallStrikes,
		LastActivityAt:       timestampToPtr(t.LastActivityAt),
		CreatedAt:            timestampToString(t.CreatedAt),
		UpdatedAt:            timestampToString(t.UpdatedAt),
	}
}

func initiativeEventToResponse(e db.InitiativeEvent) InitiativeEventResponse {
	payload := json.RawMessage(e.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	return InitiativeEventResponse{
		ID:           uuidToString(e.ID),
		WorkspaceID:  uuidToString(e.WorkspaceID),
		InitiativeID: uuidToString(e.InitiativeID),
		TaskID:       uuidToPtr(e.TaskID),
		ActorType:    e.ActorType,
		ActorID:      uuidToPtr(e.ActorID),
		EventType:    e.EventType,
		Payload:      payload,
		CreatedAt:    timestampToString(e.CreatedAt),
	}
}

func initiativeBlockerToResponse(b db.InitiativeBlocker) InitiativeBlockerResponse {
	return InitiativeBlockerResponse{
		ID:              uuidToString(b.ID),
		WorkspaceID:     uuidToString(b.WorkspaceID),
		InitiativeID:    uuidToString(b.InitiativeID),
		TaskID:          uuidToString(b.TaskID),
		SourceCommentID: uuidToPtr(b.SourceCommentID),
		Category:        textToPtr(b.Category),
		Status:          b.Status,
		Question:        b.Question,
		Resolution:      json.RawMessage(b.Resolution),
		AnsweredBy:      uuidToPtr(b.AnsweredBy),
		AnsweredAt:      timestampToPtr(b.AnsweredAt),
		CreatedAt:       timestampToString(b.CreatedAt),
		UpdatedAt:       timestampToString(b.UpdatedAt),
	}
}

// requireInitiativesEnabled hides the whole surface behind the release flag.
// 404 (not 403) so the feature is indistinguishable from absent while off.
func (h *Handler) requireInitiativesEnabled(w http.ResponseWriter, r *http.Request) bool {
	if !featureflags.InitiativesEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	return true
}

// loadInitiativeForUser resolves an initiative id path param within the
// request's workspace, writing the error response itself on failure.
// Initiatives are UUID-only (no human-readable identifier format).
func (h *Handler) loadInitiativeForUser(w http.ResponseWriter, r *http.Request, initiativeID string) (db.Initiative, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return db.Initiative{}, false
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Initiative{}, false
	}

	initiativeUUID, err := util.ParseUUID(initiativeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "initiative not found")
		return db.Initiative{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Initiative{}, false
	}

	initiative, err := h.Queries.GetInitiativeInWorkspace(r.Context(), db.GetInitiativeInWorkspaceParams{
		ID:          initiativeUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "initiative not found")
		return db.Initiative{}, false
	}
	return initiative, true
}

// writeInitiativeTransitionError maps service transition sentinels to HTTP.
func writeInitiativeTransitionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInitiativeInvalidTransition),
		errors.Is(err, service.ErrInitiativeTransitionConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "failed to update initiative")
	}
}

type CreateInitiativeRequest struct {
	Title                      string          `json:"title"`
	Idea                       string          `json:"idea"`
	Constraints                json.RawMessage `json:"constraints"`
	AutonomyLevel              *int16          `json:"autonomy_level"`
	BudgetLimitTokens          *int64          `json:"budget_limit_tokens"`
	MaxParallelTasks           *int32          `json:"max_parallel_tasks"`
	MaxAttempts                *int32          `json:"max_attempts"`
	StallTimeoutSeconds        *int32          `json:"stall_timeout_seconds"`
	ExternalWaitTimeoutSeconds *int32          `json:"external_wait_timeout_seconds"`
}

func (h *Handler) CreateInitiative(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateInitiativeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Idea) == "" {
		writeError(w, http.StatusBadRequest, "idea is required")
		return
	}
	if len(req.Constraints) > 0 && !json.Valid(req.Constraints) {
		writeError(w, http.StatusBadRequest, "constraints must be valid JSON")
		return
	}
	if req.AutonomyLevel != nil && (*req.AutonomyLevel < 1 || *req.AutonomyLevel > 3) {
		writeError(w, http.StatusBadRequest, "autonomy_level must be between 1 and 3")
		return
	}
	// The remaining numeric overrides are CHECK-constrained > 0 in the schema;
	// validate here so bad input is a 400, not a constraint-violation 500.
	for field, v := range map[string]*int64{
		"budget_limit_tokens": req.BudgetLimitTokens,
	} {
		if v != nil && *v <= 0 {
			writeError(w, http.StatusBadRequest, field+" must be positive")
			return
		}
	}
	for field, v := range map[string]*int32{
		"max_parallel_tasks":            req.MaxParallelTasks,
		"max_attempts":                  req.MaxAttempts,
		"stall_timeout_seconds":         req.StallTimeoutSeconds,
		"external_wait_timeout_seconds": req.ExternalWaitTimeoutSeconds,
	} {
		if v != nil && *v <= 0 {
			writeError(w, http.StatusBadRequest, field+" must be positive")
			return
		}
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = deriveInitiativeTitle(req.Idea)
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var autonomy pgtype.Int2
	if req.AutonomyLevel != nil {
		autonomy = pgtype.Int2{Int16: *req.AutonomyLevel, Valid: true}
	}
	var budget pgtype.Int8
	if req.BudgetLimitTokens != nil {
		budget = pgtype.Int8{Int64: *req.BudgetLimitTokens, Valid: true}
	}

	initiative, err := h.InitiativeService.Create(r.Context(), service.InitiativeCreateParams{
		WorkspaceID:                wsUUID,
		Title:                      title,
		Idea:                       req.Idea,
		Constraints:                req.Constraints,
		AutonomyLevel:              autonomy,
		BudgetLimitTokens:          budget,
		MaxParallelTasks:           ptrToInt4(req.MaxParallelTasks),
		MaxAttempts:                ptrToInt4(req.MaxAttempts),
		StallTimeoutSeconds:        ptrToInt4(req.StallTimeoutSeconds),
		ExternalWaitTimeoutSeconds: ptrToInt4(req.ExternalWaitTimeoutSeconds),
		CreatedBy:                  member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create initiative")
		return
	}

	writeJSON(w, http.StatusCreated, initiativeToResponse(initiative))
}

// deriveInitiativeTitle falls back to the idea's first non-empty line when no
// explicit title was provided, truncated on a rune boundary. Callers guarantee
// the idea is not blank.
func deriveInitiativeTitle(idea string) string {
	title := ""
	for line := range strings.SplitSeq(idea, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			title = trimmed
			break
		}
	}
	runes := []rune(title)
	if len(runes) > maxInitiativeTitleLength {
		return string(runes[:maxInitiativeTitleLength])
	}
	return title
}

type ListInitiativesResponse struct {
	Initiatives []InitiativeResponse `json:"initiatives"`
}

func (h *Handler) ListInitiatives(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	limit := parseBoundedLimit(r.URL.Query().Get("limit"), defaultInitiativeListLimit, maxInitiativeListLimit)
	var status pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		status = strToText(s)
	}

	initiatives, err := h.Queries.ListInitiativesByWorkspace(r.Context(), db.ListInitiativesByWorkspaceParams{
		WorkspaceID: wsUUID,
		Status:      status,
		Limit:       limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list initiatives")
		return
	}

	out := make([]InitiativeResponse, 0, len(initiatives))
	for _, initiative := range initiatives {
		out = append(out, initiativeToResponse(initiative))
	}
	writeJSON(w, http.StatusOK, ListInitiativesResponse{Initiatives: out})
}

func parseBoundedLimit(raw string, def, max int32) int32 {
	if raw == "" {
		return def
	}
	// 32-bit parse so out-of-range values fail here instead of wrapping into a
	// negative SQL LIMIT.
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || parsed < 1 {
		return def
	}
	if int32(parsed) > max {
		return max
	}
	return int32(parsed)
}

type InitiativeDetailResponse struct {
	Initiative       InitiativeResponse       `json:"initiative"`
	Tasks            []InitiativeTaskResponse `json:"tasks"`
	Progress         InitiativeProgress       `json:"progress"`
	OpenBlockerCount int                      `json:"open_blocker_count"`
}

func (h *Handler) GetInitiative(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	tasks, err := h.Queries.ListInitiativeTasks(r.Context(), db.ListInitiativeTasksParams{
		InitiativeID: initiative.ID,
		PlanVersion:  initiative.PlanVersion,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load initiative tasks")
		return
	}

	openBlockers, err := h.Queries.ListInitiativeBlockers(r.Context(), db.ListInitiativeBlockersParams{
		InitiativeID: initiative.ID,
		Status:       strToText("open"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load initiative blockers")
		return
	}

	taskResponses := make([]InitiativeTaskResponse, 0, len(tasks))
	progress := InitiativeProgress{Total: len(tasks)}
	for _, task := range tasks {
		if task.State == service.InitiativeTaskStateDone {
			progress.Done++
		}
		taskResponses = append(taskResponses, initiativeTaskToResponse(task))
	}

	writeJSON(w, http.StatusOK, InitiativeDetailResponse{
		Initiative:       initiativeToResponse(initiative),
		Tasks:            taskResponses,
		Progress:         progress,
		OpenBlockerCount: len(openBlockers),
	})
}

type UpdateInitiativeRequest struct {
	Title             *string         `json:"title"`
	Idea              *string         `json:"idea"`
	Constraints       json.RawMessage `json:"constraints"`
	AutonomyLevel     *int16          `json:"autonomy_level"`
	BudgetLimitTokens *int64          `json:"budget_limit_tokens"`
}

func (h *Handler) UpdateInitiative(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, uuidToString(initiative.WorkspaceID))
	if !ok {
		return
	}

	var req UpdateInitiativeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title cannot be empty")
		return
	}
	if req.Idea != nil && strings.TrimSpace(*req.Idea) == "" {
		writeError(w, http.StatusBadRequest, "idea cannot be empty")
		return
	}
	if len(req.Constraints) > 0 && !json.Valid(req.Constraints) {
		writeError(w, http.StatusBadRequest, "constraints must be valid JSON")
		return
	}
	if req.AutonomyLevel != nil && (*req.AutonomyLevel < 1 || *req.AutonomyLevel > 3) {
		writeError(w, http.StatusBadRequest, "autonomy_level must be between 1 and 3")
		return
	}
	if req.BudgetLimitTokens != nil && *req.BudgetLimitTokens <= 0 {
		writeError(w, http.StatusBadRequest, "budget_limit_tokens must be positive")
		return
	}

	var autonomy pgtype.Int2
	if req.AutonomyLevel != nil {
		autonomy = pgtype.Int2{Int16: *req.AutonomyLevel, Valid: true}
	}
	var budget pgtype.Int8
	if req.BudgetLimitTokens != nil {
		budget = pgtype.Int8{Int64: *req.BudgetLimitTokens, Valid: true}
	}

	updated, err := h.InitiativeService.UpdateMeta(r.Context(), initiative,
		service.MemberInitiativeActor(member.UserID), db.UpdateInitiativeMetaParams{
			Title:             util.PtrToText(req.Title),
			Idea:              util.PtrToText(req.Idea),
			Constraints:       req.Constraints,
			AutonomyLevel:     autonomy,
			BudgetLimitTokens: budget,
		})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update initiative")
		return
	}
	writeJSON(w, http.StatusOK, initiativeToResponse(updated))
}

type RequestInitiativePlanRequest struct {
	Reason string `json:"reason"`
}

// RequestInitiativePlan moves the initiative into `planning`. The planner
// decision dispatch hooks in here once the decision substrate lands (PR 2/3);
// until then the transition itself is the whole behavior.
func (h *Handler) RequestInitiativePlan(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, uuidToString(initiative.WorkspaceID))
	if !ok {
		return
	}

	var req RequestInitiativePlanRequest
	if r.Body != nil {
		// Body is optional; ignore decode errors from an empty body.
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	updated, err := h.InitiativeService.Transition(r.Context(), initiative, service.InitiativeStatusPlanning,
		service.MemberInitiativeActor(member.UserID), service.InitiativeTransitionOpts{
			EventPayload: map[string]any{"trigger": "plan_requested", "reason": req.Reason},
		})
	if err != nil {
		writeInitiativeTransitionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, initiativeToResponse(updated))
}

type InitiativePlanResponse struct {
	PlanVersion int32                    `json:"plan_version"`
	Tasks       []InitiativeTaskResponse `json:"tasks"`
}

func (h *Handler) GetInitiativePlan(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	version := initiative.PlanVersion
	if raw := r.URL.Query().Get("version"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "invalid version")
			return
		}
		version = int32(parsed)
	}

	tasks, err := h.Queries.ListInitiativeTasks(r.Context(), db.ListInitiativeTasksParams{
		InitiativeID: initiative.ID,
		PlanVersion:  version,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load initiative plan")
		return
	}

	out := make([]InitiativeTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, initiativeTaskToResponse(task))
	}
	writeJSON(w, http.StatusOK, InitiativePlanResponse{PlanVersion: version, Tasks: out})
}

func (h *Handler) ApproveInitiativePlan(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, uuidToString(initiative.WorkspaceID))
	if !ok {
		return
	}

	updated, err := h.InitiativeService.ApprovePlan(r.Context(), initiative, member.ID)
	if err != nil {
		if errors.Is(err, service.ErrInitiativeTransitionConflict) {
			writeError(w, http.StatusConflict, "initiative is not awaiting plan approval")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to approve plan")
		return
	}
	writeJSON(w, http.StatusOK, initiativeToResponse(updated))
}

type PauseInitiativeRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) PauseInitiative(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, uuidToString(initiative.WorkspaceID))
	if !ok {
		return
	}

	var req PauseInitiativeRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	updated, err := h.InitiativeService.Pause(r.Context(), initiative, service.MemberInitiativeActor(member.UserID), req.Reason)
	if err != nil {
		writeInitiativeTransitionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, initiativeToResponse(updated))
}

func (h *Handler) ResumeInitiative(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, uuidToString(initiative.WorkspaceID))
	if !ok {
		return
	}

	updated, err := h.InitiativeService.Resume(r.Context(), initiative, service.MemberInitiativeActor(member.UserID))
	if err != nil {
		writeInitiativeTransitionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, initiativeToResponse(updated))
}

type CancelInitiativeRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) CancelInitiative(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, uuidToString(initiative.WorkspaceID))
	if !ok {
		return
	}

	var req CancelInitiativeRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	updated, err := h.InitiativeService.Cancel(r.Context(), initiative, service.MemberInitiativeActor(member.UserID), req.Reason)
	if err != nil {
		writeInitiativeTransitionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, initiativeToResponse(updated))
}

// InitiativeEventsCursorResponse is the keyset cursor for the next (older)
// page. CreatedAt is RFC3339Nano — full microsecond precision — because the
// event rows' display timestamps are second-precision RFC3339 and truncated
// cursors silently skip same-second events (same contract as chat messages,
// see ChatMessagesCursorResponse).
type InitiativeEventsCursorResponse struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

type ListInitiativeEventsResponse struct {
	Events     []InitiativeEventResponse       `json:"events"`
	HasMore    bool                            `json:"has_more"`
	NextCursor *InitiativeEventsCursorResponse `json:"next_cursor,omitempty"`
}

func (h *Handler) ListInitiativeEvents(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	limit := parseBoundedLimit(r.URL.Query().Get("limit"), defaultInitiativeEventLimit, maxInitiativeEventLimit)

	var beforeCreatedAt pgtype.Timestamptz
	var beforeID pgtype.UUID
	if raw := r.URL.Query().Get("before_created_at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before_created_at")
			return
		}
		beforeCreatedAt = pgtype.Timestamptz{Time: parsed, Valid: true}
		id, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("before_id"), "before_id")
		if !ok {
			return
		}
		beforeID = id
	}

	// Fetch one extra row to learn whether an older page exists.
	events, err := h.Queries.ListInitiativeEvents(r.Context(), db.ListInitiativeEventsParams{
		InitiativeID:    initiative.ID,
		BeforeCreatedAt: beforeCreatedAt,
		BeforeID:        beforeID,
		Limit:           limit + 1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list initiative events")
		return
	}

	hasMore := len(events) > int(limit)
	if hasMore {
		events = events[:limit]
	}
	var nextCursor *InitiativeEventsCursorResponse
	if hasMore && len(events) > 0 {
		oldest := events[len(events)-1]
		nextCursor = &InitiativeEventsCursorResponse{
			CreatedAt: oldest.CreatedAt.Time.Format(time.RFC3339Nano),
			ID:        uuidToString(oldest.ID),
		}
	}

	out := make([]InitiativeEventResponse, 0, len(events))
	for _, event := range events {
		out = append(out, initiativeEventToResponse(event))
	}
	writeJSON(w, http.StatusOK, ListInitiativeEventsResponse{Events: out, HasMore: hasMore, NextCursor: nextCursor})
}

type ListInitiativeBlockersResponse struct {
	Blockers []InitiativeBlockerResponse `json:"blockers"`
}

func (h *Handler) ListInitiativeBlockers(w http.ResponseWriter, r *http.Request) {
	if !h.requireInitiativesEnabled(w, r) {
		return
	}
	initiative, ok := h.loadInitiativeForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var status pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		status = strToText(s)
	}

	blockers, err := h.Queries.ListInitiativeBlockers(r.Context(), db.ListInitiativeBlockersParams{
		InitiativeID: initiative.ID,
		Status:       status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list initiative blockers")
		return
	}

	out := make([]InitiativeBlockerResponse, 0, len(blockers))
	for _, blocker := range blockers {
		out = append(out, initiativeBlockerToResponse(blocker))
	}
	writeJSON(w, http.StatusOK, ListInitiativeBlockersResponse{Blockers: out})
}
