package handler

import (
	"context"
	"fmt"

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
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	ident := fmt.Sprintf("%s-%d", prefix, issue.Number)
	content := fmt.Sprintf("从本线程创建了 Issue %s：%s", ident, issue.Title)
	h.postThreadSystemMessage(ctx, thread, content, map[string]any{
		"kind":     "issue_created",
		"issue_id": uuidToString(issue.ID),
	})
}

// reflowIssueStatus posts a system activity message into the issue's source
// thread when the issue's status changes. No-op when the issue did not come
// from a thread.
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
	h.postThreadSystemMessage(ctx, thread, content, map[string]any{
		"kind":     "issue_status",
		"issue_id": uuidToString(issue.ID),
		"status":   issue.Status,
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
