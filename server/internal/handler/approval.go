package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response types ──────────────────────────────────────────────────────────

type ApprovalRequestResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	Operation       string  `json:"operation"`
	TargetType      string  `json:"target_type"`
	TargetID        *string `json:"target_id"`
	Reason          string  `json:"reason"`
	Status          string  `json:"status"`
	InitiatedByType string  `json:"initiated_by_type"`
	InitiatedByID   string  `json:"initiated_by_id"`
	CurrentStep     int32   `json:"current_step"`
	DecidedByType   *string `json:"decided_by_type"`
	DecidedByID     *string `json:"decided_by_id"`
	DecidedAt       *string `json:"decided_at"`
	DecisionComment string  `json:"decision_comment"`
	Payload         any     `json:"payload"`
	ExpiresAt       *string `json:"expires_at"`
	ExecutedAt      *string `json:"executed_at"`
	ExecutionError  *string `json:"execution_error"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type ApprovalEventResponse struct {
	ID                string  `json:"id"`
	ApprovalRequestID string  `json:"approval_request_id"`
	EventType         string  `json:"event_type"`
	ActorType         *string `json:"actor_type"`
	ActorID           *string `json:"actor_id"`
	Comment           string  `json:"comment"`
	Details           any     `json:"details"`
	CreatedAt         string  `json:"created_at"`
}

func approvalRequestToResponse(ar db.ApprovalRequest) ApprovalRequestResponse {
	var payload any
	if len(ar.Payload) > 0 {
		_ = json.Unmarshal(ar.Payload, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return ApprovalRequestResponse{
		ID:              uuidToString(ar.ID),
		WorkspaceID:     uuidToString(ar.WorkspaceID),
		Operation:       ar.Operation,
		TargetType:      ar.TargetType,
		TargetID:        uuidToPtr(ar.TargetID),
		Reason:          ar.Reason,
		Status:          ar.Status,
		InitiatedByType: ar.InitiatedByType,
		InitiatedByID:   uuidToString(ar.InitiatedByID),
		CurrentStep:     ar.CurrentStep,
		DecidedByType:   textToPtr(ar.DecidedByType),
		DecidedByID:     uuidToPtr(ar.DecidedByID),
		DecidedAt:       timestampToPtr(ar.DecidedAt),
		DecisionComment: ar.DecisionComment,
		Payload:         payload,
		ExpiresAt:       timestampToPtr(ar.ExpiresAt),
		ExecutedAt:      timestampToPtr(ar.ExecutedAt),
		ExecutionError:  textToPtr(ar.ExecutionError),
		CreatedAt:       timestampToString(ar.CreatedAt),
		UpdatedAt:       timestampToString(ar.UpdatedAt),
	}
}

func approvalEventToResponse(e db.ApprovalEvent) ApprovalEventResponse {
	var details any
	if len(e.Details) > 0 {
		_ = json.Unmarshal(e.Details, &details)
	}
	if details == nil {
		details = map[string]any{}
	}
	var actorID *string
	if e.ActorID.Valid {
		s := uuidToString(e.ActorID)
		actorID = &s
	}
	return ApprovalEventResponse{
		ID:                uuidToString(e.ID),
		ApprovalRequestID: uuidToString(e.ApprovalRequestID),
		EventType:         e.EventType,
		ActorType:         textToPtr(e.ActorType),
		ActorID:           actorID,
		Comment:           e.Comment,
		Details:           details,
		CreatedAt:         timestampToString(e.CreatedAt),
	}
}

// ── Request types ───────────────────────────────────────────────────────────

type CreateApprovalRequest struct {
	Operation string           `json:"operation"`
	TargetID  string           `json:"target_id"`
	Reason    string           `json:"reason"`
	Payload   map[string]any   `json:"payload"`
	ExpiresAt *string          `json:"expires_at"`
}

type DecideApprovalRequest struct {
	Comment *string `json:"comment"`
}

type UpdateApprovalConfigRequest struct {
	Enabled    bool     `json:"enabled"`
	Operations []string `json:"operations"`
}

type ApprovalConfigResponse struct {
	Enabled    bool     `json:"enabled"`
	Operations []string `json:"operations"`
	Available  []string `json:"available"`
}

// ── Config helpers ──────────────────────────────────────────────────────────

// approvalConfig is the per-workspace approval-flow config, stored under
// workspace.settings.approval_flow. Operations is the set that requires
// approval; empty means "all built-in operations".
type approvalConfig struct {
	Enabled    bool     `json:"enabled"`
	Operations []string `json:"operations"`
}

// operationRequires reports whether the operation needs approval under this
// config. Empty Operations defaults to every registered (built-in) operation.
func (c approvalConfig) operationRequires(op string) bool {
	if len(c.Operations) == 0 {
		_, ok := sensitiveOpIndex[op]
		return ok
	}
	for _, o := range c.Operations {
		if o == op {
			return true
		}
	}
	return false
}

// approvalFlowEnabled is the platform master switch (feature flag). Per-workspace
// enablement is separate, read via approvalConfig.
func (h *Handler) approvalFlowEnabled(ctx context.Context, workspaceID string) bool {
	return h.FeatureFlags != nil && featureflags.ApprovalFlowEnabled(ctx, h.FeatureFlags)
}

func (h *Handler) approvalConfig(ctx context.Context, workspaceID string) (approvalConfig, error) {
	var cfg approvalConfig
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return cfg, err
	}
	ws, err := h.Queries.GetWorkspace(ctx, wsUUID)
	if err != nil {
		return cfg, err
	}
	var raw map[string]json.RawMessage
	if len(ws.Settings) > 0 {
		if err := json.Unmarshal(ws.Settings, &raw); err == nil {
			if v, ok := raw["approval_flow"]; ok {
				_ = json.Unmarshal(v, &cfg)
			}
		}
	}
	return cfg, nil
}

func readSettingsMap(raw []byte) map[string]any {
	m := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// ── Create (shared by the HTTP handler and the requireApproval gate) ────────

// createApprovalInternal validates and persists a pending approval request plus
// its "created" audit event in one transaction. It writes the error response on
// failure and returns (nil, false); on success returns the new request.
func (h *Handler) createApprovalInternal(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, op, targetID, reason string, payload map[string]any, expiresAt *time.Time) (*db.ApprovalRequest, bool) {
	operation, ok := LookupSensitiveOperation(op)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown operation")
		return nil, false
	}
	if operation.ValidatePayload != nil && payload != nil {
		if err := operation.ValidatePayload(payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return nil, false
		}
	}
	workspaceID := uuidToString(wsUUID)
	if !h.approvalFlowEnabled(r.Context(), workspaceID) {
		writeError(w, http.StatusConflict, "approval flow is not enabled")
		return nil, false
	}
	cfg, err := h.approvalConfig(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read approval config")
		return nil, false
	}
	if !cfg.Enabled {
		writeError(w, http.StatusConflict, "approval flow is not enabled for this workspace")
		return nil, false
	}
	if !cfg.operationRequires(op) {
		writeError(w, http.StatusBadRequest, "operation does not require approval in this workspace")
		return nil, false
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return nil, false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	var targetUUID pgtype.UUID
	if targetID != "" {
		targetUUID, ok = parseUUIDOrBadRequest(w, targetID, "target_id")
		if !ok {
			return nil, false
		}
	}
	payloadBytes := []byte("{}")
	if payload != nil {
		payloadBytes, _ = json.Marshal(payload)
	}
	var expires pgtype.Timestamptz
	if expiresAt != nil {
		expires = pgtype.Timestamptz{Time: *expiresAt, Valid: true}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create approval")
		return nil, false
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	ar, err := qtx.CreateApprovalRequest(r.Context(), db.CreateApprovalRequestParams{
		WorkspaceID:     wsUUID,
		Operation:       op,
		TargetType:      operation.TargetType,
		TargetID:        targetUUID,
		Reason:          reason,
		InitiatedByType: actorType,
		InitiatedByID:   parseUUID(actorID),
		Payload:         payloadBytes,
		ExpiresAt:       expires,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create approval")
		return nil, false
	}

	if _, err := qtx.CreateApprovalEvent(r.Context(), db.CreateApprovalEventParams{
		ApprovalRequestID: ar.ID,
		WorkspaceID:       wsUUID,
		EventType:         "created",
		ActorType:         strToText(actorType),
		ActorID:           parseUUID(actorID),
		Comment:           reason,
		Details:           payloadBytes,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create approval")
		return nil, false
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create approval")
		return nil, false
	}
	return &ar, true
}

// lookupApprovedForOp resolves an approval_id query param to an approved,
// unexecuted request matching the operation. Used by requireApproval to satisfy
// the gate after a request has been approved.
func (h *Handler) lookupApprovedForOp(ctx context.Context, approvalID, workspaceID, op string) (*db.ApprovalRequest, error) {
	id, err := util.ParseUUID(approvalID)
	if err != nil {
		return nil, err
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, err
	}
	ar, err := h.Queries.GetApprovalRequestInWorkspace(ctx, db.GetApprovalRequestInWorkspaceParams{
		ID:          id,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		return nil, err
	}
	if ar.Operation != op || ar.Status != "approved" || ar.ExecutedAt.Valid {
		return nil, fmt.Errorf("not an approved, unexecuted request for this operation")
	}
	return &ar, nil
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (h *Handler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "pending" && status != "approved" && status != "rejected" && status != "cancelled" && status != "expired" {
		writeError(w, http.StatusBadRequest, "invalid status filter")
		return
	}
	var statusText pgtype.Text
	if status != "" {
		statusText = strToText(status)
	}
	limit := listLimitOrDefault(r, 50, 200)
	rows, err := h.Queries.ListApprovalRequests(r.Context(), db.ListApprovalRequestsParams{
		WorkspaceID: wsUUID,
		Limit:       int32(limit),
		Status:      statusText,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list approvals")
		return
	}
	resp := make([]ApprovalRequestResponse, 0, len(rows))
	for _, ar := range rows {
		resp = append(resp, approvalRequestToResponse(ar))
	}
	_ = member
	writeJSON(w, http.StatusOK, map[string]any{"approvals": resp})
}

func (h *Handler) ListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	limit := listLimitOrDefault(r, 50, 200)
	rows, err := h.Queries.ListPendingApprovalRequests(r.Context(), db.ListPendingApprovalRequestsParams{
		WorkspaceID: wsUUID,
		Limit:       int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending approvals")
		return
	}
	resp := make([]ApprovalRequestResponse, 0, len(rows))
	for _, ar := range rows {
		resp = append(resp, approvalRequestToResponse(ar))
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": resp})
}

func (h *Handler) CreateApproval(w http.ResponseWriter, r *http.Request) {
	var req CreateApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Operation == "" {
		writeError(w, http.StatusBadRequest, "operation is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
			return
		}
		expiresAt = &t
	}
	ar, ok := h.createApprovalInternal(w, r, wsUUID, req.Operation, req.TargetID, req.Reason, req.Payload, expiresAt)
	if !ok {
		return
	}
	resp := approvalRequestToResponse(*ar)
	h.publish(protocol.EventApprovalRequestCreated, workspaceID, ar.InitiatedByType, uuidToString(ar.InitiatedByID), map[string]any{"approval": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetApproval(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "approval id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "approval not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	ar, err := h.Queries.GetApprovalRequestInWorkspace(r.Context(), db.GetApprovalRequestInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	writeJSON(w, http.StatusOK, approvalRequestToResponse(ar))
}

// decideApproval handles both approve and reject (owner/admin only). The
// pending->terminal transition is a single conditional UPDATE, so concurrent
// decides cannot both succeed; a no-row result is reported as 409.
func (h *Handler) decideApproval(w http.ResponseWriter, r *http.Request, decision string) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "approval id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "approval not found", "owner", "admin")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req DecideApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	comment := ""
	if req.Comment != nil {
		comment = *req.Comment
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decide approval")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	ar, err := qtx.DecideApprovalRequest(r.Context(), db.DecideApprovalRequestParams{
		ID:              idUUID,
		WorkspaceID:     wsUUID,
		Status:          decision,
		DecidedByType:   strToText("member"),
		DecidedByID:     member.UserID,
		DecisionComment: comment,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusConflict, "approval is no longer pending")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to decide approval")
		return
	}
	if _, err := qtx.CreateApprovalEvent(r.Context(), db.CreateApprovalEventParams{
		ApprovalRequestID: ar.ID,
		WorkspaceID:       wsUUID,
		EventType:         decision,
		ActorType:         strToText("member"),
		ActorID:           member.UserID,
		Comment:           comment,
		Details:           []byte("{}"),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decide approval")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decide approval")
		return
	}

	resp := approvalRequestToResponse(ar)
	h.publish(protocol.EventApprovalRequestDecided, workspaceID, "member", uuidToString(member.UserID), map[string]any{"approval": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ApproveApproval(w http.ResponseWriter, r *http.Request) {
	h.decideApproval(w, r, "approved")
}

func (h *Handler) RejectApproval(w http.ResponseWriter, r *http.Request) {
	h.decideApproval(w, r, "rejected")
}

// CancelApproval lets the initiator withdraw a still-pending request.
func (h *Handler) CancelApproval(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "approval id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	ar, err := h.Queries.CancelApprovalRequest(r.Context(), db.CancelApprovalRequestParams{
		ID:              idUUID,
		WorkspaceID:     wsUUID,
		InitiatedByType: actorType,
		InitiatedByID:   parseUUID(actorID),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusConflict, "approval can only be cancelled by its initiator while pending")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to cancel approval")
		return
	}
	if _, err := h.Queries.CreateApprovalEvent(r.Context(), db.CreateApprovalEventParams{
		ApprovalRequestID: ar.ID,
		WorkspaceID:       wsUUID,
		EventType:         "cancelled",
		ActorType:         strToText(actorType),
		ActorID:           parseUUID(actorID),
		Comment:           "",
		Details:           []byte("{}"),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel approval")
		return
	}
	resp := approvalRequestToResponse(ar)
	h.publish(protocol.EventApprovalRequestCancelled, workspaceID, actorType, actorID, map[string]any{"approval": resp})
	writeJSON(w, http.StatusOK, resp)
}

// ExecuteApproval runs the registered executor for an approved, unexecuted
// request. The approval is the authorization; any workspace member may trigger
// execution (owner/admin already gated the decision). Strong-blocking model:
// the action runs only here, after approval.
func (h *Handler) ExecuteApproval(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "approval id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "approval not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	ar, err := h.Queries.GetApprovalRequestInWorkspace(r.Context(), db.GetApprovalRequestInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	if ar.Status != "approved" {
		writeError(w, http.StatusConflict, "only approved requests can be executed")
		return
	}
	if ar.ExecutedAt.Valid {
		writeError(w, http.StatusConflict, "approval already executed")
		return
	}
	op, ok := LookupSensitiveOperation(ar.Operation)
	if !ok || op.Execute == nil {
		writeError(w, http.StatusInternalServerError, "no executor registered for operation")
		return
	}

	result, execErr := op.Execute(r.Context(), h, wsUUID, ar)
	eventType := "executed"
	execErrStr := ""
	if execErr != nil {
		eventType = "execution_failed"
		execErrStr = execErr.Error()
	}
	var execErrText pgtype.Text
	if execErrStr != "" {
		execErrText = strToText(execErrStr)
	}
	_ = h.Queries.MarkApprovalRequestExecuted(r.Context(), db.MarkApprovalRequestExecutedParams{
		ID:             ar.ID,
		ExecutionError: execErrText,
	})
	details, _ := json.Marshal(map[string]any{"result": result, "error": execErrStr})
	_, _ = h.Queries.CreateApprovalEvent(r.Context(), db.CreateApprovalEventParams{
		ApprovalRequestID: ar.ID,
		WorkspaceID:       wsUUID,
		EventType:         eventType,
		Comment:           "",
		Details:           details,
	})

	if execErr != nil {
		writeError(w, http.StatusInternalServerError, "execution failed: "+execErrStr)
		return
	}
	h.publish(protocol.EventApprovalRequestExecuted, workspaceID, "system", "", map[string]any{"approval_id": uuidToString(ar.ID), "result": result})
	writeJSON(w, http.StatusOK, map[string]any{"approval_id": uuidToString(ar.ID), "result": result})
}

// ListApprovalEvents is the approval history for one request (oldest first).
func (h *Handler) ListApprovalEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "approval id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "approval not found"); !ok {
		return
	}
	rows, err := h.Queries.ListApprovalEvents(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list approval events")
		return
	}
	resp := make([]ApprovalEventResponse, 0, len(rows))
	for _, e := range rows {
		resp = append(resp, approvalEventToResponse(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": resp})
}

func (h *Handler) GetApprovalConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.approvalFlowEnabled(r.Context(), workspaceID) {
		writeError(w, http.StatusNotFound, "approval flow is not enabled")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	cfg, err := h.approvalConfig(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read approval config")
		return
	}
	writeJSON(w, http.StatusOK, ApprovalConfigResponse{
		Enabled:    cfg.Enabled,
		Operations: cfg.Operations,
		Available:  BuiltinSensitiveOpKeys(),
	})
}

func (h *Handler) UpdateApprovalConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.approvalFlowEnabled(r.Context(), workspaceID) {
		writeError(w, http.StatusNotFound, "approval flow is not enabled")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var req UpdateApprovalConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for _, op := range req.Operations {
		if _, ok := LookupSensitiveOperation(op); !ok {
			writeError(w, http.StatusBadRequest, "unknown operation: "+op)
			return
		}
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read workspace")
		return
	}
	settings := readSettingsMap(ws.Settings)
	ops := req.Operations
	if ops == nil {
		ops = []string{}
	}
	settings["approval_flow"] = approvalConfig{Enabled: req.Enabled, Operations: ops}
	out, err := json.Marshal(settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode approval config")
		return
	}
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{
		ID:       wsUUID,
		Settings: out,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update approval config")
		return
	}
	writeJSON(w, http.StatusOK, ApprovalConfigResponse{
		Enabled:    req.Enabled,
		Operations: ops,
		Available:  BuiltinSensitiveOpKeys(),
	})
}

// listLimitOrDefault parses ?limit=, clamps it to [1, max], defaulting to def.
func listLimitOrDefault(r *http.Request, def, max int) int {
	q := r.URL.Query().Get("limit")
	if q == "" {
		return def
	}
	n, err := atoiPositive(q)
	if err != nil || n < 1 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func atoiPositive(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-'0')
		if n > 1<<31 {
			return 0, fmt.Errorf("too large")
		}
	}
	return n, nil
}
