package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	agentver "github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// maxPreviewTriggerIssues caps a single preview request so a pathological
// selection cannot fan out into thousands of readiness probes.
const maxPreviewTriggerIssues = 500

// issueTriggerWriteProbe builds the probe the write paths feed to
// WillEnqueueRun. The private-agent gate is already enforced at the HTTP
// boundary (validateAssigneePair on assign) and inside enqueueSquadLeaderTask
// (canEnqueueSquadLeader), so a write must NOT re-run or sink it — it passes
// allow-all. The self-loop check needs the request's X-Task-ID header.
func (h *Handler) issueTriggerWriteProbe(r *http.Request, actorType string, issue db.Issue) service.IssueTriggerProbe {
	return service.IssueTriggerProbe{
		CanAccessAgent: nil, // allow-all; gate lives at the write boundary
		IsSelfLoop: func() bool {
			return h.isAgentRunningOnIssue(r, actorType, issue)
		},
	}
}

// issueTriggerPreviewProbe mirrors the real write-time gates for the read-only
// preview: the private-agent gate (so preview never leaks a private agent's
// readiness to a member who cannot see it — matching validateAssigneePair /
// canEnqueueSquadLeader) and the same self-loop guard.
func (h *Handler) issueTriggerPreviewProbe(r *http.Request, actorType, actorID, workspaceID string, issue db.Issue) service.IssueTriggerProbe {
	originatorUserID := h.invokeOriginatorFromRequest(r, actorType, actorID)
	return service.IssueTriggerProbe{
		CanAccessAgent: func(agent db.Agent) bool {
			return h.canInvokeAgent(r.Context(), agent, actorType, actorID, originatorUserID, workspaceID)
		},
		IsSelfLoop: func() bool {
			return h.isAgentRunningOnIssue(r, actorType, issue)
		},
	}
}

// dispatchIssueRun executes the enqueue side effect for a decision produced by
// WillEnqueueRun, carrying an optional handoff note into the run's opening
// context. The squad path still flows through enqueueSquadLeaderTask so the
// leader access gate and pending dedup stay in one place.
func (h *Handler) dispatchIssueRun(ctx context.Context, issue db.Issue, trigger service.IssueRunTrigger, actorType, actorID, handoffNote string) {
	switch trigger.AssigneeType {
	case "agent":
		_, _ = h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, issue, handoffNote)
	case "squad":
		h.enqueueSquadLeaderTask(ctx, issue, pgtype.UUID{}, actorType, actorID, handoffNote)
	}
}

// IssueTriggerPreviewRequest asks "if I apply this assignee and/or status to
// these issues (or create one), which runs will start". All fields are
// optional; a nil prospective field means "leave unchanged".
type IssueTriggerPreviewRequest struct {
	// IssueIDs are existing issues to evaluate (single assign, single status,
	// or a batch). Empty with IsCreate=true evaluates a candidate new issue.
	IssueIDs []string `json:"issue_ids"`
	// IsCreate previews a not-yet-persisted issue from AssigneeType/ID/Status.
	IsCreate     bool    `json:"is_create"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	Status       *string `json:"status"`
}

// IssueTriggerPreviewItem is one issue that WILL start a run under the
// prospective write. AgentID is the runnable agent (squad leader for squads).
// HandoffSupported is the soft-gate signal: false when the target runtime's
// daemon is too old to render a handoff note, so the UI can gray out the note
// box rather than silently drop the text. The assignment itself still works.
type IssueTriggerPreviewItem struct {
	IssueID          string `json:"issue_id"`
	AgentID          string `json:"agent_id"`
	Source           string `json:"source"`
	HandoffSupported bool   `json:"handoff_supported"`
}

// IssueTriggerPreviewResponse lists every issue that will enqueue plus a total
// the UI can show directly ("将启动 N 个"). Issues that will NOT start a run are
// simply absent, so total_count == len(triggers).
type IssueTriggerPreviewResponse struct {
	Triggers   []IssueTriggerPreviewItem `json:"triggers"`
	TotalCount int                       `json:"total_count"`
}

// PreviewIssueTrigger dry-runs WillEnqueueRun for a prospective issue write and
// returns the runs that would start, without any side effect. It is the single
// authority the four entry points (create / single assign / single status /
// batch) consult so the frontend never re-implements the enqueue rule
// (MUL-3375). Mirrors PreviewCommentTriggers.
func (h *Handler) PreviewIssueTrigger(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace is required")
		return
	}

	var req IssueTriggerPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IssueIDs) > maxPreviewTriggerIssues {
		writeError(w, http.StatusBadRequest, "too many issue_ids")
		return
	}

	// Resolve the prospective assignee once — a malformed id is a deterministic
	// 400, never a silent miscount.
	var (
		newAssigneeType pgtype.Text
		newAssigneeID   pgtype.UUID
		hasNewAssignee  bool
	)
	if req.AssigneeType != nil && *req.AssigneeType != "" && req.AssigneeID != nil && *req.AssigneeID != "" {
		id, parseOK := parseUUIDOrBadRequest(w, *req.AssigneeID, "assignee_id")
		if !parseOK {
			return
		}
		newAssigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
		newAssigneeID = id
		hasNewAssignee = true
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	resp := IssueTriggerPreviewResponse{Triggers: make([]IssueTriggerPreviewItem, 0)}

	appendTrigger := func(issue db.Issue, in service.IssueTriggerInput) {
		probe := h.issueTriggerPreviewProbe(r, actorType, actorID, workspaceID, issue)
		if trigger, ok := h.IssueService.WillEnqueueRun(r.Context(), in, probe); ok {
			resp.Triggers = append(resp.Triggers, IssueTriggerPreviewItem{
				IssueID:          uuidToString(trigger.IssueID),
				AgentID:          uuidToString(trigger.AgentID),
				Source:           string(trigger.Source),
				HandoffSupported: h.runtimeSupportsHandoff(r.Context(), trigger.AgentID),
			})
		}
	}

	if req.IsCreate {
		wsUUID, err := util.ParseUUID(workspaceID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workspace")
			return
		}
		status := "todo"
		if req.Status != nil && *req.Status != "" {
			status = *req.Status
		}
		candidate := db.Issue{
			WorkspaceID:  wsUUID,
			Status:       status,
			AssigneeType: newAssigneeType,
			AssigneeID:   newAssigneeID,
		}
		appendTrigger(candidate, service.IssueTriggerInput{Issue: candidate, IsCreate: true})
		resp.TotalCount = len(resp.Triggers)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	for _, rawID := range req.IssueIDs {
		issueUUID, err := util.ParseUUID(rawID)
		if err != nil {
			continue // malformed id contributes no trigger; deterministic
		}
		loaded, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueUUID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			continue // cross-workspace / unknown id contributes no trigger
		}

		post := loaded
		in := service.IssueTriggerInput{PrevStatus: loaded.Status}
		if hasNewAssignee {
			post.AssigneeType = newAssigneeType
			post.AssigneeID = newAssigneeID
			in.AssigneeChanged = loaded.AssigneeType.String != newAssigneeType.String ||
				uuidToString(loaded.AssigneeID) != uuidToString(newAssigneeID)
		}
		if req.Status != nil && *req.Status != "" {
			post.Status = *req.Status
			in.StatusChanged = loaded.Status != *req.Status
		}
		in.Issue = post
		appendTrigger(post, in)
	}

	resp.TotalCount = len(resp.Triggers)
	writeJSON(w, http.StatusOK, resp)
}

// runtimeSupportsHandoff reports whether the agent's bound runtime reports a
// CLI version new enough to render handoff notes. Drives the preview's
// handoff_supported soft-gate signal. Any resolution failure → false (degrade).
func (h *Handler) runtimeSupportsHandoff(ctx context.Context, agentID pgtype.UUID) bool {
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || !agent.RuntimeID.Valid {
		return false
	}
	rt, err := h.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return false
	}
	return agentver.HandoffSupported(readRuntimeCLIVersion(rt.Metadata))
}
