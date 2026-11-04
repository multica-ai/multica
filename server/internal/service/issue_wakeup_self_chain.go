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
			var actor struct {
				Type          string `json:"actor_type"`
				ID            string `json:"actor_id"`
				MemberActorID string `json:"member_actor_id"`
			}
			if err = json.Unmarshal(fact.Payload, &actor); err != nil {
				return fmt.Errorf("read wakeup continuation actor: %w", err)
			}
			if (actor.Type == "member" && actor.ID != "") ||
				(fact.EventType == wakeupConditionEventType && actor.MemberActorID != "") {
				return nil
			}
		}
		// Dispatch snapshots the rule's source onto its run. Updating or
		// deleting the consumed rule cannot rewrite that run's ancestry.
		if !task.DelegatedFromTaskID.Valid {
			return nil
		}
		source = task.DelegatedFromTaskID
	}
	return fmt.Errorf("%w: agent self-continuation wakeup chain exceeds %d links; escalate to a human before scheduling another continuation", ErrWakeupInput, wakeupSelfChainLimit)
}
