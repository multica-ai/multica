package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type taskAccessRecheckFixture struct {
	pool         *pgxpool.Pool
	service      *TaskService
	workspaceID  string
	spaceID      string
	otherSpaceID string
	runtimeID    string
	ownerID      string
	memberID     string
}

func newTaskAccessRecheckFixture(t *testing.T) *taskAccessRecheckFixture {
	t.Helper()
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)

	fixture := &taskAccessRecheckFixture{pool: pool}
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Task Access Owner', 'task-access-owner-' || gen_random_uuid() || '@multica.test')
		RETURNING id
	`).Scan(&fixture.ownerID); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Task Access Member', 'task-access-member-' || gen_random_uuid() || '@multica.test')
		RETURNING id
	`).Scan(&fixture.memberID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('Task Access Recheck', 'task-access-recheck-' || gen_random_uuid())
		RETURNING id
	`).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, fixture.workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id IN ($1, $2)`, fixture.ownerID, fixture.memberID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner'), ($1, $3, 'member')
	`, fixture.workspaceID, fixture.ownerID, fixture.memberID); err != nil {
		t.Fatalf("seed workspace members: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, is_default, visibility, created_by)
		VALUES ($1, 'Current Space', 'CUR', true, 'open', $2)
		RETURNING id
	`, fixture.workspaceID, fixture.ownerID).Scan(&fixture.spaceID); err != nil {
		t.Fatalf("seed current Space: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, visibility, created_by)
		VALUES ($1, 'Other Space', 'OTH', 'open', $2)
		RETURNING id
	`, fixture.workspaceID, fixture.ownerID).Scan(&fixture.otherSpaceID); err != nil {
		t.Fatalf("seed other Space: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, 'Task Access Runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2, now())
		RETURNING id
	`, fixture.workspaceID, fixture.ownerID).Scan(&fixture.runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}

	queries := db.New(pool)
	fixture.service = NewTaskService(queries, pool, nil, events.New())
	return fixture
}

func (f *taskAccessRecheckFixture) createAgent(t *testing.T, permissionMode, availabilityMode string) string {
	t.Helper()
	visibility := "workspace"
	if permissionMode == "private" {
		visibility = "private"
	}
	var agentID string
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, availability_mode,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args
		)
		VALUES (
			$1, 'Task Access Agent ' || gen_random_uuid(), 'cloud', '{}'::jsonb, $2,
			$3, $4, $5, 1, $6, '', '{}'::jsonb, '[]'::jsonb
		)
		RETURNING id
	`, f.workspaceID, f.runtimeID, visibility, permissionMode, availabilityMode, f.ownerID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return agentID
}

func (f *taskAccessRecheckFixture) grantWorkspaceInvocation(t *testing.T, agentID string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		VALUES ($1, 'workspace', $2, $3)
	`, agentID, f.workspaceID, f.ownerID); err != nil {
		t.Fatalf("grant workspace invocation: %v", err)
	}
}

func (f *taskAccessRecheckFixture) createIssue(t *testing.T, creatorID, agentID string) db.Issue {
	t.Helper()
	var issueID string
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, space_id, title, creator_type, creator_id,
			assignee_type, assignee_id
		)
		VALUES ($1, $2, 'Task Access Issue ' || gen_random_uuid(), 'member', $3, 'agent', $4)
		RETURNING id
	`, f.workspaceID, f.spaceID, creatorID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	issue, err := f.service.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	return issue
}

func (f *taskAccessRecheckFixture) createFailedIssueTask(t *testing.T, issue db.Issue, agentID, originatorID string) db.AgentTaskQueue {
	t.Helper()
	var taskID string
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, completed_at,
			attempt, max_attempts, failure_reason, originator_user_id
		)
		VALUES ($1, $2, $3, 'failed', 0, now(), 1, 2, 'runtime_offline', $4)
		RETURNING id
	`, agentID, f.runtimeID, util.UUIDToString(issue.ID), originatorID).Scan(&taskID); err != nil {
		t.Fatalf("seed failed task: %v", err)
	}
	task, err := f.service.Queries.GetAgentTask(context.Background(), util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	return task
}

func TestMaybeRetryFailedIssueTaskRechecksInvocationAudience(t *testing.T) {
	t.Run("private owner remains allowed", func(t *testing.T) {
		fixture := newTaskAccessRecheckFixture(t)
		agentID := fixture.createAgent(t, "private", "private")
		issue := fixture.createIssue(t, fixture.ownerID, agentID)
		parent := fixture.createFailedIssueTask(t, issue, agentID, fixture.ownerID)

		child, err := fixture.service.MaybeRetryFailedTask(context.Background(), parent)
		if err != nil {
			t.Fatalf("retry private owner task: %v", err)
		}
		if child == nil || child.Status != "queued" {
			t.Fatalf("private owner retry = %#v, want queued child", child)
		}
	})

	t.Run("revoked member is denied", func(t *testing.T) {
		fixture := newTaskAccessRecheckFixture(t)
		agentID := fixture.createAgent(t, "public_to", "workspace")
		fixture.grantWorkspaceInvocation(t, agentID)
		issue := fixture.createIssue(t, fixture.memberID, agentID)
		parent := fixture.createFailedIssueTask(t, issue, agentID, fixture.memberID)

		if _, err := fixture.pool.Exec(context.Background(), `
			DELETE FROM agent_invocation_target WHERE agent_id = $1
		`, agentID); err != nil {
			t.Fatalf("revoke invocation audience: %v", err)
		}
		child, err := fixture.service.MaybeRetryFailedTask(context.Background(), parent)
		if err != nil {
			t.Fatalf("retry revoked member task: %v", err)
		}
		if child != nil {
			t.Fatalf("revoked member retry created child %s", util.UUIDToString(child.ID))
		}
		var retryCount int
		if err := fixture.pool.QueryRow(context.Background(), `
			SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1
		`, util.UUIDToString(parent.ID)).Scan(&retryCount); err != nil {
			t.Fatalf("count retry children: %v", err)
		}
		if retryCount != 0 {
			t.Fatalf("revoked member retry children = %d, want 0", retryCount)
		}
	})
}

func (f *taskAccessRecheckFixture) createDueDeferredTask(t *testing.T, issue db.Issue, agentID, originatorID string) db.AgentTaskQueue {
	t.Helper()
	var primaryTaskID string
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, completed_at, originator_user_id
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), $4)
		RETURNING id
	`, agentID, f.runtimeID, util.UUIDToString(issue.ID), originatorID).Scan(&primaryTaskID); err != nil {
		t.Fatalf("seed primary task: %v", err)
	}
	task, err := f.service.EnqueueDeferredAssigneeFallback(
		context.Background(),
		issue,
		util.MustParseUUID(agentID),
		pgtype.UUID{},
		util.MustParseUUID(primaryTaskID),
		pgtype.UUID{},
		time.Now().Add(-time.Second),
	)
	if err != nil {
		t.Fatalf("enqueue deferred fallback: %v", err)
	}
	// Deferred tasks now carry their attribution snapshot from creation time;
	// promotion revalidates that same principal against current Agent access.
	return task
}

func TestPromoteDueDeferredTaskRechecksAgentAccess(t *testing.T) {
	tests := []struct {
		name             string
		permissionMode   string
		availabilityMode string
		originator       func(*taskAccessRecheckFixture) string
		arrange          func(*testing.T, *taskAccessRecheckFixture, string)
		wantStatus       string
	}{
		{
			name:             "private owner remains runnable",
			permissionMode:   "private",
			availabilityMode: "private",
			originator:       func(f *taskAccessRecheckFixture) string { return f.ownerID },
			wantStatus:       "queued",
		},
		{
			name:             "revoked member audience is cancelled",
			permissionMode:   "public_to",
			availabilityMode: "workspace",
			originator:       func(f *taskAccessRecheckFixture) string { return f.memberID },
			arrange: func(t *testing.T, f *taskAccessRecheckFixture, agentID string) {
				f.grantWorkspaceInvocation(t, agentID)
			},
			wantStatus: "cancelled",
		},
		{
			name:             "revoked Space availability is cancelled",
			permissionMode:   "public_to",
			availabilityMode: "selected_spaces",
			originator:       func(f *taskAccessRecheckFixture) string { return f.ownerID },
			arrange: func(t *testing.T, f *taskAccessRecheckFixture, agentID string) {
				f.grantWorkspaceInvocation(t, agentID)
				if _, err := f.pool.Exec(context.Background(), `
					INSERT INTO agent_available_space (agent_id, workspace_id, space_id, created_by)
					VALUES ($1, $2, $3, $4)
				`, agentID, f.workspaceID, f.spaceID, f.ownerID); err != nil {
					t.Fatalf("grant current Space availability: %v", err)
				}
			},
			wantStatus: "cancelled",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTaskAccessRecheckFixture(t)
			agentID := fixture.createAgent(t, test.permissionMode, test.availabilityMode)
			if test.arrange != nil {
				test.arrange(t, fixture, agentID)
			}
			originatorID := test.originator(fixture)
			issue := fixture.createIssue(t, originatorID, agentID)
			deferred := fixture.createDueDeferredTask(t, issue, agentID, originatorID)

			switch test.name {
			case "revoked member audience is cancelled":
				if _, err := fixture.pool.Exec(context.Background(), `
					DELETE FROM agent_invocation_target WHERE agent_id = $1
				`, agentID); err != nil {
					t.Fatalf("revoke invocation audience: %v", err)
				}
			case "revoked Space availability is cancelled":
				if _, err := fixture.pool.Exec(context.Background(), `
					UPDATE agent_available_space SET space_id = $2 WHERE agent_id = $1
				`, agentID, fixture.otherSpaceID); err != nil {
					t.Fatalf("move availability away from issue Space: %v", err)
				}
			}

			if err := fixture.service.PromoteDueDeferredTasksForRuntime(
				context.Background(),
				util.MustParseUUID(fixture.runtimeID),
			); err != nil {
				t.Fatalf("promote due deferred tasks: %v", err)
			}
			got, err := fixture.service.Queries.GetAgentTask(context.Background(), deferred.ID)
			if err != nil {
				t.Fatalf("load deferred task after promotion: %v", err)
			}
			if got.Status != test.wantStatus {
				t.Fatalf("deferred status = %q, want %q", got.Status, test.wantStatus)
			}
			if test.wantStatus == "cancelled" && !got.CompletedAt.Valid {
				t.Fatal("cancelled deferred task must have completed_at")
			}
		})
	}
}
