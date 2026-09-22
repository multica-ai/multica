package handler

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resolveTaskTokenKnowledgeSubject resolves the human principal for a
// task-token knowledge read. The user_id carried by mat_ is the runtime owner,
// not necessarily the human who started the task, so it is never used as the
// knowledge subject by itself. The returned privateAccess bit is true only
// for the narrower personal-chat proof; shared bases still require the
// resolved subject to be a current workspace member.
func (h *Handler) resolveTaskTokenKnowledgeSubject(r *http.Request, workspaceID, tokenUserID string) (subjectUserID string, privateAccess, ok bool) {
	if h.Queries == nil {
		return "", false, false
	}
	taskID, err := util.ParseUUID(r.Header.Get("X-Task-ID"))
	if err != nil {
		return "", false, false
	}
	workspace, err := util.ParseUUID(workspaceID)
	if err != nil {
		return "", false, false
	}
	if _, err := util.ParseUUID(tokenUserID); err != nil {
		return "", false, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil || isTerminalTaskStatus(task.Status) || !task.AgentID.Valid || !task.ChatSessionID.Valid {
		return "", false, false
	}
	// An issue-backed, autopilot, or otherwise non-personal run must not be
	// used as a bridge into a private library, even if it happens to carry a
	// chat_session_id from an older or malformed row.
	if task.IssueID.Valid || task.AutopilotRunID.Valid {
		return "", false, false
	}
	if headerAgentID, parseErr := util.ParseUUID(r.Header.Get("X-Agent-ID")); parseErr != nil || headerAgentID != task.AgentID {
		return "", false, false
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil || util.UUIDToString(agent.WorkspaceID) != workspaceID {
		return "", false, false
	}
	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{ID: task.ChatSessionID, WorkspaceID: workspace})
	if err != nil || session.Status != "active" || session.AgentID != task.AgentID {
		return "", false, false
	}
	if _, err := h.Queries.GetChannelChatSessionBindingBySessionAny(r.Context(), task.ChatSessionID); err == nil {
		return "", false, false
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, false
	}

	// Prefer the top-of-chain human, with the task initiator as the documented
	// fallback for older direct-chat rows. If both are present they must agree;
	// a divergent pair is not a trustworthy proof of a personal chat.
	subjectID := ""
	if task.OriginatorUserID.Valid {
		subjectID = util.UUIDToString(task.OriginatorUserID)
	}
	if task.InitiatorUserID.Valid {
		initiatorID := util.UUIDToString(task.InitiatorUserID)
		if subjectID != "" && subjectID != initiatorID {
			return "", false, false
		}
		if subjectID == "" {
			subjectID = initiatorID
		}
	}
	subjectUUID, err := util.ParseUUID(subjectID)
	if err != nil || subjectUUID != session.CreatorID {
		return "", false, false
	}
	if _, err := h.getWorkspaceMember(r.Context(), subjectID, workspaceID); err != nil {
		return "", false, false
	}
	if !h.canInvokeAgent(r.Context(), agent, "agent", util.UUIDToString(agent.ID), subjectID, workspaceID) {
		return "", false, false
	}
	return subjectID, true, true
}
