package workflows

// loop_advance.go closes the FIR-3052 gap: the Issue-workflow loop only ever
// advanced from Plan to Build when the issue's status changed to BuildStatus,
// and by design (see loops/compile.go PlanningDispatchRule + spec.go) that flip
// was left to the plan agent doing it by hand. When the agent finished the plan
// but never moved the issue (live test FIR-3337), Build never started.
//
// This is the "step hook" the loop was missing: on plan-task completion the
// engine performs the transition the agent was trusted to do. The plan task
// carries the loop's From/To statuses in its context (stamped in
// dispatchPhaseRunOnIssue); this reads them and dispatches the exact
// TriggerStatusChanged event a manual move would have produced, so
// loop:dispatch-build fires through the normal engine path — same conditions,
// same idempotency — with no new trigger model.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// LoopPhaseAdvancer advances a loop from Plan to Build when the plan run
// completes. It holds the workflow Service so it can reuse Service.Dispatch
// (and the same issue queries the action runner uses). Wired onto
// TaskService.WorkflowLoopAdvancer in router.go; nil-safe at the call site so
// an unwired build is a plain no-op.
type LoopPhaseAdvancer struct {
	svc *Service
}

// NewLoopPhaseAdvancer builds an advancer over the workflow Service.
func NewLoopPhaseAdvancer(svc *Service) *LoopPhaseAdvancer {
	return &LoopPhaseAdvancer{svc: svc}
}

// loopAdvanceContext is the narrow slice of a plan-phase task's context the
// advancer reads. Every other field is ignored — only a plan dispatch carries
// loop_advance_to_status, so any other task no-ops immediately.
type loopAdvanceContext struct {
	LoopPhase             string `json:"loop_phase"`
	WorkflowTargetIssueID string `json:"workflow_target_issue_id"`
	AdvanceFromStatus     string `json:"loop_advance_from_status"`
	AdvanceToStatus       string `json:"loop_advance_to_status"`
}

// AdvanceOnComplete is the seam TaskService.CompleteTask calls once a task
// finishes. No-op for the overwhelming majority of tasks: only a plan-phase
// dispatch carries loop_advance_to_status. Best-effort throughout — a missing
// issue or a write error is logged, never surfaced, and never blocks the
// task-completion transaction (which already committed before this runs).
func (a *LoopPhaseAdvancer) AdvanceOnComplete(ctx context.Context, task db.AgentTaskQueue) {
	if a == nil || a.svc == nil || !a.svc.Enabled() || len(task.Context) == 0 {
		return
	}
	var tc loopAdvanceContext
	if err := json.Unmarshal(task.Context, &tc); err != nil {
		return
	}
	if strings.TrimSpace(tc.LoopPhase) != "plan" {
		return // only the plan phase auto-advances to build.
	}
	toStatus := strings.TrimSpace(tc.AdvanceToStatus)
	fromStatus := strings.TrimSpace(tc.AdvanceFromStatus)
	if toStatus == "" {
		return // not a hook-carrying plan dispatch — nothing to advance to.
	}
	issueID, err := parseUUID(strings.TrimSpace(tc.WorkflowTargetIssueID))
	if err != nil {
		slog.Warn("workflow loop advance: plan completion without target issue",
			"task_id", uuidString(task.ID))
		return
	}
	issue, err := a.svc.issues.GetIssue(ctx, issueID)
	if err != nil {
		slog.Warn("workflow loop advance: load issue failed",
			"issue_id", uuidString(issueID), "error", err)
		return
	}

	// Guard: only advance a plan still sitting on its planning status. If the
	// plan agent already moved it forward, or this fires twice, From won't
	// match and this is a no-op — deterministic, idempotent, and it never
	// overrides a manual move the agent already made.
	if fromStatus != "" && !strings.EqualFold(issue.Status, fromStatus) {
		slog.Info("workflow loop advance: issue not on planning status; skipping",
			"issue_id", uuidString(issueID), "current", issue.Status, "from", fromStatus)
		return
	}
	if strings.EqualFold(issue.Status, toStatus) {
		return // already at build status.
	}

	updated, err := a.svc.issues.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          issue.ID,
		Status:      toStatus,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("workflow loop advance: set build status failed",
			"issue_id", uuidString(issueID), "error", err)
		return
	}

	// Plain-language trail on the shared plan document (Jesper's ask): the loop
	// says, in human words, that it handed the baton from Plan to Build instead
	// of silently looking finished.
	if a.svc.planDocuments != nil {
		if _, _, err := a.svc.planDocuments.AppendIssueStatus(ctx, updated.WorkspaceID, updated.ID,
			"Plan complete. Handing off to Build automatically."); err != nil {
			slog.Warn("workflow loop advance: plan document update failed",
				"issue_id", uuidString(issueID), "error", err)
		}
	}

	// Fire the same status-changed trigger the plan agent's manual move would
	// have produced, so loop:dispatch-build starts Build. Reusing Dispatch runs
	// the engine's idempotency + conditions exactly as usual; the EventID is
	// deterministic per (issue, task, target) so a re-run collapses.
	te := TriggerEvent{
		EventID:     "loop_advance:" + uuidString(issue.ID) + ":" + uuidString(task.ID) + ":" + toStatus,
		WorkspaceID: uuidString(updated.WorkspaceID),
		ProjectID:   uuidString(updated.ProjectID),
		IssueID:     uuidString(updated.ID),
		Type:        TriggerStatusChanged,
		FromStatus:  issue.Status,
		ToStatus:    toStatus,
		Raw: map[string]any{
			"issue": map[string]any{
				"id":         uuidString(updated.ID),
				"status":     toStatus,
				"project_id": uuidString(updated.ProjectID),
			},
		},
	}
	if err := a.svc.Dispatch(ctx, te); err != nil {
		slog.Error("workflow loop advance: dispatch build failed",
			"issue_id", uuidString(issueID), "error", err)
		return
	}
	slog.Info("workflow loop advanced plan -> build",
		"issue_id", uuidString(issueID), "from", issue.Status, "to", toStatus)
}
