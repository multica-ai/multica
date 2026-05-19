package inbound

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DispatchCompletionStore interface {
	GetDispatchCompletion(ctx context.Context, inboundEventID string) (reply string, ok bool, err error)
	MarkDispatchCompleted(ctx context.Context, inboundEventID string, reply string) error
}

type DBDispatchCompletionStore struct {
	pool *pgxpool.Pool
}

func NewDBDispatchCompletionStore(pool *pgxpool.Pool) *DBDispatchCompletionStore {
	return &DBDispatchCompletionStore{pool: pool}
}

func (s *DBDispatchCompletionStore) GetDispatchCompletion(ctx context.Context, inboundEventID string) (string, bool, error) {
	if s == nil || s.pool == nil || inboundEventID == "" {
		return "", false, nil
	}
	var reply string
	err := s.pool.QueryRow(ctx, `
SELECT dispatch_reply_text
FROM channel_inbound_event
WHERE id = $1
  AND dispatch_completed_at IS NOT NULL
`, inboundEventID).Scan(&reply)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return reply, true, nil
}

func (s *DBDispatchCompletionStore) MarkDispatchCompleted(ctx context.Context, inboundEventID string, reply string) error {
	if s == nil || s.pool == nil || inboundEventID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
UPDATE channel_inbound_event
SET dispatch_completed_at = COALESCE(dispatch_completed_at, now()),
    dispatch_reply_text = $2,
    updated_at = now()
WHERE id = $1
`, inboundEventID, reply)
	return err
}

var _ DispatchCompletionStore = (*DBDispatchCompletionStore)(nil)
