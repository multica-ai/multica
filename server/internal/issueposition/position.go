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

// NextTopPositionForSpace returns a position that sorts before every existing
// issue in the (workspace, space, status) column when manual sorting orders by
// position ASC.
func NextTopPositionForSpace(ctx context.Context, q queryRower, workspaceID, spaceID pgtype.UUID, status string) (float64, error) {
	var minPos float64
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(MIN(position), 0) FROM issue WHERE workspace_id = $1 AND space_id = $2 AND status = $3`,
		workspaceID, spaceID, status,
	).Scan(&minPos); err != nil {
		return 0, fmt.Errorf("query min space issue position: %w", err)
	}
	return minPos - 1, nil
}
