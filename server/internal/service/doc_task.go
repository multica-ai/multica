package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// DocCommentContextType marks a run started from a doc discussion (MUL-9933).
const DocCommentContextType = "doc_comment"

// DocCommentContext is the task context of a doc discussion run. The doc and
// discussion live in their own columns; the workspace is kept here so the
// run's events and daemon routes still resolve once the doc is deleted.
type DocCommentContext struct {
	Type        string `json:"type"`
	WorkspaceID string `json:"workspace_id"`
}

// ParseDocCommentContext returns the doc discussion context of task, if it is
// a doc discussion run.
func ParseDocCommentContext(task db.AgentTaskQueue) (DocCommentContext, bool) {
	if !task.DocID.Valid || len(task.Context) == 0 {
		return DocCommentContext{}, false
	}
	var dc DocCommentContext
	if err := json.Unmarshal(task.Context, &dc); err != nil || dc.Type != DocCommentContextType {
		return DocCommentContext{}, false
	}
	return dc, true
}

// EnqueueDocCommentTask starts agent's run for a doc discussion, triggered by
// comment. When the agent already has a run for the discussion that has not
// started, the comment folds into it instead (merged is true): the run reads
// the whole discussion when it starts, so the newer comment becomes its
// trigger and the run now acts for that comment's human. The caller has
// checked that the comment's author may invoke agent.
func (s *TaskService) EnqueueDocCommentTask(ctx context.Context, agent db.Agent, doc db.Doc, comment db.DocComment) (task db.AgentTaskQueue, merged bool, err error) {
	if agent.ArchivedAt.Valid {
		return db.AgentTaskQueue{}, false, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return db.AgentTaskQueue{}, false, fmt.Errorf("agent has no runtime")
	}
	threadID := comment.ID
	if comment.ParentID.Valid {
		threadID = comment.ParentID
	}

	attr := s.attributionFromDocComment(ctx, comment)
	attr, err = s.applyAttributionFallback(ctx, attr, agent)
	if err != nil {
		return db.AgentTaskQueue{}, false, err
	}
	attrSource, attrDelegatedFrom, attrEvidenceKind, attrEvidenceRef := attributionCreateParams(attr)
	contextJSON, err := json.Marshal(DocCommentContext{Type: DocCommentContextType, WorkspaceID: util.UUIDToString(doc.WorkspaceID)})
	if err != nil {
		return db.AgentTaskQueue{}, false, fmt.Errorf("marshal doc comment context: %w", err)
	}
	summary := pgtype.Text{}
	if text := truncateForSummary(comment.Content, triggerSummaryMaxLen); text != "" {
		summary = pgtype.Text{String: text, Valid: true}
	}
	overlay := s.buildRuntimeMCPOverlay(ctx, attr.UserID, agent)

	err = s.runInTx(ctx, func(qtx *db.Queries) error {
		// Agent row first, as on every enqueue path; holding it also
		// serializes two comments racing to start the same agent's run.
		if _, err := qtx.GetAgentForClaimUpdate(ctx, agent.ID); err != nil {
			return fmt.Errorf("lock agent: %w", err)
		}
		queued, err := qtx.GetQueuedDocThreadTaskForUpdate(ctx, db.GetQueuedDocThreadTaskForUpdateParams{
			DocID: doc.ID, DocThreadID: threadID, AgentID: agent.ID,
		})
		if err == nil {
			task, err = qtx.MergeDocCommentIntoQueuedTask(ctx, db.MergeDocCommentIntoQueuedTaskParams{
				ID:                   queued.ID,
				DocCommentID:         comment.ID,
				TriggerSummary:       summary,
				OriginatorUserID:     attr.UserID,
				AccountableUserID:    attr.AccountableUserID,
				OriginatorSource:     attrSource,
				DelegatedFromTaskID:  attrDelegatedFrom,
				TriggerEvidenceKind:  attrEvidenceKind,
				TriggerEvidenceRefID: attrEvidenceRef,
				RuntimeMcpOverlay:    overlay.Overlay,
				RuntimeConnectedApps: overlay.ConnectedApps,
			})
			merged = err == nil
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("find queued doc task: %w", err)
		}
		task, err = qtx.CreateDocCommentTask(ctx, db.CreateDocCommentTaskParams{
			ID:                   dbid.NewV7(),
			AgentID:              agent.ID,
			Priority:             priorityToInt("high"),
			Context:              contextJSON,
			TriggerSummary:       summary,
			DocID:                doc.ID,
			DocThreadID:          threadID,
			DocCommentID:         comment.ID,
			OriginatorUserID:     attr.UserID,
			AccountableUserID:    attr.AccountableUserID,
			RuntimeMcpOverlay:    overlay.Overlay,
			RuntimeConnectedApps: overlay.ConnectedApps,
			OriginatorSource:     attrSource,
			DelegatedFromTaskID:  attrDelegatedFrom,
			TriggerEvidenceKind:  attrEvidenceKind,
			TriggerEvidenceRefID: attrEvidenceRef,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("agent or workspace is gone")
		}
		return err
	})
	if err != nil {
		return db.AgentTaskQueue{}, false, err
	}
	s.RecordHumanDelegation(ctx, attr)
	if merged {
		s.BroadcastTaskCoalesced(ctx, task)
		return task, true, nil
	}
	slog.Info("doc comment task enqueued",
		"task_id", util.UUIDToString(task.ID),
		"agent_id", util.UUIDToString(agent.ID),
		"doc_id", util.UUIDToString(doc.ID),
		"doc_thread_id", util.UUIDToString(threadID),
	)
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.NotifyTaskEnqueued(ctx, task)
	return task, false, nil
}

// attributionFromDocComment resolves who a doc discussion run acts for, the
// same way an issue comment does: a member's comment is that member's direct
// action; an agent's comment inherits the human behind the run that wrote it.
func (s *TaskService) attributionFromDocComment(ctx context.Context, comment db.DocComment) attribution.Result {
	facts := attribution.CommentFacts{
		CommentID:  comment.ID,
		AuthorType: comment.AuthorType,
		AuthorID:   comment.AuthorID,
		CreatedAt:  comment.CreatedAt,
	}
	if comment.AuthorType == "agent" && comment.SourceTaskID.Valid {
		facts.SourceTaskID = comment.SourceTaskID
		if parent, err := s.Queries.GetAgentTask(ctx, comment.SourceTaskID); err == nil {
			facts.ParentOriginator = parent.OriginatorUserID
			facts.ParentAccountable = parent.AccountableUserID
		}
	}
	attr := attribution.ClassifyComment(facts, attribution.SourceDelegation)
	attr.EvidenceKind = attribution.EvidenceDocComment
	return attr
}

// CancelTasksForDoc cancels every active run on a deleted doc, the doc
// counterpart of CancelTasksForIssue.
func (s *TaskService) CancelTasksForDoc(ctx context.Context, docID pgtype.UUID) error {
	return s.cancelDocTasks(ctx, func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
		return qtx.CancelAgentTasksByDoc(ctx, docID)
	})
}

// CancelTasksForDocThread cancels every active run on a deleted discussion.
func (s *TaskService) CancelTasksForDocThread(ctx context.Context, docID, threadID pgtype.UUID) error {
	return s.cancelDocTasks(ctx, func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
		return qtx.CancelAgentTasksByDocThread(ctx, db.CancelAgentTasksByDocThreadParams{DocID: docID, DocThreadID: threadID})
	})
}

func (s *TaskService) cancelDocTasks(ctx context.Context, cancel func(*db.Queries) ([]db.AgentTaskQueue, error)) error {
	var cancelled []db.AgentTaskQueue
	if err := s.runInTx(ctx, func(qtx *db.Queries) error {
		var err error
		cancelled, err = cancel(qtx)
		if err != nil {
			return err
		}
		return SettleTerminalTaskState(ctx, qtx, cancelled...)
	}); err != nil {
		return err
	}
	for _, t := range cancelled {
		s.captureTaskCancelled(ctx, t)
		s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, t)
	}
	for _, agentID := range distinctAgentIDs(cancelled) {
		s.ReconcileAgentStatus(ctx, agentID)
	}
	s.notifyTasksFinished(cancelled)
	return nil
}

// ensureDocReply posts a completed run's final output into its discussion
// when the agent ended without replying there, the doc counterpart of the
// issue fallback comment: a discussion never waits on a run that already
// finished. The reply notifies members like any other reply.
func (s *TaskService) ensureDocReply(ctx context.Context, task db.AgentTaskQueue, result []byte) {
	dc, ok := ParseDocCommentContext(task)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(dc.WorkspaceID)
	if err != nil {
		return
	}
	replied, err := s.Queries.HasDocTaskReply(ctx, db.HasDocTaskReplyParams{
		WorkspaceID: wsUUID, DocID: task.DocID, TaskID: task.ID, DocThreadID: task.DocThreadID,
	})
	if err != nil || replied {
		return
	}
	var payload protocol.TaskCompletedPayload
	if json.Unmarshal(result, &payload) != nil || payload.Output == "" {
		return
	}
	body := util.DecodeEscapedAgentOutput(payload.Output)
	if isTrivialDoneOutput(body) {
		return
	}
	// The doc or the discussion may have been deleted while the run worked.
	doc, err := s.Queries.GetDoc(ctx, db.GetDocParams{ID: task.DocID, WorkspaceID: wsUUID})
	if err != nil {
		return
	}
	root, err := s.Queries.GetDocComment(ctx, db.GetDocCommentParams{ID: task.DocThreadID, DocID: task.DocID, WorkspaceID: wsUUID})
	if err != nil {
		return
	}
	comment, err := s.Queries.CreateDocComment(ctx, db.CreateDocCommentParams{
		ID:           dbid.NewV7(),
		WorkspaceID:  wsUUID,
		DocID:        task.DocID,
		ParentID:     task.DocThreadID,
		AuthorType:   "agent",
		AuthorID:     task.AgentID,
		Content:      truncateFallbackCommentBody(redact.Text(body), maxSynthesizedFallbackCommentRunes),
		SourceTaskID: task.ID,
	})
	if err != nil {
		slog.Warn("doc comment task: fallback reply failed", "task_id", util.UUIDToString(task.ID), "error", err)
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventDocCommentChanged,
		WorkspaceID: dc.WorkspaceID,
		ActorType:   "agent",
		ActorID:     util.UUIDToString(task.AgentID),
		Payload: map[string]any{
			"doc_id": util.UUIDToString(task.DocID), "comment_id": util.UUIDToString(comment.ID),
		},
	})
	NotifyDocComment(ctx, s.Queries, s.Bus, doc, root, comment, "agent", util.UUIDToString(task.AgentID))
}
