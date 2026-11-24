package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Agents in doc discussions (MUL-9933). The routing follows issue comments:
//
//   - a comment that @mentions agents starts a run for each of them;
//   - a member's reply with no mention goes to the agent whose comment it
//     answers, else to the agent that last took part in the discussion; a
//     reply that names a member, @all or a squad goes to them instead;
//   - an agent's comment only starts runs for the agents it @mentions, never
//     for itself;
//   - who may start a run for an agent is canInvokeAgent, as on issues.
//
// Squads are not supported in doc discussions.

// docCommentAgentIDs returns the agents a new doc comment hands work to.
// replyTo is the comment the new one answers, before replies are folded onto
// the discussion's first comment; nil for a new discussion.
func (h *Handler) docCommentAgentIDs(ctx context.Context, doc db.Doc, root db.DocComment, replyTo *db.DocComment, comment db.DocComment, actorType, actorID string) []string {
	mentions := util.ParseMentions(comment.Content)
	var mentioned []string
	for _, m := range mentions {
		if m.Type != "agent" || (actorType == "agent" && m.ID == actorID) {
			continue
		}
		mentioned = append(mentioned, m.ID)
	}
	if len(mentioned) > 0 {
		return mentioned
	}
	// A comment that names anyone — a member, @all, or a squad, which doc
	// discussions do not run — is addressed to them, not to the discussion's
	// agent.
	if actorType != "member" || !comment.ParentID.Valid || hasMemberMention(mentions) ||
		hasAgentOrSquadMention(mentions) || util.HasMentionAll(mentions) {
		return nil
	}
	// Answering an agent's comment goes to that agent, as replying to an
	// agent's issue comment does, even when another agent spoke since.
	if replyTo != nil && replyTo.AuthorType == "agent" {
		return []string{uuidToString(replyTo.AuthorID)}
	}
	agentID, err := h.Queries.GetDocThreadContinuationAgent(ctx, db.GetDocThreadContinuationAgentParams{
		DocID: doc.ID, DocThreadID: root.ID, WorkspaceID: doc.WorkspaceID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("doc comment: find discussion agent failed", "doc_id", uuidToString(doc.ID), "thread_id", uuidToString(root.ID), "error", err)
		}
		return nil
	}
	return []string{uuidToString(agentID)}
}

// triggerDocCommentAgents starts (or folds the comment into) a run for each
// agent the comment hands work to. A target that cannot run — unknown, not
// invocable by this author, archived, or without a runtime — is skipped.
func (h *Handler) triggerDocCommentAgents(r *http.Request, doc db.Doc, root db.DocComment, replyTo *db.DocComment, comment db.DocComment, actorType, actorID string) {
	ctx := r.Context()
	agentIDs := h.docCommentAgentIDs(ctx, doc, root, replyTo, comment, actorType, actorID)
	if len(agentIDs) == 0 {
		return
	}
	workspaceID := uuidToString(doc.WorkspaceID)
	originator := h.invokeOriginatorFromRequest(r, actorType, actorID)
	seen := map[string]bool{}
	for _, id := range agentIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		agentUUID, err := util.ParseUUID(id)
		if err != nil {
			continue
		}
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentUUID, WorkspaceID: doc.WorkspaceID})
		if err != nil {
			continue
		}
		if !h.canInvokeAgent(ctx, agent, actorType, actorID, originator, workspaceID) || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
			continue
		}
		if _, _, err := h.TaskService.EnqueueDocCommentTask(ctx, agent, doc, comment); err != nil {
			slog.Warn("doc comment: start agent run failed",
				"doc_id", uuidToString(doc.ID), "comment_id", uuidToString(comment.ID), "agent_id", id, "error", err)
		}
	}
}

// DocTaskRunResponse is what a discussion shows of an agent's run on it: its
// state, and whether the viewer may stop it. A run's error is shown only to
// those who can see the agent, as its other run surfaces do.
type DocTaskRunResponse struct {
	ID            string                 `json:"id"`
	AgentID       string                 `json:"agent_id"`
	DocThreadID   string                 `json:"doc_thread_id"`
	Status        string                 `json:"status"`
	FailureReason string                 `json:"failure_reason,omitempty"`
	Error         *string                `json:"error"`
	CancelledBy   *TaskCancellationActor `json:"cancelled_by,omitempty"`
	CreatedAt     string                 `json:"created_at"`
	CompletedAt   *string                `json:"completed_at"`
	CanCancel     bool                   `json:"can_cancel"`
}

// ListDocTaskRuns lists a doc's recent agent runs, newest first, so its
// discussions can show what each agent is doing.
func (h *Handler) ListDocTaskRuns(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	rows, err := h.Queries.ListDocTasks(r.Context(), doc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	visible := map[string]bool{}
	canSee := func(agentID string) bool {
		if v, ok := visible[agentID]; ok {
			return v
		}
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: parseUUID(agentID), WorkspaceID: wsUUID})
		visible[agentID] = err == nil && h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID)
		return visible[agentID]
	}
	resp := make([]DocTaskRunResponse, len(rows))
	for i, t := range rows {
		run := DocTaskRunResponse{
			ID:          uuidToString(t.ID),
			AgentID:     uuidToString(t.AgentID),
			DocThreadID: uuidToString(t.DocThreadID),
			Status:      t.Status,
			CancelledBy: taskCancellationActorToResponse(t),
			CreatedAt:   timestampToString(t.CreatedAt),
			CompletedAt: timestampToPtr(t.CompletedAt),
		}
		if t.FailureReason.Valid {
			run.FailureReason = t.FailureReason.String
		}
		if canSee(run.AgentID) {
			run.Error = textToPtr(t.Error)
			switch t.Status {
			case "queued", "dispatched", "running", "waiting_local_directory", "deferred":
				run.CanCancel = true
			}
		}
		resp[i] = run
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": resp})
}

// applyDocCommentClaim fills a doc discussion run's claim: the doc, the
// discussion and its trigger comment, plus the session and workdir this agent
// last used on the doc. A daemon that cannot run doc discussions gets the task
// failed with an update hint, which the discussion shows as the run's outcome.
func (h *Handler) applyDocCommentClaim(r *http.Request, task *db.AgentTaskQueue, resp *AgentTaskResponse, runtimeID, runtimeWorkspaceID string) *claimBuildFailure {
	ctx := r.Context()
	if !requestHasClientCapability(r, protocol.DaemonCapabilityDocCommentV1) {
		return h.failClaimedTaskBeforeLaunch(ctx, task,
			"This agent's runtime is too old to answer doc comments. Update the Multica CLI on that machine, then ask again.",
			taskfailure.ReasonAgentRuntimeVersionUnsupported,
			"error_doc_comment_unsupported", http.StatusConflict, "daemon does not support doc discussion tasks")
	}
	dc, ok := service.ParseDocCommentContext(*task)
	if !ok {
		return h.failClaimedTaskBeforeLaunch(ctx, task,
			"Task identity is invalid: the doc discussion context is missing.",
			taskfailure.ReasonInvalidTaskIdentity,
			"error_invalid_task_identity", http.StatusConflict, "doc discussion context is missing")
	}
	resp.WorkspaceID = dc.WorkspaceID
	if failure := h.rejectClaimOnWorkspaceMismatch(ctx, task, resp.WorkspaceID, runtimeID, runtimeWorkspaceID, false); failure != nil {
		return failure
	}
	wsUUID := parseUUID(dc.WorkspaceID)
	doc, err := h.Queries.GetDoc(ctx, db.GetDocParams{ID: task.DocID, WorkspaceID: wsUUID})
	if err != nil {
		return h.rejectClaimSourceLoad(ctx, task, err, "doc", uuidToString(task.DocID))
	}
	root, err := h.Queries.GetDocComment(ctx, db.GetDocCommentParams{ID: task.DocThreadID, DocID: doc.ID, WorkspaceID: wsUUID})
	if err != nil {
		return h.rejectClaimSourceLoad(ctx, task, err, "doc discussion", uuidToString(task.DocThreadID))
	}
	resp.DocTitle = doc.Title
	resp.ThreadName = doc.Title
	resp.DocThreadQuote = root.AnchorQuote
	// A trigger reply deleted before the claim leaves the discussion to read.
	if trigger, err := h.Queries.GetDocComment(ctx, db.GetDocCommentParams{ID: task.DocCommentID, DocID: doc.ID, WorkspaceID: wsUUID}); err == nil {
		resp.DocCommentContent = trigger.Content
		resp.DocCommentAuthorName = h.actorDisplayName(ctx, wsUUID, trigger.AuthorType, trigger.AuthorID)
	}

	switch {
	case !task.ForceFreshSession:
		prior, err := h.Queries.GetLastDocTaskSession(ctx, db.GetLastDocTaskSessionParams{AgentID: task.AgentID, DocID: task.DocID})
		if err == nil && prior.SessionID.Valid {
			if prior.RuntimeID == task.RuntimeID {
				resp.PriorSessionID = prior.SessionID.String
			}
			if prior.WorkDir.Valid {
				resp.PriorWorkDir = prior.WorkDir.String
			}
		}
	case task.RetryOfTaskID.Valid:
		applyFreshSessionRetryWorkdir(*task, resp, requestHasClientCapability(r, protocol.DaemonCapabilityCheckoutKeepsWorkV1))
	}
	return nil
}
