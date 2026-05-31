package handler

// CEREBRO-PATCH(orchestration-cerebro): FIR-2564 — server-side trigger for the
// cerebro orchestration layer. This whole file is cerebro-only; it is wired
// into upstream code from exactly two marked call sites (AttachLabel and the
// child-done path in UpdateIssue), documented in docs/cerebro-patches.md.
//
// Behavior: attaching the `orchestrate` label to a parent issue makes the
// platform drive that issue's sub-issues automatically. The sub-issues are the
// tasks; their `blocks` edges are the dependencies. On label attach we start
// every child whose blockers are already terminal; each time a child finishes,
// we start the children it just unblocked. Jesper works in the UI, not the
// CLI — setting the label IS the run command, and removing it stops further
// auto-starts. The label is the only control: there is no separate workspace
// toggle (it belongs on the issue, not the workspace).
//
// Squad-only trigger (FIR-2564): orchestration runs ONLY when the parent issue
// is assigned to a squad. The squad leader both owns the composite task and is
// the independent verifier of each finished sub-issue. A non-squad parent with
// the label is told to assign a squad and does nothing else.
//
// Plan-adequacy precheck (FIR-2564): before starting any wave the engine checks
// the plan is detailed enough — every sub-issue must carry acceptance criteria
// (a `- [ ]` checklist). Without criteria there is nothing for the verifier to
// judge. An under-specified plan is NOT bounced back to the human: the engine
// hands it to the SQUAD LEADER to finish (add the criteria, fix dependencies)
// and re-apply the label.
//
// Sub-issues are driven primarily through squads — a squad-assigned sub-issue
// wakes the squad leader, who delegates within the team. Agent-assigned
// sub-issues are also supported and wake the agent directly.
//
// Verification gate (FIR-2564): a sub-issue reaching `done` does NOT release
// the work that waits on it on its own word. An independent verifier judges the
// deliverable against the sub-issue's acceptance criteria first; only an
// `orch-verified` (PASS) label releases dependents, while `orch-rejected` (FAIL)
// re-opens the child for rework. See the "Verification gate" section below.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/orchestration"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CEREBRO-PATCH(orchestration-thread): FIR-2564 — single Orchestrator comment
// thread + dedup fix + in_review completion.
// orchestrationThreadMetaKey stores, per issue, the comment id that roots all of
// that issue's Orchestrator comments — so they stay in one thread instead of
// flooding the issue with top-level system comments.
const orchestrationThreadMetaKey = "orch_thread"

const (
	orchestrateLabelName = "orchestrate"
	// orchestrateVerifiedLabelName is the PASS token an independent verifier
	// attaches to a finished sub-issue once it confirms the deliverable. Only
	// then does the engine let that sub-issue release its dependents.
	orchestrateVerifiedLabelName = "orch-verified"
	// orchestrateRejectedLabelName is the FAIL token a verifier attaches when
	// the deliverable does not meet the sub-issue's acceptance criteria. The
	// engine re-opens the child and re-dispatches its worker.
	orchestrateVerifiedLabelColor = "#16a34a"
	orchestrateRejectedLabelName  = "orch-rejected"
	orchestrateRejectedLabelColor = "#dc2626"
	// orchestrateEvalLabelName marks that the final full-delivery evaluation has
	// already been requested for a parent, so it is requested exactly once.
	orchestrateEvalLabelName  = "orch-eval-done"
	orchestrateEvalLabelColor = "#7c3aed"

	// CEREBRO-PATCH(orchestration-comments-eval): FIR-2564 — orchestrator
	// attribution prefix + dedup + full-delivery eval.
	// orchestratorCommentPrefix is prepended to every comment the engine posts so
	// it is unmistakably the Orchestrator speaking — not a human or a worker agent.
	orchestratorCommentPrefix = "🎼 **Orchestrator**\n\n"
)

// maybeStartOrchestrationOnLabel is the entrypoint from AttachLabel. It routes
// the three orchestration labels: `orchestrate` starts/advances a parent's
// sub-issues; `orch-verified` (PASS) and `orch-rejected` (FAIL) are the
// independent-verifier verdicts on a finished child. Any other label is a
// no-op. Best-effort: failures are logged, never surfaced to the label-attach
// response (the label attach already succeeded).
func (h *Handler) maybeStartOrchestrationOnLabel(ctx context.Context, issue db.Issue, labelID pgtype.UUID) {
	label, err := h.Queries.GetLabel(ctx, db.GetLabelParams{ID: labelID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(label.Name)) {
	case orchestrateLabelName:
		// CEREBRO-PATCH(orchestration-squad-gate): FIR-2564 — squad-only trigger.
		// Orchestration runs only on a parent issue assigned to a squad. The
		// squad leader is the team that owns the composite task AND the
		// independent verifier of each finished sub-issue, so squad-ownership is
		// the precondition for the whole flow.
		if !issueAssignedToSquad(issue) {
			h.postOrchestrationComment(ctx, issue,
				"Orchestration only runs on an issue assigned to a **squad**. "+
					"Assign this issue to a squad (the squad leader drives the sub-issues and "+
					"verifies each one), then re-apply the `orchestrate` label.")
			return
		}
		h.runOrchestration(ctx, issue, true)
	case orchestrateVerifiedLabelName:
		h.advanceOnChildVerified(ctx, issue)
	case orchestrateRejectedLabelName:
		h.reopenRejectedChild(ctx, issue)
	}
}

// issueAssignedToSquad reports whether the issue is assigned to a squad.
func issueAssignedToSquad(issue db.Issue) bool {
	return issue.AssigneeType.Valid && issue.AssigneeType.String == "squad"
}

// isOrchestratedParent reports whether a parent issue is eligible to be driven:
// it carries the `orchestrate` label AND is assigned to a squad. Used by the
// child-done / verdict entrypoints so they honor the same squad-only gate as
// the label trigger.
func (h *Handler) isOrchestratedParent(ctx context.Context, parent db.Issue) bool {
	return issueAssignedToSquad(parent) && h.issueHasOrchestrateLabel(ctx, parent)
}

// advanceOrchestrationOnChildDone is the entrypoint from the UpdateIssue
// child-done path. When a child of an orchestrated parent reaches `done`, it
// may have unblocked siblings — start whichever are now ready. The parent is
// "orchestrated" iff it currently carries the `orchestrate` label.
func (h *Handler) advanceOrchestrationOnChildDone(ctx context.Context, prev, issue db.Issue) {
	if !issue.ParentIssueID.Valid {
		return
	}
	if prev.Status == "done" || issue.Status != "done" {
		return
	}
	parent, err := h.Queries.GetIssue(ctx, issue.ParentIssueID)
	if err != nil {
		return
	}
	if parent.Status == "done" || parent.Status == "cancelled" {
		return
	}
	if !h.isOrchestratedParent(ctx, parent) {
		return
	}

	// Verification gate (FIR-2564): a child reporting `done` is NOT trusted to
	// release its dependents on its own word. Before advancing, if this child
	// is done and not yet independently verified, request verification — an
	// independent agent judges the deliverable against the child's acceptance
	// criteria (or, when no independent agent exists, the human owner is asked).
	// runOrchestration below still releases any OTHER children that became
	// ready (e.g. siblings unblocked by a cancelled or already-verified
	// blocker), but this child's own dependents stay blocked until it is
	// verified.
	if issue.Status == "done" && !h.issueHasLabelNamed(ctx, issue, orchestrateVerifiedLabelName) {
		h.requestChildVerification(ctx, parent, issue)
	}

	h.runOrchestration(ctx, parent, false)
}

// issueHasOrchestrateLabel reports whether the issue currently carries the
// `orchestrate` label.
func (h *Handler) issueHasOrchestrateLabel(ctx context.Context, issue db.Issue) bool {
	return h.issueHasLabelNamed(ctx, issue, orchestrateLabelName)
}

// issueHasLabelNamed reports whether the issue currently carries a label with
// the given (case-insensitive) name.
func (h *Handler) issueHasLabelNamed(ctx context.Context, issue db.Issue, name string) bool {
	labels, err := h.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return false
	}
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l.Name), name) {
			return true
		}
	}
	return false
}

// runOrchestration loads the parent's children + their blocker edges, validates
// the dependency graph, and starts every child that is ready now. When
// postSummary is true (the label-attach entrypoint) it also posts a system
// comment describing the wave plan and what it started — so a UI-only user sees
// the engine working. The child-done entrypoint passes postSummary=false to
// keep step-by-step advancement quiet (one comment per finished child would be
// noise); it still posts a short note when it actually starts something.
func (h *Handler) runOrchestration(ctx context.Context, parent db.Issue, postSummary bool) {
	childIssues, err := h.Queries.ListChildIssues(ctx, parent.ID)
	if err != nil {
		slog.Warn("orchestration: list children failed", "parent_id", uuidToString(parent.ID), "error", err)
		return
	}

	issueByID := map[string]db.Issue{}
	childIDs := make([]pgtype.UUID, 0, len(childIssues))
	children := make([]orchestration.ChildState, 0, len(childIssues))
	for _, c := range childIssues {
		id := uuidToString(c.ID)
		issueByID[orchestration.NormalizeID(id)] = c
		childIDs = append(childIDs, c.ID)
		children = append(children, orchestration.ChildState{
			ID:     id,
			Number: c.Number,
			Title:  c.Title,
			Status: c.Status,
		})
	}

	if len(children) == 0 {
		if postSummary {
			h.postOrchestrationComment(ctx, parent,
				"Orchestration label set, but this issue has no sub-issues to drive. "+
					"Add sub-issues and `blocks` dependencies between them, then re-apply the label.")
		}
		return
	}

	blockersByChild := h.loadBlockers(ctx, parent.WorkspaceID, childIDs)
	h.markVerificationGate(ctx, childIssues, blockersByChild)
	plan := orchestration.PlanFromChildren(parent.Title, children, blockersByChild)

	if orchestration.DetectCycle(plan.Nodes) {
		if postSummary {
			h.postOrchestrationComment(ctx, parent,
				"Cannot orchestrate: the sub-issues have a circular `blocks` dependency "+
					"(some sub-issue waits on one that waits back on it). Fix the dependency "+
					"loop and re-apply the `orchestrate` label.")
		}
		return
	}

	// Plan-adequacy precheck (FIR-2564): the plan must be detailed enough to
	// verify before the engine runs it. Every sub-issue needs acceptance
	// criteria (a `- [ ]` checklist in its description) — without them the
	// independent verifier has nothing to judge "delivered as planned" against.
	// Refuse an under-specified plan and say exactly which sub-issues to fix.
	var missingCriteria []string
	for _, c := range childIssues {
		if !orchestration.HasAcceptanceCriteria(c.Description.String) {
			missingCriteria = append(missingCriteria, "#"+strconv.Itoa(int(c.Number)))
		}
	}
	if len(missingCriteria) > 0 {
		if postSummary {
			// The plan isn't ready to run. The SQUAD LEADER finishes it — not the
			// human. Hand it to the leader to add each sub-issue's acceptance
			// criteria and re-apply the label. The owner is never asked to fill
			// in criteria themselves.
			h.requestPlanRefinement(ctx, parent, missingCriteria)
		}
		return
	}

	ready := orchestration.ReadyToStart(children, blockersByChild)
	started := []string{}
	for _, rc := range ready {
		issue, ok := issueByID[orchestration.NormalizeID(rc.ID)]
		if !ok {
			continue
		}
		if h.startOrchestratedChild(ctx, issue) {
			started = append(started, "#"+strconv.Itoa(int(issue.Number)))
		}
	}

	if postSummary {
		waves := orchestration.RenderWaves(children, plan)
		var b strings.Builder
		b.WriteString("Orchestration started for this issue's sub-issues.\n\n")
		b.WriteString("Plan (waves run top to bottom; everything in a wave can run at once):\n")
		for _, line := range waves {
			b.WriteString("- " + line + "\n")
		}
		if len(started) > 0 {
			b.WriteString("\nStarted now: " + strings.Join(started, ", ") + ".")
		} else {
			b.WriteString("\nNothing started yet — the first wave's sub-issues are either not assigned to a squad/agent, or already running.")
		}
		h.postOrchestrationComment(ctx, parent, b.String())
	} else if len(started) > 0 {
		h.postOrchestrationComment(ctx, parent,
			"Orchestration advanced: started "+strings.Join(started, ", ")+" now that their dependencies are done.")
	}

	// Full-delivery evaluation (FIR-2564): per-sub-issue verification is not the
	// same as "the whole feature actually works." Once every sub-issue is
	// complete (verified-done or cancelled), the orchestrator asks the squad
	// leader to evaluate the COMPLETE delivery end-to-end against the parent's
	// goal — is it built, deployed and working live — before this is called done.
	h.maybeRequestFullDeliveryEval(ctx, parent, childIssues)
}

// loadBlockers returns, per child issue id (string), the issues that block it.
func (h *Handler) loadBlockers(ctx context.Context, workspaceID pgtype.UUID, childIDs []pgtype.UUID) map[string][]orchestration.BlockerState {
	out := map[string][]orchestration.BlockerState{}
	if len(childIDs) == 0 {
		return out
	}
	rows, err := h.Queries.ListBlockedByForIssues(ctx, db.ListBlockedByForIssuesParams{
		IssueIds:    childIDs,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		slog.Warn("orchestration: list blockers failed", "error", err)
		return out
	}
	for _, row := range rows {
		blocked := uuidToString(row.IssueID)
		out[blocked] = append(out[blocked], orchestration.BlockerState{
			ID:     uuidToString(row.ID),
			Status: row.Status,
		})
	}
	return out
}

// startOrchestratedChild promotes a ready child out of `backlog` (so the
// platform's own enqueue gates accept it) and dispatches it. Returns true only
// when work was actually dispatched.
//
// Squad-assigned sub-issues are the primary path: the squad leader is woken and
// delegates within the team. Agent-assigned sub-issues are also supported and
// wake the agent directly. Children assigned to a human (or to nobody), or that
// already have a pending task, are left alone — orchestration never restarts
// running work and never auto-starts a human-owned issue.
func (h *Handler) startOrchestratedChild(ctx context.Context, child db.Issue) bool {
	// CEREBRO-PATCH(orchestration-cerebro): FIR-2564 squad-first dispatch.
	// Squad path (primary).
	if child.AssigneeType.Valid && child.AssigneeType.String == "squad" {
		if !h.isSquadLeaderReady(ctx, child) {
			return false
		}
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          child.AssigneeID,
			WorkspaceID: child.WorkspaceID,
		})
		if err != nil {
			return false
		}
		if h.hasPendingTask(ctx, child.ID, squad.LeaderID) {
			return false
		}
		promoted, ok := h.promoteFromBacklog(ctx, child)
		if !ok {
			return false
		}
		h.enqueueSquadLeaderTask(ctx, promoted, pgtype.UUID{}, "system", "")
		return true
	}

	// Agent path.
	if !h.isAgentAssigneeReady(ctx, child) {
		return false
	}
	if h.hasPendingTask(ctx, child.ID, child.AssigneeID) {
		return false
	}
	promoted, ok := h.promoteFromBacklog(ctx, child)
	if !ok {
		return false
	}
	if _, err := h.TaskService.EnqueueTaskForIssue(ctx, promoted); err != nil {
		slog.Warn("orchestration: enqueue child failed", "child_id", uuidToString(promoted.ID), "error", err)
		return false
	}
	return true
}

// hasPendingTask reports whether the given agent already has an active task for
// the issue. A lookup error is treated as "pending" so we fail closed and never
// double-dispatch.
func (h *Handler) hasPendingTask(ctx context.Context, issueID, agentID pgtype.UUID) bool {
	pending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: agentID,
	})
	return err != nil || pending
}

// promoteFromBacklog flips a sub-issue from `backlog` to `todo` so the
// platform's enqueue gates accept it, and broadcasts the change. A non-backlog
// child is returned unchanged. Returns ok=false only when the status write
// itself fails.
func (h *Handler) promoteFromBacklog(ctx context.Context, child db.Issue) (db.Issue, bool) {
	if child.Status != "backlog" {
		return child, true
	}
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          child.ID,
		Status:      "todo",
		WorkspaceID: child.WorkspaceID,
	})
	if err != nil {
		slog.Warn("orchestration: promote child failed", "child_id", uuidToString(child.ID), "error", err)
		return child, false
	}
	h.publishOrchestratedStatus(ctx, updated)
	return updated, true
}

// publishOrchestratedStatus emits the issue:updated event so UI boards reflect
// the backlog→todo promotion immediately, mirroring the payload shape that
// UpdateIssue broadcasts.
func (h *Handler) publishOrchestratedStatus(ctx context.Context, issue db.Issue) {
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	h.publishToAudience(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"issue":            issueToResponse(issue, prefix),
		"assignee_changed": false,
		"status_changed":   true,
		"prev_status":      "backlog",
	}, h.audienceForIssue(ctx, issue))
}

// postOrchestrationComment writes a system comment on the given issue and
// broadcasts it. Best-effort: failure is logged, not propagated.
func (h *Handler) postOrchestrationComment(ctx context.Context, issue db.Issue, content string) {
	h.postOrchestrationCommentReturning(ctx, issue, content)
}

// postOrchestrationCommentReturning is postOrchestrationComment but returns the
// created comment so callers can use its ID as a task trigger (e.g. the
// independent-verifier request). Returns ok=false on write failure.
func (h *Handler) postOrchestrationCommentReturning(ctx context.Context, issue db.Issue, content string) (db.Comment, bool) {
	// Attribution: make every engine comment unmistakably the Orchestrator's.
	body := orchestratorCommentPrefix + content
	// Dedup: the same engine action can fire twice (label re-applied, double
	// child-done nudge, watchdog tick). Skip posting if an identical Orchestrator
	// comment is already among the most recent comments on this issue.
	if existing := h.findRecentOrchestrationComment(ctx, issue, body); existing != nil {
		return *existing, true
	}
	// Single thread: keep all of this issue's Orchestrator comments under one
	// root so the issue doesn't fill with scattered top-level system comments.
	root := h.orchestrationThreadRoot(ctx, issue)
	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     body,
		Type:        "system",
		ParentID:    root,
	})
	if err != nil {
		slog.Warn("orchestration: create system comment failed", "issue_id", uuidToString(issue.ID), "error", err)
		return db.Comment{}, false
	}
	if !root.Valid {
		// First Orchestrator comment on this issue → it becomes the thread root.
		h.setOrchestrationThreadRoot(ctx, issue, comment.ID)
	}
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})
	return comment, true
}

// findRecentOrchestrationComment returns a recent system comment on the issue
// whose content exactly equals body, or nil if none. Used to suppress duplicate
// Orchestrator comments from a doubly-fired action. Best-effort: on query
// failure it returns nil (post proceeds).
func (h *Handler) findRecentOrchestrationComment(ctx context.Context, issue db.Issue, body string) *db.Comment {
	// Newest-first: the chronological ListCommentsForIssue returns oldest-first,
	// so on a long thread it never sees the just-posted duplicate.
	recent, err := h.Queries.ListRecentCommentsForIssue(ctx, db.ListRecentCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       25,
	})
	if err != nil {
		return nil
	}
	for i := range recent {
		c := recent[i]
		if c.AuthorType == "system" && c.Content == body {
			return &c
		}
	}
	return nil
}

// orchestrationThreadRoot returns the comment id that roots this issue's
// Orchestrator thread (from issue metadata), or an invalid UUID if none yet.
func (h *Handler) orchestrationThreadRoot(ctx context.Context, issue db.Issue) pgtype.UUID {
	fresh, err := h.Queries.GetIssue(ctx, issue.ID)
	if err != nil || len(fresh.Metadata) == 0 {
		return pgtype.UUID{}
	}
	var m map[string]any
	if json.Unmarshal(fresh.Metadata, &m) != nil {
		return pgtype.UUID{}
	}
	s, _ := m[orchestrationThreadMetaKey].(string)
	if s == "" {
		return pgtype.UUID{}
	}
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

// setOrchestrationThreadRoot records the thread root comment id in issue
// metadata. Best-effort.
func (h *Handler) setOrchestrationThreadRoot(ctx context.Context, issue db.Issue, commentID pgtype.UUID) {
	val, err := json.Marshal(uuidToString(commentID))
	if err != nil {
		return
	}
	if _, err := h.Queries.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{
		Key:         orchestrationThreadMetaKey,
		Value:       val,
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		slog.Warn("orchestration: set thread root failed", "issue_id", uuidToString(issue.ID), "error", err)
	}
}

// ---------------------------------------------------------------------------
// CEREBRO-PATCH(orchestration-verify-gate): FIR-2564 — independent verification
// gate. Cerebro-only; reuses the existing AttachLabel + child-done seams.
// Verification gate (FIR-2564)
//
// The orchestrator never trusts a sub-issue's own `done` to release the work
// that waits on it. When a child reaches `done`, an INDEPENDENT verifier judges
// the deliverable against the child's acceptance criteria before its dependents
// run. Defense-in-depth, cheapest layer first:
//
//   - Independent judge (a different agent than the worker): squad-owned parent
//     -> the squad leader; agent-owned parent -> the parent agent. The verifier
//     reviews and either attaches `orch-verified` (PASS) or `orch-rejected`
//     (FAIL).
//   - Human gate (no independent agent available, e.g. a human owns the parent):
//     the engine asks the owner to confirm, with explicit instructions.
//
// PASS (`orch-verified`) releases the child's dependents; FAIL (`orch-rejected`)
// re-opens the child and re-dispatches its worker. A child stays a blocker until
// it is verified, so nothing downstream runs on an unverified deliverable.
// ---------------------------------------------------------------------------

// markVerificationGate enriches the loaded blocker edges with verification
// state: a blocker that is one of this parent's own children must be VERIFIED
// (carry `orch-verified`), not merely done, before it stops holding back
// dependents. Cross-tree blockers are untouched (they keep terminal-status
// semantics). This is the handler-side input to orchestration.BlockerSatisfied.
func (h *Handler) markVerificationGate(ctx context.Context, childIssues []db.Issue, blockersByChild map[string][]orchestration.BlockerState) {
	verifiedByID := map[string]bool{}
	isChild := map[string]bool{}
	for _, c := range childIssues {
		id := orchestration.NormalizeID(uuidToString(c.ID))
		isChild[id] = true
		verifiedByID[id] = h.issueHasLabelNamed(ctx, c, orchestrateVerifiedLabelName)
	}
	for blockedID, blockers := range blockersByChild {
		for i := range blockers {
			bid := orchestration.NormalizeID(blockers[i].ID)
			if isChild[bid] {
				blockers[i].RequiresVerification = true
				blockers[i].Verified = verifiedByID[bid]
			}
		}
		blockersByChild[blockedID] = blockers
	}
}

// CEREBRO-PATCH(orchestration-plan-refinement): FIR-2564 — squad leader finishes
// an under-specified plan, not the human.
// requestPlanRefinement hands an under-specified plan to the SQUAD LEADER to
// finish — never the human. The leader adds the missing acceptance criteria to
// each named sub-issue (and fixes dependencies), then re-applies the
// `orchestrate` label to start. Idempotent: if the leader already has a pending
// task on the parent, nothing is posted (no spam on a re-applied label).
func (h *Handler) requestPlanRefinement(ctx context.Context, parent db.Issue, missing []string) {
	if !h.isSquadLeaderReady(ctx, parent) {
		// Parent is squad-gated, so this is rare; surface it instead of silently
		// stalling so the issue isn't stuck with no one to plan it.
		h.postOrchestrationComment(ctx, parent,
			"This plan needs acceptance criteria on sub-issues "+strings.Join(missing, ", ")+
				", but this squad has no available leader to finish the plan. Assign a squad "+
				"with an active leader and re-apply the `orchestrate` label.")
		return
	}
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          parent.AssigneeID,
		WorkspaceID: parent.WorkspaceID,
	})
	if err != nil {
		return
	}
	if h.hasPendingTask(ctx, parent.ID, squad.LeaderID) {
		return
	}
	instructions := "This issue is set to `orchestrate`, but the plan isn't ready to run yet: " +
		"these sub-issues have no acceptance criteria — " + strings.Join(missing, ", ") + ".\n\n" +
		"As squad leader, finish the plan (the issue owner should NOT have to do this):\n" +
		"1. Add a definition-of-done checklist (lines like `- [ ] ...`) to each listed sub-issue, " +
		"describing what \"delivered\" means — that is exactly what the verification step judges against.\n" +
		"2. Check the `blocks` dependencies between the sub-issues are right.\n" +
		"3. Then remove and re-apply the `orchestrate` label on this issue to start the run."
	comment, ok := h.postOrchestrationCommentReturning(ctx, parent, instructions)
	if !ok {
		return
	}
	h.enqueueSquadLeaderTask(ctx, parent, comment.ID, "system", "")
}

// requestChildVerification dispatches an independent verification of a finished
// child, or asks the human owner when no independent agent exists. Idempotent:
// a child already carrying a verdict label, or with a pending verifier task, is
// left alone.
func (h *Handler) requestChildVerification(ctx context.Context, parent, child db.Issue) {
	if h.issueHasLabelNamed(ctx, child, orchestrateVerifiedLabelName) ||
		h.issueHasLabelNamed(ctx, child, orchestrateRejectedLabelName) {
		return
	}
	// Ensure the verdict labels exist so the verifier (or the human) can attach
	// them by name without first creating them.
	h.ensureWorkspaceLabel(ctx, parent.WorkspaceID, orchestrateVerifiedLabelName, orchestrateVerifiedLabelColor)
	h.ensureWorkspaceLabel(ctx, parent.WorkspaceID, orchestrateRejectedLabelName, orchestrateRejectedLabelColor)

	verifierID, ok := h.selectVerifierAgent(ctx, parent, child)
	childRef := "#" + strconv.Itoa(int(child.Number))

	if !ok {
		// Human gate: no independent agent. Ask the parent owner to confirm.
		h.postOrchestrationComment(ctx, parent,
			"Sub-issue "+childRef+" reports done — it needs your verification before the "+
				"sub-issues that depend on it start.\n\n"+
				"Check "+childRef+" against its acceptance criteria (the checklist in its "+
				"description). If it delivered: add the `"+orchestrateVerifiedLabelName+"` label to "+
				childRef+" and the next wave starts. If not: add `"+orchestrateRejectedLabelName+"` "+
				"to send it back for rework.")
		return
	}

	if h.hasPendingTask(ctx, child.ID, verifierID) {
		return
	}
	req, posted := h.postOrchestrationCommentReturning(ctx, child, h.buildVerifierInstructions(childRef))
	if !posted {
		return
	}
	if _, err := h.TaskService.EnqueueTaskForMention(ctx, child, verifierID, req.ID); err != nil {
		slog.Warn("orchestration: enqueue verifier failed", "child_id", uuidToString(child.ID), "error", err)
		return
	}
	h.postOrchestrationComment(ctx, parent,
		"Verifying "+childRef+" independently before releasing the sub-issues that depend on it.")
}

// buildVerifierInstructions is the judge prompt posted on the child as the
// verifier's task trigger. It is explicit enough to act on without the
// orchestration-verify skill, but that skill documents the same contract.
func (h *Handler) buildVerifierInstructions(childRef string) string {
	return "Independent verification requested for " + childRef + ".\n\n" +
		"You did NOT do this work — your job is to judge it, not to trust the `done`.\n\n" +
		"1. Read this issue's acceptance criteria (the definition-of-done checklist in its " +
		"description) and the work that was delivered (PR, artifact, comments).\n" +
		"2. Verify against evidence, not claims — a checklist item counts only if you can see " +
		"it actually delivered (tests green, file exists, PR merged, feature works). " +
		"\"should/probably/seems\" is not verified.\n" +
		"3. Verdict:\n" +
		"   - PASS → attach the `" + orchestrateVerifiedLabelName + "` label to this issue. " +
		"That releases the sub-issues waiting on it.\n" +
		"   - FAIL → attach the `" + orchestrateRejectedLabelName + "` label and add one comment " +
		"listing exactly which criteria are unmet. The issue is re-opened and its worker redoes it.\n\n" +
		"Default to FAIL if you cannot confirm the deliverable."
}

// selectVerifierAgent picks an agent that is independent of the child's worker.
// Squad-owned parent -> the squad leader. Agent-owned parent (different from the
// child's own agent) -> the parent agent. Otherwise ok=false (human gate).
func (h *Handler) selectVerifierAgent(ctx context.Context, parent, child db.Issue) (pgtype.UUID, bool) {
	if parent.AssigneeType.Valid && parent.AssigneeType.String == "squad" {
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          parent.AssigneeID,
			WorkspaceID: parent.WorkspaceID,
		})
		if err != nil || !squad.LeaderID.Valid {
			return pgtype.UUID{}, false
		}
		// Leader must not be the child's own agent worker.
		if child.AssigneeType.Valid && child.AssigneeType.String == "agent" &&
			child.AssigneeID == squad.LeaderID {
			return pgtype.UUID{}, false
		}
		return squad.LeaderID, true
	}
	if parent.AssigneeType.Valid && parent.AssigneeType.String == "agent" && parent.AssigneeID.Valid {
		if child.AssigneeType.Valid && child.AssigneeType.String == "agent" &&
			child.AssigneeID == parent.AssigneeID {
			return pgtype.UUID{}, false
		}
		return parent.AssigneeID, true
	}
	return pgtype.UUID{}, false
}

// advanceOnChildVerified runs when an `orch-verified` label is attached to a
// child. If the child's parent is orchestrated, release whatever the now-verified
// child unblocked.
func (h *Handler) advanceOnChildVerified(ctx context.Context, child db.Issue) {
	if !child.ParentIssueID.Valid {
		return
	}
	parent, err := h.Queries.GetIssue(ctx, child.ParentIssueID)
	if err != nil || parent.Status == "done" || parent.Status == "cancelled" {
		return
	}
	if !h.isOrchestratedParent(ctx, parent) {
		return
	}
	// Workers often finish a sub-issue into `in_review` (or leave it
	// in_progress), not `done`. A PASS verdict completes it so the parent can
	// see it terminal and the wave/full-delivery logic can advance.
	if child.Status == "in_review" || child.Status == "in_progress" || child.Status == "todo" {
		if updated, uerr := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:          child.ID,
			Status:      "done",
			WorkspaceID: child.WorkspaceID,
		}); uerr == nil {
			h.publishOrchestratedStatus(ctx, updated)
		}
	}
	h.runOrchestration(ctx, parent, false)
}

// reopenRejectedChild runs when an `orch-rejected` label is attached to a child.
// The deliverable failed verification: re-open the child and re-dispatch its
// worker so the work is redone. The rejected label is removed so the next
// verification round starts clean. No-op unless the parent is orchestrated.
func (h *Handler) reopenRejectedChild(ctx context.Context, child db.Issue) {
	if !child.ParentIssueID.Valid {
		return
	}
	parent, err := h.Queries.GetIssue(ctx, child.ParentIssueID)
	if err != nil || parent.Status == "done" || parent.Status == "cancelled" {
		return
	}
	if !h.isOrchestratedParent(ctx, parent) {
		return
	}
	// Re-open: send the child back to todo so its worker is re-dispatched.
	reopened, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          child.ID,
		Status:      "todo",
		WorkspaceID: child.WorkspaceID,
	})
	if err != nil {
		slog.Warn("orchestration: reopen rejected child failed", "child_id", uuidToString(child.ID), "error", err)
		return
	}
	// Clear the rejected marker so the NEXT completion re-triggers verification
	// (requestChildVerification skips a child that still carries a verdict label).
	h.detachWorkspaceLabel(ctx, child, orchestrateRejectedLabelName)
	h.publishOrchestratedStatus(ctx, reopened)
	h.startOrchestratedChild(ctx, reopened)
	h.postOrchestrationComment(ctx, parent,
		"Sub-issue #"+strconv.Itoa(int(child.Number))+" failed verification and was sent back for rework.")
}

// detachWorkspaceLabel removes a label (by name) from an issue if present.
// Best-effort: lookup/detach failures are logged, not propagated.
func (h *Handler) detachWorkspaceLabel(ctx context.Context, issue db.Issue, name string) {
	labels, err := h.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return
	}
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l.Name), name) {
			if err := h.Queries.DetachLabelFromIssue(ctx, db.DetachLabelFromIssueParams{
				IssueID:     issue.ID,
				LabelID:     l.ID,
				WorkspaceID: issue.WorkspaceID,
			}); err != nil {
				slog.Warn("orchestration: detach label failed", "name", name, "error", err)
			}
			return
		}
	}
}

// maybeRequestFullDeliveryEval dispatches a final, holistic evaluation of the
// COMPLETE delivery once every sub-issue is complete (verified-done or
// cancelled, with at least one real delivery). Per-sub-issue verification only
// proves each step in isolation; this asks the squad leader to confirm the whole
// feature is built, deployed and actually working live before the parent is
// done. Fires exactly once (guarded by the `orch-eval-done` label on the parent).
func (h *Handler) maybeRequestFullDeliveryEval(ctx context.Context, parent db.Issue, childIssues []db.Issue) {
	if len(childIssues) == 0 || h.issueHasLabelNamed(ctx, parent, orchestrateEvalLabelName) {
		return
	}
	anyDelivered := false
	for _, c := range childIssues {
		switch c.Status {
		case "cancelled":
			// complete, nothing delivered
		case "done":
			if !h.issueHasLabelNamed(ctx, c, orchestrateVerifiedLabelName) {
				return // a done child still awaiting verification — not complete yet
			}
			anyDelivered = true
		default:
			return // a child still in flight — not complete yet
		}
	}
	if !anyDelivered {
		return // everything cancelled — nothing to evaluate
	}
	if !h.isSquadLeaderReady(ctx, parent) {
		return
	}
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          parent.AssigneeID,
		WorkspaceID: parent.WorkspaceID,
	})
	if err != nil || h.hasPendingTask(ctx, parent.ID, squad.LeaderID) {
		return
	}
	// Mark requested (once) before dispatching. Attaching via Queries does NOT
	// re-enter the label trigger (that only fires from the HTTP AttachLabel seam).
	if id := h.ensureWorkspaceLabel(ctx, parent.WorkspaceID, orchestrateEvalLabelName, orchestrateEvalLabelColor); id.Valid {
		_ = h.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
			IssueID:     parent.ID,
			LabelID:     id,
			WorkspaceID: parent.WorkspaceID,
		})
	}
	content := "All sub-issues are delivered and individually verified. Requesting a " +
		"**full-delivery evaluation** from the squad leader before this is called done.\n\n" +
		"Squad leader: evaluate the COMPLETE delivery end-to-end against this issue's goal:\n" +
		"1. Does the whole feature actually work together — not just that each step's checks " +
		"passed in isolation?\n" +
		"2. Is it merged AND deployed live? Verify in the real app, not from the PRs alone " +
		"(\"should/probably/seems\" is not verified — show evidence).\n" +
		"3. Do the delivered pieces still fit the original plan, or did something drift?\n\n" +
		"Post a short evaluation — what works live, what doesn't, and a clear verdict (delivered " +
		"or not). If it is genuinely delivered and working live, mark this issue done; otherwise " +
		"open follow-up sub-issues for the gaps."
	comment, ok := h.postOrchestrationCommentReturning(ctx, parent, content)
	if !ok {
		return
	}
	h.enqueueSquadLeaderTask(ctx, parent, comment.ID, "system", "")
}

// ensureWorkspaceLabel returns the id of a workspace label with the given name,
// creating it if absent. Best-effort: returns invalid on failure.
func (h *Handler) ensureWorkspaceLabel(ctx context.Context, workspaceID pgtype.UUID, name, color string) pgtype.UUID {
	labels, err := h.Queries.ListLabels(ctx, workspaceID)
	if err == nil {
		for _, l := range labels {
			if strings.EqualFold(strings.TrimSpace(l.Name), name) {
				return l.ID
			}
		}
	}
	created, err := h.Queries.CreateLabel(ctx, db.CreateLabelParams{
		WorkspaceID: workspaceID,
		Name:        name,
		Color:       color,
	})
	if err != nil {
		slog.Warn("orchestration: ensure label failed", "name", name, "error", err)
		return pgtype.UUID{}
	}
	return created.ID
}
