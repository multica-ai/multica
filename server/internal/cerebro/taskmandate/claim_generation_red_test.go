package taskmandate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errClaimGenerationAuthorizationUnavailable = errors.New("task mandate claim generation authorization unavailable")

const (
	missingClaimGenerationReason = "task mandate missing"
	expiredClaimGenerationReason = "task mandate expired"
	identityMismatchReason       = "task mandate identity mismatch"
	staleClaimGenerationReason   = "task mandate stale claim generation"
)

type claimGenerationFixture struct {
	pool        *pgxpool.Pool
	workspaceID pgtype.UUID
	agentID     pgtype.UUID
	otherAgent  pgtype.UUID
	taskID      pgtype.UUID
}

func newClaimGenerationFixture(t *testing.T) claimGenerationFixture {
	t.Helper()
	pool := openMandateTestPool(t)
	ctx := context.Background()
	var fixture claimGenerationFixture
	fixture.pool = pool
	var runtimeID, issueID pgtype.UUID

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('Task Mandate Generation', 'task-mandate-generation-' || gen_random_uuid())
		RETURNING id`).Scan(&fixture.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, fixture.workspaceID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		VALUES ($1, 'Task Mandate Generation Runtime', 'local', 'codex')
		RETURNING id`, fixture.workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'Task Mandate Generation Agent', 'local', $2)
		RETURNING id`, fixture.workspaceID, runtimeID).Scan(&fixture.agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'Task Mandate Other Agent', 'local', $2)
		RETURNING id`, fixture.workspaceID, runtimeID).Scan(&fixture.otherAgent); err != nil {
		t.Fatalf("create other agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'Task Mandate Generation', 'agent', $2)
		RETURNING id`, fixture.workspaceID, fixture.agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id)
		VALUES ($1, $2, $3)
		RETURNING id`, fixture.agentID, issueID, runtimeID).Scan(&fixture.taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return fixture
}

func requireClaimGenerationDenialReason(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Authorize returned nil, want denial reason %q", want)
	}
	if err.Error() != want {
		t.Fatalf("Authorize denial reason = %q, want %q", err.Error(), want)
	}
}

type claimGenerationAuthorizer interface {
	AuthorizeClaimGeneration(
		context.Context,
		pgtype.UUID,
		pgtype.UUID,
		pgtype.UUID,
		int64,
		string,
	) error
}

func authorizeClaimGeneration(
	store *Store,
	ctx context.Context,
	taskID, workspaceID, agentID pgtype.UUID,
	generation int64,
	tool string,
) error {
	authorizer, ok := any(store).(claimGenerationAuthorizer)
	if !ok {
		return errClaimGenerationAuthorizationUnavailable
	}
	return authorizer.AuthorizeClaimGeneration(ctx, taskID, workspaceID, agentID, generation, tool)
}

func TestAuthorizeMissingClaimGenerationHasDistinctReason(t *testing.T) {
	fixture := newClaimGenerationFixture(t)

	err := NewStore(fixture.pool).Authorize(
		context.Background(), fixture.taskID, fixture.workspaceID, fixture.agentID, "tools:Read",
	)
	requireClaimGenerationDenialReason(t, err, missingClaimGenerationReason)
}

func TestAuthorizeExpiredClaimGenerationHasDistinctReason(t *testing.T) {
	fixture := newClaimGenerationFixture(t)
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO cerebro_task_mandate (
			task_id, workspace_id, agent_id, allowed_tools, issued_at, expires_at
		) VALUES ($1, $2, $3, '["tools:Read"]'::jsonb, $4, $5)`,
		fixture.taskID,
		fixture.workspaceID,
		fixture.agentID,
		time.Now().Add(-2*time.Hour),
		time.Now().Add(-time.Hour),
	); err != nil {
		t.Fatalf("insert expired claim generation: %v", err)
	}

	err := NewStore(fixture.pool).Authorize(
		ctx, fixture.taskID, fixture.workspaceID, fixture.agentID, "tools:Read",
	)
	requireClaimGenerationDenialReason(t, err, expiredClaimGenerationReason)
}

func TestAuthorizeIdentityMismatchedClaimGenerationHasDistinctReason(t *testing.T) {
	fixture := newClaimGenerationFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	if err := store.Issue(
		ctx, fixture.taskID, fixture.workspaceID, fixture.agentID, []string{"tools:Read"}, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("issue claim generation: %v", err)
	}

	err := store.Authorize(ctx, fixture.taskID, fixture.workspaceID, fixture.otherAgent, "tools:Read")
	requireClaimGenerationDenialReason(t, err, identityMismatchReason)
}

func TestAuthorizeStaleClaimGenerationHasDistinctReason(t *testing.T) {
	fixture := newClaimGenerationFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	if err := store.Issue(
		ctx, fixture.taskID, fixture.workspaceID, fixture.agentID, []string{"tools:Read"}, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("issue first claim generation: %v", err)
	}
	firstGeneration := int64(1)
	if err := store.Issue(
		ctx, fixture.taskID, fixture.workspaceID, fixture.agentID, []string{"tools:Write"}, time.Now().Add(2*time.Hour),
	); err != nil {
		t.Fatalf("issue replacement claim generation: %v", err)
	}

	// The caller still holds the first claim and must not be evaluated against
	// the replacement row without carrying that generation into authorization.
	err := authorizeClaimGeneration(
		store, ctx, fixture.taskID, fixture.workspaceID, fixture.agentID, firstGeneration, "tools:Read",
	)
	requireClaimGenerationDenialReason(t, err, staleClaimGenerationReason)
}

func TestClaimGenerationStorageShapeIsAdditive(t *testing.T) {
	fixture := newClaimGenerationFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	if err := store.Issue(
		ctx, fixture.taskID, fixture.workspaceID, fixture.agentID, []string{"tools:Read"}, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("issue legacy claim generation: %v", err)
	}

	var (
		generation                         int64
		producer, finalizer                *string
		inventoryVersion, discoveryVersion *string
		finalizedGrantDigest               *string
		lifecycleState                     string
	)
	err := fixture.pool.QueryRow(ctx, `
		SELECT claim_generation, producer, finalizer, lifecycle_state,
		       inventory_version, discovery_version, finalized_grant_digest
		FROM cerebro_task_mandate
		WHERE task_id = $1`, fixture.taskID,
	).Scan(
		&generation,
		&producer,
		&finalizer,
		&lifecycleState,
		&inventoryVersion,
		&discoveryVersion,
		&finalizedGrantDigest,
	)
	if err != nil {
		t.Fatalf("read additive claim generation storage: %v", err)
	}
	if generation != 1 || ClaimLifecycleState(lifecycleState) != ClaimLifecycleLegacy {
		t.Fatalf("legacy claim generation = (%d, %q), want (1, %q)", generation, lifecycleState, ClaimLifecycleLegacy)
	}
	if producer != nil || finalizer != nil || inventoryVersion != nil || discoveryVersion != nil || finalizedGrantDigest != nil {
		t.Fatalf(
			"legacy metadata = producer:%v finalizer:%v inventory:%v discovery:%v digest:%v, want nil",
			producer, finalizer, inventoryVersion, discoveryVersion, finalizedGrantDigest,
		)
	}
}
