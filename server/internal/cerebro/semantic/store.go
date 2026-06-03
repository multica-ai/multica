package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// QueueItem is one row of cerebro_search_embedding_queue plus the embedding
// input text we materialised by joining onto issue / comment.
type QueueItem struct {
	ID          int64
	TargetType  string
	TargetID    pgtype.UUID
	WorkspaceID pgtype.UUID
	Content     string
}

// ContentHash is the SHA-256 of the embedding input. Stored alongside the
// embedding so a re-run of the worker over content the model already saw can
// skip the network call.
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// FetchPending claims up to limit queue rows for the caller and returns the
// embedding input materialised from the source row. Uses FOR UPDATE SKIP
// LOCKED so multiple workers can drain in parallel without stepping on each
// other.
//
// Sites that DO NOT have pgvector installed still call this — the queue
// table exists unconditionally — but the upsert path is a no-op for them
// (see Upsert).
func FetchPending(ctx context.Context, tx pgx.Tx, limit int) ([]QueueItem, error) {
	const sql = `
		WITH claimed AS (
			SELECT id, target_type, target_id, workspace_id
			FROM cerebro_search_embedding_queue
			ORDER BY enqueued_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		SELECT c.id, c.target_type, c.target_id, c.workspace_id,
			CASE c.target_type
				WHEN 'issue'   THEN COALESCE((SELECT i.title || E'\n' || COALESCE(i.description, '') FROM issue i WHERE i.id = c.target_id), '')
				WHEN 'comment' THEN COALESCE((SELECT cm.content FROM comment cm WHERE cm.id = c.target_id), '')
			END AS content
		FROM claimed c
	`
	rows, err := tx.Query(ctx, sql, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch pending: %w", err)
	}
	defer rows.Close()
	var out []QueueItem
	for rows.Next() {
		var item QueueItem
		if err := rows.Scan(&item.ID, &item.TargetType, &item.TargetID, &item.WorkspaceID, &item.Content); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Upsert persists one embedding for (target_type, target_id) and removes the
// matching queue row in the same transaction. Returns ErrStoreUnavailable
// when pgvector is not installed — the worker logs and continues.
func Upsert(ctx context.Context, tx pgx.Tx, item QueueItem, vec []float32, model, contentHash string) error {
	if !storeAvailable(ctx, tx) {
		return ErrStoreUnavailable
	}
	const upsert = `
		INSERT INTO cerebro_search_embedding
			(target_type, target_id, workspace_id, embedding, model, content_hash, generated_at)
		VALUES ($1, $2, $3, $4::vector, $5, $6, now())
		ON CONFLICT (target_type, target_id) DO UPDATE
			SET embedding    = EXCLUDED.embedding,
			    model        = EXCLUDED.model,
			    content_hash = EXCLUDED.content_hash,
			    generated_at = now()
	`
	if _, err := tx.Exec(ctx, upsert,
		item.TargetType, item.TargetID, item.WorkspaceID,
		formatVector(vec), model, contentHash,
	); err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cerebro_search_embedding_queue WHERE id = $1`, item.ID); err != nil {
		return fmt.Errorf("delete queue row: %w", err)
	}
	return nil
}

// MarkFailed bumps attempts and stores the error string so retries are
// visible in the queue table. Caller decides whether to keep retrying.
func MarkFailed(ctx context.Context, tx pgx.Tx, id int64, errMsg string) {
	_, _ = tx.Exec(ctx, `
		UPDATE cerebro_search_embedding_queue
		SET attempts = attempts + 1,
		    last_error = $2
		WHERE id = $1
	`, id, errMsg)
}

// ErrStoreUnavailable is returned when the pgvector extension is missing.
// The worker treats it as "stop for this tick" so we don't log every row.
var ErrStoreUnavailable = errors.New("semantic store unavailable (pgvector missing)")

// storeAvailable probes for the pgvector type without raising on missing.
func storeAvailable(ctx context.Context, tx pgx.Tx) bool {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'vector')`).Scan(&ok); err != nil {
		return false
	}
	if !ok {
		return false
	}
	// Table presence is the second condition — the migration skips the
	// CREATE TABLE on installs without the extension. Once it exists, it
	// stays.
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'cerebro_search_embedding')`).Scan(&ok); err != nil {
		return false
	}
	return ok
}

// StoreAvailable is the exported probe used by the query path and tests so
// they can decide whether to call into the semantic SQL at all.
func StoreAvailable(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) bool {
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'vector')`).Scan(&ok); err != nil || !ok {
		return false
	}
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'cerebro_search_embedding')`).Scan(&ok); err != nil {
		return false
	}
	return ok
}

// FormatVectorLiteral is the exported form of formatVector — the search
// handler uses it to materialise the parameter literal it hands BuildHybrid.
func FormatVectorLiteral(vec []float32) string { return formatVector(vec) }

// formatVector renders a []float32 in the textual format pgvector accepts on
// the wire ("[0.1,0.2,...]"). Avoids a binary codec dependency and keeps
// migrations simple.
func formatVector(vec []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", v)
	}
	b.WriteByte(']')
	return b.String()
}
