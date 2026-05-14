package cascade

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// CascadeRow is the shape returned by ListCascades. Mirrors the
// fields the dashboard surfaces — issue identity, cascade state,
// progress summary, last event time. Status (the orthogonal multica
// issue.status) is included so the dashboard renders the right badge
// next to the cascade state.
type CascadeRow struct {
	IssueID            uuid.UUID
	IssueNumber        int32
	IssueTitle         string
	IssueStatus        string
	IssueAssigneeID    *uuid.UUID
	IssueAssigneeType  string
	CascadeState       string
	CascadeStartedAt   time.Time
	CascadeLastEventAt *time.Time
	// Progress is the decoded JSONB. Nil when the row hasn't been
	// initialized yet (atomic init not run); the dashboard renders
	// "—" in that case.
	Progress *Progress
}

// ListFilters narrows the result set. Empty fields = no filter.
type ListFilters struct {
	WorkspaceID uuid.UUID
	CascadeState string    // approved | paused | loop_guarded | completed; empty = all non-NULL
	AgentID      uuid.UUID // assignee_id filter; zero = no filter
}

// ListPage is the pagination cursor. Page is 1-indexed; PerPage is
// capped at 50 by ListCascades regardless of caller input.
type ListPage struct {
	Page    int
	PerPage int
}

// pgQuerier is the read-side equivalent of pgPool — exists so tests
// can substitute without standing up Postgres, though the production
// path is straight pgxpool.Pool.
type pgQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ListCascades returns one page of active cascades for the workspace.
// Ordered cascade_last_event_at DESC (most recently progressed first),
// with NULL last-event-times sorting after non-NULL so freshly
// approved cascades land below cascades that are actually moving.
//
// Hit path: idx_issue_cascade_active (partial on cascade_state IS NOT NULL).
// EXPLAIN ANALYZE this query on the prod-shape DB and ensure
// "Index Scan using idx_issue_cascade_active" appears — the partial
// index is what keeps p99 ≤ 2s with 100 active cascades.
//
// The Progress decode is best-effort: a malformed JSONB row surfaces
// with nil Progress instead of failing the whole page, so one bad
// row never bricks the dashboard.
func ListCascades(ctx context.Context, q pgQuerier, filters ListFilters, page ListPage) ([]CascadeRow, error) {
	if filters.WorkspaceID == uuid.Nil {
		return nil, fmt.Errorf("cascade: ListCascades requires WorkspaceID")
	}
	if page.PerPage <= 0 || page.PerPage > 50 {
		page.PerPage = 50
	}
	if page.Page < 1 {
		page.Page = 1
	}
	offset := (page.Page - 1) * page.PerPage

	args := []any{pgtype.UUID{Bytes: filters.WorkspaceID, Valid: true}}
	conds := []string{
		"workspace_id = $1",
		"cascade_state IS NOT NULL",
	}
	if filters.CascadeState != "" {
		args = append(args, filters.CascadeState)
		conds = append(conds, fmt.Sprintf("cascade_state = $%d", len(args)))
	}
	if filters.AgentID != uuid.Nil {
		args = append(args, pgtype.UUID{Bytes: filters.AgentID, Valid: true})
		conds = append(conds, fmt.Sprintf("assignee_id = $%d", len(args)))
	}

	args = append(args, page.PerPage, offset)
	limitPlaceholder := fmt.Sprintf("$%d", len(args)-1)
	offsetPlaceholder := fmt.Sprintf("$%d", len(args))

	sql := fmt.Sprintf(`
SELECT id, number, title, status, assignee_id, assignee_type,
       cascade_state, cascade_started_at, cascade_last_event_at, cascade_progress
FROM issue
WHERE %s
ORDER BY cascade_last_event_at DESC NULLS LAST, cascade_started_at DESC NULLS LAST
LIMIT %s OFFSET %s`, strings.Join(conds, " AND "), limitPlaceholder, offsetPlaceholder)

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("cascade: query: %w", err)
	}
	defer rows.Close()

	out := make([]CascadeRow, 0, page.PerPage)
	for rows.Next() {
		var (
			id          pgtype.UUID
			number      int32
			title       string
			status      string
			assigneeID  pgtype.UUID
			assigneeTyp pgtype.Text
			state       pgtype.Text
			startedAt   pgtype.Timestamptz
			lastEventAt pgtype.Timestamptz
			progressRaw []byte
		)
		if err := rows.Scan(&id, &number, &title, &status, &assigneeID, &assigneeTyp, &state, &startedAt, &lastEventAt, &progressRaw); err != nil {
			return nil, fmt.Errorf("cascade: scan: %w", err)
		}

		row := CascadeRow{
			IssueID:     uuid.UUID(id.Bytes),
			IssueNumber: number,
			IssueTitle:  title,
			IssueStatus: status,
		}
		if assigneeID.Valid {
			a := uuid.UUID(assigneeID.Bytes)
			row.IssueAssigneeID = &a
		}
		if assigneeTyp.Valid {
			row.IssueAssigneeType = assigneeTyp.String
		}
		if state.Valid {
			row.CascadeState = state.String
		}
		if startedAt.Valid {
			row.CascadeStartedAt = startedAt.Time
		}
		if lastEventAt.Valid {
			t := lastEventAt.Time
			row.CascadeLastEventAt = &t
		}
		if len(progressRaw) > 0 {
			if p, perr := UnmarshalProgress(progressRaw); perr == nil {
				row.Progress = &p
			}
			// On decode failure: leave nil; caller renders "—".
			// We intentionally do not fail the whole page.
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cascade: iterate: %w", err)
	}
	return out, nil
}

