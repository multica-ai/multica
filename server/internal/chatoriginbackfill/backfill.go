// Package chatoriginbackfill marks legacy first-party Chats with the durable
// origin signal introduced by the channel route migration.
package chatoriginbackfill

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultBatchSize bounds each statement so the migration does not rewrite
// the full chat_session table in one transaction.
const DefaultBatchSize = 1000

type Result struct {
	RowsVisited    int64
	RowsBackfilled int64
	Pages          int
}

// HookOptions has production-safe zero values. Table overrides exist only so
// integration tests can point the hook at an isolated schema; callers must
// provide trusted, already-quoted identifiers.
type HookOptions struct {
	Logger       *slog.Logger
	BatchSize    int
	SessionTable string
	BindingTable string
	MessageTable string
}

// Each page follows the chat_session primary key instead of repeatedly
// scanning all previously visited rows. The update remains provenance-based:
// a binding or any channel-ingested message is enough to keep a legacy Chat
// classified as channel-originated even if one of those records was removed.
const backfillPageSQL = `
WITH page AS MATERIALIZED (
    SELECT id
    FROM %s
    WHERE ($1::uuid IS NULL OR id > $1::uuid)
    ORDER BY id
    LIMIT $2
), updated AS (
    UPDATE %s AS session
    SET explicitly_created_at = session.created_at
    FROM page
    WHERE session.id = page.id
      AND session.explicitly_created_at IS NULL
      AND NOT EXISTS (
          SELECT 1
          FROM %s AS binding
          WHERE binding.chat_session_id = session.id
      )
      AND NOT EXISTS (
          SELECT 1
          FROM %s AS message
          WHERE message.chat_session_id = session.id
            AND message.channel_ingested
      )
    RETURNING session.id
)
SELECT
    COALESCE((SELECT id::text FROM page ORDER BY id DESC LIMIT 1), ''),
    (SELECT count(*) FROM page),
    (SELECT count(*) FROM updated)`

// Hook walks legacy Chats in short, idempotent pages. The migration runner
// invokes it only after migration 420 has added explicitly_created_at and while
// the normal deployment contract keeps the server stopped. If a batch fails,
// already updated rows remain valid and the next migrate run safely revisits
// them before continuing from the primary-key cursor.
func Hook(ctx context.Context, pool *pgxpool.Pool, opts HookOptions) (Result, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	sessionTable := opts.SessionTable
	if sessionTable == "" {
		sessionTable = "chat_session"
	}
	bindingTable := opts.BindingTable
	if bindingTable == "" {
		bindingTable = "channel_chat_session_binding"
	}
	messageTable := opts.MessageTable
	if messageTable == "" {
		messageTable = "chat_message"
	}

	query := fmt.Sprintf(backfillPageSQL, sessionTable, sessionTable, bindingTable, messageTable)
	var result Result
	var cursor any
	for {
		var nextCursor string
		var visited, updated int64
		if err := pool.QueryRow(ctx, query, cursor, batchSize).Scan(&nextCursor, &visited, &updated); err != nil {
			return result, fmt.Errorf("backfill explicit Chat origin page: %w", err)
		}
		if visited == 0 {
			break
		}

		result.Pages++
		result.RowsVisited += visited
		result.RowsBackfilled += updated
		cursor = nextCursor
		log.Info("Chat origin backfill: page complete",
			"visited", visited,
			"backfilled", updated,
			"total_visited", result.RowsVisited,
			"total_backfilled", result.RowsBackfilled)
	}

	log.Info("Chat origin backfill: complete",
		"visited", result.RowsVisited,
		"backfilled", result.RowsBackfilled,
		"pages", result.Pages)
	return result, nil
}
