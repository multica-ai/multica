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
// mirrors the local issue_review_thread columns the agent actually needs.
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
	Title           string  `json:"title"`
	Body            string  `json:"body"`
	URL             string  `json:"url"`
	AuthorLogin     string  `json:"author_login"`
	State           string  `json:"state"`
	ResolvedByAgent *string `json:"resolved_by_agent,omitempty"`
	ResolvedAt      *string `json:"resolved_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func reviewThreadToDTO(t db.IssueReviewThread) reviewThreadDTO {
	d := reviewThreadDTO{
		ID:           uuidToString(t.ID),
		IssueID:      uuidToString(t.IssueID),
		PrRepo:       t.PrRepo,
		PrNumber:     t.PrNumber,
		GhCommentID:  t.GhCommentID,
		FilePath:     t.FilePath,
		Severity:     t.Severity,
		Title:        t.Title,
		Body:         t.Body,
		URL:          t.Url,
		AuthorLogin:  t.AuthorLogin,
		State:        t.State,
		CreatedAt:    timestampToString(t.CreatedAt),
		UpdatedAt:    timestampToString(t.UpdatedAt),
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
	return d
}

// ListReviewThreads handles GET /api/issues/{id}/review-threads.
//
// Query params:
//   state=unresolved   Only return rows where state='unresolved'.
//                      Default (omitted) returns all threads on the issue.
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
	threads, err := h.Queries.ListReviewThreadsByIssue(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load review threads")
		return db.IssueReviewThread{}, false
	}
	for _, t := range threads {
		if uuidToString(t.ID) == threadID {
			return t, true
		}
	}
	writeError(w, http.StatusNotFound, "review thread not found on this issue")
	return db.IssueReviewThread{}, false
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

type replyRequest struct {
	Content string `json:"content"`
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

	writeJSON(w, http.StatusOK, map[string]any{
		"thread_id":   uuidToString(thread.ID),
		"comment_id":  res.CommentID,
		"comment_url": res.CommentURL,
	})
}

type resolveRequest struct {
	// Optional: when set, the handler posts a reply first and then resolves.
	// Lets the dev agent do "explain + resolve" in a single round-trip.
	Reply string `json:"reply,omitempty"`
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

	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	thread, ok := h.loadThreadForIssue(w, r, issue.ID, chi.URLParam(r, "threadID"))
	if !ok {
		return
	}

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
		replyRes, err := h.ReviewActions.ReplyToReviewThread(ctx, binding, thread, reply)
		if err != nil {
			writeError(w, http.StatusBadGateway, "github reply failed: "+err.Error())
			return
		}
		resp["comment_id"] = replyRes.CommentID
		resp["comment_url"] = replyRes.CommentURL
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
