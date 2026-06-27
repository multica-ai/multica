package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// computeNextRun delegates to the shared cron helper in the service package.
func computeNextRun(cronExpr, timezone string) (time.Time, error) {
	return service.ComputeNextRun(cronExpr, timezone)
}

// ── Response types ──────────────────────────────────────────────────────────

type AutopilotResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	ProjectID   *string `json:"project_id"`
	// AssigneeType is "agent" or "squad". Path A from MUL-2429: when set
	// to "squad", AssigneeID points at squad(id) rather than agent(id) and
	// dispatch resolves to squad.leader_id at run time.
	AssigneeType       string  `json:"assignee_type"`
	AssigneeID         string  `json:"assignee_id"`
	Status             string  `json:"status"`
	ExecutionMode      string  `json:"execution_mode"`
	IssueTitleTemplate *string `json:"issue_title_template"`
	CreatedByType      string  `json:"created_by_type"`
	CreatedByID        string  `json:"created_by_id"`
	LastRunAt          *string `json:"last_run_at"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`

	// List-endpoint-only derived fields (absent on the detail/create/update
	// responses and on older servers — clients must treat them as optional).
	// Enabled triggers only; last_run_status is the most recent run's status.
	TriggerKinds  []string `json:"trigger_kinds,omitempty"`
	NextRunAt     *string  `json:"next_run_at,omitempty"`
	LastRunStatus *string  `json:"last_run_status,omitempty"`

	// Always non-nil (empty slice when no subscribers configured) so
	// frontend optional-chain rules can treat the field as authoritative.
	Subscribers []AutopilotSubscriberEntry `json:"subscribers"`
}

// user_type is restricted to "member" at the DB layer; the field is kept on
// the wire so a future expansion to agents/squads is additive, not breaking.
type AutopilotSubscriberEntry struct {
	UserType  string `json:"user_type"`
	UserID    string `json:"user_id"`
	CreatedAt string `json:"created_at"`
}

type AutopilotTriggerResponse struct {
	ID             string  `json:"id"`
	AutopilotID    string  `json:"autopilot_id"`
	Kind           string  `json:"kind"`
	Enabled        bool    `json:"enabled"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	NextRunAt      *string `json:"next_run_at"`
	WebhookToken   *string `json:"webhook_token"`
	// WebhookPath is computed from webhook_token. Always present for webhook
	// triggers; nil for schedule/api. Not stored — see triggerToResponse.
	WebhookPath *string `json:"webhook_path"`
	// WebhookURL is the absolute URL composed from the server's
	// MULTICA_PUBLIC_URL setting. Nil when the server has no public URL
	// configured; clients then build the URL themselves from webhook_path
	// plus their API base / current origin.
	WebhookURL *string `json:"webhook_url"`
	// Provider names the per-endpoint signing/dedupe convention. For now:
	// "generic" (bearer URL only, Idempotency-Key for dedupe) or "github"
	// (X-Hub-Signature-256 + X-GitHub-Delivery). Omitted for non-webhook
	// triggers.
	Provider *string `json:"provider"`
	// HasSigningSecret indicates whether a signing secret is configured on
	// the trigger. The secret itself is never returned — it is set via a
	// dedicated write-only endpoint. Always false for non-webhook triggers.
	HasSigningSecret bool `json:"has_signing_secret"`
	// SigningSecretHint is the last 4 characters of the configured secret,
	// surfaced to help operators tell two secrets apart in the UI. Nil when
	// no secret is configured.
	SigningSecretHint *string `json:"signing_secret_hint"`
	Label             *string `json:"label"`
	LastFiredAt       *string `json:"last_fired_at"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	// EventFilters is the declared event scope. Only present for webhook
	// triggers; omitted when the trigger accepts all events. Serializes as
	// a JSON array of {event, actions?} objects — never as a base64 string
	// (which is what []byte would produce through encoding/json).
	EventFilters []WebhookEventFilter `json:"event_filters,omitempty"`
}

type AutopilotRunResponse struct {
	ID             string  `json:"id"`
	AutopilotID    string  `json:"autopilot_id"`
	TriggerID      *string `json:"trigger_id"`
	Source         string  `json:"source"`
	Status         string  `json:"status"`
	IssueID        *string `json:"issue_id"`
	TaskID         *string `json:"task_id"`
	TriggeredAt    string  `json:"triggered_at"`
	CompletedAt    *string `json:"completed_at"`
	FailureReason  *string `json:"failure_reason"`
	TriggerPayload any     `json:"trigger_payload"`
	Result         any     `json:"result"`
	CreatedAt      string  `json:"created_at"`
}

// ── Converters ──────────────────────────────────────────────────────────────

func autopilotToResponse(a db.Autopilot, subscribers []db.AutopilotSubscriber) AutopilotResponse {
	assigneeType := a.AssigneeType
	if assigneeType == "" {
		// Older rows pre-MUL-2429 may surface as "" against an out-of-date
		// schema view; default to "agent" so the API contract stays
		// non-null.
		assigneeType = "agent"
	}
	subResp := make([]AutopilotSubscriberEntry, len(subscribers))
	for i, s := range subscribers {
		subResp[i] = AutopilotSubscriberEntry{
			UserType:  s.UserType,
			UserID:    uuidToString(s.UserID),
			CreatedAt: timestampToString(s.CreatedAt),
		}
	}
	return AutopilotResponse{
		ID:                 uuidToString(a.ID),
		WorkspaceID:        uuidToString(a.WorkspaceID),
		Title:              a.Title,
		Description:        textToPtr(a.Description),
		ProjectID:          uuidToPtr(a.ProjectID),
		AssigneeType:       assigneeType,
		AssigneeID:         uuidToString(a.AssigneeID),
		Status:             a.Status,
		ExecutionMode:      a.ExecutionMode,
		IssueTitleTemplate: textToPtr(a.IssueTitleTemplate),
		CreatedByType:      a.CreatedByType,
		CreatedByID:        uuidToString(a.CreatedByID),
		LastRunAt:          timestampToPtr(a.LastRunAt),
		CreatedAt:          timestampToString(a.CreatedAt),
		UpdatedAt:          timestampToString(a.UpdatedAt),
		Subscribers:        subResp,
	}
}

func (h *Handler) triggerToResponse(t db.AutopilotTrigger) AutopilotTriggerResponse {
	resp := AutopilotTriggerResponse{
		ID:             uuidToString(t.ID),
		AutopilotID:    uuidToString(t.AutopilotID),
		Kind:           t.Kind,
		Enabled:        t.Enabled,
		CronExpression: textToPtr(t.CronExpression),
		Timezone:       textToPtr(t.Timezone),
		NextRunAt:      timestampToPtr(t.NextRunAt),
		WebhookToken:   textToPtr(t.WebhookToken),
		Label:          textToPtr(t.Label),
		LastFiredAt:    timestampToPtr(t.LastFiredAt),
		CreatedAt:      timestampToString(t.CreatedAt),
		UpdatedAt:      timestampToString(t.UpdatedAt),
	}
	if t.Kind == "webhook" && t.WebhookToken.Valid && t.WebhookToken.String != "" {
		path := webhookPathForToken(t.WebhookToken.String)
		resp.WebhookPath = &path
		if h.cfg.PublicURL != "" {
			full := h.cfg.PublicURL + path
			resp.WebhookURL = &full
		}
		provider := t.Provider
		if provider == "" {
			provider = "generic"
		}
		resp.Provider = &provider
		if t.SigningSecret.Valid && t.SigningSecret.String != "" {
			resp.HasSigningSecret = true
			hint := signingSecretHint(t.SigningSecret.String)
			resp.SigningSecretHint = &hint
		}
		if len(t.EventFilters) > 0 {
			var filters []WebhookEventFilter
			if err := json.Unmarshal(t.EventFilters, &filters); err == nil {
				resp.EventFilters = filters
			}
			// On unmarshal error we deliberately drop the field instead of
			// surfacing raw bytes or 500ing — strict write-time validation
			// is supposed to make this branch unreachable, and the matcher
			// fails closed if a corrupt row ever slips through.
		}
	}
	return resp
}

// signingSecretHint returns the last 4 characters of the signing secret so a
// configured-vs-rotated state is visible in the UI without exposing the
// secret itself. Truncating below 4 chars (which the validator already
// rejects) just returns an empty string.
func signingSecretHint(secret string) string {
	if len(secret) < 4 {
		return ""
	}
	return secret[len(secret)-4:]
}

// webhookPathForToken composes the path used by the public ingress route.
// Kept as a free function (no Handler receiver) so test code that builds
// expected URLs without instantiating a Handler can call it.
func webhookPathForToken(token string) string {
	return "/api/webhooks/autopilots/" + token
}

func runToResponse(r db.AutopilotRun) AutopilotRunResponse {
	var payload any
	if r.TriggerPayload != nil {
		json.Unmarshal(r.TriggerPayload, &payload)
	}
	var result any
	if r.Result != nil {
		json.Unmarshal(r.Result, &result)
	}
	return AutopilotRunResponse{
		ID:             uuidToString(r.ID),
		AutopilotID:    uuidToString(r.AutopilotID),
		TriggerID:      uuidToPtr(r.TriggerID),
		Source:         r.Source,
		Status:         r.Status,
		IssueID:        uuidToPtr(r.IssueID),
		TaskID:         uuidToPtr(r.TaskID),
		TriggeredAt:    timestampToString(r.TriggeredAt),
		CompletedAt:    timestampToPtr(r.CompletedAt),
		FailureReason:  textToPtr(r.FailureReason),
		TriggerPayload: payload,
		Result:         result,
		CreatedAt:      timestampToString(r.CreatedAt),
	}
}

// runToResponseSlim mirrors runToResponse but omits TriggerPayload, intended
// for list endpoints where echoing the full webhook envelope (up to
// 256 KiB × N rows) would dominate response size. Clients fetch the full
// payload via GET /api/autopilots/{id}/runs/{runId} when the user opens
// the run detail dialog.
func runToResponseSlim(r db.AutopilotRun) AutopilotRunResponse {
	resp := runToResponse(r)
	resp.TriggerPayload = nil
	return resp
}

// ── Request types ───────────────────────────────────────────────────────────

type CreateAutopilotRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	ProjectID   *string `json:"project_id"`
	// AssigneeType is optional and defaults to "agent" — preserves backward
	// compatibility with desktop clients shipped before MUL-2429.
	AssigneeType       *string           `json:"assignee_type"`
	AssigneeID         string            `json:"assignee_id"`
	ExecutionMode      string            `json:"execution_mode"`
	IssueTitleTemplate *string           `json:"issue_title_template"`
	Subscribers        []SubscriberInput `json:"subscribers"`
}

type UpdateAutopilotRequest struct {
	Title              *string `json:"title"`
	Description        *string `json:"description"`
	ProjectID          *string `json:"project_id"`
	AssigneeType       *string `json:"assignee_type"`
	AssigneeID         *string `json:"assignee_id"`
	Status             *string `json:"status"`
	ExecutionMode      *string `json:"execution_mode"`
	IssueTitleTemplate *string `json:"issue_title_template"`
	// Wholesale replacement when present; omit to leave subscribers untouched.
	Subscribers []SubscriberInput `json:"subscribers"`
}

type SubscriberInput struct {
	UserType string `json:"user_type"`
	UserID   string `json:"user_id"`
}

type CreateAutopilotTriggerRequest struct {
	Kind           string  `json:"kind"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	Label          *string `json:"label"`
	// Provider is currently only meaningful for kind=webhook. Allowed
	// values: "generic" (default) or "github". Unset → "generic".
	Provider *string `json:"provider"`
	// EventFilters is an optional list of {event, actions?} scopes. Only
	// meaningful for webhook triggers. nil/empty means "accept all events".
	EventFilters []WebhookEventFilter `json:"event_filters,omitempty"`
}

// SetSigningSecretRequest is the body shape for PUT
// /api/autopilots/{id}/triggers/{triggerId}/signing-secret. Lives in its own
// type so the secret never appears alongside other fields on the trigger
// update path — handlers that log request bodies for debugging cannot pick it
// up by accident.
type SetSigningSecretRequest struct {
	// SigningSecret is the new HMAC key. Sending an empty string explicitly
	// clears the secret (disables signature verification). Pass any
	// reasonably entropic value — GitHub's docs recommend at least 32 random
	// characters; we enforce a 16-char minimum on non-empty input.
	SigningSecret string `json:"signing_secret"`
}

type UpdateAutopilotTriggerRequest struct {
	Enabled        *bool   `json:"enabled"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	Label          *string `json:"label"`
	// EventFilters is the desired event-filter set with tri-state PATCH
	// semantics:
	//
	//   - omitted / explicit null (nil pointer) → leave the existing value
	//     untouched.
	//   - explicit [] (non-nil, length 0)       → clear filters (the trigger
	//     reverts to "accept all events").
	//   - explicit [...]                        → replace with the supplied
	//     list.
	//
	// This is why the pointer matters: with a plain []WebhookEventFilter
	// there is no way to tell "field absent from the PATCH body" from "field
	// present but empty", and the user can never clear filters once set.
	EventFilters *[]WebhookEventFilter `json:"event_filters,omitempty"`
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (h *Handler) ListAutopilots(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}

	autopilots, err := h.Queries.ListAutopilots(r.Context(), db.ListAutopilotsParams{
		WorkspaceID: parseUUID(workspaceID),
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list autopilots")
		return
	}

	resp := make([]AutopilotResponse, len(autopilots))
	for i, row := range autopilots {
		// Omit subscribers to avoid an N+1; GET /api/autopilots/{id} is
		// the source of truth for the populated template.
		r := autopilotToResponse(row.Autopilot, nil)
		r.TriggerKinds = row.TriggerKinds
		if row.NextRunAt.Valid {
			r.NextRunAt = timestampToPtr(row.NextRunAt)
		}
		if row.LastRunStatus != "" {
			s := row.LastRunStatus
			r.LastRunStatus = &s
		}
		resp[i] = r
	}
	writeJSON(w, http.StatusOK, map[string]any{"autopilots": resp, "total": len(resp)})
}

func (h *Handler) GetAutopilot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	autopilot, ok := h.loadAutopilotInWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	subs, err := h.Queries.ListAutopilotSubscribers(r.Context(), autopilot.ID)
	if err != nil {
		// Don't 500 the detail fetch over template metadata.
		subs = nil
	}
	resp := autopilotToResponse(autopilot, subs)

	// Include triggers.
	triggers, err := h.Queries.ListAutopilotTriggers(r.Context(), autopilot.ID)
	if err != nil {
		triggers = nil
	}
	triggerResp := make([]AutopilotTriggerResponse, len(triggers))
	for i, t := range triggers {
		triggerResp[i] = h.triggerToResponse(t)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"autopilot": resp,
		"triggers":  triggerResp,
	})
}

func (h *Handler) loadAutopilotInWorkspace(w http.ResponseWriter, r *http.Request, autopilotID, workspaceID string) (db.Autopilot, bool) {
	autopilotUUID, ok := parseUUIDOrBadRequest(w, autopilotID, "autopilot id")
	if !ok {
		return db.Autopilot{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Autopilot{}, false
	}

	autopilot, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          autopilotUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return db.Autopilot{}, false
	}
	return autopilot, true
}

func (h *Handler) CreateAutopilot(w http.ResponseWriter, r *http.Request) {
	var req CreateAutopilotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.AssigneeID == "" {
		writeError(w, http.StatusBadRequest, "assignee_id is required")
		return
	}
	if req.ExecutionMode == "" {
		writeError(w, http.StatusBadRequest, "execution_mode is required")
		return
	}
	if req.ExecutionMode != "create_issue" && req.ExecutionMode != "run_only" {
		writeError(w, http.StatusBadRequest, "execution_mode must be create_issue or run_only")
		return
	}
	if req.IssueTitleTemplate != nil {
		if err := service.ValidateIssueTitleTemplate(*req.IssueTitleTemplate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	assigneeUUID, ok := parseUUIDOrBadRequest(w, req.AssigneeID, "assignee_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	assigneeType := "agent"
	if req.AssigneeType != nil && *req.AssigneeType != "" {
		assigneeType = *req.AssigneeType
	}
	if !isValidAutopilotAssigneeType(assigneeType) {
		writeError(w, http.StatusBadRequest, "assignee_type must be agent or squad")
		return
	}
	if !h.validateAutopilotAssignee(w, r, assigneeType, assigneeUUID, wsUUID) {
		return
	}
	projectID, ok := h.parseAutopilotProjectID(w, r, req.ProjectID, wsUUID)
	if !ok {
		return
	}

	// Validate before insert so a bad payload doesn't half-create the row.
	subscriberUUIDs, ok := h.validateAutopilotSubscribers(w, r, req.Subscribers, workspaceID)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create autopilot")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	autopilot, err := qtx.CreateAutopilot(r.Context(), db.CreateAutopilotParams{
		WorkspaceID:        wsUUID,
		Title:              req.Title,
		AssigneeType:       assigneeType,
		AssigneeID:         assigneeUUID,
		Status:             "active",
		ExecutionMode:      req.ExecutionMode,
		CreatedByType:      "member",
		CreatedByID:        parseUUID(userID),
		Description:        ptrToText(req.Description),
		IssueTitleTemplate: ptrToText(req.IssueTitleTemplate),
		ProjectID:          projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create autopilot")
		return
	}

	for _, uid := range subscriberUUIDs {
		if err := qtx.AddAutopilotSubscriber(r.Context(), db.AddAutopilotSubscriberParams{
			AutopilotID: autopilot.ID,
			UserType:    "member",
			UserID:      uid,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add autopilot subscriber")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create autopilot")
		return
	}
	subs, err := h.Queries.ListAutopilotSubscribers(r.Context(), autopilot.ID)
	if err != nil {
		subs = nil
	}

	resp := autopilotToResponse(autopilot, subs)
	h.publish(protocol.EventAutopilotCreated, workspaceID, "member", userID, map[string]any{"autopilot": resp})
	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AutopilotCreated(
		userID,
		workspaceID,
		uuidToString(autopilot.ID),
		"manual",
		"manual",
	))
	writeJSON(w, http.StatusCreated, resp)
}

// Writes an HTTP error and returns ok=false on the first invalid entry.
// Returns (nil, true) when raw is empty — caller distinguishes "leave alone"
// from "replace with empty" via the raw-fields map, not this return.
func (h *Handler) validateAutopilotSubscribers(
	w http.ResponseWriter,
	r *http.Request,
	raw []SubscriberInput,
	workspaceID string,
) ([]pgtype.UUID, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	out := make([]pgtype.UUID, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for i, entry := range raw {
		if entry.UserType != "member" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("subscribers[%d].user_type must be 'member'", i))
			return nil, false
		}
		if entry.UserID == "" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("subscribers[%d].user_id is required", i))
			return nil, false
		}
		uid, ok := parseUUIDOrBadRequest(w, entry.UserID, fmt.Sprintf("subscribers[%d].user_id", i))
		if !ok {
			return nil, false
		}
		if seen[entry.UserID] {
			continue
		}
		seen[entry.UserID] = true
		if !h.isWorkspaceEntity(r.Context(), entry.UserType, entry.UserID, workspaceID) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("subscribers[%d] is not a member of this workspace", i))
			return nil, false
		}
		out = append(out, uid)
	}
	return out, true
}

func (h *Handler) UpdateAutopilot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	prev, ok := h.loadAutopilotInWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateAutopilotRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateAutopilotParams{
		ID:                 prev.ID,
		Description:        prev.Description,
		AssigneeID:         prev.AssigneeID,
		IssueTitleTemplate: prev.IssueTitleTemplate,
		ProjectID:          prev.ProjectID,
	}
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.ExecutionMode != nil {
		params.ExecutionMode = pgtype.Text{String: *req.ExecutionMode, Valid: true}
	}
	if _, ok := rawFields["description"]; ok {
		params.Description = ptrToText(req.Description)
	}
	if _, ok := rawFields["issue_title_template"]; ok {
		if req.IssueTitleTemplate != nil {
			if err := service.ValidateIssueTitleTemplate(*req.IssueTitleTemplate); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		params.IssueTitleTemplate = ptrToText(req.IssueTitleTemplate)
	}
	if _, ok := rawFields["project_id"]; ok {
		projectID, ok := h.parseAutopilotProjectID(w, r, req.ProjectID, prev.WorkspaceID)
		if !ok {
			return
		}
		params.ProjectID = projectID
	}
	// assignee_type and assignee_id are validated as a pair: switching
	// between agent and squad without supplying a new id would leave the
	// row pointing at the wrong table. The client is expected to send both
	// fields on any change; partial updates that change only one are
	// rejected.
	_, typeSent := rawFields["assignee_type"]
	_, idSent := rawFields["assignee_id"]
	if typeSent || idSent {
		nextType := prev.AssigneeType
		if typeSent && req.AssigneeType != nil && *req.AssigneeType != "" {
			nextType = *req.AssigneeType
		}
		if !isValidAutopilotAssigneeType(nextType) {
			writeError(w, http.StatusBadRequest, "assignee_type must be agent or squad")
			return
		}
		nextID := prev.AssigneeID
		if idSent {
			if req.AssigneeID == nil {
				writeError(w, http.StatusBadRequest, "assignee_id cannot be null")
				return
			}
			parsed, ok := parseUUIDOrBadRequest(w, *req.AssigneeID, "assignee_id")
			if !ok {
				return
			}
			nextID = parsed
		}
		// Reject the agent↔squad switch without a paired id, otherwise the
		// row would address agent(id) under assignee_type='squad' or vice
		// versa.
		if typeSent && !idSent && nextType != prev.AssigneeType {
			writeError(w, http.StatusBadRequest, "assignee_id is required when changing assignee_type")
			return
		}
		if !h.validateAutopilotAssignee(w, r, nextType, nextID, prev.WorkspaceID) {
			return
		}
		if typeSent {
			params.AssigneeType = pgtype.Text{String: nextType, Valid: true}
		}
		if idSent {
			params.AssigneeID = nextID
		}
	}

	// Subscribers are validated up-front (before any write) so a bad payload
	// doesn't leave the autopilot row updated but the template stale.
	var (
		subscriberUUIDs    []pgtype.UUID
		replaceSubscribers bool
	)
	if _, sent := rawFields["subscribers"]; sent {
		replaceSubscribers = true
		validated, vok := h.validateAutopilotSubscribers(w, r, req.Subscribers, workspaceID)
		if !vok {
			return
		}
		subscriberUUIDs = validated
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update autopilot")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	autopilot, err := qtx.UpdateAutopilot(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update autopilot")
		return
	}

	if replaceSubscribers {
		if err := qtx.DeleteAutopilotSubscribersForAutopilot(r.Context(), autopilot.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update subscribers")
			return
		}
		for _, uid := range subscriberUUIDs {
			if err := qtx.AddAutopilotSubscriber(r.Context(), db.AddAutopilotSubscriberParams{
				AutopilotID: autopilot.ID,
				UserType:    "member",
				UserID:      uid,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to add autopilot subscriber")
				return
			}
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update autopilot")
		return
	}

	subs, err := h.Queries.ListAutopilotSubscribers(r.Context(), autopilot.ID)
	if err != nil {
		subs = nil
	}
	resp := autopilotToResponse(autopilot, subs)
	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{"autopilot": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) parseAutopilotProjectID(
	w http.ResponseWriter,
	r *http.Request,
	raw *string,
	workspaceID pgtype.UUID,
) (pgtype.UUID, bool) {
	if raw == nil || *raw == "" {
		return pgtype.UUID{}, true
	}
	projectID, ok := parseUUIDOrBadRequest(w, *raw, "project_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          projectID,
		WorkspaceID: workspaceID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "project_id must reference a project in this workspace")
		return pgtype.UUID{}, false
	}
	return projectID, true
}

func (h *Handler) DeleteAutopilot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	idUUID, ok := parseUUIDOrBadRequest(w, id, "autopilot id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// autopilot_subscriber carries no DB-level foreign key/cascade (repo rule:
	// referential cleanup lives in the application layer), so delete the
	// subscriber template alongside the autopilot in one transaction. Without
	// this, deleting an autopilot would orphan its subscriber rows.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete autopilot")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.DeleteAutopilotSubscribersForAutopilot(r.Context(), idUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete autopilot")
		return
	}
	if err := qtx.DeleteAutopilot(r.Context(), idUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete autopilot")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete autopilot")
		return
	}

	h.publish(protocol.EventAutopilotDeleted, workspaceID, "member", userID, map[string]any{"autopilot_id": uuidToString(idUUID)})
	w.WriteHeader(http.StatusNoContent)
}

// ── Trigger management ──────────────────────────────────────────────────────

func (h *Handler) CreateAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	ap, ok := h.loadAutopilotInWorkspace(w, r, autopilotID, workspaceID)
	if !ok {
		return
	}

	var req CreateAutopilotTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Kind == "" {
		writeError(w, http.StatusBadRequest, "kind is required")
		return
	}
	if req.Kind != "schedule" && req.Kind != "webhook" {
		// "api" kind is deprecated: it was reserved-but-inert (no scheduler,
		// no ingress route), and the only way to actually fire one was via
		// the manual /trigger endpoint — which already works regardless of
		// trigger kind. Surface stragglers with 400 so callers move to
		// schedule or webhook.
		writeError(w, http.StatusBadRequest, "kind must be schedule or webhook")
		return
	}
	if req.Kind == "schedule" && (req.CronExpression == nil || *req.CronExpression == "") {
		writeError(w, http.StatusBadRequest, "cron_expression is required for schedule triggers")
		return
	}
	if req.Kind == "webhook" && req.Timezone != nil && *req.Timezone != "" {
		// Webhook triggers fire on demand from external POSTs — they have no
		// next_run_at to compute, so a timezone is meaningless. Reject loudly
		// instead of silently dropping the field.
		writeError(w, http.StatusBadRequest, "timezone is not valid for webhook triggers")
		return
	}
	if req.Kind != "webhook" && len(req.EventFilters) > 0 {
		// event_filters narrows webhook ingress — it has no meaning for a
		// schedule trigger and would otherwise be silently dropped.
		writeError(w, http.StatusBadRequest, "event_filters is only valid for webhook triggers")
		return
	}
	if err := validateWebhookEventFilters(req.EventFilters); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Provider only applies to webhook triggers and the value space is
	// closed — reject unknowns early so a typo on create doesn't quietly
	// degrade into a "generic" trigger that bypasses provider-specific
	// dedupe / signature behaviour.
	provider := "generic"
	if req.Provider != nil && *req.Provider != "" {
		if req.Kind != "webhook" {
			writeError(w, http.StatusBadRequest, "provider is only valid for webhook triggers")
			return
		}
		if !isAllowedWebhookProvider(*req.Provider) {
			writeError(w, http.StatusBadRequest, "provider must be generic or github")
			return
		}
		provider = *req.Provider
	}

	if req.Timezone != nil && *req.Timezone != "" {
		if err := service.ValidateTimezone(*req.Timezone); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// kind-specific normalization. Webhook triggers ignore cron/timezone/
	// next_run_at — they're fired on demand.
	var (
		nextRunAt    pgtype.Timestamptz
		cronText     pgtype.Text
		tzText       pgtype.Text
		webhookToken pgtype.Text
	)
	switch req.Kind {
	case "schedule":
		cronText = ptrToText(req.CronExpression)
		tzText = ptrToText(req.Timezone)
		tz := "UTC"
		if req.Timezone != nil && *req.Timezone != "" {
			tz = *req.Timezone
		}
		t, err := computeNextRun(*req.CronExpression, tz)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		nextRunAt = pgtype.Timestamptz{Time: t, Valid: true}
	case "webhook":
		// Mint the token BEFORE the INSERT so the row never exists in a
		// half-written kind=webhook + webhook_token=NULL state. If the
		// random token happens to collide with an existing unique-index
		// entry (vanishingly unlikely with 256 bits but the retry keeps
		// the failure mode obvious if RNG is degraded), we re-generate
		// and re-INSERT — never UPDATE.
		eventFiltersBytes, err := encodeWebhookEventFilters(req.EventFilters)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode event_filters")
			return
		}
		trigger, err := h.createWebhookTriggerWithMintedToken(r, ap.ID, ptrToText(req.Label), provider, eventFiltersBytes)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create trigger")
			return
		}
		resp := h.triggerToResponse(trigger)
		userID, _ := requireUserID(w, r)
		h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{
			"autopilot_id": uuidToString(ap.ID),
			"trigger":      resp,
		})
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	trigger, err := h.Queries.CreateAutopilotTrigger(r.Context(), db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           req.Kind,
		Enabled:        true,
		CronExpression: cronText,
		Timezone:       tzText,
		NextRunAt:      nextRunAt,
		Label:          ptrToText(req.Label),
		WebhookToken:   webhookToken,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create trigger")
		return
	}

	resp := h.triggerToResponse(trigger)
	userID, _ := requireUserID(w, r)
	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{
		"autopilot_id": uuidToString(ap.ID),
		"trigger":      resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

// createWebhookTriggerWithMintedToken atomically creates a webhook trigger
// with a freshly minted bearer token in the same INSERT. Avoids the older
// two-step (INSERT then UPDATE webhook_token) pattern which could leave a
// kind=webhook row with NULL webhook_token visible in the UI if the second
// statement failed.
//
// Retries on the unique-index collision case so a vanishingly-rare RNG
// collision turns into a clean retry rather than a 500.
func (h *Handler) createWebhookTriggerWithMintedToken(
	r *http.Request,
	autopilotID pgtype.UUID,
	label pgtype.Text,
	provider string,
	eventFilters []byte,
) (db.AutopilotTrigger, error) {
	for attempt := 0; attempt < 3; attempt++ {
		token, err := generateWebhookToken()
		if err != nil {
			return db.AutopilotTrigger{}, err
		}
		trigger, err := h.Queries.CreateAutopilotTrigger(r.Context(), db.CreateAutopilotTriggerParams{
			AutopilotID:  autopilotID,
			Kind:         "webhook",
			Enabled:      true,
			Label:        label,
			WebhookToken: pgtype.Text{String: token, Valid: true},
			Provider:     pgtype.Text{String: provider, Valid: provider != ""},
			EventFilters: eventFilters,
		})
		if err == nil {
			return trigger, nil
		}
		if !isUniqueViolation(err) {
			return db.AutopilotTrigger{}, err
		}
	}
	return db.AutopilotTrigger{}, fmt.Errorf("could not mint unique webhook token")
}

func isAllowedWebhookProvider(p string) bool {
	switch p {
	case "generic", "github":
		return true
	default:
		return false
	}
}

func isValidAutopilotAssigneeType(t string) bool {
	switch t {
	case "agent", "squad":
		return true
	default:
		return false
	}
}

// validateAutopilotAssignee checks that the assignee (agent or squad) exists
// in the given workspace, and for squad assignees that the squad's leader
// agent is in a workable state at create / update time. Writes an HTTP error
// and returns false on any failure.
//
// At dispatch time the same checks (resolveAutopilotLeader + AgentReadiness)
// run again — they live there to handle "leader was online at save time but
// went offline by trigger time". Save-time validation exists so the user gets
// immediate feedback ("can't pick this squad because its leader is archived")
// instead of discovering the autopilot is dead at the next schedule tick.
func (h *Handler) validateAutopilotAssignee(w http.ResponseWriter, r *http.Request, assigneeType string, assigneeID, workspaceID pgtype.UUID) bool {
	switch assigneeType {
	case "agent":
		if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: workspaceID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "assignee must be a valid agent in this workspace")
			return false
		}
		return true
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "assignee must be a valid squad in this workspace")
			return false
		}
		// Archived squads must be rejected at save time: the dispatcher will
		// otherwise produce an unbroken stream of skipped runs against a
		// squad that can never be revived without an explicit un-archive.
		// Pair with TransferSquadAutopilotsToLeader on DeleteSquad so any
		// autopilot that survives the archive flips to assignee_type='agent'
		// (the leader) and stops referencing the dead squad row.
		if squad.ArchivedAt.Valid {
			writeError(w, http.StatusUnprocessableEntity, "squad is archived; pick a different squad")
			return false
		}
		leader, err := h.Queries.GetAgent(r.Context(), squad.LeaderID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "squad leader agent not found")
			return false
		}
		if leader.ArchivedAt.Valid {
			writeError(w, http.StatusUnprocessableEntity, "squad leader is archived; pick a different squad or rotate the leader before assigning autopilot")
			return false
		}
		// Private-leader gate: the member configuring the autopilot must have
		// access to the private leader, same as validateAssigneePair.
		actorType, actorID := h.resolveActor(r, requestUserID(r), util.UUIDToString(workspaceID))
		if !h.canAccessPrivateAgent(r.Context(), leader, actorType, actorID, util.UUIDToString(workspaceID)) {
			writeError(w, http.StatusForbidden, "cannot assign autopilot to squad with private leader")
			return false
		}
		return true
	default:
		writeError(w, http.StatusBadRequest, "assignee_type must be agent or squad")
		return false
	}
}

func (h *Handler) UpdateAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "triggerId")
	workspaceID := h.resolveWorkspaceID(r)

	ap, ok := h.loadAutopilotInWorkspace(w, r, autopilotID, workspaceID)
	if !ok {
		return
	}

	triggerUUID, ok := parseUUIDOrBadRequest(w, triggerID, "trigger id")
	if !ok {
		return
	}

	prev, err := h.Queries.GetAutopilotTrigger(r.Context(), triggerUUID)
	if err != nil || uuidToString(prev.AutopilotID) != uuidToString(ap.ID) {
		writeError(w, http.StatusNotFound, "trigger not found")
		return
	}

	var req UpdateAutopilotTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Kind-specific validation. Mirrors the create-path discipline: cron
	// and timezone only make sense on schedule triggers, so reject loudly
	// rather than persisting fields that no code path reads. enabled and
	// label remain valid on every kind.
	if prev.Kind != "schedule" {
		if req.CronExpression != nil {
			writeError(w, http.StatusBadRequest, "cron_expression is only valid for schedule triggers")
			return
		}
		if req.Timezone != nil {
			writeError(w, http.StatusBadRequest, "timezone is only valid for schedule triggers")
			return
		}
	}

	params := db.UpdateAutopilotTriggerParams{
		ID:             prev.ID,
		CronExpression: prev.CronExpression,
		Timezone:       prev.Timezone,
		NextRunAt:      prev.NextRunAt,
		Label:          prev.Label,
	}
	if req.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}
	if req.CronExpression != nil {
		params.CronExpression = pgtype.Text{String: *req.CronExpression, Valid: true}
	}
	if req.Timezone != nil {
		if *req.Timezone != "" {
			if err := service.ValidateTimezone(*req.Timezone); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		params.Timezone = pgtype.Text{String: *req.Timezone, Valid: true}
	}
	if req.Label != nil {
		params.Label = pgtype.Text{String: *req.Label, Valid: true}
	}
	// Tri-state PATCH for event_filters. A nil pointer (field omitted or
	// JSON null) leaves the existing row untouched — params.EventFilters
	// stays unset and the COALESCE in the UPDATE preserves the previous
	// value. A non-nil pointer is authoritative: an empty slice clears
	// filters (encoded as the JSONB literal `[]` so COALESCE replaces
	// rather than preserves), a populated slice replaces.
	if req.EventFilters != nil {
		if prev.Kind != "webhook" {
			writeError(w, http.StatusBadRequest, "event_filters is only valid for webhook triggers")
			return
		}
		if err := validateWebhookEventFilters(*req.EventFilters); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		encoded, err := encodeWebhookEventFiltersAlways(*req.EventFilters)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode event_filters")
			return
		}
		params.EventFilters = encoded
	}

	// Recompute next_run_at if cron or timezone changed.
	cronExpr := prev.CronExpression.String
	if req.CronExpression != nil {
		cronExpr = *req.CronExpression
	}
	tz := "UTC"
	if prev.Timezone.Valid {
		tz = prev.Timezone.String
	}
	if req.Timezone != nil {
		tz = *req.Timezone
	}
	if prev.Kind == "schedule" && cronExpr != "" {
		t, err := computeNextRun(cronExpr, tz)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.NextRunAt = pgtype.Timestamptz{Time: t, Valid: true}
	}

	trigger, err := h.Queries.UpdateAutopilotTrigger(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update trigger")
		return
	}

	resp := h.triggerToResponse(trigger)
	userID, _ := requireUserID(w, r)
	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{
		"autopilot_id": uuidToString(ap.ID),
		"trigger":      resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "triggerId")
	workspaceID := h.resolveWorkspaceID(r)

	autopilotUUID, ok := parseUUIDOrBadRequest(w, autopilotID, "autopilot id")
	if !ok {
		return
	}
	triggerUUID, ok := parseUUIDOrBadRequest(w, triggerID, "trigger id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          autopilotUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}

	trigger, err := h.Queries.GetAutopilotTrigger(r.Context(), triggerUUID)
	if err != nil || uuidToString(trigger.AutopilotID) != uuidToString(autopilotUUID) {
		writeError(w, http.StatusNotFound, "trigger not found")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if err := h.Queries.DeleteAutopilotTrigger(r.Context(), triggerUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete trigger")
		return
	}

	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{
		"autopilot_id": uuidToString(autopilotUUID),
		"trigger_id":   uuidToString(triggerUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RotateAutopilotTriggerWebhookToken issues a fresh bearer token for an
// existing webhook trigger. The old token stops working immediately because
// the unique-index lookup in the public ingress route is keyed on the
// current row value.
func (h *Handler) RotateAutopilotTriggerWebhookToken(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "triggerId")
	workspaceID := h.resolveWorkspaceID(r)

	ap, ok := h.loadAutopilotInWorkspace(w, r, autopilotID, workspaceID)
	if !ok {
		return
	}

	triggerUUID, ok := parseUUIDOrBadRequest(w, triggerID, "trigger id")
	if !ok {
		return
	}
	prev, err := h.Queries.GetAutopilotTrigger(r.Context(), triggerUUID)
	if err != nil || uuidToString(prev.AutopilotID) != uuidToString(ap.ID) {
		writeError(w, http.StatusNotFound, "trigger not found")
		return
	}
	if prev.Kind != "webhook" {
		writeError(w, http.StatusBadRequest, "trigger is not a webhook trigger")
		return
	}

	var rotated db.AutopilotTrigger
	for attempt := 0; attempt < 3; attempt++ {
		token, terr := generateWebhookToken()
		if terr != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate webhook token")
			return
		}
		rotated, err = h.Queries.RotateAutopilotTriggerWebhookToken(r.Context(), db.RotateAutopilotTriggerWebhookTokenParams{
			ID:           triggerUUID,
			WebhookToken: pgtype.Text{String: token, Valid: true},
		})
		if err == nil {
			break
		}
		if !isUniqueViolation(err) {
			writeError(w, http.StatusInternalServerError, "failed to rotate webhook token")
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate webhook token")
		return
	}

	resp := h.triggerToResponse(rotated)
	userID, _ := requireUserID(w, r)
	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{
		"autopilot_id": uuidToString(ap.ID),
		"trigger":      resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

// SetAutopilotTriggerSigningSecret sets (or clears) the HMAC signing secret
// for a webhook trigger. Lives on its own endpoint so the secret value never
// shares a request body with any other field — keeping it out of generic
// request-body logs and audit captures that may include patch payloads.
//
// Empty body / empty `signing_secret` clears the secret and reverts the
// trigger to bearer-token-only authentication. The response carries
// `has_signing_secret` + `signing_secret_hint`; the secret itself is never
// echoed back, matching the GitHub / Stripe industry pattern.
func (h *Handler) SetAutopilotTriggerSigningSecret(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "triggerId")
	workspaceID := h.resolveWorkspaceID(r)

	ap, ok := h.loadAutopilotInWorkspace(w, r, autopilotID, workspaceID)
	if !ok {
		return
	}
	triggerUUID, ok := parseUUIDOrBadRequest(w, triggerID, "trigger id")
	if !ok {
		return
	}
	prev, err := h.Queries.GetAutopilotTrigger(r.Context(), triggerUUID)
	if err != nil || uuidToString(prev.AutopilotID) != uuidToString(ap.ID) {
		writeError(w, http.StatusNotFound, "trigger not found")
		return
	}
	if prev.Kind != "webhook" {
		writeError(w, http.StatusBadRequest, "trigger is not a webhook trigger")
		return
	}

	var req SetSigningSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	secret := strings.TrimSpace(req.SigningSecret)
	// 16 chars is the floor: enough to make brute force impractical for the
	// SHA-256 HMAC but low enough not to reject providers that mint shorter
	// keys (Slack signing secrets are 32 hex chars; GitHub recommends 32).
	if secret != "" && len(secret) < 16 {
		writeError(w, http.StatusBadRequest, "signing_secret must be at least 16 characters")
		return
	}

	param := db.SetAutopilotTriggerSigningSecretParams{ID: triggerUUID}
	if secret != "" {
		param.SigningSecret = pgtype.Text{String: secret, Valid: true}
	}
	updated, err := h.Queries.SetAutopilotTriggerSigningSecret(r.Context(), param)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update signing secret")
		return
	}

	resp := h.triggerToResponse(updated)
	userID, _ := requireUserID(w, r)
	// Publish the trigger update so the UI can refresh the has_signing_secret
	// badge in real time. The event payload only carries the response shape,
	// which excludes the secret.
	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{
		"autopilot_id": uuidToString(ap.ID),
		"trigger":      resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

// ── Runs ────────────────────────────────────────────────────────────────────

func (h *Handler) ListAutopilotRuns(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	autopilot, ok := h.loadAutopilotInWorkspace(w, r, autopilotID, workspaceID)
	if !ok {
		return
	}

	limit := int32(20)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = int32(v)
		}
	}
	if limit > 100 {
		limit = 100
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = int32(v)
		}
	}

	runs, err := h.Queries.ListAutopilotRuns(r.Context(), db.ListAutopilotRunsParams{
		AutopilotID: autopilot.ID,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}

	resp := make([]AutopilotRunResponse, len(runs))
	for i, run := range runs {
		// Omit trigger_payload in the list response — a webhook envelope
		// can be up to 256 KiB and `limit` defaults to 20, so the full
		// list would be a ~5 MB worst case. Detail dialog fetches the
		// full payload from GetAutopilotRun.
		resp[i] = runToResponseSlim(run)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": resp, "total": len(resp)})
}

// GetAutopilotRun returns a single run including its full trigger_payload.
// Workspace scoping is enforced via loadAutopilotInWorkspace; the run is
// then re-checked to belong to that autopilot so a guessed runId from
// another workspace cannot leak data.
func (h *Handler) GetAutopilotRun(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	runID := chi.URLParam(r, "runId")
	workspaceID := h.resolveWorkspaceID(r)

	autopilot, ok := h.loadAutopilotInWorkspace(w, r, autopilotID, workspaceID)
	if !ok {
		return
	}

	runUUID, ok := parseUUIDOrBadRequest(w, runID, "run id")
	if !ok {
		return
	}

	run, err := h.Queries.GetAutopilotRun(r.Context(), runUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if uuidToString(run.AutopilotID) != uuidToString(autopilot.ID) {
		// Guard against a runId from another autopilot being requested via
		// this autopilot's URL — fail closed with 404 so the response shape
		// matches the "not found" case and no information is leaked.
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	writeJSON(w, http.StatusOK, runToResponse(run))
}

// ── Manual trigger ──────────────────────────────────────────────────────────

func (h *Handler) TriggerAutopilot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	autopilot, ok := h.loadAutopilotInWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}
	if autopilot.Status != "active" {
		writeError(w, http.StatusBadRequest, "autopilot is not active")
		return
	}

	run, err := h.AutopilotService.DispatchAutopilot(r.Context(), autopilot, pgtype.UUID{}, "manual", nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to trigger autopilot: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, runToResponse(*run))
}
