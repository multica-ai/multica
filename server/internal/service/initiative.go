package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var (
	// ErrInitiativeInvalidTransition means the requested edge does not exist in
	// the state machine (caller bug or stale client) — map to 409.
	ErrInitiativeInvalidTransition = errors.New("initiative: invalid transition")
	// ErrInitiativeTransitionConflict means the edge exists but the CAS lost a
	// race: the row moved to a different status between load and update.
	ErrInitiativeTransitionConflict = errors.New("initiative: transition conflict")
)

// InitiativeService owns the initiative entity: creation, CAS status
// transitions, and the append-only event trail. Reconciler-driven behavior
// (dispatch, convergence, decisions) layers on top of it in later phases.
type InitiativeService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Bus       *events.Bus
}

func NewInitiativeService(q *db.Queries, tx TxStarter, bus *events.Bus) *InitiativeService {
	return &InitiativeService{Queries: q, TxStarter: tx, Bus: bus}
}

// InitiativeActor identifies who caused a mutation, for the event trail and
// WS fanout. Type is "member", "agent", or "system"; ID is invalid for
// "system".
type InitiativeActor struct {
	Type string
	ID   pgtype.UUID
}

func SystemInitiativeActor() InitiativeActor {
	return InitiativeActor{Type: "system"}
}

func MemberInitiativeActor(memberID pgtype.UUID) InitiativeActor {
	return InitiativeActor{Type: "member", ID: memberID}
}

type InitiativeCreateParams struct {
	WorkspaceID                pgtype.UUID
	Title                      string
	Idea                       string
	Constraints                []byte
	AutonomyLevel              pgtype.Int2
	BudgetLimitTokens          pgtype.Int8
	MaxParallelTasks           pgtype.Int4
	MaxAttempts                pgtype.Int4
	StallTimeoutSeconds        pgtype.Int4
	ExternalWaitTimeoutSeconds pgtype.Int4
	CreatedBy                  pgtype.UUID
}

// Create inserts the initiative in `draft` plus its `created` audit event in
// one transaction, then broadcasts initiative:created.
func (s *InitiativeService) Create(ctx context.Context, p InitiativeCreateParams) (db.Initiative, error) {
	constraints := p.Constraints
	if len(constraints) == 0 {
		constraints = []byte("{}")
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Initiative{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	initiative, err := qtx.CreateInitiative(ctx, db.CreateInitiativeParams{
		WorkspaceID:                p.WorkspaceID,
		Title:                      p.Title,
		Idea:                       p.Idea,
		Constraints:                constraints,
		AutonomyLevel:              p.AutonomyLevel,
		BudgetLimitTokens:          p.BudgetLimitTokens,
		MaxParallelTasks:           p.MaxParallelTasks,
		MaxAttempts:                p.MaxAttempts,
		StallTimeoutSeconds:        p.StallTimeoutSeconds,
		ExternalWaitTimeoutSeconds: p.ExternalWaitTimeoutSeconds,
		CreatedBy:                  p.CreatedBy,
	})
	if err != nil {
		return db.Initiative{}, fmt.Errorf("create initiative: %w", err)
	}

	actor := MemberInitiativeActor(p.CreatedBy)
	if err := s.AppendEvent(ctx, qtx, initiative, pgtype.UUID{}, actor, "created", nil); err != nil {
		return db.Initiative{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return db.Initiative{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publishInitiative(protocol.EventInitiativeCreated, initiative, actor)
	return initiative, nil
}

// AppendEvent writes one initiative_event row through the supplied queries
// handle (tx-scoped where the caller needs atomicity with the mutation it
// records). A nil payload becomes {}.
func (s *InitiativeService) AppendEvent(ctx context.Context, q *db.Queries, initiative db.Initiative, taskID pgtype.UUID, actor InitiativeActor, eventType string, payload map[string]any) error {
	raw := []byte("{}")
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal initiative event payload: %w", err)
		}
		raw = encoded
	}
	_, err := q.CreateInitiativeEvent(ctx, db.CreateInitiativeEventParams{
		WorkspaceID:  initiative.WorkspaceID,
		InitiativeID: initiative.ID,
		TaskID:       taskID,
		ActorType:    actor.Type,
		ActorID:      actor.ID,
		EventType:    eventType,
		Payload:      raw,
	})
	if err != nil {
		return fmt.Errorf("append initiative event: %w", err)
	}
	return nil
}

// InitiativeTransitionOpts carries the status-specific bookkeeping columns.
// They are written as-is on every transition (NULL clears), so entering
// paused/needs_human sets them and leaving resets them without extra queries.
type InitiativeTransitionOpts struct {
	PausePrevStatus  pgtype.Text
	PauseReason      pgtype.Text
	NeedsHumanReason pgtype.Text
	// EventPayload is merged into the status_changed audit payload alongside
	// the from/to fields.
	EventPayload map[string]any
}

// Transition CAS-moves the initiative from its observed status to `to`,
// appends the status_changed event in the same transaction, and broadcasts
// initiative:updated. ErrInitiativeInvalidTransition when the edge does not
// exist; ErrInitiativeTransitionConflict when the row moved concurrently.
func (s *InitiativeService) Transition(ctx context.Context, initiative db.Initiative, to string, actor InitiativeActor, opts InitiativeTransitionOpts) (db.Initiative, error) {
	if !CanTransitionInitiative(initiative.Status, to) {
		return db.Initiative{}, fmt.Errorf("%w: %s -> %s", ErrInitiativeInvalidTransition, initiative.Status, to)
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Initiative{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	updated, err := qtx.TransitionInitiativeStatus(ctx, db.TransitionInitiativeStatusParams{
		ID:               initiative.ID,
		Status:           to,
		PausePrevStatus:  opts.PausePrevStatus,
		PauseReason:      opts.PauseReason,
		NeedsHumanReason: opts.NeedsHumanReason,
		FromStatuses:     []string{initiative.Status},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Initiative{}, fmt.Errorf("%w: %s -> %s", ErrInitiativeTransitionConflict, initiative.Status, to)
		}
		return db.Initiative{}, fmt.Errorf("transition initiative: %w", err)
	}

	payload := map[string]any{}
	maps.Copy(payload, opts.EventPayload)
	payload["from"] = initiative.Status
	payload["to"] = to
	if err := s.AppendEvent(ctx, qtx, updated, pgtype.UUID{}, actor, "status_changed", payload); err != nil {
		return db.Initiative{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return db.Initiative{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publishInitiative(protocol.EventInitiativeUpdated, updated, actor)
	return updated, nil
}

// Pause is the kill switch: remembers where to resume to and stops the
// reconciler from dispatching anything new. needs_human_reason is carried
// through so pausing a needs_human initiative does not erase why it escalated
// (the transition query SETs the column on every write).
func (s *InitiativeService) Pause(ctx context.Context, initiative db.Initiative, actor InitiativeActor, reason string) (db.Initiative, error) {
	return s.Transition(ctx, initiative, InitiativeStatusPaused, actor, InitiativeTransitionOpts{
		PausePrevStatus:  util.StrToText(initiative.Status),
		PauseReason:      util.PtrToText(nonEmptyPtr(reason)),
		NeedsHumanReason: initiative.NeedsHumanReason,
		EventPayload:     map[string]any{"reason": reason},
	})
}

// Resume returns a paused initiative to the status it was paused from,
// carrying needs_human_reason back so a needs_human → paused → needs_human
// round-trip preserves the escalation prompt. A missing/invalid
// pause_prev_status (should not happen) degrades to needs_human rather than
// guessing.
func (s *InitiativeService) Resume(ctx context.Context, initiative db.Initiative, actor InitiativeActor) (db.Initiative, error) {
	if initiative.Status != InitiativeStatusPaused {
		return db.Initiative{}, fmt.Errorf("%w: %s -> resume", ErrInitiativeInvalidTransition, initiative.Status)
	}
	target := initiative.PausePrevStatus.String
	if !CanTransitionInitiative(InitiativeStatusPaused, target) {
		target = InitiativeStatusNeedsHuman
		return s.Transition(ctx, initiative, target, actor, InitiativeTransitionOpts{
			NeedsHumanReason: util.StrToText("resume_missing_prev_status"),
		})
	}
	return s.Transition(ctx, initiative, target, actor, InitiativeTransitionOpts{
		NeedsHumanReason: initiative.NeedsHumanReason,
	})
}

// Cancel terminates the initiative and explicitly cleans up its linked issues
// and non-terminal tasks in the same transaction (no DB cascades by repo
// rule), then fans out initiative:updated plus issue:updated per cancelled
// issue.
func (s *InitiativeService) Cancel(ctx context.Context, initiative db.Initiative, actor InitiativeActor, reason string) (db.Initiative, error) {
	if !CanTransitionInitiative(initiative.Status, InitiativeStatusCancelled) {
		return db.Initiative{}, fmt.Errorf("%w: %s -> cancelled", ErrInitiativeInvalidTransition, initiative.Status)
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Initiative{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	updated, err := qtx.TransitionInitiativeStatus(ctx, db.TransitionInitiativeStatusParams{
		ID:           initiative.ID,
		Status:       InitiativeStatusCancelled,
		FromStatuses: []string{initiative.Status},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Initiative{}, fmt.Errorf("%w: %s -> cancelled", ErrInitiativeTransitionConflict, initiative.Status)
		}
		return db.Initiative{}, fmt.Errorf("cancel initiative: %w", err)
	}

	tasks, err := qtx.ListInitiativeTasksAllVersions(ctx, initiative.ID)
	if err != nil {
		return db.Initiative{}, fmt.Errorf("list initiative tasks: %w", err)
	}

	var cancelledIssues []db.Issue
	for _, task := range tasks {
		if !IsInitiativeTaskStateTerminal(task.State) {
			if _, err := qtx.TransitionInitiativeTaskState(ctx, db.TransitionInitiativeTaskStateParams{
				ID:          task.ID,
				State:       InitiativeTaskStateFailed,
				StateReason: util.StrToText("initiative_cancelled"),
				FromStates:  []string{task.State},
			}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return db.Initiative{}, fmt.Errorf("fail initiative task: %w", err)
			}
		}
		if !task.IssueID.Valid {
			continue
		}
		issue, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          task.IssueID,
			WorkspaceID: initiative.WorkspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Issue deleted by a human — nothing left to clean up.
			continue
		}
		if err != nil {
			return db.Initiative{}, fmt.Errorf("load initiative issue: %w", err)
		}
		if issue.Status == "done" || issue.Status == "cancelled" {
			continue
		}
		cancelled, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:          issue.ID,
			Status:      "cancelled",
			WorkspaceID: initiative.WorkspaceID,
		})
		if err != nil {
			return db.Initiative{}, fmt.Errorf("cancel initiative issue: %w", err)
		}
		cancelledIssues = append(cancelledIssues, cancelled)
	}

	if err := s.AppendEvent(ctx, qtx, updated, pgtype.UUID{}, actor, "cancelled", map[string]any{
		"reason":           reason,
		"cancelled_issues": len(cancelledIssues),
	}); err != nil {
		return db.Initiative{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return db.Initiative{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publishInitiative(protocol.EventInitiativeUpdated, updated, actor)
	prefix := s.getIssuePrefix(initiative.WorkspaceID)
	for _, issue := range cancelledIssues {
		s.Bus.Publish(events.Event{
			Type:        protocol.EventIssueUpdated,
			WorkspaceID: util.UUIDToString(initiative.WorkspaceID),
			ActorType:   actor.Type,
			ActorID:     util.UUIDToString(actor.ID),
			Payload:     map[string]any{"issue": issueToMap(issue, prefix)},
		})
	}
	return updated, nil
}

// UpdateMeta edits the human-editable fields (title/idea/constraints and the
// autonomy/budget overrides) with an `updated` audit event in the same
// transaction.
func (s *InitiativeService) UpdateMeta(ctx context.Context, initiative db.Initiative, actor InitiativeActor, params db.UpdateInitiativeMetaParams) (db.Initiative, error) {
	params.ID = initiative.ID
	params.WorkspaceID = initiative.WorkspaceID

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Initiative{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	updated, err := qtx.UpdateInitiativeMeta(ctx, params)
	if err != nil {
		return db.Initiative{}, fmt.Errorf("update initiative: %w", err)
	}
	if err := s.AppendEvent(ctx, qtx, updated, pgtype.UUID{}, actor, "updated", nil); err != nil {
		return db.Initiative{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Initiative{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publishInitiative(protocol.EventInitiativeUpdated, updated, actor)
	return updated, nil
}

// ApprovePlan is the human plan-approval gate: an atomic CAS out of
// plan_review with the approval stamp, plus the audit event.
func (s *InitiativeService) ApprovePlan(ctx context.Context, initiative db.Initiative, memberID pgtype.UUID) (db.Initiative, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Initiative{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	updated, err := qtx.ApproveInitiativePlan(ctx, db.ApproveInitiativePlanParams{
		ID:         initiative.ID,
		ApprovedBy: memberID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Initiative{}, fmt.Errorf("%w: %s -> executing", ErrInitiativeTransitionConflict, initiative.Status)
		}
		return db.Initiative{}, fmt.Errorf("approve initiative plan: %w", err)
	}

	actor := MemberInitiativeActor(memberID)
	if err := s.AppendEvent(ctx, qtx, updated, pgtype.UUID{}, actor, "plan_approved", map[string]any{
		"plan_version": updated.PlanVersion,
	}); err != nil {
		return db.Initiative{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return db.Initiative{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publishInitiative(protocol.EventInitiativeUpdated, updated, actor)
	return updated, nil
}

func (s *InitiativeService) publishInitiative(eventType string, initiative db.Initiative, actor InitiativeActor) {
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(initiative.WorkspaceID),
		ActorType:   actor.Type,
		ActorID:     util.UUIDToString(actor.ID),
		Payload:     map[string]any{"initiative": InitiativeWSPayload(initiative)},
	})
}

func (s *InitiativeService) getIssuePrefix(workspaceID pgtype.UUID) string {
	ws, err := s.Queries.GetWorkspace(context.Background(), workspaceID)
	if err != nil {
		return ""
	}
	return ws.IssuePrefix
}

// InitiativeWSPayload is the WS payload shape for initiative:created/updated.
// Field names and value shapes must match the REST InitiativeResponse so
// frontends can patch caches from either source; the handler package has a
// drift-guard test comparing the two key sets. Exported for that test.
func InitiativeWSPayload(i db.Initiative) map[string]any {
	constraints := json.RawMessage(i.Constraints)
	if len(constraints) == 0 {
		constraints = json.RawMessage("{}")
	}
	return map[string]any{
		"id":                            util.UUIDToString(i.ID),
		"workspace_id":                  util.UUIDToString(i.WorkspaceID),
		"title":                         i.Title,
		"idea":                          i.Idea,
		"constraints":                   constraints,
		"status":                        i.Status,
		"autonomy_level":                util.Int2ToPtr(i.AutonomyLevel),
		"plan_version":                  i.PlanVersion,
		"orchestrator_agent_id":         util.UUIDToPtr(i.OrchestratorAgentID),
		"budget_limit_tokens":           util.Int8ToPtr(i.BudgetLimitTokens),
		"budget_spent_tokens":           i.BudgetSpentTokens,
		"max_parallel_tasks":            util.Int4ToPtr(i.MaxParallelTasks),
		"max_attempts":                  util.Int4ToPtr(i.MaxAttempts),
		"stall_timeout_seconds":         util.Int4ToPtr(i.StallTimeoutSeconds),
		"external_wait_timeout_seconds": util.Int4ToPtr(i.ExternalWaitTimeoutSeconds),
		"pause_prev_status":             util.TextToPtr(i.PausePrevStatus),
		"pause_reason":                  util.TextToPtr(i.PauseReason),
		"needs_human_reason":            util.TextToPtr(i.NeedsHumanReason),
		"created_by":                    util.UUIDToString(i.CreatedBy),
		"approved_by":                   util.UUIDToPtr(i.ApprovedBy),
		"approved_at":                   util.TimestampToPtr(i.ApprovedAt),
		"created_at":                    util.TimestampToString(i.CreatedAt),
		"updated_at":                    util.TimestampToString(i.UpdatedAt),
	}
}

func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
