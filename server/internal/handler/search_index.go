package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Local search index sync (MUL-7754).
//
// Web and Desktop keep a per-workspace copy of issue, comment, and project text
// and search it locally. A client bootstraps with GET /manifest (which pins the
// snapshot the copy starts from) followed by GET /snapshot pages, then catches
// up with POST /changes. Catch-up reads search_index_change (migration 561):
// an entity is returned when its latest write committed in the target snapshot
// but not in the snapshot the client already holds. Comparing xids against
// pg_snapshot values is exact under any commit order and is not held back by
// long-running transactions elsewhere in the database.
//
// Every read runs on the primary: a replica would hand out a snapshot and rows
// from different points in time, and the difference would be lost silently.
const (
	searchIndexSnapshotDefaultLimit = 200
	searchIndexSnapshotMaxLimit     = 500
	searchIndexChangesDefaultLimit  = 500
	searchIndexChangesMaxLimit      = 1000
	// A pg_snapshot lists every in-progress xid, so its text grows with write
	// concurrency. This bounds a cursor far above any realistic snapshot.
	searchIndexCursorMaxBytes = 256 << 10
	searchIndexCursorVersion  = 1
	searchIndexStatementLimit = 15 * time.Second
)

// searchIndexBootstrapSlots bounds the manifest and snapshot reads one API
// process runs at once. Every client builds its first copy with these, and they
// all start together right after a release or an outage; without a bound that
// herd would take most of the connection pool. Callers that cannot get a slot
// soon answer 503 with Retry-After, and clients back off with jitter.
var searchIndexBootstrapSlots = make(chan struct{}, 4)

const searchIndexBootstrapWait = 2 * time.Second

func acquireSearchIndexBootstrapSlot(ctx context.Context) (release func(), ok bool) {
	timer := time.NewTimer(searchIndexBootstrapWait)
	defer timer.Stop()
	select {
	case searchIndexBootstrapSlots <- struct{}{}:
		return func() { <-searchIndexBootstrapSlots }, true
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}

func writeSearchIndexBusy(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "5")
	writeError(w, http.StatusServiceUnavailable, "search index is busy; retry later")
}

var pgSnapshotTextRe = regexp.MustCompile(`^[0-9]{1,20}:[0-9]{1,20}:([0-9]{1,20}(,[0-9]{1,20})*)?$`)

// validPGSnapshotText applies pg_snapshot's own input rules. Postgres only
// checks them when a plan happens to evaluate the cast, which an empty range
// can skip, so a bad cursor must be rejected here.
func validPGSnapshotText(s string) bool {
	if !pgSnapshotTextRe.MatchString(s) {
		return false
	}
	parts := strings.SplitN(s, ":", 3)
	xmin, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || xmin == 0 {
		return false
	}
	xmax, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || xmin > xmax {
		return false
	}
	if parts[2] == "" {
		return true
	}
	last := xmin
	for _, raw := range strings.Split(parts[2], ",") {
		xip, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || xip < last || xip >= xmax {
			return false
		}
		last = xip
	}
	return true
}

// searchIndexCursor is opaque to clients. Since is the snapshot whose changes
// the client already holds. Target and the After* keyset are set only while a
// catch-up spans several pages; once it completes, Target becomes the next
// Since.
type searchIndexCursor struct {
	Version   int    `json:"v"`
	Since     string `json:"s"`
	Target    string `json:"t,omitempty"`
	AfterXid  string `json:"x,omitempty"`
	AfterType string `json:"e,omitempty"`
	AfterID   string `json:"i,omitempty"`
}

func encodeSearchIndexCursor(c searchIndexCursor) string {
	c.Version = searchIndexCursorVersion
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSearchIndexCursor(s string) (searchIndexCursor, error) {
	var c searchIndexCursor
	if s == "" || len(s) > searchIndexCursorMaxBytes {
		return c, errors.New("invalid cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, errors.New("invalid cursor")
	}
	if err := json.Unmarshal(raw, &c); err != nil || c.Version != searchIndexCursorVersion {
		return c, errors.New("invalid cursor")
	}
	if !validPGSnapshotText(c.Since) {
		return c, errors.New("invalid cursor")
	}
	if c.Target == "" {
		if c.AfterXid != "" || c.AfterType != "" || c.AfterID != "" {
			return c, errors.New("invalid cursor")
		}
		return c, nil
	}
	if !validPGSnapshotText(c.Target) {
		return c, errors.New("invalid cursor")
	}
	if _, err := strconv.ParseUint(c.AfterXid, 10, 64); err != nil {
		return c, errors.New("invalid cursor")
	}
	switch c.AfterType {
	case "issue", "comment", "project":
	default:
		return c, errors.New("invalid cursor")
	}
	if _, err := parseUUIDStrict(c.AfterID); err != nil {
		return c, errors.New("invalid cursor")
	}
	return c, nil
}

func parseUUIDStrict(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, err
	}
	if !u.Valid {
		return u, errors.New("invalid uuid")
	}
	return u, nil
}

// SearchIndexIssue is the issue record a client indexes: the same shape the
// search endpoint returns, plus the sub-second updated_at that server-side
// ranking breaks ties with (IssueResponse.UpdatedAt is second-precision).
type SearchIndexIssue struct {
	IssueResponse
	SearchUpdatedAt string `json:"search_updated_at"`
}

// SearchIndexProject is the project search result shape without the issue and
// resource counts, which change without a project write and are not shown in
// search results.
type SearchIndexProject struct {
	ProjectResponse
	SearchUpdatedAt string `json:"search_updated_at"`
}

type SearchIndexComment struct {
	ID        string `json:"id"`
	IssueID   string `json:"issue_id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

type SearchIndexManifestResponse struct {
	Cursor       string `json:"cursor"`
	IssueCount   int64  `json:"issue_count"`
	CommentCount int64  `json:"comment_count"`
	ProjectCount int64  `json:"project_count"`
	// TextBytes totals the UTF-8 bytes of every title, description, and live
	// comment, so a client can decline a workspace its memory budget cannot hold.
	TextBytes int64 `json:"text_bytes"`
}

type SearchIndexSnapshotResponse struct {
	Issues          []SearchIndexIssue   `json:"issues"`
	Comments        []SearchIndexComment `json:"comments"`
	Projects        []SearchIndexProject `json:"projects"`
	NextAfterNumber int32                `json:"next_after_number"`
	Done            bool                 `json:"done"`
}

type SearchIndexDeleted struct {
	Issues   []string `json:"issues"`
	Comments []string `json:"comments"`
	Projects []string `json:"projects"`
}

type SearchIndexChangesResponse struct {
	Issues   []SearchIndexIssue   `json:"issues"`
	Comments []SearchIndexComment `json:"comments"`
	Projects []SearchIndexProject `json:"projects"`
	Deleted  SearchIndexDeleted   `json:"deleted"`
	Cursor   string               `json:"cursor"`
	HasMore  bool                 `json:"has_more"`
}

// RequireLocalSearchIndex answers 404 while the local_search_index kill switch
// is off. Clients treat that like an older server without these endpoints and
// keep using server search.
func (h *Handler) RequireLocalSearchIndex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !featureflags.LocalSearchIndexEnabled(r.Context(), h.FeatureFlags) {
			writeError(w, http.StatusNotFound, "local search index is disabled")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// runSearchIndexRead runs fn in a read-only REPEATABLE READ transaction, so the
// snapshot returned by pg_current_snapshot() is exactly the one fn reads under.
func (h *Handler) runSearchIndexRead(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin search index tx: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Must be the first statement: the isolation level cannot change once the
	// transaction has taken its snapshot.
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"); err != nil {
		return fmt.Errorf("set search index isolation: %w", err)
	}
	timeoutMs := int(searchIndexStatementLimit / time.Millisecond)
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		return fmt.Errorf("set search index statement_timeout: %w", err)
	}
	if err := fn(h.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (h *Handler) writeSearchIndexError(w http.ResponseWriter, r *http.Request, op string, err error) {
	if isSearchStatementTimeout(err) {
		writeError(w, http.StatusServiceUnavailable, "search index read timed out; please retry")
		return
	}
	slog.Warn("search index read failed", append(logger.RequestAttrs(r), "op", op, "error", err)...)
	writeError(w, http.StatusInternalServerError, "failed to read search index")
}

// GetSearchIndexManifest returns the snapshot a new local copy starts from and
// the size of the text it would hold.
func (h *Handler) GetSearchIndexManifest(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	release, ok := acquireSearchIndexBootstrapSlot(r.Context())
	if !ok {
		writeSearchIndexBusy(w)
		return
	}
	defer release()
	var row db.GetSearchIndexManifestRow
	err := h.runSearchIndexRead(r.Context(), func(q *db.Queries) error {
		var err error
		row, err = q.GetSearchIndexManifest(r.Context(), wsUUID)
		return err
	})
	if err != nil {
		h.writeSearchIndexError(w, r, "manifest", err)
		return
	}
	writeJSON(w, http.StatusOK, SearchIndexManifestResponse{
		Cursor:       encodeSearchIndexCursor(searchIndexCursor{Since: row.Snapshot}),
		IssueCount:   row.IssueCount,
		CommentCount: row.CommentCount,
		ProjectCount: row.ProjectCount,
		TextBytes:    row.IssueBytes + row.CommentBytes + row.ProjectBytes,
	})
}

// GetSearchIndexSnapshot returns one page of issues ordered by number, every
// live comment on those issues, and (on the first page) every project.
func (h *Handler) GetSearchIndexSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	afterNumber := int64(0)
	if raw := r.URL.Query().Get("after_number"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || v < 0 {
			writeError(w, http.StatusBadRequest, "invalid after_number")
			return
		}
		afterNumber = v
	}
	limit := searchIndexSnapshotDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(v, searchIndexSnapshotMaxLimit)
	}
	release, ok := acquireSearchIndexBootstrapSlot(ctx)
	if !ok {
		writeSearchIndexBusy(w)
		return
	}
	defer release()

	var (
		issues   []db.Issue
		comments []db.ListSearchIndexCommentsByIssuesRow
		projects []db.Project
	)
	err := h.runSearchIndexRead(ctx, func(q *db.Queries) error {
		var err error
		issues, err = q.ListSearchIndexIssuesPage(ctx, db.ListSearchIndexIssuesPageParams{
			WorkspaceID: wsUUID,
			AfterNumber: int32(afterNumber),
			PageLimit:   int32(limit),
		})
		if err != nil {
			return err
		}
		if len(issues) > 0 {
			ids := make([]pgtype.UUID, len(issues))
			for i, is := range issues {
				ids[i] = is.ID
			}
			comments, err = q.ListSearchIndexCommentsByIssues(ctx, db.ListSearchIndexCommentsByIssuesParams{
				WorkspaceID: wsUUID,
				IssueIds:    ids,
			})
			if err != nil {
				return err
			}
		}
		if afterNumber == 0 {
			projects, err = q.ListSearchIndexProjects(ctx, wsUUID)
		}
		return err
	})
	if err != nil {
		h.writeSearchIndexError(w, r, "snapshot", err)
		return
	}

	resp := SearchIndexSnapshotResponse{
		Issues:          h.searchIndexIssues(ctx, wsUUID, issues),
		Comments:        make([]SearchIndexComment, 0, len(comments)),
		Projects:        searchIndexProjects(projects),
		NextAfterNumber: int32(afterNumber),
		Done:            len(issues) < limit,
	}
	for _, c := range comments {
		resp.Comments = append(resp.Comments, searchIndexComment(c.ID, c.IssueID, c.Content, c.CreatedAt))
	}
	if n := len(issues); n > 0 {
		resp.NextAfterNumber = issues[n-1].Number
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListSearchIndexChanges returns the entities written after the cursor's
// snapshot: live rows as upserts, missing rows (and comment tombstones) as
// deletions. Returns 410 when retention pruning may have dropped a change the
// cursor has not seen; the client must then rebuild its copy.
func (h *Handler) ListSearchIndexChanges(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	var req struct {
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, searchIndexCursorMaxBytes+1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cur, err := decodeSearchIndexCursor(req.Cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}
	limit := searchIndexChangesDefaultLimit
	if req.Limit > 0 {
		limit = min(req.Limit, searchIndexChangesMaxLimit)
	}

	var (
		pruned   bool
		target   = cur.Target
		changes  []db.ListSearchIndexChangesRow
		issues   []db.Issue
		comments []db.ListSearchIndexCommentsByIDsRow
		projects []db.Project
	)
	err = h.runSearchIndexRead(ctx, func(q *db.Queries) error {
		var err error
		if pruned, err = q.SearchIndexSinceIsPruned(ctx, cur.Since); err != nil || pruned {
			return err
		}
		if target == "" {
			if target, err = q.GetSearchIndexCurrentSnapshot(ctx); err != nil {
				return err
			}
		}
		params := db.ListSearchIndexChangesParams{
			WorkspaceID:    wsUUID,
			SinceSnapshot:  cur.Since,
			TargetSnapshot: target,
			AfterXid:       "0",
			AfterType:      "",
			AfterID:        pgtype.UUID{Valid: true},
			PageLimit:      int32(limit + 1),
		}
		if cur.Target != "" {
			params.AfterXid = cur.AfterXid
			params.AfterType = cur.AfterType
			params.AfterID, _ = parseUUIDStrict(cur.AfterID)
		}
		if changes, err = q.ListSearchIndexChanges(ctx, params); err != nil {
			return err
		}
		if len(changes) > limit {
			changes = changes[:limit]
		}
		var issueIDs, commentIDs, projectIDs []pgtype.UUID
		for _, c := range changes {
			switch c.EntityType {
			case "issue":
				issueIDs = append(issueIDs, c.EntityID)
			case "comment":
				commentIDs = append(commentIDs, c.EntityID)
			case "project":
				projectIDs = append(projectIDs, c.EntityID)
			}
		}
		if len(issueIDs) > 0 {
			if issues, err = q.ListSearchIndexIssuesByIDs(ctx, db.ListSearchIndexIssuesByIDsParams{WorkspaceID: wsUUID, Ids: issueIDs}); err != nil {
				return err
			}
		}
		if len(commentIDs) > 0 {
			if comments, err = q.ListSearchIndexCommentsByIDs(ctx, db.ListSearchIndexCommentsByIDsParams{WorkspaceID: wsUUID, Ids: commentIDs}); err != nil {
				return err
			}
		}
		if len(projectIDs) > 0 {
			if projects, err = q.ListSearchIndexProjectsByIDs(ctx, db.ListSearchIndexProjectsByIDsParams{WorkspaceID: wsUUID, Ids: projectIDs}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			// A syntactically valid snapshot the server still rejects, e.g.
			// xmin greater than xmax.
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		h.writeSearchIndexError(w, r, "changes", err)
		return
	}
	if pruned {
		writeError(w, http.StatusGone, "search index cursor expired; rebuild the local index")
		return
	}

	resp := SearchIndexChangesResponse{
		Issues:   h.searchIndexIssues(ctx, wsUUID, issues),
		Comments: []SearchIndexComment{},
		Projects: searchIndexProjects(projects),
		Deleted: SearchIndexDeleted{
			Issues:   []string{},
			Comments: []string{},
			Projects: []string{},
		},
	}
	liveIssues := make(map[pgtype.UUID]bool, len(issues))
	for _, is := range issues {
		liveIssues[is.ID] = true
	}
	liveProjects := make(map[pgtype.UUID]bool, len(projects))
	for _, p := range projects {
		liveProjects[p.ID] = true
	}
	liveComments := make(map[pgtype.UUID]bool, len(comments))
	for _, c := range comments {
		if c.DeletedAt.Valid {
			continue
		}
		liveComments[c.ID] = true
		resp.Comments = append(resp.Comments, searchIndexComment(c.ID, c.IssueID, c.Content, c.CreatedAt))
	}
	for _, c := range changes {
		switch c.EntityType {
		case "issue":
			if !liveIssues[c.EntityID] {
				resp.Deleted.Issues = append(resp.Deleted.Issues, uuidToString(c.EntityID))
			}
		case "comment":
			if !liveComments[c.EntityID] {
				resp.Deleted.Comments = append(resp.Deleted.Comments, uuidToString(c.EntityID))
			}
		case "project":
			if !liveProjects[c.EntityID] {
				resp.Deleted.Projects = append(resp.Deleted.Projects, uuidToString(c.EntityID))
			}
		}
	}

	if n := len(changes); n == limit {
		// A full page may have more behind it. Keep the target snapshot fixed
		// until the keyset runs dry, then promote it to the next Since.
		last := changes[n-1]
		resp.HasMore = true
		resp.Cursor = encodeSearchIndexCursor(searchIndexCursor{
			Since:     cur.Since,
			Target:    target,
			AfterXid:  last.ChangeXid,
			AfterType: last.EntityType,
			AfterID:   uuidToString(last.EntityID),
		})
	} else {
		resp.Cursor = encodeSearchIndexCursor(searchIndexCursor{Since: target})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) searchIndexIssues(ctx context.Context, wsUUID pgtype.UUID, issues []db.Issue) []SearchIndexIssue {
	out := make([]SearchIndexIssue, 0, len(issues))
	if len(issues) == 0 {
		return out
	}
	prefix := h.getIssuePrefix(ctx, wsUUID)
	var originals []pgtype.UUID
	for _, is := range issues {
		originals = appendDuplicateOriginal(originals, is.Status, is.DuplicateOfIssueID)
	}
	fill := h.newStatusCategoryFiller(ctx, wsUUID, originals...)
	for _, is := range issues {
		resp := issueToResponse(is, prefix)
		fill(&resp)
		out = append(out, SearchIndexIssue{
			IssueResponse:   resp,
			SearchUpdatedAt: is.UpdatedAt.Time.UTC().Format(time.RFC3339Nano),
		})
	}
	return out
}

func searchIndexProjects(projects []db.Project) []SearchIndexProject {
	out := make([]SearchIndexProject, 0, len(projects))
	for _, p := range projects {
		out = append(out, SearchIndexProject{
			ProjectResponse: projectToResponse(p),
			SearchUpdatedAt: p.UpdatedAt.Time.UTC().Format(time.RFC3339Nano),
		})
	}
	return out
}

func searchIndexComment(id, issueID pgtype.UUID, content string, createdAt pgtype.Timestamptz) SearchIndexComment {
	return SearchIndexComment{
		ID:        uuidToString(id),
		IssueID:   uuidToString(issueID),
		Content:   content,
		CreatedAt: createdAt.Time.UTC().Format(time.RFC3339Nano),
	}
}
