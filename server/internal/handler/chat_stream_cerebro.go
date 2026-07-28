package handler

// CEREBRO-PATCH(chat-run-stream): FIR-2835 Phase 1 — net-new cerebro endpoint
// that streams a chat session's agent run as Server-Sent Events in the Vercel
// AI SDK UI-message-stream (v1) protocol, replacing the send→poll→translate
// loop every embedding app (Finance/AI CFO first) had to hand-roll. Lives in
// a cerebro-prefixed sibling file so the upstream chat handler stays
// untouched; the protocol/broker mechanics live in
// server/internal/cerebro/chatstream.

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/chatstream"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ChatRunStreamBroker is the seam the handler uses to receive chat-run
// events; satisfied by *chatstream.Broker, wired by the router.
type ChatRunStreamBroker interface {
	Subscribe(sessionID, taskID string) (<-chan chatstream.Event, func())
}

// chatStreamMaxWait caps how long a single stream request stays open waiting
// for a run to finish. Long chat runs are legitimate (minutes); the cap only
// protects the server from abandoned-but-connected clients. Clients may
// simply reconnect — the replay path picks up a finished run from the DB.
const chatStreamMaxWait = 15 * time.Minute

// chatStreamPingInterval keeps intermediaries from idling out the SSE socket.
const chatStreamPingInterval = 15 * time.Second

// StreamChatSessionRun handles GET /api/chat/sessions/{sessionId}/stream.
//
// Contract (consumed by the Finance AI CFO first):
//   - ?task_id=<uuid> streams that specific run; without it the session's
//     current pending task is used, and 204 No Content is returned when no
//     run is active (mirrors the AI SDK resume contract).
//   - 200 responses are `text/event-stream` carrying UI-message-stream v1
//     chunks: start → live tool input/result chunks → text-start →
//     text-delta → text-end → finish
//     (messageMetadata: taskId, messageId, elapsedMs, model, usage) →
//     `[DONE]`. Failures surface as an in-stream `error` chunk with the real
//     failure text — never a silently dropped socket.
//   - Runs that finished before the client connected are replayed from the
//     persisted assistant message, so the send → open-stream race is safe.
func (h *Handler) StreamChatSessionRun(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}
	if h.ChatStream == nil {
		writeError(w, http.StatusServiceUnavailable, "chat streaming is not enabled")
		return
	}
	resolvedSessionID := uuidToString(session.ID)

	// Resolve the run to follow.
	var task db.AgentTaskQueue
	if raw := r.URL.Query().Get("task_id"); raw != "" {
		taskUUID, ok := parseUUIDOrBadRequest(w, raw, "task_id")
		if !ok {
			return
		}
		t, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
		if err != nil || uuidToString(t.ChatSessionID) != resolvedSessionID {
			writeError(w, http.StatusNotFound, "task not found for chat session")
			return
		}
		task = t
	} else {
		pending, err := h.Queries.GetPendingChatTask(r.Context(), session.ID)
		if err != nil {
			// Nothing active — the AI SDK resume contract expects 204.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t, err := h.Queries.GetAgentTask(r.Context(), pending.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load pending task")
			return
		}
		task = t
	}

	// Subscribe before the terminal re-check so a run finishing between the
	// check and the subscription cannot slip through unobserved.
	taskID := uuidToString(task.ID)
	sub, cancelSub := h.ChatStream.Subscribe(resolvedSessionID, taskID)
	defer cancelSub()

	sw, err := chatstream.NewWriter(w)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Fresh read: the task may have gone terminal since it was resolved.
	if t, err := h.Queries.GetAgentTask(r.Context(), task.ID); err == nil {
		task = t
	}
	switch task.Status {
	case "completed":
		h.replayCompletedChatTask(r, sw, task)
		return
	case "failed", "cancelled":
		sw.WriteError(h.chatTaskFailureText(r.Context(), task))
		return
	}

	messageID := ""
	if msg, err := h.Queries.GetAssistantChatMessageByTaskID(r.Context(), task.ID); err == nil {
		messageID = uuidToString(msg.ID)
	}
	if sw.WriteStart(messageID) != nil {
		return
	}
	toolParts := chatstream.NewToolPartBuilder()
	lastSeq, err := h.writeChatTaskToolParts(r.Context(), sw, toolParts, task.ID, 0)
	if err != nil {
		sw.WriteError("failed to load task progress")
		return
	}
	ping := time.NewTicker(chatStreamPingInterval)
	defer ping.Stop()
	maxWait := time.NewTimer(chatStreamMaxWait)
	defer maxWait.Stop()

	for {
		select {
		case ev, open := <-sub:
			if !open {
				return
			}
			if ev.TaskID != taskID {
				continue
			}
			switch ev.Type {
			case chatstream.EventMessage:
				if ev.Message == nil || ev.Message.Seq <= lastSeq {
					continue
				}
				if sw.WriteParts(toolParts.Parts(*ev.Message)) != nil {
					return
				}
				lastSeq = ev.Message.Seq
				continue
			case chatstream.EventDone:
				if _, err := h.writeChatTaskToolParts(r.Context(), sw, toolParts, task.ID, lastSeq); err != nil {
					sw.WriteError("failed to load final task progress")
					return
				}
				parts := h.chatStreamArtifactParts(r, ev.Content, workspaceID)
				sw.WriteAssistantMessageTail(ev.Content, parts, h.chatStreamFinishMetadata(r.Context(), task.ID, ev.MessageID, ev.ElapsedMs))
			case chatstream.EventFailed, chatstream.EventCancelled:
				if t, err := h.Queries.GetAgentTask(r.Context(), task.ID); err == nil {
					task = t
				}
				sw.WriteError(h.chatTaskFailureText(r.Context(), task))
			default:
				continue
			}
			return
		case <-ping.C:
			if sw.WritePing() != nil {
				return
			}
		case <-maxWait.C:
			sw.WriteError("stream timeout: the run is still active — reconnect to resume")
			return
		case <-r.Context().Done():
			return
		}
	}
}

// replayCompletedChatTask re-emits a finished run from the persisted
// assistant message, covering clients that connect after completion.
func (h *Handler) replayCompletedChatTask(r *http.Request, sw *chatstream.Writer, task db.AgentTaskQueue) {
	ctx := r.Context()
	msg, err := h.Queries.GetAssistantChatMessageByTaskID(ctx, task.ID)
	if err != nil {
		sw.WriteError("assistant reply not found for completed task")
		return
	}
	if msg.FailureReason.Valid && msg.FailureReason.String != "" {
		sw.WriteError(msg.FailureReason.String + ": " + msg.Content)
		return
	}
	var elapsedMs int64
	if msg.ElapsedMs.Valid {
		elapsedMs = msg.ElapsedMs.Int64
	}
	messageID := uuidToString(msg.ID)
	if sw.WriteStart(messageID) != nil {
		return
	}
	toolParts := chatstream.NewToolPartBuilder()
	if _, err := h.writeChatTaskToolParts(ctx, sw, toolParts, task.ID, 0); err != nil {
		sw.WriteError("failed to load task progress")
		return
	}
	parts := h.chatStreamArtifactParts(r, msg.Content, ctxWorkspaceID(ctx))
	sw.WriteAssistantMessageTail(msg.Content, parts, h.chatStreamFinishMetadata(ctx, task.ID, messageID, elapsedMs))
}

// writeChatTaskToolParts catches a stream up from the persisted, redacted task
// transcript. Reading before live events and once more at completion closes
// both the subscribe race and the broker's deliberate slow-client drop path.
func (h *Handler) writeChatTaskToolParts(
	ctx context.Context,
	sw *chatstream.Writer,
	builder *chatstream.ToolPartBuilder,
	taskID pgtype.UUID,
	afterSeq int,
) (int, error) {
	messages, err := h.Queries.ListTaskMessagesSince(ctx, db.ListTaskMessagesSinceParams{
		TaskID: taskID,
		Seq:    int32(afterSeq),
	})
	if err != nil {
		return afterSeq, err
	}
	lastSeq := afterSeq
	for _, message := range messages {
		payload := taskMessageToPayload(message, uuidToString(taskID), "")
		if err := sw.WriteParts(builder.Parts(payload)); err != nil {
			return lastSeq, err
		}
		lastSeq = int(message.Seq)
	}
	return lastSeq, nil
}

// chatTaskFailureText resolves the most useful failure text for a terminal
// task: the persisted assistant failure message (the daemon's real error,
// already redacted) when present, otherwise the task row's error/reason.
// Surfacing the real error is the point — the blanket "could not complete"
// strings are exactly what FIR-2835 Phase 0 removed on the Finance side.
func (h *Handler) chatTaskFailureText(ctx context.Context, task db.AgentTaskQueue) string {
	reason := task.Status
	if task.FailureReason.Valid && task.FailureReason.String != "" {
		reason = task.FailureReason.String
	}
	if msg, err := h.Queries.GetAssistantChatMessageByTaskID(ctx, task.ID); err == nil && msg.Content != "" {
		return reason + ": " + msg.Content
	}
	if task.Error.Valid && task.Error.String != "" {
		return reason + ": " + task.Error.String
	}
	return reason
}

// chatStreamFinishMetadata assembles the finish chunk's messageMetadata:
// run identity plus token/cost spend from task_usage (best effort — a run
// whose usage rows have not landed yet just omits the usage block).
func (h *Handler) chatStreamFinishMetadata(ctx context.Context, taskID pgtype.UUID, messageID string, elapsedMs int64) chatstream.FinishMetadata {
	meta := chatstream.FinishMetadata{
		TaskID:    uuidToString(taskID),
		MessageID: messageID,
		ElapsedMs: elapsedMs,
	}
	rows, err := h.Queries.GetTaskUsage(ctx, taskID)
	if err != nil || len(rows) == 0 {
		return meta
	}
	usage := &chatstream.FinishUsage{}
	model := rows[0].Model
	for _, row := range rows {
		if row.Model != model {
			model = "" // mixed-model run — leave the label blank rather than mislead
		}
		usage.InputTokens += row.InputTokens
		usage.OutputTokens += row.OutputTokens
		usage.CacheReadTokens += row.CacheReadTokens
		usage.CacheWriteTokens += row.CacheWriteTokens
		usage.CostCents += preferGatewayCost(row.CostCents, row.Model, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheWriteTokens)
	}
	meta.Model = model
	meta.Usage = usage
	return meta
}
