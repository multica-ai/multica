package semantic

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// VectorHit is one row from a top-N nearest-neighbour lookup. Distance is
// the pgvector cosine distance (0..2) — lower is closer.
type VectorHit struct {
	TargetType string
	TargetID   pgtype.UUID
	Distance   float64
}

// NearestNeighbours returns the K closest embeddings to vec, scoped to the
// workspace. Returns nil when the store is unavailable so callers can fall
// back to keyword-only.
//
// We over-fetch the requested k by 2x because the hybrid ranker downstream
// may discard rows (issue deleted between embed and query, comment without
// access). The handler trims to its display limit after RRF.
func NearestNeighbours(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, workspaceID pgtype.UUID, vec []float32, k int) ([]VectorHit, error) {
	if !StoreAvailable(ctx, q) {
		return nil, nil
	}
	if len(vec) != EmbeddingDim {
		return nil, fmt.Errorf("nearest neighbours: wrong-dimension vector (%d, want %d)", len(vec), EmbeddingDim)
	}
	const sql = `
		SELECT target_type, target_id, embedding <=> $2::vector AS distance
		FROM cerebro_search_embedding
		WHERE workspace_id = $1
		ORDER BY embedding <=> $2::vector
		LIMIT $3
	`
	rows, err := q.Query(ctx, sql, workspaceID, formatVector(vec), k*2)
	if err != nil {
		return nil, fmt.Errorf("nearest neighbours: %w", err)
	}
	defer rows.Close()
	var out []VectorHit
	for rows.Next() {
		var h VectorHit
		if err := rows.Scan(&h.TargetType, &h.TargetID, &h.Distance); err != nil {
			return nil, fmt.Errorf("nearest neighbours scan: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
