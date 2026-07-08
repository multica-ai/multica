package issueposition

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NextTopPosition returns a position that sorts before every existing issue in
// the workspace/status column when manual sorting orders by position ASC.
func NextTopPosition(ctx context.Context, q queryRower, workspaceID pgtype.UUID, status string) (float64, error) {
	var minPos float64
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(MIN(position), 0) FROM issue WHERE workspace_id = $1 AND status = $2`,
		workspaceID, status,
	).Scan(&minPos); err != nil {
		return 0, fmt.Errorf("query min issue position: %w", err)
	}
	return minPos - 1, nil
}
