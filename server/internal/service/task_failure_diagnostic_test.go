package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestNormalizeTaskFailureDiagnostic(t *testing.T) {
	tests := []struct {
		name       string
		diagnostic string
		want       string
	}{
		{
			name:       "valid UTF-8 is unchanged",
			diagnostic: "worker 失败：诊断详情",
			want:       "worker 失败：诊断详情",
		},
		{
			name:       "embedded NUL is removed",
			diagnostic: "worker failed\x00 diagnostic details",
			want:       "worker failed diagnostic details",
		},
		{
			name:       "invalid UTF-8 is replaced",
			diagnostic: string([]byte{'b', 'a', 'd', 0xff, 't', 'e', 'x', 't'}),
			want:       "bad\uFFFDtext",
		},
		{
			name:       "only NUL becomes empty",
			diagnostic: "\x00\x00",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeTaskFailureDiagnostic(tt.diagnostic); got != tt.want {
				t.Fatalf("normalizeTaskFailureDiagnostic() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFailTaskSanitizesNULDiagnostic(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	_, _, agentID, _ := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load agent runtime: %v", err)
	}

	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue
			(agent_id, runtime_id, status, priority, attempt, max_attempts, started_at)
		VALUES ($1, $2, 'running', 0, 1, 1, now())
		RETURNING id`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	if _, err := svc.FailTask(ctx, util.MustParseUUID(taskID), "worker failed\x00 diagnostic details", "", "", "", "agent_error", false, ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var (
		status      string
		completedAt pgtype.Timestamptz
		diagnostic  string
	)
	if err := pool.QueryRow(ctx, `
		SELECT status, completed_at, error
		FROM agent_task_queue
		WHERE id = $1`, taskID).Scan(&status, &completedAt, &diagnostic); err != nil {
		t.Fatalf("read failed task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("status = %q, want failed", status)
	}
	if !completedAt.Valid {
		t.Fatal("completed_at is NULL, want terminal timestamp")
	}
	if diagnostic != "worker failed diagnostic details" {
		t.Fatalf("error = %q, want %q", diagnostic, "worker failed diagnostic details")
	}
}
