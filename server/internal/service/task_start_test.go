package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type startReplayDB struct {
	mockDBTX
	startErr  error
	lookupErr error
	queries   []string
}

func (m *startReplayDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	m.queries = append(m.queries, sql)
	if strings.Contains(sql, "-- name: StartAgentTask :one") {
		return &mockRow{err: m.startErr}
	}
	if m.lookupErr != nil {
		return &mockRow{err: m.lookupErr}
	}
	return m.mockDBTX.QueryRow(ctx, sql, args...)
}

func TestStartTaskReplay(t *testing.T) {
	started := pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	for _, status := range []string{"running", "queued", "dispatched", "waiting_local_directory", "cancelled", "completed", "failed"} {
		t.Run(status, func(t *testing.T) {
			store := &startReplayDB{
				mockDBTX: mockDBTX{task: db.AgentTaskQueue{ID: testUUID(1), Status: status, StartedAt: started}},
				startErr: pgx.ErrNoRows,
			}
			svc := &TaskService{Queries: db.New(store)}
			got, err := svc.StartTask(context.Background(), store.task.ID)
			if status == "running" {
				if err != nil || got == nil {
					t.Fatalf("lost acknowledgement must be recoverable: task = %v, error = %v", got, err)
				}
				if got.ID != store.task.ID || got.StartedAt != started || got.Status != status {
					t.Fatalf("replay must preserve the committed start: %+v", got)
				}
			} else if got != nil || !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("replay must not revive or claim a %s task: task = %v, error = %v", status, got, err)
			}
			if len(store.queries) != 2 || !strings.Contains(store.queries[1], "-- name: GetAgentTask :one") {
				t.Fatalf("replay should only attempt the transition and read its result, got %d queries", len(store.queries))
			}
		})
	}
}

func TestStartTaskReplayReadFailure(t *testing.T) {
	for _, lookupErr := range []error{pgx.ErrNoRows, errors.New("database unavailable")} {
		store := &startReplayDB{startErr: pgx.ErrNoRows, lookupErr: lookupErr}
		svc := &TaskService{Queries: db.New(store)}
		got, err := svc.StartTask(context.Background(), testUUID(1))
		if got != nil || !errors.Is(err, lookupErr) {
			t.Fatalf("lookup failure must not acknowledge a start: task = %v, error = %v", got, err)
		}
	}
}

func TestStartTaskDatabaseFailureDoesNotAcknowledgeRunningTask(t *testing.T) {
	wantErr := errors.New("database unavailable")
	store := &startReplayDB{startErr: wantErr, mockDBTX: mockDBTX{task: db.AgentTaskQueue{Status: "running"}}}
	svc := &TaskService{Queries: db.New(store)}
	got, err := svc.StartTask(context.Background(), testUUID(1))
	if got != nil || !errors.Is(err, wantErr) || len(store.queries) != 1 {
		t.Fatalf("database failure must be preserved without a replay lookup: task = %v, error = %v, queries = %d", got, err, len(store.queries))
	}
}
