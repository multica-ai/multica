package handler

// CEREBRO-PATCH(orchestration-watchdog): FIR-2564 — stall watchdog. Cerebro-only
// file. The event-driven orchestration engine wakes on label-attach and
// child-done; if a sub-issue's run hangs or ends without reaching done, there is
// no event to wake the engine and the whole flow freezes silently — the failure
// mode Jesper hit ("denne fejlede"). This background sweeper periodically scans
// every orchestrated parent, detects a started-but-stuck sub-issue, re-dispatches
// it once, and escalates (a visible Orchestrator comment) if it is still stuck.

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	orchestrateStalledLabelName  = "orch-stalled"
	orchestrateStalledLabelColor = "#f59e0b"

	// defaultWatchdogInterval is how often the stall sweep runs in production.
	defaultWatchdogInterval = 10 * time.Minute
	// defaultStallAfter is how long an in-flight sub-issue may sit with no active
	// run before the watchdog treats it as stalled.
	defaultStallAfter = 20 * time.Minute
)

// OrchestrationWatchdog periodically scans orchestrated parents for stalled
// waves and recovers them. A sub-issue is "stalled" when the engine started it
// (status todo/in_progress/in_review) but it has had no active run for a while
// and has not reached done — i.e. its agent run hung or ended without finishing.
type OrchestrationWatchdog struct {
	h          *Handler
	interval   time.Duration
	stallAfter time.Duration
	stopCh     chan struct{}
	doneCh     chan struct{}
}

// NewOrchestrationWatchdog builds a watchdog with production defaults.
func NewOrchestrationWatchdog(h *Handler) *OrchestrationWatchdog {
	return &OrchestrationWatchdog{
		h:          h,
		interval:   defaultWatchdogInterval,
		stallAfter: defaultStallAfter,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// Run drives the periodic sweep. Intended to be invoked once in its own
// goroutine from main.go; returns after Stop or ctx cancellation.
func (w *OrchestrationWatchdog) Run(ctx context.Context) {
	defer close(w.doneCh)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			w.sweepOnce(ctx, time.Now())
		}
	}
}

// Stop signals Run to exit and blocks until it has.
func (w *OrchestrationWatchdog) Stop() {
	close(w.stopCh)
	<-w.doneCh
}

// sweepOnce scans every orchestrated parent for stalled sub-issues. Exposed
// (unexported) for tests that drive a single sweep without waiting for a tick.
func (w *OrchestrationWatchdog) sweepOnce(ctx context.Context, now time.Time) {
	parents, err := w.h.Queries.ListNonTerminalIssuesWithLabelName(ctx, orchestrateLabelName)
	if err != nil {
		slog.Warn("orchestration watchdog: list parents failed", "error", err)
		return
	}
	for _, parent := range parents {
		if !w.h.isOrchestratedParent(ctx, parent) {
			continue // label gone or not squad-assigned — not eligible
		}
		w.sweepParent(ctx, parent, now)
	}
}

func (w *OrchestrationWatchdog) sweepParent(ctx context.Context, parent db.Issue, now time.Time) {
	children, err := w.h.Queries.ListChildIssues(ctx, parent.ID)
	if err != nil {
		return
	}
	for _, child := range children {
		// Already adjudicated (PASS/FAIL) -> the verdict path owns it. Clear any
		// stale marker and leave it alone.
		if w.h.issueHasLabelNamed(ctx, child, orchestrateVerifiedLabelName) ||
			w.h.issueHasLabelNamed(ctx, child, orchestrateRejectedLabelName) {
			if w.h.issueHasLabelNamed(ctx, child, orchestrateStalledLabelName) {
				w.h.detachWorkspaceLabel(ctx, child, orchestrateStalledLabelName)
			}
			// CEREBRO-PATCH(orchestration-status-reconcile): FIR-2564 — status must
			// match reality: a VERIFIED sub-issue is complete, so it must be `done`.
			// (A run can bump a verified child back to in_progress/in_review — e.g.
			// a paused/rate-limited agent — leaving it stuck in a non-done status.)
			if w.h.issueHasLabelNamed(ctx, child, orchestrateVerifiedLabelName) &&
				child.Status != "done" && child.Status != "cancelled" {
				if updated, err := w.h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
					ID:          child.ID,
					Status:      "done",
					WorkspaceID: child.WorkspaceID,
				}); err == nil {
					w.h.publishOrchestratedStatus(ctx, updated)
				}
			}
			continue
		}
		switch child.Status {
		case "in_review":
			// CEREBRO-PATCH(orchestration-thread): FIR-2564 — in_review = worker
			// finished, awaiting verification; nudge it, don't treat it as stalled.
			// The worker finished into in_review (not done), so the verification
			// gate never fired. Nudge verification rather than treat it as stalled.
			if active, err := w.h.Queries.HasActiveTaskForIssue(ctx, child.ID); err == nil && !active {
				w.h.requestChildVerification(ctx, parent, child)
			}
		case "todo", "in_progress":
			w.checkStalledChild(ctx, parent, child, now)
		default:
			// Backlog/terminal children are not "started + stuck"; clear any
			// stale marker so a future stall is flagged fresh.
			if w.h.issueHasLabelNamed(ctx, child, orchestrateStalledLabelName) {
				w.h.detachWorkspaceLabel(ctx, child, orchestrateStalledLabelName)
			}
		}
	}
}

// checkStalledChild flags + recovers a todo/in_progress sub-issue whose run has
// gone stale with no active task.
func (w *OrchestrationWatchdog) checkStalledChild(ctx context.Context, parent, child db.Issue, now time.Time) {
	// Still within the grace window since its last change -> a normal run is
	// likely in progress; leave it alone.
	if child.UpdatedAt.Valid && now.Sub(child.UpdatedAt.Time) < w.stallAfter {
		return
	}
	active, err := w.h.Queries.HasActiveTaskForIssue(ctx, child.ID)
	if err != nil {
		return
	}
	if active {
		// A run is active -> not stalled; clear any prior marker.
		if w.h.issueHasLabelNamed(ctx, child, orchestrateStalledLabelName) {
			w.h.detachWorkspaceLabel(ctx, child, orchestrateStalledLabelName)
		}
		return
	}

	ref := "#" + strconv.Itoa(int(child.Number))
	if w.h.issueHasLabelNamed(ctx, child, orchestrateStalledLabelName) {
		// Already re-dispatched once and still stuck -> escalate, don't loop.
		// (Comment dedup keeps this from repeating every sweep.)
		w.h.postOrchestrationComment(ctx, parent,
			"Sub-issue "+ref+" is still stalled after an automatic re-dispatch — its run keeps "+
				"ending without finishing. This needs attention (check the agent/runtime or the "+
				"sub-issue's blockers). The orchestrator will not keep re-dispatching it.")
		return
	}

	// First detection -> mark stalled + re-dispatch the worker once.
	if id := w.h.ensureWorkspaceLabel(ctx, parent.WorkspaceID, orchestrateStalledLabelName, orchestrateStalledLabelColor); id.Valid {
		_ = w.h.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
			IssueID:     child.ID,
			LabelID:     id,
			WorkspaceID: child.WorkspaceID,
		})
	}
	redispatched := w.redispatchChild(ctx, child)
	if redispatched {
		w.h.postOrchestrationComment(ctx, parent,
			"Sub-issue "+ref+" looked stalled (started, but no run has been active and it has not "+
				"finished). Re-dispatched its worker automatically.")
	} else {
		w.h.postOrchestrationComment(ctx, parent,
			"Sub-issue "+ref+" looked stalled and could not be re-dispatched (no ready agent/squad "+
				"leader). This needs attention.")
	}
}

// redispatchChild re-enqueues the worker for an in-flight sub-issue without
// changing its status. Squad-assigned -> wake the squad leader; agent-assigned
// -> enqueue the agent. Guarded so a child that already has a pending run is
// left alone.
func (w *OrchestrationWatchdog) redispatchChild(ctx context.Context, child db.Issue) bool {
	if child.AssigneeType.Valid && child.AssigneeType.String == "squad" {
		if !w.h.isSquadLeaderReady(ctx, child) {
			return false
		}
		// CEREBRO-PATCH(orchestration-handoff-provenance): FIR-2564 — carry human
		// origin so the re-dispatched leader can still wake a verifier/QA agent.
		w.h.enqueueOrchestrationLeaderTask(ctx, child, pgtype.UUID{})
		return true
	}
	if child.AssigneeType.Valid && child.AssigneeType.String == "agent" && child.AssigneeID.Valid {
		if !w.h.isAgentAssigneeReady(ctx, child) || w.h.hasPendingTask(ctx, child.ID, child.AssigneeID) {
			return false
		}
		if _, err := w.h.TaskService.EnqueueTaskForIssue(ctx, child); err != nil {
			slog.Warn("orchestration watchdog: redispatch enqueue failed", "child_id", uuidToString(child.ID), "error", err)
			return false
		}
		return true
	}
	return false
}
