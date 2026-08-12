package recruitinbox

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresLedger struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, databaseURL string) (*PostgresLedger, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresLedger{pool: pool}, nil
}

func (l *PostgresLedger) Claim(ctx context.Context, key, sourceMessageID string, at time.Time) (bool, error) {
	tag, err := l.pool.Exec(ctx, `INSERT INTO recruitment_inbox_event(message_key, source_message_id, processing_state, received_at, updated_at) VALUES($1, $2, 'processing', $3, $3) ON CONFLICT(message_key) DO NOTHING`, key, sourceMessageID, at.UTC())
	return tag.RowsAffected() == 1, err
}

func (l *PostgresLedger) Pending(ctx context.Context) ([]Record, error) {
	rows, err := l.pool.Query(ctx, `SELECT message_key, source_message_id, received_at, updated_at FROM recruitment_inbox_event WHERE processing_state='processing' ORDER BY received_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var rec Record
		if err := rows.Scan(&rec.MessageKey, &rec.SourceMessageID, &rec.ReceivedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		rec.State = StateProcessing
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (l *PostgresLedger) MarkReplied(ctx context.Context, key string, summary Summary, roleVersion, sentKey string, at time.Time) error {
	raw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = l.pool.Exec(ctx, `UPDATE recruitment_inbox_event SET source_message_id='', structured_summary=$1::jsonb, role_version=$2, processing_state='replied', updated_at=$3, error_code='', sent_message_key=$4, sent_status='sent' WHERE message_key=$5`, raw, roleVersion, at.UTC(), sentKey, key)
	return err
}

func (l *PostgresLedger) MarkIgnored(ctx context.Context, key string, at time.Time) error {
	_, err := l.pool.Exec(ctx, `UPDATE recruitment_inbox_event SET source_message_id='', processing_state='ignored', updated_at=$1 WHERE message_key=$2`, at.UTC(), key)
	return err
}

func (l *PostgresLedger) MarkDeadLetter(ctx context.Context, key, errorCode, sentKey string, at time.Time) error {
	status := "failed"
	if sentKey != "" {
		status = "sent"
	}
	_, err := l.pool.Exec(ctx, `UPDATE recruitment_inbox_event SET source_message_id='', processing_state='dead_letter', updated_at=$1, error_code=$2, sent_message_key=$3, sent_status=$4 WHERE message_key=$5`, at.UTC(), errorCode, sentKey, status, key)
	return err
}

func (l *PostgresLedger) Health(ctx context.Context) error { return l.pool.Ping(ctx) }
func (l *PostgresLedger) Close() error {
	l.pool.Close()
	return nil
}
