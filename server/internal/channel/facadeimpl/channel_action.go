package facadeimpl

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
)

const (
	channelActionCreateIssue = "create_issue"
	channelActionAddComment  = "add_comment"
	channelActionSetStatus   = "set_status"
	channelActionSetAssignee = "set_assignee"
	channelActionSetPriority = "set_priority"
	channelActionAddLabel    = "add_label"
	channelActionRemoveLabel = "remove_label"
)

type channelActionPayload struct {
	IssueID   string `json:"issue_id,omitempty"`
	CommentID string `json:"comment_id,omitempty"`
}

func withChannelAction(ctx context.Context, pool *pgxpool.Pool, inboundEventID pgtype.UUID, actionKind string, run func(pgx.Tx) (channelActionPayload, error)) (channelActionPayload, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return channelActionPayload{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if inboundEventID.Valid {
		if _, err := tx.Exec(ctx, `
INSERT INTO channel_action_result (inbound_event_id, action_kind, status)
VALUES ($1, $2, 'processing')
ON CONFLICT (inbound_event_id, action_kind) DO NOTHING
`, inboundEventID, actionKind); err != nil {
			return channelActionPayload{}, err
		}

		var status string
		var raw []byte
		if err := tx.QueryRow(ctx, `
SELECT status, result_payload
FROM channel_action_result
WHERE inbound_event_id = $1 AND action_kind = $2
FOR UPDATE
`, inboundEventID, actionKind).Scan(&status, &raw); err != nil {
			return channelActionPayload{}, err
		}
		if status == "completed" {
			var payload channelActionPayload
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &payload); err != nil {
					return channelActionPayload{}, err
				}
			}
			if err := tx.Commit(ctx); err != nil {
				return channelActionPayload{}, err
			}
			return payload, nil
		}
	}

	payload, err := run(tx)
	if err != nil {
		return channelActionPayload{}, err
	}
	if inboundEventID.Valid {
		raw, err := json.Marshal(payload)
		if err != nil {
			return channelActionPayload{}, err
		}
		if _, err := tx.Exec(ctx, `
UPDATE channel_action_result
SET status = 'completed',
    result_payload = $3,
    completed_at = now(),
    updated_at = now(),
    last_error = NULL
WHERE inbound_event_id = $1 AND action_kind = $2
`, inboundEventID, actionKind, raw); err != nil {
			return channelActionPayload{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return channelActionPayload{}, err
	}
	return payload, nil
}

func payloadUUID(id string) (pgtype.UUID, error) {
	if id == "" {
		return pgtype.UUID{}, fmt.Errorf("channel action result missing resource id")
	}
	return util.ParseUUID(id)
}
