package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type startReplayDB struct {
	mockDBTX
	startErr error
	calls    int
}

func (m *startReplayDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	m.calls++
	return &mockRow{err: m.startErr}
}

func TestStartTaskLegacyDoesNotAcknowledgeReplayOrDatabaseFailure(t *testing.T) {
	for _, want := range []error{pgx.ErrNoRows, errors.New("database unavailable")} {
		store := &startReplayDB{startErr: want}
		svc := &TaskService{Queries: db.New(store)}
		got, err := svc.StartTask(context.Background(), testUUID(1))
		if got != nil || !errors.Is(err, want) || store.calls != 1 {
			t.Fatalf("legacy start: task=%v err=%v queries=%d", got, err, store.calls)
		}
	}
}
