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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issueguard"
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

const autopilotRecentDuplicateWindow = 60 * time.Second

func NewAutopilotService(q *db.Queries, tx TxStarter, bus *events.Bus, taskSvc *TaskService) *AutopilotService {
	return &AutopilotService{Queries: q, TxStarter: tx, Bus: bus, TaskSvc: taskSvc}
}

// autopilotRuleConfigSummary captures the substantive (accountability-bearing)
// config of an autopilot at publish time, stored on each rule-version snapshot for
// audit display (MUL-4302 §7). Cosmetic fields (title / description / issue title
// template) are intentionally excluded — changing them does not transfer
// accountability. Trigger config (cron / webhook / event_filters) lives in a
// separate table and is not inlined here; a trigger edit still republishes the
// rule (recording the editing member + timestamp), the summary just carries the
// autopilot row's core config.
type autopilotRuleConfigSummary struct {
	AssigneeType  string `json:"assignee_type"`
	AssigneeID    string `json:"assignee_id"`
	Status        string `json:"status"`
	ExecutionMode string `json:"execution_mode"`
}

// RecordAutopilotRuleVersion appends one rule-version snapshot for a substantive
// publish (MUL-4302 §3.4), recording the publisher and the effective config. Shared
// by the handler publish paths (create / update / trigger edits / archive, run in
// their tx) and the failure monitor's system-pause (a different package). q is the
// caller's *db.Queries (tx-scoped where the caller wants atomicity). publishedByType
// is "member" (with the acting member id) or "system" (with an invalid id, e.g. the
// auto-pause monitor).
func RecordAutopilotRuleVersion(ctx context.Context, q *db.Queries, ap db.Autopilot, publishedByType string, publishedByID pgtype.UUID) error {
	summary, err := json.Marshal(autopilotRuleConfigSummary{
		AssigneeType:  ap.AssigneeType,
		AssigneeID:    util.UUIDToString(ap.AssigneeID),
		Status:        ap.Status,
		ExecutionMode: ap.ExecutionMode,
	})
	if err != nil {
		return fmt.Errorf("marshal rule version config summary: %w", err)
	}
	if _, err := q.CreateAutopilotRuleVersion(ctx, db.CreateAutopilotRuleVersionParams{
		AutopilotID:     ap.ID,
		WorkspaceID:     ap.WorkspaceID,
		PublishedByType: publishedByType,
		PublishedByID:   publishedByID,
		ConfigSummary:   summary,
	}); err != nil {
		return fmt.Errorf("create autopilot rule version: %w", err)
	}
	return nil
}

// DispatchAutopilot is the core execution entry point.
// It creates a run and either creates an issue or enqueues a direct agent task
// depending on execution_mode.
//
// Before run_only work is queued we run an admission check against the assignee
// agent's runtime: if it is not online, we record a `skipped` run with a
// failure_reason and return without enqueueing. This is the "触发时准入" gate
// from MUL-1899 — without it a paused laptop / offline daemon causes scheduled
// autopilots to pile thousands of doomed tasks onto agent_task_queue.
//
// create_issue mode is different: its primary contract is a durable audit
// trail. If the assignee has a runtime but that runtime is merely offline,
// dispatch still creates the issue and issue task so the work is visible and
// can be claimed when the runtime returns.
//
// When assignee_type='squad' the gate runs against the squad leader (Path A
// from MUL-2429: Autopilot-on-squad ≈ Autopilot-on-leader), with the same
// create_issue audit-trail exception for a merely offline leader runtime.
func (s *AutopilotService) DispatchAutopilot(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
) (*db.AutopilotRun, error) {
	// No member actor on this entry point (schedule / webhook / api, or a manual
	// trigger without a resolved member): attribution resolves rule_owner. These
	// callers don't surface a per-run reason code to a human, so it is dropped.
	// webhookDeliveryID is invalid here — durable webhook deliveries admit through
	// AdmitAutopilotWebhookDelivery instead of this entry point.
	run, _, err := s.dispatchAutopilot(ctx, autopilot, triggerID, source, payload, pgtype.Timestamptz{}, pgtype.UUID{}, pgtype.UUID{})
	return run, err
}

// DispatchAutopilotManual is the "run now" entry point for a member manually
// triggering an autopilot. Unlike scheduled / webhook / api dispatch (no human in
// the loop → rule_owner), a manual trigger is a direct human action: the run is
// attributed direct_human to actorUserID, which becomes BOTH its originator
// (authorization) and accountable human (MUL-4302 §4), across both execution modes.
// An invalid actorUserID behaves exactly like DispatchAutopilot(source="manual").
func (s *AutopilotService) DispatchAutopilotManual(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	payload []byte,
	actorUserID pgtype.UUID,
) (*db.AutopilotRun, dispatch.ReasonCode, error) {
	// The manual path is the one surface that shows a per-run outcome to a human,
	// so it returns the typed reason code decided at the admission source. No
	// webhook delivery on the manual path.
	return s.dispatchAutopilot(ctx, autopilot, triggerID, "manual", payload, pgtype.Timestamptz{}, pgtype.UUID{}, actorUserID)
}

// AdmitAutopilotWebhookDelivery creates or reuses the idempotent run for a
// durable webhook delivery without executing its downstream issue/task side
// effect. The HTTP ingress calls this synchronously so the public webhook
// response can retain its 200 accepted/skipped + run_id contract while the
// database-backed worker still owns recoverable dispatch.
func (s *AutopilotService) AdmitAutopilotWebhookDelivery(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	payload []byte,
	deliveryID pgtype.UUID,
) (*db.AutopilotRun, error) {
	if !deliveryID.Valid {
		return nil, fmt.Errorf("admit webhook delivery: delivery_id is required")
	}

	existing, err := s.Queries.GetAutopilotRunByWebhookDelivery(ctx, deliveryID)
	switch {
	case err == nil:
		return &existing, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("admit webhook delivery: lookup existing run: %w", err)
	}

	// Webhook admission has no member actor → automation principal (rule_owner);
	// the per-run reason code is not surfaced to a human here, so it is dropped.
	if reason, _, skip := s.shouldSkipDispatch(ctx, autopilot, pgtype.UUID{}); skip {
		run, err := s.recordSkippedRun(
			ctx,
			autopilot,
			triggerID,
			"webhook",
			payload,
			pgtype.Timestamptz{},
			deliveryID,
			reason,
		)
		if err != nil {
			return s.recoverConcurrentWebhookAdmission(
				ctx,
				deliveryID,
				fmt.Errorf("admit webhook delivery: create skipped run: %w", err),
			)
		}
		return run, nil
	}

	initialStatus := "issue_created"
	if autopilot.ExecutionMode == "run_only" {
		initialStatus = "running"
	}
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:       autopilot.ID,
		TriggerID:         triggerID,
		Source:            "webhook",
		Status:            initialStatus,
		TriggerPayload:    payload,
		SquadID:           autopilotSquadAttribution(autopilot),
		WebhookDeliveryID: deliveryID,
	})
	if err != nil {
		return s.recoverConcurrentWebhookAdmission(
			ctx,
			deliveryID,
			fmt.Errorf("admit webhook delivery: create run: %w", err),
		)
	}
	s.captureAutopilotRunStarted(autopilot, run, "webhook")
	return &run, nil
}

func (s *AutopilotService) recoverConcurrentWebhookAdmission(
	ctx context.Context,
	deliveryID pgtype.UUID,
	cause error,
) (*db.AutopilotRun, error) {
	// Another server replica may have claimed the durable delivery after
	// ingress persisted it but before the admission lookup. The unique
	// delivery/run index chooses one winner; the loser reuses that run.
	var pgErr *pgconn.PgError
	if !errors.As(cause, &pgErr) || pgErr.Code != "23505" {
		return nil, cause
	}
	existing, err := s.Queries.GetAutopilotRunByWebhookDelivery(ctx, deliveryID)
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("admit webhook delivery: reload concurrent run: %w", err)
	}
	return nil, cause
}

// DispatchAutopilotForWebhookDelivery is the durable webhook worker entry
// point. webhook_delivery_id is persisted on the run and protected by a
// partial unique index, so reclaiming a queued delivery after a process crash
// reuses the original run instead of creating a second issue or task.
func (s *AutopilotService) DispatchAutopilotForWebhookDelivery(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	payload []byte,
	deliveryID pgtype.UUID,
) (*db.AutopilotRun, error) {
	run, err := s.AdmitAutopilotWebhookDelivery(ctx, autopilot, triggerID, payload, deliveryID)
	if err != nil {
		return nil, err
	}
	if isAutopilotRunComplete(*run) {
		if autopilot.ExecutionMode == "create_issue" && run.IssueID.Valid {
			if repairErr := s.ensureWebhookCreateIssueTask(ctx, autopilot, *run); repairErr != nil {
				return run, repairErr
			}
		}
		return run, nil
	}

	// A run_only task may have committed immediately before the process died
	// while linking task_id back to the run. Repair that linkage and wake the
	// daemon; otherwise continue the same partial run below.
	if autopilot.ExecutionMode == "run_only" && !run.TaskID.Valid {
		task, taskErr := s.Queries.GetAutopilotTaskByRun(ctx, run.ID)
		switch {
		case taskErr == nil:
			updated, updateErr := s.Queries.UpdateAutopilotRunRunning(ctx, db.UpdateAutopilotRunRunningParams{
				ID:     run.ID,
				TaskID: task.ID,
			})
			if updateErr != nil {
				return run, fmt.Errorf("dispatch for webhook delivery: repair task linkage: %w", updateErr)
			}
			s.TaskSvc.NotifyTaskEnqueued(ctx, task)
			return &updated, nil
		case !errors.Is(taskErr, pgx.ErrNoRows):
			return run, fmt.Errorf("dispatch for webhook delivery: lookup linked task: %w", taskErr)
		}
	}
	// Webhook worker dispatch has no member actor and no human reason-code
	// surface, so actorUserID is invalid and the reason code is dropped.
	dispatched, _, err := s.dispatchAutopilotRun(ctx, autopilot, triggerID, "webhook", run, pgtype.UUID{})
	return dispatched, err
}

// ensureWebhookCreateIssueTask repairs the create_issue crash window after the
// issue/run transaction commits but before the ordinary task enqueue commits.
// Any existing issue task is sufficient evidence that ownership has already
// moved downstream; otherwise enqueue exactly the same assignee path used by
// the original dispatch.
func (s *AutopilotService) ensureWebhookCreateIssueTask(ctx context.Context, autopilot db.Autopilot, run db.AutopilotRun) error {
	tasks, err := s.Queries.ListTasksByIssue(ctx, run.IssueID)
	if err != nil {
		return fmt.Errorf("dispatch for webhook delivery: inspect issue tasks: %w", err)
	}
	if len(tasks) > 0 {
		return nil
	}
	issue, err := s.Queries.GetIssue(ctx, run.IssueID)
	if err != nil {
		return fmt.Errorf("dispatch for webhook delivery: load linked issue: %w", err)
	}
	if issue.Status != "todo" && issue.Status != "in_progress" {
		return nil
	}
	if autopilot.AssigneeType == "squad" {
		leader, _, err := s.resolveAutopilotLeader(ctx, autopilot)
		if err != nil {
			return fmt.Errorf("dispatch for webhook delivery: resolve squad leader: %w", err)
		}
		if _, err := s.TaskSvc.EnqueueTaskForSquadLeader(ctx, issue, leader.ID, autopilot.AssigneeID, pgtype.UUID{}); err != nil {
			return fmt.Errorf("dispatch for webhook delivery: repair squad task: %w", err)
		}
		return nil
	}
	if _, err := s.TaskSvc.EnqueueTaskForIssue(ctx, issue); err != nil {
		return fmt.Errorf("dispatch for webhook delivery: repair issue task: %w", err)
	}
	return nil
}

// DispatchAutopilotForPlan is the entry point for scheduled triggers that
// already know the canonical UTC plan_time of the occurrence they are
// firing. The plan_time is persisted on autopilot_run.planned_at, and the
// (trigger_id, planned_at) partial unique index — combined with this
// method's idempotent lookup — guarantees that the SAME planned occurrence
// cannot produce two SUCCESSFUL runs even if a stale-steal in
// sys_cron_executions re-enters this method after a prior attempt.
//
// Semantics for an already-existing run at (trigger_id, planned_at):
//
//   - If the existing run is COMPLETE (terminal status, or in-flight
//     with the appropriate downstream linkage — issue_id for
//     create_issue, task_id for run_only), it is returned unchanged.
//     The handler then writes SUCCESS in sys_cron_executions; no
//     duplicate issue/task is produced.
//   - If the existing run is in a PARTIAL state (a prior attempt
//     wrote the run row but crashed before creating its downstream
//     issue/task), it is marked FAILED with a recovery reason and
//     its planned_at is cleared, releasing the partial-unique slot.
//     Dispatch then proceeds normally and creates a fresh run at the
//     same plan_time. Without this branch, a crash-during-dispatch
//     would let a subsequent retry see the in-flight run, return it
//     unchanged, and let the scheduler mark the occurrence SUCCESS
//     without an actual issue/task ever being created (#4443 review).
//
// triggerID and plannedAt MUST both be valid; passing zero values
// would silently disable the idempotency guard. Manual / webhook /
// api callers should use DispatchAutopilot instead.
func (s *AutopilotService) DispatchAutopilotForPlan(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
	plannedAt time.Time,
) (*db.AutopilotRun, error) {
	if !triggerID.Valid {
		return nil, fmt.Errorf("dispatch for plan: trigger_id is required")
	}
	if plannedAt.IsZero() {
		return nil, fmt.Errorf("dispatch for plan: planned_at is required")
	}
	plannedTS := pgtype.Timestamptz{Time: plannedAt.UTC(), Valid: true}

	// Fast path: prior attempt already created a run for this exact
	// occurrence. The partial unique index uq_autopilot_run_trigger_planned
	// would also reject a duplicate INSERT, but doing the lookup up
	// front lets us short-circuit on a complete run and gives us a
	// chance to recover a partial run before retrying.
	existing, err := s.Queries.GetAutopilotRunByTriggerAndPlanned(ctx, db.GetAutopilotRunByTriggerAndPlannedParams{
		TriggerID: triggerID,
		PlannedAt: plannedTS,
	})
	switch {
	case err == nil && isAutopilotRunComplete(existing):
		// A prior attempt produced a complete run. Hand it back so the
		// handler can record SUCCESS in sys_cron_executions without
		// duplicating any downstream side effect.
		return &existing, nil

	case err == nil:
		// Partial-state run from a crashed attempt. Mark it failed
		// (with a recovery reason) and release its partial-unique
		// slot so the fresh dispatch below can create a new row.
		slog.Warn("autopilot dispatch for plan: recovering partial run",
			"run_id", util.UUIDToString(existing.ID),
			"trigger_id", util.UUIDToString(triggerID),
			"planned_at", plannedAt.UTC().Format(time.RFC3339),
			"status", existing.Status,
			"issue_set", existing.IssueID.Valid,
			"task_set", existing.TaskID.Valid,
		)
		if err := s.Queries.RecoverPartialAutopilotRun(ctx, existing.ID); err != nil {
			return nil, fmt.Errorf("dispatch for plan: recover partial run: %w", err)
		}
		// Fall through to a fresh dispatch below.

	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("dispatch for plan: lookup existing run: %w", err)
	}

	// Scheduled dispatch has no member actor → rule_owner attribution, and no
	// human surface for a per-run reason code, so it is dropped. No webhook
	// delivery on the scheduled-plan path.
	run, _, err := s.dispatchAutopilot(ctx, autopilot, triggerID, source, payload, plannedTS, pgtype.UUID{}, pgtype.UUID{})
	return run, err
}

// isAutopilotRunComplete decides whether an existing autopilot_run row
// for (trigger_id, planned_at) is safe to reuse on a stale-steal retry.
//
// A run is "complete" if either:
//
//   - It is in a terminal state (completed / failed / skipped). Nothing
//     more to do downstream; the caller can return it as-is.
//
//   - It is in-flight in a state whose downstream side effect is
//     observable:
//
//   - issue_created with a valid issue_id — the issue exists and
//     the issue-event listener owns task creation from here.
//
//   - running with a valid task_id — the task is queued, the
//     listener will close the run when the task terminates.
//
// Anything else — most importantly issue_created/running with NULL
// issue_id/task_id, or the brief 'pending' state — is a partial run:
// the run row was inserted before the dispatch path could create the
// downstream resource, and a stale-steal retry MUST NOT treat it as
// complete (#4443 review).
func isAutopilotRunComplete(run db.AutopilotRun) bool {
	switch run.Status {
	case "completed", "failed", "skipped":
		return true
	case "issue_created":
		return run.IssueID.Valid
	case "running":
		return run.TaskID.Valid
	default:
		return false
	}
}

// dispatchAutopilot is the shared core of the two public Dispatch entry
// points. plannedAt is the canonical UTC plan_time for scheduled triggers;
// for manual / webhook / api dispatch it is the zero pgtype.Timestamptz and
// the resulting autopilot_run row has planned_at IS NULL. webhookDeliveryID
// is set only by the durable webhook worker.
func (s *AutopilotService) dispatchAutopilot(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
	plannedAt pgtype.Timestamptz,
	webhookDeliveryID pgtype.UUID,
	actorUserID pgtype.UUID,
) (*db.AutopilotRun, dispatch.ReasonCode, error) {
	if reason, code, skip := s.shouldSkipDispatch(ctx, autopilot, actorUserID); skip {
		run, err := s.recordSkippedRun(ctx, autopilot, triggerID, source, payload, plannedAt, webhookDeliveryID, reason)
		return run, code, err
	}

	// Determine initial status based on execution mode.
	initialStatus := "issue_created"
	if autopilot.ExecutionMode == "run_only" {
		initialStatus = "running"
	}

	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:       autopilot.ID,
		TriggerID:         triggerID,
		Source:            source,
		Status:            initialStatus,
		TriggerPayload:    payload,
		SquadID:           autopilotSquadAttribution(autopilot),
		PlannedAt:         plannedAt,
		WebhookDeliveryID: webhookDeliveryID,
	})
	if err != nil {
		return nil, dispatch.ReasonInternalError, fmt.Errorf("create run: %w", err)
	}
	s.captureAutopilotRunStarted(autopilot, run, source)
	return s.dispatchAutopilotRun(ctx, autopilot, triggerID, source, &run, actorUserID)
}

// dispatchAutopilotRun performs the downstream side effect for an already
// persisted run. Keeping creation separate lets the webhook worker resume the
// same idempotency-anchored run after a crash between run creation and issue
// or task creation.
func (s *AutopilotService) dispatchAutopilotRun(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	run *db.AutopilotRun,
	actorUserID pgtype.UUID,
) (*db.AutopilotRun, dispatch.ReasonCode, error) {
	switch autopilot.ExecutionMode {
	case "create_issue":
		triggerTimezone := s.resolveAutopilotTriggerTimezone(ctx, triggerID)
		if err := s.dispatchCreateIssue(ctx, autopilot, run, triggerTimezone, actorUserID); err != nil {
			if skipped, code := s.handleDispatchSkip(ctx, autopilot, run, err); skipped != nil {
				return skipped, code, nil
			}
			s.failRun(ctx, run.ID, err.Error())
			s.captureAutopilotRunFailed(autopilot, *run, source, err.Error())
			return run, dispatchFailReasonCode(err), fmt.Errorf("dispatch create_issue: %w", err)
		}
	case "run_only":
		if err := s.dispatchRunOnly(ctx, autopilot, run, actorUserID); err != nil {
			if skipped, code := s.handleDispatchSkip(ctx, autopilot, run, err); skipped != nil {
				return skipped, code, nil
			}
			s.failRun(ctx, run.ID, err.Error())
			s.captureAutopilotRunFailed(autopilot, *run, source, err.Error())
			return run, dispatchFailReasonCode(err), fmt.Errorf("dispatch run_only: %w", err)
		}
	default:
		s.failRun(ctx, run.ID, "unknown execution_mode: "+autopilot.ExecutionMode)
		s.captureAutopilotRunFailed(autopilot, *run, source, "unknown execution_mode: "+autopilot.ExecutionMode)
		return run, dispatch.ReasonInternalError, fmt.Errorf("unknown execution_mode: %s", autopilot.ExecutionMode)
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

	return run, "", nil
}

// dispatchFailReasonCode types a dispatch error that fell through to failRun.
// It inspects the error with typed checks (never substring matching): a
// fail-closed attribution refusal is attribution_blocked; everything else is an
// unclassified internal error.
func dispatchFailReasonCode(err error) dispatch.ReasonCode {
	if errors.Is(err, ErrAttributionFailClosed) {
		return dispatch.ReasonAttributionBlocked
	}
	return dispatch.ReasonInternalError
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
func (s *AutopilotService) dispatchCreateIssue(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun, triggerTimezone string, actorUserID pgtype.UUID) error {
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

	if duplicate, found, err := issueguard.LockAndFindRecentAutopilotDuplicate(
		ctx, qtx, ap.WorkspaceID, ap.ID, ap.ProjectID, title, autopilotRecentDuplicateWindow,
	); err != nil {
		return fmt.Errorf("recent duplicate guard: %w", err)
	} else if found {
		return &errDispatchSkipped{reason: "recent duplicate autopilot issue: " + util.UUIDToString(duplicate.ID), code: dispatch.ReasonAlreadyActive}
	}

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

	// Fan out the default subscriber template inside the same tx as the
	// issue insert, before EventIssueCreated fires — so notification
	// listeners see the full subscriber set on the first event instead of
	// racing the listener that would otherwise hydrate the template.
	templateSubs, err := qtx.ListAutopilotSubscribers(ctx, ap.ID)
	if err != nil {
		return fmt.Errorf("list autopilot subscribers: %w", err)
	}
	for _, sub := range templateSubs {
		if err := qtx.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID:  issue.ID,
			UserType: sub.UserType,
			UserID:   sub.UserID,
			Reason:   "autopilot",
		}); err != nil {
			return fmt.Errorf("add autopilot subscriber to issue: %w", err)
		}
	}

	// Link the run inside the same tx as the issue insert. This makes the
	// recent-duplicate guard count only fully observable autopilot issues and
	// avoids a crash window where recovery would see an orphan issue but no
	// linked run.
	updatedRun, err := qtx.UpdateAutopilotRunIssueCreated(ctx, db.UpdateAutopilotRunIssueCreatedParams{
		ID:      run.ID,
		IssueID: issue.ID,
	})
	if err != nil {
		return fmt.Errorf("link run to issue: %w", err)
	}
	*run = updatedRun

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

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

	// The issue:created notification listener only handles handler.IssueResponse
	// payloads and only direct-notifies the assignee + @mentions; subscribers
	// don't get an inbox at creation time on the manual path because there are
	// none yet. The autopilot path is different: the template subscribers were
	// fanned out into issue_subscriber inside the tx above, so they exist at the
	// moment of creation and OQ3 says they should receive the same subscription
	// events as reason='manual'. Issue creation is one such event — so write
	// the inbox rows directly here. Done after commit so a failure here doesn't
	// roll back the issue itself.
	s.notifyAutopilotSubscribersOnCreate(ctx, ap, issue, leader.ID, templateSubs)

	// Enqueue agent task via the existing flow. Squad-assigned autopilots
	// route to the resolved leader as the executing agent (Path A from
	// MUL-2429); agent-assigned autopilots go through the standard issue
	// path. Both code paths land in agent_task_queue with agent_id = leader.
	// A MANUAL trigger (valid actorUserID) is a direct human action: enqueue via the
	// actor-carrying entry points so attribution resolves direct_human to the
	// triggering member (originator == accountable == actor, MUL-4302 §4). Schedule /
	// webhook dispatch has no actor and takes the plain entry points, where the
	// autopilot-origin issue resolves to rule_owner. The *WithHandoff variants are
	// the existing actor-carrying enqueue methods; the handoff note is empty here.
	if ap.AssigneeType == "squad" {
		// Fail-closed invocation gate: verify the admission principal (manual
		// clicker, else creator — see autopilotAdmitInvoke) may still invoke the
		// leader. Catches configs that predate the save-time gate, and configs
		// that no longer pass (MUL-3963 / MUL-4525).
		if !s.autopilotAdmitInvoke(ctx, ap, leader, actorUserID) {
			return fmt.Errorf("not allowed to invoke private squad leader")
		}
		if actorUserID.Valid {
			if _, err := s.TaskSvc.EnqueueTaskForSquadLeaderWithHandoff(ctx, issue, leader.ID, ap.AssigneeID, "", actorUserID); err != nil {
				return fmt.Errorf("enqueue squad leader task: %w", err)
			}
		} else if _, err := s.TaskSvc.EnqueueTaskForSquadLeader(ctx, issue, leader.ID, ap.AssigneeID, pgtype.UUID{}); err != nil {
			return fmt.Errorf("enqueue squad leader task: %w", err)
		}
	} else if actorUserID.Valid {
		if _, err := s.TaskSvc.EnqueueTaskForIssueWithHandoff(ctx, issue, "", actorUserID); err != nil {
			return fmt.Errorf("enqueue task for issue: %w", err)
		}
	} else if _, err := s.TaskSvc.EnqueueTaskForIssue(ctx, issue); err != nil {
		return fmt.Errorf("enqueue task for issue: %w", err)
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

// notifyAutopilotSubscribersOnCreate writes an inbox_item for each template
// subscriber of an autopilot-created issue and broadcasts an inbox:new event
// so the recipient's inbox updates in real time. Mirrors the inbox payload
// shape from notification_listeners.go so the WS consumer sees the same fields
// the listener-driven path produces. Failures are logged, not propagated:
// the issue and its subscriber rows are already committed, and an inbox-write
// hiccup must not bubble up as a dispatch failure.
func (s *AutopilotService) notifyAutopilotSubscribersOnCreate(
	ctx context.Context,
	ap db.Autopilot,
	issue db.Issue,
	leaderID pgtype.UUID,
	subscribers []db.AutopilotSubscriber,
) {
	if len(subscribers) == 0 {
		return
	}
	details, _ := json.Marshal(map[string]string{
		"autopilot_id": util.UUIDToString(ap.ID),
		"reason":       "autopilot",
	})
	for _, sub := range subscribers {
		// Autopilot subscribers are restricted to user_type='member' at the
		// handler boundary; defend in case that constraint is ever relaxed
		// (agents don't have inbox).
		if sub.UserType != "member" {
			continue
		}
		item, err := s.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   ap.WorkspaceID,
			RecipientType: "member",
			RecipientID:   sub.UserID,
			Type:          "issue_subscribed",
			Severity:      "info",
			IssueID:       issue.ID,
			Title:         issue.Title,
			Body:          pgtype.Text{},
			ActorType:     pgtype.Text{String: "agent", Valid: true},
			ActorID:       leaderID,
			Details:       details,
		})
		if err != nil {
			slog.Error("autopilot subscriber inbox write failed",
				"autopilot_id", util.UUIDToString(ap.ID),
				"issue_id", util.UUIDToString(issue.ID),
				"recipient_id", util.UUIDToString(sub.UserID),
				"error", err,
			)
			continue
		}
		s.Bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: util.UUIDToString(ap.WorkspaceID),
			ActorType:   "agent",
			ActorID:     util.UUIDToString(leaderID),
			Payload: map[string]any{
				"item": map[string]any{
					"id":             util.UUIDToString(item.ID),
					"workspace_id":   util.UUIDToString(item.WorkspaceID),
					"recipient_type": item.RecipientType,
					"recipient_id":   util.UUIDToString(item.RecipientID),
					"type":           item.Type,
					"severity":       item.Severity,
					"issue_id":       util.UUIDToPtr(item.IssueID),
					"issue_status":   issue.Status,
					"title":          item.Title,
					"body":           util.TextToPtr(item.Body),
					"read":           item.Read,
					"archived":       item.Archived,
					"created_at":     util.TimestampToString(item.CreatedAt),
					"actor_type":     util.TextToPtr(item.ActorType),
					"actor_id":       util.UUIDToPtr(item.ActorID),
					"details":        json.RawMessage(item.Details),
				},
			},
		})
	}
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
	// code is the stable, typed admission reason decided at THIS branch and
	// carried through to the response (MUL-4525) — never reverse-engineered from
	// the human-readable reason string above.
	code dispatch.ReasonCode
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
func (s *AutopilotService) dispatchRunOnly(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun, actorUserID pgtype.UUID) error {
	agent, _, err := s.resolveAutopilotLeader(ctx, ap)
	if err != nil {
		// Same admission-vs-failure classification as shouldSkipDispatch:
		// if the row disappeared or the squad was archived between
		// admission and dispatch, that is a skip, not a failure.
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errSquadArchived) {
			return &errDispatchSkipped{reason: formatAdmissionReason(ap, "assignee no longer resolvable"), code: dispatch.ReasonTargetUnavailable}
		}
		return fmt.Errorf("resolve leader: %w", err)
	}
	ready, reason, err := AgentReadiness(ctx, s.Queries, agent)
	if err != nil {
		return fmt.Errorf("check agent readiness: %w", err)
	}
	if !ready {
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, reason), code: agentReadinessReasonCode(agent)}
	}

	// Fail-closed invocation gate for squad autopilots (admission principal =
	// manual clicker, else creator — see autopilotAdmitInvoke).
	if ap.AssigneeType == "squad" && !s.autopilotAdmitInvoke(ctx, ap, agent, actorUserID) {
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, "not allowed to invoke private squad leader"), code: dispatch.ReasonInvocationNotAllowed}
	}

	// Attribution splits on the trigger. A MANUAL trigger is a direct human action:
	// the triggering member is direct_human and becomes BOTH originator (so the run
	// carries their authorization context) and accountable (MUL-4302 §4). A
	// schedule / webhook trigger has no human — originator_user_id stays NULL and
	// the audit-accountable human is the member currently RESPONSIBLE for the firing
	// trigger's effective config (its creator, then whoever last substantively edited
	// it) — trigger_owner, resolved from run.TriggerID (MUL-4302; Elon must-fix) —
	// degrading to the rule version publisher (rule_owner) when no such member is
	// recoverable, then to unattributed. Either way evidence points at the autopilot
	// run and the row is never a NULL-source bypass.
	var autopilotAttr attribution.Result
	if actorUserID.Valid {
		autopilotAttr = attribution.DirectHumanRun(actorUserID, attribution.EvidenceAutopilotRun, run.ID)
	} else {
		autopilotAttr = triggerOwnerAttribution(ctx, s.Queries, run.TriggerID, ap.WorkspaceID, ap.ID, attribution.EvidenceAutopilotRun, run.ID)
	}
	// If no precise human resolved (a version-less autopilot), degrade to
	// owner_fallback (accountable = agent owner), or skip the dispatch when the
	// workspace is fail-closed (MUL-4302 §3.5).
	autopilotAttr, err = s.TaskSvc.applyAttributionFallback(ctx, autopilotAttr, agent)
	if err != nil {
		return &errDispatchSkipped{reason: formatAdmissionReason(ap, "workspace fail-closed: no accountable human for autopilot run"), code: dispatch.ReasonAttributionBlocked}
	}
	apSource, _, apEvidenceKind, apEvidenceRef := attributionCreateParams(autopilotAttr)
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
		OriginatorUserID:     autopilotAttr.UserID,
		AccountableUserID:    autopilotAttr.AccountableUserID,
		RuleVersionID:        autopilotAttr.RuleVersionID,
		OriginatorSource:     apSource,
		TriggerEvidenceKind:  apEvidenceKind,
		TriggerEvidenceRefID: apEvidenceRef,
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

// SyncRunFromLinkedIssueTask fails a create_issue autopilot run when its
// linked issue task fails terminally before the issue itself reaches a
// terminal status. create_issue tasks are linked through issue_id rather than
// autopilot_run_id, so SyncRunFromTask cannot see them directly. Without this
// the run would hang in `issue_created` forever — and because the failure-rate
// auto-pause monitor excludes issue_created/running runs, a consistently
// failing autopilot would never trip the auto-pause either.
//
// "Terminal" means no task is still active for the issue. FailTask enqueues an
// auto-retry for infra-shaped failures (timeout, runtime offline/recovery,
// codex no-progress) BEFORE it broadcasts the failure event, so an active task
// here means another attempt is already in flight — we wait for it instead of
// failing the run prematurely. Once retries are exhausted (or the failure was
// never retryable in the first place), the run fails carrying the task's reason.
func (s *AutopilotService) SyncRunFromLinkedIssueTask(ctx context.Context, task db.AgentTaskQueue) {
	if task.AutopilotRunID.Valid || !task.IssueID.Valid || task.Status != "failed" {
		return
	}
	// Only create_issue runs link through issue_id (and their linked issue is
	// always origin_type=autopilot by construction), so a hit here both
	// identifies an in-flight create_issue run and bails the common case of
	// ordinary issue/chat task failures after a single query.
	run, err := s.Queries.GetAutopilotRunByIssue(ctx, task.IssueID)
	if err != nil {
		return // no active run linked to this issue
	}
	// A still-active task — typically the auto-retry FailTask just enqueued —
	// means the dispatch isn't terminal yet; wait for the final attempt.
	hasActive, err := s.Queries.HasActiveTaskForIssue(ctx, task.IssueID)
	if err != nil {
		slog.Warn("failed to check active tasks for autopilot issue failure",
			"issue_id", util.UUIDToString(task.IssueID),
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
		return
	}
	if hasActive {
		return
	}
	autopilot, err := s.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		return
	}

	reason := taskFailureReasonForAutopilotRun(task)
	updatedRun, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
		ID:            run.ID,
		FailureReason: pgtype.Text{String: reason, Valid: reason != ""},
	})
	if err != nil {
		slog.Warn("failed to fail autopilot run from linked issue task",
			"run_id", util.UUIDToString(run.ID),
			"issue_id", util.UUIDToString(task.IssueID),
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
		return
	}
	s.captureAutopilotRunFailed(autopilot, updatedRun, updatedRun.Source, reason)
	s.publishRunDone(util.UUIDToString(autopilot.WorkspaceID), updatedRun, "failed")
}

func taskFailureReasonForAutopilotRun(task db.AgentTaskQueue) string {
	if task.Error.Valid && strings.TrimSpace(task.Error.String) != "" {
		return task.Error.String
	}
	if task.FailureReason.Valid && strings.TrimSpace(task.FailureReason.String) != "" {
		return task.FailureReason.String
	}
	return "task failed"
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
func (s *AutopilotService) handleDispatchSkip(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun, err error) (*db.AutopilotRun, dispatch.ReasonCode) {
	var skipErr *errDispatchSkipped
	if !errors.As(err, &skipErr) {
		return nil, ""
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
		return nil, ""
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
	return run, skipErr.code
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
func (s *AutopilotService) shouldSkipDispatch(ctx context.Context, ap db.Autopilot, actorUserID pgtype.UUID) (string, dispatch.ReasonCode, bool) {
	if !ap.AssigneeID.Valid {
		return "autopilot has no assignee", dispatch.ReasonTargetUnavailable, true
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
			return "assignee squad is archived", dispatch.ReasonTargetUnavailable, true
		case missing && squadResolved:
			return "assignee squad cannot be resolved", dispatch.ReasonTargetUnavailable, true
		case missing && !squadResolved:
			// Agent row gone. With migration 096 the FK is gone too, so
			// this is the new "agent was hard-deleted under us" case. Skip
			// rather than fail-open: we know retrying will not help.
			return "assignee agent no longer exists", dispatch.ReasonTargetUnavailable, true
		}
		// Transient DB error — fail-open so the next scheduler tick gets a
		// chance to succeed.
		return "", "", false
	}
	ready, reason, err := AgentReadiness(ctx, s.Queries, agent)
	if err != nil {
		slog.Warn("autopilot admission: failed to load runtime",
			"autopilot_id", util.UUIDToString(ap.ID),
			"runtime_id", util.UUIDToString(agent.RuntimeID),
			"error", err,
		)
		return "", "", false
	}
	if !ready {
		if ap.ExecutionMode == "create_issue" && strings.HasPrefix(reason, "agent runtime is ") {
			slog.Info("autopilot admission: allowing create_issue dispatch for offline runtime",
				"autopilot_id", util.UUIDToString(ap.ID),
				"runtime_id", util.UUIDToString(agent.RuntimeID),
				"reason", reason,
			)
		} else {
			return formatAdmissionReason(ap, reason), agentReadinessReasonCode(agent), true
		}
	}
	// Invocation gate at the autopilot layer (MUL-3963 / MUL-4525). The
	// admission principal depends on how the dispatch was triggered: a MANUAL
	// "run now" (actorUserID valid) is a direct human action gated by the
	// current CLICKER's access — not the autopilot creator's — so admission and
	// attribution credit the same member and never fork. Automation (schedule /
	// webhook / api, actorUserID invalid) has no human in the loop and falls
	// back to the creator. Admins do NOT bypass a private agent they do not own;
	// agent-created autopilots are judged as workspace principals. For squad
	// autopilots the gate runs against the resolved leader.
	if !s.autopilotAdmitInvoke(ctx, ap, agent, actorUserID) {
		if actorUserID.Valid {
			return "you are not allowed to trigger this autopilot's assignee agent", dispatch.ReasonInvocationNotAllowed, true
		}
		return "autopilot creator lacks access to private assignee agent", dispatch.ReasonInvocationNotAllowed, true
	}
	return "", "", false
}

// agentReadinessReasonCode types the reason an AgentReadiness check failed from
// the agent's own state rather than the human-readable reason string (MUL-4525).
// An archived agent cannot run at all; anything else (no runtime bound, or a
// bound runtime that is not online) is a runtime-availability problem.
func agentReadinessReasonCode(agent db.Agent) dispatch.ReasonCode {
	if agent.ArchivedAt.Valid {
		return dispatch.ReasonTargetUnavailable
	}
	return dispatch.ReasonRuntimeOffline
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
	plannedAt pgtype.Timestamptz,
	webhookDeliveryID pgtype.UUID,
	reason string,
) (*db.AutopilotRun, error) {
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:       autopilot.ID,
		TriggerID:         triggerID,
		Source:            source,
		Status:            "skipped",
		TriggerPayload:    payload,
		SquadID:           autopilotSquadAttribution(autopilot),
		PlannedAt:         plannedAt,
		WebhookDeliveryID: webhookDeliveryID,
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

// canCreatorInvokeAgent checks whether the autopilot's creator may invoke the
// target agent under the invocation-permission model (MUL-3963). It mirrors
// handler.canInvokeAgent with the autopilot creator as the effective user:
//   - member creator who owns the agent -> always
//   - private agent -> only the owner (NO admin bypass, NO agent-created bypass)
//   - public_to agent -> workspace target admits any workspace-member creator
//     (and agent-created autopilots as workspace principals); member target
//     admits the matching creator; team targets are inert.
//
// Fail-closed on any lookup error.
// autopilotAdmitInvoke decides whether the dispatch's admission principal may
// invoke the target agent (MUL-4525). A MANUAL "run now" (actorUserID valid) is
// a direct human action gated by the CURRENT clicker's access, so admission and
// attribution credit the same member. Automation (schedule / webhook / api,
// actorUserID invalid) has no human in the loop and falls back to the autopilot
// creator. Both branches fail closed and never grant an admin bypass.
func (s *AutopilotService) autopilotAdmitInvoke(ctx context.Context, ap db.Autopilot, agent db.Agent, actorUserID pgtype.UUID) bool {
	if actorUserID.Valid {
		return s.canMemberInvokeAgent(ctx, agent, actorUserID, ap.WorkspaceID)
	}
	return s.canCreatorInvokeAgent(ctx, ap, agent)
}

// canMemberInvokeAgent checks whether a specific member may invoke the agent
// under the invocation-permission model (MUL-3963). It mirrors
// handler.canInvokeAgent with a member effective user — used for a manual
// autopilot "run now" where the clicker, not the creator, is the admission
// principal. Fail-closed on any lookup error; no admin bypass.
func (s *AutopilotService) canMemberInvokeAgent(ctx context.Context, agent db.Agent, memberUserID pgtype.UUID, workspaceID pgtype.UUID) bool {
	userID := util.UUIDToString(memberUserID)
	if userID == "" {
		return false
	}
	if util.UUIDToString(agent.OwnerID) == userID {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}
	targets, err := s.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}
	isWorkspaceMember := false
	if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      memberUserID,
		WorkspaceID: workspaceID,
	}); err == nil {
		isWorkspaceMember = true
	}
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isWorkspaceMember {
				return true
			}
		case "member":
			if util.UUIDToString(t.TargetID) == userID {
				return true
			}
		}
	}
	return false
}

func (s *AutopilotService) canCreatorInvokeAgent(ctx context.Context, ap db.Autopilot, agent db.Agent) bool {
	creatorID := util.UUIDToString(ap.CreatedByID)
	if ap.CreatedByType == "member" && util.UUIDToString(agent.OwnerID) == creatorID {
		return true
	}
	if agent.PermissionMode != "public_to" {
		// private (or unknown mode): deny-by-default; only the owner branch
		// above passes. Admins and agent-created autopilots do not bypass.
		return false
	}
	targets, err := s.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}
	// Agent-created autopilots are workspace-internal principals: a workspace
	// target admits them. Member creators must be workspace members.
	workspaceBroad := ap.CreatedByType == "agent"
	isWorkspaceMember := false
	if ap.CreatedByType == "member" {
		if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      ap.CreatedByID,
			WorkspaceID: ap.WorkspaceID,
		}); err == nil {
			isWorkspaceMember = true
		}
	}
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isWorkspaceMember || workspaceBroad {
				return true
			}
		case "member":
			if ap.CreatedByType == "member" && util.UUIDToString(t.TargetID) == creatorID {
				return true
			}
		}
	}
	return false
}
