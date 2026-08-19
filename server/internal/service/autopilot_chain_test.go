package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestChainEdgeStatuses pins the per-edge status filter: a 'completed'
// upstream fires 'completed'+'any' edges, a 'failed' upstream fires
// 'failed'+'any' edges, and a non-terminal / non-outcome status (e.g.
// 'skipped', 'running') fans out nothing. This is the observable core of the
// failure-semantics config (chain_on_status) and the skipped-does-not-trigger
// rule.
func TestChainEdgeStatuses(t *testing.T) {
	cases := map[string][]string{
		"completed": {"completed", "any"},
		"failed":    {"failed", "any"},
		"skipped":   nil,
		"running":   nil,
		"pending":   nil,
		"":          nil,
	}
	for status, want := range cases {
		got := chainEdgeStatuses(status)
		if len(got) != len(want) {
			t.Errorf("chainEdgeStatuses(%q) = %v, want %v", status, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("chainEdgeStatuses(%q)[%d] = %q, want %q", status, i, got[i], want[i])
			}
		}
	}
}

// TestChainUpstreamStatusesGate pins the terminal-status gate that
// dispatchChainSuccessors opens on: completed and failed fan out, skipped and
// anything else do not. A 'skipped' run is deliberately excluded so a depth
// cap or admission cap never cascades down a 'failed' edge.
func TestChainUpstreamStatusesGate(t *testing.T) {
	if !chainUpstreamStatuses["completed"] {
		t.Errorf("completed must fan out")
	}
	if !chainUpstreamStatuses["failed"] {
		t.Errorf("failed must fan out")
	}
	if chainUpstreamStatuses["skipped"] {
		t.Errorf("skipped must NOT fan out (no cascade)")
	}
	if chainUpstreamStatuses["running"] {
		t.Errorf("running must NOT fan out (not terminal)")
	}
}

// TestBuildChainPayload verifies the chain-fired downstream payload carries the
// upstream context needed for observability and (future) template hooks, and
// that the upstream run's result is passed through when present.
func TestBuildChainPayload(t *testing.T) {
	upstreamRun := db.AutopilotRun{
		ID:          util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
		AutopilotID: pgtype.UUID{Bytes: [16]byte{0x22}, Valid: true},
		Result:      json.RawMessage(`{"summary":"ok","count":3}`),
	}
	b := buildChainPayload(upstreamRun, db.Autopilot{}, "completed", "completed")
	if b == nil {
		t.Fatal("payload is nil")
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("payload not valid json: %v", err)
	}
	chain, ok := got["chain"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing chain block: %v", got)
	}
	if chain["upstream_run_id"] != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("upstream_run_id = %v", chain["upstream_run_id"])
	}
	if chain["upstream_status"] != "completed" {
		t.Errorf("upstream_status = %v", chain["upstream_status"])
	}
	if chain["chain_on_status"] != "completed" {
		t.Errorf("chain_on_status = %v", chain["chain_on_status"])
	}
	res, ok := got["upstream_result"].(map[string]any)
	if !ok {
		t.Fatalf("upstream_result missing or wrong type: %v", got["upstream_result"])
	}
	if res["summary"] != "ok" || res["count"] != float64(3) {
		t.Errorf("upstream_result not passed through: %v", res)
	}
}

// TestBuildChainPayload_EmptyResult verifies a payload built from an upstream
// run with no result still carries the chain block and does not crash.
func TestBuildChainPayload_EmptyResult(t *testing.T) {
	upstreamRun := db.AutopilotRun{
		ID:          util.MustParseUUID("22222222-2222-2222-2222-222222222222"),
		AutopilotID: pgtype.UUID{Bytes: [16]byte{0x22}, Valid: true},
	}
	b := buildChainPayload(upstreamRun, db.Autopilot{}, "failed", "failed")
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("payload not valid json: %v", err)
	}
	chain, ok := got["chain"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing chain block: %v", got)
	}
	if chain["upstream_status"] != "failed" {
		t.Errorf("upstream_status = %v", chain["upstream_status"])
	}
}

// DetectChainCycle tests are DB-backed (CI Postgres, skip-if-unavailable like
// the rest of the suite). They seed a real chain graph and assert the DFS cycle
// guard: a proposed edge closing a loop is rejected, a non-closing edge and a
// self-edge behave correctly, and an empty graph never reports a cycle.
func TestDetectChainCycle(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	wsID, apA, apB, apC := seedChainFixtureReal(t)

	// Seed edges A->B and B->C (trigger on the downstream, naming upstream).
	seedChainEdge(t, pool, apB, apA, "completed") // A fires B
	seedChainEdge(t, pool, apC, apB, "completed") // B fires C

	// Proposed edge C->A (C fires A; trigger on A naming C upstream): does A
	// already reach C? A->B->C yes => adding C->A closes the loop => cycle.
	cycle, err := DetectChainCycle(ctx, q, wsID, apA, apC) // proposedDownstream=A, proposedUpstream=C
	if err != nil {
		t.Fatalf("DetectChainCycle C->A: %v", err)
	}
	if !cycle {
		t.Errorf("C->A should be a cycle (A->B->C already exists)")
	}

	// Proposed edge A->C (A fires C; trigger on C naming A upstream): does C
	// already reach A? C has no outgoing edges => no cycle.
	cycle, err = DetectChainCycle(ctx, q, wsID, apC, apA) // proposedDownstream=C, proposedUpstream=A
	if err != nil {
		t.Fatalf("DetectChainCycle A->C: %v", err)
	}
	if cycle {
		t.Errorf("A->C should NOT be a cycle (C cannot reach A)")
	}

	// Self-edge: an autopilot naming itself upstream is always a cycle.
	cycle, err = DetectChainCycle(ctx, q, wsID, apB, apB)
	if err != nil {
		t.Fatalf("DetectChainCycle self-edge: %v", err)
	}
	if !cycle {
		t.Errorf("self-edge should be a cycle")
	}

	// Empty graph: a workspace with no chain edges never reports a cycle.
	empty := seedEmptyWorkspaceAutopilot(t, pool)
	if cycle, err := DetectChainCycle(ctx, q, empty.ws, empty.ap, apA); err != nil {
		t.Fatalf("DetectChainCycle empty graph: %v", err)
	} else if cycle {
		t.Errorf("empty graph should not report a cycle")
	}
}

// TestChainRunIdempotencyIndex validates migration 215 at the DB level: two
// chain runs anchored on the same (chain_upstream_run_id, trigger_id) cannot
// coexist - the second INSERT must fail with unique_violation (23505). A
// non-chain run, or a chain run anchored on a different upstream run, must
// remain free to coexist. This is the hard guard behind the idempotency fast
// path + recoverConcurrentChainAdmission in DispatchAutopilotForChain.
func TestChainRunIdempotencyIndex(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	_, apA, apB, _ := seedChainFixtureReal(t)

	// A chain trigger on B naming A upstream.
	triggerID := seedChainEdge(t, pool, apB, apA, "completed")

	// Upstream run on A.
	upstreamRunID := seedAutopilotRun(t, pool, apA, "completed")

	// First chain run on B anchored on (upstreamRunID, triggerID).
	if _, err := pool.Exec(ctx, `
		INSERT INTO autopilot_run (autopilot_id, trigger_id, source, status, chain_depth, chain_upstream_run_id)
		VALUES ($1, $2, 'chain', 'completed', 1, $3)`,
		apB, triggerID, upstreamRunID); err != nil {
		t.Fatalf("seed first chain run: %v", err)
	}

	// Second chain run with the same anchor must violate the partial unique index.
	_, err := pool.Exec(ctx, `
		INSERT INTO autopilot_run (autopilot_id, trigger_id, source, status, chain_depth, chain_upstream_run_id)
		VALUES ($1, $2, 'chain', 'completed', 1, $3)`,
		apB, triggerID, upstreamRunID)
	if err == nil {
		t.Fatalf("duplicate chain run insert unexpectedly succeeded; idempotency index missing")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected pgconn.PgError, got %T: %v", err, err)
	}
	if pgErr.Code != "23505" {
		t.Errorf("duplicate chain run error code = %s, want 23505 (unique_violation)", pgErr.Code)
	}

	// A non-chain run on the same trigger + upstream may coexist: the partial
	// index only covers source='chain'.
	if _, err := pool.Exec(ctx, `
		INSERT INTO autopilot_run (autopilot_id, trigger_id, source, status, chain_upstream_run_id)
		VALUES ($1, $2, 'manual', 'completed', $3)`,
		apB, triggerID, upstreamRunID); err != nil {
		t.Fatalf("non-chain run on same trigger/upstream should be allowed: %v", err)
	}

	// A chain run anchored on a DIFFERENT upstream run is allowed.
	otherUpstreamRunID := seedAutopilotRun(t, pool, apA, "completed")
	if _, err := pool.Exec(ctx, `
		INSERT INTO autopilot_run (autopilot_id, trigger_id, source, status, chain_depth, chain_upstream_run_id)
		VALUES ($1, $2, 'chain', 'completed', 1, $3)`,
		apB, triggerID, otherUpstreamRunID); err != nil {
		t.Fatalf("chain run on different upstream should be allowed: %v", err)
	}
}

// --- helpers ---

// chainTestPool is the subset of *pgxpool.Pool the seed helpers need.
type chainTestPool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// seedChainFixtureReal creates a workspace + agent + three autopilots A/B/C
// (assignee = that agent) for chain tests, reusing seedAttributionFixture for
// the workspace/user/agent/runtime graph. Returns the workspace + autopilot
// UUIDs.
func seedChainFixtureReal(t *testing.T) (wsID, apA, apB, apC pgtype.UUID) {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	mk := func(name string) pgtype.UUID {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO autopilot (workspace_id, title, assignee_id, assignee_type, execution_mode, status,
				created_by_type, created_by_id)
			VALUES ($1, $2, $3, 'agent', 'run_only', 'active', 'member', $4)
			RETURNING id`, workspaceID, name, agentID, userID).Scan(&id); err != nil {
			t.Fatalf("seed autopilot %s: %v", name, err)
		}
		return util.MustParseUUID(id)
	}
	return util.MustParseUUID(workspaceID), mk("chain-A"), mk("chain-B"), mk("chain-C")
}

// seedChainEdge inserts a chain trigger on `downstream` naming `upstream` as
// its upstream, enabled, with the given chain_on_status. Returns the trigger id.
func seedChainEdge(t *testing.T, pool chainTestPool, downstream, upstream pgtype.UUID, onStatus string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, upstream_autopilot_id, chain_on_status)
		VALUES ($1, 'chain', true, $2, $3)
		RETURNING id`, downstream, upstream, onStatus).Scan(&id); err != nil {
		t.Fatalf("seed chain edge: %v", err)
	}
	return util.MustParseUUID(id)
}

// seedAutopilotRun inserts a minimal manual autopilot_run and returns its id.
func seedAutopilotRun(t *testing.T, pool chainTestPool, autopilotID pgtype.UUID, status string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', $2)
		RETURNING id`, autopilotID, status).Scan(&id); err != nil {
		t.Fatalf("seed autopilot run: %v", err)
	}
	return util.MustParseUUID(id)
}

// seedEmptyWorkspaceAutopilot returns a fresh workspace + a single autopilot
// with no chain edges, for the empty-graph cycle case. Cleanup is on t.
func seedEmptyWorkspaceAutopilot(t *testing.T, pool chainTestPool) struct{ ws, ap pgtype.UUID } {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('chain empty', $1) RETURNING id`,
		"chain-empty-"+suffix+"@multica.test").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	var wsID string
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('chain empty ws', $1) RETURNING id`,
		"chain-empty-"+suffix).Scan(&wsID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID) })

	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'chain-empty-rt', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id`, wsID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}

	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'chain-empty-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, wsID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	var apID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, assignee_type, execution_mode, status,
			created_by_type, created_by_id)
		VALUES ($1, 'chain-empty-ap', $2, 'agent', 'run_only', 'active', 'member', $3)
		RETURNING id`, wsID, agentID, userID).Scan(&apID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	return struct{ ws, ap pgtype.UUID }{ws: util.MustParseUUID(wsID), ap: util.MustParseUUID(apID)}
}

// ensure the unused-import guard for pgxpool stays accurate: newResolveOriginatorPool
// returns *pgxpool.Pool, which satisfies chainTestPool.
var _ chainTestPool = (*pgxpool.Pool)(nil)
