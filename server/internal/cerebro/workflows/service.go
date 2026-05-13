package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
)

// retryBackoffs are the spec-locked delays applied to attempts 2, 3, 4. The
// first attempt runs immediately. After the 4th failure the engine creates
// an escalation sub-issue under the triggered issue and marks the run
// escalated — no further retries.
var retryBackoffs = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

const maxAttempts = 4 // 1 initial + 3 retries; matches len(retryBackoffs)+1

// nowFunc is overridden in tests so we can verify backoff math without
// sleeping.
var nowFunc = time.Now

// Service is the runtime engine. One per process, attached to the bus by
// NewListener(svc).Attach(bus) at server start.
type Service struct {
	queries *cerebrodb.Queries
	issues  IssueActions
	bus     *events.Bus

	// enabled is the env-gated phase-1 master switch. PR 2 ships the
	// per-user `cerebro_workflows` UI flag alongside this env var; the env
	// var stays as the master kill switch so an ops-driven disable doesn't
	// require a UI round-trip.
	enabled bool
}

// New builds a Service. enabled is true when the CEREBRO_WORKFLOWS_ENABLED
// env var is set to a truthy value (1, true, yes); otherwise the engine
// still receives bus events but no-ops on them. The bus is needed by the
// send_reminder action for inbox:new fan-out — passing nil there disables
// the live notification but keeps the inbox row write working.
func New(queries *cerebrodb.Queries, issues IssueActions, bus *events.Bus) *Service {
	return &Service{
		queries: queries,
		issues:  issues,
		bus:     bus,
		enabled: envFlagEnabled("CEREBRO_WORKFLOWS_ENABLED"),
	}
}

// Enabled reports whether the engine should act on events. Exported so the
// listener can short-circuit before doing any work on a hot path.
func (s *Service) Enabled() bool { return s.enabled }

// workflow is the engine's flat projection of a cerebrodb.CerebroWorkflow
// row. Mostly an alias, but separating it lets test code build a workflow
// without depending on sqlc-generated types.
//
// editorMode / editorLayout (phase 2) are not used by the runtime engine
// itself — they exist purely to round-trip the column data through the
// engine in case a future trigger needs to inspect them. The handler
// reads them via the cerebrodb row directly, not through this struct.
type workflow struct {
	id            pgtype.UUID
	workspaceID   pgtype.UUID
	projectID     pgtype.UUID
	triggerType   string
	triggerConfig []byte
	conditions    []byte
	actionType    string
	actionConfig  []byte
	createdByID   pgtype.UUID
	createdByType string
}

// Dispatch is the listener-facing entry point: load matching workflows,
// filter to the ones that should fire, and execute each via the retry-aware
// inner Execute.
func (s *Service) Dispatch(ctx context.Context, te TriggerEvent) error {
	if !s.enabled {
		return nil
	}
	wsID, err := parseUUID(te.WorkspaceID)
	if err != nil {
		return fmt.Errorf("dispatch: workspace_id: %w", err)
	}

	rows, err := s.queries.ListCerebroWorkflowsForTrigger(ctx, cerebrodb.ListCerebroWorkflowsForTriggerParams{
		WorkspaceID: wsID,
		TriggerType: te.Type,
	})
	if err != nil {
		return fmt.Errorf("dispatch: list workflows: %w", err)
	}

	for _, row := range rows {
		wf := workflow{
			id:            row.ID,
			workspaceID:   row.WorkspaceID,
			projectID:     row.ProjectID,
			triggerType:   row.TriggerType,
			triggerConfig: row.TriggerConfig,
			conditions:    row.Conditions,
			actionType:    row.ActionType,
			actionConfig:  row.ActionConfig,
			createdByID:   row.CreatedByID,
			createdByType: row.CreatedByType,
		}
		if !triggerMatches(wf, te) {
			continue
		}
		if !s.conditionsHold(ctx, wf, te) {
			continue
		}
		if err := s.Execute(ctx, wf, te); err != nil {
			slog.Error("workflow execute failed",
				"workflow_id", uuidString(wf.id),
				"issue_id", te.IssueID,
				"error", err,
			)
		}
	}
	return nil
}

// Execute claims the (workflow, event) pair via the idempotency table,
// records a workflow_run, runs the action, and on failure schedules a retry
// or escalates. Returning an error from Execute signals an *engine* error
// (DB unreachable, JSON malformed) — action failures are absorbed into the
// run row and the retry ladder.
func (s *Service) Execute(ctx context.Context, wf workflow, te TriggerEvent) error {
	key := IdempotencyKey(te.EventID, uuidString(wf.id))
	claimed, err := s.queries.InsertCerebroWorkflowIdempotencyKey(ctx, cerebrodb.InsertCerebroWorkflowIdempotencyKeyParams{
		Key:        key,
		WorkflowID: wf.id,
		RunID:      pgtype.UUID{},
	})
	if err != nil {
		return fmt.Errorf("idempotency claim: %w", err)
	}
	if claimed == 0 {
		// Another listener call already handled this event.
		return nil
	}

	runEvent, _ := json.Marshal(te)
	issueID, _ := optionalUUID(te.IssueID)
	run, err := s.queries.CreateCerebroWorkflowRun(ctx, cerebrodb.CreateCerebroWorkflowRunParams{
		WorkflowID:    wf.id,
		WorkspaceID:   wf.workspaceID,
		TriggerEvent:  runEvent,
		TargetIssueID: issueID,
		Status:        "running",
		Attempt:       1,
		StartedAt:     pgtype.Timestamptz{Time: nowFunc(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	return s.attempt(ctx, run.ID, wf, te, 1)
}

// attempt runs action for the given attempt number, then dispatches to the
// success/failure/escalation paths. Pulled out so the retry sweeper can
// re-enter at higher attempt numbers without re-creating the run row.
func (s *Service) attempt(ctx context.Context, runID pgtype.UUID, wf workflow, te TriggerEvent, attemptN int) error {
	actErr := s.runAction(ctx, wf, te)
	if actErr == nil {
		// Future PR will attach task_id here once the action surface grows
		// to enqueue agent_task_queue rows; today's actions are direct
		// mutations so there's no task to link.
		if err := s.queries.MarkCerebroWorkflowRunSuccess(ctx, cerebrodb.MarkCerebroWorkflowRunSuccessParams{
			ID:     runID,
			TaskID: pgtype.UUID{},
		}); err != nil {
			return fmt.Errorf("mark success: %w", err)
		}
		return nil
	}

	// Action failed — schedule the next attempt or escalate.
	if attemptN >= maxAttempts {
		return s.escalate(ctx, runID, wf, te, actErr)
	}

	nextAt := nowFunc().Add(retryBackoffs[attemptN-1])
	if err := s.queries.MarkCerebroWorkflowRunFailed(ctx, cerebrodb.MarkCerebroWorkflowRunFailedParams{
		ID:          runID,
		Error:       pgtype.Text{String: actErr.Error(), Valid: true},
		NextRetryAt: pgtype.Timestamptz{Time: nextAt, Valid: true},
	}); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}

// escalate creates a sub-issue under the triggered issue, assigned to the
// workflow owner, and marks the run as escalated. Pulled out so the message
// composition lives in one spot and the retry sweeper reuses it.
func (s *Service) escalate(ctx context.Context, runID pgtype.UUID, wf workflow, te TriggerEvent, lastErr error) error {
	if te.IssueID == "" {
		// No parent issue to attach the escalation to; just terminate.
		return s.queries.MarkCerebroWorkflowRunEscalated(ctx, runID)
	}
	esc := workflow{
		id:            wf.id,
		workspaceID:   wf.workspaceID,
		createdByID:   wf.createdByID,
		createdByType: wf.createdByType,
		actionType:    ActionCreateSubIssue,
		actionConfig: mustJSON(ActionConfigCreateSubIssue{
			Title:        "Workflow failed after 4 attempts",
			Description:  "Workflow " + uuidString(wf.id) + " failed:\n" + lastErr.Error(),
			AssigneeID:   uuidString(wf.createdByID),
			AssigneeType: wf.createdByType,
		}),
	}
	if _, err := s.actionCreateSubIssue(ctx, esc, te); err != nil {
		// Even escalation failed — surface in the run row, but don't loop.
		return s.queries.MarkCerebroWorkflowRunFailed(ctx, cerebrodb.MarkCerebroWorkflowRunFailedParams{
			ID:          runID,
			Error:       pgtype.Text{String: "escalation failed: " + err.Error(), Valid: true},
			NextRetryAt: pgtype.Timestamptz{},
		})
	}
	return s.queries.MarkCerebroWorkflowRunEscalated(ctx, runID)
}

// RunRetrySweeper polls for failed runs whose next_retry_at has elapsed and
// reattempts them. Started as a goroutine at server boot. The sweeper is the
// only place attempt numbers > 1 are realized — callers cannot bypass it.
func (s *Service) RunRetrySweeper(ctx context.Context, tick time.Duration) {
	if tick <= 0 {
		tick = 30 * time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !s.enabled {
				continue
			}
			if err := s.sweepRetries(ctx); err != nil {
				slog.Warn("workflow retry sweep failed", "error", err)
			}
		}
	}
}

func (s *Service) sweepRetries(ctx context.Context) error {
	rows, err := s.queries.ListPendingCerebroWorkflowRunRetries(ctx, cerebrodb.ListPendingCerebroWorkflowRunRetriesParams{
		NextRetryAt: pgtype.Timestamptz{Time: nowFunc(), Valid: true},
		Limit:       100,
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		wfRow, err := s.queries.GetCerebroWorkflow(ctx, row.WorkflowID)
		if err != nil {
			slog.Warn("workflow retry: workflow gone", "run_id", uuidString(row.ID), "error", err)
			continue
		}
		var te TriggerEvent
		if err := json.Unmarshal(row.TriggerEvent, &te); err != nil {
			slog.Warn("workflow retry: bad trigger event", "run_id", uuidString(row.ID), "error", err)
			continue
		}
		next := int(row.Attempt) + 1
		if err := s.queries.BumpCerebroWorkflowRunAttempt(ctx, cerebrodb.BumpCerebroWorkflowRunAttemptParams{
			ID:      row.ID,
			Attempt: int32(next),
		}); err != nil {
			slog.Warn("workflow retry: bump attempt", "run_id", uuidString(row.ID), "error", err)
			continue
		}
		wf := workflow{
			id:            wfRow.ID,
			workspaceID:   wfRow.WorkspaceID,
			projectID:     wfRow.ProjectID,
			triggerType:   wfRow.TriggerType,
			triggerConfig: wfRow.TriggerConfig,
			conditions:    wfRow.Conditions,
			actionType:    wfRow.ActionType,
			actionConfig:  wfRow.ActionConfig,
			createdByID:   wfRow.CreatedByID,
			createdByType: wfRow.CreatedByType,
		}
		if err := s.attempt(ctx, row.ID, wf, te, next); err != nil {
			slog.Warn("workflow retry: attempt failed", "run_id", uuidString(row.ID), "error", err)
		}
	}
	return nil
}

// triggerMatches checks the trigger_config-side conditions (status-from/to
// equality). Returns true when the workflow's trigger should fire for this
// event.
func triggerMatches(wf workflow, te TriggerEvent) bool {
	switch te.Type {
	case TriggerStatusChanged:
		var cfg TriggerConfigStatusChanged
		if len(wf.triggerConfig) > 0 {
			_ = json.Unmarshal(wf.triggerConfig, &cfg)
		}
		if cfg.FromStatus != "" && cfg.FromStatus != te.FromStatus {
			return false
		}
		if cfg.ToStatus != "" && cfg.ToStatus != te.ToStatus {
			return false
		}
		return true
	default:
		// Triggers that don't carry trigger_config conditionals match
		// purely on type, which Dispatch already filtered on.
		return true
	}
}

// conditionsHold parses and evaluates the conditions JSON. An empty array
// or a parse error returns true (with a log) — failing closed on a parse
// error would silently disable a workflow, which is worse than firing it.
//
// Phase-2 ext (JEH-1114, PR 2): the `evidence_present` op needs DB access
// (it scans the issue's recent comments + attachments), so this is now a
// Service method. Pure ops still flow through evaluate(); evidence_present
// is checked separately and short-circuits the rest of the chain. DB lookup
// failures fail CLOSED (return false) — for evidence-presence specifically,
// firing a `set_status: done` workflow without confirmed evidence is much
// worse than skipping a run we'll see again on the next event.
func (s *Service) conditionsHold(ctx context.Context, wf workflow, te TriggerEvent) bool {
	conds, err := parseConditions(wf.conditions)
	if err != nil {
		slog.Warn("workflow conditions: parse error",
			"workflow_id", uuidString(wf.id),
			"error", err,
		)
		return true
	}
	if len(conds) == 0 {
		return true
	}

	pure := make([]Condition, 0, len(conds))
	for _, c := range conds {
		if c.Op == OpEvidencePresent {
			ok, err := s.evaluateEvidence(ctx, wf, te, c)
			if err != nil {
				slog.Warn("workflow conditions: evidence eval failed",
					"workflow_id", uuidString(wf.id),
					"issue_id", te.IssueID,
					"error", err,
				)
				return false
			}
			if !ok {
				return false
			}
			continue
		}
		pure = append(pure, c)
	}
	if len(pure) == 0 {
		return true
	}
	return evaluate(pure, te.Raw)
}

// envFlagEnabled returns true for the small set of values we accept as on.
func envFlagEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse uuid %q: %w", s, err)
	}
	return u, nil
}

func optionalUUID(s string) (pgtype.UUID, bool) {
	if s == "" {
		return pgtype.UUID{}, false
	}
	u, err := parseUUID(s)
	if err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b, err := u.Value()
	if err != nil {
		return ""
	}
	s, _ := b.(string)
	return s
}

func nullableText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// JSON marshaling errors on stdlib-known types are programming
		// errors — fail loudly rather than papering over them.
		panic(fmt.Sprintf("workflows: marshal config: %v", err))
	}
	return b
}

