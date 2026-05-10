package handler

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
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

// CompletePomodoro handles POST /api/pomodoro/complete.
// If phase='work', creates a time_entry of type='pomodoro' for the work duration.
// Then flips the phase (work→break 5min, break→work 25min), resets elapsed and started_at, sets status='idle'.
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

	// If this was a work phase, record a time entry for the completed focus block.
	if existing.Phase == "work" && existing.StartedAt.Valid {
		desc := "Pomodoro 专注"
		stopTime := pgtype.Timestamptz{Time: time.Now(), Valid: true}
		_, teErr := h.queries.CreateTimeEntry(ctx, db.CreateTimeEntryParams{
			WorkspaceID:     workspaceID,
			UserID:          userID,
			IssueID:         pgtype.UUID{}, // not linked to an issue
			Description:     pgtype.Text{String: desc, Valid: true},
			StartTime:       existing.StartedAt,
			StopTime:        stopTime,
			DurationSeconds: int64(existing.PhaseDurationSeconds),
			Type:            "pomodoro",
		})
		if teErr != nil {
			// Log but don't block the phase flip — the session state is more important.
			slog.Warn("pomodoro complete: failed to create time entry", append(logger.RequestAttrs(r), "error", teErr)...)
		}
	}

	// Flip phase and reset timers.
	nextPhase := "break"
	nextDuration := int32(300) // 5-minute break
	if existing.Phase == "break" {
		nextPhase = "work"
		nextDuration = 1500 // 25-minute work
	}

	session, err := h.queries.UpdatePomodoroSession(ctx, db.UpdatePomodoroSessionParams{
		UserID:               userID,
		WorkspaceID:          workspaceID,
		Phase:                nextPhase,
		PhaseDurationSeconds: nextDuration,
		Status:               "idle",
		ElapsedSeconds:       0,
		StartedAt:            pgtype.Timestamptz{Valid: false}, // NULL
	})
	if err != nil {
		slog.Warn("update pomodoro session for complete failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete pomodoro")
		return
	}

	writeJSON(w, http.StatusOK, sessionToResponse(session))
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
