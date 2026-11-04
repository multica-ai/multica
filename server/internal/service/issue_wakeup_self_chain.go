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

// guardWakeupSelfChain counts the proposed rule and walks its source runs.
// Rule IDs change on each one-shot continuation, so the per-rule loop and
// hourly limits cannot bound this chain. Trigger evidence belongs to the run:
// a member's input resets it even when an agent originally created the rule.
func guardWakeupSelfChain(ctx context.Context, q *db.Queries, issue db.Issue, source pgtype.UUID) error {
	for links := 1; links <= wakeupSelfChainLimit; links++ {
		task, err := q.GetAgentTask(ctx, source)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read wakeup continuation source: %w", err)
		}
		if task.IssueID != issue.ID {
			return nil
		}
		var stored struct {
			WakeupID string         `json:"wakeup_id"`
			Evidence wakeupEvidence `json:"wakeup_evidence"`
		}
		if len(task.Context) > 0 {
			if err = json.Unmarshal(task.Context, &stored); err != nil {
				return fmt.Errorf("read wakeup continuation context: %w", err)
			}
		}
		// Ordinary member assignments and mentions have no wakeup parent.
		if stored.WakeupID == "" {
			return nil
		}
		for _, fact := range stored.Evidence.Facts {
			actor, _, _ := receiptActor(db.IssueWakeupReceipt{Payload: fact.Payload})
			if actor == "member" || fact.EventType == wakeupManualEventType {
				return nil
			}
		}
		id, err := wakeupUUID(stored.WakeupID)
		if err != nil {
			return err
		}
		w, err := q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: id, WorkspaceID: issue.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read wakeup continuation rule: %w", err)
		}
		if w.IssueID != issue.ID || !w.SourceTaskID.Valid {
			return nil
		}
		source = w.SourceTaskID
	}
	return fmt.Errorf("%w: agent self-continuation wakeup chain exceeds %d links; escalate to a human before scheduling another continuation", ErrWakeupInput, wakeupSelfChainLimit)
}
