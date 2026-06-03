package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSQLTracerLogsSummaryWithoutSQLDetail(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	tracer := NewSQLTracer(Config{SQLDetail: false})
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "-- name: GetIssue :one\nSELECT * FROM issue WHERE id = $1",
		Args: []any{"secret"},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 1")})

	logs := buf.String()
	for _, want := range []string{`msg="sql query"`, `name=GetIssue`, `operation=SELECT`, `table=issue`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("expected log to contain %q, got %s", want, logs)
		}
	}
	if strings.Contains(logs, "SELECT * FROM issue") || strings.Contains(logs, "secret") {
		t.Fatalf("expected summary log to omit SQL text and args, got %s", logs)
	}
}

func TestSQLTracerLogsDetailWithoutArgs(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	tracer := NewSQLTracer(Config{SQLDetail: true})
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "-- name: UpdateIssue :one\nUPDATE issue SET title = $1 WHERE id = $2",
		Args: []any{"sensitive title", "id"},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("UPDATE 1")})

	logs := buf.String()
	if !strings.Contains(logs, `sql="-- name: UpdateIssue :one UPDATE issue SET title = $1 WHERE id = $2"`) {
		t.Fatalf("expected detailed SQL text, got %s", logs)
	}
	if !strings.Contains(logs, "arg_count=2") {
		t.Fatalf("expected arg count, got %s", logs)
	}
	if strings.Contains(logs, "sensitive title") {
		t.Fatalf("expected detailed log to omit raw args, got %s", logs)
	}
}
