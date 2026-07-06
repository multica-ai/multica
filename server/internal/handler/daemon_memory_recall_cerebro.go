package handler

// CEREBRO-PATCH(daemon-memory-autorecall): FIR-1794 layer 3 — automatic memory
// recall for LOCAL daemon runtimes. The cognee-memory-service is only reachable
// from the server ('.internal' host), so the recall must happen server-side:
// the claim handler resolves the memory gates, recalls memories relevant to the
// task, and ships the rendered block in AgentTaskResponse.MemoryContext. The
// daemon inlines it into the start prompt (daemon/memory_recall_cerebro.go),
// mirroring how GraphifyNudge travels.
//
// Best-effort by design: any failure (no recaller wired, bad workspace id,
// memory flag off, service down, nothing recalled) leaves the claim untouched —
// auto-recall is context enrichment and must never block a claim.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/multica-ai/multica/server/internal/util"
)

// CerebroMemoryAutoRecaller is the seam through which the claim path asks the
// cerebro runtime package for an automatic memory-recall block. An interface —
// not a direct call — because the recall implementation lives in
// server/internal/cerebro/runtime, which already imports handler; importing it
// back would be a cycle. The router wires *cerebroruntime.CerebroMemoryAutoRecall
// in as the concrete value (same pattern as CerebroAPIConnectionBriefResolver).
type CerebroMemoryAutoRecaller interface {
	// AutoRecallBlock joins the non-empty query parts, resolves the memory
	// gates for (workspace, agent, originating user), and returns a
	// ready-to-inject markdown block — or "" when memory is off for the
	// workspace, nothing was recalled, or the service failed.
	AutoRecallBlock(ctx context.Context, workspaceID, agentID, originUser pgtype.UUID, queryParts ...string) string
}

// applyMemoryAutoRecall fills resp.MemoryContext with automatically recalled
// memories for this task. Must run late in the claim, after the issue / trigger
// comment / wakeup fields are populated, because those are the recall query.
func (h *Handler) applyMemoryAutoRecall(ctx context.Context, resp *AgentTaskResponse, task db.AgentTaskQueue) {
	if h.MemoryAutoRecall == nil || resp == nil {
		return
	}
	wsID, err := util.ParseUUID(resp.WorkspaceID)
	if err != nil || !wsID.Valid {
		return
	}
	resp.MemoryContext = h.MemoryAutoRecall.AutoRecallBlock(ctx, wsID, task.AgentID, task.OriginalUserID,
		resp.ThreadName, resp.WakeupPrompt, resp.TriggerCommentContent, resp.ChatMessage)
}
