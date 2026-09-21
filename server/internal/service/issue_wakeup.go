package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/eventcontract"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var ErrWakeupInput = errors.New("invalid wakeup")
var ErrWakeupConflict = errors.New("wakeup changed; refresh and retry")
var ErrWakeupForbidden = errors.New("wakeup permission denied")

// WakeupEventTypes is independent of plugin availability. These facts have
// transactional capture into matching receipts, including non-HTTP writers.
var WakeupEventTypes = eventcontract.WakeupTypes

type WakeupInput struct {
	AgentID         string     `json:"agent_id"`
	Instruction     string     `json:"instruction"`
	Kind            string     `json:"kind"`
	Mode            string     `json:"mode"`
	EventTypes      []string   `json:"event_types"`
	FilterAgentID   string     `json:"filter_agent_id"`
	FilterActorType string     `json:"filter_actor_type"`
	FilterActorID   string     `json:"filter_actor_id"`
	FilterTaskID    string     `json:"filter_task_id"`
	ParentCommentID string     `json:"parent_comment_id"`
	AfterSeconds    int64      `json:"after_seconds"`
	At              *time.Time `json:"at"`
	IntervalSeconds int64      `json:"interval_seconds"`
	CronExpression  string     `json:"cron_expression"`
	Timezone        string     `json:"timezone"`
}

type IssueWakeupService struct{ Tasks *TaskService }

func (s *IssueWakeupService) Validate(in *WakeupInput, now time.Time) (pgtype.Timestamptz, error) {
	bad := func(msg string) (pgtype.Timestamptz, error) {
		return pgtype.Timestamptz{}, fmt.Errorf("%w: %s", ErrWakeupInput, msg)
	}
	in.Instruction = strings.TrimSpace(in.Instruction)
	if len(in.Instruction) == 0 || len(in.Instruction) > 12000 {
		return bad("instruction must contain 1–12000 bytes")
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return bad("invalid timezone")
	}
	if in.Mode == "" {
		if in.Kind == "every" || in.Kind == "cron" {
			in.Mode = "continuous"
		} else {
			in.Mode = "once"
		}
	}
	if in.Mode != "once" && in.Mode != "continuous" {
		return bad("mode must be once or continuous")
	}
	if in.FilterActorType != "" || in.FilterActorID != "" {
		if in.Kind != "event" || (in.FilterActorType != "member" && in.FilterActorType != "agent") || in.FilterActorID == "" {
			return bad("actor filter requires an event, member or agent type, and actor ID")
		}
		if in.FilterAgentID != "" || in.FilterTaskID != "" {
			return bad("choose an actor filter or agent/run filters")
		}
		for _, event := range in.EventTypes {
			if strings.HasPrefix(event, "task.") {
				return bad("actor filters apply to issue, comment, reaction and attachment changes; use agent/run filters for task events")
			}
		}
	}
	switch in.Kind {
	case "event":
		if len(in.EventTypes) == 0 || len(in.EventTypes) > len(WakeupEventTypes) {
			return bad("select at least one supported event")
		}
		for _, e := range in.EventTypes {
			if slices.Contains(eventcontract.LifecycleTypes, e) {
				return bad(e + " cannot wake its own issue; subscriptions require an existing, open issue")
			}
			if !slices.Contains(WakeupEventTypes, e) {
				return bad("unsupported event: " + e)
			}
		}
		if in.FilterTaskID != "" {
			for _, e := range in.EventTypes {
				if !strings.HasPrefix(e, "task.") {
					return bad("task filter requires task events")
				}
			}
		}
		if in.AfterSeconds != 0 || in.At != nil || in.IntervalSeconds != 0 || in.CronExpression != "" {
			return bad("event wakeups cannot contain a schedule")
		}
		// Legacy mutation-only agent filters are an alias of actor=agent. Keep
		// mixed task/mutation subscriptions unchanged for existing API clients.
		if in.FilterAgentID != "" && in.FilterTaskID == "" {
			mutationOnly := true
			for _, event := range in.EventTypes {
				if strings.HasPrefix(event, "task.") {
					mutationOnly = false
				}
			}
			if mutationOnly {
				in.FilterActorType, in.FilterActorID = "agent", in.FilterAgentID
				in.FilterAgentID = ""
			}
		}
		return pgtype.Timestamptz{}, nil
	case "at":
		if in.Mode != "once" {
			return bad("single time requires once mode")
		}
		if (in.At == nil) == (in.AfterSeconds == 0) || in.AfterSeconds < 0 || in.AfterSeconds > 31536000 {
			return bad("provide at or after_seconds (1–31536000)")
		}
		if in.IntervalSeconds != 0 || in.CronExpression != "" {
			return bad("incompatible schedule fields")
		}
		if in.At != nil {
			return pgtype.Timestamptz{Time: in.At.UTC(), Valid: true}, nil
		}
		return pgtype.Timestamptz{Time: now.Add(time.Duration(in.AfterSeconds) * time.Second), Valid: true}, nil
	case "every":
		if in.Mode != "continuous" || in.IntervalSeconds < 60 || in.IntervalSeconds > 31536000 || in.At != nil || in.AfterSeconds != 0 || in.CronExpression != "" {
			return bad("every requires continuous mode and interval_seconds between 60 and 31536000")
		}
		return pgtype.Timestamptz{Time: now.Add(time.Duration(in.IntervalSeconds) * time.Second), Valid: true}, nil
	case "cron":
		if in.Mode != "continuous" || in.At != nil || in.AfterSeconds != 0 || in.IntervalSeconds != 0 {
			return bad("cron requires continuous mode without other schedule fields")
		}
		t, err := NextOccurrenceAfterUTC(in.CronExpression, in.Timezone, now)
		if err != nil || t.IsZero() {
			return bad("cron must have a future occurrence")
		}
		return pgtype.Timestamptz{Time: t, Valid: true}, nil
	default:
		return bad("kind must be event, at, every or cron")
	}
}

func wakeupUUID(s string) (pgtype.UUID, error) {
	if s == "" {
		return pgtype.UUID{}, nil
	}
	id, err := util.ParseUUID(s)
	if err != nil {
		return id, fmt.Errorf("%w: invalid UUID", ErrWakeupInput)
	}
	return id, nil
}

func wakeupIssueActive(ctx context.Context, q *db.Queries, i db.Issue) (bool, error) {
	category, err := issuestatus.CategoryWithError(ctx, q, i.WorkspaceID, i.Status)
	if err != nil {
		return false, err
	}
	return category != "done" && category != "closed", nil
}

func (s *IssueWakeupService) Create(ctx context.Context, issueID, member, source pgtype.UUID, in WakeupInput) (db.IssueWakeup, error) {
	return s.Save(ctx, issueID, member, source, pgtype.UUID{}, in)
}

type WakeupEnableInput struct {
	Revision int64      `json:"revision"`
	At       *time.Time `json:"at,omitempty"`
	Rearm    bool       `json:"rearm,omitempty"`
}

type WakeupInstructionInput struct {
	Instruction         string `json:"instruction"`
	ExpectedInstruction string `json:"expected_instruction"`
	Revision            int64  `json:"revision"`
}

// EditInstruction preserves the subscription revision and queued work. Compare
// both the revision and the prior text so concurrent edits cannot overwrite one
// another, without invalidating captured events or rearming a consumed rule.
func (s *IssueWakeupService) EditInstruction(ctx context.Context, issueID, id, member pgtype.UUID, in WakeupInstructionInput) error {
	in.Instruction = strings.TrimSpace(in.Instruction)
	if len(in.Instruction) == 0 || len(in.Instruction) > 12000 || in.Revision < 1 {
		return fmt.Errorf("%w: instruction must be 1–12000 bytes and revision is required", ErrWakeupInput)
	}
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.Tasks.Queries.WithTx(tx)
	var workspace pgtype.UUID
	if err = tx.QueryRow(ctx, "SELECT w.id FROM workspace w JOIN issue i ON i.workspace_id=w.id WHERE i.id=$1 FOR KEY SHARE OF w", issueID).Scan(&workspace); err != nil {
		return err
	}
	issue, err := q.LockWakeupIssue(ctx, issueID)
	if err != nil {
		return err
	}
	membership, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: member, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return ErrWakeupForbidden
	}
	w, err := q.LockIssueWakeup(ctx, id)
	if err != nil {
		return err
	}
	if w.IssueID != issue.ID || w.WorkspaceID != issue.WorkspaceID {
		return pgx.ErrNoRows
	}
	if w.CreatedBy != member && membership.Role != "owner" && membership.Role != "admin" {
		return ErrWakeupForbidden
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: w.AgentID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return ErrWakeupForbidden
	}
	if err = s.authorize(ctx, q, issue.WorkspaceID, member, agent); err != nil {
		return err
	}
	if w.Revision != in.Revision || w.Instruction != in.ExpectedInstruction {
		return ErrWakeupConflict
	}
	_, err = tx.Exec(ctx, "UPDATE issue_wakeup SET instruction=$2,updated_at=now() WHERE id=$1 AND workspace_id=$3", id, in.Instruction, workspace)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Enable reads the configuration under Save's locks; clients never round-trip
// instructions or filters. The revision fences stale toggles and duplicate rearm.
func (s *IssueWakeupService) Enable(ctx context.Context, issueID, member, source, id pgtype.UUID, in WakeupEnableInput) (db.IssueWakeup, error) {
	return s.save(ctx, issueID, member, source, id, WakeupInput{}, &in)
}

// Save replaces a subscription explicitly; revisions fence obsolete queued work.
func (s *IssueWakeupService) Save(ctx context.Context, issueID, member, source, existingID pgtype.UUID, in WakeupInput) (db.IssueWakeup, error) {
	return s.save(ctx, issueID, member, source, existingID, in, nil)
}

func (s *IssueWakeupService) save(ctx context.Context, issueID, member, source, existingID pgtype.UUID, in WakeupInput, enable *WakeupEnableInput) (db.IssueWakeup, error) {
	var out db.IssueWakeup
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	q := s.Tasks.Queries.WithTx(tx)
	var workspace pgtype.UUID
	if err = tx.QueryRow(ctx, "SELECT w.id FROM workspace w JOIN issue i ON i.workspace_id=w.id WHERE i.id=$1 FOR KEY SHARE OF w", issueID).Scan(&workspace); err != nil {
		return out, err
	}
	issue, err := q.LockWakeupIssue(ctx, issueID)
	if err != nil {
		return out, err
	}
	active, err := wakeupIssueActive(ctx, q, issue)
	if err != nil {
		return out, err
	}
	if !active {
		return out, fmt.Errorf("%w: issue is closed", ErrWakeupInput)
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		return out, err
	}

	if enable != nil {
		old, e := q.LockIssueWakeup(ctx, existingID)
		if e != nil {
			return out, e
		}
		if old.IssueID != issueID || old.WorkspaceID != issue.WorkspaceID {
			return out, pgx.ErrNoRows
		}
		if enable.Revision < 1 {
			return out, fmt.Errorf("%w: revision is required", ErrWakeupInput)
		}
		if old.Revision != enable.Revision {
			return out, ErrWakeupConflict
		}
		optionalID := func(id pgtype.UUID) string {
			if id.Valid {
				return util.UUIDToString(id)
			}
			return ""
		}
		in = WakeupInput{AgentID: optionalID(old.AgentID), Instruction: old.Instruction, Kind: old.Kind, Mode: old.Mode, EventTypes: old.EventTypes, FilterAgentID: optionalID(old.FilterAgentID), FilterTaskID: optionalID(old.FilterTaskID), FilterActorType: old.FilterActorType.String, FilterActorID: optionalID(old.FilterActorID), ParentCommentID: optionalID(old.ParentCommentID), IntervalSeconds: old.IntervalSeconds.Int64, CronExpression: old.CronExpression.String, Timezone: old.Timezone}
		if enable.At != nil && old.Kind != "at" {
			return out, fmt.Errorf("%w: only single-time wakeups accept a new time", ErrWakeupInput)
		}
		if !old.Enabled && old.Mode == "once" && (!old.DisabledAt.Valid || old.LastTaskID.Valid) {
			if !enable.Rearm {
				return out, fmt.Errorf("%w: consumed one-shot requires explicit rearm", ErrWakeupInput)
			}
			var activeRun bool
			if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agent_task_queue WHERE issue_id=$1 AND context->>'wakeup_id'=$2 AND status IN ('queued','deferred','dispatched','running','waiting_local_directory'))", issueID, optionalID(old.ID)).Scan(&activeRun); e != nil {
				return out, e
			}
			if activeRun {
				return out, fmt.Errorf("%w: previous run is still active", ErrWakeupConflict)
			}
		}
		if old.Kind == "at" {
			if old.NextFireAt.Valid {
				at := old.NextFireAt.Time
				in.At = &at
			}
			if enable.At != nil {
				in.At = enable.At
			}
			if !old.Enabled && (in.At == nil || !in.At.After(now)) {
				return out, fmt.Errorf("%w: choose a future time", ErrWakeupInput)
			}
		}
	}
	next, err := s.Validate(&in, now)
	if err != nil {
		return out, err
	}
	agentID, err := wakeupUUID(in.AgentID)
	if err != nil || !agentID.Valid {
		return out, fmt.Errorf("%w: agent_id is required", ErrWakeupInput)
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return out, ErrWakeupForbidden
	}
	if err = s.authorize(ctx, q, issue.WorkspaceID, member, agent); err != nil {
		return out, err
	}
	filterAgent, err := wakeupUUID(in.FilterAgentID)
	if err != nil {
		return out, err
	}
	filterTask, err := wakeupUUID(in.FilterTaskID)
	if err != nil {
		return out, err
	}
	filterActor, err := wakeupUUID(in.FilterActorID)
	if err != nil {
		return out, err
	}
	if filterActor.Valid {
		if in.FilterActorType == "member" {
			_, err = q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: filterActor, WorkspaceID: issue.WorkspaceID})
		} else {
			_, err = q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: filterActor, WorkspaceID: issue.WorkspaceID})
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrWakeupForbidden
		}
		if err != nil {
			return out, err
		}
	}
	parent, err := wakeupUUID(in.ParentCommentID)
	if err != nil {
		return out, err
	}
	if filterAgent.Valid {
		if _, err = q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: filterAgent, WorkspaceID: issue.WorkspaceID}); err != nil {
			return out, ErrWakeupForbidden
		}
	}
	if in.Kind != "event" && (len(in.EventTypes) > 0 || filterAgent.Valid || filterTask.Valid) {
		return out, fmt.Errorf("%w: time wakeup cannot contain event filters", ErrWakeupInput)
	}
	if parent.Valid {
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM comment WHERE id=$1 AND issue_id=$2 AND deleted_at IS NULL)", parent, issueID).Scan(&exists)
		if err != nil {
			return out, err
		}
		if !exists {
			return out, fmt.Errorf("%w: comment not found", ErrWakeupInput)
		}
	}
	if existingID.Valid {
		old, e := q.LockIssueWakeup(ctx, existingID)
		if e != nil {
			return out, e
		}
		if old.IssueID != issueID || old.WorkspaceID != issue.WorkspaceID {
			return out, pgx.ErrNoRows
		}
		membership, e := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: member, WorkspaceID: issue.WorkspaceID})
		if e != nil {
			return out, ErrWakeupForbidden
		}
		if old.CreatedBy != member && membership.Role != "owner" && membership.Role != "admin" {
			return out, ErrWakeupForbidden
		}
		if enable != nil && old.Enabled {
			if enable.Rearm || enable.At != nil {
				return out, ErrWakeupConflict
			}
			return old, tx.Commit(ctx)
		}
		if err = q.DiscardWakeupReceipts(ctx, old.ID); err != nil {
			return out, err
		}
		if _, err = q.CancelUnstartedWakeupTasks(ctx, util.UUIDToString(old.ID)); err != nil {
			return out, err
		}
	}
	var target db.AgentTaskQueue
	if filterTask.Valid {
		target, err = q.LockWakeupSourceTask(ctx, db.LockWakeupSourceTaskParams{ID: filterTask, IssueID: issueID})
		if errors.Is(err, pgx.ErrNoRows) {
			return out, fmt.Errorf("%w: run does not belong to this issue", ErrWakeupInput)
		}
		if err != nil {
			return out, err
		}
		if filterAgent.Valid && filterAgent != target.AgentID {
			return out, fmt.Errorf("%w: run and agent filters disagree", ErrWakeupInput)
		}
	}
	params := db.CreateIssueWakeupParams{ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AgentID: agent.ID, CreatedBy: member, SourceTaskID: source, ParentCommentID: parent, Instruction: in.Instruction, Kind: in.Kind, Mode: in.Mode, EventTypes: append([]string{}, in.EventTypes...), FilterAgentID: filterAgent, FilterTaskID: filterTask, FilterActorType: pgtype.Text{String: in.FilterActorType, Valid: in.FilterActorType != ""}, FilterActorID: filterActor, IntervalSeconds: pgtype.Int8{Int64: in.IntervalSeconds, Valid: in.IntervalSeconds > 0}, CronExpression: pgtype.Text{String: in.CronExpression, Valid: in.CronExpression != ""}, Timezone: in.Timezone, NextFireAt: next}
	if existingID.Valid {
		_, err = tx.Exec(ctx, `UPDATE issue_wakeup SET agent_id=$2,created_by=$3,source_task_id=$4,parent_comment_id=$5,instruction=$6,kind=$7,mode=$8,event_types=$9,filter_agent_id=$10,filter_task_id=$11,interval_seconds=$12,cron_expression=$13,timezone=$14,next_fire_at=$15,filter_actor_type=$16,filter_actor_id=$17,enabled=true,disabled_at=NULL,revision=revision+1,last_task_id=NULL,last_error=NULL,updated_at=now() WHERE id=$1`, existingID, params.AgentID, member, source, parent, in.Instruction, in.Kind, in.Mode, params.EventTypes, filterAgent, filterTask, params.IntervalSeconds, params.CronExpression, in.Timezone, next, params.FilterActorType, filterActor)
		if err == nil {
			out, err = q.LockIssueWakeup(ctx, existingID)
		}
	} else {
		out, err = q.CreateIssueWakeup(ctx, params)
	}
	if err != nil {
		return out, err
	}
	// Subscribe and inspect the concrete source under the same row lock. This
	// closes the already-terminal race even if the caller exits immediately.
	if filterTask.Valid && slices.Contains([]string{"completed", "failed", "cancelled"}, target.Status) && slices.Contains(in.EventTypes, eventcontract.TaskEvent(target.Status)) {
		payload, _ := json.Marshal(map[string]any{
			"event_id": util.UUIDToString(target.ID) + ":" + target.Status, "event_type": eventcontract.TaskEvent(target.Status), "version": 1,
			"occurred_at": target.CompletedAt, "observed_at": now, "registration_snapshot": true,
			"workspace_id": util.UUIDToString(issue.WorkspaceID), "issue_id": util.UUIDToString(issue.ID),
			"task_id": util.UUIDToString(target.ID), "source_task_id": util.UUIDToString(target.ID), "status": target.Status,
			"agent_id": util.UUIDToString(target.AgentID), "actor_type": "agent", "actor_id": util.UUIDToString(target.AgentID),
			"retry_of_task_id": target.RetryOfTaskID, "rerun_of_task_id": target.RerunOfTaskID,
		})
		_, err = q.RecordWakeupReceipt(ctx, db.RecordWakeupReceiptParams{ID: dbid.NewV7(), WakeupID: out.ID, Revision: out.Revision, EventKey: util.UUIDToString(target.ID) + ":" + target.Status, EventType: "task." + target.Status, Payload: payload})
		if err != nil {
			return out, err
		}
		if out.Mode == "once" {
			out.Enabled = false
			err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: out.ID, Enabled: false})
			if err != nil {
				return out, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}

func (s *IssueWakeupService) authorize(ctx context.Context, q *db.Queries, ws, member pgtype.UUID, a db.Agent) error {
	if !member.Valid || a.ArchivedAt.Valid || !a.RuntimeID.Valid {
		return ErrWakeupForbidden
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: member, WorkspaceID: ws}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWakeupForbidden
		}
		return err
	}
	if !CanMemberInvokeAgent(ctx, q, a, member, ws) {
		return ErrWakeupForbidden
	}
	return nil
}

// Tick uses the existing scheduler lease. Each config has a row lock as well,
// so a manual retry or a second server cannot dispatch the same receipt twice.
func (s *IssueWakeupService) Tick(ctx context.Context) error {
	// Receipts are operational evidence, not the run history. Match the existing
	// event telemetry's seven-day retention; never expire unprocessed inputs.
	cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 2*time.Second)
	_, cleanupErr := s.Tasks.Queries.DeleteExpiredWakeupReceipts(cleanupCtx, pgtype.Timestamptz{Time: time.Now().Add(-7 * 24 * time.Hour), Valid: true})
	cleanupCancel()
	rows, err := s.Tasks.Queries.ListReadyWakeups(ctx)
	if err != nil {
		return err
	}
	var errs []error
	if cleanupErr != nil {
		errs = append(errs, fmt.Errorf("expire wakeup receipts: %w", cleanupErr))
	}
	for _, w := range rows {
		if ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}
		// Outcome writes must not turn a busy rule row into another batch-wide
		// wait. Use the batch context, not dispatch's expired per-rule context.
		if err = s.dispatch(ctx, w); err != nil {
			errs = append(errs, fmt.Errorf("wakeup %s: %w", util.UUIDToString(w.ID), err))
			outcomeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			_ = s.Tasks.Queries.NoteWakeupFailure(outcomeCtx, db.NoteWakeupFailureParams{ID: w.ID, LastError: pgtype.Text{String: truncateForSummary(err.Error(), 500), Valid: true}})
			cancel()
		}
		outcomeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		if touchErr := s.Tasks.Queries.TouchWakeupDispatch(outcomeCtx, w.ID); touchErr != nil {
			errs = append(errs, touchErr)
		}
		cancel()
	}
	return errors.Join(errs...)
}

func (s *IssueWakeupService) dispatch(ctx context.Context, prev db.IssueWakeup) error {
	// A rule gets at most two seconds including credentials and SQL. Lock waits
	// below are shorter: even 100 contended rules consume only ~5s of the 45s
	// batch budget, leaving time to dispatch unrelated issues.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// Resolve optional connected-app credentials before taking database locks.
	candidate, readErr := s.Tasks.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: prev.AgentID, WorkspaceID: prev.WorkspaceID})
	if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
		return readErr
	}
	var overlay runtimeMCPOverlayData
	if candidate.ID.Valid && s.authorize(ctx, s.Tasks.Queries, prev.WorkspaceID, prev.CreatedBy, candidate) == nil {
		overlay = s.Tasks.buildRuntimeMCPOverlay(ctx, prev.CreatedBy, candidate)
	}
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout = '50ms'"); err != nil {
		return err
	}
	q := s.Tasks.Queries.WithTx(tx)
	var fenced bool
	if candidate.ID.Valid {
		if err = tx.QueryRow(ctx, "SELECT lock_task_owner_rows($1,$2,$3)", candidate.ID, prev.IssueID, candidate.RuntimeID).Scan(&fenced); err != nil {
			return err
		}
		if !fenced {
			return pgx.ErrNoRows
		}
	} else {
		if _, err = q.LockWorkspaceForChatSessionCreate(ctx, prev.WorkspaceID); err != nil {
			return err
		}
	}
	issue, err := q.LockWakeupIssue(ctx, prev.IssueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return qCleanupMissingWakeup(ctx, tx, prev.ID)
	}
	if err != nil {
		return err
	}
	w, err := q.LockIssueWakeup(ctx, prev.ID)
	if err != nil {
		return err
	}
	if w.Revision != prev.Revision {
		return tx.Commit(ctx)
	}
	active, err := wakeupIssueActive(ctx, q, issue)
	if err != nil {
		return err
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: w.AgentID, WorkspaceID: w.WorkspaceID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if agent.ID.Valid && agent.RuntimeID != candidate.RuntimeID {
		return tx.Commit(ctx)
	}
	authErr := s.authorize(ctx, q, w.WorkspaceID, w.CreatedBy, agent)
	if w.DisabledAt.Valid || !active || authErr != nil {
		if authErr != nil && !errors.Is(authErr, ErrWakeupForbidden) {
			return authErr
		}
		_, err = tx.Exec(ctx, "UPDATE issue_wakeup SET enabled=false,disabled_at=COALESCE(disabled_at,now()),last_error=$2 WHERE id=$1", w.ID, "Wakeup disabled: issue closed or permission unavailable")
		if err != nil {
			return err
		}
		if err = q.DiscardWakeupReceipts(ctx, w.ID); err != nil {
			return err
		}
		if _, err = q.CancelUnstartedWakeupTasks(ctx, util.UUIDToString(w.ID)); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, "UPDATE issue_wakeup_receipt SET processed_at=now() WHERE wakeup_id=$1 AND revision<>$2 AND processed_at IS NULL", w.ID, w.Revision); err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		return err
	}
	next := w.NextFireAt
	enabled := w.Enabled
	if w.Mode == "once" && w.LastTaskID.Valid {
		if err := q.DiscardWakeupReceipts(ctx, w.ID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if w.Enabled && w.Kind != "event" && next.Valid && !next.Time.After(now) {
		planned := next.Time
		switch w.Kind {
		case "at":
			enabled = false
			next = pgtype.Timestamptz{}
		case "every":
			step := time.Duration(w.IntervalSeconds.Int64) * time.Second
			planned = planned.Add(now.Sub(planned) / step * step)
			next.Time = planned.Add(step)
		case "cron":
			next.Time, err = NextOccurrenceAfterUTC(w.CronExpression.String, w.Timezone, now)
			if err != nil {
				return err
			}
			if next.Time.IsZero() {
				enabled = false
				next.Valid = false
			}
		}
		payload, _ := json.Marshal(map[string]any{"planned_at": planned.UTC().Format(time.RFC3339), "kind": w.Kind})
		var latest db.IssueWakeupReceipt
		latest, err = q.RecordWakeupReceipt(ctx, db.RecordWakeupReceiptParams{ID: dbid.NewV7(), WakeupID: w.ID, Revision: w.Revision, EventKey: planned.UTC().Format(time.RFC3339Nano), EventType: "time.due", Payload: payload})
		if err == nil {
			_, err = tx.Exec(ctx, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND event_type='time.due' AND processed_at IS NULL AND id<>$2", w.ID, latest.ID)
		}
		if err != nil {
			return err
		}
	}
	task, err := q.FindPendingWakeupTask(ctx, util.UUIDToString(w.ID))
	if err == nil && task.Status == "dispatched" {
		// A claimed prompt is immutable. Recovery belongs to the ordinary
		// claim/prepare lease, not a second wakeup-specific task timeout.
		var waiting pgtype.Text
		if task.DispatchedAt.Valid && now.Sub(task.DispatchedAt.Time) >= claimResponseRecoveryWindow &&
			(!task.PrepareLeaseExpiresAt.Valid || !task.PrepareLeaseExpiresAt.Time.After(now)) {
			waiting = pgtype.Text{String: "Waiting for claimed run " + util.UUIDToString(task.ID) + " to start or recover; new trigger inputs are retained.", Valid: true}
		}
		// Persist timer progress even while waiting; do not regenerate each
		// elapsed tick. Keep once enabled until its pending input is assigned.
		if err := q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: w.Enabled, NextFireAt: next, LastError: waiting}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	taskExists := err == nil
	receipts, err := q.ListPendingWakeupReceipts(ctx, db.ListPendingWakeupReceiptsParams{WakeupID: w.ID, Revision: w.Revision})
	if err != nil {
		return err
	}
	if len(receipts) == 0 {
		return tx.Commit(ctx)
	}
	ids := make([]pgtype.UUID, 0, len(receipts))
	for _, r := range receipts {
		ids = append(ids, r.ID)
	}
	if w.Mode == "once" {
		enabled = false
	}
	note, evidence := mergeWakeupEvidence(w, task, receipts)
	if taskExists {
		task, err = q.ReplaceWakeupEvidence(ctx, db.ReplaceWakeupEvidenceParams{ID: task.ID, HandoffNote: pgtype.Text{String: note, Valid: true}, WakeupEvidence: evidence})
	} else {
		if err = guardIssueNotInTriage(ctx, q, issue.ID, OriginNamed); err != nil {
			return err
		}
		contextJSON, _ := json.Marshal(map[string]any{"wakeup_id": util.UUIDToString(w.ID), "wakeup_revision": w.Revision, "wakeup_evidence": evidence})
		task, err = q.CreateWakeupTask(ctx, db.CreateWakeupTaskParams{ID: dbid.NewV7(), AgentID: w.AgentID, RuntimeID: agent.RuntimeID, IssueID: w.IssueID, Priority: priorityToInt(issue.Priority), TriggerCommentID: w.ParentCommentID, TriggerSummary: pgtype.Text{String: "Wakeup: " + truncateForSummary(w.Instruction, 160), Valid: true}, HandoffNote: pgtype.Text{String: note, Valid: true}, OriginatorUserID: w.CreatedBy, AccountableUserID: w.CreatedBy, OriginatorSource: pgtype.Text{String: "trigger_owner", Valid: true}, TriggerEvidenceKind: pgtype.Text{String: "issue_wakeup", Valid: true}, TriggerEvidenceRefID: w.ID, DelegatedFromTaskID: w.SourceTaskID, WakeupContext: contextJSON, RuntimeMcpOverlay: overlay.Overlay, RuntimeConnectedApps: overlay.ConnectedApps})
	}
	if err != nil {
		return err
	}
	if err = q.ConsumeWakeupReceipts(ctx, db.ConsumeWakeupReceiptsParams{Ids: ids, TaskID: task.ID}); err != nil {
		return err
	}
	if err = q.AdvanceIssueWakeup(ctx, db.AdvanceIssueWakeupParams{ID: w.ID, Enabled: enabled, NextFireAt: next, LastTaskID: task.ID}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.Tasks.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.Tasks.NotifyTaskEnqueued(ctx, task)
	return nil
}

func qCleanupMissingWakeup(ctx context.Context, tx pgx.Tx, id pgtype.UUID) error {
	if _, err := tx.Exec(ctx, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id=$1", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM issue_wakeup WHERE id=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Disable withdraws only work not yet claimed. A running task keeps the
// ordinary Stop control; closing a subscription never kills unrelated work.
func (s *IssueWakeupService) Disable(ctx context.Context, issueID, id, member pgtype.UUID) (db.IssueWakeup, error) {
	var out db.IssueWakeup
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	q := s.Tasks.Queries.WithTx(tx)
	var workspace pgtype.UUID
	if err = tx.QueryRow(ctx, "SELECT w.id FROM workspace w JOIN issue i ON i.workspace_id=w.id WHERE i.id=$1 FOR KEY SHARE OF w", issueID).Scan(&workspace); err != nil {
		return out, err
	}
	issue, err := q.LockWakeupIssue(ctx, issueID)
	if err != nil {
		return out, err
	}
	membership, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: member, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return out, ErrWakeupForbidden
	}
	out, err = q.LockIssueWakeup(ctx, id)
	if err != nil {
		return out, err
	}
	if out.IssueID != issue.ID || out.WorkspaceID != issue.WorkspaceID {
		return db.IssueWakeup{}, pgx.ErrNoRows
	}
	if out.CreatedBy != member && membership.Role != "owner" && membership.Role != "admin" {
		return db.IssueWakeup{}, ErrWakeupForbidden
	}
	_, err = tx.Exec(ctx, "UPDATE issue_wakeup SET enabled=false,disabled_at=COALESCE(disabled_at,now()),updated_at=now() WHERE id=$1", id)
	if err != nil {
		return out, err
	}
	if err = q.DiscardWakeupReceipts(ctx, id); err != nil {
		return out, err
	}
	tasks, err := q.CancelUnstartedWakeupTasks(ctx, util.UUIDToString(id))
	if err != nil {
		return out, err
	}
	out, err = q.LockIssueWakeup(ctx, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	for _, task := range tasks {
		s.Tasks.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)
	}
	return out, nil
}

// CheckClaim revalidates stored human authority after an offline wait. The
// enqueue-time MCP overlay does not grant permission to execute forever.
func (s *IssueWakeupService) CheckClaim(ctx context.Context, task db.AgentTaskQueue) error {
	var source struct {
		ID       string `json:"wakeup_id"`
		Revision int64  `json:"wakeup_revision"`
	}
	if err := json.Unmarshal(task.Context, &source); err != nil || source.ID == "" {
		return nil
	}
	id, err := wakeupUUID(source.ID)
	if err != nil {
		return ErrWakeupForbidden
	}
	w, err := s.Tasks.Queries.LocklessWakeup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWakeupForbidden
	}
	if err != nil {
		return err
	}
	if w.DisabledAt.Valid || w.Revision != source.Revision || w.IssueID != task.IssueID || w.AgentID != task.AgentID || w.CreatedBy != task.OriginatorUserID {
		return ErrWakeupForbidden
	}
	agent, err := s.Tasks.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: w.AgentID, WorkspaceID: w.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWakeupForbidden
	}
	if err != nil {
		return err
	}
	return s.authorize(ctx, s.Tasks.Queries, w.WorkspaceID, w.CreatedBy, agent)
}
