package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type CommentResponse struct {
	ID             string               `json:"id"`
	IssueID        string               `json:"issue_id"`
	AuthorType     string               `json:"author_type"`
	AuthorID       string               `json:"author_id"`
	Content        string               `json:"content"`
	Type           string               `json:"type"`
	ParentID       *string              `json:"parent_id"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
	ResolvedAt     *string              `json:"resolved_at"`
	ResolvedByType *string              `json:"resolved_by_type"`
	ResolvedByID   *string              `json:"resolved_by_id"`
	SourceTaskID   *string              `json:"source_task_id,omitempty"`
	Reactions      []ReactionResponse   `json:"reactions"`
	Attachments    []AttachmentResponse `json:"attachments"`
	// Orientation stats — populated only on the roots_only path and omitted in
	// every other mode, so the default response shape stays byte-identical for
	// existing callers. ReplyCount is the number of descendants in the thread;
	// LastActivityAt is the MAX(created_at) across the whole subtree. Together
	// they let an agent triage which thread to drill into without fetching any
	// replies.
	ReplyCount     *int    `json:"reply_count,omitempty"`
	LastActivityAt *string `json:"last_activity_at,omitempty"`
	// ContentTruncated is set only under summary=true: true when Content was
	// clipped to the summary budget, false when it fit. nil (omitted) means the
	// caller did not request a summary projection, so Content is verbatim.
	ContentTruncated *bool `json:"content_truncated,omitempty"`
	// Fold projection fields — populated only under fold=true, and only on the
	// thread ROOT of a resolved thread (mirrors the human timeline fold; see
	// foldResolvedThreads). Both are nil/omitted everywhere else: every comment
	// under an unresolved thread, and every non-root comment. ThreadResolved
	// marks a thread whose discussion was collapsed to its conclusion;
	// FoldedCount is how many comments in that thread were dropped from the
	// response (0 when the thread was already at root + conclusion). The reader
	// pulls the folded comments back with `comment list --full`.
	ThreadResolved *bool `json:"thread_resolved,omitempty"`
	FoldedCount    *int  `json:"folded_count,omitempty"`
}

func commentToResponse(c db.Comment, reactions []ReactionResponse, attachments []AttachmentResponse) CommentResponse {
	if reactions == nil {
		reactions = []ReactionResponse{}
	}
	if attachments == nil {
		attachments = []AttachmentResponse{}
	}
	return CommentResponse{
		ID:             uuidToString(c.ID),
		IssueID:        uuidToString(c.IssueID),
		AuthorType:     c.AuthorType,
		AuthorID:       uuidToString(c.AuthorID),
		Content:        c.Content,
		Type:           c.Type,
		ParentID:       uuidToPtr(c.ParentID),
		CreatedAt:      timestampToString(c.CreatedAt),
		UpdatedAt:      timestampToString(c.UpdatedAt),
		ResolvedAt:     timestampToPtr(c.ResolvedAt),
		ResolvedByType: textToPtr(c.ResolvedByType),
		ResolvedByID:   uuidToPtr(c.ResolvedByID),
		SourceTaskID:   uuidToPtr(c.SourceTaskID),
		Reactions:      reactions,
		Attachments:    attachments,
	}
}

// summaryContentRunes bounds comment content under summary=true. 200 runes is
// enough to tell what a comment is about (its opening) while cutting the bulk
// of a long body out of an agent's context budget. Counted in runes, not bytes,
// so multi-byte (e.g. CJK) content is clipped on a character boundary.
const summaryContentRunes = 200

// summarizeContent clips content to summaryContentRunes for the summary
// projection. Returns the (possibly clipped) content and whether it was
// truncated. An ellipsis marks a clip so the reader knows more text exists.
//
// It scans by rune and stops at the (budget+1)th rune rather than allocating a
// full []rune for the whole body — so a pathologically long comment costs only
// the budget, not its full length, under summary mode.
func summarizeContent(content string) (string, bool) {
	count := 0
	for byteOffset := range content { // range over a string yields rune start offsets
		if count == summaryContentRunes {
			return content[:byteOffset] + "…", true
		}
		count++
	}
	return content, false
}

// foldStat is the per-thread fold annotation attached to a resolved thread's
// root under fold=true. FoldedCount is how many comments in the thread were
// dropped from the response. See CommentResponse.ThreadResolved / FoldedCount.
type foldStat struct {
	FoldedCount int
}

// foldResolvedThreads collapses every resolved thread in a COMPLETE-thread
// comment set down to the comments a reader actually needs, mirroring the human
// timeline fold (deriveThreadResolution in
// packages/views/issues/components/thread-utils.ts) so agents see what humans
// see:
//
//   - unresolved thread     → every comment kept, unchanged, no annotation.
//   - reply-resolved thread → keep the thread root + the resolution reply (the
//     conclusion), drop every other reply. Root + conclusion, never conclusion
//     alone: a conclusion is often referential ("go with what we said above")
//     and unreadable without the root question.
//   - root-resolved thread  → keep only the root, drop every reply (the whole
//     discussion was a settled dead-end; the root states the topic).
//
// The thread root of each resolved thread is annotated (foldStat keyed by the
// root's id string) so the response can mark it thread_resolved and report how
// many comments were folded; a reader that needs the dropped discussion pulls
// it back with `comment list --full`.
//
// Resolution is derived exactly as the UI does: the root wins if resolved;
// otherwise the reply with the latest resolved_at is THE resolution. The single
// resolved-per-thread invariant (ClearOtherThreadResolutions) normally makes
// this unambiguous; the latest-wins tiebreak keeps the projection total if an
// older/concurrent write ever left two.
//
// Callers MUST invoke this only on a set containing COMPLETE threads (the
// default list, --recent, and untailed --thread reads). Partial-thread reads
// (--since, --tail) are rejected upstream because a fold computed over a partial
// thread could drop a resolution that was never fetched. As defense in depth,
// any comment whose thread root is absent from the set is treated as its own
// root and kept verbatim, so a partial thread degrades to "unchanged" rather
// than to silent data loss.
func foldResolvedThreads(comments []db.Comment) ([]db.Comment, map[string]foldStat) {
	if len(comments) == 0 {
		return comments, nil
	}

	byID := make(map[string]db.Comment, len(comments))
	for _, c := range comments {
		byID[uuidToString(c.ID)] = c
	}

	// rootOf walks parent_id up to the thread root within this set. It stops at
	// the first comment whose parent is absent (in a complete-thread set that is
	// the real root; in a partial set it is the highest ancestor we hold). The
	// loop is bounded by len(comments) so an unexpected cycle cannot hang it —
	// the PK forbids real cycles, but never trust a graph walk over stored data.
	rootOf := func(c db.Comment) db.Comment {
		cur := c
		for i := 0; i < len(comments); i++ {
			if !cur.ParentID.Valid {
				return cur
			}
			parent, ok := byID[uuidToString(cur.ParentID)]
			if !ok {
				return cur
			}
			cur = parent
		}
		return cur
	}

	type thread struct {
		root    db.Comment
		replies []db.Comment // non-root, in input (chronological) order
	}
	threads := map[string]*thread{}
	for _, c := range comments {
		root := rootOf(c)
		rid := uuidToString(root.ID)
		th := threads[rid]
		if th == nil {
			th = &thread{root: root}
			threads[rid] = th
		}
		if uuidToString(c.ID) != rid {
			th.replies = append(th.replies, c)
		}
	}

	keep := make(map[string]bool, len(comments))
	stats := map[string]foldStat{}
	for rid, th := range threads {
		// Root-resolved: keep only the root.
		if th.root.ResolvedAt.Valid {
			keep[rid] = true
			stats[rid] = foldStat{FoldedCount: len(th.replies)}
			continue
		}
		// Reply-resolved: the latest-resolved reply is the conclusion.
		var resolution *db.Comment
		for i := range th.replies {
			r := &th.replies[i]
			if !r.ResolvedAt.Valid {
				continue
			}
			if resolution == nil || r.ResolvedAt.Time.After(resolution.ResolvedAt.Time) {
				resolution = r
			}
		}
		if resolution == nil {
			// Unresolved thread: keep everything, no annotation.
			keep[rid] = true
			for _, r := range th.replies {
				keep[uuidToString(r.ID)] = true
			}
			continue
		}
		keep[rid] = true
		keep[uuidToString(resolution.ID)] = true
		// Every reply except the conclusion is folded away.
		stats[rid] = foldStat{FoldedCount: len(th.replies) - 1}
	}

	out := make([]db.Comment, 0, len(comments))
	for _, c := range comments {
		if keep[uuidToString(c.ID)] {
			out = append(out, c)
		}
	}
	return out, stats
}

// commentHardCap bounds the comments returned per issue. Sized as a defensive
// safety net rather than a UX paging window: prod p99 is ~30 comments and
// the all-time max observed is ~1.1k, so 2000 leaves ~2x headroom while still
// preventing a runaway response if some user manages to accumulate a wild
// number of rows on a single issue.
const commentHardCap = 2000

// ListComments returns comments for an issue. The default behaviour is
// unchanged — full chronological dump capped at commentHardCap — so existing
// callers and the desktop UI keep working as-is. Optional query params give
// agent-style readers bounded views that scale to long issues without dragging
// every prior reply into context:
//
//   - roots_only=true — return only top-level comments (parent_id IS NULL),
//     each annotated with reply_count + last_activity_at so the caller can
//     triage which thread to drill into. May combine with since for incremental
//     polling of newly created roots, but is exclusive with thread/recent/tail/
//     cursor modes because those have their own grouping or pagination semantics.
//
//   - summary=true — orthogonal content projection. Clips each returned
//     comment's content to a fixed budget and sets content_truncated, so an
//     agent can scan a list cheaply before pulling a full body. Composes with
//     every mode (default, since, thread, recent, roots_only).
//
//   - fold=true — resolve-aware thread projection. Collapses each resolved
//     thread to root + conclusion (reply-resolved) or root only (root-resolved),
//     reusing the human timeline fold so an agent does not pay tokens for
//     settled discussion. The resolved thread's root carries thread_resolved +
//     folded_count; `--full` (no fold param) brings the dropped comments back.
//     Needs whole threads to compute a resolution, so it is rejected with since,
//     tail, and roots_only (partial-thread / reply-less reads) and composes with
//     the default list, recent, untailed thread, and summary.
//
//   - thread=<comment-uuid> — return the root of the thread containing this
//     comment plus every descendant. The anchor may be a root or any reply;
//     the server walks up to the root via a recursive CTE, so callers do not
//     need to know whether the id they have is a root.
//
//   - tail=<N> — only valid with thread. Cap the reply count at the N most
//     recent replies (per (created_at, id)). The thread root is always
//     returned, even when N=0, so the reader keeps the "what is this thread
//     about" context. Without tail, thread returns the entire thread (the
//     pre-MUL-2421 behavior).
//
//   - recent=<N> — return the N most recently active threads (root + every
//     descendant per thread). A thread's recency is MAX(created_at) across
//     the whole subtree, so a stale-but-recently-replied thread ranks ahead
//     of an active-but-quiet one. Row-based "newest N comments" is
//     deliberately NOT exposed — it surfaces unrelated thread tails and
//     hides relevant history (#2340).
//
//   - before=<RFC3339> + before-id=<uuid> — cursor. The pair's meaning is
//     context-dependent so the flag surface stays small:
//
//   - with recent: a *thread* cursor — (last_activity_at, root_id) — and
//     the next page returns threads strictly less recent.
//
//   - with thread + tail: a *reply* cursor — (created_at, id) — and the
//     next page returns replies in the same thread strictly older than
//     that reply.
//
// Both values must be set together so the cursor can tie-break entries
// landing in the same microsecond. The cursor for the next page is
// emitted via the X-Multica-Next-Before / X-Multica-Next-Before-Id
// response headers.
//
// Combination rules (kept narrow on purpose — Elon flagged the matrix risk):
//
//   - roots_only is exclusive with thread, recent, tail, and before/before-id.
//     It may combine with since. This keeps "list issue roots" separate from
//     "read a specific thread" and "read recently active threads".
//   - thread is exclusive with recent. Asking for "the most recent N within
//     thread X" mixes two different navigation models and is rejected.
//   - thread + before/before-id requires tail. Without tail, thread returns
//     the entire thread and a cursor would be ignored — reject loudly so
//     the documented "cursor scrolls within a tailed window" rule holds.
//   - tail requires thread (it is a thread-scoped limit; outside of thread
//     it has no defined behavior).
//   - thread may combine with since (incremental polling of one thread),
//     and the since filter is applied after the tail/cursor cut so the
//     thread root is still emitted but stale rows drop out.
//   - recent may combine with before/before-id (scroll older threads) and
//     with since (recent activity in a window).
//
// The response body is always chronological (oldest → newest); under recent
// that means threads are listed oldest-active first and the freshest thread
// sits at the tail, closest to "now" in an agent prompt.
func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	q := r.URL.Query()

	var sinceTime pgtype.Timestamptz
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			// Fall back to RFC3339 for backwards-compat with the original CLI.
			t, err = time.Parse(time.RFC3339, v)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid since parameter; expected RFC3339 format")
				return
			}
		}
		sinceTime = pgtype.Timestamptz{Time: t, Valid: true}
	}

	threadStr := q.Get("thread")
	recentStr := q.Get("recent")
	tailStr := q.Get("tail")
	beforeTimeStr := q.Get("before")
	beforeIDStr := q.Get("before_id")
	if beforeIDStr == "" {
		// Accept hyphenated alias to match CLI flag convention.
		beforeIDStr = q.Get("before-id")
	}

	rootsOnlyStr := q.Get("roots_only")
	if rootsOnlyStr == "" {
		// Accept hyphenated alias to match CLI flag convention.
		rootsOnlyStr = q.Get("roots-only")
	}

	rootsOnly := false
	if rootsOnlyStr != "" {
		switch rootsOnlyStr {
		case "true":
			rootsOnly = true
		case "false":
		default:
			writeError(w, http.StatusBadRequest, "invalid roots_only parameter; expected boolean")
			return
		}
	}

	// summary=true is an orthogonal content projection: it clips each comment's
	// content to a fixed budget so an agent can scan a list without pulling full
	// bodies into context. It is intentionally NOT mutually exclusive with any
	// mode — it composes with the default list, since, thread, recent, and
	// roots_only alike.
	summary := false
	if summaryStr := q.Get("summary"); summaryStr != "" {
		switch summaryStr {
		case "true":
			summary = true
		case "false":
		default:
			writeError(w, http.StatusBadRequest, "invalid summary parameter; expected boolean")
			return
		}
	}

	// fold=true is a thread-level projection: it collapses every resolved thread
	// in the result to root + conclusion (reply-resolved) or root only
	// (root-resolved), reusing the human timeline's fold semantics so an agent
	// reading a long issue doesn't pay tokens for settled discussion. Unlike
	// summary (a per-comment content clip that composes with everything), fold
	// needs WHOLE threads to compute a resolution, so it is rejected on the
	// partial-thread reads (since / tail) and on roots_only (which carries no
	// replies to fold). It composes with summary, the default list, recent, and
	// untailed thread reads.
	fold := false
	if foldStr := q.Get("fold"); foldStr != "" {
		switch foldStr {
		case "true":
			fold = true
		case "false":
		default:
			writeError(w, http.StatusBadRequest, "invalid fold parameter; expected boolean")
			return
		}
	}

	// --- combination validation ----------------------------------------
	if fold && sinceTime.Valid {
		writeError(w, http.StatusBadRequest, "fold and since are mutually exclusive: since returns a partial thread, and a fold over a partial thread could hide a resolution that was not fetched")
		return
	}
	if fold && tailStr != "" {
		writeError(w, http.StatusBadRequest, "fold and tail are mutually exclusive: tail returns a partial thread, which cannot be folded safely")
		return
	}
	if fold && rootsOnly {
		writeError(w, http.StatusBadRequest, "fold and roots_only are mutually exclusive: roots_only returns no replies to fold")
		return
	}
	if rootsOnly && threadStr != "" {
		writeError(w, http.StatusBadRequest, "roots_only and thread are mutually exclusive")
		return
	}
	if rootsOnly && recentStr != "" {
		writeError(w, http.StatusBadRequest, "roots_only and recent are mutually exclusive")
		return
	}
	if rootsOnly && tailStr != "" {
		writeError(w, http.StatusBadRequest, "roots_only and tail are mutually exclusive")
		return
	}
	if rootsOnly && (beforeTimeStr != "" || beforeIDStr != "") {
		writeError(w, http.StatusBadRequest, "roots_only does not support before / before_id")
		return
	}
	if threadStr != "" && recentStr != "" {
		writeError(w, http.StatusBadRequest, "thread and recent are mutually exclusive")
		return
	}
	if tailStr != "" && threadStr == "" {
		writeError(w, http.StatusBadRequest, "tail requires thread (it is a thread-scoped limit)")
		return
	}
	if (beforeTimeStr == "") != (beforeIDStr == "") {
		writeError(w, http.StatusBadRequest, "before and before_id must be set together (composite cursor)")
		return
	}
	// Cursor needs either a recent window (thread cursor) or a tailed thread
	// (reply cursor). A bare cursor would otherwise fall through to the
	// default / since path — returning a full timeline that the caller did
	// not ask for. Reject loudly so the API surface matches the documented
	// semantics.
	if beforeTimeStr != "" && recentStr == "" && (threadStr == "" || tailStr == "") {
		writeError(w, http.StatusBadRequest, "before / before_id require recent (thread cursor) or thread + tail (reply cursor)")
		return
	}

	// --- parse cursor / recent ----------------------------------------
	var beforeCursor pgtype.Timestamptz
	var beforeUUID pgtype.UUID
	hasCursor := false
	if beforeTimeStr != "" {
		t, err := time.Parse(time.RFC3339Nano, beforeTimeStr)
		if err != nil {
			t, err = time.Parse(time.RFC3339, beforeTimeStr)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid before parameter; expected RFC3339 format")
				return
			}
		}
		beforeCursor = pgtype.Timestamptz{Time: t, Valid: true}
		uuid, perr := util.ParseUUID(beforeIDStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid before_id parameter; expected UUID")
			return
		}
		beforeUUID = uuid
		hasCursor = true
	}

	recentN := 0
	if recentStr != "" {
		n, err := strconv.Atoi(recentStr)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid recent parameter; expected positive integer")
			return
		}
		if n > commentHardCap {
			n = commentHardCap
		}
		recentN = n
	}

	// tail=0 is allowed (returns root only — useful for "what is this thread
	// about" lookups without dragging any replies into context). Negative
	// values are rejected because they'd round-trip to LIMIT -N which
	// PostgreSQL flags as a syntax error.
	threadTail := -1
	threadTailSet := false
	if tailStr != "" {
		n, err := strconv.Atoi(tailStr)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid tail parameter; expected non-negative integer")
			return
		}
		if n > commentHardCap {
			n = commentHardCap
		}
		threadTail = n
		threadTailSet = true
	}

	result, err := h.fetchCommentsForList(r.Context(), fetchCommentsArgs{
		Issue:         issue,
		Since:         sinceTime,
		ThreadAnchor:  threadStr,
		ThreadTail:    threadTail,
		ThreadTailSet: threadTailSet,
		RecentN:       recentN,
		HasCursor:     hasCursor,
		BeforeAt:      beforeCursor,
		BeforeID:      beforeUUID,
		RootsOnly:     rootsOnly,
	})
	if err != nil {
		switch err {
		case errCommentThreadNotFound:
			writeError(w, http.StatusNotFound, "thread anchor not found in this issue")
			return
		case errCommentThreadBadID:
			writeError(w, http.StatusBadRequest, "invalid thread parameter; expected UUID")
			return
		default:
			writeError(w, http.StatusInternalServerError, "failed to list comments")
			return
		}
	}

	// Apply the resolve-aware fold before anything keys off the comment set
	// (reaction/attachment grouping, the response array): folding drops comments,
	// and the dropped ones should not pay a reactions/attachments round-trip or
	// appear in the response. fetchCommentsForList only ever returns complete
	// threads on the modes fold is allowed with (default, recent, untailed
	// thread), which is the precondition foldResolvedThreads documents.
	var foldInfo map[string]foldStat
	if fold {
		result.Comments, foldInfo = foldResolvedThreads(result.Comments)
	}

	commentIDs := make([]pgtype.UUID, len(result.Comments))
	for i, c := range result.Comments {
		commentIDs[i] = c.ID
	}
	grouped := h.groupReactions(r, commentIDs)
	groupedAtt := h.groupAttachments(r, commentIDs)

	resp := make([]CommentResponse, len(result.Comments))
	for i, c := range result.Comments {
		cid := uuidToString(c.ID)
		resp[i] = commentToResponse(c, grouped[cid], groupedAtt[cid])
		// Attach roots_only orientation stats when present (nil map elsewhere).
		if st, ok := result.RootStats[cid]; ok {
			rc := st.ReplyCount
			resp[i].ReplyCount = &rc
			if st.LastActivityAt.Valid {
				la := timestampToString(st.LastActivityAt)
				resp[i].LastActivityAt = &la
			}
		}
		// Attach fold annotations on a resolved thread's root (nil map elsewhere;
		// keyed by root id, so only root comments match).
		if st, ok := foldInfo[cid]; ok {
			resolved := true
			resp[i].ThreadResolved = &resolved
			fc := st.FoldedCount
			resp[i].FoldedCount = &fc
		}
		// Apply the summary projection last so it clips whatever content the
		// chosen read mode produced, uniformly across every mode.
		if summary {
			clipped, truncated := summarizeContent(resp[i].Content)
			resp[i].Content = clipped
			resp[i].ContentTruncated = &truncated
		}
	}

	// Emit the next cursor as response headers when the page is likely not
	// the last one. The cursor's meaning is context-dependent: under recent
	// it points at the oldest thread in the page (next page = older threads);
	// under thread + tail it points at the oldest reply in the page (next
	// page = older replies in the same thread). Headers stay out of the JSON
	// body so the default flat-array response shape — which the desktop UI
	// and existing callers depend on — is unchanged.
	if result.NextBefore != "" && result.NextBeforeID != "" {
		w.Header().Set("X-Multica-Next-Before", result.NextBefore)
		w.Header().Set("X-Multica-Next-Before-Id", result.NextBeforeID)
	}

	writeJSON(w, http.StatusOK, resp)
}

// fetchCommentsArgs bundles the parsed query params so fetchCommentsForList
// stays readable. Sentinel errors below let the caller turn DB-layer outcomes
// into the right HTTP status without leaking SQL details.
//
// ThreadTail is split into a value + a "set" flag because tail=0 is a
// meaningful caller intent (return just the root). A bare int would collapse
// "user did not pass --tail" and "user passed --tail 0" into the same state,
// which would silently downgrade the latter to the full-thread path.
type fetchCommentsArgs struct {
	Issue         db.Issue
	Since         pgtype.Timestamptz
	RootsOnly     bool
	ThreadAnchor  string
	ThreadTail    int
	ThreadTailSet bool
	RecentN       int
	HasCursor     bool
	BeforeAt      pgtype.Timestamptz
	BeforeID      pgtype.UUID
}

// fetchCommentsResult carries both the materialised comments and (for the
// recent/thread-grouped path) the cursor to use for the next page. Cursor
// fields are empty strings when there is no next page or the path does not
// support cursors.
type fetchCommentsResult struct {
	Comments     []db.Comment
	NextBefore   string
	NextBeforeID string
	// RootStats carries per-root orientation stats keyed by comment id string.
	// Populated only on the roots_only path; nil for every other mode.
	RootStats map[string]rootStat
}

// rootStat is the per-thread orientation metadata attached to each root comment
// on the roots_only path. See CommentResponse.ReplyCount / LastActivityAt.
type rootStat struct {
	ReplyCount     int
	LastActivityAt pgtype.Timestamptz
}

var (
	errCommentThreadNotFound = &commentFetchError{"thread anchor not found"}
	errCommentThreadBadID    = &commentFetchError{"invalid thread anchor id"}
)

type commentFetchError struct{ msg string }

func (e *commentFetchError) Error() string { return e.msg }

func (h *Handler) fetchCommentsForList(ctx context.Context, args fetchCommentsArgs) (fetchCommentsResult, error) {
	issue := args.Issue

	// Thread-scoped read. Server resolves the anchor → root via recursive
	// CTE, so we don't have to assume two-layer flat threads here.
	if args.ThreadAnchor != "" {
		anchor, err := util.ParseUUID(args.ThreadAnchor)
		if err != nil {
			return fetchCommentsResult{}, errCommentThreadBadID
		}
		// Tailed path: paged query that returns root + the @reply_limit
		// most recent replies (per (created_at, id)). The thread root is
		// always returned, so a reader can land on a long thread without
		// dragging hundreds of replies into context. The reply-internal
		// cursor (--before / --before-id under --thread + --tail) scrolls
		// to older replies inside the same thread.
		if args.ThreadTailSet {
			// Probe for has-more by asking the SQL for one extra reply
			// beyond what the caller wants. If we get back >tail replies
			// there is at least one older reply still on disk; if we get
			// back ≤tail the page is the tail of the thread and there is
			// nothing older to scroll to (so we must NOT emit a cursor —
			// otherwise the next page is wasted round-trip that returns
			// just the root). This is the exact-boundary fix called out
			// in the MUL-2421 review.
			rows, err := h.Queries.ListThreadCommentsForIssuePaged(ctx, db.ListThreadCommentsForIssuePagedParams{
				AnchorID:    anchor,
				IssueID:     issue.ID,
				WorkspaceID: issue.WorkspaceID,
				HasCursor:   args.HasCursor,
				BeforeAt:    args.BeforeAt,
				BeforeID:    args.BeforeID,
				ReplyLimit:  int32(args.ThreadTail) + 1,
			})
			if err != nil {
				return fetchCommentsResult{}, err
			}
			if len(rows) == 0 {
				return fetchCommentsResult{}, errCommentThreadNotFound
			}
			// Split the result into root + replies (ASC order preserved).
			// Root is identified by parent_id IS NULL and is always
			// present in the SQL output; we keep it out of the cursor /
			// tail-trim logic so the user always sees thread context.
			var rootComment *db.Comment
			replies := make([]db.Comment, 0, len(rows))
			for _, r := range rows {
				c := db.Comment{
					ID:             r.ID,
					IssueID:        r.IssueID,
					AuthorType:     r.AuthorType,
					AuthorID:       r.AuthorID,
					Content:        r.Content,
					Type:           r.Type,
					CreatedAt:      r.CreatedAt,
					UpdatedAt:      r.UpdatedAt,
					ParentID:       r.ParentID,
					WorkspaceID:    r.WorkspaceID,
					ResolvedAt:     r.ResolvedAt,
					ResolvedByType: r.ResolvedByType,
					ResolvedByID:   r.ResolvedByID,
				}
				if !r.ParentID.Valid {
					root := c
					rootComment = &root
					continue
				}
				replies = append(replies, c)
			}
			// Trim the probe overflow back to the caller's tail. The SQL
			// emits ASC, so the extra row is the oldest reply — dropping
			// it from the head is what aligns "newest N" with the user's
			// request.
			hasMore := len(replies) > args.ThreadTail
			if hasMore {
				replies = replies[1:]
			}
			out := make([]db.Comment, 0, len(replies)+1)
			if rootComment != nil {
				out = append(out, *rootComment)
			}
			for _, r := range replies {
				// since drops stale rows AFTER the tail / cursor cut.
				// The root is exempt (already appended above): a reader
				// who set --since to skip already-seen replies still
				// needs the root context if the page only contained
				// the root.
				if args.Since.Valid && !r.CreatedAt.Time.After(args.Since.Time) {
					continue
				}
				out = append(out, r)
			}
			// Emit a reply cursor only when we proved an older reply
			// exists (hasMore). On an exact-boundary page (replyCount
			// == tail with no overflow) hasMore is false and the cursor
			// stays empty.
			//
			// Additionally suppress the cursor when `since` is set and
			// the oldest retained reply on this page is already <= since.
			// The next page walks replies strictly older than that one,
			// so every older reply has created_at strictly less — if the
			// cursor target itself can't satisfy `> since`, no older
			// reply can either, and continuing to paginate would only
			// return root-only pages until the agent walks the entire
			// pre-`since` history. This mirrors the head-thread guard on
			// the recent + since path. Flagged by Elon's second review on
			// MUL-2421.
			res := fetchCommentsResult{Comments: out}
			emitCursor := hasMore && len(replies) > 0
			if emitCursor && args.Since.Valid && !replies[0].CreatedAt.Time.After(args.Since.Time) {
				emitCursor = false
			}
			if emitCursor {
				oldest := replies[0]
				res.NextBefore = oldest.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
				res.NextBeforeID = uuidToString(oldest.ID)
			}
			return res, nil
		}
		rows, err := h.Queries.ListThreadCommentsForIssue(ctx, db.ListThreadCommentsForIssueParams{
			AnchorID:    anchor,
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			RowLimit:    commentHardCap,
		})
		if err != nil {
			return fetchCommentsResult{}, err
		}
		if len(rows) == 0 {
			return fetchCommentsResult{}, errCommentThreadNotFound
		}
		out := make([]db.Comment, 0, len(rows))
		for _, r := range rows {
			if args.Since.Valid && !r.CreatedAt.Time.After(args.Since.Time) {
				continue
			}
			out = append(out, db.Comment{
				ID:             r.ID,
				IssueID:        r.IssueID,
				AuthorType:     r.AuthorType,
				AuthorID:       r.AuthorID,
				Content:        r.Content,
				Type:           r.Type,
				CreatedAt:      r.CreatedAt,
				UpdatedAt:      r.UpdatedAt,
				ParentID:       r.ParentID,
				WorkspaceID:    r.WorkspaceID,
				ResolvedAt:     r.ResolvedAt,
				ResolvedByType: r.ResolvedByType,
				ResolvedByID:   r.ResolvedByID,
			})
		}
		return fetchCommentsResult{Comments: out}, nil
	}

	// Thread-grouped recent read: N most recently active threads.
	if args.RecentN > 0 {
		rows, err := h.Queries.ListRecentThreadCommentsForIssue(ctx, db.ListRecentThreadCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			HasCursor:   args.HasCursor,
			BeforeAt:    args.BeforeAt,
			BeforeID:    args.BeforeID,
			ThreadLimit: int32(args.RecentN),
		})
		if err != nil {
			return fetchCommentsResult{}, err
		}

		// The SQL already orders rows by (last_activity_at ASC, root_id ASC,
		// created_at ASC, id ASC), so the OLDEST-active thread sits at the
		// head and the FRESHEST thread at the tail. Walk the rows once to:
		//   1. Strip the thread-metadata columns down to db.Comment for the
		//      caller (uniform shape across paths).
		//   2. Count distinct threads in the page so we know whether a "next
		//      older page" is likely to exist.
		//   3. Capture the head thread's (last_activity_at, root_id) — that
		//      is the cursor for the next page (next page = threads strictly
		//      less recent than this one).
		comments := make([]db.Comment, 0, len(rows))
		var headRoot pgtype.UUID
		var headLast pgtype.Timestamptz
		seenRoot := map[string]struct{}{}
		for _, r := range rows {
			if !headRoot.Valid {
				headRoot = r.ThreadRootID
				headLast = r.ThreadLastActivityAt
			}
			seenRoot[uuidToString(r.ThreadRootID)] = struct{}{}
			// Since filter on the recent path: drop comments older than
			// `since`. Done in-memory so we keep the thread-grouped
			// semantics from the query (don't pre-filter rows before the
			// MAX(created_at) ranking — that would silently downgrade a
			// thread whose most recent activity falls inside the window).
			if args.Since.Valid && !r.CreatedAt.Time.After(args.Since.Time) {
				continue
			}
			comments = append(comments, db.Comment{
				ID:             r.ID,
				IssueID:        r.IssueID,
				AuthorType:     r.AuthorType,
				AuthorID:       r.AuthorID,
				Content:        r.Content,
				Type:           r.Type,
				CreatedAt:      r.CreatedAt,
				UpdatedAt:      r.UpdatedAt,
				ParentID:       r.ParentID,
				WorkspaceID:    r.WorkspaceID,
				ResolvedAt:     r.ResolvedAt,
				ResolvedByType: r.ResolvedByType,
				ResolvedByID:   r.ResolvedByID,
			})
		}

		// Only emit a cursor when the page is full. Fewer threads than
		// requested ⇒ the SELECT exhausted matching threads, so there is
		// no older page to scroll to.
		//
		// Additionally suppress the cursor when `since` is set and the head
		// thread's last_activity_at is already <= since. The pagination
		// walks threads in strictly decreasing last_activity_at, so every
		// older page has last_activity_at strictly less than the head's —
		// if the head itself can't satisfy `> since`, no older thread can
		// either. Predicating on the head (not on whether `comments` is
		// empty) also catches the mixed case where this page keeps rows
		// from fresher threads but the head thread is already past `since`.
		// Flagged by Elon in #2787's second review (MUL-2340 nit).
		out := fetchCommentsResult{Comments: comments}
		emitCursor := len(seenRoot) >= args.RecentN && headRoot.Valid && headLast.Valid
		if emitCursor && args.Since.Valid && !headLast.Time.After(args.Since.Time) {
			emitCursor = false
		}
		if emitCursor {
			out.NextBefore = headLast.Time.UTC().Format(time.RFC3339Nano)
			out.NextBeforeID = uuidToString(headRoot)
		}
		return out, nil
	}

	if args.RootsOnly {
		// Root-only read for issue-level orientation. This intentionally
		// stays separate from thread/recent modes: callers get the global
		// top-level discussion first, then fetch a specific thread only when
		// they need reply context. Each root carries reply_count +
		// last_activity_at so the reader can triage which thread to drill into.
		stats := map[string]rootStat{}
		if args.Since.Valid {
			rows, err := h.Queries.ListRootCommentsSinceForIssue(ctx, db.ListRootCommentsSinceForIssueParams{
				IssueID:     issue.ID,
				WorkspaceID: issue.WorkspaceID,
				Since:       args.Since,
				RowLimit:    commentHardCap,
			})
			if err != nil {
				return fetchCommentsResult{}, err
			}
			comments := make([]db.Comment, len(rows))
			for i, r := range rows {
				comments[i] = db.Comment{
					ID: r.ID, IssueID: r.IssueID, AuthorType: r.AuthorType, AuthorID: r.AuthorID,
					Content: r.Content, Type: r.Type, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
					ParentID: r.ParentID, WorkspaceID: r.WorkspaceID, ResolvedAt: r.ResolvedAt,
					ResolvedByType: r.ResolvedByType, ResolvedByID: r.ResolvedByID,
				}
				stats[uuidToString(r.ID)] = rootStat{ReplyCount: int(r.ReplyCount), LastActivityAt: r.LastActivityAt}
			}
			return fetchCommentsResult{Comments: comments, RootStats: stats}, nil
		}

		rows, err := h.Queries.ListRootCommentsForIssue(ctx, db.ListRootCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			RowLimit:    commentHardCap,
		})
		if err != nil {
			return fetchCommentsResult{}, err
		}
		comments := make([]db.Comment, len(rows))
		for i, r := range rows {
			comments[i] = db.Comment{
				ID: r.ID, IssueID: r.IssueID, AuthorType: r.AuthorType, AuthorID: r.AuthorID,
				Content: r.Content, Type: r.Type, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
				ParentID: r.ParentID, WorkspaceID: r.WorkspaceID, ResolvedAt: r.ResolvedAt,
				ResolvedByType: r.ResolvedByType, ResolvedByID: r.ResolvedByID,
			}
			stats[uuidToString(r.ID)] = rootStat{ReplyCount: int(r.ReplyCount), LastActivityAt: r.LastActivityAt}
		}
		return fetchCommentsResult{Comments: comments, RootStats: stats}, nil
	}

	// Default + since paths preserved verbatim (no behavioural change for
	// existing callers).
	if args.Since.Valid {
		comments, err := h.Queries.ListCommentsSinceForIssue(ctx, db.ListCommentsSinceForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			CreatedAt:   args.Since,
			Limit:       commentHardCap,
		})
		return fetchCommentsResult{Comments: comments}, err
	}
	comments, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       commentHardCap,
	})
	return fetchCommentsResult{Comments: comments}, err
}

type CreateCommentRequest struct {
	Content          string   `json:"content"`
	Type             string   `json:"type"`
	ParentID         *string  `json:"parent_id"`
	AttachmentIDs    []string `json:"attachment_ids"`
	SuppressAgentIDs []string `json:"suppress_agent_ids"`
}

type CommentTriggerPreviewRequest struct {
	Content          string  `json:"content"`
	ParentID         *string `json:"parent_id"`
	EditingCommentID *string `json:"editing_comment_id"`
}

type CommentTriggerPreviewResponse struct {
	Agents []CommentTriggerAgentResponse `json:"agents"`
}

type CommentTriggerAgentResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url,omitempty"`
	Source    string  `json:"source"`
	Reason    string  `json:"reason"`
}

type commentAgentTriggerSource string

const (
	commentTriggerSourceIssueAssignee      commentAgentTriggerSource = "issue_assignee"
	commentTriggerSourceMentionAgent       commentAgentTriggerSource = "mention_agent"
	commentTriggerSourceMentionSquadLeader commentAgentTriggerSource = "mention_squad_leader"
	commentTriggerSourceThreadParent       commentAgentTriggerSource = "thread_parent"
	commentTriggerSourceConversation       commentAgentTriggerSource = "conversation_continuation"
)

const defaultCommentRoutingEscalationDelay = 5 * time.Minute

func (h *Handler) commentRoutingEscalationDelay(ctx context.Context, workspaceID pgtype.UUID) time.Duration {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil || len(ws.Settings) == 0 {
		return defaultCommentRoutingEscalationDelay
	}

	var settings struct {
		CommentRouting struct {
			EscalationSeconds *int `json:"escalation_seconds"`
		} `json:"comment_routing"`
	}
	if err := json.Unmarshal(ws.Settings, &settings); err != nil || settings.CommentRouting.EscalationSeconds == nil {
		return defaultCommentRoutingEscalationDelay
	}
	if *settings.CommentRouting.EscalationSeconds <= 0 {
		return 0
	}
	return time.Duration(*settings.CommentRouting.EscalationSeconds) * time.Second
}

type commentEscalationFallback struct {
	Agent db.Agent
	Squad *db.Squad
}

type commentAgentTrigger struct {
	Agent              db.Agent
	Source             commentAgentTriggerSource
	Squad              *db.Squad
	EscalationFallback *commentEscalationFallback
	AlreadyPending     bool
}

type commentTriggerComputeOptions struct {
	ExcludeTriggerCommentID pgtype.UUID
	// OriginatorUserID is the top-of-chain human user id for this trigger
	// (MUL-3963). Only consulted for AGENT actors — canInvokeAgent judges A2A
	// by the originator, not the immediate agent principal. Members are their
	// own originator so this may be empty for member-authored triggers.
	OriginatorUserID string
}

func commentAgentTriggerReason(trigger commentAgentTrigger) string {
	switch trigger.Source {
	case commentTriggerSourceIssueAssignee:
		return "Current issue assignment will trigger this agent."
	case commentTriggerSourceMentionAgent:
		return "This agent was mentioned in the comment."
	case commentTriggerSourceMentionSquadLeader:
		return "A mentioned squad will trigger its leader."
	case commentTriggerSourceThreadParent:
		return "This reply will trigger the parent comment's author."
	case commentTriggerSourceConversation:
		return "This follow-up will continue the recent agent conversation."
	default:
		return "This comment will trigger this agent."
	}
}

func commentAgentTriggerToResponse(trigger commentAgentTrigger) CommentTriggerAgentResponse {
	return CommentTriggerAgentResponse{
		ID:        uuidToString(trigger.Agent.ID),
		Name:      trigger.Agent.Name,
		AvatarURL: textToPtr(trigger.Agent.AvatarUrl),
		Source:    string(trigger.Source),
		Reason:    commentAgentTriggerReason(trigger),
	}
}

func (h *Handler) PreviewCommentTriggers(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CommentTriggerPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var editingComment *db.Comment
	var opts commentTriggerComputeOptions
	if req.EditingCommentID != nil {
		editingID, ok := parseUUIDOrBadRequest(w, *req.EditingCommentID, "editing_comment_id")
		if !ok {
			return
		}
		comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          editingID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil || uuidToString(comment.IssueID) != uuidToString(issue.ID) {
			writeError(w, http.StatusBadRequest, "invalid editing comment")
			return
		}
		editingComment = &comment
		opts.ExcludeTriggerCommentID = editingID
	}

	var parentID pgtype.UUID
	if req.ParentID != nil {
		parentID, ok = parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !ok {
			return
		}

		if editingComment != nil && uuidToString(parentID) != uuidToString(editingComment.ParentID) {
			writeError(w, http.StatusBadRequest, "parent_id does not match editing comment")
			return
		}
	} else if editingComment != nil && editingComment.ParentID.Valid {
		parentID = editingComment.ParentID
	}

	var parentComment *db.Comment
	if parentID.Valid {
		parent, err := h.Queries.GetComment(r.Context(), parentID)
		if err != nil || uuidToString(parent.IssueID) != uuidToString(issue.ID) {
			writeError(w, http.StatusBadRequest, "invalid parent comment")
			return
		}
		parentComment = &parent
	}

	content := req.Content
	if content == "" {
		writeJSON(w, http.StatusOK, CommentTriggerPreviewResponse{Agents: []CommentTriggerAgentResponse{}})
		return
	}

	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	opts.OriginatorUserID = h.invokeOriginatorFromRequest(r, actorType, actorID)
	triggers := h.computeCommentAgentTriggers(r.Context(), issue, content, parentComment, actorType, actorID, opts)
	resp := CommentTriggerPreviewResponse{Agents: make([]CommentTriggerAgentResponse, 0, len(triggers))}
	for _, trigger := range triggers {
		resp.Agents = append(resp.Agents, commentAgentTriggerToResponse(trigger))
	}
	writeJSON(w, http.StatusOK, resp)
}

// taskCoversReplyParent reports whether parentID is a comment this task is
// authorized to reply under. A comment-triggered task may reply to its trigger
// comment OR to any earlier comment that was folded into the same run while it
// was still queued (coalesced_comment_ids). A coalesced run answers each root
// thread it covered inside that thread, so its replies legitimately target
// those threads' comments — not just the trigger (MUL-4348 per-thread fan-out).
//
// Every other parent on the task's own issue is still rejected: this is the
// defense against resumed-session --parent drift and cross-thread misplacement.
// The allow-list is exactly the set the run was given to answer, so it cannot
// reach arbitrary comments.
func taskCoversReplyParent(task db.AgentTaskQueue, parentID pgtype.UUID) bool {
	if !parentID.Valid {
		return false
	}
	target := uuidToString(parentID)
	if task.TriggerCommentID.Valid && uuidToString(task.TriggerCommentID) == target {
		return true
	}
	for _, id := range task.CoalescedCommentIds {
		if id.Valid && uuidToString(id) == target {
			return true
		}
	}
	return false
}

func (h *Handler) CreateComment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Type == "" {
		req.Type = "comment"
	}

	var parentID pgtype.UUID
	var parentComment *db.Comment
	if req.ParentID != nil {
		var parsed pgtype.UUID
		parsed, ok = parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !ok {
			return
		}
		parentID = parsed
		parent, err := h.Queries.GetComment(r.Context(), parentID)
		if err != nil || uuidToString(parent.IssueID) != uuidToString(issue.ID) {
			writeError(w, http.StatusBadRequest, "invalid parent comment")
			return
		}
		parentComment = &parent
	}

	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}
	suppressAgentIDs, ok := parseUUIDSliceOrBadRequest(w, req.SuppressAgentIDs, "suppress_agent_ids")
	if !ok {
		return
	}

	// Determine author identity: agent (via X-Agent-ID header) or member.
	authorType, authorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))

	// Defense against resumed-session drift: when an agent posts from inside a
	// comment-triggered task AND the comment is being posted on that same
	// issue, the parent_id must exactly match the task's trigger comment.
	// Resumed Claude sessions otherwise carry forward a previous turn's
	// --parent UUID and silently misplace the reply.
	//
	// The task.IssueID scope is important: the CLI stamps X-Task-ID on every
	// request, so an agent legitimately commenting on a different issue must
	// not be blocked by its current task's trigger. Assignment-triggered
	// tasks (no TriggerCommentID) are also unaffected.
	// sourceTaskID captures the agent's currently-executing task when it posts
	// via the CLI (X-Task-ID header). Stamping it on the comment row keeps the
	// originator inheritance chain (resolveOriginatorFromTriggerComment →
	// comment.source_task_id → parent task's originator_user_id) intact across
	// the leader→worker mention hop. Without this stamp, a private squad
	// leader's worker-agent whose completion wakes the leader via
	// routeAssignedSquadLeaderFallback can't pass canInvokeAgent — the
	// worker's task originator is unattributed, effectiveUser resolves to "",
	// and the private-agent gate denies the wake (MUL-4015).
	var sourceTaskID pgtype.UUID
	if authorType == "agent" {
		if taskIDHeader := r.Header.Get("X-Task-ID"); taskIDHeader != "" {
			taskUUID, parseErr := util.ParseUUID(taskIDHeader)
			if parseErr == nil {
				task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
				if err == nil && task.IssueID.Valid && uuidToString(task.IssueID) == uuidToString(issue.ID) {
					if task.TriggerCommentID.Valid {
						if !taskCoversReplyParent(task, parentID) {
							writeError(w, http.StatusConflict,
								"parent_id must be this task's trigger comment id ("+uuidToString(task.TriggerCommentID)+") or one of the earlier comments it coalesced")
							return
						}
					}
					noAction, checkErr := service.HasSquadLeaderNoActionEvaluationForTask(r.Context(), h.Queries, task)
					if checkErr != nil {
						slog.Warn("checking squad leader no_action evaluation failed", append(logger.RequestAttrs(r),
							"error", checkErr,
							"task_id", taskIDHeader,
							"issue_id", issueID,
						)...)
					} else if noAction {
						writeError(w, http.StatusConflict, "squad leader recorded no_action; comments are not allowed for this task")
						return
					}
					// Only stamp source_task_id for a task belonging to THIS
					// issue. An agent legitimately commenting on a DIFFERENT
					// issue than its current task must not stamp that task's
					// id here — the resulting chain would then attribute the
					// out-of-band comment to an unrelated task's originator.
					sourceTaskID = taskUUID
				}
			}
		}
	}

	// NOTE: Comment content is stored as Markdown source. XSS is handled at the
	// rendering layer (rehype-sanitize) and at the editor layer
	// (@tiptap/markdown with html:false). Running an HTML sanitizer here would
	// entity-encode Markdown syntax characters (>, ", &, <) and corrupt the
	// source. See issue #1303 / discussion in MUL-1119, MUL-1125.

	// parent_id stores the exact comment being replied to. Thread-level behavior
	// (for example auto-unresolving a resolved thread) resolves the root
	// separately so storing a reply-to-reply does not destroy the direct-parent
	// signal used by trigger decisions.
	var rootComment *db.Comment
	if parentID.Valid {
		if root, err := h.Queries.GetThreadRoot(r.Context(), db.GetThreadRootParams{
			CommentID:   parentID,
			WorkspaceID: issue.WorkspaceID,
		}); err == nil {
			rootComment = &root
		}
	}

	comment, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:      issue.ID,
		WorkspaceID:  issue.WorkspaceID,
		AuthorType:   authorType,
		AuthorID:     parseUUID(authorID),
		Content:      req.Content,
		Type:         req.Type,
		ParentID:     parentID,
		SourceTaskID: sourceTaskID,
	})
	if err != nil {
		slog.Warn("create comment failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to create comment: "+err.Error())
		return
	}

	// Link uploaded attachments to this comment.
	if len(attachmentIDs) > 0 {
		h.linkAttachmentsByIDs(r.Context(), comment.ID, issue.ID, attachmentIDs)
	}

	// Fetch linked attachments so the response includes them.
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{comment.ID})
	resp := commentToResponse(comment, nil, groupedAtt[uuidToString(comment.ID)])
	slog.Info("comment created", append(logger.RequestAttrs(r), "comment_id", uuidToString(comment.ID), "issue_id", issueID)...)
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), authorType, authorID, map[string]any{
		"comment":             resp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})

	// A reply in a resolved thread re-opens it. Done after CreateComment commits
	// so the reply is visible regardless of the unresolve outcome. Shared with
	// the agent task path (TaskService.createAgentComment) — both reply paths
	// must keep the resolved root in sync.
	h.TaskService.AutoUnresolveThreadOnReply(r.Context(), rootComment, uuidToString(issue.WorkspaceID), authorType, authorID)
	if authorType == "agent" {
		h.TaskService.CancelDeferredEscalationsForIssueAgent(r.Context(), issue.ID, comment.AuthorID)
	}

	originatorUserID := h.invokeOriginatorFromRequest(r, authorType, authorID)
	h.triggerTasksForComment(r.Context(), issue, comment, parentComment, authorType, authorID, originatorUserID, suppressAgentIDs)

	writeJSON(w, http.StatusCreated, resp)
}

// noteCommentPrefix marks a comment as a human-only note. A comment whose first
// whitespace-delimited token is this prefix (case-insensitive) is stored like
// any other comment but never triggers an agent.
const noteCommentPrefix = "/note"

// isNoteComment reports whether content opts out of agent triggering via the
// reserved /note prefix. The prefix must be the comment's first token, so
// "/note check expiry", "  /NOTE", and "/note" all match, while "/notes",
// "/ note", and "see foo/note" do not.
func isNoteComment(content string) bool {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	firstToken := trimmed
	if i := strings.IndexFunc(trimmed, unicode.IsSpace); i >= 0 {
		firstToken = trimmed[:i]
	}
	return strings.EqualFold(firstToken, noteCommentPrefix)
}

func (h *Handler) triggerTasksForComment(ctx context.Context, issue db.Issue, comment db.Comment, parentComment *db.Comment, actorType, actorID, originatorUserID string, suppressAgentIDs []pgtype.UUID) {
	if isNoteComment(comment.Content) {
		return
	}
	triggers := h.computeCommentAgentTriggers(ctx, issue, comment.Content, parentComment, actorType, actorID, commentTriggerComputeOptions{
		ExcludeTriggerCommentID: comment.ID,
		OriginatorUserID:        originatorUserID,
	})
	triggers = filterSuppressedCommentAgentTriggers(triggers, suppressAgentIDs)
	h.enqueueCommentAgentTriggers(ctx, issue, comment.ID, triggers)
}

func filterSuppressedCommentAgentTriggers(triggers []commentAgentTrigger, suppressAgentIDs []pgtype.UUID) []commentAgentTrigger {
	if len(triggers) == 0 || len(suppressAgentIDs) == 0 {
		return triggers
	}
	suppressed := make(map[string]struct{}, len(suppressAgentIDs))
	for _, id := range suppressAgentIDs {
		if id.Valid {
			suppressed[uuidToString(id)] = struct{}{}
		}
	}
	if len(suppressed) == 0 {
		return triggers
	}
	filtered := make([]commentAgentTrigger, 0, len(triggers))
	for _, trigger := range triggers {
		if _, ok := suppressed[uuidToString(trigger.Agent.ID)]; ok {
			continue
		}
		filtered = append(filtered, trigger)
	}
	return filtered
}

func (h *Handler) enqueueCommentAgentTriggers(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, triggers []commentAgentTrigger) {
	var escalationDelay time.Duration
	escalationDelayLoaded := false
	getEscalationDelay := func() time.Duration {
		if !escalationDelayLoaded {
			escalationDelay = h.commentRoutingEscalationDelay(ctx, issue.WorkspaceID)
			escalationDelayLoaded = true
		}
		return escalationDelay
	}
	for _, trigger := range triggers {
		if trigger.AlreadyPending {
			// MUL-4195: a queued/dispatched task for this (issue, agent)
			// already exists. Historically we DROPPED the comment here, losing
			// the user's follow-up instruction. Instead try to fold it into the
			// queued (not-yet-claimed) task so a single run still covers every
			// comment, re-stamping the run's originator/overlay to the new
			// comment (mergeCommentIntoPendingTask).
			if h.mergeCommentIntoPendingTask(ctx, issue, trigger, triggerCommentID) {
				continue
			}
			// The merge found no queued task to fold into: the existing task
			// is already dispatched/running (its claim response is built), or
			// the queued row was just claimed. We must NOT enqueue a
			// fresh queued task in that case — a dispatched sibling would trip
			// the idx_one_pending_task_per_issue_agent unique index (dropping
			// the comment again) and even where the index allows it we'd risk a
			// duplicate concurrent run. When an active task exists, its
			// completion reconcile (reconcileCommentsOnCompletion) is what
			// guarantees this comment earns a bounded follow-up. Only when NO
			// active task exists is a fresh enqueue both safe and necessary.
			if h.hasActiveTaskForIssueAndAgent(ctx, issue.ID, trigger.Agent.ID) {
				continue
			}
		}
		h.enqueueSingleCommentTrigger(ctx, issue, triggerCommentID, trigger, getEscalationDelay)
	}
}

// hasActiveTaskForIssueAndAgent reports whether the (issue, agent) pair has any
// non-terminal task whose completion will drive completion reconciliation. Used
// by the comment enqueue path to decide, after a merge miss, between deferring
// to reconcile (an active task exists) and enqueuing a fresh follow-up (none
// does). Fail-closed: on a DB error we return true so the caller does NOT
// enqueue a possibly-colliding duplicate — worst case the comment is caught by
// reconcile rather than double-run.
func (h *Handler) hasActiveTaskForIssueAndAgent(ctx context.Context, issueID, agentID pgtype.UUID) bool {
	active, err := h.Queries.HasActiveTaskForIssueAndAgent(ctx, db.HasActiveTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: agentID,
	})
	if err != nil {
		slog.Warn("has active task for issue+agent check failed",
			"issue_id", uuidToString(issueID), "agent_id", uuidToString(agentID), "error", err)
		return true
	}
	return active
}

// mergeCommentIntoPendingTask folds a newly-arrived comment into an existing
// not-yet-started task for (issue, agent) instead of dropping it (MUL-4195).
// Returns true when the comment was HANDLED — either merged, or a non-fatal DB
// error we deliberately do not convert into a duplicate task. Returns false
// only when no pending task exists anymore (pgx.ErrNoRows: it was
// claimed/started between the dedup check and now), so the caller enqueues a
// fresh task and the deliberate comment is never lost.
//
// The merge is GATED on the originator being unchanged (MUL-4195 review
// mergeCommentIntoPendingTask folds a newly-arrived comment into the existing
// QUEUED (not-yet-claimed) task for (issue, agent) instead of dropping it
// (MUL-4195). Returns true when the comment was handled (merged, or a
// non-fatal DB error we deliberately do not turn into a duplicate). Returns
// false only when no queued task exists to merge into (pgx.ErrNoRows) — the
// existing task is already dispatched/running, or was just claimed — in which
// case the caller decides between deferring to completion reconcile and a fresh
// enqueue.
//
// Recompute-on-merge (MUL-4195 review must-fix #1): the run's
// originator_user_id, runtime_mcp_overlay and runtime_connected_apps are
// re-stamped to the NEW trigger comment's originator, and trigger_summary is
// refreshed. This is what lets a different member's comment safely fold into a
// task another member created: the single coalescing run then carries the
// latest instruction's originator and the matching connected-app overlay,
// instead of answering the new comment under the original user's capabilities
// and audit identity. It also replaces the earlier originator gate + fresh
// fallback, which could not create a second pending task anyway (the
// one-pending-per-(issue,agent) unique index) and so silently dropped the
// mismatched-originator comment.
func (h *Handler) mergeCommentIntoPendingTask(ctx context.Context, issue db.Issue, trigger commentAgentTrigger, newTriggerCommentID pgtype.UUID) bool {
	originator := h.TaskService.ResolveOriginatorFromTriggerComment(ctx, issue.WorkspaceID, newTriggerCommentID)
	overlay, connectedApps := h.TaskService.BuildRuntimeMCPOverlayForMerge(ctx, originator, trigger.Agent)
	row, err := h.Queries.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:                 issue.ID,
		AgentID:                 trigger.Agent.ID,
		NewTriggerCommentID:     newTriggerCommentID,
		NewOriginatorUserID:     originator,
		NewTriggerSummary:       h.TaskService.BuildCommentTriggerSummary(ctx, issue.WorkspaceID, newTriggerCommentID),
		NewRuntimeMcpOverlay:    overlay,
		NewRuntimeConnectedApps: connectedApps,
	})
	if err != nil {
		if isNotFound(err) {
			// No pre-claim (queued/deferred) task to merge into. The caller
			// defers to completion reconcile when an active task exists, or
			// enqueues fresh when none does.
			return false
		}
		// Unknown error: the pending task most likely still exists, so do NOT
		// risk enqueuing a duplicate. Log and treat as handled.
		slog.Warn("merge comment into pending task failed",
			"issue_id", uuidToString(issue.ID),
			"agent_id", uuidToString(trigger.Agent.ID),
			"error", err)
		return true
	}
	slog.Info("merged comment into pending task",
		"task_id", uuidToString(row.ID),
		"issue_id", uuidToString(issue.ID),
		"agent_id", uuidToString(trigger.Agent.ID),
		"new_trigger_comment_id", uuidToString(newTriggerCommentID),
		"coalesced_count", len(row.CoalescedCommentIds))
	return true
}

// enqueueSingleCommentTrigger creates a fresh task for one computed trigger.
// Split out of enqueueCommentAgentTriggers so the merge-not-drop path
// (MUL-4195) can fall back to it when a pending task vanished mid-flight.
func (h *Handler) enqueueSingleCommentTrigger(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, trigger commentAgentTrigger, getEscalationDelay func() time.Duration) {
	switch trigger.Source {
	case commentTriggerSourceIssueAssignee:
		if trigger.Squad != nil {
			if _, err := h.TaskService.EnqueueTaskForSquadLeader(ctx, issue, trigger.Agent.ID, trigger.Squad.ID, triggerCommentID); err != nil {
				slog.Warn("enqueue squad leader task failed",
					"issue_id", uuidToString(issue.ID),
					"squad_id", uuidToString(trigger.Squad.ID),
					"leader_id", uuidToString(trigger.Agent.ID),
					"error", err)
			}
			return
		}
		if _, err := h.TaskService.EnqueueTaskForIssue(ctx, issue, triggerCommentID); err != nil {
			slog.Warn("enqueue agent task on comment failed", "issue_id", uuidToString(issue.ID), "error", err)
		}
	case commentTriggerSourceMentionSquadLeader:
		if _, err := h.TaskService.EnqueueTaskForSquadLeader(ctx, issue, trigger.Agent.ID, trigger.Squad.ID, triggerCommentID); err != nil {
			slog.Warn("enqueue squad leader mention task failed",
				"issue_id", uuidToString(issue.ID),
				"agent_id", uuidToString(trigger.Agent.ID),
				"error", err)
		}
	case commentTriggerSourceMentionAgent:
		if _, err := h.TaskService.EnqueueTaskForMention(ctx, issue, trigger.Agent.ID, triggerCommentID); err != nil {
			slog.Warn("enqueue mention agent task failed",
				"issue_id", uuidToString(issue.ID),
				"agent_id", uuidToString(trigger.Agent.ID),
				"error", err)
		}
	case commentTriggerSourceThreadParent, commentTriggerSourceConversation:
		var task db.AgentTaskQueue
		var err error
		if trigger.Source == commentTriggerSourceConversation && trigger.Squad != nil {
			task, err = h.TaskService.EnqueueTaskForSquadLeader(ctx, issue, trigger.Agent.ID, trigger.Squad.ID, triggerCommentID)
		} else {
			task, err = h.TaskService.EnqueueTaskForThreadParent(ctx, issue, trigger.Agent.ID, triggerCommentID)
		}
		if err != nil {
			slog.Warn("enqueue routed comment agent task failed",
				"issue_id", uuidToString(issue.ID),
				"agent_id", uuidToString(trigger.Agent.ID),
				"source", trigger.Source,
				"error", err)
			return
		}
		if trigger.EscalationFallback == nil || getEscalationDelay() <= 0 {
			return
		}
		var squadID pgtype.UUID
		if trigger.EscalationFallback.Squad != nil {
			squadID = trigger.EscalationFallback.Squad.ID
		}
		if _, err := h.TaskService.EnqueueDeferredAssigneeFallback(ctx, issue, trigger.EscalationFallback.Agent.ID, squadID, task.ID, triggerCommentID, time.Now().Add(getEscalationDelay())); err != nil {
			slog.Warn("enqueue deferred assignee fallback failed",
				"issue_id", uuidToString(issue.ID),
				"primary_agent_id", uuidToString(trigger.Agent.ID),
				"fallback_agent_id", uuidToString(trigger.EscalationFallback.Agent.ID),
				"error", err)
		}
	}
}

func (h *Handler) computeCommentAgentTriggers(ctx context.Context, issue db.Issue, content string, parentComment *db.Comment, actorType, actorID string, opts commentTriggerComputeOptions) []commentAgentTrigger {
	if isNoteComment(content) {
		return nil
	}

	mentions := util.ParseMentions(content)
	if util.HasMentionAll(mentions) {
		return nil
	}

	if hasAgentOrSquadMention(mentions) {
		return h.resolveMentionedAgentCommentTriggers(ctx, issue, mentions, actorType, actorID, opts)
	}
	if hasMemberMention(mentions) {
		return nil
	}

	if actorType != "member" {
		// Agent-authored comments do not participate in the member-driven
		// conversation routing (parent-author / thread-root continuation) or
		// the member assignee fallback. They retain one narrow path restored
		// after MUL-3794 (MUL-3879): a worker-agent result comment on a
		// squad-assigned issue can still wake the assigned squad leader, so
		// the leader→worker→leader coordination loop stays closed. The leader
		// self-trigger guard lives in
		// routeAssignedSquadLeaderFallback. Explicit @agent / @squad mentions
		// are already handled above, so this never double-enqueues a mentioned
		// target alongside the assigned leader.
		if issue.AssigneeType.Valid && issue.AssigneeType.String == "squad" {
			if trigger, ok := h.routeAssignedSquadLeaderFallback(ctx, issue, actorType, actorID, opts); ok {
				return []commentAgentTrigger{trigger}
			}
		}
		return nil
	}

	if parentComment != nil && parentComment.AuthorType == "agent" {
		trigger, ok := h.routeReplyToParentAuthor(ctx, issue, parentComment, actorType, actorID, opts)
		if !ok {
			return nil
		}
		if fallback, ok := h.routeAssigneeFallback(ctx, issue, actorType, actorID, opts); ok &&
			uuidToString(fallback.Agent.ID) != uuidToString(trigger.Agent.ID) {
			trigger.EscalationFallback = &commentEscalationFallback{
				Agent: fallback.Agent,
				Squad: fallback.Squad,
			}
		}
		return []commentAgentTrigger{trigger}
	}

	if parentComment != nil {
		triggers, handled := h.routeThreadRootOwners(ctx, issue, parentComment, actorID, opts)
		if handled {
			if len(triggers) == 0 {
				return nil
			}
			if len(triggers) == 1 {
				if fallback, ok := h.routeAssigneeFallback(ctx, issue, actorType, actorID, opts); ok &&
					uuidToString(fallback.Agent.ID) != uuidToString(triggers[0].Agent.ID) {
					triggers[0].EscalationFallback = &commentEscalationFallback{
						Agent: fallback.Agent,
						Squad: fallback.Squad,
					}
				}
			}
			return triggers
		}
	}

	if trigger, ok := h.routeAssigneeFallback(ctx, issue, actorType, actorID, opts); ok {
		return []commentAgentTrigger{trigger}
	}
	return nil
}

func hasAgentOrSquadMention(mentions []util.Mention) bool {
	for _, m := range mentions {
		if m.Type == "agent" || m.Type == "squad" {
			return true
		}
	}
	return false
}

func hasMemberMention(mentions []util.Mention) bool {
	for _, m := range mentions {
		if m.Type == "member" {
			return true
		}
	}
	return false
}

func (h *Handler) routeReplyToParentAuthor(ctx context.Context, issue db.Issue, parent *db.Comment, authorType, authorID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool) {
	if parent == nil || parent.AuthorType != "agent" || !parent.AuthorID.Valid {
		return commentAgentTrigger{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          parent.AuthorID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return commentAgentTrigger{}, false
	}
	if !h.canInvokeAgent(ctx, agent, authorType, authorID, opts.OriginatorUserID, uuidToString(issue.WorkspaceID)) {
		return commentAgentTrigger{}, false
	}
	hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, parent.AuthorID, opts)
	if err != nil {
		return commentAgentTrigger{}, false
	}
	return commentAgentTrigger{Agent: agent, Source: commentTriggerSourceThreadParent, AlreadyPending: hasPending}, true
}

type conversationRoutedAgentInfo struct {
	SquadID pgtype.UUID
}

func (h *Handler) routeThreadRootOwners(ctx context.Context, issue db.Issue, parent *db.Comment, memberID string, opts commentTriggerComputeOptions) ([]commentAgentTrigger, bool) {
	if parent == nil || !parent.ID.Valid {
		return nil, false
	}
	root, err := h.Queries.GetThreadRoot(ctx, db.GetThreadRootParams{
		CommentID:   parent.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !root.ID.Valid || root.AuthorType != "member" {
		return nil, false
	}
	return h.routeConversationOwnersForRoot(ctx, issue, root, memberID, opts)
}

func (h *Handler) routeConversationOwnersForRoot(ctx context.Context, issue db.Issue, root db.Comment, memberID string, opts commentTriggerComputeOptions) ([]commentAgentTrigger, bool) {
	if !root.ID.Valid || root.AuthorType != "member" {
		return nil, false
	}
	if trigger, hasExplicitOwner, ok := h.routeFirstExplicitRootMentionOwner(ctx, issue, root, memberID, opts); hasExplicitOwner {
		if !ok {
			return nil, true
		}
		return []commentAgentTrigger{trigger}, true
	}

	rootID := uuidToString(root.ID)
	excludedID := uuidToString(opts.ExcludeTriggerCommentID)

	tasks, err := h.Queries.ListTasksByIssue(ctx, issue.ID)
	if err != nil {
		return nil, false
	}
	routedAgents := make(map[string]conversationRoutedAgentInfo)
	for _, task := range tasks {
		if !task.TriggerCommentID.Valid || !task.AgentID.Valid {
			continue
		}
		if excludedID != "" && uuidToString(task.TriggerCommentID) == excludedID {
			continue
		}
		if uuidToString(task.TriggerCommentID) != rootID {
			continue
		}
		agentID := uuidToString(task.AgentID)
		info := routedAgents[agentID]
		if !info.SquadID.Valid {
			info.SquadID = task.SquadID
		}
		routedAgents[agentID] = info
	}
	if len(routedAgents) == 0 {
		return nil, false
	}

	triggers := make([]commentAgentTrigger, 0, len(routedAgents))
	for agentID, info := range routedAgents {
		trigger, ok := h.routeConversationContinuationToAgent(ctx, issue, parseUUID(agentID), info.SquadID, memberID, opts)
		if ok {
			triggers = append(triggers, trigger)
		}
	}
	return triggers, true
}

func (h *Handler) routeFirstExplicitRootMentionOwner(ctx context.Context, issue db.Issue, root db.Comment, memberID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool, bool) {
	for _, mention := range util.ParseMentions(root.Content) {
		switch mention.Type {
		case "agent":
			agentID, err := util.ParseUUID(mention.ID)
			if err != nil {
				return commentAgentTrigger{}, true, false
			}
			trigger, ok := h.routeConversationContinuationToAgent(ctx, issue, agentID, pgtype.UUID{}, memberID, opts)
			return trigger, true, ok
		case "squad":
			squadID, err := util.ParseUUID(mention.ID)
			if err != nil {
				return commentAgentTrigger{}, true, false
			}
			squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
				ID:          squadID,
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil {
				return commentAgentTrigger{}, true, false
			}
			trigger, ok := h.routeConversationContinuationToAgent(ctx, issue, squad.LeaderID, squadID, memberID, opts)
			return trigger, true, ok
		}
	}
	return commentAgentTrigger{}, false, false
}

func (h *Handler) routeConversationContinuationToAgent(ctx context.Context, issue db.Issue, agentID, squadID pgtype.UUID, memberID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool) {
	if !agentID.Valid {
		return commentAgentTrigger{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          agentID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return commentAgentTrigger{}, false
	}
	if !h.canInvokeAgent(ctx, agent, "member", memberID, memberID, uuidToString(issue.WorkspaceID)) {
		return commentAgentTrigger{}, false
	}
	hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, agentID, opts)
	if err != nil {
		return commentAgentTrigger{}, false
	}
	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceConversation, AlreadyPending: hasPending}
	if squadID.Valid {
		if squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          squadID,
			WorkspaceID: issue.WorkspaceID,
		}); err == nil {
			trigger.Squad = &squad
		}
	}
	return trigger, true
}

func (h *Handler) routeAssigneeFallback(ctx context.Context, issue db.Issue, authorType, authorID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool) {
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return commentAgentTrigger{}, false
	}
	switch issue.AssigneeType.String {
	case "agent":
		agent, hasPending, ok := h.assigneeFallbackAgent(ctx, issue, authorType, authorID, opts)
		if !ok {
			return commentAgentTrigger{}, false
		}
		return commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee, AlreadyPending: hasPending}, true
	case "squad":
		return h.routeAssignedSquadLeaderFallback(ctx, issue, authorType, authorID, opts)
	default:
		return commentAgentTrigger{}, false
	}
}

func (h *Handler) routeAssignedSquadLeaderFallback(ctx context.Context, issue db.Issue, authorType, authorID string, opts commentTriggerComputeOptions) (commentAgentTrigger, bool) {
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return commentAgentTrigger{}, false
	}
	if authorType == "agent" && authorID == uuidToString(squad.LeaderID) &&
		h.shouldSuppressSquadLeaderSelfTrigger(ctx, issue.ID, squad.LeaderID, squad.ID) {
		return commentAgentTrigger{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          squad.LeaderID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return commentAgentTrigger{}, false
	}
	if !h.canInvokeAgent(ctx, agent, authorType, authorID, opts.OriginatorUserID, uuidToString(issue.WorkspaceID)) {
		return commentAgentTrigger{}, false
	}
	hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, squad.LeaderID, opts)
	if err != nil {
		return commentAgentTrigger{}, false
	}
	return commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee, Squad: &squad, AlreadyPending: hasPending}, true
}

func (h *Handler) hasPendingTaskForIssueAndAgent(ctx context.Context, issueID, agentID pgtype.UUID, opts commentTriggerComputeOptions) (bool, error) {
	// Key dedup on the reviewed head so re-pushing to the PR mid-review
	// invalidates dedup and a fresh run enqueues against the new HEAD (TEN-356).
	headSha := h.TaskService.ResolveIssueReviewSHAParam(ctx, issueID)
	if opts.ExcludeTriggerCommentID.Valid {
		return h.Queries.HasPendingTaskForIssueAndAgentExcludingTriggerComment(ctx, db.HasPendingTaskForIssueAndAgentExcludingTriggerCommentParams{
			IssueID:                 issueID,
			AgentID:                 agentID,
			ExcludeTriggerCommentID: opts.ExcludeTriggerCommentID,
			HeadSha:                 headSha,
		})
	}
	return h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: agentID,
		HeadSha: headSha,
	})
}

// resolveMentionedAgentCommentTriggers parses explicit @agent and @squad
// mentions from the current comment and returns the runnable agent recipients.
// Skips agents with on_mention trigger disabled, and private agents mentioned
// by non-owner members (only the agent owner or workspace admin/owner can
// mention a private agent). Self-mentions are intentionally allowed so an
// agent running in one issue can explicitly enqueue itself on another (e.g.
// a child-issue run notifying the parent issue whose assignee is the same
// agent); runaway loops are prevented by HasPendingTaskForIssueAndAgent
// dedupe and the natural queued/dispatched coalescing of the task queue.
// Note: no issue status gate here — @mention is an explicit action and should
// work even on done/cancelled issues (the agent can reopen the issue if needed).
func (h *Handler) resolveMentionedAgentCommentTriggers(ctx context.Context, issue db.Issue, mentions []util.Mention, authorType, authorID string, opts commentTriggerComputeOptions) []commentAgentTrigger {
	wsID := uuidToString(issue.WorkspaceID)
	triggers := make([]commentAgentTrigger, 0, len(mentions))
	seen := make(map[string]struct{}, len(mentions))
	add := func(trigger commentAgentTrigger) {
		id := uuidToString(trigger.Agent.ID)
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		triggers = append(triggers, trigger)
	}
	for _, m := range mentions {
		if m.Type == "squad" {
			// @squad mention → trigger the squad's leader agent.
			squadUUID := parseUUID(m.ID)
			squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
				ID:          squadUUID,
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil {
				continue
			}
			leaderID := squad.LeaderID
			// Prevent self-trigger unless the agent's last activity on this
			// issue was a worker task for this same squad. Generic non-leader
			// tasks (direct @agent mentions, thread-parent replies) are not a
			// worker-role handoff and must not re-enqueue the leader.
			if authorType == "agent" && authorID == uuidToString(leaderID) &&
				h.shouldSuppressSquadLeaderSelfTrigger(ctx, issue.ID, leaderID, squad.ID) {
				continue
			}
			// Verify leader agent is ready (has runtime, not archived).
			agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
				ID:          leaderID,
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
				continue
			}
			// Private-agent gate: prevent triggering a private leader via squad mention.
			if !h.canInvokeAgent(ctx, agent, authorType, authorID, opts.OriginatorUserID, wsID) {
				continue
			}
			hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, leaderID, opts)
			if err != nil {
				continue
			}
			add(commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionSquadLeader, Squad: &squad, AlreadyPending: hasPending})
			continue
		}
		if m.Type != "agent" {
			continue
		}
		agentUUID := parseUUID(m.ID)
		// Load the agent scoped to the current issue's workspace. Using the
		// bare GetAgent here would let a mention resolve to an agent in a
		// different workspace, and the visibility check below would then be
		// applied against the wrong workspace's roles (a workspace owner in
		// THIS workspace would pass the gate for a private agent that lives
		// in someone else's workspace).
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          agentUUID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			continue
		}
		// Private-agent gate (member→private requires allowed_principals;
		// agent→agent always passes).
		if !h.canInvokeAgent(ctx, agent, authorType, authorID, opts.OriginatorUserID, wsID) {
			continue
		}
		hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, agentUUID, opts)
		if err != nil {
			continue
		}
		add(commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent, AlreadyPending: hasPending})
	}
	return triggers
}

func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	commentId := chi.URLParam(r, "commentId")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	commentUUID, ok := parseUUIDOrBadRequest(w, commentId, "comment id")
	if !ok {
		return
	}

	// Load comment scoped to current workspace.
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	existing, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          commentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	isAuthor := existing.AuthorType == actorType && uuidToString(existing.AuthorID) == actorID
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "only comment author or admin can edit")
		return
	}

	var req struct {
		Content          string    `json:"content"`
		AttachmentIDs    *[]string `json:"attachment_ids"`
		SuppressAgentIDs []string  `json:"suppress_agent_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	var attachmentIDs []pgtype.UUID
	replaceAttachments := req.AttachmentIDs != nil
	if replaceAttachments {
		var ok bool
		attachmentIDs, ok = parseUUIDSliceOrBadRequest(w, *req.AttachmentIDs, "attachment_ids")
		if !ok {
			return
		}
	}
	suppressAgentIDs, ok := parseUUIDSliceOrBadRequest(w, req.SuppressAgentIDs, "suppress_agent_ids")
	if !ok {
		return
	}

	// NOTE: See CreateComment — Markdown is sanitized at render/edit time, not here.

	oldContent := existing.Content
	var triggerIssue *db.Issue
	var cancelled []db.AgentTaskQueue
	if oldContent != req.Content {
		issue, err := h.Queries.GetIssue(r.Context(), existing.IssueID)
		if err != nil {
			slog.Warn("load issue for edit post-processing failed", "issue_id", uuidToString(existing.IssueID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to load issue")
			return
		}
		triggerIssue = &issue
		cancelled, err = h.TaskService.CancelTasksByTriggerComment(r.Context(), existing.ID)
		if err != nil {
			slog.Warn("cancel tasks for edited comment failed", "comment_id", uuidToString(existing.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to prepare comment edit")
			return
		}
	}

	comment, err := h.Queries.UpdateComment(r.Context(), db.UpdateCommentParams{
		ID:      commentUUID,
		Content: req.Content,
	})
	if err != nil {
		slog.Warn("update comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", commentId)...)
		if triggerIssue != nil {
			// Cancellation committed but the edit did not. Restore the complete
			// original batch, including the still-valid unchanged comment.
			h.retriggerCancelledTaskSurvivors(r.Context(), *triggerIssue, cancelled, pgtype.UUID{})
		}
		writeError(w, http.StatusInternalServerError, "failed to update comment")
		return
	}
	retriggerEditedComment := func() {
		if oldContent == comment.Content {
			return
		}
		issue := *triggerIssue
		var parentComment *db.Comment
		if existing.ParentID.Valid {
			parent, err := h.Queries.GetComment(r.Context(), existing.ParentID)
			if err == nil {
				parentComment = &parent
			}
		}

		h.retriggerCancelledTaskSurvivors(r.Context(), issue, cancelled, existing.ID)
		h.triggerTasksForComment(r.Context(), issue, comment, parentComment, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), suppressAgentIDs)
	}

	// Replace the comment attachment set when a modern client sends
	// attachment_ids. Older clients omit the field; in that case preserve the
	// existing attachment links rather than unlinking everything.
	if replaceAttachments {
		if err := h.Queries.ReplaceCommentAttachments(r.Context(), db.ReplaceCommentAttachmentsParams{
			CommentID:     comment.ID,
			IssueID:       existing.IssueID,
			AttachmentIds: attachmentIDs,
		}); err != nil {
			slog.Error("failed to replace comment attachments", "error", err)
			// UpdateComment already committed the new body. Even though attachment
			// replacement failed, repair task routing for that persisted edit so a
			// dispatched run cannot permanently keep the old comment version.
			retriggerEditedComment()
			writeError(w, http.StatusInternalServerError, "failed to update attachments")
			return
		}
	}

	// Fetch reactions and attachments for the updated comment.
	grouped := h.groupReactions(r, []pgtype.UUID{comment.ID})
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{comment.ID})
	cid := uuidToString(comment.ID)
	resp := commentToResponse(comment, grouped[cid], groupedAtt[cid])
	slog.Info("comment updated", append(logger.RequestAttrs(r), "comment_id", commentId)...)
	h.publish(protocol.EventCommentUpdated, workspaceID, actorType, actorID, map[string]any{"comment": resp})

	retriggerEditedComment()

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	commentId := chi.URLParam(r, "commentId")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	commentUUID, ok := parseUUIDOrBadRequest(w, commentId, "comment id")
	if !ok {
		return
	}

	// Load comment scoped to current workspace.
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          commentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	isAuthor := comment.AuthorType == actorType && uuidToString(comment.AuthorID) == actorID
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "only comment author or admin can delete")
		return
	}
	issue, err := h.Queries.GetIssue(r.Context(), comment.IssueID)
	if err != nil {
		slog.Warn("load issue for delete post-processing failed", "issue_id", uuidToString(comment.IssueID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load issue")
		return
	}

	// Collect attachment URLs before CASCADE delete removes them.
	attachmentURLs, _ := h.Queries.ListAttachmentURLsByCommentID(r.Context(), comment.ID)

	// Cancel any active task whose planned batch contains this comment so the
	// agent does not run with the now-deleted content already embedded. Must
	// run before DeleteComment because the FK ON DELETE SET NULL would
	// otherwise nullify trigger_comment_id and orphan those tasks in queued.
	cancelled, cancelErr := h.TaskService.CancelTasksByTriggerComment(r.Context(), comment.ID)
	if cancelErr != nil {
		slog.Warn("cancel tasks for deleted trigger comment failed", append(logger.RequestAttrs(r), "error", cancelErr, "comment_id", commentId)...)
	}

	if err := h.Queries.DeleteComment(r.Context(), db.DeleteCommentParams{
		ID:          comment.ID,
		WorkspaceID: comment.WorkspaceID,
	}); err != nil {
		slog.Warn("delete comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", commentId)...)
		// Cancellation already committed but deletion did not. The comment is
		// still valid, so rebuild the complete cancelled batch (including this
		// trigger) before returning the storage error.
		h.retriggerCancelledTaskSurvivors(r.Context(), issue, cancelled, pgtype.UUID{})
		writeError(w, http.StatusInternalServerError, "failed to delete comment")
		return
	}

	h.deleteS3Objects(r.Context(), attachmentURLs)
	slog.Info("comment deleted", append(logger.RequestAttrs(r), "comment_id", commentId, "issue_id", uuidToString(comment.IssueID))...)
	h.publish(protocol.EventCommentDeleted, workspaceID, actorType, actorID, map[string]any{
		"comment_id": uuidToString(comment.ID),
		"issue_id":   uuidToString(comment.IssueID),
	})
	h.retriggerCancelledTaskSurvivors(r.Context(), issue, cancelled, comment.ID)
	w.WriteHeader(http.StatusNoContent)
}

// retriggerCancelledTaskSurvivors repairs the surviving inputs from a
// batch containing a comment that was edited or deleted. Cancellation releases
// the one-pending-task constraint; every other comment is replayed through the
// normal authorization/routing path, scoped to the agent whose task carried
// it. Chronological replay makes those survivors coalesce back into one batch
// and lets the latest real comment restamp originator + connected-app context.
func (h *Handler) retriggerCancelledTaskSurvivors(ctx context.Context, issue db.Issue, cancelled []db.AgentTaskQueue, excludedCommentID pgtype.UUID) {
	if len(cancelled) == 0 {
		return
	}
	targetsByComment := make(map[string]map[string]pgtype.UUID)
	for _, task := range cancelled {
		if !task.AgentID.Valid {
			continue
		}
		plannedCommentIDs := append([]pgtype.UUID{}, task.CoalescedCommentIds...)
		if task.TriggerCommentID.Valid {
			plannedCommentIDs = append(plannedCommentIDs, task.TriggerCommentID)
		}
		for _, commentID := range plannedCommentIDs {
			if !commentID.Valid || (excludedCommentID.Valid && commentID == excludedCommentID) {
				continue
			}
			commentKey := uuidToString(commentID)
			if targetsByComment[commentKey] == nil {
				targetsByComment[commentKey] = make(map[string]pgtype.UUID)
			}
			targetsByComment[commentKey][uuidToString(task.AgentID)] = task.AgentID
		}
	}

	comments := make([]db.Comment, 0, len(targetsByComment))
	for commentID := range targetsByComment {
		comment, err := h.Queries.GetComment(ctx, parseUUID(commentID))
		if err != nil {
			slog.Warn("retrigger cancelled comment batch: load survivor failed",
				"issue_id", uuidToString(issue.ID), "comment_id", commentID, "error", err)
			continue
		}
		if comment.IssueID != issue.ID {
			continue
		}
		comments = append(comments, comment)
	}
	sort.Slice(comments, func(i, j int) bool {
		if !comments[i].CreatedAt.Time.Equal(comments[j].CreatedAt.Time) {
			return comments[i].CreatedAt.Time.Before(comments[j].CreatedAt.Time)
		}
		return uuidToString(comments[i].ID) < uuidToString(comments[j].ID)
	})

	for i := range comments {
		comment := comments[i]
		if isNoteComment(comment.Content) {
			continue
		}
		var parentComment *db.Comment
		if comment.ParentID.Valid {
			if parent, err := h.Queries.GetComment(ctx, comment.ParentID); err == nil {
				parentComment = &parent
			}
		}
		actorType := comment.AuthorType
		actorID := uuidToString(comment.AuthorID)
		originatorUserID := actorID
		if actorType != "member" {
			originatorUserID = uuidToString(h.TaskService.ResolveOriginatorFromTriggerComment(ctx, issue.WorkspaceID, comment.ID))
		}
		triggers := h.computeCommentAgentTriggers(ctx, issue, comment.Content, parentComment, actorType, actorID, commentTriggerComputeOptions{
			ExcludeTriggerCommentID: comment.ID,
			OriginatorUserID:        originatorUserID,
		})
		targets := targetsByComment[uuidToString(comment.ID)]
		scoped := make([]commentAgentTrigger, 0, len(targets))
		for _, trigger := range triggers {
			if _, ok := targets[uuidToString(trigger.Agent.ID)]; ok {
				scoped = append(scoped, trigger)
			}
		}
		if len(scoped) > 0 {
			h.enqueueCommentAgentTriggers(ctx, issue, comment.ID, scoped)
		}
	}
}

// loadCommentForActor resolves a {commentId} URL param to a comment in the
// caller's workspace. Returns the comment, the workspace UUID, the actor
// identity, and ok. Resolve / unresolve handlers share this scaffolding so the
// workspace membership + tenant guard stay identical. Any comment (root or
// reply) may be resolved: resolving a root collapses the whole thread; resolving
// a reply marks it as the thread's resolution. Which one is the thread's
// resolution is a pure frontend derivation, so the backend stays a plain setter.
func (h *Handler) loadCommentForActor(w http.ResponseWriter, r *http.Request) (db.Comment, string, string, string, bool) {
	commentId := chi.URLParam(r, "commentId")
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Comment{}, "", "", "", false
	}
	commentUUID, ok := parseUUIDOrBadRequest(w, commentId, "comment id")
	if !ok {
		return db.Comment{}, "", "", "", false
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Comment{}, "", "", "", false
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return db.Comment{}, "", "", "", false
	}
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          commentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return db.Comment{}, "", "", "", false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	return comment, workspaceID, actorType, actorID, true
}

func (h *Handler) ResolveComment(w http.ResponseWriter, r *http.Request) {
	comment, workspaceID, actorType, actorID, ok := h.loadCommentForActor(w, r)
	if !ok {
		return
	}
	wasResolved := comment.ResolvedAt.Valid

	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid actor id")
		return
	}

	// Single-resolution invariant: a thread has at most one resolved comment, so
	// resolving this one must clear any other resolution in the same thread. Both
	// writes run in one tx — clearing the old resolution and setting the new one
	// is atomic, so a crash can never leave two resolutions (or none) visible.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	cleared, err := qtx.ClearOtherThreadResolutions(r.Context(), db.ClearOtherThreadResolutionsParams{
		TargetID:    comment.ID,
		IssueID:     comment.IssueID,
		WorkspaceID: comment.WorkspaceID,
	})
	if err != nil {
		slog.Warn("clear other thread resolutions failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}

	updated, err := qtx.ResolveComment(r.Context(), db.ResolveCommentParams{
		ID:             comment.ID,
		ResolvedByType: pgtype.Text{String: actorType, Valid: true},
		ResolvedByID:   actorUUID,
	})
	if err != nil {
		slog.Warn("resolve comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("resolve comment commit failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}

	// Emit a comment:unresolved per cleared sibling so granular realtime
	// consumers (which patch a single comment in place) drop the stale
	// resolution instead of showing two. Published after commit so no event ever
	// describes an uncommitted state.
	for _, c := range cleared {
		clearedID := uuidToString(c.ID)
		clearedReactions := h.groupReactions(r, []pgtype.UUID{c.ID})
		clearedAtt := h.groupAttachments(r, []pgtype.UUID{c.ID})
		clearedResp := commentToResponse(c, clearedReactions[clearedID], clearedAtt[clearedID])
		slog.Info("comment unresolved (replaced)", append(logger.RequestAttrs(r), "comment_id", clearedID)...)
		h.publish(protocol.EventCommentUnresolved, workspaceID, actorType, actorID, map[string]any{"comment": clearedResp})
	}

	grouped := h.groupReactions(r, []pgtype.UUID{updated.ID})
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{updated.ID})
	cid := uuidToString(updated.ID)
	resp := commentToResponse(updated, grouped[cid], groupedAtt[cid])

	// Suppress the target event on a re-resolve no-op so consumers do not
	// re-process an unchanged thread (notifications, log spam). Cleared siblings
	// still get their own events above — those rows did change.
	if !wasResolved {
		slog.Info("comment resolved", append(logger.RequestAttrs(r), "comment_id", cid)...)
		h.publish(protocol.EventCommentResolved, workspaceID, actorType, actorID, map[string]any{"comment": resp})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UnresolveComment(w http.ResponseWriter, r *http.Request) {
	comment, workspaceID, actorType, actorID, ok := h.loadCommentForActor(w, r)
	if !ok {
		return
	}
	wasResolved := comment.ResolvedAt.Valid

	updated, err := h.Queries.UnresolveComment(r.Context(), comment.ID)
	if err != nil {
		slog.Warn("unresolve comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to unresolve comment")
		return
	}

	grouped := h.groupReactions(r, []pgtype.UUID{updated.ID})
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{updated.ID})
	cid := uuidToString(updated.ID)
	resp := commentToResponse(updated, grouped[cid], groupedAtt[cid])

	if wasResolved {
		slog.Info("comment unresolved", append(logger.RequestAttrs(r), "comment_id", cid)...)
		h.publish(protocol.EventCommentUnresolved, workspaceID, actorType, actorID, map[string]any{"comment": resp})
	}
	writeJSON(w, http.StatusOK, resp)
}
