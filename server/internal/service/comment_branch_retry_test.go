package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentBranchIsNotEligibleForGenericAutoRetry(t *testing.T) {
	task := db.AgentTaskQueue{
		Attempt:       1,
		MaxAttempts:   2,
		IssueID:       pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		BranchContext: []byte(`{"version":1}`),
	}
	if retryEligible("timeout", task) {
		t.Fatal("comment branch became eligible for generic retry without preserving its frozen snapshot")
	}
}

func TestDeferredCommentBranchPairScanIsBoundedAndSkipsBlockedPairs(t *testing.T) {
	svc, pool, _, blockedAgentID, issueID, runtimeID := rerunQueueFixture(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	insertAgent := func(label string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent (
				workspace_id, name, description, runtime_mode, runtime_config,
				runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
			)
			SELECT workspace_id, $2, description, runtime_mode, runtime_config,
				runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
			FROM agent WHERE id = $1
			RETURNING id
		`, blockedAgentID, fmt.Sprintf("branch-sweep-%s-%d", label, suffix)).Scan(&id); err != nil {
			t.Fatalf("insert %s agent: %v", label, err)
		}
		return id
	}
	promotableFirstAgentID := insertAgent("first")
	promotableSecondAgentID := insertAgent("second")

	var taskIDs []pgtype.UUID
	insertTask := func(agentID, status string, createdAt time.Time, branch bool) {
		t.Helper()
		var id pgtype.UUID
		if branch {
			if err := pool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (
					agent_id, runtime_id, issue_id, status, priority, created_at,
					branch_point_comment_id, branch_context, branch_request_id
				)
				VALUES ($1, $2, $3, $4, 0, $5, gen_random_uuid(), '{"version":1}'::jsonb, gen_random_uuid())
				RETURNING id
			`, agentID, runtimeID, issueID, status, createdAt).Scan(&id); err != nil {
				t.Fatalf("insert %s branch task: %v", status, err)
			}
		} else if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
			VALUES ($1, $2, $3, $4, 0, $5)
			RETURNING id
		`, agentID, runtimeID, issueID, status, createdAt).Scan(&id); err != nil {
			t.Fatalf("insert %s ordinary task: %v", status, err)
		}
		taskIDs = append(taskIDs, id)
	}
	now := time.Now().UTC()
	insertTask(blockedAgentID, "deferred", now.Add(-4*time.Minute), true)
	insertTask(blockedAgentID, "running", now.Add(-3*time.Minute), false)
	insertTask(promotableFirstAgentID, "deferred", now.Add(-2*time.Minute), true)
	insertTask(promotableSecondAgentID, "deferred", now.Add(-time.Minute), true)

	t.Cleanup(func() {
		for _, id := range taskIDs {
			_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, id)
		}
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent WHERE id = ANY($1::uuid[])`, []string{promotableFirstAgentID, promotableSecondAgentID})
	})

	pairs, err := svc.Queries.ListDeferredCommentBranchPairs(ctx, 1)
	if err != nil {
		t.Fatalf("ListDeferredCommentBranchPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("pair scan returned %d rows, want hard limit 1", len(pairs))
	}
	if got := util.UUIDToString(pairs[0].AgentID); got != promotableFirstAgentID {
		t.Fatalf("pair scan selected agent %s, want oldest promotable %s; blocked head must not starve it", got, promotableFirstAgentID)
	}
}

func TestDeferredCommentBranchSuppressesAutomaticRetrySuccessor(t *testing.T) {
	svc, pool, _, agentID, issueID, runtimeID := rerunQueueFixture(t)
	ctx := context.Background()
	var branchID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			branch_point_comment_id, branch_context, branch_request_id
		)
		VALUES ($1, $2, $3, 'deferred', 0, gen_random_uuid(), '{"version":1}'::jsonb, gen_random_uuid())
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&branchID); err != nil {
		t.Fatalf("insert deferred comment branch: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, branchID) })

	hasSuccessor, err := hasRunnableSuccessor(ctx, svc.Queries, db.AgentTaskQueue{
		IssueID: util.MustParseUUID(issueID),
		AgentID: util.MustParseUUID(agentID),
	})
	if err != nil {
		t.Fatalf("hasRunnableSuccessor: %v", err)
	}
	if !hasSuccessor {
		t.Fatal("deferred comment branch was not treated as the human-selected successor")
	}
}
