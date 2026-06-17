// CEREBRO-PATCH(workspace-copy): TECH-3742 — per-issue extras that travel with
// a copied issue: scheduled agent wakeups and date reminders.
//
// These hang off an issue and only make sense once the issue exists in the
// target, so they are copied inside copyIssueTx (after the issue row + comments
// are in place, so origin_comment_id can be remapped). Agents are copied before
// issues in the fixed merge order, so a wakeup's agent_id resolves to the copied
// agent; a wakeup whose agent was not copied is skipped (agent_id is NOT NULL and
// a wakeup pointing at a source-workspace agent would never dispatch correctly).
//
// Only still-pending wakeups are carried — already dispatched or cancelled ones
// are history and stay in the source. Carried wakeups are reset to a clean
// pending state (claimed/dispatched/cancelled cleared) so the target scheduler
// owns them fresh.
package workspacecopy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// copyIssueWakeupsTx copies the still-pending agent wakeups attached to
// sourceIssue onto the copied issue, remapping agent_id, watch_issue_id and
// origin_comment_id to their copied counterparts. Wakeups whose agent has not
// been copied are skipped. Returns the count copied.
func copyIssueWakeupsTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace, sourceIssue, targetIssue pgtype.UUID, commentMap map[[16]byte]pgtype.UUID) (int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, agent_id, prompt, trigger_type, fire_at, watch_issue_id, watch_status,
		       created_by_id, origin_comment_id
		FROM cerebro_agent_wakeup
		WHERE issue_id = $1 AND cancelled_at IS NULL AND dispatched_at IS NULL`,
		sourceIssue)
	if err != nil {
		return 0, fmt.Errorf("list wakeups: %w", err)
	}
	type wakeup struct {
		id, agentID, watchIssue, createdBy, originComment pgtype.UUID
		prompt, triggerType                               string
		watchStatus                                       pgtype.Text
		fireAt                                            pgtype.Timestamptz
	}
	var wakeups []wakeup
	for rows.Next() {
		var wk wakeup
		if err := rows.Scan(&wk.id, &wk.agentID, &wk.prompt, &wk.triggerType, &wk.fireAt,
			&wk.watchIssue, &wk.watchStatus, &wk.createdBy, &wk.originComment); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan wakeup: %w", err)
		}
		wakeups = append(wakeups, wk)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate wakeups: %w", err)
	}

	var copied int64
	for _, wk := range wakeups {
		if _, done, err := lookupMapping(ctx, tx, targetWorkspace, "wakeup", wk.id); err != nil {
			return copied, err
		} else if done {
			continue
		}
		// agent must be copied; otherwise skip (NOT NULL, and a cross-workspace
		// agent ref would never dispatch in the target).
		targetAgent, ok, err := lookupMapping(ctx, tx, targetWorkspace, "agent", wk.agentID)
		if err != nil {
			return copied, err
		}
		if !ok {
			continue
		}
		// watch_issue_id / origin_comment_id remap to copies when available, else cleared.
		watchIssue, err := remapIfCopied(ctx, tx, targetWorkspace, "issue", wk.watchIssue)
		if err != nil {
			return copied, err
		}
		originComment := pgtype.UUID{}
		if wk.originComment.Valid {
			if mapped, ok := commentMap[wk.originComment.Bytes]; ok {
				originComment = mapped
			}
		}
		var targetID pgtype.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO cerebro_agent_wakeup (id, workspace_id, agent_id, issue_id, prompt, trigger_type,
			                                  fire_at, watch_issue_id, watch_status, state, created_by_id,
			                                  created_at, updated_at, consecutive_postpones, origin_comment_id)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9, now(), now(), 0, $10)
			RETURNING id`,
			targetWorkspace, targetAgent, targetIssue, wk.prompt, wk.triggerType, wk.fireAt,
			watchIssue, wk.watchStatus, wk.createdBy, originComment).Scan(&targetID); err != nil {
			return copied, fmt.Errorf("copy wakeup: %w", err)
		}
		if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "wakeup", wk.id, targetID, nil, nil); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, nil
}

// copyIssueRemindersTx copies the date reminders attached to sourceIssue onto
// the copied issue. Idempotent via the (issue_id, kind) primary-key-style guard.
func copyIssueRemindersTx(ctx context.Context, tx pgx.Tx, sourceIssue, targetIssue pgtype.UUID) (int64, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO cerebro_issue_date_reminder (issue_id, kind, reminder_date, created_at)
		SELECT $1, r.kind, r.reminder_date, r.created_at
		FROM cerebro_issue_date_reminder r WHERE r.issue_id = $2
		ON CONFLICT DO NOTHING`,
		targetIssue, sourceIssue)
	if err != nil {
		return 0, fmt.Errorf("copy reminders: %w", err)
	}
	return tag.RowsAffected(), nil
}
