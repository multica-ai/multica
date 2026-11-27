package handler

import (
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) applyChatForkClaim(r *http.Request, task *db.AgentTaskQueue, runtime db.AgentRuntime, chat db.ChatSession, resp *AgentTaskResponse) (bool, *claimBuildFailure) {
	if !chat.ForkedFromChatID.Valid {
		return false, nil
	}
	if !requestHasClientCapability(r, protocol.DaemonCapabilityCodexChatForkV1) ||
		runtime.Provider != "codex" || runtime.RuntimeMode != "local" || chat.RuntimeID != task.RuntimeID || chat.AgentID != task.AgentID {
		return false, &claimBuildFailure{
			outcome: "chat_fork_runtime_incompatible", status: http.StatusConflict,
			message: "native chat fork requires its original compatible Codex runtime",
		}
	}
	if chat.ForkInitializationTaskID.Valid {
		source, err := h.Queries.GetPendingChatForkSource(r.Context(), db.GetPendingChatForkSourceParams{
			DestinationChatID: chat.ID, TaskID: task.ID,
		})
		if err != nil {
			return false, h.rejectClaimSourceLoad(r.Context(), task, err, "chat fork source", uuidToString(chat.ForkedFromChatID))
		}
		resp.CodexChatFork = &protocol.CodexChatForkTask{
			SourceChatID: uuidToString(source.ID), SourceSessionID: source.SessionID.String,
			SourceWorkDir: source.WorkDir.String, SourceRequiresNativeResume: source.ForkedFromChatID.Valid,
		}
		return true, nil
	}
	if task.ForceFreshSession || task.ChannelContextRevision.Valid || !chat.SessionID.Valid || chat.SessionID.String == "" {
		return false, &claimBuildFailure{
			outcome: "chat_fork_history_unavailable", status: http.StatusConflict,
			message: "required native chat history is unavailable",
		}
	}
	resp.RequireNativeResume = true
	resp.PriorSessionID = chat.SessionID.String
	return false, nil
}
