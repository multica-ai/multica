package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCreateRetryTaskFireAtControlsDeferral locks in the SQL half of the
// three-tier provider_network schedule (MUL-4910): CreateRetryTask inserts a
// 'deferred' child carrying fire_at when the fire_at param is set (the final,
// backed-off attempt) and an immediately-claimable 'queued' child when it is
// NULL (every other retry). Both continue the resume chain — force_fresh_session
// stays false for a provider_network parent.
func TestCreateRetryTaskFireAtControlsDeferral(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	// agent_task_queue.runtime_id is NOT NULL; reuse the fixture agent's runtime.
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}

	// Parent: a provider_network failure on its second attempt — the point at
	// which the schedule wants the next (final) retry deferred.
	var parentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts, failure_reason, session_id, work_dir)
		VALUES ($1, $2, $3, 'failed', 0, 2, 2, 'agent_error.provider_network', 'src-session', '/tmp/src-workdir')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("insert parent task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1`, parentID)
	})

	cases := []struct {
		name       string
		fireAt     pgtype.Timestamptz
		wantStatus string
		wantFireAt bool
	}{
		{"deferred when fire_at set", pgtype.Timestamptz{Time: time.Now().Add(5 * time.Second), Valid: true}, "deferred", true},
		{"queued when fire_at null", pgtype.Timestamptz{}, "queued", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			child, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parentID, FireAt: tc.fireAt})
			if err != nil {
				t.Fatalf("CreateRetryTask: %v", err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, child.ID) })

			if child.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", child.Status, tc.wantStatus)
			}
			if child.FireAt.Valid != tc.wantFireAt {
				t.Errorf("fire_at valid = %v, want %v", child.FireAt.Valid, tc.wantFireAt)
			}
			if child.Attempt != 3 {
				t.Errorf("attempt = %d, want 3 (parent attempt 2 + 1)", child.Attempt)
			}
			// provider_network is resume-safe: the retry must continue the session.
			if child.ForceFreshSession {
				t.Errorf("force_fresh_session = true, want false (provider_network resumes) for %s", util.UUIDToString(child.ID))
			}
		})
	}
}
