package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	focusModePomodoro   = "pomodoro"
	focusModeFlowtime   = "flowtime"
	focusModeQuickStart = "quick_start"

	focusPhaseIdle           = "idle"
	focusPhaseFocusing       = "focusing"
	focusPhasePaused         = "paused"
	focusPhaseBreakSuggested = "break_suggested"
	focusPhaseBreaking       = "breaking"
	focusPhaseAbandoned      = "abandoned"

	focusEventStarted             = "focus_started"
	focusEventPaused              = "focus_paused"
	focusEventResumed             = "focus_resumed"
	focusEventCompleted           = "focus_completed"
	focusEventAbandoned           = "focus_abandoned"
	focusEventBreakSuggested      = "break_suggested"
	focusEventBreakStarted        = "break_started"
	focusEventBreakSkipped        = "break_skipped"
	focusEventBreakCompleted      = "break_completed"
	focusEventQuickStartCompleted = "quick_start_completed"
)

type focusSessionResponse struct {
	ID                    *string  `json:"id,omitempty"`
	WorkspaceID           string   `json:"workspace_id,omitempty"`
	UserID                string   `json:"user_id,omitempty"`
	Mode                  string   `json:"mode"`
	Phase                 string   `json:"phase"`
	Preset                *string  `json:"preset,omitempty"`
	IssueID               *string  `json:"issue_id,omitempty"`
	Description           *string  `json:"description,omitempty"`
	CommitmentText        *string  `json:"commitment_text,omitempty"`
	LabelIDs              []string `json:"label_ids"`
	FirstStartedAt        *string  `json:"first_started_at,omitempty"`
	StartedAt             *string  `json:"started_at,omitempty"`
	PausedAt              *string  `json:"paused_at,omitempty"`
	ElapsedFocusSeconds   int32    `json:"elapsed_focus_seconds"`
	SuggestedBreakSeconds *int32   `json:"suggested_break_seconds,omitempty"`
	StatusReason          *string  `json:"status_reason,omitempty"`
	ReasonNote            *string  `json:"reason_note,omitempty"`
	CreatedAt             *string  `json:"created_at,omitempty"`
	UpdatedAt             *string  `json:"updated_at,omitempty"`
}

type focusEventResponse struct {
	ID              string         `json:"id"`
	WorkspaceID     string         `json:"workspace_id"`
	UserID          string         `json:"user_id"`
	FocusSessionID  *string        `json:"focus_session_id,omitempty"`
	EventType       string         `json:"event_type"`
	Reason          *string        `json:"reason,omitempty"`
	Note            *string        `json:"note,omitempty"`
	DurationSeconds *int32         `json:"duration_seconds,omitempty"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       string         `json:"created_at"`
}

type focusStartRequest struct {
	Mode                string   `json:"mode"`
	Preset              *string  `json:"preset"`
	IssueID             *string  `json:"issue_id"`
	Description         *string  `json:"description"`
	CommitmentText      *string  `json:"commitment_text"`
	LabelIDs            []string `json:"label_ids"`
	TimerConflictAction string   `json:"timer_conflict_action"`
	ResistanceReason    *string  `json:"resistance_reason"`
	ResistanceNote      *string  `json:"resistance_note"`
}

type focusUpdateRequest struct {
	IssueID        *string  `json:"issue_id"`
	Description    *string  `json:"description"`
	CommitmentText *string  `json:"commitment_text"`
	LabelIDs       []string `json:"label_ids"`
}

type focusReasonRequest struct {
	Reason *string `json:"reason"`
	Note   *string `json:"note"`
}

type focusCompleteRequest struct {
	Note      *string `json:"note"`
	EndReason *string `json:"end_reason"`
}

func defaultFocusSessionResponse(workspaceID, userID string) focusSessionResponse {
	return focusSessionResponse{
		WorkspaceID:         workspaceID,
		UserID:              userID,
		Mode:                focusModeFlowtime,
		Phase:               focusPhaseIdle,
		LabelIDs:            []string{},
		ElapsedFocusSeconds: 0,
	}
}

func decodeLabelIDs(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var labels []string
	if err := json.Unmarshal(raw, &labels); err != nil {
		return []string{}
	}
	return labels
}

func encodeLabelIDs(labelIDs []string) []byte {
	if labelIDs == nil {
		labelIDs = []string{}
	}
	raw, err := json.Marshal(labelIDs)
	if err != nil {
		return []byte("[]")
	}
	return raw
}

func encodeMetadata(metadata map[string]any) []byte {
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func decodeMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return map[string]any{}
	}
	return metadata
}

func int4ToPtr(value pgtype.Int4) *int32 {
	if !value.Valid {
		return nil
	}
	return &value.Int32
}

func timestampToOptionalString(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	formatted := value.Time.UTC().Format(time.RFC3339)
	return &formatted
}

func focusSessionToResponse(session db.FocusSession) focusSessionResponse {
	id := uuidToString(session.ID)
	return focusSessionResponse{
		ID:                    &id,
		WorkspaceID:           uuidToString(session.WorkspaceID),
		UserID:                uuidToString(session.UserID),
		Mode:                  session.Mode,
		Phase:                 session.Phase,
		Preset:                textToPtr(session.Preset),
		IssueID:               uuidToPtr(session.IssueID),
		Description:           textToPtr(session.Description),
		CommitmentText:        textToPtr(session.CommitmentText),
		LabelIDs:              decodeLabelIDs(session.LabelIds),
		FirstStartedAt:        timestampToOptionalString(session.FirstStartedAt),
		StartedAt:             timestampToOptionalString(session.StartedAt),
		PausedAt:              timestampToOptionalString(session.PausedAt),
		ElapsedFocusSeconds:   session.ElapsedFocusSeconds,
		SuggestedBreakSeconds: int4ToPtr(session.SuggestedBreakSeconds),
		StatusReason:          textToPtr(session.StatusReason),
		ReasonNote:            textToPtr(session.ReasonNote),
		CreatedAt:             timestampToOptionalString(session.CreatedAt),
		UpdatedAt:             timestampToOptionalString(session.UpdatedAt),
	}
}

func focusEventToResponse(event db.FocusEvent) focusEventResponse {
	return focusEventResponse{
		ID:              uuidToString(event.ID),
		WorkspaceID:     uuidToString(event.WorkspaceID),
		UserID:          uuidToString(event.UserID),
		FocusSessionID:  uuidToPtr(event.FocusSessionID),
		EventType:       event.EventType,
		Reason:          textToPtr(event.Reason),
		Note:            textToPtr(event.Note),
		DurationSeconds: int4ToPtr(event.DurationSeconds),
		Metadata:        decodeMetadata(event.Metadata),
		CreatedAt:       timestampToString(event.CreatedAt),
	}
}

func validFocusMode(mode string) bool {
	switch mode {
	case focusModePomodoro, focusModeFlowtime, focusModeQuickStart:
		return true
	default:
		return false
	}
}

func validFocusReason(reason *string) bool {
	if reason == nil || *reason == "" {
		return true
	}
	switch *reason {
	case "unclear_next_step", "too_large", "low_energy", "avoidance", "interruption", "blocked", "urgent_work", "not_needed", "other":
		return true
	default:
		return false
	}
}

func focusElapsedSeconds(session db.FocusSession, now time.Time) int32 {
	elapsed := session.ElapsedFocusSeconds
	if session.Phase == focusPhaseFocusing && session.StartedAt.Valid {
		running := int32(math.Round(now.Sub(session.StartedAt.Time).Seconds()))
		if running > 0 {
			elapsed += running
		}
	}
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func suggestedBreakSeconds(focusSeconds int32) int32 {
	if focusSeconds < 25*60 {
		return 5 * 60
	}
	if focusSeconds <= 50*60 {
		return 10 * 60
	}
	return 15 * 60
}

func focusSessionUpdateParams(session db.FocusSession) db.UpdateFocusSessionParams {
	return db.UpdateFocusSessionParams{
		ID:                    session.ID,
		UserID:                session.UserID,
		WorkspaceID:           session.WorkspaceID,
		Mode:                  session.Mode,
		Phase:                 session.Phase,
		Preset:                session.Preset,
		IssueID:               session.IssueID,
		Description:           session.Description,
		CommitmentText:        session.CommitmentText,
		LabelIds:              session.LabelIds,
		FirstStartedAt:        session.FirstStartedAt,
		StartedAt:             session.StartedAt,
		PausedAt:              session.PausedAt,
		ElapsedFocusSeconds:   session.ElapsedFocusSeconds,
		SuggestedBreakSeconds: session.SuggestedBreakSeconds,
		StatusReason:          session.StatusReason,
		ReasonNote:            session.ReasonNote,
	}
}

func (h *Handler) resolveFocusRequest(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return "", "", false
	}
	return userID, workspaceID, true
}

func (h *Handler) GetCurrentFocus(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.resolveFocusRequest(w, r)
	if !ok {
		return
	}

	session, err := h.Queries.GetFocusSession(r.Context(), db.GetFocusSessionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"session": defaultFocusSessionResponse(workspaceID, userID)})
			return
		}
		slog.Warn("get focus session failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get focus session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"session": focusSessionToResponse(session)})
}

func (h *Handler) StartFocus(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.resolveFocusRequest(w, r)
	if !ok {
		return
	}
	var req focusStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Mode == "" {
		req.Mode = focusModeFlowtime
	}
	if !validFocusMode(req.Mode) {
		writeError(w, http.StatusBadRequest, "invalid_focus_mode")
		return
	}
	if !validFocusReason(req.ResistanceReason) {
		writeError(w, http.StatusBadRequest, "invalid_focus_reason")
		return
	}
	issueID := parseOptionalUUID(req.IssueID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Warn("begin focus start transaction failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start focus")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	existingTimer, timerErr := qtx.GetRunningTimerByUser(r.Context(), db.GetRunningTimerByUserParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if timerErr != nil && !errors.Is(timerErr, pgx.ErrNoRows) {
		slog.Warn("check running timer for focus failed", append(logger.RequestAttrs(r), "error", timerErr)...)
		writeError(w, http.StatusInternalServerError, "failed to start focus")
		return
	}
	if timerErr == nil {
		if req.TimerConflictAction == "" || req.TimerConflictAction == "cancel" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "timer_conflict", "code": "timer_conflict"})
			return
		}
		if req.TimerConflictAction != "stop_existing" && req.TimerConflictAction != "convert_existing" {
			writeError(w, http.StatusBadRequest, "invalid_timer_conflict_action")
			return
		}
		stopTime := time.Now()
		elapsed := int64(stopTime.Sub(existingTimer.StartTime.Time).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		if _, err := qtx.StopTimeEntry(r.Context(), db.StopTimeEntryParams{
			ID:              existingTimer.ID,
			WorkspaceID:     existingTimer.WorkspaceID,
			StopTime:        pgTimestamp(stopTime),
			DurationSeconds: elapsed,
		}); err != nil {
			slog.Warn("stop running timer for focus failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to start focus")
			return
		}
		if err := qtx.ClearRunningTimerByUser(r.Context(), parseUUID(userID)); err != nil {
			slog.Warn("clear running timer for focus failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to start focus")
			return
		}
	}

	session, err := qtx.UpsertFocusStart(r.Context(), db.UpsertFocusStartParams{
		UserID:         parseUUID(userID),
		WorkspaceID:    parseUUID(workspaceID),
		Mode:           req.Mode,
		Preset:         ptrToText(req.Preset),
		IssueID:        issueID,
		Description:    ptrToText(req.Description),
		CommitmentText: ptrToText(req.CommitmentText),
		LabelIds:       encodeLabelIDs(req.LabelIDs),
		StatusReason:   ptrToText(req.ResistanceReason),
		ReasonNote:     ptrToText(req.ResistanceNote),
	})
	if err != nil {
		slog.Warn("upsert focus start failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start focus")
		return
	}
	event, err := qtx.CreateFocusEvent(r.Context(), db.CreateFocusEventParams{
		WorkspaceID:    parseUUID(workspaceID),
		UserID:         parseUUID(userID),
		FocusSessionID: session.ID,
		EventType:      focusEventStarted,
		Reason:         ptrToText(req.ResistanceReason),
		Note:           ptrToText(req.ResistanceNote),
		Metadata:       encodeMetadata(map[string]any{"mode": req.Mode, "preset": req.Preset}),
	})
	if err != nil {
		slog.Warn("create focus started event failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start focus")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit focus start failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start focus")
		return
	}
	h.publish(protocol.EventFocusUpdated, workspaceID, "member", userID, map[string]any{"session": focusSessionToResponse(session)})
	writeJSON(w, http.StatusOK, map[string]any{"session": focusSessionToResponse(session), "event": focusEventToResponse(event)})
}

func (h *Handler) UpdateCurrentFocus(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.resolveFocusRequest(w, r)
	if !ok {
		return
	}
	var req focusUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	session, err := h.Queries.GetFocusSession(r.Context(), db.GetFocusSessionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "focus_session_not_found")
		return
	}
	params := focusSessionUpdateParams(session)
	params.IssueID = parseOptionalUUID(req.IssueID)
	params.Description = ptrToText(req.Description)
	params.CommitmentText = ptrToText(req.CommitmentText)
	if req.LabelIDs != nil {
		params.LabelIds = encodeLabelIDs(req.LabelIDs)
	}
	updated, err := h.Queries.UpdateFocusSession(r.Context(), params)
	if err != nil {
		slog.Warn("update focus session failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update focus")
		return
	}
	h.publish(protocol.EventFocusUpdated, workspaceID, "member", userID, map[string]any{"session": focusSessionToResponse(updated)})
	writeJSON(w, http.StatusOK, map[string]any{"session": focusSessionToResponse(updated)})
}

func (h *Handler) PauseFocus(w http.ResponseWriter, r *http.Request) {
	h.transitionFocusWithReason(w, r, focusPhaseFocusing, focusPhasePaused, focusEventPaused)
}

func (h *Handler) ResumeFocus(w http.ResponseWriter, r *http.Request) {
	h.transitionFocusWithReason(w, r, focusPhasePaused, focusPhaseFocusing, focusEventResumed)
}

func (h *Handler) CompleteQuickStart(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.resolveFocusRequest(w, r)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete quick start")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	session, err := qtx.GetFocusSession(r.Context(), db.GetFocusSessionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "focus_session_not_found")
		return
	}
	if session.Mode != focusModeQuickStart || session.Phase != focusPhaseFocusing {
		writeError(w, http.StatusBadRequest, "invalid_focus_phase")
		return
	}

	now := time.Now()
	elapsed := focusElapsedSeconds(session, now)
	params := focusSessionUpdateParams(session)
	params.Mode = focusModeFlowtime
	params.Preset = pgtype.Text{String: "flowtime_default", Valid: true}
	params.StartedAt = pgTimestamp(now)
	params.ElapsedFocusSeconds = elapsed
	updated, err := qtx.UpdateFocusSession(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete quick start")
		return
	}
	event, err := qtx.CreateFocusEvent(r.Context(), db.CreateFocusEventParams{
		WorkspaceID:     parseUUID(workspaceID),
		UserID:          parseUUID(userID),
		FocusSessionID:  session.ID,
		EventType:       focusEventQuickStartCompleted,
		DurationSeconds: pgtype.Int4{Int32: elapsed, Valid: true},
		Metadata:        encodeMetadata(map[string]any{"next_mode": focusModeFlowtime}),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete quick start")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete quick start")
		return
	}
	h.publish(protocol.EventFocusUpdated, workspaceID, "member", userID, map[string]any{"session": focusSessionToResponse(updated)})
	writeJSON(w, http.StatusOK, map[string]any{"session": focusSessionToResponse(updated), "event": focusEventToResponse(event)})
}

func (h *Handler) AbandonFocus(w http.ResponseWriter, r *http.Request) {
	h.transitionFocusWithReason(w, r, "", focusPhaseAbandoned, focusEventAbandoned)
}

func (h *Handler) transitionFocusWithReason(w http.ResponseWriter, r *http.Request, requiredPhase string, targetPhase string, eventType string) {
	userID, workspaceID, ok := h.resolveFocusRequest(w, r)
	if !ok {
		return
	}
	var req focusReasonRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if !validFocusReason(req.Reason) {
		writeError(w, http.StatusBadRequest, "invalid_focus_reason")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update focus")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	session, err := qtx.GetFocusSession(r.Context(), db.GetFocusSessionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "focus_session_not_found")
		return
	}
	if requiredPhase != "" && session.Phase != requiredPhase {
		writeError(w, http.StatusBadRequest, "invalid_focus_phase")
		return
	}
	now := time.Now()
	params := focusSessionUpdateParams(session)
	params.Phase = targetPhase
	params.StatusReason = ptrToText(req.Reason)
	params.ReasonNote = ptrToText(req.Note)
	if eventType == focusEventPaused || eventType == focusEventAbandoned {
		params.ElapsedFocusSeconds = focusElapsedSeconds(session, now)
		params.StartedAt = pgtype.Timestamptz{}
		params.PausedAt = pgTimestamp(now)
	}
	if eventType == focusEventResumed {
		params.StartedAt = pgTimestamp(now)
		params.PausedAt = pgtype.Timestamptz{}
	}
	updated, err := qtx.UpdateFocusSession(r.Context(), params)
	if err != nil {
		slog.Warn("transition focus failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update focus")
		return
	}
	event, err := qtx.CreateFocusEvent(r.Context(), db.CreateFocusEventParams{
		WorkspaceID:    parseUUID(workspaceID),
		UserID:         parseUUID(userID),
		FocusSessionID: session.ID,
		EventType:      eventType,
		Reason:         ptrToText(req.Reason),
		Note:           ptrToText(req.Note),
		Metadata:       encodeMetadata(map[string]any{"phase": targetPhase}),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update focus")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update focus")
		return
	}
	h.publish(protocol.EventFocusUpdated, workspaceID, "member", userID, map[string]any{"session": focusSessionToResponse(updated)})
	writeJSON(w, http.StatusOK, map[string]any{"session": focusSessionToResponse(updated), "event": focusEventToResponse(event)})
}

func (h *Handler) CompleteFocus(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.resolveFocusRequest(w, r)
	if !ok {
		return
	}
	var req focusCompleteRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete focus")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	session, err := qtx.GetFocusSession(r.Context(), db.GetFocusSessionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "focus_session_not_found")
		return
	}
	if session.Phase != focusPhaseFocusing && session.Phase != focusPhasePaused {
		writeError(w, http.StatusBadRequest, "invalid_focus_phase")
		return
	}

	now := time.Now()
	elapsed := focusElapsedSeconds(session, now)
	breakSeconds := suggestedBreakSeconds(elapsed)
	description := textToPtr(session.Description)
	if req.Note != nil {
		description = req.Note
	} else if description == nil {
		description = textToPtr(session.CommitmentText)
	}
	firstStarted := session.FirstStartedAt
	if !firstStarted.Valid {
		firstStarted = pgTimestamp(now.Add(-time.Duration(elapsed) * time.Second))
	}
	entryType := session.Mode
	if entryType == focusModeQuickStart {
		entryType = "flowtime"
	}
	entry, err := qtx.CreateTimeEntry(r.Context(), db.CreateTimeEntryParams{
		WorkspaceID:     parseUUID(workspaceID),
		UserID:          parseUUID(userID),
		IssueID:         session.IssueID,
		Description:     ptrToText(description),
		StartTime:       firstStarted,
		StopTime:        pgTimestamp(now),
		DurationSeconds: int64(elapsed),
		Type:            entryType,
	})
	if err != nil {
		slog.Warn("create focus time entry failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete focus")
		return
	}
	if err := h.replaceTimeEntryLabelsWithQueries(r.Context(), qtx, entry, decodeLabelIDs(session.LabelIds)); err != nil {
		statusCode, message := timeEntryLabelMutationErrorResponse(err)
		writeError(w, statusCode, message)
		return
	}
	params := focusSessionUpdateParams(session)
	params.Phase = focusPhaseBreakSuggested
	params.StartedAt = pgtype.Timestamptz{}
	params.PausedAt = pgtype.Timestamptz{}
	params.ElapsedFocusSeconds = elapsed
	params.SuggestedBreakSeconds = pgtype.Int4{Int32: breakSeconds, Valid: true}
	params.StatusReason = ptrToText(req.EndReason)
	params.ReasonNote = ptrToText(req.Note)
	updated, err := qtx.UpdateFocusSession(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete focus")
		return
	}
	completedEvent, err := qtx.CreateFocusEvent(r.Context(), db.CreateFocusEventParams{
		WorkspaceID:     parseUUID(workspaceID),
		UserID:          parseUUID(userID),
		FocusSessionID:  session.ID,
		EventType:       focusEventCompleted,
		Reason:          ptrToText(req.EndReason),
		Note:            ptrToText(req.Note),
		DurationSeconds: pgtype.Int4{Int32: elapsed, Valid: true},
		Metadata:        encodeMetadata(map[string]any{"time_entry_id": uuidToString(entry.ID), "mode": session.Mode}),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete focus")
		return
	}
	if _, err := qtx.CreateFocusEvent(r.Context(), db.CreateFocusEventParams{
		WorkspaceID:     parseUUID(workspaceID),
		UserID:          parseUUID(userID),
		FocusSessionID:  session.ID,
		EventType:       focusEventBreakSuggested,
		DurationSeconds: pgtype.Int4{Int32: breakSeconds, Valid: true},
		Metadata:        encodeMetadata(map[string]any{"suggested_break_seconds": breakSeconds, "focus_duration_seconds": elapsed}),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete focus")
		return
	}
	entryResp, buildErr := h.buildTimeEntryResponseWithQueries(r.Context(), qtx, entry)
	if buildErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete focus")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete focus")
		return
	}
	h.publish(protocol.EventFocusUpdated, workspaceID, "member", userID, map[string]any{"session": focusSessionToResponse(updated)})
	h.publish(protocol.EventTimeEntryStopped, workspaceID, "member", userID, map[string]any{"time_entry": entryResp})
	writeJSON(w, http.StatusOK, map[string]any{
		"session":     focusSessionToResponse(updated),
		"time_entry":  entryResp,
		"event":       focusEventToResponse(completedEvent),
		"next_action": "start_break",
	})
}

func (h *Handler) StartFocusBreak(w http.ResponseWriter, r *http.Request) {
	h.transitionFocusBreak(w, r, focusPhaseBreakSuggested, focusPhaseBreaking, focusEventBreakStarted, nil)
}

func (h *Handler) SkipFocusBreak(w http.ResponseWriter, r *http.Request) {
	var req focusReasonRequest
	h.transitionFocusBreak(w, r, focusPhaseBreakSuggested, focusPhaseIdle, focusEventBreakSkipped, &req)
}

func (h *Handler) CompleteFocusBreak(w http.ResponseWriter, r *http.Request) {
	h.transitionFocusBreak(w, r, focusPhaseBreaking, focusPhaseIdle, focusEventBreakCompleted, nil)
}

func (h *Handler) transitionFocusBreak(w http.ResponseWriter, r *http.Request, requiredPhase string, targetPhase string, eventType string, reasonReq *focusReasonRequest) {
	userID, workspaceID, ok := h.resolveFocusRequest(w, r)
	if !ok {
		return
	}
	if reasonReq != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(reasonReq); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if !validFocusReason(reasonReq.Reason) {
			writeError(w, http.StatusBadRequest, "invalid_focus_reason")
			return
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update focus break")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	session, err := qtx.GetFocusSession(r.Context(), db.GetFocusSessionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "focus_session_not_found")
		return
	}
	if session.Phase != requiredPhase {
		writeError(w, http.StatusBadRequest, "invalid_focus_phase")
		return
	}
	now := time.Now()
	params := focusSessionUpdateParams(session)
	params.Phase = targetPhase
	var duration *int32
	if eventType == focusEventBreakStarted {
		params.StartedAt = pgTimestamp(now)
	} else {
		params.StartedAt = pgtype.Timestamptz{}
		if eventType == focusEventBreakCompleted && session.StartedAt.Valid {
			breakDuration := int32(math.Round(now.Sub(session.StartedAt.Time).Seconds()))
			if breakDuration < 0 {
				breakDuration = 0
			}
			duration = &breakDuration
		}
	}
	updated, err := qtx.UpdateFocusSession(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update focus break")
		return
	}
	var reason *string
	var note *string
	if reasonReq != nil {
		reason = reasonReq.Reason
		note = reasonReq.Note
	}
	event, err := qtx.CreateFocusEvent(r.Context(), db.CreateFocusEventParams{
		WorkspaceID:    parseUUID(workspaceID),
		UserID:         parseUUID(userID),
		FocusSessionID: session.ID,
		EventType:      eventType,
		Reason:         ptrToText(reason),
		Note:           ptrToText(note),
		DurationSeconds: func() pgtype.Int4 {
			if duration == nil {
				return pgtype.Int4{}
			}
			return pgtype.Int4{Int32: *duration, Valid: true}
		}(),
		Metadata: encodeMetadata(map[string]any{"suggested_break_seconds": int4ToPtr(session.SuggestedBreakSeconds)}),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update focus break")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update focus break")
		return
	}
	h.publish(protocol.EventFocusUpdated, workspaceID, "member", userID, map[string]any{"session": focusSessionToResponse(updated)})
	writeJSON(w, http.StatusOK, map[string]any{"session": focusSessionToResponse(updated), "event": focusEventToResponse(event)})
}

func (h *Handler) ListFocusEvents(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := h.resolveFocusRequest(w, r)
	if !ok {
		return
	}
	session, err := h.Queries.GetFocusSession(r.Context(), db.GetFocusSessionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "focus_session_not_found")
		return
	}
	events, err := h.Queries.ListFocusEventsBySession(r.Context(), db.ListFocusEventsBySessionParams{
		WorkspaceID:    parseUUID(workspaceID),
		UserID:         parseUUID(userID),
		FocusSessionID: session.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list focus events")
		return
	}
	resp := make([]focusEventResponse, len(events))
	for index, event := range events {
		resp[index] = focusEventToResponse(event)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": resp})
}
