package migrations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
)

func TestRuntimePoolContractMigrationSeparatesConcurrentIndexes(t *testing.T) {
	dir := realMigrationsDir(t)
	contractRaw, err := os.ReadFile(filepath.Join(dir, "267_runtime_pool_contract.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	contract := strings.ToUpper(string(contractRaw))
	for _, forbidden := range []string{"CREATE INDEX", "CREATE UNIQUE INDEX", "PRIMARY KEY", " UNIQUE"} {
		if strings.Contains(contract, forbidden) {
			t.Fatalf("267 contract migration contains forbidden inline index/key syntax %q", forbidden)
		}
	}
	if strings.Contains(contract, "FOREIGN KEY") || strings.Contains(contract, "REFERENCES") || strings.Contains(contract, "CASCADE") {
		t.Fatal("267 contract migration must not add foreign keys or cascade actions")
	}

	files := []struct {
		name string
		want string
	}{
		{"268_comment_followup_id_unique.up.sql", "CREATE UNIQUE INDEX CONCURRENTLY IDX_AGENT_COMMENT_FOLLOWUP_OBLIGATION_ID"},
		{"268_comment_followup_id_unique.down.sql", "DROP INDEX CONCURRENTLY IF EXISTS IDX_AGENT_COMMENT_FOLLOWUP_OBLIGATION_ID"},
		{"269_comment_followup_primary_key.up.sql", "PRIMARY KEY USING INDEX IDX_AGENT_COMMENT_FOLLOWUP_OBLIGATION_ID"},
		{"269_comment_followup_primary_key.down.sql", "DROP CONSTRAINT AGENT_COMMENT_FOLLOWUP_OBLIGATION_PKEY"},
		{"270_comment_followup_agent_comment_unique.up.sql", "CREATE UNIQUE INDEX CONCURRENTLY IDX_AGENT_COMMENT_FOLLOWUP_OBLIGATION_AGENT_COMMENT"},
		{"270_comment_followup_agent_comment_unique.down.sql", "DROP INDEX CONCURRENTLY IDX_AGENT_COMMENT_FOLLOWUP_OBLIGATION_AGENT_COMMENT"},
		{"271_comment_followup_fifo_index.up.sql", "CREATE INDEX CONCURRENTLY IDX_AGENT_COMMENT_FOLLOWUP_OBLIGATION_FIFO"},
		{"271_comment_followup_fifo_index.down.sql", "DROP INDEX CONCURRENTLY IDX_AGENT_COMMENT_FOLLOWUP_OBLIGATION_FIFO"},
	}
	for _, file := range files {
		t.Run(file.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, file.name))
			if err != nil {
				t.Fatal(err)
			}
			statements := sqlStatementsForLint(string(raw))
			if len(statements) != 1 {
				t.Fatalf("%s has %d top-level statements, want 1", file.name, len(statements))
			}
			if !strings.Contains(strings.ToUpper(statements[0]), file.want) {
				t.Fatalf("%s statement = %q, want contract %q", file.name, statements[0], file.want)
			}
		})
	}
}

func TestRuntimePoolContractTruthTable(t *testing.T) {
	pool := pooltestdb.Open(t)

	t.Run("fixed_lifecycle", func(t *testing.T) {
		fixture := newRuntimePoolContractFixture(t, pool)
		assertTaskAllowed(t, pool, fixture, taskShape{status: "queued", runtimeID: fixture.runtimeID})
		assertTaskAllowed(t, pool, fixture, taskShape{status: "completed", completed: true})
		assertTaskRejectedBy(t, pool, fixture, taskShape{status: "queued"}, "agent_task_queue_routing_lifecycle_check")
		assertTaskRejectedBy(t, pool, fixture, taskShape{status: "waiting_runtime"}, "agent_task_queue_routing_lifecycle_check")
		assertTaskRejectedBy(t, pool, fixture, taskShape{
			status: "queued", runtimeID: fixture.runtimeID,
			placementWorkspaceID: fixture.workspaceID, requesterUserID: fixture.userID,
		}, "agent_task_queue_fixed_snapshot_check")
		assertTaskRejectedBy(t, pool, fixture, taskShape{status: "completed"}, "agent_task_queue_terminal_completed_at_check")
		assertTaskRejectedBy(t, pool, fixture, taskShape{status: "queued", runtimeID: fixture.runtimeID, completed: true}, "agent_task_queue_terminal_completed_at_check")
		assertTaskRejectedBy(t, pool, fixture, taskShape{status: "queued", bindingMode: "future", runtimeID: fixture.runtimeID}, "agent_task_queue_routing_lifecycle_check")
		assertAgentRejectedBy(t, pool, fixture, "fixed", "pool", fixture.runtimeID, "agent_runtime_binding_mode_check")
		assertAgentRejectedBy(t, pool, fixture, "future", "local", fixture.runtimeID, "agent_runtime_binding_mode_check")
	})

	t.Run("pool_lifecycle", func(t *testing.T) {
		fixture := newRuntimePoolContractFixture(t, pool)
		fixture.ensurePoolAgent(t, pool)
		for _, status := range []string{"waiting_runtime", "deferred"} {
			assertTaskAllowed(t, pool, fixture, fixture.poolTask(status, ""))
		}
		for _, status := range []string{"queued", "dispatched", "running", "waiting_local_directory"} {
			assertTaskAllowed(t, pool, fixture, fixture.poolTask(status, fixture.runtimeID))
		}
		assertTaskAllowed(t, pool, fixture, taskShape{
			status: "cancelled", bindingMode: "pool", completed: true,
			placementWorkspaceID: fixture.workspaceID, requesterUserID: fixture.userID,
		})
		assertTaskRejectedBy(t, pool, fixture, fixture.poolTask("waiting_runtime", fixture.runtimeID), "agent_task_queue_routing_lifecycle_check")
		assertTaskRejectedBy(t, pool, fixture, fixture.poolTask("queued", ""), "agent_task_queue_routing_lifecycle_check")
		missingPlacement := fixture.poolTask("waiting_runtime", "")
		missingPlacement.placementWorkspaceID = ""
		assertTaskRejectedBy(t, pool, fixture, missingPlacement, "agent_task_queue_routing_lifecycle_check")
		missingRequester := fixture.poolTask("waiting_runtime", "")
		missingRequester.requesterUserID = ""
		assertTaskRejectedBy(t, pool, fixture, missingRequester, "agent_task_queue_routing_lifecycle_check")
		assertAgentRejectedBy(t, pool, fixture, "pool", "pool", fixture.runtimeID, "agent_runtime_binding_mode_check")
		assertAgentRejectedBy(t, pool, fixture, "pool", "local", "", "agent_runtime_binding_mode_check")
	})

	t.Run("affinity_pairs", func(t *testing.T) {
		fixture := newRuntimePoolContractFixture(t, pool)
		fixture.ensurePoolAgent(t, pool)
		fixture.ensureChatSession(t, pool)
		assertTaskAllowed(t, pool, fixture, taskShape{
			status: "waiting_runtime", bindingMode: "pool", affinityState: "unresolved",
			placementWorkspaceID: fixture.workspaceID, requesterUserID: fixture.userID,
			chatSessionID: fixture.chatSessionID, waitReason: "chat_predecessor_pending",
		})
		assertTaskAllowed(t, pool, fixture, fixture.poolTask("waiting_runtime", ""))
		pinned := fixture.poolTask("waiting_runtime", "")
		pinned.affinityState = "pinned"
		pinned.affinityRuntimeID = fixture.runtimeID
		assertTaskAllowed(t, pool, fixture, pinned)
		assertTaskAllowed(t, pool, fixture, fixture.removedTask())

		unresolvedWithRuntime := fixture.poolTask("waiting_runtime", "")
		unresolvedWithRuntime.affinityState = "unresolved"
		unresolvedWithRuntime.affinityRuntimeID = fixture.runtimeID
		unresolvedWithRuntime.chatSessionID = fixture.chatSessionID
		unresolvedWithRuntime.waitReason = "chat_predecessor_pending"
		assertTaskRejectedBy(t, pool, fixture, unresolvedWithRuntime, "agent_task_queue_affinity_pair_check")
		noneWithRuntime := fixture.poolTask("waiting_runtime", "")
		noneWithRuntime.affinityRuntimeID = fixture.runtimeID
		assertTaskRejectedBy(t, pool, fixture, noneWithRuntime, "agent_task_queue_affinity_pair_check")
		assertTaskRejectedBy(t, pool, fixture, taskShape{
			status: "waiting_runtime", bindingMode: "pool", affinityState: "pinned",
			placementWorkspaceID: fixture.workspaceID, requesterUserID: fixture.userID,
		}, "agent_task_queue_affinity_pair_check")
		removedWithRuntime := fixture.removedTask()
		removedWithRuntime.affinityRuntimeID = fixture.runtimeID
		assertTaskRejectedBy(t, pool, fixture, removedWithRuntime, "agent_task_queue_affinity_pair_check")
	})

	t.Run("unresolved_chat_only", func(t *testing.T) {
		fixture := newRuntimePoolContractFixture(t, pool)
		fixture.ensurePoolAgent(t, pool)
		fixture.ensureChatSession(t, pool)
		valid := taskShape{
			status: "deferred", bindingMode: "pool", affinityState: "unresolved",
			placementWorkspaceID: fixture.workspaceID, requesterUserID: fixture.userID,
			chatSessionID: fixture.chatSessionID, waitReason: "chat_predecessor_pending",
		}
		assertTaskAllowed(t, pool, fixture, valid)
		noChat := valid
		noChat.chatSessionID = ""
		assertTaskRejectedBy(t, pool, fixture, noChat, "agent_task_queue_unresolved_check")
		queued := valid
		queued.status = "queued"
		queued.runtimeID = fixture.runtimeID
		assertTaskRejectedBy(t, pool, fixture, queued, "agent_task_queue_unresolved_check")
		wrongReason := valid
		wrongReason.waitReason = "no_eligible_runtime"
		assertTaskRejectedBy(t, pool, fixture, wrongReason, "agent_task_queue_unresolved_check")
		missingReason := valid
		missingReason.waitReason = ""
		assertTaskRejectedBy(t, pool, fixture, missingReason, "agent_task_queue_unresolved_check")
	})

	t.Run("removed_cancellation", func(t *testing.T) {
		fixture := newRuntimePoolContractFixture(t, pool)
		fixture.ensurePoolAgent(t, pool)
		assertTaskAllowed(t, pool, fixture, fixture.removedTask())
		wrongStatus := fixture.removedTask()
		wrongStatus.status = "completed"
		assertTaskRejectedBy(t, pool, fixture, wrongStatus, "agent_task_queue_removed_check")
		missingCompleted := fixture.removedTask()
		missingCompleted.completed = false
		assertTaskRejected(t, pool, fixture, missingCompleted)
		withRuntime := fixture.removedTask()
		withRuntime.runtimeID = fixture.runtimeID
		assertTaskRejectedBy(t, pool, fixture, withRuntime, "agent_task_queue_removed_check")
		wrongReason := fixture.removedTask()
		wrongReason.waitReason = "no_eligible_runtime"
		assertTaskRejectedBy(t, pool, fixture, wrongReason, "agent_task_queue_removed_check")
		missingReason := fixture.removedTask()
		missingReason.waitReason = ""
		assertTaskRejectedBy(t, pool, fixture, missingReason, "agent_task_queue_removed_check")
	})

	t.Run("explicit_fresh", func(t *testing.T) {
		fixture := newRuntimePoolContractFixture(t, pool)
		fixture.ensurePoolAgent(t, pool)
		fresh := fixture.poolTask("waiting_runtime", "")
		fresh.explicitFresh = true
		assertTaskAllowed(t, pool, fixture, fresh)
		pinnedFresh := fresh
		pinnedFresh.affinityState = "pinned"
		pinnedFresh.affinityRuntimeID = fixture.runtimeID
		assertTaskRejectedBy(t, pool, fixture, pinnedFresh, "agent_task_queue_explicit_fresh_check")
		assertTaskRejectedBy(t, pool, fixture, taskShape{
			status: "queued", runtimeID: fixture.runtimeID, explicitFresh: true,
		}, "agent_task_queue_explicit_fresh_check")
	})

	t.Run("release_shapes", func(t *testing.T) {
		fixture := newRuntimePoolContractFixture(t, pool)
		assertReleaseAllowed(t, pool, fixture, "fixed", "", "")
		assertReleaseAllowed(t, pool, fixture, "fixed", fixture.squadID, fixture.runtimeID)
		assertReleaseAllowed(t, pool, fixture, "pool", fixture.squadID, "")
		assertReleaseRejectedBy(t, pool, fixture, "fixed", fixture.squadID, "", "platform_extension_release_runtime_routing_check")
		assertReleaseRejectedBy(t, pool, fixture, "pool", fixture.squadID, fixture.runtimeID, "platform_extension_release_runtime_routing_check")
		assertReleaseRejectedBy(t, pool, fixture, "future", "", "", "platform_extension_release_runtime_binding_mode_check")
	})
}

type runtimePoolContractFixture struct {
	tx            pgx.Tx
	userID        string
	workspaceID   string
	runtimeID     string
	fixedAgentID  string
	poolAgentID   string
	issueID       string
	chatSessionID string
	squadID       string
	seed          int64
}

func newRuntimePoolContractFixture(t *testing.T, pool *pgxpool.Pool) *runtimePoolContractFixture {
	t.Helper()
	ctx := context.Background()
	seed := time.Now().UnixNano()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin isolated contract fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback isolated contract fixture: %v", err)
		}
	})
	fixture := &runtimePoolContractFixture{tx: tx, seed: seed}
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Runtime Pool Contract', $1) RETURNING id
	`, fmt.Sprintf("runtime-pool-contract-%d@example.test", seed)).Scan(&fixture.userID); err != nil {
		t.Fatalf("create contract user: %v", err)
	}
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Runtime Pool Contract', $1, '', 'RPC') RETURNING id
	`, fmt.Sprintf("runtime-pool-contract-%d", seed)).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("create contract workspace: %v", err)
	}
	if _, err := fixture.tx.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("create contract member: %v", err)
	}
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, visibility
		) VALUES ($1, $2, 'local', 'contract-provider', 'online', '', '{}'::jsonb, $3, 'private')
		RETURNING id
	`, fixture.workspaceID, fmt.Sprintf("Contract Runtime %d", seed), fixture.userID).Scan(&fixture.runtimeID); err != nil {
		t.Fatalf("create contract runtime: %v", err)
	}
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args
		) VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, fixture.workspaceID, fmt.Sprintf("Contract Fixed Agent %d", seed), fixture.runtimeID, fixture.userID).Scan(&fixture.fixedAgentID); err != nil {
		t.Fatalf("create fixed contract agent: %v", err)
	}
	if err := fixture.tx.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		VALUES ($1, $2, 'backlog', 'none', 'member', $3, 0, 1) RETURNING id
	`, fixture.workspaceID, fmt.Sprintf("Runtime Pool Contract %d", seed), fixture.userID).Scan(&fixture.issueID); err != nil {
		t.Fatalf("create contract issue: %v", err)
	}
	if err := fixture.tx.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&fixture.squadID); err != nil {
		t.Fatalf("create contract squad id: %v", err)
	}
	return fixture
}

func (f *runtimePoolContractFixture) ensurePoolAgent(t *testing.T, _ *pgxpool.Pool) {
	t.Helper()
	if f.poolAgentID != "" {
		return
	}
	if err := f.tx.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id, instructions,
			custom_env, custom_args, runtime_binding_mode, runtime_requirements
		) VALUES ($1, $2, '', 'pool', '{}'::jsonb, NULL, 'private', 'private', 1, $3,
			'', '{}'::jsonb, '[]'::jsonb, 'pool',
			'{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1"]}'::jsonb)
		RETURNING id
	`, f.workspaceID, fmt.Sprintf("Contract Pool Agent %d", f.seed), f.userID).Scan(&f.poolAgentID); err != nil {
		t.Fatalf("create pool contract agent: %v", err)
	}
}

func (f *runtimePoolContractFixture) ensureChatSession(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if f.chatSessionID != "" {
		return
	}
	f.ensurePoolAgent(t, pool)
	if err := f.tx.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id) VALUES ($1, $2, $3) RETURNING id
	`, f.workspaceID, f.poolAgentID, f.userID).Scan(&f.chatSessionID); err != nil {
		t.Fatalf("create contract chat session: %v", err)
	}
}

func (f *runtimePoolContractFixture) poolTask(status, runtimeID string) taskShape {
	return taskShape{
		status: status, bindingMode: "pool", runtimeID: runtimeID,
		placementWorkspaceID: f.workspaceID, requesterUserID: f.userID,
	}
}

func (f *runtimePoolContractFixture) removedTask() taskShape {
	return taskShape{
		status: "cancelled", bindingMode: "pool", affinityState: "removed", completed: true,
		placementWorkspaceID: f.workspaceID, requesterUserID: f.userID,
		waitReason: "session_runtime_removed",
	}
}

type taskShape struct {
	status               string
	bindingMode          string
	runtimeID            string
	placementWorkspaceID string
	requesterUserID      string
	affinityState        string
	affinityRuntimeID    string
	explicitFresh        bool
	chatSessionID        string
	waitReason           string
	completed            bool
}

func assertTaskAllowed(t *testing.T, pool *pgxpool.Pool, fixture *runtimePoolContractFixture, shape taskShape) {
	t.Helper()
	_, err := insertContractTask(t, pool, fixture, shape)
	if err != nil {
		t.Fatalf("insert valid task shape %#v: %v", shape, err)
	}
}

func assertTaskRejected(t *testing.T, pool *pgxpool.Pool, fixture *runtimePoolContractFixture, shape taskShape) {
	t.Helper()
	_, err := insertContractTask(t, pool, fixture, shape)
	if !isCheckViolation(err) {
		t.Fatalf("insert invalid task shape %#v: got %v, want SQLSTATE 23514", shape, err)
	}
}

func assertTaskRejectedBy(t *testing.T, pool *pgxpool.Pool, fixture *runtimePoolContractFixture, shape taskShape, constraint string) {
	t.Helper()
	_, err := insertContractTask(t, pool, fixture, shape)
	assertCheckViolationConstraint(t, err, constraint)
}

func insertContractTask(t *testing.T, pool *pgxpool.Pool, fixture *runtimePoolContractFixture, shape taskShape) (string, error) {
	t.Helper()
	if shape.bindingMode == "" {
		shape.bindingMode = "fixed"
	}
	if shape.affinityState == "" {
		shape.affinityState = "none"
	}
	agentID := fixture.fixedAgentID
	if shape.bindingMode == "pool" {
		fixture.ensurePoolAgent(t, pool)
		agentID = fixture.poolAgentID
	}
	return withContractSavepoint(t, fixture, func(tx pgx.Tx) (string, error) {
		var id string
		err := tx.QueryRow(context.Background(), `
			INSERT INTO agent_task_queue (
				agent_id, issue_id, status, runtime_id, runtime_binding_mode,
				placement_workspace_id, runtime_requester_user_id, session_affinity_state,
				session_affinity_runtime_id, explicit_fresh_session, chat_session_id,
				wait_reason, completed_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
				CASE WHEN $13::boolean THEN now() ELSE NULL END)
			RETURNING id
		`, agentID, fixture.issueID, shape.status, nullString(shape.runtimeID), shape.bindingMode,
			nullString(shape.placementWorkspaceID), nullString(shape.requesterUserID), shape.affinityState,
			nullString(shape.affinityRuntimeID), shape.explicitFresh, nullString(shape.chatSessionID),
			nullString(shape.waitReason), shape.completed).Scan(&id)
		return id, err
	})
}

func assertAgentRejectedBy(t *testing.T, _ *pgxpool.Pool, fixture *runtimePoolContractFixture, bindingMode, runtimeMode, runtimeID, constraint string) {
	t.Helper()
	_, err := withContractSavepoint(t, fixture, func(tx pgx.Tx) (pgconn.CommandTag, error) {
		return tx.Exec(context.Background(), `
			INSERT INTO agent (
				workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
				visibility, permission_mode, max_concurrent_tasks, owner_id, instructions,
				custom_env, custom_args, runtime_binding_mode, runtime_requirements
			) VALUES ($1, $2, '', $3, '{}'::jsonb, $4, 'private', 'private', 1, $5,
				'', '{}'::jsonb, '[]'::jsonb, $6, '{}'::jsonb)
		`, fixture.workspaceID, fmt.Sprintf("Rejected Contract Agent %d %s %s", time.Now().UnixNano(), bindingMode, runtimeMode),
			runtimeMode, nullString(runtimeID), fixture.userID, bindingMode)
	})
	assertCheckViolationConstraint(t, err, constraint)
}

func assertReleaseAllowed(t *testing.T, pool *pgxpool.Pool, fixture *runtimePoolContractFixture, bindingMode, squadID, runtimeID string) {
	t.Helper()
	_, err := insertContractRelease(t, pool, fixture, bindingMode, squadID, runtimeID)
	if err != nil {
		t.Fatalf("insert valid release %s/%s/%s: %v", bindingMode, squadID, runtimeID, err)
	}
}

func assertReleaseRejectedBy(t *testing.T, pool *pgxpool.Pool, fixture *runtimePoolContractFixture, bindingMode, squadID, runtimeID, constraint string) {
	t.Helper()
	_, err := insertContractRelease(t, pool, fixture, bindingMode, squadID, runtimeID)
	assertCheckViolationConstraint(t, err, constraint)
}

func insertContractRelease(t *testing.T, _ *pgxpool.Pool, fixture *runtimePoolContractFixture, bindingMode, squadID, runtimeID string) (string, error) {
	t.Helper()
	seed := time.Now().UnixNano()
	return withContractSavepoint(t, fixture, func(tx pgx.Tx) (string, error) {
		var id string
		err := tx.QueryRow(context.Background(), `
			INSERT INTO platform_extension_release (
				workspace_id, extension_key, name, version, digest, manifest, runtime_id,
				squad_id, resources, created_by, runtime_binding_mode, runtime_requirements
			) VALUES ($1, $2, 'Contract Extension', $3, 'digest', '{}'::jsonb, $4, $5,
				'{}'::jsonb, $6, $7, '{}'::jsonb)
			RETURNING id
		`, fixture.workspaceID, fmt.Sprintf("contract.extension.%d", seed), fmt.Sprintf("1.0.%d", seed),
			nullString(runtimeID), nullString(squadID), fixture.userID, bindingMode).Scan(&id)
		return id, err
	})
}

func withContractSavepoint[T any](t *testing.T, fixture *runtimePoolContractFixture, action func(pgx.Tx) (T, error)) (T, error) {
	t.Helper()
	ctx := context.Background()
	savepoint, err := fixture.tx.Begin(ctx)
	if err != nil {
		t.Fatalf("begin isolated contract assertion: %v", err)
	}
	value, actionErr := action(savepoint)
	if err := savepoint.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("rollback isolated contract assertion: %v", err)
	}
	return value, actionErr
}

func assertCheckViolationConstraint(t *testing.T, err error, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("got %v, want SQLSTATE 23514 from %s", err, constraint)
	}
	if pgErr.ConstraintName != constraint {
		t.Fatalf("check violation constraint = %q, want %q", pgErr.ConstraintName, constraint)
	}
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
