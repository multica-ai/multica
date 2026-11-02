package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type terminalLookupDB struct {
	reads      int
	generation time.Time
	lookupErr  error
}

type terminalLookupRow func(...any) error

func (f terminalLookupRow) Scan(dest ...any) error { return f(dest...) }

func (d *terminalLookupDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}

func (d *terminalLookupDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}

func (d *terminalLookupDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if !strings.Contains(sql, "-- name: GetAgentTask :one") {
		return terminalLookupRow(func(...any) error { return pgx.ErrNoRows })
	}
	d.reads++
	if d.reads == 1 {
		return terminalLookupRow(func(...any) error { return d.lookupErr })
	}
	return terminalLookupRow(func(dest ...any) error {
		*dest[3].(*string) = "running"
		*dest[5].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: d.generation, Valid: true}
		return nil
	})
}

func TestFencedTerminalLookupFailureRemainsRetryable(t *testing.T) {
	generation := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for _, kind := range []string{"complete", "fail"} {
		t.Run(kind, func(t *testing.T) {
			lookupErr := errors.New("temporary connection failure")
			fake := &terminalLookupDB{generation: generation.Add(time.Second), lookupErr: lookupErr}
			svc := &TaskService{Queries: db.New(fake)}
			var task *db.AgentTaskQueue
			var changed bool
			var err error
			if kind == "complete" {
				task, changed, err = svc.CompleteTaskWithTransition(context.Background(), pgtype.UUID{}, nil, "", "", "", false, "", "", generation)
			} else {
				task, changed, err = svc.FailTaskWithTransition(context.Background(), pgtype.UUID{}, "cancelled", "", "", "", "cancelled", false, "", "", generation)
			}
			if !errors.Is(err, lookupErr) || task != nil || changed {
				t.Fatalf("unknown generation must remain retryable: task=%v transitioned=%v err=%v", task, changed, err)
			}
			if fake.reads != 1 {
				t.Fatalf("retried classification through the unfenced path: reads=%d", fake.reads)
			}
		})
	}
}
