package analytics

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type resultRows interface {
	Next() bool
	Values() ([]any, error)
	Err() error
	Close()
}

type QueryResult struct {
	Columns    []string         `json:"columns"`
	Rows       []map[string]any `json:"rows"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type Executor struct {
	queryer queryer
}

func NewExecutor(queryer queryer) *Executor {
	return &Executor{queryer: queryer}
}

func (e *Executor) Execute(ctx context.Context, query Query, workspaceID string) (QueryResult, error) {
	if err := query.Normalize(); err != nil {
		return QueryResult{}, err
	}
	plan, err := BuildSQL(query, workspaceID)
	if err != nil {
		return QueryResult{}, err
	}
	rows, err := e.queryer.Query(ctx, plan.SQL, plan.Args...)
	if err != nil {
		return QueryResult{}, fmt.Errorf("analytics: execute query: %w", err)
	}
	defer rows.Close()
	return collectResult(query, rows)
}

func collectResult(query Query, rows resultRows) (QueryResult, error) {
	limit := query.Page.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	}
	columns := make([]string, 0, len(query.Dimensions)+len(query.Metrics))
	for _, dimension := range query.Dimensions {
		columns = append(columns, string(dimension))
	}
	for _, metric := range query.Metrics {
		columns = append(columns, string(metric))
	}
	result := QueryResult{Columns: columns, Rows: []map[string]any{}}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return QueryResult{}, fmt.Errorf("analytics: read row: %w", err)
		}
		if len(values) != len(columns) {
			return QueryResult{}, fmt.Errorf("analytics: row has %d columns, expected %d", len(values), len(columns))
		}
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			row[column] = values[i]
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("analytics: iterate rows: %w", err)
	}
	hasMore := len(result.Rows) > limit
	if hasMore {
		result.Rows = result.Rows[:limit]
		if contains(query.Dimensions, DimensionTime) && len(result.Rows) > 0 {
			value := result.Rows[len(result.Rows)-1][string(DimensionTime)]
			switch cursor := value.(type) {
			case time.Time:
				result.NextCursor = cursor.Format(time.RFC3339Nano)
			case string:
				if parsed, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
					result.NextCursor = parsed.Format(time.RFC3339Nano)
				}
			}
		} else {
			offset, _ := parseOffsetCursor(query.Page.Cursor)
			result.NextCursor = "offset:" + strconv.Itoa(offset+limit)
		}
	}
	return result, nil
}
