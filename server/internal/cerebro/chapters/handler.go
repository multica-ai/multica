package chapters

import (
	"context"
	"encoding/json"
	"net/http"
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

type chapterResponse struct {
	ID               string    `json:"id"`
	IssueID          string    `json:"issue_id"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	Position         int32     `json:"position"`
	HandoffSummary   string    `json:"handoff_summary"`
	HandoffDone      []string  `json:"handoff_done"`
	HandoffRemaining []string  `json:"handoff_remaining"`
	PlanRef          *string   `json:"plan_ref"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type handoffRequest struct {
	Summary   string   `json:"summary"`
	Done      []string `json:"done"`
	Remaining []string `json:"remaining"`
	PlanRef   *string  `json:"plan_ref"`
}

type createRequest struct {
	Name    string          `json:"name"`
	Status  string          `json:"status"`
	Handoff *handoffRequest `json:"handoff"`
}

type updateRequest struct {
	Name     *string         `json:"name"`
	Status   *string         `json:"status"`
	Position *int32          `json:"position"`
	Handoff  *handoffRequest `json:"handoff"`
}

// startFreshRequest closes the open chapter with a handoff and opens the next
// one. Mode decides whether the next chapter carries the handoff forward
// ("handoff", the default) or starts as a blank session with no context
// ("blank"). The handoff is always recorded on the chapter being closed, so a
// blank start never loses the finished session's summary.
type startFreshRequest struct {
	handoffRequest
	Mode string `json:"mode"`
}

const (
	startModeHandoff = "handoff"
	startModeBlank   = "blank"
)

// resolveStartMode normalizes the requested start mode. An empty mode defaults
// to carrying the handoff forward. It returns ok=false for any unknown value so
// the handler can reject it with a 400 instead of silently picking a behavior.
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
		SELECT id, issue_id, name, status, position, handoff_summary, handoff_done,
		       handoff_remaining, plan_ref, created_at, updated_at
		FROM cerebro_chapters
		WHERE issue_id = $1
		ORDER BY position ASC, created_at ASC, id ASC`, issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chapters")
		return
	}
	defer rows.Close()
	out := []chapterResponse{}
	for rows.Next() {
		chapter, err := scanChapter(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read chapters")
			return
		}
		out = append(out, chapter)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	status := req.Status
	if status == "" {
		status = "in_progress"
	}
	if !validStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	handoff := normalizeHandoff(req.Handoff)
	chapter, err := h.createChapter(r.Context(), issue.ID, name, status, handoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create chapter")
		return
	}
	writeJSON(w, http.StatusCreated, chapter)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}
	chapterID, ok := parseUUIDParam(w, r, "chapterId")
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
		writeError(w, http.StatusInternalServerError, "failed to update chapter")
		return
	}
	defer tx.Rollback(r.Context())

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE cerebro_chapters SET name = $1, updated_at = now() WHERE id = $2 AND issue_id = $3`, name, chapterID, issue.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update chapter")
			return
		}
	}
	if req.Status != nil {
		if !validStatus(*req.Status) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE cerebro_chapters SET status = $1, updated_at = now() WHERE id = $2 AND issue_id = $3`, *req.Status, chapterID, issue.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update chapter")
			return
		}
	}
	if req.Position != nil {
		if _, err := tx.Exec(r.Context(), `UPDATE cerebro_chapters SET position = $1, updated_at = now() WHERE id = $2 AND issue_id = $3`, *req.Position, chapterID, issue.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update chapter")
			return
		}
	}
	if req.Handoff != nil {
		if err := updateHandoff(r.Context(), tx, issue.ID, chapterID, normalizeHandoff(req.Handoff)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update handoff")
			return
		}
	}
	chapter, err := getChapter(r.Context(), tx, issue.ID, chapterID)
	if err != nil {
		writeError(w, http.StatusNotFound, "chapter not found")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update chapter")
		return
	}
	writeJSON(w, http.StatusOK, chapter)
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

	var openID pgtype.UUID
	err = tx.QueryRow(r.Context(), `
		SELECT id FROM cerebro_chapters
		WHERE issue_id = $1 AND status <> 'done'
		ORDER BY position DESC, created_at DESC, id DESC
		LIMIT 1`, issue.ID).Scan(&openID)
	if err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to load open chapter")
		return
	}
	handoff := normalizeHandoff(&req.handoffRequest)
	if openID.Valid {
		if err := updateHandoff(r.Context(), tx, issue.ID, openID, handoff); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to write handoff")
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE cerebro_chapters SET status = 'done', updated_at = now() WHERE id = $1 AND issue_id = $2`, openID, issue.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to close chapter")
			return
		}
	}
	// The closing chapter always keeps the handoff (above). The NEW chapter only
	// inherits it when the user chose to carry it forward; a blank start opens an
	// empty session with no context.
	nextHandoff := handoff
	if mode == startModeBlank {
		nextHandoff = handoffRequest{Done: []string{}, Remaining: []string{}}
	}
	next, err := createChapterTx(r.Context(), tx, issue.ID, "", "in_progress", nextHandoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create next chapter")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start fresh")
		return
	}
	writeJSON(w, http.StatusCreated, next)
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

func (h *Handler) createChapter(ctx context.Context, issueID pgtype.UUID, name string, status string, handoff handoffRequest) (chapterResponse, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return chapterResponse{}, err
	}
	defer tx.Rollback(ctx)
	chapter, err := createChapterTx(ctx, tx, issueID, name, status, handoff)
	if err != nil {
		return chapterResponse{}, err
	}
	return chapter, tx.Commit(ctx)
}

func createChapterTx(ctx context.Context, tx pgx.Tx, issueID pgtype.UUID, name string, status string, handoff handoffRequest) (chapterResponse, error) {
	var position int32
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0)::int + 1 FROM cerebro_chapters WHERE issue_id = $1`, issueID).Scan(&position); err != nil {
		return chapterResponse{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = "Session " + strconvI32(position)
	}
	done, _ := json.Marshal(handoff.Done)
	remaining, _ := json.Marshal(handoff.Remaining)
	row := tx.QueryRow(ctx, `
		INSERT INTO cerebro_chapters (issue_id, name, status, position, handoff_summary, handoff_done, handoff_remaining, plan_ref)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8)
		RETURNING id, issue_id, name, status, position, handoff_summary, handoff_done, handoff_remaining, plan_ref, created_at, updated_at`,
		issueID, name, status, position, handoff.Summary, done, remaining, handoff.PlanRef)
	return scanChapter(row)
}

func updateHandoff(ctx context.Context, tx pgx.Tx, issueID pgtype.UUID, chapterID pgtype.UUID, handoff handoffRequest) error {
	done, _ := json.Marshal(handoff.Done)
	remaining, _ := json.Marshal(handoff.Remaining)
	_, err := tx.Exec(ctx, `
		UPDATE cerebro_chapters
		SET handoff_summary = $1, handoff_done = $2::jsonb, handoff_remaining = $3::jsonb, plan_ref = $4, updated_at = now()
		WHERE id = $5 AND issue_id = $6`, handoff.Summary, done, remaining, handoff.PlanRef, chapterID, issueID)
	return err
}

func getChapter(ctx context.Context, tx pgx.Tx, issueID pgtype.UUID, chapterID pgtype.UUID) (chapterResponse, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, issue_id, name, status, position, handoff_summary, handoff_done,
		       handoff_remaining, plan_ref, created_at, updated_at
		FROM cerebro_chapters
		WHERE id = $1 AND issue_id = $2`, chapterID, issueID)
	return scanChapter(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanChapter(row scanner) (chapterResponse, error) {
	var id, issueID pgtype.UUID
	var planRef *string
	var doneRaw, remainingRaw []byte
	out := chapterResponse{HandoffDone: []string{}, HandoffRemaining: []string{}}
	if err := row.Scan(&id, &issueID, &out.Name, &out.Status, &out.Position, &out.HandoffSummary, &doneRaw, &remainingRaw, &planRef, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return chapterResponse{}, err
	}
	_ = json.Unmarshal(doneRaw, &out.HandoffDone)
	_ = json.Unmarshal(remainingRaw, &out.HandoffRemaining)
	out.ID = util.UUIDToString(id)
	out.IssueID = util.UUIDToString(issueID)
	out.PlanRef = planRef
	return out, nil
}

func normalizeHandoff(in *handoffRequest) handoffRequest {
	if in == nil {
		return handoffRequest{Done: []string{}, Remaining: []string{}}
	}
	return handoffRequest{
		Summary:   strings.TrimSpace(in.Summary),
		Done:      compactStrings(in.Done),
		Remaining: compactStrings(in.Remaining),
		PlanRef:   trimPtr(in.PlanRef),
	}
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

func strconvI32(v int32) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	n := v
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
