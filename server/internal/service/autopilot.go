package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issueposition"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TxStarter abstracts transaction creation (satisfied by pgxpool.Pool).
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type AutopilotService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Bus       *events.Bus
	TaskSvc   *TaskService
}

// DefaultAutopilotTriggerTimezone is the timezone used to render Autopilot
// trigger output when a trigger has no configured timezone or the configured
// timezone fails to load. Exported so the scheduler can use the same default
// when computing next run times.
const DefaultAutopilotTriggerTimezone = "UTC"

func NewAutopilotService(q *db.Queries, tx TxStarter, bus *events.Bus, taskSvc *TaskService) *AutopilotService {
	return &AutopilotService{Queries: q, TxStarter: tx, Bus: bus, TaskSvc: taskSvc}
}

// DispatchAutopilot is the core execution entry point.
// It creates a run and either creates an issue or enqueues a direct agent task
// depending on execution_mode.
//
// Before any work is queued we run an admission check against the assignee
// agent's runtime. Two skip outcomes are possible (MUL-1899 + MUL-2863):
//
//  1. The runtime is offline. MUL-2863 makes this a *durable* skip: we
//     persist a `pending_runtime` run with the runtime_id it was waiting
//     behind, so when the runtime/daemon reappears the
//     DispatchPendingRuntimeRunsForRuntime hook picks it up and dispatches
//     the actual work. This is the "the cron should not be silently lost
//     while my laptop is asleep" fix from MUL-2863.
//
//  2. Any other admission failure (agent archived, squad archived, no
//     runtime bound, private-agent gate, etc.) is a *terminal* skip. The
//     cron is recorded as `skipped` with a failure_reason, mirroring the
//     legacy MUL-1899 behaviour. Retrying wouldn't change anything, and
//     retrying would inflate the failure-rate auto-pause monitor.
//
// When assignee_type='squad' the gate runs against the squad leader (Path A
// from MUL-2429: Autopilot-on-squad ≈ Autopilot-on-leader), so an offline or
// archived leader produces the same skip behaviour as an offline solo agent.
func (s *AutopilotService) DispatchAutopilot(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
) (*db.AutopilotRun, error) {
	if reason, skip := s.shouldSkipDispatch(ctx, autopilot); skip {
		if isRuntimeOfflineAdmissionReason(reason) {
			if run, err := s.recordPendingRuntimeRun(ctx, autopilot, triggerID, source, payload, reason); err == nil {
				return run, nil
			} else {
				// Falling back to a terminal skip keeps a DB error from
				// silently dropping the cron: the run row still gets
				// written, the failure reason still surfaces in the UI,
				// and the next tick gets another chance.
				slog.Warn("autopilot dispatch: pending_runtime insert failed, falling back to skipped",
					"autopilot_id", util.UUIDToString(autopilot.ID),
					"error", err,
				)
			}
		}
		return s.recordSkippedRun(ctx, autopilot, triggerID, source, payload, reason)
	}

	// Determine initial status based on execution mode.
	initialStatus := "issue_created"
	if autopilot.ExecutionMode == "run_only" {
		initialStatus = "running"
	}

	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:    autopilot.ID,
		TriggerID:      triggerID,
		Source:         source,
		Status:         initialStatus,
		TriggerPayload: payload,
		SquadID:        autopilotSquadAttribution(autopilot),
	})
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	s.captureAutopilotRunStarted(autopilot, run, source)

	switch autopilot.ExecutionMode {
	case "create_issue":
		triggerTimezone := s.resolveAutopilotTriggerTimezone(ctx, triggerID)
		if err := s.dispatchCreateIssue(ctx, autopilot, &run, triggerTimezone); err != nil {
			if skipped := s.handleDispatchSkip(ctx, autopilot, &run, err); skipped != nil {
				return skipped, nil
			}
			s.failRun(ctx, run.ID, err.Error())
			s.captureAutopilotRunFailed(autopilot, run, source, err.Error())
			return &run, fmt.Errorf("dispatch create_issue: %w", err)
		}
	case "run_only":
		if err := s.dispatchRunOnly(ctx, autopilot, &run); err != nil {
			if skipped := s.handleDispatchSkip(ctx, autopilot, &run, err); skipped != nil {
				return skipped, nil
			}
			s.failRun(ctx, run.ID, err.Error())
			s.captureAutopilotRunFailed(autopilot, run, source, err.Error())
			return &run, fmt.Errorf("dispatch run_only: %w", err)
		}
	default:
		s.failRun(ctx, run.ID, "unknown execution_mode: "+autopilot.ExecutionMode)
		s.captureAutopilotRunFailed(autopilot, run, source, "unknown execution_mode: "+autopilot.ExecutionMode)
		return &run, fmt.Errorf("unknown execution_mode: %s", autopilot.ExecutionMode)
	}

	// Update last_run_at on the autopilot.
	s.Queries.UpdateAutopilotLastRunAt(ctx, autopilot.ID)

	// Publish run start event.
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAutopilotRunStart,
		WorkspaceID: util.UUIDToString(autopilot.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"run_id":       util.UUIDToString(run.ID),
			"autopilot_id": util.UUIDToString(autopilot.ID),
			"source":       source,
			"status":       run.Status,
		},
	})

	return &run, nil
}

// dispatchCreateIssue creates an issue and enqueues a task for the agent.
//
// When the autopilot is assigned to a squad (Path A from MUL-2429), the
// created issue inherits assignee_type='squad' + assignee_id=squad. The
// existing issue listener chain (shouldEnqueueSquadLeaderOnAssign →
// enqueueSquadLeaderTask) then routes the work to the squad leader, exactly
// as a human manually assigning the issue to that squad would.
//
// Creator on the issue is always the agent that will actually do the work
// (the resolved leader for a squad autopilot, otherwise the assignee agent
// itself), so activity / mentions render with the right author identity.
func (s *AutopilotService) dispatchCreateIssue(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun, triggerTimezone string) error {
	leader, _, err := s.resolveAutopilotLeader(ctx, ap)
	if err != nil {
		return fmt.Errorf("resolve leader: %w", err)
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	title := s.interpolateTemplate(ap, *run, triggerTimezone)
	description := s.buildIssueDescription(ap, *run, triggerTimezone)

	issueNumber, err := qtx.IncrementIssueCounter(ctx, ap.WorkspaceID)
	if err != nil {
		return fmt.Errorf("increment issue counter: %w", err)
	}

	newPosition, err := issueposition.NextTopPosition(ctx, tx, ap.WorkspaceID, "todo")
	if err != nil {
		return fmt.Errorf("get next issue position: %w", err)
	}

	issue, err := qtx.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		WorkspaceID:  ap.WorkspaceID,
		Title:        title,
		Description:  description,
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: ap.AssigneeType, Valid: true},
		AssigneeID:   ap.AssigneeID,
		// The agent that the autopilot dispatches to is the issue's creator,
		// not the human who originally configured the autopilot. The latter
		// is captured separately via origin_type=autopilot + origin_id. For
		// squad-assigned autopilots, the creator is the resolved leader —
		// the same agent the issue listener will end up enqueueing.
		CreatorType:   "agent",
		CreatorID:     leader.ID,
		ParentIssueID: pgtype.UUID{},
		Position:      newPosition,
		StartDate:     pgtype.Date{},
		DueDate:       pgtype.Date{},
		Number:        issueNumber,
		ProjectID:     ap.ProjectID,
		OriginType:    pgtype.Text{String: "autopilot", Valid: true},
		OriginID:      ap.ID,
	})
	if err != nil {
		return fmt.Errorf("create issue: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	// Update run with the linked issue.
	updatedRun, err := s.Queries.UpdateAutopilotRunIssueCreated(ctx, db.UpdateAutopilotRunIssueCreatedParams{
		ID:      run.ID,
		IssueID: issue.ID,
	})
	if err != nil {
		return fmt.Errorf("link run to issue: %w", err)
	}
	*run = updatedRun

	// Publish issue:created so the existing event chain fires
	// (subscriber listeners, activity listeners, notification listeners). For
	// squad autopilots, this is what triggers shouldEnqueueSquadLeaderOnAssign
	// → enqueueSquadLeaderTask — no separate squad-routing code needed here.
	prefix := s.getIssuePrefix(ap.WorkspaceID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: util.UUIDToString(ap.WorkspaceID),
		ActorType:   "agent",
		ActorID:     util.UUIDToString(leader.ID),
		Payload: map[string]any{
			"issue": issueToMap(issue, prefix),
		},
	})
	s.captureIssueCreatedFromAutopilot(ap, run, issue, leader.ID)

	// Enqueue agent task via the existing flow. Squad-assigned autopilots
	// route to the resolved leader as the executing agent (Path A from
	// MUL-2429); agent-assigned autopilots go through the standard issue
	// path. Both code paths land in agent_task_queue with agent_id = leader.
	if ap.AssigneeType == "squad" {
		// Fail-closed private-leader gate: if the leader is private, verify
		// the autopilot creator still has access. This catches illegitimate
		// configs that were saved before the save-time gate was added.
		if leader.Visibility == "private" && !s.canCreatorAccessPrivateLeader(ctx, ap, leader) {
			return fmt.Errorf("autopilot creator cannot access private squad leader")
		}
		if _, err := s.TaskSvc.EnqueueTaskForSquadLeader(ctx, issue, leader.ID, pgtype.UUID{}); err != nil {
			return fmt.Errorf("enqueue squad leader task: %w", err)
		}
	} else {
		if _, err := s.TaskSvc.EnqueueTaskForIssue(ctx, issue); err != nil {
			return fmt.Errorf("enqueue task for issue: %w", err)
		}
	}

	slog.Info("autopilot dispatched (create_issue)",
		"autopilot_id", util.UUIDToString(ap.ID),
		"assignee_type", ap.AssigneeType,
		"issue_id", util.UUIDToString(issue.ID),
		"leader_id", util.UUIDToString(leader.ID),
		"run_id", util.UUIDToString(run.ID),
	)
	return nil
}

// errDispatchSkipped wraps a readiness failure encountered after the
// admission gate has already passed. dispatchRunOnly returns this when a
// resolved leader has gone offline / been archived between admission and
// task creation; DispatchAutopilot recognises it and records a `skipped`
// run (with the wrapped reason) instead of a `failed` run.
//
// Without the sentinel, the existing failRun path would mark these races as
// failures and bubble a 500 out of the manual-trigger handler — both wrong
// (the work was never attempted, no one is at fault) and noisy (the failure
// monitor would auto-pause autopilots whose only crime was a flaky runtime).
type errDispatchSkipped struct {
	reason string
}

func (e *errDispatchSkipped) Error() string { return e.reason }

// dispatchRunOnly enqueues a direct agent task without creating an issue.
//
// For squad autopilots, the executing agent is the squad leader resolved at
// trigger time (Path A from MUL-2429). The same archived / runtime-bound /
// runtime-online gates that the upstream admission check (shouldSkipDispatch)
// applies also run here as belt-and-braces: if the leader changed between
// admission and dispatch, or the runtime went offline in the gap, we still
// fail closed instead of enqueueing a doomed task.
func (s *AutopilotService) dispatchRunOnly(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun) error {
	agent, _, err := s.resolveAutopilotLeader(ctx, ap)
	if err != nil {
		// Same admission-vs-failure classification as shouldSkipDispatch:
		// if the row disappeared or the squad was archived between
		// admission and dispatch, that is a skip, not a failure.
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errSquadArchived) {
			return &errDispatchSkipped{reason: formatAdmissionReason(ap, "assignee no longer resolvable")}
		}
		return fmt.Errorf("resolve leader: %w", err)
	}
	ready, reason, err := AgentReadiness(ctx, s.Queries, agent)
	if err != nil {
		return fmt.Errorf("check agent readiness: %w", err)
	}
	if !ready {
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, reason)}
	}

	// Fail-closed private-leader gate for squad autopilots.
	if ap.AssigneeType == "squad" && agent.Visibility == "private" && !s.canCreatorAccessPrivateLeader(ctx, ap, agent) {
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, "creator cannot access private squad leader")}
	}

	task, err := s.Queries.CreateAutopilotTask(ctx, db.CreateAutopilotTaskParams{
		AgentID:        agent.ID,
		RuntimeID:      agent.RuntimeID,
		Priority:       0,
		AutopilotRunID: run.ID,
		// Snapshot the autopilot title so task rows self-describe later
		// without joining back to autopilot. Truncated for the same
		// transmission-cost reason as comment-driven summaries.
		TriggerSummary: pgtype.Text{
			String: truncateForSummary(ap.Title, triggerSummaryMaxLen),
			Valid:  ap.Title != "",
		},
	})
	if err != nil {
		return fmt.Errorf("create autopilot task: %w", err)
	}

	// Update run with task reference.
	updatedRun, err := s.Queries.UpdateAutopilotRunRunning(ctx, db.UpdateAutopilotRunRunningParams{
		ID:     run.ID,
		TaskID: task.ID,
	})
	if err != nil {
		slog.Warn("failed to update run with task_id", "run_id", util.UUIDToString(run.ID), "error", err)
	} else {
		*run = updatedRun
	}

	// Drop the empty-claim cache and wake the daemon. dispatchRunOnly
	// inserts the task row directly via Queries.CreateAutopilotTask
	// (bypassing TaskService.Enqueue*), so without this the runtime
	// would not get a wakeup and any cached "empty" verdict would
	// stall the task until the TTL expired.
	s.TaskSvc.NotifyTaskEnqueued(ctx, task)

	slog.Info("autopilot dispatched (run_only)",
		"autopilot_id", util.UUIDToString(ap.ID),
		"task_id", util.UUIDToString(task.ID),
		"run_id", util.UUIDToString(run.ID),
	)
	return nil
}

// SyncRunFromIssue updates the autopilot run when its linked issue reaches a terminal status.
func (s *AutopilotService) SyncRunFromIssue(ctx context.Context, issue db.Issue) {
	if !issue.OriginType.Valid || issue.OriginType.String != "autopilot" {
		return
	}

	run, err := s.Queries.GetAutopilotRunByIssue(ctx, issue.ID)
	if err != nil {
		return // no active run linked to this issue
	}
	autopilot, err := s.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		return
	}

	wsID := util.UUIDToString(issue.WorkspaceID)

	switch issue.Status {
	case "done", "in_review":
		updatedRun, err := s.Queries.UpdateAutopilotRunCompleted(ctx, db.UpdateAutopilotRunCompletedParams{
			ID: run.ID,
		})
		if err != nil {
			slog.Warn("failed to complete autopilot run", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.captureAutopilotRunCompleted(autopilot, updatedRun)
		s.publishRunDone(wsID, updatedRun, "completed")
	case "cancelled", "blocked":
		reason := "issue " + issue.Status
		updatedRun, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
			ID:            run.ID,
			FailureReason: pgtype.Text{String: reason, Valid: true},
		})
		if err != nil {
			slog.Warn("failed to fail autopilot run", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.captureAutopilotRunFailed(autopilot, updatedRun, updatedRun.Source, reason)
		s.publishRunDone(wsID, updatedRun, "failed")
	}
}

// SyncRunFromTask updates the autopilot run when a run_only task completes or fails.
func (s *AutopilotService) SyncRunFromTask(ctx context.Context, task db.AgentTaskQueue) {
	if !task.AutopilotRunID.Valid {
		return
	}

	run, err := s.Queries.GetAutopilotRun(ctx, task.AutopilotRunID)
	if err != nil {
		return
	}

	autopilot, err := s.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		return
	}
	wsID := util.UUIDToString(autopilot.WorkspaceID)

	switch task.Status {
	case "completed":
		updatedRun, err := s.Queries.UpdateAutopilotRunCompleted(ctx, db.UpdateAutopilotRunCompletedParams{
			ID:     run.ID,
			Result: task.Result,
		})
		if err != nil {
			slog.Warn("failed to complete autopilot run from task", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.captureAutopilotRunCompleted(autopilot, updatedRun)
		s.publishRunDone(wsID, updatedRun, "completed")
	case "failed", "cancelled":
		reason := "task " + task.Status
		if task.Error.Valid {
			reason = task.Error.String
		}
		updatedRun, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
			ID:            run.ID,
			FailureReason: pgtype.Text{String: reason, Valid: true},
		})
		if err != nil {
			slog.Warn("failed to fail autopilot run from task", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.captureAutopilotRunFailed(autopilot, updatedRun, updatedRun.Source, reason)
		s.publishRunDone(wsID, updatedRun, "failed")
	}
}

// handleDispatchSkip recognises an errDispatchSkipped returned from a
// dispatch function and rewrites the in-flight run to `skipped` (instead of
// `failed`). Returns the updated run on a real skip, nil otherwise — callers
// fall through to the failure path on nil.
//
// Lives here, not inside dispatchRunOnly, because the run row was created by
// DispatchAutopilot up the stack and the failure-vs-skip distinction is
// owned by the dispatcher entry point. Keeps dispatchRunOnly free of
// state-mutation helpers.
func (s *AutopilotService) handleDispatchSkip(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun, err error) *db.AutopilotRun {
	var skipErr *errDispatchSkipped
	if !errors.As(err, &skipErr) {
		return nil
	}
	updated, uerr := s.Queries.UpdateAutopilotRunSkipped(ctx, db.UpdateAutopilotRunSkippedParams{
		ID:            run.ID,
		FailureReason: pgtype.Text{String: skipErr.reason, Valid: true},
	})
	if uerr != nil {
		slog.Warn("failed to mark dispatch as skipped",
			"run_id", util.UUIDToString(run.ID), "error", uerr)
		// Leave the run in its current (running/issue_created) state if
		// the update failed; the failure monitor will eventually fail it
		// out, but at least we didn't pretend it succeeded.
		return nil
	}
	*run = updated
	slog.Info("autopilot dispatch skipped post-admission",
		"autopilot_id", util.UUIDToString(ap.ID),
		"run_id", util.UUIDToString(run.ID),
		"reason", skipErr.reason,
	)
	// Bump last_run_at on parity with recordSkippedRun (pre-flight skip) and
	// the success path: from the scheduler's / UI's point of view we did
	// evaluate the trigger this tick, even though the post-admission gate
	// caught a late readiness regression.
	s.Queries.UpdateAutopilotLastRunAt(ctx, ap.ID)
	s.publishRunDone(util.UUIDToString(ap.WorkspaceID), updated, "skipped")
	return run
}

func (s *AutopilotService) failRun(ctx context.Context, runID pgtype.UUID, reason string) {
	if _, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
		ID:            runID,
		FailureReason: pgtype.Text{String: reason, Valid: true},
	}); err != nil {
		slog.Warn("failed to mark autopilot run as failed", "run_id", util.UUIDToString(runID), "error", err)
	}
}

// shouldSkipDispatch is the pre-flight admission check from MUL-1899.
// Returns (reason, true) when dispatching now would only enqueue a doomed
// task — i.e. the assignee (or, for squad autopilots, the squad leader) is
// gone, archived, has no runtime bound, or its runtime is not currently
// online. Returns ("", false) on the happy path.
//
// Errors are split into two classes:
//   - pgx.ErrNoRows / errSquadArchived (the row truly doesn't exist or is
//     archived) → hard skip. Retrying won't change anything; piling failed
//     runs would pollute the failure-rate auto-pause monitor.
//   - Anything else (connection drop, statement timeout, etc.) → fail-open:
//     log + do not skip, so a transient DB hiccup never silently swallows a
//     scheduled run. Migration 096 removed the agent FK on autopilot, so an
//     agent assignee being missing is now a real condition the gate must
//     handle (previously cascade-deleted).
func (s *AutopilotService) shouldSkipDispatch(ctx context.Context, ap db.Autopilot) (string, bool) {
	if !ap.AssigneeID.Valid {
		return "autopilot has no assignee", true
	}
	agent, squadResolved, err := s.resolveAutopilotLeader(ctx, ap)
	if err != nil {
		// Hard-skip the cases where another retry will produce the same
		// outcome. Logging is unconditional so ops can still spot a run of
		// dangling rows pointing at a deleted agent / archived squad.
		missing := errors.Is(err, pgx.ErrNoRows)
		archived := errors.Is(err, errSquadArchived)
		slog.Warn("autopilot admission: failed to resolve leader",
			"autopilot_id", util.UUIDToString(ap.ID),
			"assignee_type", ap.AssigneeType,
			"assignee_id", util.UUIDToString(ap.AssigneeID),
			"missing", missing,
			"archived", archived,
			"error", err,
		)
		switch {
		case archived:
			// Squad row exists but is archived — DeleteSquad's transfer
			// should have rewritten this autopilot's assignee to the leader
			// already; surfacing the case explicitly keeps the failure
			// reason useful when something slipped past the transfer.
			return "assignee squad is archived", true
		case missing && squadResolved:
			return "assignee squad cannot be resolved", true
		case missing && !squadResolved:
			// Agent row gone. With migration 096 the FK is gone too, so
			// this is the new "agent was hard-deleted under us" case. Skip
			// rather than fail-open: we know retrying will not help.
			return "assignee agent no longer exists", true
		}
		// Transient DB error — fail-open so the next scheduler tick gets a
		// chance to succeed.
		return "", false
	}
	ready, reason, err := AgentReadiness(ctx, s.Queries, agent)
	if err != nil {
		slog.Warn("autopilot admission: failed to load runtime",
			"autopilot_id", util.UUIDToString(ap.ID),
			"runtime_id", util.UUIDToString(agent.RuntimeID),
			"error", err,
		)
		return "", false
	}
	if !ready {
		return formatAdmissionReason(ap, reason), true
	}
	// Private-agent gate at the autopilot layer. Caller identity = the
	// autopilot's creator: if the creator no longer has access to the
	// (now-private) target agent, the dispatch is recorded as `skipped`.
	// Agent-created autopilots bypass the gate to preserve A2A
	// collaboration. Errors loading the workspace member fail closed —
	// without an authoritative role the gate cannot grant access.
	//
	// For squad autopilots the gate runs against the resolved leader.
	// Leader visibility is the right thing to check — if the human creator
	// can no longer reach the leader, the autopilot would silently fail
	// even though the squad itself looks intact.
	if agent.Visibility == "private" && ap.CreatedByType == "member" {
		creatorID := util.UUIDToString(ap.CreatedByID)
		if util.UUIDToString(agent.OwnerID) != creatorID {
			member, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID:      ap.CreatedByID,
				WorkspaceID: ap.WorkspaceID,
			})
			if err != nil {
				return "autopilot creator no longer in workspace", true
			}
			if member.Role != "owner" && member.Role != "admin" {
				return "autopilot creator lacks access to private assignee agent", true
			}
		}
	}
	return "", false
}

// formatAdmissionReason rewrites the generic AgentReadiness reason into the
// admission-gate phrasing the failure monitor and existing alerting are tuned
// for. Keeping the prefix stable matters: dashboards group skip reasons by
// substring ("offline at dispatch time" is how the MUL-1899 alert fires).
//
// For squad autopilots the message names the squad so an operator looking at
// the failure_reason field knows which squad's leader is down without
// joining back to autopilot_run.squad_id.
func formatAdmissionReason(ap db.Autopilot, raw string) string {
	prefix := "assignee "
	if ap.AssigneeType == "squad" {
		prefix = "squad leader "
	}
	switch raw {
	case "agent is archived":
		return prefix + "agent is archived"
	case "agent has no runtime bound":
		return prefix + "agent has no runtime bound"
	default:
		// raw is "agent runtime is X" — surface the runtime status while
		// preserving the legacy "at dispatch time" suffix from MUL-1899
		// so alert queries do not need to change.
		return raw + " at dispatch time"
	}
}

// errSquadArchived signals that an autopilot's squad assignee has been
// archived. Distinct from a missing/loadable-but-failed squad so the
// admission gate can phrase the skip reason precisely and the failure
// monitor does not see "cannot be resolved" wear noise for what is a
// known, expected post-archive condition.
var errSquadArchived = errors.New("squad is archived")

// resolveAutopilotLeader returns the agent that will actually execute the
// autopilot's work. For assignee_type='agent' the agent is the assignee
// itself; for assignee_type='squad' it is the squad's leader_id. The second
// return is true when the resolver took the squad branch — callers use this
// to distinguish "failed loading an agent" from "failed loading a squad", so
// the admission gate can choose between fail-open (transient DB error on a
// known-good agent) and fail-closed (squad row gone, no point retrying).
//
// Archived squads are rejected here too: TransferSquadAutopilotsToLeader
// flips surviving autopilots to assignee_type='agent' on DeleteSquad, but
// the gate still has to fail closed for any row that slips through that
// transfer (e.g. squad archived through a code path that bypasses the
// handler) so an archived squad never produces work.
//
// Unknown assignee_type values return an error. assignee_type is gated by a
// CHECK constraint at the DB layer, so this only fires if a future code path
// inserts a row that bypasses the check.
func (s *AutopilotService) resolveAutopilotLeader(ctx context.Context, ap db.Autopilot) (agent db.Agent, squadResolved bool, err error) {
	switch ap.AssigneeType {
	case "", "agent":
		agent, err = s.Queries.GetAgent(ctx, ap.AssigneeID)
		return agent, false, err
	case "squad":
		squad, err := s.Queries.GetSquad(ctx, ap.AssigneeID)
		if err != nil {
			return db.Agent{}, true, fmt.Errorf("load squad: %w", err)
		}
		if squad.ArchivedAt.Valid {
			return db.Agent{}, true, errSquadArchived
		}
		agent, err = s.Queries.GetAgent(ctx, squad.LeaderID)
		if err != nil {
			return db.Agent{}, true, fmt.Errorf("load squad leader: %w", err)
		}
		return agent, true, nil
	default:
		return db.Agent{}, false, fmt.Errorf("unknown assignee_type %q", ap.AssigneeType)
	}
}

// autopilotSquadAttribution returns the squad_id attribution hook for an
// autopilot_run row. Only populated when assignee_type='squad'. First-version
// reports do not consume this; it exists so a future squad-cost view does not
// need to backfill — see RFC §4.e (MUL-2429).
func autopilotSquadAttribution(ap db.Autopilot) pgtype.UUID {
	if ap.AssigneeType == "squad" && ap.AssigneeID.Valid {
		return ap.AssigneeID
	}
	return pgtype.UUID{}
}

// isRuntimeOfflineAdmissionReason reports whether a shouldSkipDispatch
// failure_reason indicates "the assignee agent's runtime is offline". This
// is the only admission outcome that is durable (MUL-2863): we persist the
// run as 'pending_runtime' so the runtime-comes-online hook can re-dispatch
// it. All other admission failures (agent archived, squad archived, no
// runtime bound, private-agent gate failure) are terminal — retrying the
// cron would not change anything, so we keep the legacy 'skipped' status.
//
// We match on the formatted reason rather than re-running AgentReadiness
// here: the admission gate has already loaded the runtime row, and the
// "at dispatch time" suffix is the load-bearing string the MUL-1899
// failure-monitor alert already groups on. Adding a second reason-string
// would silently break that alert query.
func isRuntimeOfflineAdmissionReason(reason string) bool {
	return strings.HasPrefix(reason, "agent runtime is ") ||
		strings.HasPrefix(reason, "assignee agent runtime is ") ||
		strings.HasPrefix(reason, "squad leader agent runtime is ")
}

// recordPendingRuntimeRun persists a 'pending_runtime' autopilot_run for the
// MUL-2863 durable-dispatch path. Mirrors recordSkippedRun's contract
// (returns run + nil error so callers treat this as a successful no-op
// dispatch), but:
//   - status='pending_runtime' instead of 'skipped'
//   - pending_runtime_id is set to the runtime we're queued behind, so the
//     runtime-comes-online hook can find it
//   - failure_reason is the admission-gate text (e.g. "agent runtime is
//     offline at dispatch time") so the UI's existing failure_reason field
//     can surface "Waiting for agent runtime to come online" without a
//     schema change. The mapping from "offline at dispatch time" →
//     "Waiting for agent runtime" lives in the frontend
//     (AutopilotRunStatus handler) so the wire contract stays text-only.
//
// Returns nil run + err on a DB error so the caller can fall back to
// recordSkippedRun; we deliberately do not propagate a partial state
// (pending row without a queued runtime id) because that would create the
// exact "orphaned pending row" the durable path is meant to prevent.
func (s *AutopilotService) recordPendingRuntimeRun(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
	reason string,
) (*db.AutopilotRun, error) {
	runtimeID, err := s.resolveAssigneeRuntimeID(ctx, autopilot)
	if err != nil || !runtimeID.Valid {
		// Either the assignee agent row is gone, the squad row is archived,
		// or the agent has no runtime bound. The admission gate already
		// classified this as a non-runtime-offline skip, so the caller
		// would have routed to recordSkippedRun — we should never reach
		// here. If we do (e.g. a race between the gate and this lookup),
		// bail with no row so the caller falls back to the legacy skip.
		slog.Warn("autopilot dispatch: cannot resolve runtime for pending run",
			"autopilot_id", util.UUIDToString(autopilot.ID),
			"reason", reason,
			"err", err,
		)
		return nil, fmt.Errorf("resolve runtime: %w", err)
	}

	run, err := s.Queries.CreateAutopilotRunPendingRuntime(ctx, db.CreateAutopilotRunPendingRuntimeParams{
		AutopilotID:      autopilot.ID,
		Source:           source,
		FailureReason:    pgtype.Text{String: reason, Valid: true},
		PendingRuntimeID: runtimeID,
		TriggerID:        triggerID,
		TriggerPayload:   payload,
		SquadID:          autopilotSquadAttribution(autopilot),
	})
	if err != nil {
		return nil, fmt.Errorf("create pending_runtime run: %w", err)
	}

	// Mirror recordSkippedRun's last_run_at bump + WS event. From the
	// scheduler's / UI's point of view we *did* evaluate the trigger this
	// tick; the runtime was just offline, so we parked the run for later.
	s.Queries.UpdateAutopilotLastRunAt(ctx, autopilot.ID)

	slog.Info("autopilot dispatch parked pending runtime",
		"autopilot_id", util.UUIDToString(autopilot.ID),
		"run_id", util.UUIDToString(run.ID),
		"source", source,
		"runtime_id", util.UUIDToString(runtimeID),
		"reason", reason,
	)

	// Publish the same run-done event the skipped path publishes, but with
	// status='pending_runtime' so the frontend renders the new "Waiting
	// for runtime" pill without a new event type.
	s.publishRunDone(util.UUIDToString(autopilot.WorkspaceID), run, "pending_runtime")
	return &run, nil
}

// resolveAssigneeRuntimeID returns the runtime_id bound to the autopilot's
// resolved leader (the agent for assignee_type='agent', the squad's leader
// for assignee_type='squad'). Returns an invalid UUID when the agent has
// no runtime bound so the caller can decide what to do.
func (s *AutopilotService) resolveAssigneeRuntimeID(ctx context.Context, ap db.Autopilot) (pgtype.UUID, error) {
	agent, _, err := s.resolveAutopilotLeader(ctx, ap)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return agent.RuntimeID, nil
}

// maxPendingRuntimeRunsPerSweep caps the number of pending_runtime runs
// one runtime-online sweep dispatches in a single transaction. Keeps the
// heartbeat hot path bounded when a long-offline laptop comes back with
// hundreds of queued cron runs: dispatch the oldest, leave the rest
// pending for the next beat (or, more likely, the immediate next iteration
// of the loop after this batch commits).
const maxPendingRuntimeRunsPerSweep = 50

// DispatchPendingRuntimeRunsForRuntime is the runtime-comes-online hook
// for MUL-2863. It walks every 'pending_runtime' autopilot_run queued
// behind the given runtime_id and tries to dispatch each one, oldest first.
//
// Per-run dispatch mirrors DispatchAutopilot: re-validate the admission
// gate (in case the agent was archived, the squad was archived, or the
// agent was re-bound to a different runtime while we were waiting), then
// run the standard create_issue / run_only branch. A re-check is necessary
// because the run may have been parked for hours/days; the world may have
// moved on.
//
// The 3-outcome contract from DispatchAutopilot is preserved:
//
//   - success → run is moved to 'issue_created' or 'running' and the
//     autopilot_run.issue_id / task_id is set by the per-mode dispatcher.
//   - admission still failing for the same runtime-offline reason →
//     keep the run as 'pending_runtime' (no row changes). The next time
//     the runtime goes offline → online, we'll try again. (Defensive
//     in case the runtime went online briefly and the agent hasn't
//     heartbeated yet.)
//   - admission failing for a non-runtime reason (agent archived, etc.) →
//     rewind the run to 'skipped' with the new failure_reason so it
//     doesn't sit in pending forever. We treat this as a terminal skip
//     rather than a failure so it doesn't pollute the failure-rate auto-
//     pause monitor.
//
// The function is safe to call from heartbeat hot paths: it short-circuits
// on an empty pending set, and the per-row queries are bounded by
// maxPendingRuntimeRunsPerSweep.
func (s *AutopilotService) DispatchPendingRuntimeRunsForRuntime(ctx context.Context, runtimeID pgtype.UUID) error {
	if !runtimeID.Valid {
		return nil
	}
	rows, err := s.Queries.ListPendingRuntimeAutopilotRunsForRuntime(ctx, db.ListPendingRuntimeAutopilotRunsForRuntimeParams{
		PendingRuntimeID: runtimeID,
		Limit:            maxPendingRuntimeRunsPerSweep,
	})
	if err != nil {
		return fmt.Errorf("list pending runtime runs: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	slog.Info("autopilot dispatch: runtime came online, draining pending runs",
		"runtime_id", util.UUIDToString(runtimeID),
		"pending_count", len(rows),
	)

	for _, row := range rows {
		// Re-load the autopilot fresh. The SQL JOIN returned a snapshot
		// at SELECT time, but the autopilot may have been reassigned,
		// paused, or archived in the gap between SELECT and this loop
		// iteration. The post-admission branch (DispatchAutopilot's
		// `running` path) is idempotent on re-call, so a re-dispatch
		// of a run that already slipped through is at worst a no-op
		// double-skip — acceptable for the first version.
		autopilot, err := s.Queries.GetAutopilot(ctx, row.AutopilotID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Autopilot hard-deleted while pending. Mark the run
				// skipped with a useful reason so the UI doesn't show
				// a forever-pending row.
				s.transitionPendingToSkipped(ctx, row.ID, "autopilot was deleted while waiting for runtime")
				continue
			}
			slog.Warn("autopilot dispatch: failed to reload autopilot for pending run",
				"run_id", util.UUIDToString(row.ID),
				"autopilot_id", util.UUIDToString(row.AutopilotID),
				"error", err,
			)
			continue
		}
		if autopilot.Status != "active" {
			// The SQL WHERE clause already filters to status='active',
			// but a race (paused/archived between SELECT and reload)
			// can still flip it. Re-check defensively.
			s.transitionPendingToSkipped(ctx, row.ID, "autopilot is no longer active (paused or archived)")
			continue
		}

		// Re-run the admission gate. If the runtime is still offline
		// (e.g. another agent on this runtime is up but the autopilot's
		// agent is on a different runtime that flapped), leave the run
		// pending. Any other failure → terminal skip.
		if reason, skip := s.shouldSkipDispatch(ctx, autopilot); skip {
			if isRuntimeOfflineAdmissionReason(reason) {
				// Still offline. Leave as pending; the next time the
				// runtime comes online we'll re-try. No log line per
				// pending run per tick — that would be noisy on a
				// stable-but-offline agent.
				continue
			}
			s.transitionPendingToSkipped(ctx, row.ID, reason)
			continue
		}

		// Clear the pending_runtime_id pointer *before* the dispatch,
		// so a later ListPendingRuntimeAutopilotRunsForRuntime call (in
		// this same sweep or a concurrent heartbeat) does not double-
		// dispatch. If dispatch itself fails, the row is left in
		// 'pending_runtime' via the fallback in DispatchAutopilot and
		// would re-appear on a subsequent sweep.
		if _, err := s.Queries.ClearAutopilotRunPendingRuntime(ctx, row.ID); err != nil {
			slog.Warn("autopilot dispatch: failed to clear pending_runtime_id before re-dispatch",
				"run_id", util.UUIDToString(row.ID), "error", err)
			continue
		}

		// Re-dispatch through the standard entry point. The run row
		// already exists (we did not create a new one); pass
		// existing=true semantics via the run pointer in DispatchAutopilot.
		s.dispatchExistingPendingRun(ctx, autopilot, row)
	}
	return nil
}

// dispatchExistingPendingRun re-dispatches a pre-existing pending_runtime
// run. It mirrors DispatchAutopilot's per-mode branch but does not create
// a new autopilot_run row (one was already created when the cron fired
// and the runtime was offline). The Status transition (pending_runtime
// → issue_created / running) and the side effects (issue creation, task
// enqueue) are identical to a fresh dispatch.
func (s *AutopilotService) dispatchExistingPendingRun(
	ctx context.Context,
	autopilot db.Autopilot,
	row db.ListPendingRuntimeAutopilotRunsForRuntimeRow,
) {
	run := db.AutopilotRun{
		ID:               row.ID,
		AutopilotID:      row.AutopilotID,
		TriggerID:        row.TriggerID,
		Source:           row.Source,
		Status:           row.Status,
		IssueID:          row.IssueID,
		TaskID:           row.TaskID,
		TriggeredAt:      row.TriggeredAt,
		CompletedAt:      row.CompletedAt,
		FailureReason:    row.FailureReason,
		TriggerPayload:   row.TriggerPayload,
		Result:           row.Result,
		CreatedAt:        row.CreatedAt,
		SquadID:          row.SquadID,
		PendingRuntimeID: row.PendingRuntimeID,
	}

	switch autopilot.ExecutionMode {
	case "create_issue":
		triggerTimezone := s.resolveAutopilotTriggerTimezone(ctx, run.TriggerID)
		if err := s.dispatchCreateIssue(ctx, autopilot, &run, triggerTimezone); err != nil {
			if skipped := s.handleDispatchSkip(ctx, autopilot, &run, err); skipped != nil {
				return
			}
			s.failRun(ctx, run.ID, err.Error())
			s.captureAutopilotRunFailed(autopilot, run, run.Source, err.Error())
			return
		}
	case "run_only":
		if err := s.dispatchRunOnly(ctx, autopilot, &run); err != nil {
			if skipped := s.handleDispatchSkip(ctx, autopilot, &run, err); skipped != nil {
				return
			}
			s.failRun(ctx, run.ID, err.Error())
			s.captureAutopilotRunFailed(autopilot, run, run.Source, err.Error())
			return
		}
	default:
		s.failRun(ctx, run.ID, "unknown execution_mode: "+autopilot.ExecutionMode)
		return
	}

	// Mirror DispatchAutopilot's last_run_at + WS event so the UI
	// observes the transition from 'pending_runtime' to 'issue_created'
	// / 'running' identically to a fresh dispatch.
	s.Queries.UpdateAutopilotLastRunAt(ctx, autopilot.ID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAutopilotRunStart,
		WorkspaceID: util.UUIDToString(autopilot.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"run_id":       util.UUIDToString(run.ID),
			"autopilot_id": util.UUIDToString(autopilot.ID),
			"source":       run.Source,
			"status":       run.Status,
			// 'resumed_from_pending_runtime' lets the UI distinguish
			// "the cron fired now and started work" from "we just
			// resumed a run that was parked waiting for the runtime".
			// Both produce a fresh issue / task; the diagnostic value
			// is in user-facing run history and analytics.
			"resumed_from": "pending_runtime",
		},
	})
}

// transitionPendingToSkipped rewinds a pending_runtime run to the
// 'skipped' terminal status. Used by the runtime-comes-online hook when
// the re-validated admission gate fails for a non-runtime reason (agent
// archived, autopilot paused/archived/deleted, etc.). Mirrors
// recordSkippedRun's row finalization but on an existing run id.
func (s *AutopilotService) transitionPendingToSkipped(ctx context.Context, runID pgtype.UUID, reason string) {
	updated, err := s.Queries.UpdateAutopilotRunSkipped(ctx, db.UpdateAutopilotRunSkippedParams{
		ID:            runID,
		FailureReason: pgtype.Text{String: reason, Valid: true},
	})
	if err != nil {
		slog.Warn("autopilot dispatch: failed to transition pending_runtime to skipped",
			"run_id", util.UUIDToString(runID), "error", err)
		return
	}
	// Clear the queue pointer so future sweeps don't see this row.
	if _, err := s.Queries.ClearAutopilotRunPendingRuntime(ctx, runID); err != nil {
		slog.Warn("autopilot dispatch: failed to clear pending_runtime_id after skip",
			"run_id", util.UUIDToString(runID), "error", err)
	}
	slog.Info("autopilot dispatch: pending_runtime → skipped",
		"run_id", util.UUIDToString(runID),
		"reason", reason,
	)
	// publishRunDone uses run.WorkspaceID indirectly via autopilot_id;
	// we don't have the autopilot loaded here, so load it for the event.
	if ap, err := s.Queries.GetAutopilotByRunID(ctx, runID); err == nil {
		s.publishRunDone(util.UUIDToString(ap.WorkspaceID), updated, "skipped")
	}
}

// recordSkippedRun persists a `skipped` autopilot_run with the given reason
// and emits the same WS / analytics signals that a normal terminal transition
// would. Returns the run + nil error so callers (scheduler tick, manual
// trigger handler) treat this as a successful — but no-op — dispatch.
func (s *AutopilotService) recordSkippedRun(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
	reason string,
) (*db.AutopilotRun, error) {
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:    autopilot.ID,
		TriggerID:      triggerID,
		Source:         source,
		Status:         "skipped",
		TriggerPayload: payload,
		SquadID:        autopilotSquadAttribution(autopilot),
	})
	if err != nil {
		return nil, fmt.Errorf("create skipped run: %w", err)
	}

	updated, err := s.Queries.UpdateAutopilotRunSkipped(ctx, db.UpdateAutopilotRunSkippedParams{
		ID:            run.ID,
		FailureReason: pgtype.Text{String: reason, Valid: true},
	})
	if err == nil {
		run = updated
	} else {
		slog.Warn("failed to set skip reason on autopilot run",
			"run_id", util.UUIDToString(run.ID), "error", err)
	}

	slog.Info("autopilot dispatch skipped",
		"autopilot_id", util.UUIDToString(autopilot.ID),
		"run_id", util.UUIDToString(run.ID),
		"source", source,
		"reason", reason,
	)

	// Bump last_run_at so scheduler advancement and "last seen" UI both
	// reflect that we did evaluate the trigger this tick.
	s.Queries.UpdateAutopilotLastRunAt(ctx, autopilot.ID)

	s.publishRunDone(util.UUIDToString(autopilot.WorkspaceID), run, "skipped")
	return &run, nil
}

func (s *AutopilotService) publishRunDone(workspaceID string, run db.AutopilotRun, status string) {
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAutopilotRunDone,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"run_id":       util.UUIDToString(run.ID),
			"autopilot_id": util.UUIDToString(run.AutopilotID),
			"status":       status,
		},
	})
}

func (s *AutopilotService) captureIssueCreatedFromAutopilot(ap db.Autopilot, run *db.AutopilotRun, issue db.Issue, leaderID pgtype.UUID) {
	if s.TaskSvc == nil || s.TaskSvc.Analytics == nil {
		return
	}
	// For PostHog the agent_id should be the agent that will actually run
	// the work (the resolved leader for squad autopilots) so per-agent task
	// counts line up with what daemons report.
	obsmetrics.RecordEvent(s.TaskSvc.Analytics, s.TaskSvc.Metrics, analytics.IssueCreated(
		autopilotActorID(ap),
		util.UUIDToString(ap.WorkspaceID),
		util.UUIDToString(issue.ID),
		util.UUIDToString(leaderID),
		"",
		util.UUIDToString(run.ID),
		analytics.SourceAutopilot,
		analytics.PlatformServer,
	))
}

func (s *AutopilotService) captureAutopilotRunStarted(ap db.Autopilot, run db.AutopilotRun, triggerSource string) {
	if s.TaskSvc == nil || s.TaskSvc.Analytics == nil {
		return
	}
	obsmetrics.RecordEvent(s.TaskSvc.Analytics, s.TaskSvc.Metrics, analytics.AutopilotRunStarted(
		autopilotActorID(ap),
		util.UUIDToString(ap.WorkspaceID),
		util.UUIDToString(ap.ID),
		util.UUIDToString(run.ID),
		triggerSource, // cadence proxy: see autopilot cadence note in metrics/labels_pr3.go
		s.autopilotAssigneeAnalytics(ap),
		triggerSource,
	))
}

func (s *AutopilotService) captureAutopilotRunCompleted(ap db.Autopilot, run db.AutopilotRun) {
	if s.TaskSvc == nil || s.TaskSvc.Analytics == nil {
		return
	}
	obsmetrics.RecordEvent(s.TaskSvc.Analytics, s.TaskSvc.Metrics, analytics.AutopilotRunCompleted(
		autopilotActorID(ap),
		util.UUIDToString(ap.WorkspaceID),
		util.UUIDToString(ap.ID),
		util.UUIDToString(run.ID),
		run.Source,
		s.autopilotAssigneeAnalytics(ap),
		run.Source,
		autopilotRunDurationMS(run),
	))
}

func (s *AutopilotService) captureAutopilotRunFailed(ap db.Autopilot, run db.AutopilotRun, triggerSource, reason string) {
	if s.TaskSvc == nil || s.TaskSvc.Analytics == nil {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	obsmetrics.RecordEvent(s.TaskSvc.Analytics, s.TaskSvc.Metrics, analytics.AutopilotRunFailed(
		autopilotActorID(ap),
		util.UUIDToString(ap.WorkspaceID),
		util.UUIDToString(ap.ID),
		util.UUIDToString(run.ID),
		triggerSource,
		s.autopilotAssigneeAnalytics(ap),
		triggerSource,
		reason,
		autopilotErrorType(reason),
		false,
		autopilotRunDurationMS(run),
	))
}

// autopilotAssigneeAnalytics builds the PostHog assignee descriptor for an
// autopilot. For squad autopilots agent_id is best-effort the resolved
// leader (so per-agent funnels stay consistent); a resolve error degrades
// to the raw assignee_id rather than dropping the event — incomplete data
// in the dashboard is preferable to silent attribution gaps.
func (s *AutopilotService) autopilotAssigneeAnalytics(ap db.Autopilot) analytics.AutopilotAssignee {
	assignee := analytics.AutopilotAssignee{
		AssigneeType: ap.AssigneeType,
	}
	if ap.AssigneeType == "squad" {
		assignee.SquadID = util.UUIDToString(ap.AssigneeID)
		if leader, _, err := s.resolveAutopilotLeader(context.Background(), ap); err == nil {
			assignee.AgentID = util.UUIDToString(leader.ID)
		} else {
			assignee.AgentID = util.UUIDToString(ap.AssigneeID)
		}
	} else {
		assignee.AgentID = util.UUIDToString(ap.AssigneeID)
	}
	return assignee
}

func autopilotErrorType(reason string) string {
	switch {
	case strings.Contains(reason, "unknown execution_mode"):
		return "configuration"
	case strings.HasPrefix(reason, "issue "):
		return "issue_terminal"
	case strings.Contains(reason, "create issue"), strings.Contains(reason, "enqueue task"), strings.Contains(reason, "dispatch"):
		return "dispatch_error"
	case strings.HasPrefix(reason, "task "):
		return "task_error"
	default:
		return "autopilot_error"
	}
}

func autopilotActorID(ap db.Autopilot) string {
	id := util.UUIDToString(ap.CreatedByID)
	if ap.CreatedByType == "agent" && id != "" {
		return "agent:" + id
	}
	if id != "" {
		return id
	}
	return "system"
}

func autopilotRunDurationMS(run db.AutopilotRun) int64 {
	if !run.CompletedAt.Valid {
		return 0
	}
	start := run.TriggeredAt
	if !start.Valid {
		start = run.CreatedAt
	}
	if !start.Valid {
		return 0
	}
	ms := run.CompletedAt.Time.Sub(start.Time).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func (s *AutopilotService) resolveAutopilotTriggerTimezone(ctx context.Context, triggerID pgtype.UUID) string {
	if !triggerID.Valid || s == nil || s.Queries == nil {
		return DefaultAutopilotTriggerTimezone
	}

	trigger, err := s.Queries.GetAutopilotTrigger(ctx, triggerID)
	if err != nil {
		slog.Warn("failed to load autopilot trigger timezone; falling back to UTC",
			"trigger_id", util.UUIDToString(triggerID),
			"error", err,
		)
		return DefaultAutopilotTriggerTimezone
	}

	timezone := strings.TrimSpace(trigger.Timezone.String)
	if !trigger.Timezone.Valid || timezone == "" {
		return DefaultAutopilotTriggerTimezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		slog.Warn("invalid autopilot trigger timezone; falling back to UTC",
			"trigger_id", util.UUIDToString(triggerID),
			"timezone", timezone,
			"error", err,
		)
		return DefaultAutopilotTriggerTimezone
	}
	return timezone
}

func formatAutopilotRunTimestamp(run db.AutopilotRun, timezone string) string {
	triggeredAt := autopilotRunTriggeredAt(run)
	loc, label := autopilotTriggerLocation(timezone)
	return triggeredAt.In(loc).Format("2006-01-02 15:04") + " " + label
}

func formatAutopilotRunDate(run db.AutopilotRun, timezone string) string {
	triggeredAt := autopilotRunTriggeredAt(run)
	loc, _ := autopilotTriggerLocation(timezone)
	return triggeredAt.In(loc).Format("2006-01-02")
}

func autopilotRunTriggeredAt(run db.AutopilotRun) time.Time {
	if run.TriggeredAt.Valid {
		return run.TriggeredAt.Time
	}
	if run.CreatedAt.Valid {
		return run.CreatedAt.Time
	}
	return time.Now().UTC()
}

func autopilotTriggerLocation(timezone string) (*time.Location, string) {
	label := strings.TrimSpace(timezone)
	if label == "" {
		label = DefaultAutopilotTriggerTimezone
	}
	loc, err := time.LoadLocation(label)
	if err != nil {
		return time.UTC, DefaultAutopilotTriggerTimezone
	}
	return loc, label
}

// buildIssueDescription appends an autopilot system instruction to the
// user-provided description, asking the agent to rename the issue after
// it understands the actual work. For webhook-sourced runs, also appends
// a payload section so the agent has the event context inline (otherwise
// the agent only sees the issue body, never the run's trigger_payload).
func (s *AutopilotService) buildIssueDescription(ap db.Autopilot, run db.AutopilotRun, triggerTimezone string) pgtype.Text {
	triggeredAt := formatAutopilotRunTimestamp(run, triggerTimezone)
	var b strings.Builder
	b.WriteString(ap.Description.String)
	b.WriteString("\n\n---\n*Autopilot run triggered at ")
	b.WriteString(triggeredAt)
	b.WriteString(". After starting work, rename this issue to accurately reflect what you are doing.*")

	if run.Source == "webhook" && len(run.TriggerPayload) > 0 {
		event := "webhook.received"
		var payloadJSON []byte
		var env struct {
			Event        string          `json:"event"`
			EventPayload json.RawMessage `json:"eventPayload"`
		}
		if err := json.Unmarshal(run.TriggerPayload, &env); err == nil {
			if env.Event != "" {
				event = env.Event
			}
			if len(env.EventPayload) > 0 {
				if pretty, err := prettifyJSON(env.EventPayload); err == nil {
					payloadJSON = pretty
				}
			}
		}
		if len(payloadJSON) == 0 {
			if pretty, err := prettifyJSON(run.TriggerPayload); err == nil {
				payloadJSON = pretty
			} else {
				payloadJSON = run.TriggerPayload
			}
		}
		b.WriteString("\n\nWebhook event: ")
		b.WriteString(event)
		b.WriteString("\n\nWebhook payload:\n```json\n")
		b.Write(payloadJSON)
		b.WriteString("\n```")
	}

	return pgtype.Text{String: b.String(), Valid: true}
}

func prettifyJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}

// issueTitleTemplateTokenRE matches any {{...}} token in an issue-title
// template. We deliberately permit whitespace inside the braces ({{ date }})
// so users can format templates either way; the canonical token is still
// {{date}}.
var issueTitleTemplateTokenRE = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// interpolateTemplate substitutes supported {{name}} placeholders in the
// issue title template. Whitespace inside the braces ({{ date }}) is
// tolerated so the render layer accepts every form that
// ValidateIssueTitleTemplate accepts — otherwise users would save templates
// that pass validation but still emit a literal token at trigger time.
func (s *AutopilotService) interpolateTemplate(ap db.Autopilot, run db.AutopilotRun, triggerTimezone string) string {
	tmpl := ap.Title
	if ap.IssueTitleTemplate.Valid && ap.IssueTitleTemplate.String != "" {
		tmpl = ap.IssueTitleTemplate.String
	}
	triggerDate := formatAutopilotRunDate(run, triggerTimezone)
	return issueTitleTemplateTokenRE.ReplaceAllStringFunc(tmpl, func(match string) string {
		name := strings.TrimSpace(match[2 : len(match)-2])
		switch name {
		case "date":
			return triggerDate
		default:
			return match
		}
	})
}

// SupportedIssueTitleTemplateVariables enumerates the placeholders that
// interpolateTemplate will substitute. Keep this in sync with the
// substitution logic above and with the docs in autopilots.mdx /
// autopilots.zh.mdx.
var SupportedIssueTitleTemplateVariables = []string{"date"}

// ValidateIssueTitleTemplate rejects templates that contain any {{...}} token
// other than the supported set. An empty template is valid (the autopilot
// falls back to its own Title). The error message names the first offending
// token to keep CLI feedback actionable.
func ValidateIssueTitleTemplate(tmpl string) error {
	if tmpl == "" {
		return nil
	}
	for _, m := range issueTitleTemplateTokenRE.FindAllStringSubmatch(tmpl, -1) {
		name := m[1]
		if !isSupportedIssueTitleVariable(name) {
			return fmt.Errorf(
				"unknown template variable %q; supported: {{%s}}",
				name,
				strings.Join(SupportedIssueTitleTemplateVariables, "}}, {{"),
			)
		}
	}
	return nil
}

func isSupportedIssueTitleVariable(name string) bool {
	for _, v := range SupportedIssueTitleTemplateVariables {
		if name == v {
			return true
		}
	}
	return false
}

func (s *AutopilotService) getIssuePrefix(workspaceID pgtype.UUID) string {
	ws, err := s.Queries.GetWorkspace(context.Background(), workspaceID)
	if err != nil {
		return ""
	}
	return ws.IssuePrefix
}

// canCreatorAccessPrivateLeader checks whether the autopilot's creator still
// has access to a private leader agent. Mirrors handler.canAccessPrivateAgent
// logic: agent creators always pass; member creators must be the agent owner
// or a workspace owner/admin. Returns false (fail-closed) on any lookup error.
func (s *AutopilotService) canCreatorAccessPrivateLeader(ctx context.Context, ap db.Autopilot, leader db.Agent) bool {
	if ap.CreatedByType == "agent" {
		return true
	}
	creatorID := util.UUIDToString(ap.CreatedByID)
	if util.UUIDToString(leader.OwnerID) == creatorID {
		return true
	}
	member, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      ap.CreatedByID,
		WorkspaceID: ap.WorkspaceID,
	})
	if err != nil {
		return false
	}
	return member.Role == "owner" || member.Role == "admin"
}
