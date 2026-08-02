package taskmandate

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
	changedGrantReason           = "task mandate finalized grants changed"
	staleWriterReason            = "task mandate stale finalization writer"
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

func TestFinalizeClaimCreatesOneImmutableGeneration(t *testing.T) {
	fixture := newClaimGenerationFixture(t)
	ctx := context.Background()
	input, err := newContractInput(
		fixture.taskID,
		fixture.workspaceID,
		fixture.agentID,
		[]string{"tools:Write", "tools:Read"},
		[]string{"connection:registry"},
		"inventory:v7/discovery:v3",
	)
	if err != nil {
		t.Fatalf("newContractInput: %v", err)
	}

	expiresAt := time.Now().Add(time.Hour)
	first, err := NewStore(fixture.pool).FinalizeClaim(
		ctx, input, "local-runtime", "task-claim", expiresAt,
	)
	if err != nil {
		t.Fatalf("FinalizeClaim: %v", err)
	}
	second, err := NewStore(fixture.pool).FinalizeClaim(
		ctx, input, "local-runtime", "task-claim", expiresAt,
	)
	if err != nil {
		t.Fatalf("idempotent FinalizeClaim: %v", err)
	}
	if first.Generation != 1 || second.Generation != 1 {
		t.Fatalf("finalized generations = (%d, %d), want one immutable generation 1", first.Generation, second.Generation)
	}
	if first.FinalizedGrantDigest == nil || second.FinalizedGrantDigest == nil || *first.FinalizedGrantDigest != *second.FinalizedGrantDigest {
		t.Fatalf("finalized grant digests = (%v, %v), want one stable digest", first.FinalizedGrantDigest, second.FinalizedGrantDigest)
	}

	var (
		generation                                       int64
		producer, finalizer, lifecycle, inventoryVersion string
		discoveryVersion                                 *string
		grantDigest                                      string
		rawTools                                         []byte
		rowCount                                         int
	)
	if err := fixture.pool.QueryRow(ctx, `
		SELECT claim_generation, producer, finalizer, lifecycle_state,
		       inventory_version, discovery_version, finalized_grant_digest,
		       allowed_tools
		FROM cerebro_task_mandate
		WHERE task_id = $1`, fixture.taskID,
	).Scan(
		&generation,
		&producer,
		&finalizer,
		&lifecycle,
		&inventoryVersion,
		&discoveryVersion,
		&grantDigest,
		&rawTools,
	); err != nil {
		t.Fatalf("read finalized claim generation: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM cerebro_task_mandate WHERE task_id = $1`, fixture.taskID).Scan(&rowCount); err != nil {
		t.Fatalf("count finalized claim generations: %v", err)
	}
	var tools []string
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		t.Fatalf("decode finalized grants: %v", err)
	}
	if generation != 1 || rowCount != 1 || ClaimLifecycleState(lifecycle) != ClaimLifecycleFinalized {
		t.Fatalf("finalized generation = (%d, %d rows, %q), want (1, 1 row, %q)", generation, rowCount, lifecycle, ClaimLifecycleFinalized)
	}
	if producer != "local-runtime" || finalizer != "task-claim" || inventoryVersion != input.SourceVersion() || discoveryVersion != nil {
		t.Fatalf(
			"finalized metadata = producer:%q finalizer:%q inventory:%q discovery:%v",
			producer, finalizer, inventoryVersion, discoveryVersion,
		)
	}
	if grantDigest != *first.FinalizedGrantDigest || !reflect.DeepEqual(tools, []string{"tools:Read", "tools:Write"}) {
		t.Fatalf("finalized grants = digest:%q tools:%v, want stable digest and normalized exact tools", grantDigest, tools)
	}
}

func TestFinalizeClaimPersistsEmptyGrantSetAsArray(t *testing.T) {
	fixture := newClaimGenerationFixture(t)
	ctx := context.Background()
	input, err := newContractInput(
		fixture.taskID,
		fixture.workspaceID,
		fixture.agentID,
		nil,
		nil,
		"inventory:v7",
	)
	if err != nil {
		t.Fatalf("newContractInput: %v", err)
	}

	if _, err := NewStore(fixture.pool).FinalizeClaim(
		ctx, input, "local-runtime", "task-claim", time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("FinalizeClaim with empty grants: %v", err)
	}

	var rawTools []byte
	if err := fixture.pool.QueryRow(ctx, `
		SELECT allowed_tools
		FROM cerebro_task_mandate
		WHERE task_id = $1`, fixture.taskID,
	).Scan(&rawTools); err != nil {
		t.Fatalf("read empty finalized grants: %v", err)
	}
	if string(rawTools) != "[]" {
		t.Fatalf("empty finalized grants = %s, want []", rawTools)
	}
}

func TestContractGrantDigestIncludesEveryCanonicalGrantInput(t *testing.T) {
	t.Parallel()
	input, err := newContractInput(
		contractInputUUID(1),
		contractInputUUID(2),
		contractInputUUID(3),
		[]string{"create_workflow_hook"},
		[]string{"connection:registry"},
		"inventory:v7",
	)
	if err != nil {
		t.Fatalf("newContractInput: %v", err)
	}

	got, err := contractGrantDigest(input)
	if err != nil {
		t.Fatalf("contractGrantDigest: %v", err)
	}
	const want = "sha256:133efadd12513eee0ea96ac834197a236172d8ea1d944634774aa5eeb090ad8b"
	if got != want {
		t.Fatalf("contractGrantDigest = %q, want canonical callable, platform operation, and Connection scope digest %q", got, want)
	}
}

func TestFinalizeClaimConcurrentWritersCreateExactlyOneGeneration(t *testing.T) {
	fixture := newClaimGenerationFixture(t)
	ctx := context.Background()
	expiresAt := time.Now().Add(time.Hour)
	winnerInput, err := newContractInput(
		fixture.taskID,
		fixture.workspaceID,
		fixture.agentID,
		[]string{"tools:Read"},
		nil,
		"inventory:v7",
	)
	if err != nil {
		t.Fatalf("winner newContractInput: %v", err)
	}
	loserInput, err := newContractInput(
		fixture.taskID,
		fixture.workspaceID,
		fixture.agentID,
		[]string{"tools:Write"},
		nil,
		"inventory:v7",
	)
	if err != nil {
		t.Fatalf("loser newContractInput: %v", err)
	}

	winnerTx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin winner transaction: %v", err)
	}
	t.Cleanup(func() { _ = winnerTx.Rollback(context.Background()) })
	winner, err := NewStoreDB(winnerTx).FinalizeClaim(
		ctx, winnerInput, "local-runtime-a", "task-claim", expiresAt,
	)
	if err != nil {
		t.Fatalf("winner FinalizeClaim: %v", err)
	}

	loserStarted := make(chan struct{})
	loserResult := make(chan error, 1)
	go func() {
		close(loserStarted)
		_, err := NewStore(fixture.pool).FinalizeClaim(
			ctx, loserInput, "local-runtime-b", "task-claim", expiresAt,
		)
		loserResult <- err
	}()
	<-loserStarted
	select {
	case err := <-loserResult:
		t.Fatalf("concurrent writer returned before winner commit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := winnerTx.Commit(ctx); err != nil {
		t.Fatalf("commit winner transaction: %v", err)
	}
	select {
	case err := <-loserResult:
		if !errors.Is(err, ErrStaleFinalizationWriter) {
			t.Fatalf("concurrent loser = %v, want %v", err, ErrStaleFinalizationWriter)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent loser remained blocked after winner commit")
	}

	stored := readFinalizedClaimSnapshot(t, fixture)
	if winner.Generation != 1 || !stored.Producer.Valid || stored.Producer.String != "local-runtime-a" ||
		stored.AllowedTools != `["tools:Read"]` || stored.Generation != 1 {
		t.Fatalf(
			"stored concurrent winner = producer:%v tools:%s generation:%d, want local-runtime-a tools:Read generation 1",
			stored.Producer, stored.AllowedTools, stored.Generation,
		)
	}
}

func newFinalizedClaimFixture(t *testing.T) (claimGenerationFixture, *Store, ContractInput, time.Time) {
	t.Helper()
	fixture := newClaimGenerationFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	input, err := newContractInput(
		fixture.taskID,
		fixture.workspaceID,
		fixture.agentID,
		[]string{"tools:Read"},
		nil,
		"inventory:v7",
	)
	if err != nil {
		t.Fatalf("newContractInput: %v", err)
	}
	expiresAt := time.Now().Add(time.Hour)
	if _, err := store.FinalizeClaim(ctx, input, "local-runtime", "task-claim", expiresAt); err != nil {
		t.Fatalf("FinalizeClaim: %v", err)
	}
	return fixture, store, input, expiresAt
}

type finalizedClaimSnapshot struct {
	TaskID, WorkspaceID, AgentID               pgtype.UUID
	AllowedTools                               string
	IssuedAt, ExpiresAt                        time.Time
	Generation                                 int64
	Producer, Finalizer                        pgtype.Text
	LifecycleState                             string
	InventoryVersion, DiscoveryVersion, Digest pgtype.Text
}

func readFinalizedClaimSnapshot(t *testing.T, fixture claimGenerationFixture) finalizedClaimSnapshot {
	t.Helper()
	var snapshot finalizedClaimSnapshot
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT task_id, workspace_id, agent_id, allowed_tools::text, issued_at, expires_at,
		       claim_generation, producer, finalizer, lifecycle_state,
		       inventory_version, discovery_version, finalized_grant_digest
		FROM cerebro_task_mandate
		WHERE task_id = $1`, fixture.taskID,
	).Scan(
		&snapshot.TaskID,
		&snapshot.WorkspaceID,
		&snapshot.AgentID,
		&snapshot.AllowedTools,
		&snapshot.IssuedAt,
		&snapshot.ExpiresAt,
		&snapshot.Generation,
		&snapshot.Producer,
		&snapshot.Finalizer,
		&snapshot.LifecycleState,
		&snapshot.InventoryVersion,
		&snapshot.DiscoveryVersion,
		&snapshot.Digest,
	); err != nil {
		t.Fatalf("read finalized claim snapshot: %v", err)
	}
	return snapshot
}

func requireFinalizedClaimUnchanged(t *testing.T, fixture claimGenerationFixture, before finalizedClaimSnapshot) {
	t.Helper()
	after := readFinalizedClaimSnapshot(t, fixture)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("finalized claim changed after rejected writer:\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestFinalizeClaimRejectsChangedIdentity(t *testing.T) {
	fixture, store, _, expiresAt := newFinalizedClaimFixture(t)
	before := readFinalizedClaimSnapshot(t, fixture)

	changedIdentity, err := newContractInput(
		fixture.taskID,
		fixture.workspaceID,
		fixture.otherAgent,
		[]string{"tools:Read"},
		nil,
		"inventory:v7",
	)
	if err != nil {
		t.Fatalf("changed identity input: %v", err)
	}
	if _, err := store.FinalizeClaim(context.Background(), changedIdentity, "local-runtime", "task-claim", expiresAt); err == nil || err.Error() != identityMismatchReason {
		t.Fatalf("changed identity FinalizeClaim = %v, want %q", err, identityMismatchReason)
	}
	requireFinalizedClaimUnchanged(t, fixture, before)
}

func TestFinalizeClaimRejectsChangedGrants(t *testing.T) {
	fixture, store, _, expiresAt := newFinalizedClaimFixture(t)
	before := readFinalizedClaimSnapshot(t, fixture)

	changedGrants, err := newContractInput(
		fixture.taskID,
		fixture.workspaceID,
		fixture.agentID,
		[]string{"tools:Write"},
		nil,
		"inventory:v7",
	)
	if err != nil {
		t.Fatalf("changed grants input: %v", err)
	}
	if _, err := store.FinalizeClaim(context.Background(), changedGrants, "local-runtime", "task-claim", expiresAt); err == nil || err.Error() != changedGrantReason {
		t.Fatalf("changed grants FinalizeClaim = %v, want %q", err, changedGrantReason)
	}
	requireFinalizedClaimUnchanged(t, fixture, before)
}

func TestFinalizeClaimRejectsStaleWriter(t *testing.T) {
	fixture, store, input, expiresAt := newFinalizedClaimFixture(t)
	before := readFinalizedClaimSnapshot(t, fixture)
	if _, err := store.FinalizeClaim(context.Background(), input, "stale-runtime", "task-claim", expiresAt); err == nil || err.Error() != staleWriterReason {
		t.Fatalf("stale writer FinalizeClaim = %v, want %q", err, staleWriterReason)
	}
	requireFinalizedClaimUnchanged(t, fixture, before)
}

func TestFinalizeClaimRejectsChangedExpiryBeforeRenewal(t *testing.T) {
	fixture, store, input, _ := newFinalizedClaimFixture(t)
	before := readFinalizedClaimSnapshot(t, fixture)
	if _, err := store.FinalizeClaim(context.Background(), input, "local-runtime", "task-claim", time.Now().Add(2*time.Hour)); err == nil || err.Error() != staleWriterReason {
		t.Fatalf("changed expiry FinalizeClaim = %v, want %q", err, staleWriterReason)
	}
	requireFinalizedClaimUnchanged(t, fixture, before)
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
	firstGeneration := int64(1)
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO cerebro_task_mandate (
			task_id, workspace_id, agent_id, allowed_tools, expires_at,
			claim_generation, producer, finalizer, lifecycle_state,
			inventory_version, finalized_grant_digest
		) VALUES (
			$1, $2, $3, '["tools:Write"]'::jsonb, $4,
			2, 'replacement-runtime', 'task-claim', 'finalized',
			'inventory:v8', 'sha256:replacement'
		)`, fixture.taskID, fixture.workspaceID, fixture.agentID, time.Now().Add(2*time.Hour)); err != nil {
		t.Fatalf("insert later claim generation fixture: %v", err)
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
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO cerebro_task_mandate (
			task_id, workspace_id, agent_id, allowed_tools, expires_at
		) VALUES ($1, $2, $3, '["tools:Read"]'::jsonb, $4)`,
		fixture.taskID, fixture.workspaceID, fixture.agentID, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("insert legacy claim generation: %v", err)
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
