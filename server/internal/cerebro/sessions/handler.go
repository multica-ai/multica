// Package sessions implements FIR-1741 Model B: a session is a contiguous
// slice of an issue's comment timeline, opened by a session-start marker. There
// is no per-comment membership column (the P1 design that never got set and
// produced empty sessions). Membership is derived at read time from timeline
// order, so a new comment always lands in the latest session by construction.
package sessions

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Handler struct {
	pool     *pgxpool.Pool
	upstream *db.Queries
}

func NewHandler(pool *pgxpool.Pool, upstream *db.Queries) *Handler {
	return &Handler{pool: pool, upstream: upstream}
}

// handoffBrief is the agent-generated carry-over between sessions. In Model B
// it is a single nullable JSONB blob on the session row — there is no 4-field
// human form. An agent may author a richer brief via the API; when none is
// supplied the server auto-summarises the closing session.
type handoffBrief struct {
	Summary   string   `json:"summary"`
	Done      []string `json:"done"`
	Remaining []string `json:"remaining"`
	PlanRef   *string  `json:"plan_ref,omitempty"`
}

type sessionResponse struct {
	ID        string        `json:"id"`
	IssueID   string        `json:"issue_id"`
	Position  int32         `json:"position"`
	Name      string        `json:"name"`
	Status    string        `json:"status"`
	Handoff   *handoffBrief `json:"handoff"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// startFreshRequest opens a new session marker. Mode decides whether the new
// session carries the previous session's handoff forward ("handoff", default)
// or starts blank ("blank"). An optional agent-authored brief overrides the
// server's auto-summary; the closing session always keeps a handoff either way.
type startFreshRequest struct {
	Mode    string        `json:"mode"`
	Handoff *handoffBrief `json:"handoff"`
}

type updateRequest struct {
	Name    *string       `json:"name"`
	Status  *string       `json:"status"`
	Handoff *handoffBrief `json:"handoff"`
}

const (
	startModeHandoff = "handoff"
	startModeBlank   = "blank"
)

// resolveStartMode normalizes the requested start mode. An empty mode defaults
// to carrying the handoff forward. It returns ok=false for any unknown value so
// the handler rejects it with a 400 instead of silently picking a behavior.
func resolveStartMode(mode string) (string, bool) {
	switch mode {
	case "", startModeHandoff:
		return startModeHandoff, true
	case startModeBlank:
		return startModeBlank, true
	default:
		return "", false
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, issue_id, position, name, status, handoff, created_at, updated_at
		FROM cerebro_session
		WHERE issue_id = $1
		ORDER BY position ASC, created_at ASC, id ASC`, issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	defer rows.Close()
	out := []sessionResponse{}
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read sessions")
			return
		}
		out = append(out, session)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}
	sessionID, ok := parseUUIDParam(w, r, "sessionId")
	if !ok {
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update session")
		return
	}
	defer tx.Rollback(r.Context())

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE cerebro_session SET name = $1, updated_at = now() WHERE id = $2 AND issue_id = $3`, name, sessionID, issue.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update session")
			return
		}
	}
	if req.Status != nil {
		if !validStatus(*req.Status) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE cerebro_session SET status = $1, updated_at = now() WHERE id = $2 AND issue_id = $3`, *req.Status, sessionID, issue.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update session")
			return
		}
	}
	if req.Handoff != nil {
		if err := setHandoff(r.Context(), tx, issue.ID, sessionID, normalizeHandoff(req.Handoff)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update handoff")
			return
		}
	}
	session, err := getSession(r.Context(), tx, issue.ID, sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update session")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (h *Handler) StartFresh(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}
	var req startFreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	mode, ok := resolveStartMode(req.Mode)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid mode")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start fresh")
		return
	}
	defer tx.Rollback(r.Context())

	// FIR-1741 (Codex review): serialize concurrent Start fresh on the same
	// issue. Without this, two racing calls can both read the same open session
	// and the same MAX(position)+1 and create duplicate sessions / multiple open
	// sessions — the P1 failure class (empty/duplicate sessions) through a new
	// door. The lock is transaction-scoped and auto-releases on commit/rollback;
	// the unique index on (issue_id, position) (migration 9098) is the DB-level
	// backstop.
	if _, err = tx.Exec(r.Context(),
		`SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, issue.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock issue")
		return
	}

	// Find the currently open session (latest not-done), if any.
	var openID pgtype.UUID
	var openCreatedAt pgtype.Timestamptz
	err = tx.QueryRow(r.Context(), `
		SELECT id, created_at FROM cerebro_session
		WHERE issue_id = $1 AND status <> 'done'
		ORDER BY position DESC, created_at DESC, id DESC
		LIMIT 1`, issue.ID).Scan(&openID, &openCreatedAt)
	hasOpen := err == nil
	if err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to load open session")
		return
	}

	var sessionCount int
	if err := tx.QueryRow(r.Context(), `SELECT COUNT(*)::int FROM cerebro_session WHERE issue_id = $1`, issue.ID).Scan(&sessionCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count sessions")
		return
	}

	if sessionCount == 0 {
		// First Start fresh on a legacy issue: materialize the implicit
		// "Session 1" covering the whole existing timeline and close it with a
		// handoff, then open the new active session. Session 1's created_at is
		// the issue's creation time so every prior timeline entry windows into it.
		histHandoff := req.Handoff
		if histHandoff == nil {
			// Session 1 covers the whole existing timeline; no upper bound.
			histHandoff, err = h.generateHandoff(r.Context(), tx, issue.ID, issue.CreatedAt.Time, nil)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to summarise session")
				return
			}
		} else {
			histHandoff = normalizeHandoff(histHandoff)
		}
		// FIR-1787 point 2: an agent-authored handoff names the session it closes.
		session1Name := "Session 1"
		if n := sessionNameFromHandoff(histHandoff, req.Handoff != nil); n != "" {
			session1Name = n
		}
		if _, err := createSession(r.Context(), tx, issue.ID, 1, session1Name, "done", histHandoff, &issue.CreatedAt.Time); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create session 1")
			return
		}
		nextHandoff := histHandoff
		if mode == startModeBlank {
			nextHandoff = nil
		}
		next, err := createSession(r.Context(), tx, issue.ID, 2, "Session 2", "in_progress", nextHandoff, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create next session")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start fresh")
			return
		}
		writeJSON(w, http.StatusCreated, next)
		return
	}

	// At least one session exists. Close the open one with a handoff (agent
	// brief if supplied, else auto-summary of its window) and open the next.
	handoff := req.Handoff
	if hasOpen {
		if handoff == nil {
			// Bound the window above by the next session marker, if any, so the
			// summary never counts comments that belong to a later session. With
			// the advisory lock the open session is normally the newest (until is
			// nil), but this keeps generateHandoff correct for any open session.
			var until *time.Time
			var nextStart pgtype.Timestamptz
			nerr := tx.QueryRow(r.Context(), `
				SELECT created_at FROM cerebro_session
				WHERE issue_id = $1 AND created_at > $2
				ORDER BY created_at ASC, position ASC, id ASC
				LIMIT 1`, issue.ID, openCreatedAt.Time).Scan(&nextStart)
			if nerr == nil {
				t := nextStart.Time
				until = &t
			} else if nerr != pgx.ErrNoRows {
				writeError(w, http.StatusInternalServerError, "failed to load next session")
				return
			}
			handoff, err = h.generateHandoff(r.Context(), tx, issue.ID, openCreatedAt.Time, until)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to summarise session")
				return
			}
		} else {
			handoff = normalizeHandoff(handoff)
		}
		if err := setHandoff(r.Context(), tx, issue.ID, openID, handoff); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to write handoff")
			return
		}
		// FIR-1787 point 2: name the closing session from the agent's handoff, but
		// only if it still carries the default "Session N" (never clobber a name a
		// human or earlier agent already set).
		closeName := sessionNameFromHandoff(handoff, req.Handoff != nil)
		if _, err := tx.Exec(r.Context(), `
			UPDATE cerebro_session
			SET status = 'done',
			    name = CASE WHEN $3::text <> '' AND name ~ '^Session [0-9]+$' THEN $3::text ELSE name END,
			    updated_at = now()
			WHERE id = $1 AND issue_id = $2`, openID, issue.ID, closeName); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to close session")
			return
		}
	} else if handoff != nil {
		handoff = normalizeHandoff(handoff)
	}

	var nextPos int32
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(MAX(position), 0)::int + 1 FROM cerebro_session WHERE issue_id = $1`, issue.ID).Scan(&nextPos); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute position")
		return
	}
	nextHandoff := handoff
	if mode == startModeBlank {
		nextHandoff = nil
	}
	next, err := createSession(r.Context(), tx, issue.ID, nextPos, "Session "+strconv.Itoa(int(nextPos)), "in_progress", nextHandoff, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create next session")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start fresh")
		return
	}
	writeJSON(w, http.StatusCreated, next)
}

// generateHandoff auto-summarises a session's window [since, until) from its
// root comments. Deterministic, no LLM: it captures how many updates the session
// held and a snippet of the most recent one, so a fresh runtime opening the next
// session sees what came before even when no agent authored a brief.
//
// FIR-1741 (Codex review): the window is now half-open and bounded above by
// `until` (the next session marker's created_at), matching the frontend's
// grouping rule (`packages/cerebro-sessions/grouping.ts`: a comment belongs to
// the marker that precedes it). A nil `until` means no upper bound — correct for
// the newest session, whose window runs to "now". Without the upper bound, a
// summary could count comments that belong to a later session if markers ever
// land out of order.
func (h *Handler) generateHandoff(ctx context.Context, tx pgx.Tx, issueID pgtype.UUID, since time.Time, until *time.Time) (*handoffBrief, error) {
	var count int
	var latest *string
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::int,
		       (array_agg(content ORDER BY created_at DESC) FILTER (WHERE content IS NOT NULL))[1]
		FROM comment
		WHERE issue_id = $1
		  AND parent_id IS NULL
		  AND type = 'comment'
		  AND created_at >= $2
		  AND ($3::timestamptz IS NULL OR created_at < $3)`, issueID, since, until).Scan(&count, &latest)
	if err != nil {
		return nil, err
	}
	brief := &handoffBrief{Done: []string{}, Remaining: []string{}}
	if count == 0 {
		brief.Summary = "Auto-generated handoff: the previous session had no comments."
		return brief, nil
	}
	plural := "s"
	if count == 1 {
		plural = ""
	}
	summary := "Auto-generated handoff: " + strconv.Itoa(count) + " comment" + plural + " in the previous session."
	if latest != nil {
		summary += " Latest: " + snippet(*latest, 200)
	}
	brief.Summary = summary
	return brief, nil
}

// sessionNameFromHandoff derives a short human display name for a session being
// closed at handoff, from an AGENT-authored brief's summary. It returns "" for
// server auto-generated briefs (so the default "Session N" stays) and for empty
// summaries. This is the server half of FIR-1787 point 2 — "the agent also names
// the session at handoff" — without a new API field: the agent already supplies
// the brief, so its summary titles the session it just finished.
func sessionNameFromHandoff(brief *handoffBrief, agentAuthored bool) string {
	if !agentAuthored || brief == nil {
		return ""
	}
	s := strings.TrimSpace(brief.Summary)
	if s == "" {
		return ""
	}
	// First line only, capped — a title, not the whole summary.
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return snippet(s, 60)
}

func (h *Handler) loadIssue(w http.ResponseWriter, r *http.Request) (db.Issue, bool) {
	wsID, ok := workspaceFromContext(w, r)
	if !ok {
		return db.Issue{}, false
	}
	issueID, ok := parseUUIDParam(w, r, "issueId")
	if !ok {
		return db.Issue{}, false
	}
	issue, err := h.upstream.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: wsID})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	return issue, true
}

func createSession(ctx context.Context, tx pgx.Tx, issueID pgtype.UUID, position int32, name string, status string, handoff *handoffBrief, createdAt *time.Time) (sessionResponse, error) {
	raw, err := marshalHandoff(handoff)
	if err != nil {
		return sessionResponse{}, err
	}
	var createdParam any
	if createdAt != nil {
		createdParam = *createdAt
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO cerebro_session (issue_id, position, name, status, handoff, created_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, COALESCE($6::timestamptz, now()))
		RETURNING id, issue_id, position, name, status, handoff, created_at, updated_at`,
		issueID, position, name, status, raw, createdParam)
	return scanSession(row)
}

func setHandoff(ctx context.Context, tx pgx.Tx, issueID pgtype.UUID, sessionID pgtype.UUID, handoff *handoffBrief) error {
	raw, err := marshalHandoff(handoff)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE cerebro_session
		SET handoff = $1::jsonb, updated_at = now()
		WHERE id = $2 AND issue_id = $3`, raw, sessionID, issueID)
	return err
}

func getSession(ctx context.Context, tx pgx.Tx, issueID pgtype.UUID, sessionID pgtype.UUID) (sessionResponse, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, issue_id, position, name, status, handoff, created_at, updated_at
		FROM cerebro_session
		WHERE id = $1 AND issue_id = $2`, sessionID, issueID)
	return scanSession(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (sessionResponse, error) {
	var id, issueID pgtype.UUID
	var raw []byte
	out := sessionResponse{}
	if err := row.Scan(&id, &issueID, &out.Position, &out.Name, &out.Status, &raw, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return sessionResponse{}, err
	}
	out.ID = util.UUIDToString(id)
	out.IssueID = util.UUIDToString(issueID)
	if len(raw) > 0 {
		var brief handoffBrief
		if err := json.Unmarshal(raw, &brief); err == nil {
			if brief.Done == nil {
				brief.Done = []string{}
			}
			if brief.Remaining == nil {
				brief.Remaining = []string{}
			}
			out.Handoff = &brief
		}
	}
	return out, nil
}

// marshalHandoff returns nil (SQL NULL) for a nil brief, or the JSON encoding.
func marshalHandoff(handoff *handoffBrief) ([]byte, error) {
	if handoff == nil {
		return nil, nil
	}
	return json.Marshal(handoff)
}

func normalizeHandoff(in *handoffBrief) *handoffBrief {
	if in == nil {
		return nil
	}
	return &handoffBrief{
		Summary:   strings.TrimSpace(in.Summary),
		Done:      compactStrings(in.Done),
		Remaining: compactStrings(in.Remaining),
		PlanRef:   trimPtr(in.PlanRef),
	}
}

func snippet(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "…"
}

func compactStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		if s := strings.TrimSpace(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func trimPtr(in *string) *string {
	if in == nil {
		return nil
	}
	s := strings.TrimSpace(*in)
	if s == "" {
		return nil
	}
	return &s
}

func validStatus(status string) bool {
	return status == "todo" || status == "in_progress" || status == "done"
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(chi.URLParam(r, name))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return id, true
}

func workspaceFromContext(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := middleware.WorkspaceIDFromContext(r.Context())
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing workspace")
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace")
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
