package inbox

// FIR-2385: the owner side of the private-agent run-request flow. The tagging
// member only creates the request (see internal/cerebro/privateagentrun); the
// agent owner accepts it here.
//
// FIR-2409: approving no longer starts the agent directly. Instead it posts a
// visible approval comment on the issue (authored by the owner, mentioning the
// agent) and triggers the run with THAT comment as the trigger — so the
// approval is recorded in the thread and the run goes through the same task
// engine + pending-dedup as an ordinary tag.

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RunPrivateAgentRequest enqueues the requested run for the agent owner. Only
// the recipient (the agent owner) may call it. The agent must still be owned
// by the caller and runnable; the enqueue reuses the same mention path the
// original tag would have taken, so the server-side foreign-trigger boundary
// is never crossed.
func (h *Handler) RunPrivateAgentRequest(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	itemUUID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid inbox item id")
		return
	}
	item, err := h.Upstream.GetInboxItemInWorkspace(r.Context(), db.GetInboxItemInWorkspaceParams{
		ID:          itemUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "inbox item not found")
		return
	}
	if item.RecipientType != "member" || util.UUIDToString(item.RecipientID) != userID {
		writeError(w, http.StatusForbidden, "not your inbox item")
		return
	}
	if item.Type != "private_agent_run_request" {
		writeError(w, http.StatusBadRequest, "not a run request")
		return
	}
	if h.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "run requests are not available")
		return
	}

	var details map[string]string
	if len(item.Details) > 0 {
		_ = json.Unmarshal(item.Details, &details)
	}
	agentID, err := util.ParseUUID(details["agent_id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "run request missing agent_id")
		return
	}
	commentID, err := util.ParseUUID(details["comment_id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "run request missing comment_id")
		return
	}
	if !item.IssueID.Valid {
		writeError(w, http.StatusBadRequest, "run request missing issue")
		return
	}

	issue, err := h.Upstream.GetIssue(r.Context(), item.IssueID)
	if err != nil || util.UUIDToString(issue.WorkspaceID) != wsID {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	agent, err := h.Upstream.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	// Defense in depth: only the agent owner can run it, mirroring the trigger
	// boundary. The request was addressed to the owner, but re-check the agent
	// in case ownership changed since the request was created.
	if !agent.OwnerID.Valid || util.UUIDToString(agent.OwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the agent owner can run this agent")
		return
	}
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "agent is not runnable")
		return
	}

	// FIR-2409: record the approval as a visible comment in the thread (a reply
	// to the originally tagged comment), authored by the approving owner and
	// mentioning the agent. This is what the run is triggered from, so the
	// approval lives in the issue history instead of starting the agent silently.
	ownerUUID, perr := util.ParseUUID(userID)
	if perr != nil {
		writeError(w, http.StatusInternalServerError, "invalid owner id")
		return
	}
	approval, err := h.Upstream.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: wsUUID,
		AuthorType:  "member",
		AuthorID:    ownerUUID,
		Content: fmt.Sprintf(
			"✅ Kør-anmodning godkendt — [@%s](mention://agent/%s) sættes i gang.",
			agent.Name, util.UUIDToString(agentID),
		),
		Type:     "comment",
		ParentID: commentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to post approval comment")
		return
	}

	// Trigger via the same task engine a tag uses, off the approval comment, and
	// honour the same pending-dedup so a double approval can't double-run.
	pending, perr := h.Upstream.HasPendingTaskForIssueAndAgent(r.Context(), db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: agentID,
	})
	if perr == nil && !pending {
		if _, err := h.Tasks.EnqueueTaskForMention(r.Context(), issue, agentID, approval.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start agent")
			return
		}
	}

	// The request is fulfilled — take it out of the active inbox.
	_, _ = h.Upstream.MarkInboxRead(r.Context(), item.ID)
	_, _ = h.Upstream.ArchiveInboxItem(r.Context(), item.ID)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     util.UUIDToString(item.ID),
		"status": "queued",
	})
}
