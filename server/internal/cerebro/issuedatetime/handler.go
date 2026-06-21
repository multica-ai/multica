// Package issuedatetime hosts the REST surface for the cerebro "issue start/due
// time-of-day" feature. The issue's start_date and due_date stay pure calendar
// days on the upstream issue table; this feature stores an OPTIONAL wall-clock
// time alongside each in cerebro_issue_date_time (migration 9087).
//
// At the scheduled moment the date-reminder sweeper
// (server/cmd/server/issue_date_reminder_sweeper.go) fires: a member-assigned
// issue rings a reminder, an agent-assigned issue auto-starts its agent.
//
// Wired into the router under /api/cerebro/issues/{issueID}/date-times by the
// cerebro-issue-date-times-routes CEREBRO-PATCH in server/cmd/server/router.go.
// Every endpoint is workspace-scoped via X-Workspace-ID and authenticated via
// X-User-ID.
package issuedatetime

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const microsPerSecond = 1_000_000

// Handler serves the per-issue date-time endpoints. Cerebro holds the cerebro
// storage queries; Upstream resolves the issue (and its workspace) for access
// control.
type Handler struct {
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries
}

func NewHandler(cerebro *cerebrodb.Queries, upstream *db.Queries) *Handler {
	return &Handler{Cerebro: cerebro, Upstream: upstream}
}

// response is the wire shape: optional "HH:MM" strings, null when unset.
// AgentStartKind is the per-issue auto-start choice ("none" | "start" | "due"),
// null when the issue inherits the workspace default.
type response struct {
	StartTime      *string `json:"start_time"`
	DueTime        *string `json:"due_time"`
	AgentStartKind *string `json:"agent_start_kind"`
}

type request struct {
	StartTime      *string `json:"start_time"`
	DueTime        *string `json:"due_time"`
	AgentStartKind *string `json:"agent_start_kind"`
}

// Get returns the optional start/due times for an issue. Both null when none
// are set (or no row exists yet).
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	issue, ok := h.loadIssueScoped(w, r)
	if !ok {
		return
	}
	row, err := h.Cerebro.GetCerebroIssueDateTime(r.Context(), issue.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, response{})
			return
		}
		writeError(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, response{
		StartTime:      timeToString(row.StartTime),
		DueTime:        timeToString(row.DueTime),
		AgentStartKind: kindToString(row.AgentStartKind),
	})
}

// Put sets (or clears) the optional start/due times and the agent auto-start
// choice for an issue. A null time field clears that time; opting in to
// auto-start on a date requires that date's time to be set in the same request
// (FIR-1597). When all three end up empty the row is deleted.
func (h *Handler) Put(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	issue, ok := h.loadIssueScoped(w, r)
	if !ok {
		return
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	start, err := parseTime(req.StartTime)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid start_time")
		return
	}
	due, err := parseTime(req.DueTime)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid due_time")
		return
	}
	kind, err := parseKind(req.AgentStartKind)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent_start_kind")
		return
	}

	// Opting in to auto-start requires the matching date's time to be set in the
	// same request, so a scheduled run always fires at a deliberate wall-clock
	// moment (FIR-1597).
	if kind.Valid && kind.String == "start" && !start.Valid {
		writeError(w, http.StatusBadRequest, "start_time is required to auto-start on the start date")
		return
	}
	if kind.Valid && kind.String == "due" && !due.Valid {
		writeError(w, http.StatusBadRequest, "due_time is required to auto-start on the due date")
		return
	}

	if !start.Valid && !due.Valid && !kind.Valid {
		if err := h.Cerebro.DeleteCerebroIssueDateTime(r.Context(), issue.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "clear failed")
			return
		}
		writeJSON(w, http.StatusOK, response{})
		return
	}

	row, err := h.Cerebro.UpsertCerebroIssueDateTime(r.Context(), cerebrodb.UpsertCerebroIssueDateTimeParams{
		IssueID:        issue.ID,
		StartTime:      start,
		DueTime:        due,
		AgentStartKind: kind,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, response{
		StartTime:      timeToString(row.StartTime),
		DueTime:        timeToString(row.DueTime),
		AgentStartKind: kindToString(row.AgentStartKind),
	})
}

// loadIssueScoped resolves the {issueID} path param and enforces that it
// belongs to the request's workspace.
func (h *Handler) loadIssueScoped(w http.ResponseWriter, r *http.Request) (db.Issue, bool) {
	issueID, ok := parseUUIDParam(w, r, "issueID")
	if !ok {
		return db.Issue{}, false
	}
	issue, err := h.Upstream.GetIssue(r.Context(), issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue not found")
			return db.Issue{}, false
		}
		writeError(w, http.StatusInternalServerError, "load issue failed")
		return db.Issue{}, false
	}
	if !ensureWorkspaceMatch(w, r, issue.WorkspaceID) {
		return db.Issue{}, false
	}
	return issue, true
}

// parseTime converts an optional "HH:MM" (or "HH:MM:SS") string into a
// pgtype.Time. A nil or empty pointer yields an invalid (NULL) time.
func parseTime(s *string) (pgtype.Time, error) {
	if s == nil {
		return pgtype.Time{}, nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return pgtype.Time{}, nil
	}
	var hh, mm, ss int
	parts := strings.Split(v, ":")
	switch len(parts) {
	case 2:
		if _, err := fmt.Sscanf(v, "%d:%d", &hh, &mm); err != nil {
			return pgtype.Time{}, err
		}
	case 3:
		if _, err := fmt.Sscanf(v, "%d:%d:%d", &hh, &mm, &ss); err != nil {
			return pgtype.Time{}, err
		}
	default:
		return pgtype.Time{}, fmt.Errorf("expected HH:MM, got %q", v)
	}
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 || ss < 0 || ss > 59 {
		return pgtype.Time{}, fmt.Errorf("time out of range: %q", v)
	}
	micros := int64(hh*3600+mm*60+ss) * microsPerSecond
	return pgtype.Time{Microseconds: micros, Valid: true}, nil
}

// timeToString renders a pgtype.Time as "HH:MM", or nil when unset.
func timeToString(t pgtype.Time) *string {
	if !t.Valid {
		return nil
	}
	totalSeconds := t.Microseconds / microsPerSecond
	hh := totalSeconds / 3600
	mm := (totalSeconds % 3600) / 60
	s := fmt.Sprintf("%02d:%02d", hh, mm)
	return &s
}

// parseKind validates the optional agent auto-start choice. A nil or empty
// pointer yields an invalid (NULL) value meaning "inherit the workspace
// default". Accepts "none" | "start" | "due".
func parseKind(s *string) (pgtype.Text, error) {
	if s == nil {
		return pgtype.Text{}, nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return pgtype.Text{}, nil
	}
	switch v {
	case "none", "start", "due":
		return pgtype.Text{String: v, Valid: true}, nil
	default:
		return pgtype.Text{}, fmt.Errorf("expected none|start|due, got %q", v)
	}
}

// kindToString renders the stored agent auto-start choice, or nil when unset
// (the issue inherits the workspace default).
func kindToString(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

// ---------------------------------------------------------------------------
// Request helpers (mirror the cerebro/sprints handler conventions)
// ---------------------------------------------------------------------------

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing "+name)
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return uid, true
}

func ensureWorkspaceMatch(w http.ResponseWriter, r *http.Request, target pgtype.UUID) bool {
	raw := middleware.WorkspaceIDFromContext(r.Context())
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing workspace")
		return false
	}
	current, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace")
		return false
	}
	if !current.Valid || !target.Valid || current.Bytes != target.Bytes {
		writeError(w, http.StatusForbidden, "workspace mismatch")
		return false
	}
	return true
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
