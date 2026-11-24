package handler

import (
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Runtime rate limits (MUL-9427). While a task runs, the daemon forwards the
// subscription rate-limit state the agent CLI reports for its account (Claude
// Code's 5-hour / weekly windows, Codex's primary / secondary windows) and the
// server keeps the newest snapshot per runtime. Members who may read the
// runtime see it on the runtime pages.

const (
	maxRateLimitBodyBytes  = 32 << 10
	maxRateLimitWindows    = 16
	maxRateLimitString     = 64
	maxRateLimitPercent    = 1000
	maxRateLimitWindowMins = 366 * 24 * 60
	// rateLimitClockSkew bounds how far in the future a daemon's observation
	// time may sit before it is pulled back to the server clock, so a fast
	// daemon clock cannot pin a snapshot no later report can replace.
	rateLimitClockSkew = 5 * time.Minute
)

var rateLimitWindowKinds = map[string]bool{
	"session":       true,
	"weekly":        true,
	"weekly_opus":   true,
	"weekly_sonnet": true,
	"other":         true,
}

var rateLimitStatuses = map[string]bool{
	"":         true,
	"allowed":  true,
	"warning":  true,
	"rejected": true,
}

// runtimeRateLimitSnapshot is the stored shape. Decoding into it drops any
// field this server does not know, so the column only holds what readers
// are built for.
type runtimeRateLimitSnapshot struct {
	PlanType   string                   `json:"plan_type,omitempty"`
	Status     string                   `json:"status,omitempty"`
	Windows    []runtimeRateLimitWindow `json:"windows"`
	Credits    *runtimeRateLimitCredits `json:"credits,omitempty"`
	ObservedAt time.Time                `json:"observed_at"`
}

type runtimeRateLimitWindow struct {
	Kind          string     `json:"kind"`
	UsedPercent   *float64   `json:"used_percent,omitempty"`
	WindowMinutes int64      `json:"window_minutes,omitempty"`
	ResetsAt      *time.Time `json:"resets_at,omitempty"`
	Status        string     `json:"status,omitempty"`
}

type runtimeRateLimitCredits struct {
	HasCredits bool   `json:"has_credits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance,omitempty"`
}

// normalize validates a daemon report in place. Windows of a kind this server
// cannot label are dropped rather than rejected, so a newer daemon that learns
// a new window still reports the ones this server understands.
func (s *runtimeRateLimitSnapshot) normalize(now time.Time) bool {
	if s.ObservedAt.IsZero() || len(s.Windows) > maxRateLimitWindows {
		return false
	}
	if len(s.PlanType) > maxRateLimitString || !rateLimitStatuses[s.Status] {
		return false
	}
	if s.Credits != nil && len(s.Credits.Balance) > maxRateLimitString {
		return false
	}
	if s.ObservedAt.After(now.Add(rateLimitClockSkew)) {
		s.ObservedAt = now
	}
	s.ObservedAt = s.ObservedAt.UTC()
	windows := make([]runtimeRateLimitWindow, 0, len(s.Windows))
	for _, w := range s.Windows {
		if !rateLimitWindowKinds[w.Kind] {
			continue
		}
		if !rateLimitStatuses[w.Status] || w.WindowMinutes < 0 || w.WindowMinutes > maxRateLimitWindowMins {
			return false
		}
		if w.UsedPercent != nil {
			v := *w.UsedPercent
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > maxRateLimitPercent {
				return false
			}
		}
		if w.ResetsAt != nil {
			reset := w.ResetsAt.UTC()
			w.ResetsAt = &reset
		}
		windows = append(windows, w)
	}
	s.Windows = windows
	return true
}

// ReportTaskRateLimits stores the rate-limit snapshot the daemon observed
// while running a task, as the latest state of that task's runtime.
func (h *Handler) ReportTaskRateLimits(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRateLimitBodyBytes)
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	if !task.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "task has no runtime")
		return
	}

	var req struct {
		RateLimits *runtimeRateLimitSnapshot `json:"rate_limits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RateLimits == nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.RateLimits.normalize(time.Now()) {
		writeError(w, http.StatusBadRequest, "invalid rate limits")
		return
	}
	stored, err := json.Marshal(req.RateLimits)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode rate limits")
		return
	}
	if err := h.Queries.UpsertRuntimeRateLimit(r.Context(), db.UpsertRuntimeRateLimitParams{
		RuntimeID:   task.RuntimeID,
		WorkspaceID: parseUUID(workspaceID),
		Snapshot:    stored,
		ObservedAt:  pgtype.Timestamptz{Time: req.RateLimits.ObservedAt, Valid: true},
	}); err != nil {
		slog.Warn("store runtime rate limits failed", "task_id", uuidToString(task.ID), "runtime_id", uuidToString(task.RuntimeID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store rate limits")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetRuntimeRateLimits returns a runtime's latest rate-limit snapshot to a
// member who may read the runtime. A runtime that has not reported one (no
// task yet, an older daemon, an API-key account, or a CLI that reports no
// limits) answers 200 with null so the page can say so without an error.
func (h *Handler) GetRuntimeRateLimits(w http.ResponseWriter, r *http.Request) {
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	row, err := h.Queries.GetRuntimeRateLimit(r.Context(), rt.ID)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]any{"rate_limits": nil})
			return
		}
		if writeQueryCancellation(w, r, err) {
			return
		}
		slog.Warn("load runtime rate limits failed", "runtime_id", uuidToString(rt.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load rate limits")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rate_limits": json.RawMessage(row.Snapshot)})
}
