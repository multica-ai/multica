package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestFinalizeTaskClaimFailureRollsBackTokenThenRequeue is the regression for
// the PR #5193 review ask on the batch path's failure handling: when
// FinalizeTaskClaim fails (here: a delivery receipt referencing an id outside
// the task's comment plan, which the CAS subset-guard rejects), the token write
// is rolled back with it, and RequeueTaskAfterClaimFailure releases the exact
// dispatched claim back to `queued`. These are the two building blocks the
// batch handler composes on a finalization error.
func TestFinalizeTaskClaimFailureRollsBackTokenThenRequeue(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	queries := db.New(pool)

	taskID, userID, workspaceID := dispatchedCommentTaskFixture(t, ctx, pool)
	task, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}

	// A delivered id outside the task's plan (no trigger/coalesced match) makes
	// SetTaskDeliveredCommentIDs match zero rows → FinalizeTaskClaim errors.
	bogus := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	selectedSkill := util.MustParseUUID("22222222-2222-2222-2222-222222222222")
	_, ferr := svc.FinalizeTaskClaim(ctx, task, db.CreateTaskTokenParams{
		TokenHash:   fmt.Sprintf("finalize-fail-hash-%d", time.Now().UnixNano()),
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		WorkspaceID: util.MustParseUUID(workspaceID),
		UserID:      util.MustParseUUID(userID),
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}, []pgtype.UUID{bogus}, true, []pgtype.UUID{selectedSkill})
	if ferr == nil {
		t.Fatal("expected FinalizeTaskClaim to fail for an out-of-plan delivery receipt")
	}
	if errors.Is(ferr, context.Canceled) {
		t.Fatalf("unexpected ctx error: %v", ferr)
	}

	// Token write must have rolled back with the receipt (transactional).
	var tokenCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_token WHERE task_id = $1`, taskID).Scan(&tokenCount); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("expected token rolled back, found %d task_token rows", tokenCount)
	}
	var selectedGrantPersisted bool
	if err := pool.QueryRow(ctx, `SELECT context ? 'selected_skill_ids' FROM agent_task_queue WHERE id = $1`, taskID).Scan(&selectedGrantPersisted); err != nil {
		t.Fatalf("read selected Skill grant: %v", err)
	}
	if selectedGrantPersisted {
		t.Fatal("selected Skill grant survived failed claim finalization")
	}

	// The exact dispatched claim is released back to queued.
	if _, err := svc.RequeueTaskAfterClaimFailure(ctx, task); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want queued after requeue", status)
	}
}

func TestFinalizeTaskClaimWithoutSelectedSkillsPreservesContext(t *testing.T) {
	tests := []struct {
		name              string
		initialContext    string
		wantObject        bool
		wantSelectedCount int
	}{
		{name: "ordinary object skips grant CAS", initialContext: `{"keep":"value"}`, wantObject: true, wantSelectedCount: -1},
		{name: "stale grant is cleared safely", initialContext: `{"keep":"value","selected_skill_ids":["22222222-2222-2222-2222-222222222222"]}`, wantObject: true, wantSelectedCount: 0},
		{name: "legacy non-object context remains claimable", initialContext: `null`, wantObject: false, wantSelectedCount: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pool := newTaskClaimRacePool(t)
			svc := NewTaskService(db.New(pool), pool, nil, events.New())
			queries := db.New(pool)

			taskID, userID, workspaceID := dispatchedCommentTaskFixture(t, ctx, pool)
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET context = $2::jsonb WHERE id = $1`, taskID, tt.initialContext); err != nil {
				t.Fatalf("seed task context: %v", err)
			}
			task, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
			if err != nil {
				t.Fatalf("load task: %v", err)
			}

			if _, err := svc.FinalizeTaskClaim(ctx, task, db.CreateTaskTokenParams{
				TokenHash:   fmt.Sprintf("finalize-empty-skill-%s-%d", strings.ReplaceAll(tt.name, " ", "-"), time.Now().UnixNano()),
				TaskID:      task.ID,
				AgentID:     task.AgentID,
				WorkspaceID: util.MustParseUUID(workspaceID),
				UserID:      util.MustParseUUID(userID),
				ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
			}, nil, false, nil); err != nil {
				t.Fatalf("FinalizeTaskClaim with no selected Skills: %v", err)
			}

			var raw []byte
			if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw); err != nil {
				t.Fatalf("read task context: %v", err)
			}
			if !tt.wantObject {
				if string(raw) != "null" {
					t.Fatalf("non-object context = %s, want null", raw)
				}
				return
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatalf("decode preserved context: %v", err)
			}
			if string(object["keep"]) != `"value"` {
				t.Fatalf("pre-existing context key lost: %s", raw)
			}
			selectedRaw, present := object["selected_skill_ids"]
			if tt.wantSelectedCount < 0 {
				if present {
					t.Fatalf("empty grant unexpectedly added a context key: %s", raw)
				}
				return
			}
			var selected []string
			if !present {
				t.Fatalf("stale grant key was not cleared: %s", raw)
			}
			if err := json.Unmarshal(selectedRaw, &selected); err != nil {
				t.Fatalf("decode selected skill ids: %v", err)
			}
			if len(selected) != tt.wantSelectedCount {
				t.Fatalf("selected skill count = %d, want %d", len(selected), tt.wantSelectedCount)
			}
		})
	}
}

// dispatchedCommentTaskFixture provisions a comment-backed task already in the
// `dispatched` state (never started), returning (taskID, ownerUserID).
func dispatchedCommentTaskFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (taskID, userID, workspaceID string) {
	t.Helper()
	suffix := time.Now().UnixNano()

	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1,$2) RETURNING id`,
		"Finalize Fail Test", fmt.Sprintf("finalize-fail-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1,$2,$3,$4) RETURNING id`,
		"Finalize Fail Test", fmt.Sprintf("finalize-fail-%d", suffix), "temp finalize-fail test", "FFR").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'owner')`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'daemon-ff', 'FF RT', 'cloud', 'ff_provider', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'FF Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 5, $3)
		RETURNING id`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'ff issue', 'in_progress', 'none', $2, 'member', 600001, 0)
		RETURNING id`, workspaceID, userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	var commentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'ff comment')
		RETURNING id`, workspaceID, issueID, userID).Scan(&commentID); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, context, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, 'dispatched', 0, '{}'::jsonb, now(), NULL)
		RETURNING id`, agentID, runtimeID, issueID, commentID).Scan(&taskID); err != nil {
		t.Fatalf("create dispatched task: %v", err)
	}

	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM task_token WHERE task_id = $1`, taskID)
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(c, `DELETE FROM comment WHERE id = $1`, commentID)
		pool.Exec(c, `DELETE FROM issue WHERE id = $1`, issueID)
		pool.Exec(c, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(c, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(c, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(c, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return taskID, userID, workspaceID
}
