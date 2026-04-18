package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/mention"
	"github.com/multica-ai/multica/server/internal/sanitize"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type CommentResponse struct {
	ID          string               `json:"id"`
	IssueID     string               `json:"issue_id"`
	AuthorType  string               `json:"author_type"`
	AuthorID    string               `json:"author_id"`
	Content     string               `json:"content"`
	Type        string               `json:"type"`
	ParentID    *string              `json:"parent_id"`
	CreatedAt   string               `json:"created_at"`
	UpdatedAt   string               `json:"updated_at"`
	Reactions   []ReactionResponse   `json:"reactions"`
	Attachments []AttachmentResponse `json:"attachments"`
}

func commentToResponse(c db.Comment, reactions []ReactionResponse, attachments []AttachmentResponse) CommentResponse {
	if reactions == nil {
		reactions = []ReactionResponse{}
	}
	if attachments == nil {
		attachments = []AttachmentResponse{}
	}
	return CommentResponse{
		ID:          uuidToString(c.ID),
		IssueID:     uuidToString(c.IssueID),
		AuthorType:  c.AuthorType.String,
		AuthorID:    uuidToString(c.AuthorID),
		Content:     c.Content,
		Type:        c.Type,
		ParentID:    uuidToPtr(c.ParentID),
		CreatedAt:   timestampToString(c.CreatedAt),
		UpdatedAt:   timestampToString(c.UpdatedAt),
		Reactions:   reactions,
		Attachments: attachments,
	}
}

func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	// Parse optional pagination query params.
	q := r.URL.Query()
	var limit, offset int32
	var hasPagination bool
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit parameter")
			return
		}
		limit = int32(n)
		hasPagination = true
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset parameter")
			return
		}
		offset = int32(n)
		hasPagination = true
	}

	var sinceTime pgtype.Timestamptz
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since parameter; expected RFC3339 format")
			return
		}
		sinceTime = pgtype.Timestamptz{Time: t, Valid: true}
	}

	var comments []db.Comment
	var err error

	switch {
	case sinceTime.Valid && hasPagination:
		if limit == 0 {
			limit = 50
		}
		comments, err = h.Queries.ListCommentsSincePaginated(r.Context(), db.ListCommentsSincePaginatedParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			CreatedAt:   sinceTime,
			Limit:       limit,
			Offset:      offset,
		})
	case sinceTime.Valid:
		// Apply a server-side cap to prevent unbounded result sets when
		// --since is used without --limit.
		comments, err = h.Queries.ListCommentsSincePaginated(r.Context(), db.ListCommentsSincePaginatedParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			CreatedAt:   sinceTime,
			Limit:       500,
			Offset:      0,
		})
		hasPagination = true
	case hasPagination:
		if limit == 0 {
			limit = 50
		}
		comments, err = h.Queries.ListCommentsPaginated(r.Context(), db.ListCommentsPaginatedParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Limit:       limit,
			Offset:      offset,
		})
	default:
		comments, err = h.Queries.ListComments(r.Context(), db.ListCommentsParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
		})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}

	commentIDs := make([]pgtype.UUID, len(comments))
	for i, c := range comments {
		commentIDs[i] = c.ID
	}
	grouped := h.groupReactions(r, commentIDs)
	groupedAtt := h.groupAttachments(r, commentIDs)

	resp := make([]CommentResponse, len(comments))
	for i, c := range comments {
		cid := uuidToString(c.ID)
		resp[i] = commentToResponse(c, grouped[cid], groupedAtt[cid])
	}

	// Include total count in response header when paginating.
	if hasPagination {
		total, countErr := h.Queries.CountComments(r.Context(), db.CountCommentsParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
		})
		if countErr == nil {
			w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type CreateCommentRequest struct {
	Content       string   `json:"content"`
	Type          string   `json:"type"`
	ParentID      *string  `json:"parent_id"`
	AttachmentIDs []string `json:"attachment_ids"`
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
		parentID = parseUUID(*req.ParentID)
		parent, err := h.Queries.GetComment(r.Context(), parentID)
		if err != nil || uuidToString(parent.IssueID) != issueID {
			writeError(w, http.StatusBadRequest, "invalid parent comment")
			return
		}
		parentComment = &parent
	}

	// Determine author identity: agent (via X-Agent-ID header) or member.
	authorType, authorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))

	// Expand bare issue identifiers (e.g. MUL-117) into mention links.
	req.Content = mention.ExpandIssueIdentifiers(r.Context(), h.Queries, issue.WorkspaceID, req.Content)

	// Sanitize HTML to prevent stored XSS.
	req.Content = sanitize.HTML(req.Content)

	// Phase 3c write-through: when the workspace has a GitLab connection AND
	// the parent issue is GitLab-backed (gitlab_iid + gitlab_project_id), POST
	// the note to GitLab first, then upsert the cache row from the returned
	// representation. Falls through to the legacy direct-DB path only when no
	// connection exists; a connected workspace with a local-only parent issue
	// is a data-integrity error and returns 502 rather than silently diverging.
	if h.GitlabEnabled && h.GitlabResolver != nil {
		_, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), issue.WorkspaceID)
		if wsErr == nil {
			if !issue.GitlabIid.Valid || !issue.GitlabProjectID.Valid {
				slog.Error("gitlab connected workspace but issue has no gitlab refs",
					"issue_id", issueID, "workspace_id", uuidToString(issue.WorkspaceID))
				writeError(w, http.StatusBadGateway, "issue not linked to gitlab")
				return
			}
			h.createCommentWriteThrough(w, r, issue, parentComment, parentID, req, authorType, authorID, issueID)
			return
		}
		// err != nil → fall through to legacy path (non-connected workspace).
	}

	comment, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  pgtype.Text{String: authorType, Valid: true},
		AuthorID:    parseUUID(authorID),
		Content:     req.Content,
		Type:        req.Type,
		ParentID:    parentID,
	})
	if err != nil {
		slog.Warn("create comment failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to create comment: "+err.Error())
		return
	}

	// Link uploaded attachments to this comment.
	if len(req.AttachmentIDs) > 0 {
		h.linkAttachmentsByIDs(r.Context(), comment.ID, issue.ID, req.AttachmentIDs)
	}

	// Fetch linked attachments so the response includes them.
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{comment.ID})
	resp := commentToResponse(comment, nil, groupedAtt[uuidToString(comment.ID)])
	slog.Info("comment created", append(logger.RequestAttrs(r), "comment_id", uuidToString(comment.ID), "issue_id", issueID)...)
	h.publishCommentCreated(issue, resp, authorType, authorID)

	// Post-commit side effects: on_comment agent trigger + @mention fan-out.
	h.runCommentCreatePostCommit(r.Context(), issue, comment, parentComment, authorType, authorID, issueID)

	writeJSON(w, http.StatusCreated, resp)
}

// publishCommentCreated publishes the protocol.EventCommentCreated event with
// the shared payload shape used by both the legacy and write-through paths.
func (h *Handler) publishCommentCreated(issue db.Issue, resp CommentResponse, authorType, authorID string) {
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), authorType, authorID, map[string]any{
		"comment":             resp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})
}

// runCommentCreatePostCommit executes the on_comment agent trigger + @mention
// fan-out side effects after a comment is persisted. Shared between the
// legacy direct-DB path and the GitLab write-through path so both branches
// produce identical task-queue side effects.
func (h *Handler) runCommentCreatePostCommit(
	ctx context.Context,
	issue db.Issue,
	comment db.Comment,
	parentComment *db.Comment,
	authorType, authorID, issueID string,
) {
	// If the issue is assigned to an agent with on_comment trigger, enqueue a new task.
	// Skip when the comment comes from the assigned agent itself to avoid loops.
	// Also skip when the comment @mentions others but not the assignee agent —
	// the user is talking to someone else, not requesting work from the assignee.
	// Also skip when replying in a member-started thread without mentioning the
	// assignee — the user is continuing a member-to-member conversation.
	if authorType == "member" && h.shouldEnqueueOnComment(ctx, issue) &&
		!h.commentMentionsOthersButNotAssignee(comment.Content, issue) &&
		!h.isReplyToMemberThread(ctx, parentComment, comment.Content, issue) {
		// Resolve thread root: if the comment is a reply, agent should reply
		// to the thread root (matching frontend behavior where all replies
		// in a thread share the same top-level parent).
		replyTo := comment.ID
		if comment.ParentID.Valid {
			replyTo = comment.ParentID
		}
		if _, err := h.TaskService.EnqueueTaskForIssue(ctx, issue, replyTo); err != nil {
			slog.Warn("enqueue agent task on comment failed", "issue_id", issueID, "error", err)
		}
	}

	// Trigger @mentioned agents: parse agent mentions and enqueue tasks for each.
	// Pass parentComment so that replies inherit mentions from the thread root.
	h.enqueueMentionedAgentTasks(ctx, issue, comment, parentComment, authorType, authorID)
}

// createCommentWriteThrough implements the Phase 3c write-through branch of
// POST /api/issues/{id}/comments: POST the note to GitLab, then upsert the
// cache row from the returned representation inside a transaction. Threads
// parent_id (Multica-native) + attachments through the same txn so a partial
// failure rolls the whole thing back.
//
// On GitLab error returns a non-2xx status and aborts — we must NOT fall
// through to the legacy path, which would produce orphaned cache rows on a
// connected workspace.
func (h *Handler) createCommentWriteThrough(
	w http.ResponseWriter,
	r *http.Request,
	issue db.Issue,
	parentComment *db.Comment,
	parentID pgtype.UUID,
	req CreateCommentRequest,
	authorType, authorID, issueID string,
) {
	ctx := r.Context()
	workspaceID := uuidToString(issue.WorkspaceID)

	token, _, err := h.GitlabResolver.ResolveTokenForWrite(ctx, workspaceID, authorType, authorID)
	if err != nil {
		slog.Error("resolve gitlab token", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
		return
	}

	// Resolve the agent slug (if any) so BuildCreateNoteBody can emit the
	// canonical "**[agent:<slug>]** " prefix that TranslateNote round-trips
	// on webhook replay.
	var agentSlug string
	if authorType == "agent" {
		agentMap, err := h.buildAgentUUIDSlugMap(ctx, issue.WorkspaceID)
		if err != nil {
			slog.Error("agent slug map", "error", err, "workspace_id", workspaceID)
			writeError(w, http.StatusInternalServerError, "build agent map failed")
			return
		}
		agentSlug = agentMap[authorID]
	}

	glBody := gitlabsync.BuildCreateNoteBody(authorType, agentSlug, req.Content)

	glNote, err := h.Gitlab.CreateNote(ctx,
		token,
		issue.GitlabProjectID.Int64,
		int(issue.GitlabIid.Int32),
		glBody,
	)
	if err != nil {
		slog.Error("gitlab create note", "error", err, "issue_id", issueID)
		writeError(w, http.StatusBadGateway, "gitlab create note failed")
		return
	}

	nv := gitlabsync.TranslateNote(*glNote)

	// Author resolution mirrors the webhook handler: Multica actor refs
	// (author_type/author_id) come from the requesting user (member or
	// agent), while gitlab_author_user_id is the GitLab-side author id echoed
	// back on the note payload. This keeps the cache consistent with the
	// webhook-written shape so a subsequent webhook replay hits the clobber
	// guard and short-circuits.
	var authorTypeCol pgtype.Text
	var authorIDCol pgtype.UUID
	if authorType != "" {
		authorTypeCol = pgtype.Text{String: authorType, Valid: true}
		authorIDCol = parseUUID(authorID)
	}
	var glAuthor pgtype.Int8
	if nv.GitlabUserID != 0 {
		glAuthor = pgtype.Int8{Int64: nv.GitlabUserID, Valid: true}
	} else if glNote.Author.ID != 0 {
		glAuthor = pgtype.Int8{Int64: glNote.Author.ID, Valid: true}
	}

	externalUpdatedAt := parseGitlabTS(nv.UpdatedAt)

	glTx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Error("begin gitlab write-through tx (comment)", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}
	defer glTx.Rollback(ctx)
	qtxGL := h.Queries.WithTx(glTx)

	comment, err := qtxGL.UpsertCommentFromGitlab(ctx, db.UpsertCommentFromGitlabParams{
		WorkspaceID:        issue.WorkspaceID,
		IssueID:            issue.ID,
		AuthorType:         authorTypeCol,
		AuthorID:           authorIDCol,
		GitlabAuthorUserID: glAuthor,
		Content:            nv.Body,
		Type:               nv.Type,
		GitlabNoteID:       pgtype.Int8{Int64: glNote.ID, Valid: true},
		ExternalUpdatedAt:  externalUpdatedAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Clobber guard short-circuited: a concurrent webhook wrote a
			// newer-or-equal row. Load the existing cache copy so the
			// response still reflects reality, then skip the rest of the
			// write-through bookkeeping (parent_id + attachments). Mirrors
			// the webhook handler's tolerance for this case.
			existing, loadErr := qtxGL.GetCommentByGitlabNoteID(ctx,
				pgtype.Int8{Int64: glNote.ID, Valid: true})
			if loadErr != nil {
				slog.Error("load comment after clobber-guard short-circuit",
					"error", loadErr, "gitlab_note_id", glNote.ID)
				writeError(w, http.StatusInternalServerError, "failed to create comment")
				return
			}
			if commitErr := glTx.Commit(ctx); commitErr != nil {
				slog.Error("commit gitlab write-through tx (comment, no-op)",
					"error", commitErr)
				writeError(w, http.StatusInternalServerError, "failed to create comment")
				return
			}
			resp := commentToResponse(existing, nil, nil)
			slog.Info("comment created (clobber-guard preserved existing cache)",
				append(logger.RequestAttrs(r), "comment_id", uuidToString(existing.ID),
					"issue_id", issueID)...)
			h.publishCommentCreated(issue, resp, authorType, authorID)
			h.runCommentCreatePostCommit(ctx, issue, existing, parentComment, authorType, authorID, issueID)
			writeJSON(w, http.StatusCreated, resp)
			return
		}
		slog.Error("upsert gitlab comment cache row", "error", err)
		writeError(w, http.StatusInternalServerError, "cache upsert failed")
		return
	}

	// Thread parent_id (Multica-native; not round-tripped through GitLab).
	if parentID.Valid {
		patched, err := qtxGL.UpdateCommentParent(ctx, db.UpdateCommentParentParams{
			ID:       comment.ID,
			ParentID: parentID,
		})
		if err != nil {
			slog.Error("patch parent_id on gitlab comment cache row", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create comment")
			return
		}
		comment = patched
	}

	// Link uploaded attachments inside the same txn so a partial failure
	// rolls the whole thing back.
	if len(req.AttachmentIDs) > 0 {
		attachmentUUIDs := make([]pgtype.UUID, len(req.AttachmentIDs))
		for i, id := range req.AttachmentIDs {
			attachmentUUIDs[i] = parseUUID(id)
		}
		if err := qtxGL.LinkAttachmentsToComment(ctx, db.LinkAttachmentsToCommentParams{
			CommentID: comment.ID,
			IssueID:   issue.ID,
			Column3:   attachmentUUIDs,
		}); err != nil {
			slog.Error("link attachments to gitlab comment", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create comment")
			return
		}
	}

	if err := glTx.Commit(ctx); err != nil {
		slog.Error("commit gitlab write-through tx (comment)", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}

	// Fetch linked attachments so the response carries the populated slice.
	var attachments []AttachmentResponse
	if len(req.AttachmentIDs) > 0 {
		groupedAtt := h.groupAttachments(r, []pgtype.UUID{comment.ID})
		attachments = groupedAtt[uuidToString(comment.ID)]
	}
	resp := commentToResponse(comment, nil, attachments)

	slog.Info("comment created (gitlab write-through)",
		append(logger.RequestAttrs(r), "comment_id", uuidToString(comment.ID),
			"issue_id", issueID, "gitlab_note_id", glNote.ID)...)
	h.publishCommentCreated(issue, resp, authorType, authorID)

	// Post-commit side effects: mirror the legacy path exactly so trigger
	// behavior is identical on connected vs non-connected workspaces.
	h.runCommentCreatePostCommit(ctx, issue, comment, parentComment, authorType, authorID, issueID)

	writeJSON(w, http.StatusCreated, resp)
}

// parseGitlabTS parses a GitLab RFC3339 timestamp into pgtype.Timestamptz.
// Invalid or empty input produces a NULL timestamp — matching the behaviour
// of the sync-side parseTS so clobber-guard comparisons stay consistent.
func parseGitlabTS(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}


// commentMentionsOthersButNotAssignee returns true if the comment @mentions
// anyone but does NOT @mention the issue's assignee agent. This is used to
// suppress the on_comment trigger when the user is directing their comment at
// someone else (e.g. sharing results with a colleague, asking another agent).
// @all is treated as a broadcast — it suppresses the trigger because the user
// is announcing to everyone, not specifically requesting work from the agent.
func (h *Handler) commentMentionsOthersButNotAssignee(content string, issue db.Issue) bool {
	mentions := util.ParseMentions(content)
	// Filter out issue mentions — they are cross-references, not @people.
	filtered := mentions[:0]
	for _, m := range mentions {
		if m.Type != "issue" {
			filtered = append(filtered, m)
		}
	}
	mentions = filtered
	if len(mentions) == 0 {
		return false // No mentions (or only issue refs) — normal on_comment behavior
	}
	// @all is a broadcast to all members — suppress agent trigger.
	if util.HasMentionAll(mentions) {
		return true
	}
	if !issue.AssigneeID.Valid {
		return true // No assignee — mentions target others
	}
	assigneeID := uuidToString(issue.AssigneeID)
	for _, m := range mentions {
		if m.ID == assigneeID {
			return false // Assignee is mentioned — allow trigger
		}
	}
	return true // Others mentioned but not assignee — suppress trigger
}

// isReplyToMemberThread returns true if the comment is a reply in a thread
// started by a member and does NOT @mention the issue's assignee agent.
// When a member replies in a member-started thread, they are most likely
// continuing a human conversation — not requesting work from the assigned agent.
// Replying to an agent-started thread, or explicitly @mentioning the assignee
// in the reply, still triggers on_comment as expected.
// If the parent (thread root) itself @mentions the assignee, the thread is
// considered a conversation with the agent, so replies are allowed to trigger.
// If the assigned agent has already replied in the thread, the member is
// conversing with the agent, so replies are allowed to trigger.
func (h *Handler) isReplyToMemberThread(ctx context.Context, parent *db.Comment, content string, issue db.Issue) bool {
	if parent == nil {
		return false // Not a reply — normal top-level comment
	}
	if parent.AuthorType.String != "member" {
		return false // Thread started by an agent — allow trigger
	}
	// Thread was started by a member. Suppress on_comment unless the reply
	// or the parent explicitly @mentions the assignee agent, or the agent
	// has already participated in this thread.
	if !issue.AssigneeID.Valid {
		return true // No assignee to mention
	}
	assigneeID := uuidToString(issue.AssigneeID)
	// Check current comment mentions.
	for _, m := range util.ParseMentions(content) {
		if m.ID == assigneeID {
			return false // Assignee explicitly mentioned in reply — allow trigger
		}
	}
	// Check parent (thread root) mentions — if the thread was started by
	// mentioning the assignee, replies continue that conversation.
	for _, m := range util.ParseMentions(parent.Content) {
		if m.ID == assigneeID {
			return false // Assignee mentioned in thread root — allow trigger
		}
	}
	// Check if the assigned agent has already replied in this thread —
	// if so, the member is continuing a conversation with the agent.
	if h.Queries != nil {
		hasReplied, err := h.Queries.HasAgentRepliedInThread(ctx, db.HasAgentRepliedInThreadParams{
			ParentID: parent.ID,
			AgentID:  issue.AssigneeID,
		})
		if err == nil && hasReplied {
			return false // Agent participated in thread — allow trigger
		}
	}
	return true // Reply to member thread without agent participation — suppress
}

// enqueueMentionedAgentTasks parses @agent mentions from comment content and
// enqueues a task for each mentioned agent. When parentComment is non-nil
// (i.e. the comment is a reply), mentions from the parent (thread root) are
// also included so that agents mentioned in the top-level comment are
// re-triggered by subsequent replies in the same thread — unless the reply
// explicitly @mentions only non-agent entities (members, issues), which
// signals the user is talking to other people and not the agent.
// Skips self-mentions, agents with on_mention trigger disabled, and private
// agents mentioned by non-owner members (only the agent owner or workspace
// admin/owner can mention a private agent).
// Note: no status gate here — @mention is an explicit action and should work
// even on done/cancelled issues (the agent can reopen the issue if needed).
func (h *Handler) enqueueMentionedAgentTasks(ctx context.Context, issue db.Issue, comment db.Comment, parentComment *db.Comment, authorType, authorID string) {
	wsID := uuidToString(issue.WorkspaceID)
	mentions := util.ParseMentions(comment.Content)
	// When replying in a thread, inherit mentions from the parent comment
	// so that agents mentioned in the thread root are triggered by replies —
	// but only when the reply contains no mentions at all (a plain follow-up).
	// If the reply explicitly @mentions anyone (agents or members), the user
	// is making a deliberate choice about who to involve; don't auto-inherit.
	if parentComment != nil && len(mentions) == 0 {
		mentions = util.ParseMentions(parentComment.Content)
	}
	for _, m := range mentions {
		if m.Type != "agent" {
			continue
		}
		// Prevent self-trigger: skip if the comment author is this agent.
		if authorType == "agent" && authorID == m.ID {
			continue
		}
		agentUUID := parseUUID(m.ID)
		// Load the agent to check visibility, archive status, and trigger config.
		agent, err := h.Queries.GetAgent(ctx, agentUUID)
		if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			continue
		}
		// Private agents can only be mentioned by the agent owner or workspace admin/owner.
		if agent.Visibility == "private" && authorType == "member" {
			isOwner := uuidToString(agent.OwnerID) == authorID
			if !isOwner {
				member, err := h.getWorkspaceMember(ctx, authorID, wsID)
				if err != nil || !roleAllowed(member.Role, "owner", "admin") {
					continue
				}
			}
		}
		// Dedup: skip if this agent already has a pending task for this issue.
		hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
			IssueID: issue.ID,
			AgentID: agentUUID,
		})
		if err != nil || hasPending {
			continue
		}
		// Always use the current comment as the trigger so the agent reads the
		// actual reply that mentioned it, not the thread root.
		if _, err := h.TaskService.EnqueueTaskForMention(ctx, issue, agentUUID, comment.ID); err != nil {
			slog.Warn("enqueue mention agent task failed", "issue_id", uuidToString(issue.ID), "agent_id", m.ID, "error", err)
		}
	}
}

func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	commentId := chi.URLParam(r, "commentId")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Load comment scoped to current workspace.
	workspaceID := h.resolveWorkspaceID(r)
	existing, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          parseUUID(commentId),
		WorkspaceID: parseUUID(workspaceID),
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
	// Synced rows from GitLab may have NULL author_type/author_id (Phase 3 will
	// backfill once user_gitlab_connection lookup is wired). Treat NULL as
	// "not the author" rather than coincidentally matching empty strings.
	isAuthor := existing.AuthorType.Valid && existing.AuthorType.String == actorType &&
		existing.AuthorID.Valid && uuidToString(existing.AuthorID) == actorID
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "only comment author or admin can edit")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Sanitize HTML to prevent stored XSS.
	req.Content = sanitize.HTML(req.Content)

	comment, err := h.Queries.UpdateComment(r.Context(), db.UpdateCommentParams{
		ID:      parseUUID(commentId),
		Content: req.Content,
	})
	if err != nil {
		slog.Warn("update comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", commentId)...)
		writeError(w, http.StatusInternalServerError, "failed to update comment")
		return
	}

	// Fetch reactions and attachments for the updated comment.
	grouped := h.groupReactions(r, []pgtype.UUID{comment.ID})
	groupedAtt := h.groupAttachments(r, []pgtype.UUID{comment.ID})
	cid := uuidToString(comment.ID)
	resp := commentToResponse(comment, grouped[cid], groupedAtt[cid])
	slog.Info("comment updated", append(logger.RequestAttrs(r), "comment_id", commentId)...)
	h.publish(protocol.EventCommentUpdated, workspaceID, actorType, actorID, map[string]any{"comment": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	commentId := chi.URLParam(r, "commentId")

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Load comment scoped to current workspace.
	workspaceID := h.resolveWorkspaceID(r)
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          parseUUID(commentId),
		WorkspaceID: parseUUID(workspaceID),
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
	// Synced rows from GitLab may have NULL author_type/author_id (Phase 3 will
	// backfill once user_gitlab_connection lookup is wired). Treat NULL as
	// "not the author" rather than coincidentally matching empty strings.
	isAuthor := comment.AuthorType.Valid && comment.AuthorType.String == actorType &&
		comment.AuthorID.Valid && uuidToString(comment.AuthorID) == actorID
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "only comment author or admin can delete")
		return
	}

	// Collect attachment URLs before CASCADE delete removes them.
	attachmentURLs, _ := h.Queries.ListAttachmentURLsByCommentID(r.Context(), parseUUID(commentId))

	if err := h.Queries.DeleteComment(r.Context(), parseUUID(commentId)); err != nil {
		slog.Warn("delete comment failed", append(logger.RequestAttrs(r), "error", err, "comment_id", commentId)...)
		writeError(w, http.StatusInternalServerError, "failed to delete comment")
		return
	}

	h.deleteS3Objects(r.Context(), attachmentURLs)
	slog.Info("comment deleted", append(logger.RequestAttrs(r), "comment_id", commentId, "issue_id", uuidToString(comment.IssueID))...)
	h.publish(protocol.EventCommentDeleted, workspaceID, actorType, actorID, map[string]any{
		"comment_id": commentId,
		"issue_id":   uuidToString(comment.IssueID),
	})
	w.WriteHeader(http.StatusNoContent)
}
