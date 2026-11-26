package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
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
func (h *Handler) issueTriggerWriteProbe(r *http.Request, actorType, actorID string, issue db.Issue) service.IssueTriggerProbe {
	return service.IssueTriggerProbe{
		CanAccessAgent: nil, // allow-all; gate lives at the write boundary
		IsSelfLoop: func(targetAgentID pgtype.UUID) bool {
			return h.isAgentRunningOnIssue(r, actorType, issue, targetAgentID)
		},
		SuppressActiveSelfAssignment: func(agentID pgtype.UUID) bool {
			suppress := h.shouldSuppressActiveSelfAssignment(r.Context(), actorType, actorID, issue.ID, agentID)
			if suppress {
				slog.Info("suppressing duplicate self-assignment enqueue",
					"issue_id", uuidToString(issue.ID),
					"agent_id", uuidToString(agentID),
				)
			}
			return suppress
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
		IsSelfLoop: func(targetAgentID pgtype.UUID) bool {
			return h.isAgentRunningOnIssue(r, actorType, issue, targetAgentID)
		},
		SuppressActiveSelfAssignment: func(agentID pgtype.UUID) bool {
			return h.shouldSuppressActiveSelfAssignment(r.Context(), actorType, actorID, issue.ID, agentID)
		},
	}
}

// shouldSuppressActiveSelfAssignment prevents a trusted task-scoped agent
// actor from creating another run for the target pair merely to claim issue
// ownership. It intentionally checks the TARGET pair, not whether the actor is
// busy anywhere: cross-issue self handoffs are a supported workflow and must
// still enqueue when the target has no active run. Query errors fail closed
// against the external enqueue side effect while leaving the ownership write
// itself intact. The API still returns success for that ownership write; only
// the server log exposes the failed advisory lookup, because enqueue is the
// optional side effect and suppressing it is safer than risking duplicate work.
func (h *Handler) shouldSuppressActiveSelfAssignment(ctx context.Context, actorType, actorID string, issueID, targetAgentID pgtype.UUID) bool {
	if actorType != "agent" || actorID == "" || actorID != uuidToString(targetAgentID) {
		return false
	}
	active, err := h.Queries.HasActiveTaskForIssueAndAgent(ctx, db.HasActiveTaskForIssueAndAgentParams{IssueID: issueID, AgentID: targetAgentID})
	return active || err != nil
}

// logDeclinedIssueRun records why an issue write that could have started a run
// did not (#10845). The precise cause goes to server logs, not the write
// response (see admission.go). Parking an assignment in backlog is routine, so
// it logs at debug.
func logDeclinedIssueRun(ctx context.Context, issue db.Issue, trigger service.IssueRunTrigger) {
	if trigger.DeclineReason == "" {
		return
	}
	level := slog.LevelInfo
	if trigger.DeclineReason == ReasonDeferred {
		level = slog.LevelDebug
	}
	slog.Log(ctx, level, "issue write did not start a run",
		"issue_id", uuidToString(issue.ID),
		"assignee_type", issue.AssigneeType.String,
		"assignee_id", uuidToString(issue.AssigneeID),
		"reason", trigger.DeclineReason,
	)
}

// IssueRunOutcome tells the caller of an issue write what happened to the run
// it could have started (#10845). It has the shape of CommentTriggerOutcome:
// target_id is the assignee the issue names (the squad id for a squad) and
// reason_code a stable dispatch code. Like trigger_outcomes it goes to the
// caller only, never into the issue:updated broadcast.
type IssueRunOutcome struct {
	TargetType string             `json:"target_type"` // "agent" | "squad"
	TargetID   string             `json:"target_id"`
	Status     DispatchStatus     `json:"status"` // queued | coalesced | deferred | blocked
	ReasonCode DispatchReasonCode `json:"reason_code"`
}

// resolveIssueRun starts the run an issue write decided on and reports the
// result. willRun is the predicate's decision before suppress_run; a write
// that could not have started a run (no reason either) reports nothing.
//
// The write probe admits every caller, so a promotion can start a private
// agent the caller may not invoke. Its outcome would then expose that agent's
// readiness, so the caller gets no outcome; the server log keeps the cause.
func (h *Handler) resolveIssueRun(r *http.Request, issue db.Issue, trigger service.IssueRunTrigger, willRun, suppressRun bool, actorType, actorID, workspaceID, handoffNote string) *IssueRunOutcome {
	if issue.AssigneeType.String != "agent" && issue.AssigneeType.String != "squad" {
		return nil
	}
	var status DispatchStatus
	var reason DispatchReasonCode
	switch {
	case willRun && suppressRun:
		status, reason = DispatchDeferred, ReasonDeferred
		// Otherwise the dependency rule starts on its next pass the run this
		// write declined.
		if err := h.Queries.DismissIssueBlockerRelease(r.Context(), issue.ID); err != nil {
			slog.Warn("dismiss blocker release failed", "issue_id", uuidToString(issue.ID), "error", err)
		}
	case willRun:
		status, reason = h.dispatchIssueRun(r.Context(), issue, trigger, actorType, actorID, handoffNote)
	case trigger.DeclineReason != "":
		status, reason = declinedRunStatus(trigger.DeclineReason), trigger.DeclineReason
	default:
		return nil
	}
	if !h.callerCanInvokeIssueAssignee(r, issue, actorType, actorID, workspaceID) {
		return nil
	}
	return &IssueRunOutcome{
		TargetType: issue.AssigneeType.String,
		TargetID:   uuidToString(issue.AssigneeID),
		Status:     status,
		ReasonCode: reason,
	}
}

// declinedRunStatus maps a decline reason to its status, matching the comment
// path: a run already pending or active covers the write, backlog parks it,
// and anything else is blocked.
func declinedRunStatus(reason DispatchReasonCode) DispatchStatus {
	switch reason {
	case ReasonCoalesced:
		return DispatchCoalesced
	case ReasonDeferred, ReasonAlreadyActive:
		return DispatchDeferred
	}
	return DispatchBlocked
}

// callerCanInvokeIssueAssignee applies the preview probe's gate to the agent
// that runs: the assignee, or a squad's leader. A target that no longer
// resolves has no readiness to expose.
func (h *Handler) callerCanInvokeIssueAssignee(r *http.Request, issue db.Issue, actorType, actorID, workspaceID string) bool {
	agentID := issue.AssigneeID
	if issue.AssigneeType.String == "squad" {
		squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			return true
		}
		agentID = squad.LeaderID
	}
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil {
		return true
	}
	return h.invokeAgentDecision(r.Context(), agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID)
}

// dispatchIssueRun executes the enqueue side effect for a decision produced by
// WillEnqueueRun and reports what happened. handoffNote is a legacy API input
// retained for installed clients and travels only with a run that actually
// starts. The squad path still flows through enqueueSquadLeaderTask so the
// leader access gate and pending dedup stay in one place.
func (h *Handler) dispatchIssueRun(ctx context.Context, issue db.Issue, trigger service.IssueRunTrigger, actorType, actorID, handoffNote string) (DispatchStatus, DispatchReasonCode) {
	switch trigger.AssigneeType {
	case "agent":
		// The member who performed this assign/promote is the accountable human
		// for the run (MUL-4302 §4). An agent actor is not a human, so only a
		// member actor is threaded; otherwise attribution falls back to the chain.
		_, err := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, issue, handoffNote, memberActorUserID(actorType, actorID))
		if err == nil {
			return DispatchQueued, ReasonQueued
		}
		return issueEnqueueFailure(issue, issue.AssigneeID, err)
	case "squad":
		return h.enqueueSquadLeaderTask(ctx, issue, actorType, actorID, handoffNote, trigger.WorkflowHandoff)
	}
	return DispatchBlocked, ReasonTargetUnavailable
}

// issueEnqueueFailure types an enqueue error on the issue write path. A
// pending task that won the slot already covers the write; the service logs
// that coalesce at debug. Anything else is logged here.
func issueEnqueueFailure(issue db.Issue, agentID pgtype.UUID, err error) (DispatchStatus, DispatchReasonCode) {
	if errors.Is(err, service.ErrDuplicatePendingTask) {
		return DispatchCoalesced, ReasonCoalesced
	}
	log := slog.Default()
	if issue.AssigneeType.String == "squad" {
		log = log.With("squad_id", uuidToString(issue.AssigneeID))
	}
	log.Warn("issue write: run enqueue failed",
		"issue_id", uuidToString(issue.ID),
		"agent_id", uuidToString(agentID),
		"error", err,
	)
	if errors.Is(err, service.ErrIssueInTriage) {
		return DispatchBlocked, ReasonIssueInTriage
	}
	return DispatchBlocked, commentEnqueueFailureReason(err)
}

// memberActorUserID returns the acting member's user id as a pgtype.UUID when the
// actor is a member, and an invalid UUID otherwise (an agent actor id is not a
// human and must never become an accountable human). Used to thread the
// assign/promote actor into the attribution resolver (MUL-4302 §4).
func memberActorUserID(actorType, actorID string) pgtype.UUID {
	if actorType != "member" {
		return pgtype.UUID{}
	}
	uid, err := util.ParseUUID(actorID)
	if err != nil {
		return pgtype.UUID{}
	}
	return uid
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
	// ProjectID places a create preview in a project, so its workflow's
	// starting status and starting-step handoff apply. (MUL-7420)
	ProjectID *string `json:"project_id,omitempty"`
}

// IssueTriggerPreviewItem is one issue that WILL start a run under the
// prospective write. AgentID is the runnable agent (squad leader for squads).
type IssueTriggerPreviewItem struct {
	IssueID string `json:"issue_id"`
	AgentID string `json:"agent_id"`
	Source  string `json:"source"`
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
	// MUL-8472: an issue or project the caller cannot see previews like an
	// unknown one.
	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
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
				IssueID: uuidToString(trigger.IssueID),
				AgentID: uuidToString(trigger.AgentID),
				Source:  string(trigger.Source),
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
		// Mirror CreateIssue's workflow handling: the starting column's
		// handoff when no assignee was picked. (MUL-7420)
		if req.ProjectID != nil && *req.ProjectID != "" {
			if projectUUID, err := util.ParseUUID(*req.ProjectID); err == nil && hidden.CanSeeProject(projectUUID) {
				if def, project, err := issueworkflow.ForProject(r.Context(), h.Queries, wsUUID, projectUUID); err == nil && def != nil {
					if step, ok := def.Step(candidate.Status); ok && step.HandsOff() && !hasNewAssignee {
						if t, id, ok := issueworkflow.ResolveHandler(step, project, actorType, parseUUID(actorID)); ok {
							candidate.AssigneeType = pgtype.Text{String: t, Valid: true}
							candidate.AssigneeID = id
						}
					}
				}
			}
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
		if err != nil || !hidden.CanSeeProject(loaded.ProjectID) {
			continue // cross-workspace / unknown / hidden id contributes no trigger
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
			// A status that enters a workflow handoff step reassigns the
			// issue, exactly as UpdateIssue will. (MUL-7420)
			if !hasNewAssignee {
				params := db.UpdateIssueParams{
					ProjectID:    loaded.ProjectID,
					Status:       pgtype.Text{String: *req.Status, Valid: true},
					AssigneeType: loaded.AssigneeType,
					AssigneeID:   loaded.AssigneeID,
				}
				if hand, err := h.applyIssueWorkflow(r.Context(), loaded, &params, true, false, false, nil); err == nil && hand != nil {
					post.AssigneeType = params.AssigneeType
					post.AssigneeID = params.AssigneeID
					in.AssigneeChanged = true
					in.EnteredHandoffStep = true
				}
			}
		}
		in.Issue = post
		appendTrigger(post, in)
	}

	resp.TotalCount = len(resp.Triggers)
	writeJSON(w, http.StatusOK, resp)
}
