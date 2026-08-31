package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) RunIssueCommentFollowUp(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	commentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commentId"), "comment id")
	if !ok {
		return
	}
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID: commentID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || uuidToString(comment.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if comment.AuthorType != "agent" || !comment.SourceTaskID.Valid {
		writeError(w, http.StatusConflict, "follow-up source is no longer actionable")
		return
	}

	var actions []protocol.IssueCommentFollowUp
	if err := json.Unmarshal(comment.SuggestedFollowUps, &actions); err != nil {
		writeError(w, http.StatusConflict, "follow-up source is unavailable")
		return
	}
	actionID := chi.URLParam(r, "actionId")
	var selected *protocol.IssueCommentFollowUp
	for i := range actions {
		if actions[i].ID == actionID {
			selected = &actions[i]
			break
		}
	}
	if selected == nil {
		writeError(w, http.StatusNotFound, "follow-up action not found")
		return
	}
	if quickActionSideEffectMentionRe.MatchString(strings.ToLower(selected.Prompt)) {
		writeError(w, http.StatusConflict, "follow-up action is unsafe")
		return
	}

	task, err := h.Queries.GetAgentTask(r.Context(), comment.SourceTaskID)
	if err != nil || uuidToString(task.IssueID) != uuidToString(issue.ID) ||
		uuidToString(task.AgentID) != uuidToString(comment.AuthorID) {
		writeError(w, http.StatusConflict, "responsible agent is unavailable")
		return
	}
	assigneeType := "agent"
	assigneeID := task.AgentID
	if task.SquadID.Valid {
		assigneeType = "squad"
		assigneeID = task.SquadID
	}
	target := h.resolveQuickActionTarget(r.Context(), db.QuickAction{
		WorkspaceID: issue.WorkspaceID, AssigneeType: assigneeType, AssigneeID: assigneeID,
	})
	if !target.Found {
		h.writeDispatchBlocked(w, http.StatusConflict, ReasonTargetUnavailable)
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	originatorUserID := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if !h.canInvokeAgent(r.Context(), target.Agent, actorType, actorID, originatorUserID, workspaceID) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}

	// Validate and create under a per-anchor transaction lock. This makes a
	// suggestion one-shot even when two tabs (or two API replicas) submit it at
	// the same time: the loser waits, then sees the winner's newer thread reply.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to run follow-up")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockCommentFollowUpExecution(r.Context(), comment.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to run follow-up")
		return
	}
	lockedComment, err := qtx.GetCommentInWorkspaceForUpdate(r.Context(), db.GetCommentInWorkspaceForUpdateParams{
		ID: comment.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || uuidToString(lockedComment.IssueID) != uuidToString(issue.ID) ||
		lockedComment.AuthorType != "agent" || !lockedComment.SourceTaskID.Valid ||
		uuidToString(lockedComment.SourceTaskID) != uuidToString(comment.SourceTaskID) {
		writeError(w, http.StatusConflict, "follow-up source is no longer actionable")
		return
	}
	if err := json.Unmarshal(lockedComment.SuggestedFollowUps, &actions); err != nil {
		writeError(w, http.StatusConflict, "follow-up source is unavailable")
		return
	}
	selected = nil
	for i := range actions {
		if actions[i].ID == actionID {
			selected = &actions[i]
			break
		}
	}
	if selected == nil || quickActionSideEffectMentionRe.MatchString(strings.ToLower(selected.Prompt)) {
		writeError(w, http.StatusConflict, "follow-up action is no longer available")
		return
	}
	latest, err := qtx.GetLatestCommentInThread(r.Context(), db.GetLatestCommentInThreadParams{
		AnchorID: comment.ID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil || uuidToString(latest.ID) != uuidToString(comment.ID) {
		writeError(w, http.StatusConflict, "a newer reply already exists in this thread")
		return
	}

	body := sanitizeNullBytes(buildSuggestedFollowUpBody(selected.Prompt, target))
	created, err := qtx.CreateComment(r.Context(), db.CreateCommentParams{
		ID: dbid.NewV7(), IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		AuthorType: actorType, AuthorID: parseUUID(actorID), Content: body,
		Type: "comment", ParentID: comment.ID,
	})
	if err != nil {
		slog.Warn("issue comment follow-up create failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to run follow-up")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("issue comment follow-up commit failed", append(logger.RequestAttrs(r), "error", err, "comment_id", uuidToString(comment.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to run follow-up")
		return
	}
	createdComment := created.Comment()
	resp := commentToResponse(createdComment, nil, nil)
	resp.IssueRevision = created.IssueRevision
	h.publish(protocol.EventCommentCreated, workspaceID, actorType, actorID, map[string]any{
		"comment": resp, "issue_title": issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status, "issue_revision": created.IssueRevision,
	})
	delegationAuthority := h.autopilotDelegationAuthorityFromRequest(r, issue, actorType, actorID)
	resp.TriggerOutcomes = h.triggerTasksForComment(r.Context(), issue, createdComment, &lockedComment,
		actorType, actorID, originatorUserID, delegationAuthority, nil)
	if h.TaskService != nil {
		root, rootErr := h.Queries.GetThreadRoot(r.Context(), db.GetThreadRootParams{
			CommentID: comment.ID, WorkspaceID: issue.WorkspaceID,
		})
		if rootErr == nil {
			h.TaskService.AutoUnresolveThreadOnReply(r.Context(), &root, workspaceID, actorType, actorID)
		}
	}
	writeJSON(w, http.StatusCreated, resp)
}

func buildSuggestedFollowUpBody(prompt string, target quickActionTarget) string {
	return "[@" + target.Name + "](mention://" + target.MentionType + "/" + target.MentionID + ")\n\n" + prompt
}
