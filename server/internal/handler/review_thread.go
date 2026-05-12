package handler

// HTTP endpoints for walking and resolving CR review threads on an issue's PR.
//
// These are the surfaces the dev agent (Amelia) uses inside the BMAD fixing
// loop after a CR review lands with unresolved threads:
//
//   GET    /api/issues/{id}/review-threads?state=unresolved   List threads
//   POST   /api/issues/{id}/review-threads/{threadID}/reply   Post a reply
//   POST   /api/issues/{id}/review-threads/{threadID}/resolve Resolve thread
//
// All routes are mounted inside the workspace-membership middleware so the
// caller's identity is already validated. Mutation routes require an upstream
// ReviewActions service (GraphQL mutations against GitHub) which is wired in
// router.go from GITHUB_APP_* env vars; if it's nil the routes are not
// registered (same fail-open pattern as the inbound webhook).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	githubintegration "github.com/multica-ai/multica/server/internal/integrations/github"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// reviewThreadDTO is the JSON shape we return for a single thread row. It
// mirrors the local issue_review_thread columns the agent + UI need.
type reviewThreadDTO struct {
	ID              string  `json:"id"`
	IssueID         string  `json:"issue_id"`
	PrRepo          string  `json:"pr_repo"`
	PrNumber        int32   `json:"pr_number"`
	GhCommentID     int64   `json:"gh_comment_id"`
	GhThreadNodeID  string  `json:"gh_thread_node_id"`
	FilePath        string  `json:"file_path"`
	Line            *int32  `json:"line,omitempty"`
	Side            string  `json:"side,omitempty"`
	Severity        string  `json:"severity"`
	SeverityBadge   string  `json:"severity_badge"`
	EffortBadge     string  `json:"effort_badge"`
	Title           string  `json:"title"`
	Body            string  `json:"body"`
	AIPrompt        string  `json:"ai_prompt"`
	URL             string  `json:"url"`
	AuthorLogin     string  `json:"author_login"`
	State           string  `json:"state"`
	ResolvedByAgent *string `json:"resolved_by_agent,omitempty"`
	ResolvedAt      *string `json:"resolved_at,omitempty"`
	ProcessedAt     *string `json:"processed_by_resolver_at,omitempty"`
	ProcessedBy     *string `json:"processed_by_agent,omitempty"`
	ClaimedBy       *string `json:"claimed_by_agent,omitempty"`
	ClaimExpiresAt  *string `json:"claim_expires_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func reviewThreadToDTO(t db.IssueReviewThread) reviewThreadDTO {
	d := reviewThreadDTO{
		ID:            uuidToString(t.ID),
		IssueID:       uuidToString(t.IssueID),
		PrRepo:        t.PrRepo,
		PrNumber:      t.PrNumber,
		GhCommentID:   t.GhCommentID,
		FilePath:      t.FilePath,
		Severity:      t.Severity,
		SeverityBadge: t.SeverityBadge,
		EffortBadge:   t.EffortBadge,
		Title:         t.Title,
		Body:          t.Body,
		AIPrompt:      t.AiPrompt,
		URL:           t.Url,
		AuthorLogin:   t.AuthorLogin,
		State:         t.State,
		CreatedAt:     timestampToString(t.CreatedAt),
		UpdatedAt:     timestampToString(t.UpdatedAt),
	}
	if t.GhThreadNodeID.Valid {
		d.GhThreadNodeID = t.GhThreadNodeID.String
	}
	if t.Line.Valid {
		v := t.Line.Int32
		d.Line = &v
	}
	if t.Side.Valid {
		d.Side = t.Side.String
	}
	if t.ResolvedByAgent.Valid {
		s := uuidToString(t.ResolvedByAgent)
		d.ResolvedByAgent = &s
	}
	if t.ResolvedAt.Valid {
		s := timestampToString(t.ResolvedAt)
		d.ResolvedAt = &s
	}
	if t.ProcessedByResolverAt.Valid {
		s := timestampToString(t.ProcessedByResolverAt)
		d.ProcessedAt = &s
	}
	if t.ProcessedByAgent.Valid {
		s := uuidToString(t.ProcessedByAgent)
		d.ProcessedBy = &s
	}
	if t.ClaimedByAgent.Valid {
		s := uuidToString(t.ClaimedByAgent)
		d.ClaimedBy = &s
	}
	if t.ClaimExpiresAt.Valid {
		s := timestampToString(t.ClaimExpiresAt)
		d.ClaimExpiresAt = &s
	}
	return d
}

func claimedThreadToDTO(t db.ClaimNextUnprocessedThreadRow) reviewThreadDTO {
	thread := db.IssueReviewThread{
		ID:                    t.ID,
		WorkspaceID:           t.WorkspaceID,
		IssueID:               t.IssueID,
		PrRepo:                t.PrRepo,
		PrNumber:              t.PrNumber,
		GhCommentID:           t.GhCommentID,
		GhThreadNodeID:        t.GhThreadNodeID,
		FilePath:              t.FilePath,
		Line:                  t.Line,
		Side:                  t.Side,
		Severity:              t.Severity,
		Title:                 t.Title,
		Body:                  t.Body,
		Url:                   t.Url,
		AuthorLogin:           t.AuthorLogin,
		State:                 t.State,
		ResolvedByAgent:       t.ResolvedByAgent,
		ResolvedAt:            t.ResolvedAt,
		CreatedAt:             t.CreatedAt,
		UpdatedAt:             t.UpdatedAt,
		SeverityBadge:         t.SeverityBadge,
		EffortBadge:           t.EffortBadge,
		AiPrompt:              t.AiPrompt,
		ProcessedByResolverAt: t.ProcessedByResolverAt,
		ProcessedByAgent:      t.ProcessedByAgent,
		ClaimedByAgent:        t.ClaimedByAgent,
		ClaimExpiresAt:        t.ClaimExpiresAt,
	}
	return reviewThreadToDTO(thread)
}

// ListReviewThreads handles GET /api/issues/{id}/review-threads.
//
// Query params:
//
//	state=unresolved   Only return rows where state='unresolved'.
//	                   Default (omitted) returns all threads on the issue.
//
// The id path param accepts either a UUID or an identifier ("TIM-11").
func (h *Handler) ListReviewThreads(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	state := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state")))

	var (
		threads []db.IssueReviewThread
		err     error
	)
	if state == "unresolved" {
		threads, err = h.Queries.ListUnresolvedReviewThreadsByIssue(r.Context(), issue.ID)
	} else {
		threads, err = h.Queries.ListReviewThreadsByIssue(r.Context(), issue.ID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list review threads")
		return
	}

	// Lazy backfill: if any returned threads are missing gh_thread_node_id,
	// the dev agent won't be able to reply/resolve them. Run a one-shot
	// GraphQL backfill against the PR and refetch. This makes the LIST
	// endpoint self-healing for the legacy gap where review-comment
	// webhooks didn't carry the node_id.
	if h.ReviewActions != nil && issue.PrRepo.Valid && issue.PrNumber.Valid {
		needsBackfill := false
		for _, t := range threads {
			if !t.GhThreadNodeID.Valid || t.GhThreadNodeID.String == "" {
				needsBackfill = true
				break
			}
		}
		if needsBackfill {
			// Inline (silent) binding lookup — we do NOT use resolveBindingForIssue
			// here because that helper writes 4xx responses on failure, which
			// would clobber the LIST response.
			binding, berr := h.Queries.GetRepoBindingByRepo(r.Context(), issue.PrRepo.String)
			if berr == nil {
				if _, ferr := h.ReviewActions.BackfillThreadNodeIDs(r.Context(), binding, issue.PrNumber.Int32); ferr == nil {
					if state == "unresolved" {
						threads, _ = h.Queries.ListUnresolvedReviewThreadsByIssue(r.Context(), issue.ID)
					} else {
						threads, _ = h.Queries.ListReviewThreadsByIssue(r.Context(), issue.ID)
					}
				}
				// On backfill error: best-effort — keep the original list. The
				// caller will see empty thread_node_id values and can retry.
			}
		}
	}

	out := make([]reviewThreadDTO, 0, len(threads))
	for _, t := range threads {
		out = append(out, reviewThreadToDTO(t))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id":     uuidToString(issue.ID),
		"state_filter": state,
		"threads":      out,
		"total":        len(out),
	})
}

// loadThreadForIssue fetches a single thread by UUID and confirms it belongs
// to the issue path-param. Returns ok=false on any failure (already wrote
// the response).
func (h *Handler) loadThreadForIssue(w http.ResponseWriter, r *http.Request, issueID pgtype.UUID, threadID string) (db.IssueReviewThread, bool) {
	tid := parseUUID(threadID)
	if !tid.Valid {
		writeError(w, http.StatusBadRequest, "invalid thread id")
		return db.IssueReviewThread{}, false
	}
	thread, err := h.Queries.GetReviewThreadInIssue(r.Context(), db.GetReviewThreadInIssueParams{
		ID:      tid,
		IssueID: issueID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "review thread not found on this issue")
			return db.IssueReviewThread{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load review thread")
		return db.IssueReviewThread{}, false
	}
	return thread, true
}

// resolveBindingForIssue looks up the workspace_repo_binding for the issue's
// PR repo. Returns ok=false (and writes the response) when the issue has no
// PR or when the binding is missing.
func (h *Handler) resolveBindingForIssue(w http.ResponseWriter, r *http.Request, issue db.Issue) (db.WorkspaceRepoBinding, bool) {
	if !issue.PrRepo.Valid || issue.PrRepo.String == "" {
		writeError(w, http.StatusBadRequest, "issue has no associated pull request")
		return db.WorkspaceRepoBinding{}, false
	}
	binding, err := h.Queries.GetRepoBindingByRepo(r.Context(), issue.PrRepo.String)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no repo binding for "+issue.PrRepo.String)
			return db.WorkspaceRepoBinding{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load repo binding")
		return db.WorkspaceRepoBinding{}, false
	}
	return binding, true
}

// agentIDFromRequest returns the agent UUID stamped on the X-Agent-ID header,
// when present and well-formed. Used to attribute thread resolutions to the
// dev agent that made the call.
func agentIDFromRequest(r *http.Request) pgtype.UUID {
	id := r.Header.Get("X-Agent-ID")
	if id == "" {
		return pgtype.UUID{}
	}
	return parseUUID(id)
}

func (h *Handler) resolveAgentActor(w http.ResponseWriter, r *http.Request, userID string, workspaceID pgtype.UUID) (pgtype.UUID, bool) {
	actorType, actorIDStr := h.resolveActor(r, userID, uuidToString(workspaceID))
	if actorType != "agent" {
		writeError(w, http.StatusUnauthorized, "agent identity required (set X-Agent-ID)")
		return pgtype.UUID{}, false
	}
	actorAgentID := parseUUID(actorIDStr)
	if !actorAgentID.Valid {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return pgtype.UUID{}, false
	}
	return actorAgentID, true
}

type claimRequest struct {
	ClaimTTLSecs int `json:"claim_ttl_secs,omitempty"`
}

func (h *Handler) ClaimNextResolverThread(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	actorAgentID, ok := h.resolveAgentActor(w, r, userID, issue.WorkspaceID)
	if !ok {
		return
	}
	var req claimRequest
	if r.Body != nil && r.Body != http.NoBody {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	ttl := req.ClaimTTLSecs
	if ttl <= 0 || ttl > 3600 {
		ttl = 900
	}
	row, err := h.Queries.ClaimNextUnprocessedThread(r.Context(), db.ClaimNextUnprocessedThreadParams{
		IssueID:        issue.ID,
		ClaimedByAgent: actorAgentID,
		ClaimTtlSecs:   int32(ttl),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to claim review thread")
		return
	}
	writeJSON(w, http.StatusOK, claimedThreadToDTO(row))
}

func (h *Handler) ReleaseThreadClaim(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	thread, ok := h.loadThreadForIssue(w, r, issue.ID, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}
	actorAgentID, ok := h.resolveAgentActor(w, r, userID, issue.WorkspaceID)
	if !ok {
		return
	}
	_ = h.Queries.ReleaseThreadClaim(r.Context(), db.ReleaseThreadClaimParams{
		ID:             thread.ID,
		ClaimedByAgent: actorAgentID,
	})
	w.WriteHeader(http.StatusNoContent)
}

type processThreadRequest struct {
	Content string `json:"content"`
}

func (h *Handler) ProcessReviewThread(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	thread, ok := h.loadThreadForIssue(w, r, issue.ID, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}

	var req processThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	actorAgentID, ok := h.resolveAgentActor(w, r, userID, issue.WorkspaceID)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.MarkThreadProcessedByResolver(r.Context(), db.MarkThreadProcessedByResolverParams{
		ID:               thread.ID,
		ProcessedByAgent: actorAgentID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "thread cannot be processed: already processed, claim expired, or held by a different agent")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to mark thread processed")
		return
	}

	parentID, err := qtx.GetParentCRReviewCommentForThread(r.Context(), thread.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no cr_review_comment parent for thread")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load parent comment")
		return
	}

	comment, err := qtx.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    actorAgentID,
		Content:     req.Content,
		Type:        "fixer_reply",
		ParentID:    parentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create fixer reply")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"thread":  reviewThreadToDTO(updated),
		"comment": comment,
	})
}

func (h *Handler) GetNextUnpostedThread(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListThreadsWithUnpostedFixerReplies(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list unposted fixer replies")
		return
	}
	if len(rows) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	row := rows[0]
	out := map[string]any{
		"thread_id":              uuidToString(row.ThreadID),
		"gh_thread_node_id":      textToPtr(row.GhThreadNodeID),
		"thread_state":           row.ThreadState,
		"severity":               row.Severity,
		"file_path":              row.FilePath,
		"fixer_reply_comment_id": uuidToString(row.FixerReplyCommentID),
		"reply_body":             row.ReplyBody,
	}
	if row.Line.Valid {
		out["line"] = row.Line.Int32
	}
	writeJSON(w, http.StatusOK, out)
}

type markRepliedRequest struct {
	FixerReplyCommentID string `json:"fixer_reply_comment_id"`
}

func (h *Handler) MarkThreadReplyPosted(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	thread, ok := h.loadThreadForIssue(w, r, issue.ID, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}
	if _, ok := h.resolveAgentActor(w, r, userID, issue.WorkspaceID); !ok {
		return
	}

	var body markRepliedRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.FixerReplyCommentID) == "" {
		writeError(w, http.StatusBadRequest, "fixer_reply_comment_id is required")
		return
	}
	postedAt := h.maybeMarkFixerReplyPosted(r.Context(), issue.ID, body.FixerReplyCommentID, thread.ID)
	if postedAt == "" {
		writeError(w, http.StatusNotFound, "fixer_reply not found or does not belong to this thread")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"fixer_reply_posted_at": postedAt})
}

type dismissResponse struct {
	Dismissed         bool   `json:"dismissed"`
	DismissedReviewID string `json:"dismissed_review_id,omitempty"`
}

func (h *Handler) DismissPriorCRChangesRequested(w http.ResponseWriter, r *http.Request) {
	if h.ReviewActions == nil {
		writeError(w, http.StatusServiceUnavailable, "review actions disabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.resolveAgentActor(w, r, userID, issue.WorkspaceID); !ok {
		return
	}
	binding, ok := h.resolveBindingForIssue(w, r, issue)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), githubintegration.DefaultActionTimeout)
	defer cancel()
	res, err := h.ReviewActions.DismissPriorCRChangesRequested(ctx, binding, issue)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dismissResponse{
		Dismissed:         res.Dismissed,
		DismissedReviewID: res.ReviewID,
	})
}

type replyRequest struct {
	Content string `json:"content"`
	// Optional: when set, the handler stamps `comment.posted_to_github_at`
	// on the named fixer_reply row after the GitHub-side reply succeeds.
	// Marcus's bmad-pr-resolve skill passes this so the UI can flip its
	// "Pending" pill to "Posted" without polling. Server validates that
	// the comment exists, has type='fixer_reply', and belongs to this issue.
	FixerReplyCommentID string `json:"fixer_reply_comment_id,omitempty"`
}

// ReplyToReviewThread handles POST /api/issues/{id}/review-threads/{threadID}/reply.
//
// Body: {"content": "..."}
//
// Posts the reply via GraphQL addPullRequestReviewThreadReply. The reply does
// NOT mark the thread resolved — callers must follow up with a resolve call
// if they want to drop the unresolved count.
func (h *Handler) ReplyToReviewThread(w http.ResponseWriter, r *http.Request) {
	if h.ReviewActions == nil {
		writeError(w, http.StatusServiceUnavailable, "review actions disabled (GITHUB_APP_* env not configured)")
		return
	}

	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	thread, ok := h.loadThreadForIssue(w, r, issue.ID, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}

	var body replyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if body.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	binding, ok := h.resolveBindingForIssue(w, r, issue)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), githubintegration.DefaultActionTimeout)
	defer cancel()

	res, err := h.ReviewActions.ReplyToReviewThread(ctx, binding, thread, body.Content)
	if err != nil {
		writeError(w, http.StatusBadGateway, "github reply failed: "+err.Error())
		return
	}

	// Optional: stamp posted_to_github_at on the matching fixer_reply row.
	// Best-effort: a failed mark doesn't unwind the GitHub-side success;
	// the next call to /reply for the same thread is idempotent (the
	// MarkFixerReplyPosted query uses COALESCE so a second stamp is a
	// no-op). The UI will heal the pill on the next list refresh.
	postedAt := h.maybeMarkFixerReplyPosted(ctx, issue.ID, body.FixerReplyCommentID, thread.ID)

	resp := map[string]any{
		"thread_id":   uuidToString(thread.ID),
		"comment_id":  res.CommentID,
		"comment_url": res.CommentURL,
	}
	if postedAt != "" {
		resp["fixer_reply_posted_at"] = postedAt
	}
	writeJSON(w, http.StatusOK, resp)
}

// maybeMarkFixerReplyPosted stamps comment.posted_to_github_at on the named
// fixer_reply row when the caller passed `fixer_reply_comment_id` in the
// reply request body. Returns the timestamp as RFC3339 or "" on any failure
// (logged; never propagated as an error). Validates that the row exists,
// has type='fixer_reply', and that its parent's review_thread_id matches
// the thread we just replied on — defense against a caller passing a
// fixer_reply id that doesn't belong to this thread.
func (h *Handler) maybeMarkFixerReplyPosted(ctx context.Context, issueID pgtype.UUID, fixerReplyID string, threadID pgtype.UUID) string {
	if fixerReplyID == "" {
		return ""
	}
	cid := parseUUID(fixerReplyID)
	if !cid.Valid {
		slog.Warn("fixer_reply mark: invalid uuid", "id", fixerReplyID)
		return ""
	}
	c, err := h.Queries.GetComment(ctx, cid)
	if err != nil {
		slog.Warn("fixer_reply mark: lookup failed", "id", fixerReplyID, "error", err)
		return ""
	}
	if uuidToString(c.IssueID) != uuidToString(issueID) {
		slog.Warn("fixer_reply mark: cross-issue", "comment_issue", uuidToString(c.IssueID), "request_issue", uuidToString(issueID))
		return ""
	}
	if c.Type != "fixer_reply" {
		slog.Warn("fixer_reply mark: wrong type", "type", c.Type, "id", fixerReplyID)
		return ""
	}
	// Walk up to the parent and confirm its review_thread_id matches.
	if !c.ParentID.Valid {
		slog.Warn("fixer_reply mark: no parent", "id", fixerReplyID)
		return ""
	}
	parent, err := h.Queries.GetComment(ctx, c.ParentID)
	if err != nil {
		slog.Warn("fixer_reply mark: parent lookup failed", "id", fixerReplyID, "error", err)
		return ""
	}
	if !parent.ReviewThreadID.Valid || uuidToString(parent.ReviewThreadID) != uuidToString(threadID) {
		slog.Warn("fixer_reply mark: parent's review_thread_id does not match",
			"parent_thread", uuidToString(parent.ReviewThreadID),
			"reply_thread", uuidToString(threadID))
		return ""
	}
	updated, err := h.Queries.MarkFixerReplyPosted(ctx, cid)
	if err != nil {
		slog.Warn("fixer_reply mark: stamp failed", "id", fixerReplyID, "error", err)
		return ""
	}
	if !updated.PostedToGithubAt.Valid {
		return ""
	}
	return timestampToString(updated.PostedToGithubAt)
}

type resolveRequest struct {
	// Optional: when set, the handler posts a reply first and then resolves.
	// Lets the dev agent do "explain + resolve" in a single round-trip.
	Reply string `json:"reply,omitempty"`
	// Optional: when set alongside `reply`, the handler stamps
	// `comment.posted_to_github_at` on the named fixer_reply row after the
	// GitHub reply succeeds. Same semantics as replyRequest.FixerReplyCommentID.
	FixerReplyCommentID string `json:"fixer_reply_comment_id,omitempty"`
}

// ResolveReviewThread handles POST /api/issues/{id}/review-threads/{threadID}/resolve.
//
// Body: {"reply": "..."}  (optional)
//
// If a reply is provided, posts it first via addPullRequestReviewThreadReply,
// then resolves the thread via resolveReviewThread. The local
// issue_review_thread row is mirrored to state='resolved' immediately so the
// state machine sees the count drop without waiting for the inbound webhook.
func (h *Handler) ResolveReviewThread(w http.ResponseWriter, r *http.Request) {
	if h.ReviewActions == nil {
		writeError(w, http.StatusServiceUnavailable, "review actions disabled (GITHUB_APP_* env not configured)")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	thread, ok := h.loadThreadForIssue(w, r, issue.ID, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}

	_ = userID // used by resolveAgentActor in the reply branch below

	// Body is optional — fall through to resolve-only when missing/empty.
	var body resolveRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	binding, ok := h.resolveBindingForIssue(w, r, issue)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*githubintegration.DefaultActionTimeout)
	defer cancel()

	resp := map[string]any{
		"thread_id": uuidToString(thread.ID),
	}

	if reply := strings.TrimSpace(body.Reply); reply != "" {
		// NEW: if no FixerReplyCommentID supplied, create the fixer_reply row first
		// so the local DB mirrors what we're about to post to GitHub.
		fixerReplyID := body.FixerReplyCommentID
		if fixerReplyID == "" {
			parentID, err := h.Queries.GetParentCRReviewCommentForThread(ctx, thread.ID)
			if err == nil {
				actorAgentID, _ := h.resolveAgentActor(w, r, userID, issue.WorkspaceID)
				created, cerr := h.Queries.CreateComment(ctx, db.CreateCommentParams{
					IssueID:     issue.ID,
					WorkspaceID: issue.WorkspaceID,
					AuthorType:  "agent",
					AuthorID:    actorAgentID,
					Content:     reply,
					Type:        "fixer_reply",
					ParentID:    parentID,
				})
				if cerr == nil {
					fixerReplyID = uuidToString(created.ID)
				}
			}
		}
		
		replyRes, err := h.ReviewActions.ReplyToReviewThread(ctx, binding, thread, reply)
		if err != nil {
			writeError(w, http.StatusBadGateway, "github reply failed: "+err.Error())
			return
		}
		resp["comment_id"] = replyRes.CommentID
		resp["comment_url"] = replyRes.CommentURL
		resp["fixer_reply_id"] = fixerReplyID  // NEW: expose to caller
		if postedAt := h.maybeMarkFixerReplyPosted(ctx, issue.ID, fixerReplyID, thread.ID); postedAt != "" {
			resp["fixer_reply_posted_at"] = postedAt
		}
	}

	resolveRes, err := h.ReviewActions.ResolveReviewThread(ctx, binding, thread, agentIDFromRequest(r))
	if err != nil {
		// resolve_actions.ResolveReviewThread returns a non-nil ResolveResult
		// alongside the error when GitHub accepted the resolve but the local
		// mirror failed. We surface success in that case (the webhook
		// redelivery will heal the local row) but include a warning.
		if resolveRes != nil && resolveRes.Resolved {
			resp["resolved"] = true
			resp["thread_node_id"] = resolveRes.ThreadNodeID
			resp["warning"] = "github accepted resolve but local mirror failed: " + err.Error()
			writeJSON(w, http.StatusOK, resp)
			return
		}
		writeError(w, http.StatusBadGateway, "github resolve failed: "+err.Error())
		return
	}
	resp["resolved"] = resolveRes.Resolved
	resp["thread_node_id"] = resolveRes.ThreadNodeID

	writeJSON(w, http.StatusOK, resp)
}

// _ keeps time import used even if all timeouts are pulled from the package
// constant; defensive against future refactors that add per-handler timeouts.
var _ = time.Second
