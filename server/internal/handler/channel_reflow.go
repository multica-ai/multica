package handler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// issueStatusReflowLabel maps an issue status to the short Chinese label shown
// in the source channel thread. Only status changes are reflowed back — never
// any agent-internal processing detail.
func issueStatusReflowLabel(status string) string {
	switch status {
	case "todo":
		return "待处理"
	case "in_progress":
		return "进行中"
	case "in_review":
		return "评审中"
	case "done":
		return "已完成"
	case "blocked":
		return "已阻塞"
	case "backlog":
		return "待规划"
	case "cancelled":
		return "已关闭"
	default:
		return status
	}
}

// linkIssueToThread attaches a freshly-created issue to its source thread and
// posts a "created from thread" system message back into the thread. Best
// effort: failures are logged by callers' context, never block issue creation.
func (h *Handler) linkIssueToThread(ctx context.Context, issue *db.Issue, threadIDStr string) {
	threadUUID, err := parseUUIDErr(threadIDStr)
	if err != nil {
		return
	}
	thread, err := h.Queries.GetChannelThread(ctx, threadUUID)
	if err != nil {
		return
	}
	if uuidToString(thread.WorkspaceID) != uuidToString(issue.WorkspaceID) {
		return // cross-workspace linkage is not allowed
	}
	h.linkIssueToExistingThread(ctx, issue, thread)
}

// linkIssueToExistingThread is the inner half of linkIssueToThread once the
// thread row is in hand. Shared with the agent-convert completion path, which
// resolves the thread via ensureThreadForMessage and then reuses this so both
// conversion routes (manual vs agent) produce structurally identical issues.
func (h *Handler) linkIssueToExistingThread(ctx context.Context, issue *db.Issue, thread db.ChannelThread) {
	if uuidToString(thread.WorkspaceID) != uuidToString(issue.WorkspaceID) {
		return // cross-workspace linkage is not allowed
	}
	if err := h.Queries.LinkIssueSource(ctx, db.LinkIssueSourceParams{
		ID:              issue.ID,
		SourceChannelID: thread.ChannelID,
		SourceThreadID:  thread.ID,
	}); err != nil {
		return
	}
	// Reflect the linkage on the in-memory issue so the create response carries it.
	issue.SourceChannelID = thread.ChannelID
	issue.SourceThreadID = thread.ID
	h.linkIssueToThreadActivity(ctx, issue, thread)
}

// ensureThreadForMessage returns the thread anchored at the given message,
// creating one implicitly when the message has no thread yet — the same
// invariant ConvertMessageToIssue upholds so every issue-producing message
// has a thread to link against. createdBy is the user to attribute the implicit
// thread to. channel_thread.created_by REFERENCES "user"(id), so callers MUST
// pass a zero UUID (NULL) when the creator is an agent — agents live in the
// agent table and would otherwise violate the FK. ok=false on any
// lookup/creation failure (best-effort).
func (h *Handler) ensureThreadForMessage(ctx context.Context, channelID, messageID, workspaceID, createdBy pgtype.UUID) (db.ChannelThread, bool) {
	msg, err := h.Queries.GetChannelMessage(ctx, messageID)
	if err != nil || uuidToString(msg.ChannelID) != uuidToString(channelID) {
		return db.ChannelThread{}, false
	}
	if thread, err := h.Queries.GetThreadByRootMessage(ctx, messageID); err == nil {
		return thread, true
	}
	title := truncateUTF8(msg.Content, 50)
	thread, err := h.Queries.CreateChannelThread(ctx, db.CreateChannelThreadParams{
		ChannelID:     channelID,
		WorkspaceID:   workspaceID,
		Title:         title,
		CreatedBy:     createdBy,
		RootMessageID: messageID,
	})
	if err != nil {
		return db.ChannelThread{}, false
	}
	return thread, true
}

// LinkQuickCreateIssueToSource implements service.QuickCreateSourceLinker. It
// is the completion-callback counterpart of ConvertMessageToIssue: the
// agent-created issue is attached to the source channel thread so the two
// conversion routes (manual vs agent) produce structurally identical issues
// and both enjoy the bidirectional display + status reflow. Best-effort and
// idempotent — skips when the issue is already source-linked.
func (h *Handler) LinkQuickCreateIssueToSource(ctx context.Context, issue db.Issue, sourceChannelID, sourceMessageID, requesterID pgtype.UUID) {
	if issue.SourceThreadID.Valid {
		return
	}
	thread, ok := h.ensureThreadForMessage(ctx, sourceChannelID, sourceMessageID, issue.WorkspaceID, requesterID)
	if !ok {
		slog.Warn("quick-create source linkage: ensure thread failed",
			"issue_id", uuidToString(issue.ID),
			"source_channel_id", uuidToString(sourceChannelID),
			"source_message_id", uuidToString(sourceMessageID),
		)
		return
	}
	h.linkIssueToExistingThread(ctx, &issue, thread)
}

// linkIssueToThreadActivity posts a "created from thread" system message into
// the thread and a top-level channel activity. Called after the issue-thread
// linkage is already persisted.
func (h *Handler) linkIssueToThreadActivity(ctx context.Context, issue *db.Issue, thread db.ChannelThread) {
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	ident := fmt.Sprintf("%s-%d", prefix, issue.Number)
	content := fmt.Sprintf("从本线程创建了 Issue %s：%s", ident, issue.Title)
	h.postThreadSystemMessage(ctx, thread, content, map[string]any{
		"kind":     "issue_created",
		"issue_id": uuidToString(issue.ID),
	})
}

// reflowIssueStatus posts a system activity message into the issue's source
// thread and channel main timeline when the issue's status changes. No-op when
// the issue did not come from a thread.
func (h *Handler) reflowIssueStatus(ctx context.Context, issue db.Issue) {
	if !issue.SourceThreadID.Valid {
		return
	}
	thread, err := h.Queries.GetChannelThread(ctx, issue.SourceThreadID)
	if err != nil {
		return
	}
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	ident := fmt.Sprintf("%s-%d", prefix, issue.Number)
	content := fmt.Sprintf("%s %s", ident, issueStatusReflowLabel(issue.Status))
	// Post into the thread.
	h.postThreadSystemMessage(ctx, thread, content, map[string]any{
		"kind":     "issue_status",
		"issue_id": uuidToString(issue.ID),
		"status":   issue.Status,
	})
	// Also post a top-level message into the channel main timeline.
	h.postChannelSystemMessage(ctx, thread.ChannelID, thread.WorkspaceID, content, map[string]any{
		"kind":      "issue_status",
		"issue_id":  uuidToString(issue.ID),
		"status":    issue.Status,
		"thread_id": uuidToString(thread.ID),
	})
}

// postThreadSystemMessage inserts a system-authored message into a thread and
// broadcasts it. author_id is null for system messages.
func (h *Handler) postThreadSystemMessage(ctx context.Context, thread db.ChannelThread, content string, extra map[string]any) {
	msg, err := h.Queries.CreateChannelMessage(ctx, db.CreateChannelMessageParams{
		ThreadID:    thread.ID,
		ChannelID:   thread.ChannelID,
		WorkspaceID: thread.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{},
		Content:     content,
	})
	if err != nil {
		return
	}
	h.Queries.BumpChannelThread(ctx, thread.ID)
	h.Queries.TouchChannel(ctx, thread.ChannelID)
	payload := map[string]any{
		"message":    channelMessageToResponse(msg),
		"channel_id": uuidToString(thread.ChannelID),
		"thread_id":  uuidToString(thread.ID),
	}
	for k, v := range extra {
		payload[k] = v
	}
	h.publish(protocol.EventChannelMessageCreated, uuidToString(thread.WorkspaceID), "system", "", payload)
}

// postChannelSystemMessage inserts a top-level system message into a channel's
// main timeline (thread_id IS NULL) and broadcasts it.
func (h *Handler) postChannelSystemMessage(ctx context.Context, channelID, workspaceID pgtype.UUID, content string, extra map[string]any) {
	msg, err := h.Queries.CreateChannelMessageTopLevel(ctx, db.CreateChannelMessageTopLevelParams{
		ChannelID:   channelID,
		WorkspaceID: workspaceID,
		AuthorType:  "system",
		Content:     content,
	})
	if err != nil {
		return
	}
	h.Queries.TouchChannel(ctx, channelID)
	payload := map[string]any{
		"message":    channelMessageToV2Response(msg, 0),
		"channel_id": uuidToString(channelID),
	}
	for k, v := range extra {
		payload[k] = v
	}
	h.publish(protocol.EventChannelMessageCreated, uuidToString(workspaceID), "system", "", payload)
}
