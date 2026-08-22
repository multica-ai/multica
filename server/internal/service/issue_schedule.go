package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// issueScheduleMissedInboxType is the inbox_item.type written when a
// scheduled run fails to fire (#5927). inbox_item.type has no CHECK
// constraint, so this needs no migration; the frontend falls back to
// generic title/body rendering for a type it does not special-case
// (packages/views/inbox/components/inbox-display.ts).
const issueScheduleMissedInboxType = "issue_schedule_missed"

// Errors returned by IssueScheduleService.CreateSchedule. Handlers translate
// these to the appropriate HTTP status.
var (
	// ErrIssueScheduleAlreadyPending means the issue already has a pending
	// schedule — migration 378's partial unique index enforces this at the
	// DB level; CreateSchedule translates the resulting unique-violation
	// into this sentinel so the handler doesn't need to know about Postgres
	// error codes.
	ErrIssueScheduleAlreadyPending = errors.New("issue already has a pending schedule")
	// ErrIssueScheduleRunAtNotFuture means the requested run_at is not
	// strictly after the current time.
	ErrIssueScheduleRunAtNotFuture = errors.New("run_at must be in the future")
	// ErrIssueScheduleNoAssignee means the issue has no assignee, so there is
	// nothing for a scheduled run to trigger.
	ErrIssueScheduleNoAssignee = errors.New("issue has no assignee")
)

// IssueScheduleService owns issue_scheduled_trigger CRUD (server/internal/handler/issue_schedule.go)
// and the fire-time dispatch logic invoked by the scheduler job
// (server/internal/scheduler/jobs_issue_schedule.go). It lives in the
// service package — not the handler — specifically so the scheduler package
// can call DispatchIssueSchedule without importing handler (#5927).
type IssueScheduleService struct {
	Queries     *db.Queries
	TxStarter   TxStarter
	TaskService *TaskService
}

func NewIssueScheduleService(q *db.Queries, tx TxStarter, ts *TaskService) *IssueScheduleService {
	return &IssueScheduleService{Queries: q, TxStarter: tx, TaskService: ts}
}

// CreateSchedule registers a one-time future run for issue, attributed to
// createdByUserID (the human who set it up).
//
// The caller is responsible for the invocation-permission gate
// (canInvokeAgent / canEnqueueSquadLeader) BEFORE calling this — this
// service never runs it. That split mirrors Autopilot: a trigger is
// authorized once, at configuration time, by the handler that has full
// request/actor context; DispatchIssueSchedule (fire time) trusts that
// earlier decision instead of re-deriving it from a request that no longer
// exists. See DispatchIssueSchedule's doc comment for the full rationale.
func (s *IssueScheduleService) CreateSchedule(ctx context.Context, issue db.Issue, runAt time.Time, createdByUserID pgtype.UUID) (db.IssueScheduledTrigger, error) {
	if !runAt.After(time.Now()) {
		return db.IssueScheduledTrigger{}, ErrIssueScheduleRunAtNotFuture
	}
	if !hasAssignee(issue) {
		return db.IssueScheduledTrigger{}, ErrIssueScheduleNoAssignee
	}

	trigger, err := s.Queries.CreateIssueScheduledTrigger(ctx, db.CreateIssueScheduledTriggerParams{
		WorkspaceID:     issue.WorkspaceID,
		IssueID:         issue.ID,
		RunAt:           pgtype.Timestamptz{Time: runAt.UTC(), Valid: true},
		CreatedByUserID: createdByUserID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return db.IssueScheduledTrigger{}, ErrIssueScheduleAlreadyPending
		}
		return db.IssueScheduledTrigger{}, fmt.Errorf("create issue schedule: %w", err)
	}
	return trigger, nil
}

// CancelSchedule cancels the pending schedule for issueID, if any. Returns
// pgx.ErrNoRows when there is no pending schedule to cancel — the handler
// treats that as 404.
func (s *IssueScheduleService) CancelSchedule(ctx context.Context, issueID pgtype.UUID) (db.IssueScheduledTrigger, error) {
	trigger, err := s.Queries.CancelIssueScheduledTrigger(ctx, issueID)
	if err != nil {
		return db.IssueScheduledTrigger{}, err
	}
	return trigger, nil
}

// GetPendingSchedule returns the pending schedule for issueID, if any.
// Returns pgx.ErrNoRows when there is none.
func (s *IssueScheduleService) GetPendingSchedule(ctx context.Context, issueID pgtype.UUID) (db.IssueScheduledTrigger, error) {
	return s.Queries.GetPendingIssueScheduledTriggerForIssue(ctx, issueID)
}

// DispatchIssueSchedule fires one due schedule. Called exclusively by the
// scheduler job (server/internal/scheduler/jobs_issue_schedule.go) — never by
// the HTTP handler.
//
// It starts by atomically CLAIMING trigger.ID — transitioning it
// pending -> fired via MarkIssueScheduledTriggerFired's WHERE status =
// 'pending' guard — before doing anything else. This is deliberate and not
// just a status bookkeeping detail: the scheduler's AllowStaleReentry can
// invoke DispatchIssueSchedule twice for the same trigger under stale-lease
// theft (see jobs_issue_schedule.go's MaxAttempts doc comment), and without
// an exclusive claim taken FIRST, both invocations could pass the
// status == "pending" check on their own copy of the trigger and both call
// EnqueueTaskForIssueWithHandoff — a double-fire. Claiming first means only
// one caller ever proceeds past this point; the other gets pgx.ErrNoRows and
// returns cleanly, trusting the winner to finish the job (including
// notifying on failure).
//
// After the claim, it re-loads the issue fresh (the assignee may have
// changed since the schedule was created) and enqueues a run for the
// CURRENT assignee. If anything after the claim fails — issue gone, no
// assignee, the enqueue call itself errors — the already-claimed row is
// walked forward again, fired -> missed (MarkIssueScheduledTriggerMissed's
// WHERE status = 'fired' guard: only the caller that won the claim above can
// ever reach this), and the creator is notified via an action-required
// inbox item. The resolved answer on #5927 is "notify, don't silently
// retry" — there is no scheduler-level retry of a missed fire; MaxAttempts
// on the job spec covers only this process's own crash recovery, not the
// business condition.
//
// Deliberately does NOT re-run the invocation-permission gate
// (canInvokeAgent / canEnqueueSquadLeader): that gate already ran once,
// synchronously, in the HTTP handler when the schedule was created. This
// mirrors service.AutopilotService.DispatchAutopilotForPlan, which does not
// re-check "can this actor invoke this agent" at fire time either —
// authorization is a property of the configured trigger, not of the
// unattended dispatch that later executes it.
//
// Known gap, called out here rather than silently glossed over: this
// function cannot detect "runtime offline" or "quota exhausted" — those are
// judged later, when a daemon polls and claims the queued task.
// EnqueueTaskForIssueWithHandoff only writes the queued row, so a schedule
// whose target runtime never comes back online still shows as 'fired'
// here. Closing that gap needs its own design (see the #5927 issue thread
// and the open questions noted in the implementing PR) and is out of scope
// for this issue-scoped v1.
func (s *IssueScheduleService) DispatchIssueSchedule(ctx context.Context, trigger db.IssueScheduledTrigger) error {
	claimed, err := s.Queries.MarkIssueScheduledTriggerFired(ctx, trigger.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Lost the claim race, or already resolved by a previous
			// attempt (crash-recovery re-invocation) — either way, nothing
			// left for this caller to do.
			return nil
		}
		return fmt.Errorf("dispatch issue schedule: claim: %w", err)
	}

	issue, err := s.Queries.GetIssue(ctx, claimed.IssueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.missDispatch(ctx, claimed, "issue_not_found")
		}
		return fmt.Errorf("dispatch issue schedule: load issue: %w", err)
	}

	if !hasAssignee(issue) {
		return s.missDispatch(ctx, claimed, "no_assignee")
	}

	var dispatchErr error
	switch issue.AssigneeType.String {
	case "agent":
		dispatchErr = s.dispatchToAgent(ctx, issue, claimed.CreatedByUserID)
	case "squad":
		dispatchErr = s.dispatchToSquadLeader(ctx, issue, claimed.CreatedByUserID)
	default:
		dispatchErr = fmt.Errorf("unsupported assignee type %q", issue.AssigneeType.String)
	}
	if dispatchErr != nil {
		slog.Warn("issue schedule dispatch failed",
			"trigger_id", util.UUIDToString(claimed.ID),
			"issue_id", util.UUIDToString(claimed.IssueID),
			"error", dispatchErr,
		)
		return s.missDispatch(ctx, claimed, "dispatch_failed")
	}

	return nil
}

// dispatchToAgent enqueues a run for a directly-assigned issue. No note is
// carried — there is no human present at fire time to write one, unlike the
// assign/promote path in server/internal/handler/issue_trigger.go.
func (s *IssueScheduleService) dispatchToAgent(ctx context.Context, issue db.Issue, actorUserID pgtype.UUID) error {
	_, err := s.TaskService.EnqueueTaskForIssueWithHandoff(ctx, issue, "", actorUserID)
	return err
}

// dispatchToSquadLeader enqueues a leader run for a squad-assigned issue.
// Mirrors server/internal/handler/squad.go's enqueueSquadLeaderTask, minus
// the invocation-permission gate (see DispatchIssueSchedule's doc comment)
// but keeping the same pending-task dedup: if a leader run is already
// queued/active at the current head, the schedule's job — start a run — is
// already satisfied, so this counts as a successful fire rather than a
// miss.
func (s *IssueScheduleService) dispatchToSquadLeader(ctx context.Context, issue db.Issue, actorUserID pgtype.UUID) error {
	squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("load squad: %w", err)
	}

	hasPending, err := s.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: squad.LeaderID,
		HeadSha: s.TaskService.ResolveIssueReviewSHAParam(ctx, issue.ID),
	})
	if err != nil {
		return fmt.Errorf("check pending leader task: %w", err)
	}
	if hasPending {
		return nil
	}

	_, err = s.TaskService.EnqueueTaskForSquadLeaderWithHandoff(ctx, issue, squad.LeaderID, squad.ID, "", actorUserID)
	return err
}

// missDispatch walks an already-claimed (status='fired') trigger forward to
// 'missed' and notifies its creator. Called only from DispatchIssueSchedule
// after MarkIssueScheduledTriggerFired has already claimed trigger, so the
// status write here should always find its row; a failure would mean the
// row vanished between claim and this call, which is propagated so the
// scheduler's own crash-recovery policy gets a chance to sort it out. A
// failure to write the notification itself is logged but does not fail the
// dispatch — the trigger is unambiguously resolved either way and there is
// nothing else to retry.
func (s *IssueScheduleService) missDispatch(ctx context.Context, trigger db.IssueScheduledTrigger, reason string) error {
	missed, err := s.Queries.MarkIssueScheduledTriggerMissed(ctx, trigger.ID)
	if err != nil {
		return fmt.Errorf("dispatch issue schedule: mark missed: %w", err)
	}
	s.notifyScheduleMissed(ctx, missed, reason)
	return nil
}

// notifyScheduleMissed writes the action-required inbox item a missed
// schedule leaves for its creator (the resolved answer on #5927: notify,
// don't silently retry). Mirrors TaskService.writeQuickCreateOutcomeInbox's
// shape — same severity, same "system" actor for a platform-initiated
// event, same details-blob convention.
func (s *IssueScheduleService) notifyScheduleMissed(ctx context.Context, trigger db.IssueScheduledTrigger, reason string) {
	details, err := json.Marshal(map[string]any{
		"schedule_id": util.UUIDToString(trigger.ID),
		"run_at":      trigger.RunAt.Time.UTC().Format(time.RFC3339),
		"reason":      reason,
	})
	if err != nil {
		slog.Error("issue schedule: encode missed-notification details failed",
			"trigger_id", util.UUIDToString(trigger.ID), "error", err)
		return
	}
	_, err = s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   trigger.WorkspaceID,
		RecipientType: "member",
		RecipientID:   trigger.CreatedByUserID,
		Type:          issueScheduleMissedInboxType,
		Severity:      "action_required",
		IssueID:       trigger.IssueID,
		Title:         "Scheduled run did not start",
		Body:          pgtype.Text{String: issueScheduleMissedReasonDetail(reason), Valid: true},
		ActorType:     pgtype.Text{String: "system", Valid: true},
		ActorID:       pgtype.UUID{},
		Details:       details,
	})
	if err != nil {
		slog.Error("issue schedule: missed-notification inbox write failed",
			"trigger_id", util.UUIDToString(trigger.ID), "error", err)
	}
}

// issueScheduleMissedReasonDetail turns the internal miss reason into the
// inbox body text. Kept centralized (rather than inlined at each
// missDispatch call site) so the three reasons stay in sync with each other.
func issueScheduleMissedReasonDetail(reason string) string {
	switch reason {
	case "issue_not_found":
		return "The issue was deleted before the scheduled time arrived."
	case "no_assignee":
		return "The issue no longer has an assignee, so there was nothing to run."
	case "dispatch_failed":
		return "Starting the run failed. Check the issue's assignee and try scheduling again."
	default:
		return "The scheduled run did not start."
	}
}

// hasAssignee reports whether issue currently has a resolvable assignee
// (agent or squad). Both AssigneeType and AssigneeID must be set — mirrors
// the check enqueueIssueTaskWithCommentPlan itself performs.
func hasAssignee(issue db.Issue) bool {
	return issue.AssigneeType.Valid && issue.AssigneeType.String != "" && issue.AssigneeID.Valid
}
