package service

// CEREBRO-PATCH(retry-pause-guard-test): FIR-2442 — regression coverage for
// the retry-pause-guard. When the parent task's runtime is paused, the
// auto-retry path must skip retry creation entirely so the queue does not
// fill up with dead `runtime_paused` rows.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// pausedRuntimeRow scans an AgentRuntime row where every Timestamptz column
// reports as Valid. The only field MaybeRetryFailedTask inspects is
// PausedAt.Valid, so loosely-typed assignment is fine and keeps the test
// resilient to additional Timestamptz columns being added to agent_runtime.
type pausedRuntimeRow struct{}

func (r *pausedRuntimeRow) Scan(dest ...any) error {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	for _, d := range dest {
		switch v := d.(type) {
		case *pgtype.Timestamptz:
			*v = now
		}
	}
	return nil
}

// pauseGuardMockDBTX routes runtime lookups to a paused row and treats any
// other query as a test failure. If MaybeRetryFailedTask reaches
// CreateRetryTask (an INSERT) the test should fail loudly, since the guard
// is supposed to short-circuit before that point.
type pauseGuardMockDBTX struct {
	t           *testing.T
	createCalls int
}

func (m *pauseGuardMockDBTX) Exec(_ context.Context, sql string, _ ...interface{}) (pgconn.CommandTag, error) {
	m.t.Fatalf("unexpected Exec call: %s", sql)
	return pgconn.NewCommandTag(""), nil
}

func (m *pauseGuardMockDBTX) Query(_ context.Context, sql string, _ ...interface{}) (pgx.Rows, error) {
	m.t.Fatalf("unexpected Query call: %s", sql)
	return nil, pgx.ErrNoRows
}

func (m *pauseGuardMockDBTX) QueryRow(_ context.Context, sql string, _ ...interface{}) pgx.Row {
	if strings.Contains(sql, "FROM agent_runtime") && strings.Contains(sql, "WHERE id = $1") {
		return &pausedRuntimeRow{}
	}
	if strings.Contains(sql, "INSERT INTO agent_task_queue") {
		m.createCalls++
		m.t.Fatalf("retry-pause-guard failed: CreateRetryTask was invoked on a paused runtime")
	}
	m.t.Fatalf("unexpected QueryRow call: %s", sql)
	return nil
}

func TestMaybeRetryFailedTask_SkipsWhenRuntimePaused(t *testing.T) {
	mock := &pauseGuardMockDBTX{t: t}
	svc := &TaskService{Queries: db.New(mock)}

	parent := db.AgentTaskQueue{
		ID:            testUUID(1),
		AgentID:       testUUID(2),
		IssueID:       testUUID(3),
		RuntimeID:     testUUID(4),
		Status:        "failed",
		FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
		Attempt:       1,
		MaxAttempts:   3,
	}

	child, err := svc.MaybeRetryFailedTask(context.Background(), parent)
	if err != nil {
		t.Fatalf("MaybeRetryFailedTask returned error: %v", err)
	}
	if child != nil {
		t.Fatalf("expected nil child (no retry), got %+v", child)
	}
	if mock.createCalls != 0 {
		t.Fatalf("expected 0 CreateRetryTask calls, got %d", mock.createCalls)
	}
}
