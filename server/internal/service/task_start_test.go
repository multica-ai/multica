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

type startReplayTxStarter struct{ store *startReplayDB }

func (s startReplayTxStarter) Begin(context.Context) (pgx.Tx, error) {
	return &startReplayTx{store: s.store}, nil
}

type startReplayTx struct {
	pgx.Tx
	store *startReplayDB
}

func (tx *startReplayTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.store.QueryRow(ctx, sql, args...)
}

func (*startReplayTx) Rollback(context.Context) error { return nil }

func (m *startReplayDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	m.calls++
	return &mockRow{err: m.startErr}
}

func TestStartTaskLegacyDoesNotAcknowledgeReplayOrDatabaseFailure(t *testing.T) {
	for _, want := range []error{pgx.ErrNoRows, errors.New("database unavailable")} {
		store := &startReplayDB{startErr: want}
		svc := &TaskService{Queries: db.New(store), TxStarter: startReplayTxStarter{store: store}}
		got, err := svc.StartTask(context.Background(), testUUID(1))
		if got != nil || !errors.Is(err, want) || store.calls != 1 {
			t.Fatalf("legacy start: task=%v err=%v queries=%d", got, err, store.calls)
		}
	}
}
