package rounds

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

type Handler struct{ Service *Service }

func NewHandler(s *Service) *Handler { return &Handler{Service: s} }

type roundRequest struct {
	Name         string  `json:"name"`
	Mode         *string `json:"mode"`
	ScheduleCron *string `json:"schedule_cron"`
	Timezone     *string `json:"timezone"`
}
type memberRequest struct {
	IssueID string `json:"issue_id"`
}

func parseContext(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	ws, err := util.ParseUUID(middleware.WorkspaceIDFromContext(r.Context()))
	if err != nil {
		writeError(w, 400, "workspace is required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	user, err := util.ParseUUID(strings.TrimSpace(r.Header.Get("X-User-ID")))
	if err != nil {
		writeError(w, 401, "authentication required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return ws, user, true
}
func parseParam(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(chi.URLParam(r, name))
	if err != nil {
		writeError(w, 400, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return id, true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
func handleErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, 404, err.Error())
		return
	}
	writeError(w, 400, err.Error())
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	rows, err := h.Service.List(r.Context(), ws, u)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"rounds": rows})
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	var q roundRequest
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	schedule, timezone := "", "UTC"
	mode := "batch"
	if q.Mode != nil {
		mode = *q.Mode
	}
	if q.ScheduleCron != nil {
		schedule = *q.ScheduleCron
	}
	if q.Timezone != nil {
		timezone = *q.Timezone
	}
	row, err := h.Service.Create(r.Context(), ws, u, q.Name, mode, schedule, timezone)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 201, row)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	id, ok := parseParam(w, r, "roundId")
	if !ok {
		return
	}
	row, err := h.Service.Get(r.Context(), ws, u, id)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, row)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	id, ok := parseParam(w, r, "roundId")
	if !ok {
		return
	}
	var q roundRequest
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	row, err := h.Service.Update(r.Context(), ws, u, id, optional(q.Name), q.Mode, q.ScheduleCron, q.Timezone)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, row)
}
func optional(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	id, ok := parseParam(w, r, "roundId")
	if !ok {
		return
	}
	if err := h.Service.Delete(r.Context(), ws, u, id); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	id, ok := parseParam(w, r, "roundId")
	if !ok {
		return
	}
	var q memberRequest
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	issue, err := util.ParseUUID(q.IssueID)
	if err != nil {
		writeError(w, 400, "invalid issue_id")
		return
	}
	actorType, actorID := "member", u
	if raw := strings.TrimSpace(r.Header.Get("X-Agent-ID")); raw != "" {
		if a, e := util.ParseUUID(raw); e == nil {
			actorType, actorID = "agent", a
		}
	}
	row, err := h.Service.AddMember(r.Context(), ws, u, id, issue, actorType, actorID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 201, row)
}
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(r.Header.Get("X-Agent-ID")) != "" {
		writeError(w, 403, "only members can remove issues from a round")
		return
	}
	id, ok := parseParam(w, r, "roundId")
	if !ok {
		return
	}
	issue, ok := parseParam(w, r, "issueId")
	if !ok {
		return
	}
	if err := h.Service.RemoveMember(r.Context(), ws, u, id, issue); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	id, ok := parseParam(w, r, "roundId")
	if !ok {
		return
	}
	row, err := h.Service.Start(r.Context(), ws, u, id)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 201, row)
}

// Dismiss closes the round's surfaced 'ready' run (the UI calls this Pause) so
// the round collapses back to its planned state. Idempotent — 204 whether or
// not a ready run existed.
func (h *Handler) Dismiss(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	id, ok := parseParam(w, r, "roundId")
	if !ok {
		return
	}
	if err := h.Service.DismissRun(r.Context(), ws, u, id); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	ws, u, ok := parseContext(w, r)
	if !ok {
		return
	}
	id, ok := parseParam(w, r, "roundId")
	if !ok {
		return
	}
	round, err := h.Service.Get(r.Context(), ws, u, id)
	if err != nil {
		handleErr(w, err)
		return
	}
	members, err := h.Service.Members(r.Context(), id)
	if err != nil {
		handleErr(w, err)
		return
	}
	run, err := h.Service.ActiveRun(r.Context(), id)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"round": round, "active_run": run, "members": members})
}
func (h *Handler) IssueMembership(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := parseContext(w, r)
	if !ok {
		return
	}
	issue, ok := parseParam(w, r, "issueId")
	if !ok {
		return
	}
	m, err := h.Service.IssueMembership(r.Context(), ws, issue)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"membership": m})
}
