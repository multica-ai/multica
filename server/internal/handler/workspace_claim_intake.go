package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	claimIntakeStateResumed = "resumed"
	claimIntakeStatePaused  = "paused"
)

type workspaceClaimIntakeStatusResponse struct {
	WorkspaceID   string    `json:"workspace_id"`
	State         string    `json:"state"`
	Generation    int64     `json:"generation"`
	UpdatedByType string    `json:"updated_by_type"`
	UpdatedByID   *string   `json:"updated_by_id"`
	AuthSource    string    `json:"auth_source"`
	ActorDisplay  string    `json:"actor_display"`
	Reason        string    `json:"reason"`
	LastActionID  *string   `json:"last_action_id"`
	EffectiveAt   time.Time `json:"effective_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type workspaceClaimIntakeMutationRequest struct {
	Reason             string `json:"reason"`
	ExpectedGeneration *int64 `json:"expected_generation,omitempty"`
}

type workspaceClaimIntakeMutationResponse struct {
	ActionID        string    `json:"action_id"`
	LastActionID    *string   `json:"last_action_id"`
	WorkspaceID     string    `json:"workspace_id"`
	RequestedAction string    `json:"requested_action"`
	PreviousState   string    `json:"previous_state"`
	State           string    `json:"state"`
	Generation      int64     `json:"generation"`
	ActorType       string    `json:"actor_type"`
	ActorID         string    `json:"actor_id"`
	IdempotencyKey  string    `json:"idempotency_key"`
	Reason          string    `json:"reason"`
	RequestedAt     time.Time `json:"requested_at"`
	EffectiveAt     time.Time `json:"effective_at"`
	Result          string    `json:"result"`
	ErrorClass      *string   `json:"error_class"`
}

type workspaceClaimIntakeActionResponse struct {
	ActionID           string    `json:"action_id"`
	WorkspaceID        string    `json:"workspace_id"`
	RequestedAction    string    `json:"requested_action"`
	IdempotencyKey     string    `json:"idempotency_key"`
	ExpectedGeneration *int64    `json:"expected_generation"`
	RequestedAt        time.Time `json:"requested_at"`
	EffectiveAt        time.Time `json:"effective_at"`
	ActorType          string    `json:"actor_type"`
	ActorID            string    `json:"actor_id"`
	AuthSource         string    `json:"auth_source"`
	ActorDisplay       string    `json:"actor_display"`
	Reason             string    `json:"reason"`
	PreviousState      string    `json:"previous_state"`
	ResultingState     string    `json:"resulting_state"`
	Generation         int64     `json:"generation"`
	Result             string    `json:"result"`
	ErrorClass         *string   `json:"error_class"`
	ResponseStatus     int32     `json:"response_status"`
}

type workspaceClaimIntakeLedgerTaskResponse struct {
	TaskID                string     `json:"task_id"`
	Status                string     `json:"status"`
	AgentID               string     `json:"agent_id"`
	RuntimeID             *string    `json:"runtime_id"`
	ConsumerID            *string    `json:"consumer_id"`
	DispatchedAt          *time.Time `json:"dispatched_at"`
	PrepareLeaseExpiresAt *time.Time `json:"prepare_lease_expires_at"`
	StaleReclaimable      bool       `json:"stale_reclaimable"`
	ClaimGeneration       *int64     `json:"claim_generation"`
	ClaimActionID         *string    `json:"claim_action_id"`
	FenceClassification   string     `json:"fence_classification"`
}

type workspaceClaimIntakeLedgerResponse struct {
	WorkspaceID  string                                   `json:"workspace_id"`
	State        string                                   `json:"state"`
	Generation   int64                                    `json:"generation"`
	LastActionID *string                                  `json:"last_action_id"`
	EffectiveAt  time.Time                                `json:"effective_at"`
	Counts       map[string]int64                         `json:"counts"`
	Tasks        []workspaceClaimIntakeLedgerTaskResponse `json:"tasks"`
	Limit        int32                                    `json:"limit"`
	Offset       int32                                    `json:"offset"`
}

func (h *Handler) GetWorkspaceClaimIntakeStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceClaimIntakeRole(w, r, false); !ok {
		return
	}

	control, err := h.Queries.GetWorkspaceClaimIntakeControl(r.Context(), workspaceUUID)
	if err != nil {
		slog.Warn("load workspace claim-intake control failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusServiceUnavailable, "claim-intake control state unavailable")
		return
	}
	writeJSON(w, http.StatusOK, workspaceClaimIntakeStatusFromDB(control))
}

func (h *Handler) PauseWorkspaceClaimIntake(w http.ResponseWriter, r *http.Request) {
	h.mutateWorkspaceClaimIntake(w, r, "pause")
}

func (h *Handler) ResumeWorkspaceClaimIntake(w http.ResponseWriter, r *http.Request) {
	h.mutateWorkspaceClaimIntake(w, r, "resume")
}

func (h *Handler) ListWorkspaceClaimIntakeActions(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceClaimIntakeRole(w, r, false); !ok {
		return
	}

	limit, offset, ok := claimIntakePagination(w, r)
	if !ok {
		return
	}
	actions, err := h.Queries.ListWorkspaceClaimIntakeActions(r.Context(), db.ListWorkspaceClaimIntakeActionsParams{
		WorkspaceID:  workspaceUUID,
		ResultOffset: offset,
		ResultLimit:  limit,
	})
	if err != nil {
		slog.Warn("list workspace claim-intake actions failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to list claim-intake actions")
		return
	}

	response := make([]workspaceClaimIntakeActionResponse, 0, len(actions))
	for _, action := range actions {
		response = append(response, workspaceClaimIntakeActionFromDB(action))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"actions": response,
		"limit":   limit,
		"offset":  offset,
	})
}

func (h *Handler) ListWorkspaceClaimIntakeLedger(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceClaimIntakeRole(w, r, false); !ok {
		return
	}

	limit, offset, ok := claimIntakePagination(w, r)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "claim-intake ledger unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	control, err := qtx.LockWorkspaceClaimIntakeControlForLedger(r.Context(), workspaceUUID)
	if err != nil {
		slog.Warn("load workspace claim-intake control for ledger failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusServiceUnavailable, "claim-intake control state unavailable")
		return
	}
	countRows, err := qtx.CountWorkspaceClaimIntakeLedgerByStatus(r.Context(), workspaceUUID)
	if err != nil {
		slog.Warn("count workspace claim-intake ledger failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusServiceUnavailable, "claim-intake ledger unavailable")
		return
	}
	counts := map[string]int64{
		"queued":                  0,
		"deferred":                0,
		"dispatched":              0,
		"running":                 0,
		"waiting_local_directory": 0,
	}
	for _, row := range countRows {
		counts[row.TaskStatus] = row.TaskCount
	}

	tasks, err := qtx.ListWorkspaceClaimIntakeLedger(r.Context(), db.ListWorkspaceClaimIntakeLedgerParams{
		CurrentGeneration: control.Generation,
		ResultOffset:      offset,
		ResultLimit:       limit,
		WorkspaceID:       workspaceUUID,
	})
	if err != nil {
		slog.Warn("list workspace claim-intake ledger failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusServiceUnavailable, "claim-intake ledger unavailable")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "claim-intake ledger unavailable")
		return
	}

	responseTasks := make([]workspaceClaimIntakeLedgerTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		responseTasks = append(responseTasks, workspaceClaimIntakeLedgerTaskFromDB(task))
	}
	writeJSON(w, http.StatusOK, workspaceClaimIntakeLedgerResponse{
		WorkspaceID:  workspaceID,
		State:        control.State,
		Generation:   control.Generation,
		LastActionID: uuidToPtr(control.AuthoritativeActionID),
		EffectiveAt:  control.EffectiveAt.Time,
		Counts:       counts,
		Tasks:        responseTasks,
		Limit:        limit,
		Offset:       offset,
	})
}

func (h *Handler) mutateWorkspaceClaimIntake(w http.ResponseWriter, r *http.Request, action string) {
	requestedAt := time.Now().UTC()
	workspaceID := workspaceIDFromURL(r, "id")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceClaimIntakeRole(w, r, true)
	if !ok {
		return
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}
	if len(idempotencyKey) > 200 {
		writeError(w, http.StatusBadRequest, "Idempotency-Key must be at most 200 characters")
		return
	}

	var request workspaceClaimIntakeMutationRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	if len(request.Reason) > 2000 {
		writeError(w, http.StatusBadRequest, "reason must be at most 2000 characters")
		return
	}
	if action == "resume" && request.ExpectedGeneration == nil {
		writeError(w, http.StatusBadRequest, "expected_generation is required for resume")
		return
	}
	if request.ExpectedGeneration != nil && *request.ExpectedGeneration < 0 {
		writeError(w, http.StatusBadRequest, "expected_generation must be non-negative")
		return
	}

	actorUser, err := h.Queries.GetUser(r.Context(), member.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve mutation actor")
		return
	}
	actorDisplay := strings.TrimSpace(actorUser.Name)
	if actorDisplay == "" {
		actorDisplay = actorUser.Email
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update claim-intake control")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	replay, err := qtx.GetWorkspaceClaimIntakeActionByIdempotencyKey(r.Context(), db.GetWorkspaceClaimIntakeActionByIdempotencyKeyParams{
		WorkspaceID:    workspaceUUID,
		IdempotencyKey: idempotencyKey,
	})
	if err == nil {
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to replay claim-intake action")
			return
		}
		writeStoredClaimIntakeResponse(w, int(replay.ResponseStatus), replay.ResponseBody)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check claim-intake action replay")
		return
	}

	control, err := qtx.LockWorkspaceClaimIntakeControlForMutation(r.Context(), workspaceUUID)
	if err != nil {
		slog.Warn("lock workspace claim-intake control failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusServiceUnavailable, "claim-intake control state unavailable")
		return
	}

	// Re-check after taking the same row lock that linearizes mutations against
	// claims. A concurrent request may have committed this key while we waited.
	replay, err = qtx.GetWorkspaceClaimIntakeActionByIdempotencyKey(r.Context(), db.GetWorkspaceClaimIntakeActionByIdempotencyKeyParams{
		WorkspaceID:    workspaceUUID,
		IdempotencyKey: idempotencyKey,
	})
	if err == nil {
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to replay claim-intake action")
			return
		}
		writeStoredClaimIntakeResponse(w, int(replay.ResponseStatus), replay.ResponseBody)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check claim-intake action replay")
		return
	}

	effectiveAt := time.Now().UTC()
	actionUUID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	actorID := uuidToString(member.UserID)
	authSource := r.Header.Get("X-Auth-Source")
	if authSource != "session" && authSource != "jwt" && authSource != "pat" {
		writeError(w, http.StatusForbidden, "claim-intake mutations require authenticated human provenance")
		return
	}
	responseStatus := http.StatusOK
	previousState := control.State
	resultingState := control.State
	generation := control.Generation
	result := "noop"
	var errorClass *string
	authoritativeActionID := uuidToPtr(control.AuthoritativeActionID)

	if action == "resume" && *request.ExpectedGeneration != control.Generation {
		responseStatus = http.StatusConflict
		result = "conflict"
		value := "stale_generation"
		errorClass = &value
	} else {
		desiredState := claimIntakeStatePaused
		if action == "resume" {
			desiredState = claimIntakeStateResumed
		}
		if control.State != desiredState {
			result = "applied"
			resultingState = desiredState
			generation = control.Generation + 1
			actionID := uuidToString(actionUUID)
			authoritativeActionID = &actionID
			if _, err := qtx.ApplyWorkspaceClaimIntakeControlMutation(r.Context(), db.ApplyWorkspaceClaimIntakeControlMutationParams{
				State:                 resultingState,
				Generation:            generation,
				UpdatedByType:         "member",
				UpdatedByID:           member.UserID,
				AuthSource:            authSource,
				ActorDisplay:          actorDisplay,
				Reason:                request.Reason,
				AuthoritativeActionID: actionUUID,
				EffectiveAt:           pgtype.Timestamptz{Time: effectiveAt, Valid: true},
				WorkspaceID:           workspaceUUID,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update claim-intake control")
				return
			}
		}
	}

	response := workspaceClaimIntakeMutationResponse{
		ActionID:        uuidToString(actionUUID),
		LastActionID:    authoritativeActionID,
		WorkspaceID:     workspaceID,
		RequestedAction: action,
		PreviousState:   previousState,
		State:           resultingState,
		Generation:      generation,
		ActorType:       "member",
		ActorID:         actorID,
		IdempotencyKey:  idempotencyKey,
		Reason:          request.Reason,
		RequestedAt:     requestedAt,
		EffectiveAt:     effectiveAt,
		Result:          result,
		ErrorClass:      errorClass,
	}
	responseBody, err := json.Marshal(response)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode claim-intake response")
		return
	}

	insertParams := db.InsertWorkspaceClaimIntakeActionParams{
		ID:                 actionUUID,
		WorkspaceID:        workspaceUUID,
		Action:             action,
		IdempotencyKey:     idempotencyKey,
		ExpectedGeneration: pgtype.Int8{},
		RequestedAt:        pgtype.Timestamptz{Time: requestedAt, Valid: true},
		EffectiveAt:        pgtype.Timestamptz{Time: effectiveAt, Valid: true},
		ActorType:          "member",
		ActorID:            member.UserID,
		AuthSource:         authSource,
		ActorDisplay:       actorDisplay,
		Reason:             request.Reason,
		PreviousState:      previousState,
		ResultingState:     resultingState,
		Generation:         generation,
		Result:             result,
		ErrorClass:         pgtype.Text{},
		ResponseStatus:     int32(responseStatus),
		ResponseBody:       responseBody,
	}
	if request.ExpectedGeneration != nil {
		insertParams.ExpectedGeneration = pgtype.Int8{Int64: *request.ExpectedGeneration, Valid: true}
	}
	if errorClass != nil {
		insertParams.ErrorClass = pgtype.Text{String: *errorClass, Valid: true}
	}
	if _, err := qtx.InsertWorkspaceClaimIntakeAction(r.Context(), insertParams); err != nil {
		if isUniqueViolation(err) {
			if err := tx.Rollback(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to replay claim-intake action")
				return
			}
			replay, replayErr := h.Queries.GetWorkspaceClaimIntakeActionByIdempotencyKey(r.Context(), db.GetWorkspaceClaimIntakeActionByIdempotencyKeyParams{
				WorkspaceID:    workspaceUUID,
				IdempotencyKey: idempotencyKey,
			})
			if replayErr == nil {
				writeStoredClaimIntakeResponse(w, int(replay.ResponseStatus), replay.ResponseBody)
				return
			}
		}
		writeError(w, http.StatusInternalServerError, "failed to audit claim-intake action")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit claim-intake action")
		return
	}
	writeStoredClaimIntakeResponse(w, responseStatus, responseBody)
}

func (h *Handler) requireWorkspaceClaimIntakeRole(w http.ResponseWriter, r *http.Request, mutation bool) (db.Member, bool) {
	workspaceID := workspaceIDFromURL(r, "id")
	if middleware.DaemonAuthPathFromContext(r.Context()) == middleware.DaemonAuthPathDaemonToken ||
		middleware.DaemonIDFromContext(r.Context()) != "" ||
		r.Header.Get("X-Actor-Source") != "" {
		operation := "claim-intake operator endpoints"
		if mutation {
			operation = "claim-intake mutations"
		}
		writeError(
			w,
			http.StatusForbidden,
			operation+" are only available to human workspace owners and admins",
		)
		return db.Member{}, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Member{}, false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Member{}, false
	}
	return member, true
}

func workspaceClaimIntakeStatusFromDB(control db.WorkspaceClaimIntakeControl) workspaceClaimIntakeStatusResponse {
	return workspaceClaimIntakeStatusResponse{
		WorkspaceID:   uuidToString(control.WorkspaceID),
		State:         control.State,
		Generation:    control.Generation,
		UpdatedByType: control.UpdatedByType,
		UpdatedByID:   uuidToPtr(control.UpdatedByID),
		AuthSource:    control.AuthSource,
		ActorDisplay:  control.ActorDisplay,
		Reason:        control.Reason,
		LastActionID:  uuidToPtr(control.AuthoritativeActionID),
		EffectiveAt:   control.EffectiveAt.Time,
		UpdatedAt:     control.UpdatedAt.Time,
	}
}

func workspaceClaimIntakeActionFromDB(action db.WorkspaceClaimIntakeAction) workspaceClaimIntakeActionResponse {
	return workspaceClaimIntakeActionResponse{
		ActionID:           uuidToString(action.ID),
		WorkspaceID:        uuidToString(action.WorkspaceID),
		RequestedAction:    action.Action,
		IdempotencyKey:     action.IdempotencyKey,
		ExpectedGeneration: int8ToPtr(action.ExpectedGeneration),
		RequestedAt:        action.RequestedAt.Time,
		EffectiveAt:        action.EffectiveAt.Time,
		ActorType:          action.ActorType,
		ActorID:            uuidToString(action.ActorID),
		AuthSource:         action.AuthSource,
		ActorDisplay:       action.ActorDisplay,
		Reason:             action.Reason,
		PreviousState:      action.PreviousState,
		ResultingState:     action.ResultingState,
		Generation:         action.Generation,
		Result:             action.Result,
		ErrorClass:         textToPtr(action.ErrorClass),
		ResponseStatus:     action.ResponseStatus,
	}
}

func workspaceClaimIntakeLedgerTaskFromDB(task db.ListWorkspaceClaimIntakeLedgerRow) workspaceClaimIntakeLedgerTaskResponse {
	var dispatchedAt *time.Time
	if task.DispatchedAt.Valid {
		value := task.DispatchedAt.Time
		dispatchedAt = &value
	}
	var prepareLeaseExpiresAt *time.Time
	if task.PrepareLeaseExpiresAt.Valid {
		value := task.PrepareLeaseExpiresAt.Time
		prepareLeaseExpiresAt = &value
	}
	return workspaceClaimIntakeLedgerTaskResponse{
		TaskID:                uuidToString(task.TaskID),
		Status:                task.TaskStatus,
		AgentID:               uuidToString(task.AgentID),
		RuntimeID:             uuidToPtr(task.RuntimeID),
		ConsumerID:            textToPtr(task.ClaimConsumerID),
		DispatchedAt:          dispatchedAt,
		PrepareLeaseExpiresAt: prepareLeaseExpiresAt,
		StaleReclaimable:      task.StaleReclaimable,
		ClaimGeneration:       int8ToPtr(task.ClaimIntakeGeneration),
		ClaimActionID:         uuidToPtr(task.ClaimIntakeActionID),
		FenceClassification:   task.FenceClassification,
	}
}

func writeStoredClaimIntakeResponse(w http.ResponseWriter, status int, body []byte) {
	wireBody := append([]byte(nil), body...)
	wireBody = append(wireBody, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(wireBody)))
	w.WriteHeader(status)
	_, _ = w.Write(wireBody)
}

func claimIntakePagination(w http.ResponseWriter, r *http.Request) (int32, int32, bool) {
	limit := int64(50)
	offset := int64(0)
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.ParseInt(value, 10, 32)
		if err != nil || limit < 1 || limit > 200 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return 0, 0, false
		}
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		offset, err = strconv.ParseInt(value, 10, 32)
		if err != nil || offset < 0 {
			writeError(w, http.StatusBadRequest, "offset must be non-negative")
			return 0, 0, false
		}
	}
	return int32(limit), int32(offset), true
}
