package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/mention"
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
		AuthorType:  c.AuthorType,
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

	comment, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  authorType,
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
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), authorType, authorID, map[string]any{
		"comment":             resp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})

	// If the issue is assigned to an agent with on_comment trigger, enqueue a new task.
	// Skip when the comment comes from the assigned agent itself to avoid loops.
	// Also skip when the comment @mentions others but not the assignee agent —
	// the user is talking to someone else, not requesting work from the assignee.
	// Also skip when replying in a member-started thread without mentioning the
	// assignee — the user is continuing a member-to-member conversation.
	if authorType == "member" && h.shouldEnqueueOnComment(r.Context(), issue) &&
		!h.commentMentionsOthersButNotAssignee(comment.Content, issue) &&
		!h.isReplyToMemberThread(parentComment, comment.Content, issue) {
		// Resolve thread root: if the comment is a reply, agent should reply
		// to the thread root (matching frontend behavior where all replies
		// in a thread share the same top-level parent).
		replyTo := comment.ID
		if comment.ParentID.Valid {
			replyTo = comment.ParentID
		}
		if _, err := h.TaskService.EnqueueTaskForIssue(r.Context(), issue, replyTo); err != nil {
			slog.Warn("enqueue agent task on comment failed", "issue_id", issueID, "error", err)
		}
	}

	// Trigger @mentioned agents: parse agent mentions and enqueue tasks for each.
	// Pass parentComment so that replies inherit mentions from the thread root.
	h.enqueueMentionedAgentTasks(r.Context(), issue, comment, parentComment, authorType, authorID)

	writeJSON(w, http.StatusCreated, resp)
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
func (h *Handler) isReplyToMemberThread(parent *db.Comment, content string, issue db.Issue) bool {
	if parent == nil {
		return false // Not a reply — normal top-level comment
	}
	if parent.AuthorType != "member" {
		return false // Thread started by an agent — allow trigger
	}
	// Thread was started by a member. Suppress on_comment unless the reply
	// or the parent explicitly @mentions the assignee agent.
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
	return true // Reply to member thread without mentioning agent — suppress
}

// enqueueMentionedAgentTasks parses @agent mentions from comment content and
// enqueues a task for each mentioned agent. When parentComment is non-nil
// (i.e. the comment is a reply), mentions from the parent (thread root) are
// also included so that agents mentioned in the top-level comment are
// re-triggered by subsequent replies in the same thread.
// Skips self-mentions, agents that are already the issue's assignee (handled
// by on_comment), agents with on_mention trigger disabled, and private agents
// mentioned by non-owner members (only the agent owner or workspace
// admin/owner can mention a private agent).
// Note: no status gate here — @mention is an explicit action and should work
// even on done/cancelled issues (the agent can reopen the issue if needed).
func (h *Handler) enqueueMentionedAgentTasks(ctx context.Context, issue db.Issue, comment db.Comment, parentComment *db.Comment, authorType, authorID string) {
	wsID := uuidToString(issue.WorkspaceID)
	mentions := util.ParseMentions(comment.Content)
	// When replying in a thread, also include mentions from the parent comment
	// so that agents mentioned in the thread root are triggered by replies.
	if parentComment != nil {
		parentMentions := util.ParseMentions(parentComment.Content)
		seen := make(map[string]bool, len(mentions))
		for _, m := range mentions {
			seen[m.Type+":"+m.ID] = true
		}
		for _, m := range parentMentions {
			if !seen[m.Type+":"+m.ID] {
				mentions = append(mentions, m)
				seen[m.Type+":"+m.ID] = true
			}
		}
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
		// Prevent duplicate: skip if this agent is the issue's assignee
		// (already handled by the on_comment trigger above) — but only
		// when the issue is in a non-terminal status where on_comment
		// will actually fire. For done/cancelled issues on_comment is
		// suppressed, so an explicit @mention must still go through.
		isAssignee := issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" &&
			issue.AssigneeID.Valid && uuidToString(issue.AssigneeID) == m.ID
		isTerminal := issue.Status == "done" || issue.Status == "cancelled"
		if isAssignee && !isTerminal {
			continue
		}
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
		// Check if the agent has on_mention trigger enabled.
		if !agentHasTriggerEnabled(agent.Triggers, "on_mention") {
			continue
		}
		// Dedup: skip if this agent already has a pending task for this issue.
		hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
			IssueID: issue.ID,
			AgentID: agentUUID,
		})
		if err != nil || hasPending {
			continue
		}
		// Resolve thread root for reply threading.
		replyTo := comment.ID
		if comment.ParentID.Valid {
			replyTo = comment.ParentID
		}
		if _, err := h.TaskService.EnqueueTaskForMention(ctx, issue, agentUUID, replyTo); err != nil {
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
	workspaceID := resolveWorkspaceID(r)
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
	isAuthor := existing.AuthorType == actorType && uuidToString(existing.AuthorID) == actorID
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
	workspaceID := resolveWorkspaceID(r)
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
	isAuthor := comment.AuthorType == actorType && uuidToString(comment.AuthorID) == actorID
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
