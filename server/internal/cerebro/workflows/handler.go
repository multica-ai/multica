package workflows

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler hosts the REST surface for cerebro workflows. Wired into the
// router under /api/cerebro/workflows by the cerebro-workflows-routes
// CEREBRO-PATCH in server/cmd/server/router.go.
//
// All endpoints require an authenticated session (X-User-ID middleware) and
// scope every read/write through the workspace ID in context.
type Handler struct {
	Cerebro *cerebrodb.Queries
	// Service is an optional reference to the engine used by the test-only
	// /api/cerebro/workflows/_test/cron-sweep endpoint (phase-3, JEH-1108).
	// In production this is nil and the test endpoint returns 404. The e2e
	// suite gates the endpoint with CEREBRO_WORKFLOWS_TEST_ENDPOINTS=1.
	Service *Service
	// loopPlanningMaterializer is optional and nil-safe: see loop_planning.go.
	loopPlanningMaterializer LoopPlanningMaterializer
	// issueLoopCompiler is optional and nil-safe: see issue_loop.go.
	issueLoopCompiler IssueLoopCompiler
	// issueLoopColumns is optional and nil-safe: without it, workflow_type
	// always reads back as "standard" and an issue_loop create/update is
	// rejected (see requireIssueLoopColumns). Wired via
	// WithIssueLoopColumns from router.go.
	issueLoopColumns *IssueLoopColumnStore
	// loopCheckStore is optional and nil-safe: see issue_loop_state.go.
	loopCheckStore LoopCheckStore
	// planDocuments is optional and nil-safe: when wired, issue workflow
	// activation maintains the shared plan artifact under Agents > Workflow.
	planDocuments *PlanDocumentService
	// issueLookup is optional and nil-safe: without it, the per-issue
	// activation endpoints (FIR-2283 v2 point 8) are unavailable. Wired via
	// WithIssueLookup from router.go — reuses the IssueLookup interface
	// webhook_inbound.go already declares (same narrow need: confirm an
	// issue id is real and belongs to the caller's workspace before acting
	// on it; tenant isolation — SetGeneratedFromForIssue's FK alone would
	// catch "no such issue" but not "issue belongs to a different
	// workspace").
	issueLookup IssueLookup
}

// WithIssueLookup plugs in the upstream issue lookup for the per-issue
// activation endpoints. Returns the receiver for chainable init.
func (h *Handler) WithIssueLookup(l IssueLookup) *Handler {
	h.issueLookup = l
	return h
}

// WithIssueLoopColumns plugs in the raw-SQL store for the workflow_type /
// loop_spec / generated_from_workflow_id columns (see issue_loop_columns.go).
// Returns the receiver for chainable init.
func (h *Handler) WithIssueLoopColumns(s *IssueLoopColumnStore) *Handler {
	h.issueLoopColumns = s
	return h
}

func (h *Handler) WithPlanDocuments(s *PlanDocumentService) *Handler {
	h.planDocuments = s
	return h
}

func NewHandler(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

// WithService wires the engine reference used by the test-only cron-sweep
// hook. Callers in main.go (or test setup) invoke this after constructing
// the Service. Returns the receiver for chainable initialisation.
func (h *Handler) WithService(svc *Service) *Handler {
	h.Service = svc
	return h
}

const (
	defaultRunsLimit = 50
	maxRunsLimit     = 200
)

type workflowResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	ProjectID     string          `json:"project_id,omitempty"`
	Name          string          `json:"name"`
	Enabled       bool            `json:"enabled"`
	TriggerType   string          `json:"trigger_type"`
	TriggerConfig json.RawMessage `json:"trigger_config"`
	Conditions    json.RawMessage `json:"conditions"`
	ActionType    string          `json:"action_type"`
	ActionConfig  json.RawMessage `json:"action_config"`
	// WorkflowType is "standard" (default) or "issue_loop" (FIR-2283). LoopSpec
	// carries the recipe (goal, definition_of_done, verification[], caps,
	// planning) only for an issue_loop row; empty/omitted for standard rows.
	WorkflowType string          `json:"workflow_type"`
	LoopSpec     json.RawMessage `json:"loop_spec,omitempty"`
	// EditorMode / EditorLayout (phase 2): which builder opens this workflow
	// and the xyflow node-positions for canvas mode. Form-mode rows leave
	// EditorLayout null.
	EditorMode    string          `json:"editor_mode"`
	EditorLayout  json.RawMessage `json:"editor_layout,omitempty"`
	CreatedByID   string          `json:"created_by_id"`
	CreatedByType string          `json:"created_by_type"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`

	// Phase-3 webhook fields (JEH-1108, PR 2). The token is a *capability*
	// and is intentionally visible — the URL it forms is the integration
	// surface the user copies into the source system. The two secrets are
	// NEVER returned in plaintext after the regenerate endpoints — only the
	// presence-bool is exposed, so the UI can render "Secret set" /
	// "No secret" without learning the value.
	InboundWebhookToken      string `json:"inbound_webhook_token,omitempty"`
	InboundSigningSecretSet  bool   `json:"inbound_signing_secret_set"`
	OutboundWebhookSecretSet bool   `json:"outbound_webhook_secret_set"`
}

func toWorkflowResponse(row cerebrodb.CerebroWorkflow) workflowResponse {
	out := workflowResponse{
		ID:            util.UUIDToString(row.ID),
		WorkspaceID:   util.UUIDToString(row.WorkspaceID),
		Name:          row.Name,
		Enabled:       row.Enabled,
		TriggerType:   row.TriggerType,
		TriggerConfig: nonEmptyJSON(row.TriggerConfig),
		Conditions:    nonEmptyJSON(row.Conditions),
		ActionType:    row.ActionType,
		ActionConfig:  nonEmptyJSON(row.ActionConfig),
		WorkflowType:  WorkflowTypeStandard,
		EditorMode:    row.EditorMode,
		CreatedByID:   util.UUIDToString(row.CreatedByID),
		CreatedByType: row.CreatedByType,
		CreatedAt:     row.CreatedAt.Time.UTC().Format(rfc3339),
		UpdatedAt:     row.UpdatedAt.Time.UTC().Format(rfc3339),
	}
	if row.ProjectID.Valid {
		out.ProjectID = util.UUIDToString(row.ProjectID)
	}
	if len(row.EditorLayout) > 0 {
		out.EditorLayout = row.EditorLayout
	}
	// Mask-on-read for the phase-3 webhook secrets. Token IS visible —
	// it's the integration URL surface the user copies. Secrets are
	// surfaced only as presence-bools; the plaintext leaves the server
	// exactly once on the regenerate response.
	if row.InboundWebhookToken.Valid {
		out.InboundWebhookToken = row.InboundWebhookToken.String
	}
	out.InboundSigningSecretSet = row.InboundSigningSecret.Valid && row.InboundSigningSecret.String != ""
	out.OutboundWebhookSecretSet = row.OutboundWebhookSecret.Valid && row.OutboundWebhookSecret.String != ""
	return out
}

// withIssueLoopFields merges the workflow_type / loop_spec columns (read via
// IssueLoopColumnStore, outside the sqlc-generated row) into a response.
func withIssueLoopFields(resp workflowResponse, f IssueLoopFields) workflowResponse {
	if f.WorkflowType != "" {
		resp.WorkflowType = f.WorkflowType
	}
	resp.LoopSpec = f.LoopSpec
	return resp
}

type runResponse struct {
	ID            string `json:"id"`
	WorkflowID    string `json:"workflow_id"`
	WorkspaceID   string `json:"workspace_id"`
	TargetIssueID string `json:"target_issue_id,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	Status        string `json:"status"`
	Attempt       int32  `json:"attempt"`
	Error         string `json:"error,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
	NextRetryAt   string `json:"next_retry_at,omitempty"`
	CreatedAt     string `json:"created_at"`
}

func toRunResponse(row cerebrodb.CerebroWorkflowRun) runResponse {
	out := runResponse{
		ID:          util.UUIDToString(row.ID),
		WorkflowID:  util.UUIDToString(row.WorkflowID),
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		Status:      row.Status,
		Attempt:     row.Attempt,
		CreatedAt:   row.CreatedAt.Time.UTC().Format(rfc3339),
	}
	if row.TargetIssueID.Valid {
		out.TargetIssueID = util.UUIDToString(row.TargetIssueID)
	}
	if row.TaskID.Valid {
		out.TaskID = util.UUIDToString(row.TaskID)
	}
	if row.Error.Valid {
		out.Error = row.Error.String
	}
	if row.StartedAt.Valid {
		out.StartedAt = row.StartedAt.Time.UTC().Format(rfc3339)
	}
	if row.FinishedAt.Valid {
		out.FinishedAt = row.FinishedAt.Time.UTC().Format(rfc3339)
	}
	if row.NextRetryAt.Valid {
		out.NextRetryAt = row.NextRetryAt.Time.UTC().Format(rfc3339)
	}
	return out
}

type writeWorkflowRequest struct {
	Name          string          `json:"name"`
	Enabled       *bool           `json:"enabled,omitempty"`
	ProjectID     string          `json:"project_id,omitempty"`
	TriggerType   string          `json:"trigger_type"`
	TriggerConfig json.RawMessage `json:"trigger_config,omitempty"`
	Conditions    json.RawMessage `json:"conditions,omitempty"`
	ActionType    string          `json:"action_type"`
	ActionConfig  json.RawMessage `json:"action_config,omitempty"`
	// Phase-2 editor metadata. Optional; absent → defaults to "form" + null.
	EditorMode   string          `json:"editor_mode,omitempty"`
	EditorLayout json.RawMessage `json:"editor_layout,omitempty"`
	// WorkflowType (FIR-2283): "standard" (default, absent) or "issue_loop".
	// An issue_loop request supplies LoopSpec instead of trigger/action —
	// see validateWriteRequest and materializeIssueLoop. TriggerType /
	// ActionType are ignored for an issue_loop request; the handler pins
	// them to an inert placeholder (see issueLoopPlaceholderTrigger/Action)
	// since the compiled child rules are what actually run.
	WorkflowType string          `json:"workflow_type,omitempty"`
	LoopSpec     json.RawMessage `json:"loop_spec,omitempty"`
}

func (r writeWorkflowRequest) isIssueLoop() bool {
	return r.WorkflowType == WorkflowTypeIssueLoop
}

// validateWriteRequest enforces the shape we let through to the DB before we
// hit the CHECK constraints. Friendlier than a raw constraint violation in
// the UI, and keeps the API surface stable when the constraint list grows.
//
// Phase-3 additions:
//   - cron triggers parse the schedule_expr with robfig/cron/v3 so a typo
//     fails fast (400) instead of silently never firing.
//   - comment_mention with MatchMode=regex compiles the regex so a bad
//     pattern surfaces before runtime.
//   - webhook_outbound action_config goes through the SSRF guard's
//     URL + header validators (https-only unless localhost-bypass env
//     flag, header-name whitelist + forbidden-name set).
func validateWriteRequest(req writeWorkflowRequest) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.isIssueLoop() {
		// An issue_loop row's trigger/action are the inert placeholder the
		// handler pins server-side (issueLoopPlaceholderTrigger/Action) — the
		// compiled child rules are what actually run. Validate the recipe
		// itself instead of the (ignored) trigger/action the client sent.
		if len(req.LoopSpec) == 0 {
			return errors.New("loop_spec is required for an issue_loop workflow")
		}
		var probe struct {
			Goal string `json:"goal"`
		}
		if err := json.Unmarshal(req.LoopSpec, &probe); err != nil {
			return fmt.Errorf("loop_spec: %w", err)
		}
		return nil
	}
	if !knownTrigger(req.TriggerType) {
		return errors.New("unknown trigger_type")
	}
	if !knownAction(req.ActionType) {
		return errors.New("unknown action_type")
	}
	if req.EditorMode != "" && !knownEditorMode(req.EditorMode) {
		return errors.New("unknown editor_mode")
	}

	// Trigger-config validation.
	switch req.TriggerType {
	case TriggerCron:
		var cfg TriggerConfigCron
		if len(req.TriggerConfig) > 0 {
			if err := json.Unmarshal(req.TriggerConfig, &cfg); err != nil {
				return fmt.Errorf("trigger_config: %w", err)
			}
		}
		if err := validateCronSchedule(cfg.ScheduleExpr, cfg.Timezone); err != nil {
			return err
		}
	case TriggerCommentMention:
		var cfg TriggerConfigCommentMention
		if len(req.TriggerConfig) > 0 {
			if err := json.Unmarshal(req.TriggerConfig, &cfg); err != nil {
				return fmt.Errorf("trigger_config: %w", err)
			}
		}
		if cfg.MatchMode == CommentMatchRegex {
			if cfg.Target == "" {
				return errors.New("comment_mention regex requires target")
			}
			if _, err := regexp.Compile(cfg.Target); err != nil {
				return fmt.Errorf("comment_mention regex: %w", err)
			}
		}
	}

	// Action-config validation.
	switch req.ActionType {
	case ActionWebhookOutbound:
		var cfg ActionConfigWebhookOutbound
		if len(req.ActionConfig) > 0 {
			if err := json.Unmarshal(req.ActionConfig, &cfg); err != nil {
				return fmt.Errorf("action_config: %w", err)
			}
		}
		if cfg.URL == "" {
			return errors.New("webhook_outbound: url is required")
		}
		if err := validateOutboundURL(cfg.URL, envAllowLocalhost()); err != nil {
			return err
		}
		if err := validateOutboundHeaders(cfg.Headers); err != nil {
			return err
		}
	}
	return nil
}

func knownTrigger(t string) bool {
	switch t {
	case TriggerStatusChanged, TriggerDueDateReached, TriggerDueTimeReached,
		TriggerCron, TriggerWebhookInbound, TriggerCommentMention,
		TriggerAllChildrenDone, TriggerSubIssueCreated:
		return true
	}
	return false
}

func knownAction(a string) bool {
	switch a {
	case ActionSetStatus, ActionCreateSubIssue, ActionSendReminder,
		ActionRunSkill, ActionCommentOnIssue,
		ActionRouteByDomain, ActionEscalateToOwner,
		ActionWebhookOutbound, ActionReassignIssue:
		return true
	}
	return false
}

func knownEditorMode(m string) bool {
	switch m {
	case EditorModeForm, EditorModeCanvas:
		return true
	}
	return false
}

// issueLoopPlaceholderRule returns the inert trigger/action pair persisted on
// an issue_loop recipe's own cerebro_workflow row. The recipe itself never
// fires on its own — IssueLoopCompiler compiles it into real dispatch/gate/
// escalate child rules (see issue_loop.go) that do the actual work — so the
// parent row's trigger/action only need to satisfy the existing NOT
// NULL/CHECK constraints on cerebro_workflow without ever running. Reusing
// already-known enum values (rather than adding a new one) means no CHECK
// constraint migration is needed: webhook_inbound only fires if something
// POSTs to this row's own (never-shared, never-published) webhook token, and
// comment_on_issue with empty content is a harmless no-op even in that case.
func issueLoopPlaceholderRule() (triggerType string, triggerConfig json.RawMessage, actionType string, actionConfig json.RawMessage) {
	return TriggerWebhookInbound, json.RawMessage("{}"), ActionCommentOnIssue, json.RawMessage(`{"target":"self","content":""}`)
}

// List handles GET /api/cerebro/workflows.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroWorkflows(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	var issueLoopFields map[string]IssueLoopFields
	if h.issueLoopColumns != nil {
		issueLoopFields, err = h.issueLoopColumns.GetMany(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list workflows")
			return
		}
	}
	out := make([]workflowResponse, 0, len(rows))
	for _, row := range rows {
		f, ok := issueLoopFields[util.UUIDToString(row.ID)]
		if ok && f.GeneratedFromWorkflowID != "" {
			// Hide the dispatch/gate/escalate rules an issue_loop recipe
			// compiled — they are the bridge's output, not something a
			// person edits directly (see issue_loop_columns.go).
			continue
		}
		resp := toWorkflowResponse(row)
		if ok {
			resp = withIssueLoopFields(resp, f)
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": out})
}

// Get handles GET /api/cerebro/workflows/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	row, err := h.Cerebro.GetCerebroWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !inWorkspace(r, row.WorkspaceID) {
		// Workspace mismatch — surface as 404 to avoid leaking row existence
		// across workspace boundaries.
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	resp := toWorkflowResponse(row)
	if h.issueLoopColumns != nil {
		if f, err := h.issueLoopColumns.Get(r.Context(), id); err == nil {
			resp = withIssueLoopFields(resp, f)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// Create handles POST /api/cerebro/workflows.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}

	var req writeWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateWriteRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	creatorUUID, err := util.ParseUUID(actorID(r, userID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid actor id")
		return
	}

	var projectID pgtype.UUID
	if req.ProjectID != "" {
		parsed, err := util.ParseUUID(req.ProjectID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid project_id")
			return
		}
		projectID = parsed
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	editorMode := req.EditorMode
	if editorMode == "" {
		editorMode = EditorModeForm
	}
	triggerType, triggerConfig, actionType, actionConfig := req.TriggerType, req.TriggerConfig, req.ActionType, req.ActionConfig
	if req.isIssueLoop() {
		triggerType, triggerConfig, actionType, actionConfig = issueLoopPlaceholderRule()
	}
	row, err := h.Cerebro.CreateCerebroWorkflow(r.Context(), cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   wsUUID,
		ProjectID:     projectID,
		Name:          req.Name,
		Enabled:       enabled,
		TriggerType:   triggerType,
		TriggerConfig: defaultJSON(triggerConfig, "{}"),
		Conditions:    defaultJSON(req.Conditions, "[]"),
		ActionType:    actionType,
		ActionConfig:  defaultJSON(actionConfig, "{}"),
		EditorMode:    editorMode,
		EditorLayout:  []byte(req.EditorLayout),
		CreatedByID:   creatorUUID,
		CreatedByType: actorType(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}
	h.materializeLoopPlanning(r.Context(), wsUUID, creatorUUID, actorType(r), req)

	if req.isIssueLoop() {
		resp, syncErr := h.materializeIssueLoop(r.Context(), wsUUID, row.ID, projectID, creatorUUID, actorType(r), req.LoopSpec)
		if syncErr != nil {
			// A bad recipe must not leave a phantom workflow behind — delete
			// the row we just created and surface the validation error.
			_ = h.Cerebro.DeleteCerebroWorkflow(r.Context(), row.ID)
			writeError(w, http.StatusBadRequest, syncErr.Error())
			return
		}
		writeJSON(w, http.StatusCreated, resp)
		return
	}
	writeJSON(w, http.StatusCreated, toWorkflowResponse(row))
}

// materializeIssueLoop persists the issue_loop columns on workflowID and, if
// an IssueLoopCompiler is wired, compiles the recipe onto the engine. Returns
// the response DTO to send back on success, or an error the caller surfaces
// as a 400 (an issue_loop recipe that fails to compile must not look saved
// and working — the compiler's own validation, loops.Spec.Validate, is the
// source of truth for whether a recipe is runnable).
func (h *Handler) materializeIssueLoop(ctx context.Context, wsUUID, workflowID, projectID, creatorUUID pgtype.UUID, createdByType string, loopSpecJSON json.RawMessage) (workflowResponse, error) {
	if h.issueLoopColumns == nil {
		return workflowResponse{}, errors.New("issue workflow support is not wired on this server")
	}
	if err := h.issueLoopColumns.Set(ctx, workflowID, WorkflowTypeIssueLoop, loopSpecJSON); err != nil {
		return workflowResponse{}, err
	}
	if h.issueLoopCompiler != nil {
		if err := h.issueLoopCompiler.SyncIssueLoop(ctx, wsUUID, workflowID, projectID, creatorUUID, createdByType, loopSpecJSON); err != nil {
			return workflowResponse{}, err
		}
	}
	row, err := h.Cerebro.GetCerebroWorkflow(ctx, workflowID)
	if err != nil {
		return workflowResponse{}, err
	}
	fields, err := h.issueLoopColumns.Get(ctx, workflowID)
	if err != nil {
		return workflowResponse{}, err
	}
	return withIssueLoopFields(toWorkflowResponse(row), fields), nil
}

// materializeLoopPlanning creates the companion loop:planning-dispatch
// workflow row (see loop_planning.go) when the just-created workflow is a
// run_skill action with loop_planning=true in its action_config. Best-effort:
// a failure here is logged, not surfaced as a 500, so a transient DB error on
// the companion row never blocks creation of the workflow the user asked for.
func (h *Handler) materializeLoopPlanning(ctx context.Context, wsUUID, creatorUUID pgtype.UUID, createdByType string, req writeWorkflowRequest) {
	if h.loopPlanningMaterializer == nil || req.ActionType != ActionRunSkill {
		return
	}
	var cfg ActionConfigRunSkill
	if len(req.ActionConfig) > 0 {
		if err := json.Unmarshal(req.ActionConfig, &cfg); err != nil {
			return
		}
	}
	if !cfg.LoopPlanning {
		return
	}
	if err := h.loopPlanningMaterializer.CreatePlanningDispatch(ctx, wsUUID, creatorUUID, createdByType, cfg.AgentID, cfg.SkillName); err != nil {
		slog.Error("workflow create: loop planning-dispatch materialization failed",
			"agent_id", cfg.AgentID,
			"error", err,
		)
	}
}

// Update handles PUT /api/cerebro/workflows/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	var req writeWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateWriteRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	var projectID pgtype.UUID
	if req.ProjectID != "" {
		parsed, err := util.ParseUUID(req.ProjectID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid project_id")
			return
		}
		projectID = parsed
	}

	editorMode := req.EditorMode
	if editorMode == "" {
		editorMode = existing.EditorMode
	}
	if editorMode == "" {
		editorMode = EditorModeForm
	}
	triggerType, triggerConfig, actionType, actionConfig := req.TriggerType, req.TriggerConfig, req.ActionType, req.ActionConfig
	if req.isIssueLoop() {
		triggerType, triggerConfig, actionType, actionConfig = issueLoopPlaceholderRule()
	}
	row, err := h.Cerebro.UpdateCerebroWorkflow(r.Context(), cerebrodb.UpdateCerebroWorkflowParams{
		ID:            id,
		Name:          req.Name,
		Enabled:       enabled,
		ProjectID:     projectID,
		TriggerType:   triggerType,
		TriggerConfig: defaultJSON(triggerConfig, "{}"),
		Conditions:    defaultJSON(req.Conditions, "[]"),
		ActionType:    actionType,
		ActionConfig:  defaultJSON(actionConfig, "{}"),
		EditorMode:    editorMode,
		EditorLayout:  []byte(req.EditorLayout),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow")
		return
	}

	if req.isIssueLoop() {
		actorUUID, actorErr := util.ParseUUID(actorID(r, userID))
		if actorErr != nil {
			writeError(w, http.StatusBadRequest, "invalid actor id")
			return
		}
		resp, syncErr := h.materializeIssueLoop(r.Context(), existing.WorkspaceID, row.ID, projectID, actorUUID, actorType(r), req.LoopSpec)
		if syncErr != nil {
			writeError(w, http.StatusBadRequest, syncErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	writeJSON(w, http.StatusOK, toWorkflowResponse(row))
}

// Toggle handles POST /api/cerebro/workflows/{id}/toggle. Body: { enabled: bool }.
func (h *Handler) Toggle(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.Cerebro.SetCerebroWorkflowEnabled(r.Context(), cerebrodb.SetCerebroWorkflowEnabledParams{
		ID:      id,
		Enabled: req.Enabled,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to toggle workflow")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

// Delete handles DELETE /api/cerebro/workflows/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if err := h.Cerebro.DeleteCerebroWorkflow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RegenerateInboundToken handles POST /api/cerebro/workflows/{id}/regenerate-token.
// Generates a fresh 32-byte URL-safe-base64 token and returns the full
// inbound webhook URL so the UI can show "copy this to source system". The
// returned token is the only time the value crosses the network in
// plaintext after generation — subsequent GETs return the (visible) token
// alongside the row.
func (h *Handler) RegenerateInboundToken(w http.ResponseWriter, r *http.Request) {
	row, ok := h.loadWorkflowForWrite(w, r)
	if !ok {
		return
	}
	token, err := generateOpaqueToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	if err := h.Cerebro.UpdateCerebroWorkflowInboundToken(r.Context(), cerebrodb.UpdateCerebroWorkflowInboundTokenParams{
		ID:                  row.ID,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"inbound_webhook_token": token,
		"inbound_webhook_url":   publicWebhookURL(r, token),
	})
}

// RegenerateInboundSigningSecret handles
// POST /api/cerebro/workflows/{id}/regenerate-signing-secret.
// Generates a 32-byte secret returned exactly once. Subsequent GETs mask
// the value via the inbound_signing_secret_set boolean.
func (h *Handler) RegenerateInboundSigningSecret(w http.ResponseWriter, r *http.Request) {
	row, ok := h.loadWorkflowForWrite(w, r)
	if !ok {
		return
	}
	secret, err := generateOpaqueToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}
	if err := h.Cerebro.UpdateCerebroWorkflowInboundSigningSecret(r.Context(), cerebrodb.UpdateCerebroWorkflowInboundSigningSecretParams{
		ID:                   row.ID,
		InboundSigningSecret: pgtype.Text{String: secret, Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"inbound_signing_secret": secret,
	})
}

// RegenerateOutboundSecret handles
// POST /api/cerebro/workflows/{id}/regenerate-outbound-secret.
// Same mask-on-read shape as the inbound signing secret.
func (h *Handler) RegenerateOutboundSecret(w http.ResponseWriter, r *http.Request) {
	row, ok := h.loadWorkflowForWrite(w, r)
	if !ok {
		return
	}
	secret, err := generateOpaqueToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}
	if err := h.Cerebro.UpdateCerebroWorkflowOutboundSecret(r.Context(), cerebrodb.UpdateCerebroWorkflowOutboundSecretParams{
		ID:                    row.ID,
		OutboundWebhookSecret: pgtype.Text{String: secret, Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"outbound_webhook_secret": secret,
	})
}

// TestSweepCron is a build-time-stable but env-gated debug endpoint that
// fires one synchronous iteration of the cron sweeper. Used by the phase-3
// e2e tests so they don't have to wait for the 60-second ticker. Returns 404
// unless CEREBRO_WORKFLOWS_TEST_ENDPOINTS=1 is set on the backend — in any
// other configuration the endpoint behaves as if it doesn't exist.
//
// We gate on env rather than a Go build tag because Playwright drives the
// same binary CI uses for unit tests, and a separate build target would
// double the CI matrix without buying isolation we don't have anyway (the
// endpoint is harmless in production — same Service.sweepCron call the
// in-process ticker makes).
func (h *Handler) TestSweepCron(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("CEREBRO_WORKFLOWS_TEST_ENDPOINTS") != "1" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if h.Service == nil {
		writeError(w, http.StatusServiceUnavailable, "engine not wired")
		return
	}
	if err := h.Service.SweepCronOnce(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("sweep failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// loadWorkflowForWrite is the auth + workspace-scope check shared by the
// three regenerate endpoints (and conceptually by Update/Toggle/Delete,
// though those still inline it for readability). Returns false after
// writing the error response.
func (h *Handler) loadWorkflowForWrite(w http.ResponseWriter, r *http.Request) (cerebrodb.CerebroWorkflow, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return cerebrodb.CerebroWorkflow{}, false
	}
	id, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return cerebrodb.CerebroWorkflow{}, false
	}
	row, err := h.Cerebro.GetCerebroWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return cerebrodb.CerebroWorkflow{}, false
	}
	if !inWorkspace(r, row.WorkspaceID) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return cerebrodb.CerebroWorkflow{}, false
	}
	return row, true
}

// generateOpaqueToken mints n random bytes and returns the URL-safe-base64
// (no padding) encoding. 32 random bytes → roughly 43 chars. crypto/rand
// failure is treated as a server error by the caller.
func generateOpaqueToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// publicWebhookURL composes the full ingress URL the UI should display
// next to "Copy this to your source system". Mirrors handler/runtime_setup.go's
// publicServerURL so both the daemon-install URL and the webhook URL agree
// on host detection (MULTICA_APP_URL / FRONTEND_ORIGIN env override,
// otherwise X-Forwarded-Proto + r.Host).
func publicWebhookURL(r *http.Request, token string) string {
	return publicServerOrigin(r) + "/api/cerebro/workflows/webhook/" + token
}

// Runs handles GET /api/cerebro/workflows/{id}/runs (per-workflow log) and
// GET /api/cerebro/workflows/runs (workspace-wide log when no id is set).
// Pagination is offset/limit, capped at maxRunsLimit.
func (h *Handler) Runs(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	limit, offset := parseLimitOffset(r, defaultRunsLimit, maxRunsLimit)

	idStr := chi.URLParam(r, "id")
	ctx := r.Context()
	var rows []cerebrodb.CerebroWorkflowRun
	if idStr != "" {
		wfID, err := util.ParseUUID(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		existing, err := h.Cerebro.GetCerebroWorkflow(ctx, wfID)
		if err != nil || !inWorkspace(r, existing.WorkspaceID) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		rows, err = h.Cerebro.ListCerebroWorkflowRuns(ctx, cerebrodb.ListCerebroWorkflowRunsParams{
			WorkflowID: wfID,
			Limit:      int32(limit),
			Offset:     int32(offset),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list runs")
			return
		}
	} else {
		var err error
		rows, err = h.Cerebro.ListWorkspaceCerebroWorkflowRuns(ctx, cerebrodb.ListWorkspaceCerebroWorkflowRunsParams{
			WorkspaceID: wsUUID,
			Limit:       int32(limit),
			Offset:      int32(offset),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list runs")
			return
		}
	}

	out := make([]runResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRunResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"runs":   out,
		"limit":  limit,
		"offset": offset,
	})
}

// issueRunResponse is one row of the point-7 issue log: an issue that has run
// through a recipe's loop, with its current status and when it first/last
// entered the loop.
type issueRunResponse struct {
	IssueID          string `json:"issue_id"`
	IssueNumber      int32  `json:"issue_number"`
	IssueTitle       string `json:"issue_title"`
	IssueStatus      string `json:"issue_status"`
	FirstActivatedAt string `json:"first_activated_at"`
	LastActivatedAt  string `json:"last_activated_at"`
}

// LoopRuns handles GET /api/cerebro/workflows/{id}/loop-runs — FIR-2283 v2
// point 7: the list of issues that have run through this issue_loop recipe.
// Unlike Runs (which reads cerebro_workflow_run, empty for a recipe because a
// recipe compiles into child rules that produce the runs), this derives the
// issue log from the generated child rows' generated_for_issue_id.
func (h *Handler) LoopRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.issueLoopColumns == nil {
		writeError(w, http.StatusServiceUnavailable, "issue workflow activation is not wired on this server")
		return
	}
	wfID, ok := pathUUIDOr400(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroWorkflow(r.Context(), wfID)
	if err != nil || !inWorkspace(r, existing.WorkspaceID) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	runs, err := h.issueLoopColumns.ListIssueRunsForWorkflow(r.Context(), wfID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issue runs")
		return
	}

	out := make([]issueRunResponse, 0, len(runs))
	for _, run := range runs {
		out = append(out, issueRunResponse{
			IssueID:          run.IssueID,
			IssueNumber:      run.IssueNumber,
			IssueTitle:       run.IssueTitle,
			IssueStatus:      run.IssueStatus,
			FirstActivatedAt: run.FirstActivatedAt.Format(time.RFC3339),
			LastActivatedAt:  run.LastActivatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"issue_runs": out})
}

// loopStateResponse is the control strip's live-state read for an issue_loop
// recipe against one issue: round k/N-shaped data (round + the caps that
// bound it, read off the recipe's own loop_spec), whether the gate is
// stopped, and any human checks currently awaiting a decision.
type loopStateResponse struct {
	Round              int32               `json:"round"`
	MaxIterations      int                 `json:"max_iterations,omitempty"`
	Stopped            bool                `json:"stopped"`
	StopReason         string              `json:"stop_reason,omitempty"`
	PendingHumanChecks []PendingHumanCheck `json:"pending_human_checks"`
}

// LoopState handles GET /api/cerebro/workflows/{id}/loop-state?issue_id=<uuid>.
// {id} is the issue_loop recipe; issue_id is the specific issue being run
// through it (the recipe itself has no single "the" issue — a person picks
// one to watch, typically the test issue they just ran it on).
func (h *Handler) LoopState(w http.ResponseWriter, r *http.Request) {
	row, ok := h.loadWorkflowForWrite(w, r)
	if !ok {
		return
	}
	if h.issueLoopColumns == nil || h.loopCheckStore == nil {
		writeError(w, http.StatusServiceUnavailable, "issue workflow live state is not wired on this server")
		return
	}
	issueIDStr := r.URL.Query().Get("issue_id")
	if issueIDStr == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}

	gateID, err := h.issueLoopColumns.GeneratedChildIDByName(r.Context(), row.ID, "loop:delivery-gate")
	if err != nil {
		// Not compiled yet (e.g. a brand-new recipe that failed to sync) —
		// report the zero state rather than an error; there is nothing to
		// watch yet.
		writeJSON(w, http.StatusOK, loopStateResponse{PendingHumanChecks: []PendingHumanCheck{}})
		return
	}
	gate := util.UUIDToString(gateID)

	state, err := h.loopCheckStore.GateState(r.Context(), issueIDStr, gate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load loop state")
		return
	}
	pending, err := h.loopCheckStore.PendingHumanChecks(r.Context(), issueIDStr, gate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load pending human checks")
		return
	}
	if pending == nil {
		pending = []PendingHumanCheck{}
	}

	maxIterations := 0
	if h.issueLoopColumns != nil {
		if fields, err := h.issueLoopColumns.Get(r.Context(), row.ID); err == nil && len(fields.LoopSpec) > 0 {
			var spec struct {
				Caps struct {
					MaxIterations int `json:"max_iterations"`
				} `json:"caps"`
			}
			if json.Unmarshal(fields.LoopSpec, &spec) == nil {
				maxIterations = spec.Caps.MaxIterations
			}
		}
	}

	writeJSON(w, http.StatusOK, loopStateResponse{
		Round:              state.Round,
		MaxIterations:      maxIterations,
		Stopped:            state.Stopped,
		StopReason:         state.StopReason,
		PendingHumanChecks: pending,
	})
}

// ApproveHumanCheck handles
// POST /api/cerebro/workflows/{id}/human-checks/{checkId}/approve.
// Body: { issue_id, approved, note }. {id} is the issue_loop recipe;
// checkId is the verification id from the recipe. Records the
// authenticated caller's decision and lets the gate re-evaluate on its next
// natural pass (the same reconciliation loop a reported check/judge verdict
// feeds).
func (h *Handler) ApproveHumanCheck(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	row, ok := h.loadWorkflowForWrite(w, r)
	if !ok {
		return
	}
	if h.issueLoopColumns == nil || h.loopCheckStore == nil {
		writeError(w, http.StatusServiceUnavailable, "issue workflow approvals are not wired on this server")
		return
	}
	checkID := chi.URLParam(r, "checkId")
	if checkID == "" {
		writeError(w, http.StatusBadRequest, "checkId is required")
		return
	}

	var req struct {
		IssueID  string `json:"issue_id"`
		Approved bool   `json:"approved"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.IssueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	if !req.Approved && req.Note == "" {
		writeError(w, http.StatusBadRequest, "note is required when approved is false")
		return
	}

	gateID, err := h.issueLoopColumns.GeneratedChildIDByName(r.Context(), row.ID, "loop:delivery-gate")
	if err != nil {
		writeError(w, http.StatusNotFound, "this recipe has not been compiled onto the engine yet")
		return
	}

	if err := h.loopCheckStore.ApproveHumanCheck(r.Context(), req.IssueID, util.UUIDToString(gateID), checkID, req.Approved, req.Note, actorID(r, userID), actorType(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record decision")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"recorded": true})
}

// ActivateForIssue compiles the given issue_loop recipe scoped to a single
// issue. It is the reusable core of ActivateWorkflow (HTTP): other entry
// points — issue-create carrying workflow_id, the quick-create completion
// hook — call it so an issue and its workflow can be wired in one flow
// without duplicating the recipe-load + validation. The caller must have
// already confirmed workspaceID owns both the workflow and the issue
// (CreateIssue does this because it just minted the issue in workspaceID and
// resolved the workflow id from the same request; the HTTP handler confirms
// the issue via issueLookup before calling in).
//
// CEREBRO-PATCH(cerebro-workflows-activate-for-issue): FIR-2283 followup —
// shared activation used by the CLI/API create-with-workflow path.
func (h *Handler) ActivateForIssue(ctx context.Context, workspaceID, workflowID, issueID, creatorID, requesterID pgtype.UUID, createdByType string) error {
	if h.issueLoopColumns == nil || h.issueLoopCompiler == nil {
		return fmt.Errorf("issue workflow activation is not wired on this server")
	}
	row, err := h.Cerebro.GetCerebroWorkflow(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("workflow not found")
	}
	if row.WorkspaceID != workspaceID {
		return fmt.Errorf("workflow not found in this workspace")
	}
	fields, err := h.issueLoopColumns.Get(ctx, row.ID)
	if err != nil {
		return fmt.Errorf("failed to load recipe")
	}
	if fields.WorkflowType != WorkflowTypeIssueLoop {
		return fmt.Errorf("only an Issue workflow recipe can be activated on an issue")
	}
	if len(fields.LoopSpec) == 0 {
		return fmt.Errorf("this recipe has no loop_spec to compile")
	}
	if h.planDocuments != nil {
		if _, err := h.planDocuments.Ensure(ctx, PlanDocumentEnsureParams{
			WorkspaceID:   workspaceID,
			IssueID:       issueID,
			WorkflowID:    row.ID,
			WorkflowName:  row.Name,
			AuthorID:      creatorID,
			AuthorType:    createdByType,
			RequesterID:   requesterID,
			InitialStatus: "Workflow activated. Plan phase is starting.",
		}); err != nil {
			return fmt.Errorf("create workflow plan document: %w", err)
		}
	}
	if err := h.issueLoopCompiler.ActivateOnIssue(ctx, workspaceID, row.ID, row.ProjectID, creatorID, issueID, createdByType, fields.LoopSpec); err != nil {
		return err
	}
	// FIR-2283 followup point 1 — activation only MATERIALIZES the loop's
	// rules; it does not START them. The first phase (plan, or build when the
	// recipe has no planning phase) is dispatched by a StatusChanged rule, but
	// an issue born directly on a workflow never changes status, so that rule
	// never fires and the agent never receives the plan-mode prompt. Synthesize
	// the entry StatusChanged event here so the just-materialized first-phase
	// rule fires exactly as it would on a real board move.
	h.kickoffFirstPhase(ctx, workspaceID, row.ProjectID, issueID, fields.LoopSpec)
	return nil
}

// loopEntryFields is the minimal projection of a loop_spec the activator needs
// to decide which status enters the first phase: the planning toggle and the
// two entry statuses (planning vs build). The keys match issueLoopSpecWire /
// loops.Spec so this stays in sync with what Compile reads.
type loopEntryFields struct {
	Planning       bool   `json:"planning"`
	PlanningStatus string `json:"planning_status"`
	BuildStatus    string `json:"build_status"`
}

// kickoffFirstPhase dispatches the synthetic entry StatusChanged event that
// starts a freshly-activated issue loop. The event's ToStatus is the loop's
// entry status (planning status when the recipe plans, else build status), so
// the loop:planning-dispatch (plan-mode) or loop:dispatch-build rule that
// Compile just materialized for this issue fires. The issue-scope condition on
// those rules matches on Raw["issue"]["id"], so that is the one field the
// synthetic Raw payload must carry. Best-effort: a nil engine (unit wiring
// without a Service) or a dispatch error is logged, never surfaced — the loop's
// rules are already persisted and a later real status move still drives them.
func (h *Handler) kickoffFirstPhase(ctx context.Context, workspaceID, projectID, issueID pgtype.UUID, loopSpecJSON []byte) {
	if h.Service == nil {
		return
	}
	var entry loopEntryFields
	if err := json.Unmarshal(loopSpecJSON, &entry); err != nil {
		slog.Warn("issue loop kickoff: parse loop_spec failed",
			"issue_id", util.UUIDToString(issueID), "error", err)
		return
	}
	toStatus := entry.BuildStatus
	if entry.Planning {
		toStatus = entry.PlanningStatus
		if toStatus == "" {
			toStatus = "todo"
		}
	} else if toStatus == "" {
		toStatus = "in_progress"
	}
	issueIDStr := util.UUIDToString(issueID)
	te := TriggerEvent{
		// Stable per issue: a re-activation replaces the rule rows (new row
		// ids), so the idempotency key — keyed on (EventID, rule row id) — is
		// still fresh and the plan re-dispatches after an intentional re-sync.
		EventID:     "loop_activate:" + issueIDStr,
		WorkspaceID: util.UUIDToString(workspaceID),
		ProjectID:   util.UUIDToString(projectID),
		IssueID:     issueIDStr,
		Type:        TriggerStatusChanged,
		ToStatus:    toStatus,
		Raw: map[string]any{
			"issue": map[string]any{"id": issueIDStr},
		},
	}
	if err := h.Service.Dispatch(ctx, te); err != nil {
		slog.Warn("issue loop kickoff: dispatch failed",
			"issue_id", issueIDStr, "to_status", toStatus, "error", err)
	}
}

// ActivateWorkflow handles POST /api/cerebro/workflows/{id}/activate.
// Body: { issue_id }. {id} must be an issue_loop recipe. Compiles it scoped
// to issue_id alone (FIR-2283 v2 point 8 — "per-issue workflow activation")
// — the recipe stays a reusable template; this creates/replaces just that
// one issue's independent set of compiled rules, leaving the project-wide
// compile and every other issue's activation untouched.
func (h *Handler) ActivateWorkflow(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	row, ok := h.loadWorkflowForWrite(w, r)
	if !ok {
		return
	}
	if h.issueLoopColumns == nil || h.issueLoopCompiler == nil {
		writeError(w, http.StatusServiceUnavailable, "issue workflow activation is not wired on this server")
		return
	}
	if h.issueLookup == nil {
		writeError(w, http.StatusServiceUnavailable, "issue lookup is not wired on this server")
		return
	}

	var req struct {
		IssueID string `json:"issue_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	issueID, err := util.ParseUUID(req.IssueID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "issue_id is required and must be a valid id")
		return
	}

	issue, err := h.issueLookup.GetIssue(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if !inWorkspace(r, issue.WorkspaceID) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	wsUUID, ok := workspaceUUIDOr400(w, r)
	if !ok {
		return
	}
	creatorID, err := util.ParseUUID(actorID(r, userID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid actor id")
		return
	}
	// ActivateForIssue re-loads and validates the recipe (issue_loop + has a
	// loop_spec); the earlier load here was redundant, so it is gone.
	requesterID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid requester id")
		return
	}
	if err := h.ActivateForIssue(r.Context(), wsUUID, row.ID, issueID, creatorID, requesterID, actorType(r)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activated": true, "workflow_id": util.UUIDToString(row.ID), "issue_id": req.IssueID})
}

// ActiveWorkflowForIssue handles GET /api/cerebro/workflows/for-issue/{issueId}.
// Returns which Issue workflow recipe (if any) issueId is currently running
// — the binding is derived from the compiled rows themselves (see
// IssueLoopColumnStore.ActiveWorkflowForIssue), not a separate table.
func (h *Handler) ActiveWorkflowForIssue(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.issueLoopColumns == nil {
		writeError(w, http.StatusServiceUnavailable, "issue workflow activation is not wired on this server")
		return
	}
	issueID, ok := pathUUIDOr400(w, r, "issueId")
	if !ok {
		return
	}
	if h.issueLookup != nil {
		issue, err := h.issueLookup.GetIssue(r.Context(), issueID)
		if err != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		if !inWorkspace(r, issue.WorkspaceID) {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
	}

	workflowID, active, err := h.issueLoopColumns.ActiveWorkflowForIssue(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load active workflow")
		return
	}
	if !active {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": true, "workflow_id": util.UUIDToString(workflowID)})
}

// --- helpers ---

const rfc3339 = "2006-01-02T15:04:05Z07:00"

func workspaceUUIDOr400(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	return wsUUID, true
}

func pathUUIDOr400(w http.ResponseWriter, r *http.Request, key string) (pgtype.UUID, bool) {
	raw := chi.URLParam(r, key)
	id, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+key)
		return pgtype.UUID{}, false
	}
	return id, true
}

func inWorkspace(r *http.Request, wsID pgtype.UUID) bool {
	got := middleware.WorkspaceIDFromContext(r.Context())
	return util.UUIDToString(wsID) == got
}

func parseLimitOffset(r *http.Request, defLimit, maxLimit int) (int, int) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = defLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func actorID(r *http.Request, userID string) string {
	if v := r.Header.Get("X-Agent-ID"); v != "" {
		return v
	}
	return userID
}

func actorType(r *http.Request) string {
	if r.Header.Get("X-Agent-ID") != "" {
		return "agent"
	}
	return "member"
}

func defaultJSON(raw json.RawMessage, fallback string) []byte {
	if len(raw) == 0 {
		return []byte(fallback)
	}
	return raw
}

func nonEmptyJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// publicServerOrigin mirrors handler.publicServerURL — MULTICA_APP_URL /
// FRONTEND_ORIGIN env override, otherwise X-Forwarded-Proto + r.Host. Kept
// local to the workflows package so the cerebro zone doesn't have to depend
// on the upstream handler package for one helper.
func publicServerOrigin(r *http.Request) string {
	if v := strings.TrimSpace(os.Getenv("MULTICA_APP_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")); v != "" {
		return strings.TrimRight(v, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}
