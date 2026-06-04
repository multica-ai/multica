package logger

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type sqlTraceContextKey struct{}

type sqlTraceState struct {
	start     time.Time
	name      string
	operation string
	table     string
	sql       string
	argCount  int
}

// SQLTracer logs pgx query summaries without logging raw argument values.
type SQLTracer struct {
	Detail bool
}

// NewSQLTracer creates a pgx query tracer controlled by LOG_SQL_DETAIL.
func NewSQLTracer(cfg Config) *SQLTracer {
	return &SQLTracer{Detail: cfg.SQLDetail}
}

// TraceQueryStart records query metadata at the start of a pgx query.
func (t *SQLTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	state := sqlTraceState{
		start:     time.Now(),
		name:      queryName(data.SQL),
		operation: queryOperation(data.SQL),
		table:     queryTable(data.SQL),
		sql:       normalizeSQL(data.SQL),
		argCount:  len(data.Args),
	}
	return context.WithValue(ctx, sqlTraceContextKey{}, state)
}

// TraceQueryEnd emits a completed query log entry.
func (t *SQLTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, _ := ctx.Value(sqlTraceContextKey{}).(sqlTraceState)
	duration := time.Since(state.start)
	attrs := []any{
		"name", state.name,
		"operation", state.operation,
		"table", state.table,
		"duration", duration.Round(time.Microsecond).String(),
		"rows_affected", data.CommandTag.RowsAffected(),
	}
	if t.Detail {
		attrs = append(attrs, "sql", truncateString(state.sql, 1000), "arg_count", state.argCount)
	}
	if data.Err != nil {
		attrs = append(attrs, "error", data.Err)
		slog.Warn("sql query", attrs...)
		return
	}
	slog.Debug("sql query", attrs...)
}

func queryName(sql string) string {
	trimmed := strings.TrimSpace(sql)
	if !strings.HasPrefix(trimmed, "-- name:") {
		return ""
	}
	line := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(trimmed, "\n", 2)[0], "-- name:"))
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func queryOperation(sql string) string {
	normalized := stripLeadingSQLComment(sql)
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

func queryTable(sql string) string {
	normalized := strings.ToLower(stripLeadingSQLComment(sql))
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bfrom\s+([a-z_][a-z0-9_\.]*)`),
		regexp.MustCompile(`(?i)\binto\s+([a-z_][a-z0-9_\.]*)`),
		regexp.MustCompile(`(?i)\bupdate\s+([a-z_][a-z0-9_\.]*)`),
		regexp.MustCompile(`(?i)\btable\s+if\s+not\s+exists\s+([a-z_][a-z0-9_\.]*)`),
	}
	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(normalized)
		if len(match) > 1 {
			return strings.Trim(match[1], `"`)
		}
	}
	return ""
}

func normalizeSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

func stripLeadingSQLComment(sql string) string {
	trimmed := strings.TrimSpace(sql)
	for strings.HasPrefix(trimmed, "--") {
		parts := strings.SplitN(trimmed, "\n", 2)
		if len(parts) != 2 {
			return ""
		}
		trimmed = strings.TrimSpace(parts[1])
	}
	return trimmed
}
