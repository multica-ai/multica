package workflows

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

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
		ActionRouteByDomain,
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
	out := make([]workflowResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toWorkflowResponse(row))
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
	writeJSON(w, http.StatusOK, toWorkflowResponse(row))
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
	row, err := h.Cerebro.CreateCerebroWorkflow(r.Context(), cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   wsUUID,
		ProjectID:     projectID,
		Name:          req.Name,
		Enabled:       enabled,
		TriggerType:   req.TriggerType,
		TriggerConfig: defaultJSON(req.TriggerConfig, "{}"),
		Conditions:    defaultJSON(req.Conditions, "[]"),
		ActionType:    req.ActionType,
		ActionConfig:  defaultJSON(req.ActionConfig, "{}"),
		EditorMode:    editorMode,
		EditorLayout:  []byte(req.EditorLayout),
		CreatedByID:   creatorUUID,
		CreatedByType: actorType(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}
	writeJSON(w, http.StatusCreated, toWorkflowResponse(row))
}

// Update handles PUT /api/cerebro/workflows/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
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
	row, err := h.Cerebro.UpdateCerebroWorkflow(r.Context(), cerebrodb.UpdateCerebroWorkflowParams{
		ID:            id,
		Name:          req.Name,
		Enabled:       enabled,
		ProjectID:     projectID,
		TriggerType:   req.TriggerType,
		TriggerConfig: defaultJSON(req.TriggerConfig, "{}"),
		Conditions:    defaultJSON(req.Conditions, "[]"),
		ActionType:    req.ActionType,
		ActionConfig:  defaultJSON(req.ActionConfig, "{}"),
		EditorMode:    editorMode,
		EditorLayout:  []byte(req.EditorLayout),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow")
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
