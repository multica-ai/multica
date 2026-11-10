package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// The daemon re-reports a live environment at least once a minute, so one
	// that has not been seen for a few minutes is on a machine that went away.
	issueEnvironmentUnreachableAfter = 3 * time.Minute
	// Ended environments stay listed this long so the sidebar can show what ran.
	issueEnvironmentHistoryWindow = 7 * 24 * time.Hour
	maxIssueEnvironmentsPerSnapshot = 50
)

var issueEnvironmentSources = map[string]bool{"config": true, "detected": true}

var issueEnvironmentStates = map[string]bool{"starting": true, "running": true, "exited": true, "stopped": true}

// IssueEnvironmentReport is one environment in a daemon's per-task snapshot.
type IssueEnvironmentReport struct {
	Key       string     `json:"key"`
	Source    string     `json:"source"`
	Name      string     `json:"name"`
	Label     string     `json:"label"`
	URL       string     `json:"url"`
	Port      int        `json:"port"`
	State     string     `json:"state"`
	Detail    string     `json:"detail"`
	StartedAt time.Time  `json:"started_at"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type ReportTaskEnvironmentsRequest struct {
	Environments []IssueEnvironmentReport `json:"environments"`
}

type IssueEnvironmentResponse struct {
	ID         string  `json:"id"`
	IssueID    string  `json:"issue_id"`
	TaskID     string  `json:"task_id"`
	RuntimeID  *string `json:"runtime_id"`
	AgentID    *string `json:"agent_id"`
	Source     string  `json:"source"`
	Name       string  `json:"name"`
	Label      string  `json:"label"`
	URL        string  `json:"url"`
	Port       *int32  `json:"port"`
	State      string  `json:"state"`
	Detail     string  `json:"detail"`
	StartedAt  string  `json:"started_at"`
	ExpiresAt  *string `json:"expires_at"`
	LastSeenAt string  `json:"last_seen_at"`
	EndedAt    *string `json:"ended_at"`
}

type IssueEnvironmentsResponse struct {
	Environments []IssueEnvironmentResponse `json:"environments"`
}

// validateIssueEnvironmentReport normalizes one reported environment and
// returns a reason when it cannot be stored.
func validateIssueEnvironmentReport(env *IssueEnvironmentReport, now time.Time) string {
	env.Key = strings.TrimSpace(env.Key)
	env.Name = strings.TrimSpace(env.Name)
	env.Label = strings.TrimSpace(env.Label)
	env.URL = strings.TrimSpace(env.URL)
	switch {
	case env.Key == "" || len(env.Key) > 128:
		return "key must be 1-128 characters"
	case !issueEnvironmentSources[env.Source]:
		return "unknown source"
	case !issueEnvironmentStates[env.State]:
		return "unknown state"
	case env.Name == "" || utf8.RuneCountInString(env.Name) > 64:
		return "name must be 1-64 characters"
	case utf8.RuneCountInString(env.Label) > 120:
		return "label must be at most 120 characters"
	case env.Port < 0 || env.Port > 65535:
		return "port out of range"
	}
	if env.URL != "" {
		u, err := url.Parse(env.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(env.URL) > 2048 {
			return "url must be an http(s) URL"
		}
	}
	if utf8.RuneCountInString(env.Detail) > 500 {
		env.Detail = string([]rune(env.Detail)[:500])
	}
	if env.StartedAt.IsZero() {
		env.StartedAt = now
	}
	return ""
}

// ReportTaskEnvironments replaces the daemon's view of one task's environments.
// The snapshot is complete: an environment the task reported before and omits
// now has ended.
func (h *Handler) ReportTaskEnvironments(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	var req ReportTaskEnvironmentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Environments) > maxIssueEnvironmentsPerSnapshot {
		writeError(w, http.StatusBadRequest, "too many environments")
		return
	}
	now := time.Now()
	seen := make(map[string]bool, len(req.Environments))
	for i := range req.Environments {
		if reason := validateIssueEnvironmentReport(&req.Environments[i], now); reason != "" {
			writeError(w, http.StatusBadRequest, "environment "+req.Environments[i].Key+": "+reason)
			return
		}
		if seen[req.Environments[i].Key] {
			writeError(w, http.StatusBadRequest, "duplicate environment key "+req.Environments[i].Key)
			return
		}
		seen[req.Environments[i].Key] = true
	}
	// Environments are an issue-sidebar surface; other task kinds have nowhere
	// to show them.
	if !task.IssueID.Valid {
		writeJSON(w, http.StatusOK, map[string]int{"accepted": 0})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to report environments")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	before, err := qtx.ListOpenIssueEnvironmentsByTask(r.Context(), task.ID)
	if err != nil {
		slog.Warn("list task environments failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to report environments")
		return
	}
	changed := false
	open := make(map[string]db.IssueEnvironment, len(before))
	for _, row := range before {
		open[row.LocalKey] = row
	}
	keep := make([]string, 0, len(req.Environments))
	for _, env := range req.Environments {
		keep = append(keep, env.Key)
		params := db.UpsertIssueEnvironmentParams{
			WorkspaceID: wsUUID,
			IssueID:     task.IssueID,
			TaskID:      task.ID,
			RuntimeID:   task.RuntimeID,
			AgentID:     task.AgentID,
			LocalKey:    env.Key,
			Source:      env.Source,
			Name:        env.Name,
			Label:       env.Label,
			Url:         env.URL,
			State:       env.State,
			Detail:      env.Detail,
			StartedAt:   pgtype.Timestamptz{Time: env.StartedAt, Valid: true},
		}
		if env.Port > 0 {
			params.Port = pgtype.Int4{Int32: int32(env.Port), Valid: true}
		}
		if env.ExpiresAt != nil {
			params.ExpiresAt = pgtype.Timestamptz{Time: *env.ExpiresAt, Valid: true}
		}
		if _, err := qtx.UpsertIssueEnvironment(r.Context(), params); err != nil {
			slog.Warn("upsert task environment failed", "task_id", uuidToString(task.ID), "key", env.Key, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to report environments")
			return
		}
		prev, wasOpen := open[env.Key]
		if !wasOpen || prev.State != env.State || prev.Url != env.URL || prev.Label != env.Label || prev.Detail != env.Detail {
			changed = true
		}
	}
	for key := range open {
		if !seen[key] {
			changed = true
		}
	}
	if err := qtx.EndIssueEnvironmentsMissingFromSnapshot(r.Context(), db.EndIssueEnvironmentsMissingFromSnapshotParams{
		TaskID:   task.ID,
		KeepKeys: keep,
	}); err != nil {
		slog.Warn("end task environments failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to report environments")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to report environments")
		return
	}
	// A heartbeat that only refreshes last_seen_at changes nothing a reader
	// sees, so it stays off the realtime channel.
	if changed {
		h.publish(protocol.EventEnvironmentChanged, workspaceID, "system", "", map[string]string{
			"issue_id": uuidToString(task.IssueID),
		})
	}
	writeJSON(w, http.StatusOK, map[string]int{"accepted": len(req.Environments)})
}

// ListIssueEnvironments returns the issue's live environments and those that
// ended recently.
func (h *Handler) ListIssueEnvironments(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	now := time.Now()
	rows, err := h.Queries.ListIssueEnvironments(r.Context(), db.ListIssueEnvironmentsParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		EndedAfter:  pgtype.Timestamptz{Time: now.Add(-issueEnvironmentHistoryWindow), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list environments")
		return
	}
	out := make([]IssueEnvironmentResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, issueEnvironmentToResponse(row, now))
	}
	writeJSON(w, http.StatusOK, IssueEnvironmentsResponse{Environments: out})
}

func issueEnvironmentToResponse(row db.IssueEnvironment, now time.Time) IssueEnvironmentResponse {
	state := row.State
	if !row.EndedAt.Valid && row.LastSeenAt.Valid && now.Sub(row.LastSeenAt.Time) > issueEnvironmentUnreachableAfter {
		state = "unreachable"
	}
	resp := IssueEnvironmentResponse{
		ID:         uuidToString(row.ID),
		IssueID:    uuidToString(row.IssueID),
		TaskID:     uuidToString(row.TaskID),
		RuntimeID:  uuidToPtr(row.RuntimeID),
		AgentID:    uuidToPtr(row.AgentID),
		Source:     row.Source,
		Name:       row.Name,
		Label:      row.Label,
		URL:        row.Url,
		State:      state,
		Detail:     row.Detail,
		StartedAt:  timestampToString(row.StartedAt),
		ExpiresAt:  timestampToPtr(row.ExpiresAt),
		LastSeenAt: timestampToString(row.LastSeenAt),
		EndedAt:    timestampToPtr(row.EndedAt),
	}
	if row.Port.Valid {
		port := row.Port.Int32
		resp.Port = &port
	}
	return resp
}
