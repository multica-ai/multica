package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const SystemRuleBlockersDone = "blockers_done"
const BlockersDoneInstruction = "All tasks this issue was waiting for have ended or were removed from its dependencies. The trigger facts list their statuses. Check cancelled or deleted work before continuing. Start the current assignee's work, using the current project workflow step when present. The platform has not changed this issue's status; if it is blocked, check whether any external wait still applies."

func systemWakeupInstruction(w db.IssueWakeup, settings []byte) string {
	if w.SystemRule.String == SystemRuleBlockersDone {
		return BlockersDoneInstruction
	}
	return ChildDoneInstruction(w.Instruction, settings)
}

func evaluateIssueBlockers(ctx context.Context, tx pgx.Tx, q *db.Queries, w db.IssueWakeup) (bool, string, map[string]any, error) {
	blocked, err := q.HasOpenIssueBlockers(ctx, db.HasOpenIssueBlockersParams{WorkspaceID: w.WorkspaceID, IssueID: w.IssueID})
	if err != nil {
		return false, "", nil, err
	}
	// The snapshot preserves names of deleted or detached blockers. Statuses
	// come from current rows, including cancellations since the wait was armed.
	rows, err := tx.Query(ctx, `SELECT CASE
 WHEN COALESCE(p.visibility='private',(b.value->>'private')::boolean,false)
 THEN jsonb_build_object('restricted',true,'status','ended')
 ELSE (b.value-'private'-'project_id') || jsonb_build_object('status',COALESCE(i.status,'deleted'),'removed',NOT EXISTS(
 SELECT 1 FROM effective_issue_blocker e WHERE e.workspace_id=$2 AND e.issue_id=$3 AND e.blocker_issue_id=(b.value->>'id')::uuid)) END
 FROM jsonb_array_elements(COALESCE($1::jsonb->'blockers','[]'::jsonb)) b
 LEFT JOIN issue i ON i.id=(b.value->>'id')::uuid AND i.workspace_id=$2
 JOIN issue owner ON owner.id=$3 AND owner.workspace_id=$2
 LEFT JOIN project p ON p.id=COALESCE(i.project_id,(b.value->>'project_id')::uuid) AND p.workspace_id=$2`, w.Condition, w.WorkspaceID, w.IssueID)
	if err != nil {
		return false, "", nil, err
	}
	defer rows.Close()
	blockers := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err = rows.Scan(&raw); err != nil {
			return false, "", nil, err
		}
		blockers = append(blockers, raw)
	}
	return !blocked, "ready", map[string]any{"blockers": blockers}, rows.Err()
}

// Seed existing staged waits in bounded batches; no stage edges are stored.
func (s *IssueWakeupService) backfillBlockerRules(ctx context.Context) error {
	if s.blockersBackfilled.Load() {
		return nil
	}
	tx, err := s.Tasks.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT i.workspace_id,i.id FROM issue i
 WHERE i.parent_issue_id IS NOT NULL AND i.stage IS NOT NULL
 AND i.status NOT IN ('done','cancelled')
 AND NOT EXISTS(SELECT 1 FROM issue_wakeup w WHERE w.issue_id=i.id AND w.system_rule='blockers_done')
 AND issue_has_open_blockers(i.workspace_id,i.id) LIMIT 100`)
	if err != nil {
		return err
	}
	type pair struct{ ws, id string }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err = rows.Scan(&p.ws, &p.id); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, p := range pairs {
		if _, err = tx.Exec(ctx, "SELECT arm_issue_blocker_wakeup($1,$2)", p.ws, p.id); err != nil {
			return fmt.Errorf("arm staged blocker: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if len(pairs) < 100 {
		s.blockersBackfilled.Store(true)
	}
	return nil
}

func systemInboxType(w db.IssueWakeup) string {
	if w.SystemRule.String == SystemRuleBlockersDone {
		return "blockers_done"
	}
	return "children_done"
}
func systemTriggerSummary(w db.IssueWakeup) string {
	if w.SystemRule.String == SystemRuleBlockersDone {
		return "Wakeup: waiting tasks ended"
	}
	return "Wakeup: sub-issues closed"
}
func systemWakeupFacts(w db.IssueWakeup, receipts []db.IssueWakeupReceipt) map[string]any {
	if w.SystemRule.String != SystemRuleBlockersDone {
		return childDoneFacts(receipts)
	}
	facts := map[string]any{"rule": SystemRuleBlockersDone}
	for i := len(receipts) - 1; i >= 0; i-- {
		if receipts[i].EventType != wakeupConditionEventType {
			continue
		}
		var payload struct {
			Observed map[string]any `json:"observed"`
		}
		_ = json.Unmarshal(receipts[i].Payload, &payload)
		facts["blockers"] = payload.Observed["blockers"]
		break
	}
	return facts
}

var ErrIssueBlockerNotFound = errors.New("blocker issue not found in this workspace")

func AddIssueBlocker(ctx context.Context, q *db.Queries, workspace, issue, blocker pgtype.UUID, actorType string, actorID pgtype.UUID) error {
	if _, err := q.LockIssueBlockerTarget(ctx, db.LockIssueBlockerTargetParams{IssueID: blocker, WorkspaceID: workspace}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrIssueBlockerNotFound
		}
		return err
	}
	return q.AddIssueBlocker(ctx, db.AddIssueBlockerParams{WorkspaceID: workspace, IssueID: issue, BlockerIssueID: blocker, CreatedByType: actorType, CreatedByID: actorID})
}

// PublicWakeupCondition omits the dependency snapshot owned by the queue.
func PublicWakeupCondition(w db.IssueWakeup) json.RawMessage {
	if w.SystemRule.String == SystemRuleBlockersDone {
		return json.RawMessage(`{"type":"blockers_done"}`)
	}
	return w.Condition
}
