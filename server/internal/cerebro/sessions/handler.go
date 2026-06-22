// Package sessions implements FIR-1874: a session IS a comment thread. There is
// no separate session entity timed independently of the timeline, and no
// separate session status: a cerebro_session row is OPTIONAL metadata (a name
// and/or a handoff) keyed to a thread root via root_comment_id, and a session's
// open/closed state is the thread root's resolved_at. Membership is the thread
// itself (root comment + its replies). This removes the two pieces of "double
// logic" the time-window model carried — the status column and the marker-based
// grouping — which is exactly the misalignment FIR-1874 set out to fix.
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

// handoffBrief is the agent-generated (or auto-summarised) carry-over a session
// keeps when it is handed off. It is a single nullable JSONB blob on the row.
type handoffBrief struct {
	Summary   string   `json:"summary"`
	Done      []string `json:"done"`
	Remaining []string `json:"remaining"`
	PlanRef   *string  `json:"plan_ref,omitempty"`
}

// sessionResponse mirrors a cerebro_session row. root_comment_id ties it to the
// thread it is the metadata for; the frontend matches it to the thread and reads
// open/closed from that thread root's resolved_at (there is no status field).
type sessionResponse struct {
	ID            string        `json:"id"`
	IssueID       string        `json:"issue_id"`
	RootCommentID *string       `json:"root_comment_id"`
	Position      int32         `json:"position"`
	Name          string        `json:"name"`
	Handoff       *handoffBrief `json:"handoff"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// handoffRequest is the body of the Send-button "Start new session with Handoff"
// action: it names the open thread being handed off (root_comment_id) and may
// carry an agent-authored brief. The server resolves that thread (closing the
// session) and stores the handoff on its row. The new session is the new thread
// the composer posts separately.
type handoffRequest struct {
	RootCommentID string        `json:"root_comment_id"`
	Handoff       *handoffBrief `json:"handoff"`
}

type updateRequest struct {
	Name    *string       `json:"name"`
	Handoff *handoffBrief `json:"handoff"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, issue_id, root_comment_id, position, name, handoff, created_at, updated_at
		FROM cerebro_session
		WHERE issue_id = $1
		ORDER BY created_at ASC, id ASC`, issue.ID)
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

// Update renames a session or replaces its handoff. FIR-1874: a session is a
// thread, so the {sessionId} path param is the THREAD ROOT comment id, and the
// metadata row is upserted (created on first rename/handoff). State (open/closed)
// is the thread root's resolved_at and is never set here.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}
	rootID, ok := parseUUIDParam(w, r, "sessionId")
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

	// The target must be a root comment (a thread) on this issue.
	var isRoot bool
	if err := tx.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM comment WHERE id = $1 AND issue_id = $2 AND parent_id IS NULL)`,
		rootID, issue.ID).Scan(&isRoot); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load thread")
		return
	}
	if !isRoot {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}

	name := ""
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
	}
	session, err := upsertThreadSession(r.Context(), tx, issue.ID, rootID, name, normalizeHandoff(req.Handoff))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update session")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update session")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

// StartFresh is the Send-button "Start new session with Handoff" action. In the
// thread = session model it does NOT open a marker; it CLOSES the chosen open
// thread by resolving its root comment and stores a handoff on that thread's
// session row. The new session is simply the new thread the composer posts next.
// (The route is still /start-fresh to avoid an upstream router edit.)
func (h *Handler) StartFresh(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}
	var req handoffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rootID, err := util.ParseUUID(req.RootCommentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid root_comment_id")
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start fresh")
		return
	}
	defer tx.Rollback(r.Context())

	// The target must be a root comment (a thread) on this issue.
	var isRoot bool
	if err := tx.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM comment WHERE id = $1 AND issue_id = $2 AND parent_id IS NULL)`,
		rootID, issue.ID).Scan(&isRoot); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load thread")
		return
	}
	if !isRoot {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}

	// Handoff: agent brief if supplied, else an auto-summary of the thread.
	handoff := req.Handoff
	agentAuthored := handoff != nil
	if handoff == nil {
		handoff, err = h.generateHandoff(r.Context(), tx, issue.ID, rootID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to summarise thread")
			return
		}
	} else {
		handoff = normalizeHandoff(handoff)
	}

	// Upsert the metadata row for this thread, preserving any name a human or an
	// earlier agent set unless this brief carries a fresh one.
	name := sessionNameFromHandoff(handoff, agentAuthored)
	session, err := upsertThreadSession(r.Context(), tx, issue.ID, rootID, name, handoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write handoff")
		return
	}

	// Close the session = resolve its thread root (FIR-1874 point 3, B-100%).
	// Idempotent: a re-handoff keeps the original resolver/time. Resolver
	// preference: existing resolver > acting member > the thread author. The
	// final fallback to author_type/author_id (both NOT NULL) is what keeps the
	// resolve consistent with the comment_resolved_consistency CHECK when there
	// is no member in context — e.g. an agent-triggered Handoff, where
	// actorFromContext yields no member and $3/$4 are NULL.
	actorType, actorID := actorFromContext(r.Context())
	if _, err := tx.Exec(r.Context(), `
		UPDATE comment SET
			resolved_at = COALESCE(resolved_at, now()),
			resolved_by_type = COALESCE(resolved_by_type, $3, author_type),
			resolved_by_id = COALESCE(resolved_by_id, $4, author_id),
			updated_at = CASE WHEN resolved_at IS NULL THEN now() ELSE updated_at END
		WHERE id = $1 AND issue_id = $2`,
		rootID, issue.ID, nullText(actorType), actorID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve thread")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start fresh")
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

// generateHandoff auto-summarises a THREAD (its root comment + replies) when no
// agent brief is supplied. Deterministic, no LLM: it captures how many comments
// the thread held and the most recent one, so the handoff carries real context.
func (h *Handler) generateHandoff(ctx context.Context, tx pgx.Tx, issueID, rootID pgtype.UUID) (*handoffBrief, error) {
	var count int
	var latest *string
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::int,
		       (array_agg(content ORDER BY created_at DESC) FILTER (WHERE content IS NOT NULL))[1]
		FROM comment
		WHERE issue_id = $1
		  AND type = 'comment'
		  AND (id = $2 OR parent_id = $2)`, issueID, rootID).Scan(&count, &latest)
	if err != nil {
		return nil, err
	}
	brief := &handoffBrief{Done: []string{}, Remaining: []string{}}
	if count == 0 {
		brief.Summary = "Auto-generated handoff: the closed session had no comments."
		return brief, nil
	}
	plural := "s"
	if count == 1 {
		plural = ""
	}
	summary := "Auto-generated handoff: " + strconv.Itoa(count) + " comment" + plural + " in the closed session."
	if latest != nil {
		if body := handoffBody(*latest, 8000); body != "" {
			summary += "\n\nLatest comment:\n" + body
		}
	}
	brief.Summary = summary
	return brief, nil
}

// upsertThreadSession writes (or updates) the metadata row for a thread root.
// An empty name never clobbers an existing one; a non-empty name (from an agent
// brief) wins. Handoff is always replaced with the supplied value.
func upsertThreadSession(ctx context.Context, tx pgx.Tx, issueID, rootID pgtype.UUID, name string, handoff *handoffBrief) (sessionResponse, error) {
	raw, err := marshalHandoff(handoff)
	if err != nil {
		return sessionResponse{}, err
	}
	var existingID pgtype.UUID
	var existingName string
	err = tx.QueryRow(ctx, `SELECT id, name FROM cerebro_session WHERE issue_id = $1 AND root_comment_id = $2`, issueID, rootID).Scan(&existingID, &existingName)
	switch err {
	case nil:
		finalName := existingName
		if name != "" {
			finalName = name
		}
		// COALESCE so a rename (nil handoff) never wipes an existing handoff; a
		// non-nil handoff replaces it.
		row := tx.QueryRow(ctx, `
			UPDATE cerebro_session
			SET name = $1, handoff = COALESCE($2::jsonb, handoff), updated_at = now()
			WHERE id = $3 AND issue_id = $4
			RETURNING id, issue_id, root_comment_id, position, name, handoff, created_at, updated_at`,
			finalName, raw, existingID, issueID)
		return scanSession(row)
	case pgx.ErrNoRows:
		row := tx.QueryRow(ctx, `
			INSERT INTO cerebro_session (issue_id, root_comment_id, position, name, handoff)
			VALUES ($1, $2, 0, $3, $4::jsonb)
			RETURNING id, issue_id, root_comment_id, position, name, handoff, created_at, updated_at`,
			issueID, rootID, name, raw)
		return scanSession(row)
	default:
		return sessionResponse{}, err
	}
}

// sessionNameFromHandoff derives a short display name from an AGENT-authored
// brief's summary (first line, capped). Server auto-summaries return "" so the
// frontend's default "Session N" stays.
func sessionNameFromHandoff(brief *handoffBrief, agentAuthored bool) string {
	if !agentAuthored || brief == nil {
		return ""
	}
	s := strings.TrimSpace(brief.Summary)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return snippet(s, 60)
}

func handoffBody(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "…"
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

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (sessionResponse, error) {
	var id, issueID, rootID pgtype.UUID
	var raw []byte
	out := sessionResponse{}
	if err := row.Scan(&id, &issueID, &rootID, &out.Position, &out.Name, &raw, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return sessionResponse{}, err
	}
	out.ID = util.UUIDToString(id)
	out.IssueID = util.UUIDToString(issueID)
	if rootID.Valid {
		s := util.UUIDToString(rootID)
		out.RootCommentID = &s
	}
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

// actorFromContext returns the acting member as (type, userID) for resolved_by,
// or ("", zero) for a non-member caller — the resolve then leaves resolved_by
// NULL, which the COALESCE in the UPDATE handles.
func actorFromContext(ctx context.Context) (string, pgtype.UUID) {
	if member, ok := middleware.MemberFromContext(ctx); ok && member.UserID.Valid {
		return "member", member.UserID
	}
	return "", pgtype.UUID{}
}

func nullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
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
