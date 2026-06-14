package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PomodoroHandler handles Pomodoro session persistence endpoints.
// One session per (user, workspace) pair stored in the pomodoro_sessions table.
type PomodoroHandler struct {
	queries *db.Queries
}

// NewPomodoroHandler constructs a PomodoroHandler with the given queries.
func NewPomodoroHandler(queries *db.Queries) *PomodoroHandler {
	return &PomodoroHandler{queries: queries}
}

// PomodoroSessionResponse is the JSON shape returned to clients.
type PomodoroSessionResponse struct {
	ID                   *string `json:"id,omitempty"`
	Phase                string  `json:"phase"`
	PhaseDurationSeconds int32   `json:"phase_duration_seconds"`
	Status               string  `json:"status"`
	ElapsedSeconds       int32   `json:"elapsed_seconds"`
	StartedAt            *string `json:"started_at,omitempty"`
}

// sessionToResponse converts a db.PomodoroSession into the public response shape.
func sessionToResponse(s db.PomodoroSession) PomodoroSessionResponse {
	id := uuidToString(s.ID)
	resp := PomodoroSessionResponse{
		ID:                   &id,
		Phase:                s.Phase,
		PhaseDurationSeconds: s.PhaseDurationSeconds,
		Status:               s.Status,
		ElapsedSeconds:       s.ElapsedSeconds,
	}
	if s.StartedAt.Valid {
		ts := s.StartedAt.Time.UTC().Format(time.RFC3339)
		resp.StartedAt = &ts
	}
	return resp
}

// defaultIdleSession returns the default idle Pomodoro state when no session exists.
func defaultIdleSession() PomodoroSessionResponse {
	return PomodoroSessionResponse{
		Phase:                "work",
		PhaseDurationSeconds: 1500,
		Status:               "idle",
		ElapsedSeconds:       0,
	}
}

// resolvePomodoroPrincipal reads user_id and workspace_id from request headers.
// Returns (userUUID, workspaceUUID, rawWorkspaceID, ok).
func resolvePomodoroPrincipal(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	userIDStr, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	workspaceIDStr := resolveWorkspaceID(r)
	if workspaceIDStr == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return parseUUID(userIDStr), parseUUID(workspaceIDStr), true
}

// GetCurrentPomodoro handles GET /api/pomodoro/current.
// Returns the persisted session or a default idle session if none exists.
func (h *PomodoroHandler) GetCurrentPomodoro(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := resolvePomodoroPrincipal(w, r)
	if !ok {
		return
	}

	session, err := h.queries.GetPomodoroSession(r.Context(), db.GetPomodoroSessionParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No session yet — return the default idle state.
			writeJSON(w, http.StatusOK, defaultIdleSession())
			return
		}
		slog.Warn("get pomodoro session failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get pomodoro session")
		return
	}

	writeJSON(w, http.StatusOK, sessionToResponse(session))
}

// StartPomodoro handles POST /api/pomodoro/start.
// If no session exists, inserts a fresh running work session.
// If a session already exists (paused or idle), resumes it by setting status='running' and started_at=NOW().
func (h *PomodoroHandler) StartPomodoro(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := resolvePomodoroPrincipal(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	// Try to fetch an existing session first.
	existing, err := h.queries.GetPomodoroSession(ctx, db.GetPomodoroSessionParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})

	var session db.PomodoroSession

	if errors.Is(err, pgx.ErrNoRows) {
		// No session — insert fresh work session via upsert.
		session, err = h.queries.UpsertPomodoroStart(ctx, db.UpsertPomodoroStartParams{
			UserID:      userID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			slog.Warn("upsert pomodoro start failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to start pomodoro")
			return
		}
	} else if err != nil {
		slog.Warn("get pomodoro session for start failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start pomodoro")
		return
	} else {
		// Resume existing session: keep phase/duration/elapsed, set status='running', started_at=NOW().
		session, err = h.queries.UpdatePomodoroSession(ctx, db.UpdatePomodoroSessionParams{
			UserID:               userID,
			WorkspaceID:          workspaceID,
			Phase:                existing.Phase,
			PhaseDurationSeconds: existing.PhaseDurationSeconds,
			Status:               "running",
			ElapsedSeconds:       existing.ElapsedSeconds,
			StartedAt:            pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
		if err != nil {
			slog.Warn("update pomodoro session for resume failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to resume pomodoro")
			return
		}
	}

	writeJSON(w, http.StatusOK, sessionToResponse(session))
}

// PausePomodoro handles POST /api/pomodoro/pause.
// Calculates elapsed time since started_at, accumulates it, and sets status='paused'.
func (h *PomodoroHandler) PausePomodoro(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := resolvePomodoroPrincipal(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	existing, err := h.queries.GetPomodoroSession(ctx, db.GetPomodoroSessionParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no active pomodoro session")
			return
		}
		slog.Warn("get pomodoro session for pause failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to pause pomodoro")
		return
	}

	// Accumulate running elapsed time since started_at.
	newElapsed := existing.ElapsedSeconds
	if existing.StartedAt.Valid && existing.Status == "running" {
		runningSeconds := int32(math.Round(time.Since(existing.StartedAt.Time).Seconds()))
		newElapsed += runningSeconds
	}

	session, err := h.queries.UpdatePomodoroSession(ctx, db.UpdatePomodoroSessionParams{
		UserID:               userID,
		WorkspaceID:          workspaceID,
		Phase:                existing.Phase,
		PhaseDurationSeconds: existing.PhaseDurationSeconds,
		Status:               "paused",
		ElapsedSeconds:       newElapsed,
		StartedAt:            pgtype.Timestamptz{Valid: false}, // NULL — not running
	})
	if err != nil {
		slog.Warn("update pomodoro session for pause failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to pause pomodoro")
		return
	}

	writeJSON(w, http.StatusOK, sessionToResponse(session))
}

// completePomodoroRequest is the optional JSON body for POST /api/pomodoro/complete.
type completePomodoroRequest struct {
	IssueID        *string  `json:"issue_id,omitempty"`
	Note           *string  `json:"note,omitempty"`
	LabelIDs       []string `json:"label_ids,omitempty"`
	LongBreakAfter int      `json:"long_break_after"` // how many pomodoros before a long break; default 4
}

// CompletePomodoro handles POST /api/pomodoro/complete.
// If phase='work', creates a time_entry of type='pomodoro' for the work duration,
// increments the pomodoro count, and decides whether the next break is short or long.
// Then transitions the session to the next phase with elapsed reset.
func (h *PomodoroHandler) CompletePomodoro(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceIDStr := resolveWorkspaceID(r)
	if workspaceIDStr == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID := parseUUID(userIDStr)
	workspaceID := parseUUID(workspaceIDStr)
	ctx := r.Context()

	// Parse optional request body.
	var req completePomodoroRequest
	if r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.LongBreakAfter <= 0 {
		req.LongBreakAfter = 4 // default: long break every 4 pomodoros
	}

	existing, err := h.queries.GetPomodoroSession(ctx, db.GetPomodoroSessionParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no active pomodoro session")
			return
		}
		slog.Warn("get pomodoro session for complete failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete pomodoro")
		return
	}

	// Determine the next phase (default for non-work phases).
	nextPhase := "break"
	nextDuration := int32(300) // 5-minute short break

	if existing.Phase == "work" && existing.StartedAt.Valid {
		// Build description from optional note.
		desc := "Pomodoro 专注"
		if req.Note != nil && *req.Note != "" {
			desc = *req.Note
		}

		// Build issue_id: use provided value if present.
		issueID := pgtype.UUID{}
		if req.IssueID != nil && *req.IssueID != "" {
			issueID = parseUUID(*req.IssueID)
		}

		stopTime := pgtype.Timestamptz{Time: time.Now(), Valid: true}
		entry, teErr := h.queries.CreateTimeEntry(ctx, db.CreateTimeEntryParams{
			WorkspaceID:     workspaceID,
			UserID:          userID,
			IssueID:         issueID,
			PlanItemID:      pgtype.UUID{},
			Description:     pgtype.Text{String: desc, Valid: true},
			StartTime:       existing.StartedAt,
			StopTime:        stopTime,
			DurationSeconds: int64(existing.PhaseDurationSeconds),
			Type:            "pomodoro",
		})
		if teErr != nil {
			// Non-fatal — log but continue with phase transition.
			slog.Warn("pomodoro complete: failed to create time entry", append(logger.RequestAttrs(r), "error", teErr)...)
		} else {
			// Best-effort label attachment for the generated pomodoro time entry.
			for _, labelID := range req.LabelIDs {
				trimmedID := strings.TrimSpace(labelID)
				if trimmedID == "" {
					continue
				}

				labelUUID := parseUUID(trimmedID)
				if !labelUUID.Valid {
					continue
				}
				if _, err := h.queries.GetTimeEntryLabelInWorkspace(ctx, db.GetTimeEntryLabelInWorkspaceParams{
					ID:          labelUUID,
					WorkspaceID: workspaceID,
				}); err != nil {
					continue
				}
				if err := h.queries.AddTimeEntryLabel(ctx, db.AddTimeEntryLabelParams{
					TimeEntryID: entry.ID,
					LabelID:     labelUUID,
				}); err != nil {
					slog.Warn("pomodoro complete: failed to add label", append(logger.RequestAttrs(r), "error", err, "label_id", trimmedID)...)
				}
			}
		}

		// Increment the pomodoro count and decide next break length.
		newCount, countErr := h.queries.IncrementPomodoroCount(ctx, db.IncrementPomodoroCountParams{
			UserID:      userID,
			WorkspaceID: workspaceID,
		})
		if countErr != nil {
			slog.Warn("pomodoro complete: failed to increment count", append(logger.RequestAttrs(r), "error", countErr)...)
		} else if int(newCount)%req.LongBreakAfter == 0 {
			// Every Nth pomodoro triggers a long break (15 minutes).
			nextPhase = "long_break"
			nextDuration = 900
		}
	} else if existing.Phase == "break" || existing.Phase == "long_break" {
		// After any break, return to work.
		nextPhase = "work"
		nextDuration = 1500
	}

	session, err := h.queries.UpdatePomodoroSession(ctx, db.UpdatePomodoroSessionParams{
		UserID:               userID,
		WorkspaceID:          workspaceID,
		Phase:                nextPhase,
		PhaseDurationSeconds: nextDuration,
		Status:               "idle",
		ElapsedSeconds:       0,
		StartedAt:            pgtype.Timestamptz{Valid: false}, // NULL — not running
	})
	if err != nil {
		slog.Warn("update pomodoro session for complete failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete pomodoro")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session":    sessionToResponse(session),
		"next_phase": nextPhase,
	})
}

// GetPomodoroHistory handles GET /api/pomodoro/history.
// Returns paginated pomodoro time entries and aggregate stats for the requesting user.
func (h *PomodoroHandler) GetPomodoroHistory(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := resolvePomodoroPrincipal(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	// Parse optional pagination query params.
	limit := int32(50)
	offset := int32(0)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}

	// Compute time boundaries for stats aggregation.
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// ISO week: Monday is the first day.
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7 so Monday=1
	}
	weekStart := todayStart.AddDate(0, 0, -(weekday - 1))

	entries, err := h.queries.GetPomodoroHistory(ctx, db.GetPomodoroHistoryParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
		LimitCount:  limit,
		OffsetCount: offset,
	})
	if err != nil {
		slog.Warn("get pomodoro history failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get pomodoro history")
		return
	}

	stats, err := h.queries.GetPomodoroStats(ctx, db.GetPomodoroStatsParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
		TodayStart:  pgtype.Timestamptz{Time: todayStart, Valid: true},
		WeekStart:   pgtype.Timestamptz{Time: weekStart, Valid: true},
	})
	if err != nil {
		slog.Warn("get pomodoro stats failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get pomodoro stats")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"stats": map[string]interface{}{
			"today_count":   stats.TodayCount,
			"week_count":    stats.WeekCount,
			"total_seconds": stats.TotalSeconds,
		},
	})
}

// ResetPomodoro handles POST /api/pomodoro/reset.
// Resets the session back to the initial idle work state without recording a time entry.
func (h *PomodoroHandler) ResetPomodoro(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := resolvePomodoroPrincipal(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	// Ensure a row exists before trying to update.
	_, err := h.queries.GetPomodoroSession(ctx, db.GetPomodoroSessionParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Nothing to reset — return the default idle state.
			writeJSON(w, http.StatusOK, defaultIdleSession())
			return
		}
		slog.Warn("get pomodoro session for reset failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reset pomodoro")
		return
	}

	session, err := h.queries.UpdatePomodoroSession(ctx, db.UpdatePomodoroSessionParams{
		UserID:               userID,
		WorkspaceID:          workspaceID,
		Phase:                "work",
		PhaseDurationSeconds: 1500,
		Status:               "idle",
		ElapsedSeconds:       0,
		StartedAt:            pgtype.Timestamptz{Valid: false}, // NULL
	})
	if err != nil {
		slog.Warn("update pomodoro session for reset failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reset pomodoro")
		return
	}

	writeJSON(w, http.StatusOK, sessionToResponse(session))
}
