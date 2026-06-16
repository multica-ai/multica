package inbox

// FIR-2385: the owner side of the private-agent run-request flow. The tagging
// member only creates the request (see internal/cerebro/privateagentrun); the
// agent owner accepts it here.
//
// FIR-2409: approving posts a visible approval comment on the issue (authored
// by the owner, mentioning the agent) so the approval is recorded in the thread.
//
// TECH-3533: the run is NO LONGER triggered off that approval comment. Doing so
// handed the agent the owner's content-free acknowledgment as its triggering
// comment, so the agent saw no task and no-op'd (the bug Jesper hit on
// TECH-3472). Instead the run is started via the System Activity task path with
// an explicit prompt that embeds the ORIGINAL request, anchored to the original
// thread. Same pending-dedup as before so a double approval can't double-run.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/sysactivity"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
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

	// Record the approval as a visible comment in the thread (a reply to the
	// originally tagged comment), authored by the approving owner and mentioning
	// the agent, so the approval lives in the issue history.
	ownerUUID, perr := util.ParseUUID(userID)
	if perr != nil {
		writeError(w, http.StatusInternalServerError, "invalid owner id")
		return
	}

	// Load the original request comment so its body can seed the agent's
	// instruction and anchor the reply in the conversation that asked for it.
	original, oerr := h.Upstream.GetComment(r.Context(), commentID)
	ownerName := h.memberDisplayName(r.Context(), ownerUUID)
	requesterName := h.memberDisplayName(r.Context(), item.ActorID)

	if _, err := h.Upstream.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: wsUUID,
		AuthorType:  "member",
		AuthorID:    ownerUUID,
		Content: fmt.Sprintf(
			"✅ Run request approved — [@%s](mention://agent/%s) is starting now.",
			agent.Name, util.UUIDToString(agentID),
		),
		Type:     "comment",
		ParentID: commentID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to post approval comment")
		return
	}

	// Reply anchor = the original request's thread root so the woken agent's
	// output lands back in the conversation that asked for it.
	replyAnchor := commentID
	if oerr == nil && original.ParentID.Valid {
		replyAnchor = original.ParentID
	}

	// Trigger via the System Activity task path (the same engine wakeups use),
	// honouring the pending-dedup so a double approval can't double-run. The
	// human principal is the approving owner, so the woken run carries
	// provenance and may fan out to other agents.
	pending, perr := h.Upstream.HasPendingTaskForIssueAndAgent(r.Context(), db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: agentID,
	})
	if perr == nil && !pending {
		delegation := service.TaskDelegationContext{
			OriginalUserID: ownerUUID,
			Source:         pgtype.Text{String: "approval", Valid: true},
		}
		prompt := buildApprovalPrompt(ownerName, requesterName, original, oerr)
		if _, err := h.Tasks.EnqueueWakeupTask(r.Context(), issue, agentID, replyAnchor, "", triggerTypeApproval, prompt, delegation); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start agent")
			return
		}
		// Record the approval as a visible System Activity timeline event — this
		// is the "approve → system activity that triggers the agent" half of the
		// model. Best-effort; never blocks the run that just started.
		sysactivity.Record(r.Context(), h.Upstream, h.Bus, wsUUID, issue.ID, ownerUUID, "member", sysactivity.ActionAgentRunApproved, map[string]any{
			"agent_id":     util.UUIDToString(agentID),
			"agent_name":   agent.Name,
			"requester_id": util.UUIDToString(item.ActorID),
			"comment_id":   util.UUIDToString(commentID),
		})
	}

	// The request is fulfilled — take it out of the active inbox.
	_, _ = h.Upstream.MarkInboxRead(r.Context(), item.ID)
	_, _ = h.Upstream.ArchiveInboxItem(r.Context(), item.ID)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     util.UUIDToString(item.ID),
		"status": "queued",
	})
}

// triggerTypeApproval is the System Activity sub-type for an owner-approved
// cross-actor run-request. It flows through EnqueueWakeupTask onto the task's
// wakeup context (WakeupTriggerType) and lets the daemon prompt builder frame
// the run as an approval rather than a scheduled wakeup (TECH-3533).
const triggerTypeApproval = "approval"

// memberDisplayName resolves a member's name for the approval prompt, falling
// back to a neutral label so the instruction always reads naturally.
func (h *Handler) memberDisplayName(ctx context.Context, id pgtype.UUID) string {
	if id.Valid {
		if u, err := h.Upstream.GetUser(ctx, id); err == nil && strings.TrimSpace(u.Name) != "" {
			return u.Name
		}
	}
	return "a teammate"
}

// buildApprovalPrompt frames the woken run as an approved cross-actor request:
// the owner approved <requester>'s ask, with the original request quoted so the
// agent acts on the real instruction instead of an empty trigger (TECH-3533).
func buildApprovalPrompt(ownerName, requesterName string, original db.Comment, oerr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s approved %s's request to run you on this issue. Carry out that request now.", ownerName, requesterName)
	if oerr == nil {
		if c := strings.TrimSpace(original.Content); c != "" {
			fmt.Fprintf(&b, "\n\nThe original request was:\n> %s", c)
		}
	}
	b.WriteString("\n\nRead the issue context and the thread to understand exactly what is needed, then post your result in the original thread.")
	return b.String()
}
