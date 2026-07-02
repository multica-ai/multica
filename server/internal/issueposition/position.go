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

// NextTopPositionForTeam returns a position that sorts before every existing
// issue in the (workspace, team, status) column when manual sorting orders by
// position ASC.
func NextTopPositionForTeam(ctx context.Context, q queryRower, workspaceID, teamID pgtype.UUID, status string) (float64, error) {
	var minPos float64
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(MIN(position), 0) FROM issue WHERE workspace_id = $1 AND team_id = $2 AND status = $3`,
		workspaceID, teamID, status,
	).Scan(&minPos); err != nil {
		return 0, fmt.Errorf("query min team issue position: %w", err)
	}
	return minPos - 1, nil
}
