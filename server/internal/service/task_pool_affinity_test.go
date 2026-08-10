package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
	"github.com/multica-ai/multica/server/internal/runtimepool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestResolvePoolAffinityExplicitFreshWins(t *testing.T) {
	svc := &TaskService{}
	placement, err := svc.ResolvePoolTaskPlacement(context.Background(), PoolPlacementRequest{
		ExplicitFreshSession: true,
	})
	if err != nil {
		t.Fatalf("ResolvePoolTaskPlacement: %v", err)
	}
	if placement.State != "none" || placement.RuntimeID.Valid {
		t.Fatalf("placement = %+v, want affinity none with no Runtime", placement)
	}
}

type poolAffinityFixture struct {
	t           *testing.T
	ctx         context.Context
	tx          pgx.Tx
	service     *TaskService
	workspaceID pgtype.UUID
	userID      pgtype.UUID
	agentID     pgtype.UUID
	issueID     pgtype.UUID
}

func newPoolAffinityFixture(t *testing.T) *poolAffinityFixture {
	t.Helper()
	ctx := context.Background()
	pool := pooltestdb.Open(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin affinity fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback affinity fixture: %v", err)
		}
	})
	fixture := &poolAffinityFixture{t: t, ctx: ctx, tx: tx}
	fixture.service = &TaskService{Queries: db.New(tx)}
	seed := time.Now().UnixNano()
	if err := tx.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Pool Affinity User', $1)
		RETURNING id
	`, fmt.Sprintf("pool-affinity-%d@example.test", seed)).Scan(&fixture.userID); err != nil {
		t.Fatalf("create affinity user: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Pool Affinity', $1, '', 'PAF')
		RETURNING id
	`, fmt.Sprintf("pool-affinity-%d", seed)).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("create affinity Workspace: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("create affinity Member: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id, instructions,
			custom_env, custom_args, runtime_binding_mode, runtime_requirements
		) VALUES (
			$1, 'Pool Affinity Agent', '', 'pool', '{}'::jsonb, NULL,
			'private', 'private', 1, $2, '', '{}'::jsonb, '[]'::jsonb, 'pool', '{}'::jsonb
		)
		RETURNING id
	`, fixture.workspaceID, fixture.userID).Scan(&fixture.agentID); err != nil {
		t.Fatalf("create affinity Agent: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, assignee_type, assignee_id
		) VALUES ($1, 'Pool affinity issue', 'backlog', 'none', 'member', $2, 0, 1, 'agent', $3)
		RETURNING id
	`, fixture.workspaceID, fixture.userID, fixture.agentID).Scan(&fixture.issueID); err != nil {
		t.Fatalf("create affinity Issue: %v", err)
	}
	return fixture
}

func (f *poolAffinityFixture) createRuntime(label string) pgtype.UUID {
	f.t.Helper()
	var id pgtype.UUID
	if err := f.tx.QueryRow(f.ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, capabilities, last_seen_at
		) VALUES ($1, $2, $3, 'local', 'affinity-test', 'online', '', '{}'::jsonb,
			$4, 'private', '{}'::text[], now())
		RETURNING id
	`, f.workspaceID, "pool-affinity-"+label, "Pool Affinity "+label, f.userID).Scan(&id); err != nil {
		f.t.Fatalf("create affinity Runtime: %v", err)
	}
	return id
}

func (f *poolAffinityFixture) randomUUID() pgtype.UUID {
	f.t.Helper()
	var id pgtype.UUID
	if err := f.tx.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
		f.t.Fatalf("create random UUID: %v", err)
	}
	return id
}

func (f *poolAffinityFixture) createIssueSource(runtimeID pgtype.UUID, affinityState string, affinityRuntimeID pgtype.UUID, sessionID, workDir string) pgtype.UUID {
	f.t.Helper()
	var id pgtype.UUID
	if err := f.tx.QueryRow(f.ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, runtime_binding_mode,
			runtime_requirements, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state, session_affinity_runtime_id,
			session_id, work_dir, completed_at
		) VALUES (
			$1, $2, $3, 'completed', 'pool', '{}'::jsonb, $4, $5, $6, $7,
			NULLIF($8, ''), NULLIF($9, ''), now()
		)
		RETURNING id
	`, f.agentID, f.issueID, runtimeID, f.workspaceID, f.userID, affinityState, affinityRuntimeID, sessionID, workDir).Scan(&id); err != nil {
		f.t.Fatalf("create affinity source Task: %v", err)
	}
	return id
}

func (f *poolAffinityFixture) createRemovedIssueSource() pgtype.UUID {
	f.t.Helper()
	var id pgtype.UUID
	if err := f.tx.QueryRow(f.ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, runtime_binding_mode,
			runtime_requirements, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state, session_affinity_runtime_id, wait_reason, completed_at
		) VALUES (
			$1, $2, NULL, 'cancelled', 'pool', '{}'::jsonb, $3, $4,
			'removed', NULL, 'session_runtime_removed', now()
		)
		RETURNING id
	`, f.agentID, f.issueID, f.workspaceID, f.userID).Scan(&id); err != nil {
		f.t.Fatalf("create removed affinity source Task: %v", err)
	}
	return id
}

func assertPoolPlacement(t *testing.T, placement PoolPlacement, state string, runtimeID pgtype.UUID, reason string) {
	t.Helper()
	if placement.State != state || placement.RuntimeID != runtimeID || placement.WaitReason != reason {
		t.Fatalf("placement = %+v, want state=%q runtime=%v reason=%q", placement, state, runtimeID, reason)
	}
}

func TestResolvePoolAffinityExactRerunDeletedRuntimeIsRemoved(t *testing.T) {
	fixture := newPoolAffinityFixture(t)
	// Runtime deletion clears runtime_id on terminal history without rewriting
	// that historical Task's affinity state.
	source := fixture.createIssueSource(pgtype.UUID{}, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "")
	placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{RerunOfTaskID: source})
	if err != nil {
		t.Fatalf("ResolvePoolTaskPlacement: %v", err)
	}
	assertPoolPlacement(t, placement, runtimepool.SessionAffinityRemoved, pgtype.UUID{}, "session_runtime_removed")
}

func TestResolvePoolAffinityExactRetryDeletedRuntimeIsRemoved(t *testing.T) {
	fixture := newPoolAffinityFixture(t)
	missingRuntime := fixture.randomUUID()
	source := fixture.createIssueSource(pgtype.UUID{}, runtimepool.SessionAffinityPinned, missingRuntime, "", "")
	placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{RetryOfTaskID: source})
	if err != nil {
		t.Fatalf("ResolvePoolTaskPlacement: %v", err)
	}
	assertPoolPlacement(t, placement, runtimepool.SessionAffinityRemoved, pgtype.UUID{}, "session_runtime_removed")
}

func TestResolvePoolAffinityExactRetryUnassignedSourceRemainsNone(t *testing.T) {
	fixture := newPoolAffinityFixture(t)
	// A Pool Task may be cancelled while it is still waiting for its first
	// Runtime. That is not deletion history: retry/parent routing must remain
	// free to select a Runtime rather than inventing session_runtime_removed.
	source := fixture.createIssueSource(pgtype.UUID{}, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "")
	for _, test := range []struct {
		name    string
		request PoolPlacementRequest
	}{
		{name: "retry", request: PoolPlacementRequest{RetryOfTaskID: source}},
		{name: "parent", request: PoolPlacementRequest{ParentTaskID: source}},
	} {
		t.Run(test.name, func(t *testing.T) {
			placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, test.request)
			if err != nil {
				t.Fatalf("ResolvePoolTaskPlacement: %v", err)
			}
			assertPoolPlacement(t, placement, runtimepool.SessionAffinityNone, pgtype.UUID{}, "")
		})
	}
}

func TestResolvePoolAffinityExactSourcePrecedence(t *testing.T) {
	fixture := newPoolAffinityFixture(t)
	rerunRuntime := fixture.createRuntime("rerun")
	retryRuntime := fixture.createRuntime("retry")
	parentRuntime := fixture.createRuntime("parent")
	rerun := fixture.createIssueSource(rerunRuntime, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "")
	retry := fixture.createIssueSource(retryRuntime, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "")
	parent := fixture.createIssueSource(parentRuntime, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "")

	tests := []struct {
		name      string
		request   PoolPlacementRequest
		runtimeID pgtype.UUID
	}{
		{
			name: "rerun beats retry parent and legacy fresh",
			request: PoolPlacementRequest{
				RerunOfTaskID: rerun, RetryOfTaskID: retry, ParentTaskID: parent, ForceFreshSession: true,
			},
			runtimeID: rerunRuntime,
		},
		{
			name: "retry beats parent and legacy fresh",
			request: PoolPlacementRequest{
				RetryOfTaskID: retry, ParentTaskID: parent, ForceFreshSession: true,
			},
			runtimeID: retryRuntime,
		},
		{
			name: "parent beats legacy fresh",
			request: PoolPlacementRequest{
				ParentTaskID: parent, ForceFreshSession: true,
			},
			runtimeID: parentRuntime,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, test.request)
			if err != nil {
				t.Fatalf("ResolvePoolTaskPlacement: %v", err)
			}
			assertPoolPlacement(t, placement, runtimepool.SessionAffinityPinned, test.runtimeID, "")
		})
	}
}

func TestResolvePoolAffinityExactSourceIsAuthoritative(t *testing.T) {
	fixture := newPoolAffinityFixture(t)
	runtimeID := fixture.createRuntime("authoritative-retry")
	retry := fixture.createIssueSource(runtimeID, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "")
	_, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{
		RerunOfTaskID: fixture.randomUUID(),
		RetryOfTaskID: retry,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("ResolvePoolTaskPlacement error = %v, want missing exact rerun source", err)
	}
}

func TestResolvePoolAffinityRemovedAndMissingPinnedSource(t *testing.T) {
	fixture := newPoolAffinityFixture(t)
	removed := fixture.createRemovedIssueSource()
	placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{RerunOfTaskID: removed})
	if err != nil {
		t.Fatalf("resolve removed source: %v", err)
	}
	assertPoolPlacement(t, placement, runtimepool.SessionAffinityRemoved, pgtype.UUID{}, "session_runtime_removed")

	missingRuntime := fixture.randomUUID()
	pinned := fixture.createIssueSource(pgtype.UUID{}, runtimepool.SessionAffinityPinned, missingRuntime, "session-pinned", "")
	placement, err = fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{RerunOfTaskID: pinned})
	if err != nil {
		t.Fatalf("resolve deleted pinned source: %v", err)
	}
	assertPoolPlacement(t, placement, runtimepool.SessionAffinityRemoved, pgtype.UUID{}, "session_runtime_removed")
}

func TestResolvePoolAffinityOrdinaryHistoryNeedsSessionPointer(t *testing.T) {
	t.Run("no pointer remains none", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		runtimeID := fixture.createRuntime("ordinary-no-pointer")
		fixture.createIssueSource(runtimeID, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "")
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{
			AgentID: fixture.agentID, IssueID: fixture.issueID,
		})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityNone, pgtype.UUID{}, "")
	})

	t.Run("workdir alone pins", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		runtimeID := fixture.createRuntime("ordinary-workdir")
		fixture.createIssueSource(runtimeID, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "/tmp/pool-affinity")
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{
			AgentID: fixture.agentID, IssueID: fixture.issueID,
		})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityPinned, runtimeID, "")
	})

	t.Run("deleted workdir history is removed", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		fixture.createIssueSource(pgtype.UUID{}, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "/tmp/deleted-affinity")
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{
			AgentID: fixture.agentID, IssueID: fixture.issueID,
		})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityRemoved, pgtype.UUID{}, "session_runtime_removed")
	})

	t.Run("newer workdir history beats older session", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		olderRuntime := fixture.createRuntime("ordinary-old-session")
		newerRuntime := fixture.createRuntime("ordinary-new-workdir")
		older := fixture.createIssueSource(olderRuntime, runtimepool.SessionAffinityNone, pgtype.UUID{}, "older-session", "")
		newer := fixture.createIssueSource(newerRuntime, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "/tmp/newer-workdir")
		if _, err := fixture.tx.Exec(fixture.ctx, `
			UPDATE agent_task_queue
			SET completed_at = CASE id WHEN $1 THEN now() - interval '2 minutes' ELSE now() - interval '1 minute' END
			WHERE id IN ($1, $2)
		`, older, newer); err != nil {
			t.Fatalf("order affinity history: %v", err)
		}
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{
			AgentID: fixture.agentID, IssueID: fixture.issueID,
		})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityPinned, newerRuntime, "")
	})

	t.Run("pinned soft reference without provider pointers remains authoritative", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		missingRuntime := fixture.randomUUID()
		fixture.createIssueSource(pgtype.UUID{}, runtimepool.SessionAffinityPinned, missingRuntime, "", "")
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{
			AgentID: fixture.agentID, IssueID: fixture.issueID,
		})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityRemoved, pgtype.UUID{}, "session_runtime_removed")
	})
}

func (f *poolAffinityFixture) createChatSession(sessionID, workDir string, runtimeID pgtype.UUID) pgtype.UUID {
	f.t.Helper()
	var id pgtype.UUID
	if err := f.tx.QueryRow(f.ctx, `
		INSERT INTO chat_session (
			workspace_id, agent_id, creator_id, title, session_id, work_dir, runtime_id
		) VALUES ($1, $2, $3, 'Pool affinity chat', NULLIF($4, ''), NULLIF($5, ''), $6)
		RETURNING id
	`, f.workspaceID, f.agentID, f.userID, sessionID, workDir, runtimeID).Scan(&id); err != nil {
		f.t.Fatalf("create affinity ChatSession: %v", err)
	}
	return id
}

func (f *poolAffinityFixture) createChatSource(chatSessionID, runtimeID pgtype.UUID, sessionID, workDir string) pgtype.UUID {
	f.t.Helper()
	var id pgtype.UUID
	if err := f.tx.QueryRow(f.ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, runtime_id, status, runtime_binding_mode,
			runtime_requirements, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state, session_id, work_dir, completed_at
		) VALUES (
			$1, $2, $3, 'completed', 'pool', '{}'::jsonb, $4, $5,
			'none', NULLIF($6, ''), NULLIF($7, ''), now()
		)
		RETURNING id
	`, f.agentID, chatSessionID, runtimeID, f.workspaceID, f.userID, sessionID, workDir).Scan(&id); err != nil {
		f.t.Fatalf("create affinity Chat Task: %v", err)
	}
	return id
}

func TestResolvePoolAffinityChatSessionPointersAreAuthoritative(t *testing.T) {
	t.Run("locked ChatSession pointer beats newer Task history", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		sessionRuntime := fixture.createRuntime("chat-session")
		historyRuntime := fixture.createRuntime("chat-history")
		chatID := fixture.createChatSession("chat-pointer", "", sessionRuntime)
		fixture.createChatSource(chatID, historyRuntime, "newer-history", "")
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{ChatSessionID: chatID})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityPinned, sessionRuntime, "")
	})

	t.Run("ChatSession workdir alone pins", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		runtimeID := fixture.createRuntime("chat-workdir")
		chatID := fixture.createChatSession("", "/tmp/chat-workdir", runtimeID)
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{ChatSessionID: chatID})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityPinned, runtimeID, "")
	})

	t.Run("pointer without Runtime is removed and does not fall back", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		historyRuntime := fixture.createRuntime("chat-removed-history")
		chatID := fixture.createChatSession("deleted-chat-pointer", "", pgtype.UUID{})
		fixture.createChatSource(chatID, historyRuntime, "healthy-history", "")
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{ChatSessionID: chatID})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityRemoved, pgtype.UUID{}, "session_runtime_removed")
	})

	t.Run("empty ChatSession falls back to workdir-only history", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		historyRuntime := fixture.createRuntime("chat-fallback-workdir")
		chatID := fixture.createChatSession("", "", pgtype.UUID{})
		fixture.createChatSource(chatID, historyRuntime, "", "/tmp/chat-history")
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{ChatSessionID: chatID})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityPinned, historyRuntime, "")
	})

	t.Run("empty ChatSession uses the newest recoverable history", func(t *testing.T) {
		fixture := newPoolAffinityFixture(t)
		olderRuntime := fixture.createRuntime("chat-old-session")
		newerRuntime := fixture.createRuntime("chat-new-workdir")
		chatID := fixture.createChatSession("", "", pgtype.UUID{})
		older := fixture.createChatSource(chatID, olderRuntime, "older-session", "")
		newer := fixture.createChatSource(chatID, newerRuntime, "", "/tmp/newer-chat-workdir")
		if _, err := fixture.tx.Exec(fixture.ctx, `
			UPDATE agent_task_queue
			SET completed_at = CASE id WHEN $1 THEN now() - interval '2 minutes' ELSE now() - interval '1 minute' END
			WHERE id IN ($1, $2)
		`, older, newer); err != nil {
			t.Fatalf("order Chat affinity history: %v", err)
		}
		placement, err := fixture.service.ResolvePoolTaskPlacement(fixture.ctx, PoolPlacementRequest{ChatSessionID: chatID})
		if err != nil {
			t.Fatalf("ResolvePoolTaskPlacement: %v", err)
		}
		assertPoolPlacement(t, placement, runtimepool.SessionAffinityPinned, newerRuntime, "")
	})
}

func TestTaskAnalyticsContextPoolDeletedRuntimeDoesNotFallBackToAgentMode(t *testing.T) {
	fixture := newPoolAffinityFixture(t)
	taskID := fixture.createIssueSource(pgtype.UUID{}, runtimepool.SessionAffinityNone, pgtype.UUID{}, "", "")
	task, err := fixture.service.Queries.GetAgentTask(fixture.ctx, taskID)
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}

	got := fixture.service.taskAnalyticsContext(fixture.ctx, task)
	if got.WorkspaceID == "" {
		t.Fatal("WorkspaceID is empty, want Agent fallback to remain available")
	}
	if got.RuntimeMode != "" || got.Provider != "" {
		t.Fatalf("RuntimeMode/Provider = %q/%q, want empty physical Runtime analytics", got.RuntimeMode, got.Provider)
	}
}
