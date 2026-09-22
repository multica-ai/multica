package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests pin the config-API surface for ordered multi-runtime bindings
// (SE-37711 / SE-37664): the create/get/update paths that let a caller read and
// write an agent's runtime pool. They cover the invariants that acceptance calls
// out — the legacy runtime_id path is unchanged (I1/I3), an ordered pool round-
// trips (I3), a cleared pool unbinds the agent (I14), and the validation rules
// that keep a pool sane: mutual exclusion of the two wire shapes (I15), no
// duplicate runtime, a multi-runtime pool must span two provider families, and
// no binding onto a private or cross-workspace runtime.

// createPoolRuntime inserts a workspace-visible runtime owned by the test user
// with the given provider, so a pool test can assemble runtimes across distinct
// provider families. Returns the runtime id.
func createPoolRuntime(t *testing.T, ctx context.Context, name, provider string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, $5, 'public', now())
		RETURNING id
	`, testWorkspaceID, name, provider, name+" device", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert pool runtime %q: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime_binding WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

// createForeignPrivateRuntime inserts a private runtime in the test workspace
// owned by someone else, so the caller (the test user) fails the owner gate. The
// owner is a real user row (agent_runtime.owner_id has an FK to "user"), distinct
// from testUserID so the owner check rejects.
func createForeignPrivateRuntime(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	var ownerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, name+" owner", name+"-owner@handler-test.invalid").Scan(&ownerID); err != nil {
		t.Fatalf("insert foreign private runtime owner %q: %v", name, err)
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'anthropic', 'online', $3, '{}'::jsonb, $4, 'private', now())
		RETURNING id
	`, testWorkspaceID, name, name+" device", ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert foreign private runtime %q: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID)
	})
	return runtimeID
}

// createForeignWorkspaceRuntime inserts a runtime in a different workspace, so a
// workspace-scoped lookup from the test workspace cannot see it. The workspace is
// a real row (agent_runtime.workspace_id has an FK to workspace).
func createForeignWorkspaceRuntime(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, name+" workspace", name+"-ws", "foreign workspace for pool tests", "FWS").Scan(&workspaceID); err != nil {
		t.Fatalf("insert foreign workspace %q: %v", name, err)
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'anthropic', 'online', $3, '{}'::jsonb, $4, 'public', now())
		RETURNING id
	`, workspaceID, name, name+" device", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert foreign workspace runtime %q: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	return runtimeID
}

func cleanupPoolAgent(t *testing.T, name string) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			DELETE FROM agent_runtime_binding
			WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1 AND name = $2)
		`, testWorkspaceID, name)
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name)
	})
}

func createPoolAgent(t *testing.T, name string, body map[string]any) AgentResponse {
	t.Helper()
	cleanupPoolAgent(t, name)
	body["name"] = name
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent %q: expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode CreateAgent response: %v", err)
	}
	return resp
}

func updatePoolAgent(t *testing.T, agentID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(
		newRequest(http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID))
	return w
}

func getPoolAgent(t *testing.T, agentID string) AgentResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID, nil), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode GetAgent response: %v", err)
	}
	return resp
}

func assertBindings(t *testing.T, got []AgentRuntimeBindingDTO, wantRuntimeIDs ...string) {
	t.Helper()
	if len(got) != len(wantRuntimeIDs) {
		t.Fatalf("runtime_bindings: got %d entries %+v, want %d %v", len(got), got, len(wantRuntimeIDs), wantRuntimeIDs)
	}
	for i, want := range wantRuntimeIDs {
		if got[i].RuntimeID != want {
			t.Fatalf("runtime_bindings[%d].runtime_id = %q, want %q", i, got[i].RuntimeID, want)
		}
		if int(got[i].Priority) != i {
			t.Fatalf("runtime_bindings[%d].priority = %d, want %d", i, got[i].Priority, i)
		}
	}
}

// TestCreateAgent_LegacyRuntimeIDUnchanged: the pre-pool wire shape still works
// and now surfaces a singleton pool that agrees with the flat projection (I1/I3).
func TestCreateAgent_LegacyRuntimeIDUnchanged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	resp := createPoolAgent(t, "pool-legacy-create", map[string]any{
		"runtime_id": testRuntimeID,
		"visibility": "private",
	})
	if resp.RuntimeID != testRuntimeID {
		t.Fatalf("runtime_id = %q, want %q", resp.RuntimeID, testRuntimeID)
	}
	if !resp.RuntimeBound {
		t.Fatalf("runtime_bound = false, want true")
	}
	assertBindings(t, resp.RuntimeBindings, testRuntimeID)
}

// TestCreateAgent_OrderedRuntimeIDs: an ordered pool is persisted in order and
// its priority-0 runtime becomes the flat runtime_id projection (I3).
func TestCreateAgent_OrderedRuntimeIDs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	a := createPoolRuntime(t, ctx, "pool-create-a", "anthropic")
	b := createPoolRuntime(t, ctx, "pool-create-b", "openai")

	resp := createPoolAgent(t, "pool-ordered-create", map[string]any{
		"runtime_ids": []string{a, b},
		"visibility":  "private",
	})
	if resp.RuntimeID != a {
		t.Fatalf("runtime_id projection = %q, want priority-0 %q", resp.RuntimeID, a)
	}
	assertBindings(t, resp.RuntimeBindings, a, b)

	// The binding rows the create wrote are what a fresh read returns.
	assertBindings(t, getPoolAgent(t, resp.ID).RuntimeBindings, a, b)
}

// TestCreateAgent_MutualExclusion: runtime_id and runtime_ids together is a 400
// (I15) — the caller must pick one wire shape.
func TestCreateAgent_MutualExclusion(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	b := createPoolRuntime(t, ctx, "pool-mutex-b", "openai")
	cleanupPoolAgent(t, "pool-mutex-create")

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":        "pool-mutex-create",
		"runtime_id":  testRuntimeID,
		"runtime_ids": []string{b},
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateAgent_PoolValidationRejections: the pool-shape rules — no duplicate,
// a multi-runtime pool must span two provider families, no private or
// cross-workspace runtime.
func TestCreateAgent_PoolValidationRejections(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	a := createPoolRuntime(t, ctx, "pool-reject-a", "anthropic")
	sameFamily := createPoolRuntime(t, ctx, "pool-reject-a2", "anthropic")
	private := createForeignPrivateRuntime(t, ctx, "pool-reject-private")
	foreign := createForeignWorkspaceRuntime(t, ctx, "pool-reject-foreign")

	cases := []struct {
		name       string
		runtimeIDs []string
		wantStatus int
	}{
		{"duplicate", []string{a, a}, http.StatusBadRequest},
		{"same-provider-family", []string{a, sameFamily}, http.StatusBadRequest},
		{"private-runtime", []string{private}, http.StatusForbidden},
		{"cross-workspace-runtime", []string{foreign}, http.StatusBadRequest},
		{"empty-pool", []string{}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentName := "pool-reject-" + tc.name
			cleanupPoolAgent(t, agentName)
			w := httptest.NewRecorder()
			testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", map[string]any{
				"name":        agentName,
				"runtime_ids": tc.runtimeIDs,
			}))
			if w.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

// TestUpdateAgent_LegacyRuntimeIDReplacesPool: a legacy runtime_id PATCH
// collapses a multi-runtime pool to that single runtime (I1).
func TestUpdateAgent_LegacyRuntimeIDReplacesPool(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	a := createPoolRuntime(t, ctx, "pool-update-a", "anthropic")
	b := createPoolRuntime(t, ctx, "pool-update-b", "openai")
	c := createPoolRuntime(t, ctx, "pool-update-c", "google")

	agent := createPoolAgent(t, "pool-legacy-update", map[string]any{
		"runtime_ids": []string{a, b},
		"visibility":  "private",
	})
	assertBindings(t, agent.RuntimeBindings, a, b)

	w := updatePoolAgent(t, agent.ID, map[string]any{"runtime_id": c})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RuntimeID != c {
		t.Fatalf("runtime_id = %q, want %q", resp.RuntimeID, c)
	}
	assertBindings(t, resp.RuntimeBindings, c)
	assertBindings(t, getPoolAgent(t, agent.ID).RuntimeBindings, c)
}

// TestUpdateAgent_OrderedRuntimeIDsRoundTrip: an ordered pool set via update
// round-trips through GET and a reorder is honoured in order (I3).
func TestUpdateAgent_OrderedRuntimeIDsRoundTrip(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	a := createPoolRuntime(t, ctx, "pool-rt-a", "anthropic")
	b := createPoolRuntime(t, ctx, "pool-rt-b", "openai")

	agent := createPoolAgent(t, "pool-roundtrip", map[string]any{
		"runtime_id": testRuntimeID,
		"visibility": "private",
	})

	if w := updatePoolAgent(t, agent.ID, map[string]any{"runtime_ids": []string{a, b}}); w.Code != http.StatusOK {
		t.Fatalf("set pool: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertBindings(t, getPoolAgent(t, agent.ID).RuntimeBindings, a, b)

	if w := updatePoolAgent(t, agent.ID, map[string]any{"runtime_ids": []string{b, a}}); w.Code != http.StatusOK {
		t.Fatalf("reorder pool: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	reordered := getPoolAgent(t, agent.ID)
	assertBindings(t, reordered.RuntimeBindings, b, a)
	if reordered.RuntimeID != b {
		t.Fatalf("after reorder runtime_id projection = %q, want %q", reordered.RuntimeID, b)
	}
}

// TestUpdateAgent_ClearRuntimePoolUnbinds: an empty runtime_ids array (and the
// legacy runtime_id: "") empties the pool and unbinds the agent (I14).
func TestUpdateAgent_ClearRuntimePoolUnbinds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	a := createPoolRuntime(t, ctx, "pool-clear-a", "anthropic")
	b := createPoolRuntime(t, ctx, "pool-clear-b", "openai")

	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"empty-runtime_ids", map[string]any{"runtime_ids": []string{}}},
		{"legacy-empty-runtime_id", map[string]any{"runtime_id": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := createPoolAgent(t, "pool-clear-"+tc.name, map[string]any{
				"runtime_ids": []string{a, b},
				"visibility":  "private",
			})
			w := updatePoolAgent(t, agent.ID, tc.body)
			if w.Code != http.StatusOK {
				t.Fatalf("clear: expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var resp AgentResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.RuntimeBound {
				t.Fatalf("runtime_bound = true after clear, want false")
			}
			if resp.RuntimeID != "" {
				t.Fatalf("runtime_id = %q after clear, want empty", resp.RuntimeID)
			}
			assertBindings(t, resp.RuntimeBindings)
			assertBindings(t, getPoolAgent(t, agent.ID).RuntimeBindings)
		})
	}
}

// TestUpdateAgent_MutualExclusion: runtime_id and runtime_ids together is a 400
// on update as well (I15).
func TestUpdateAgent_MutualExclusion(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	b := createPoolRuntime(t, ctx, "pool-update-mutex-b", "openai")
	agent := createPoolAgent(t, "pool-update-mutex", map[string]any{
		"runtime_id": testRuntimeID,
		"visibility": "private",
	})
	w := updatePoolAgent(t, agent.ID, map[string]any{
		"runtime_id":  testRuntimeID,
		"runtime_ids": []string{b},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetAgent_SynthesizesLegacySingleton: an agent that predates pools (row
// written with a runtime_id but no binding rows) still reports a singleton pool
// so the ordered view never disagrees with the flat projection (I3).
func TestGetAgent_SynthesizesLegacySingleton(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createCascadeFixtureRuntime(t, ctx, "Pool Legacy Get Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Pool Legacy Get Agent")

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_runtime_binding WHERE agent_id = $1`, agentID).Scan(&count); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if count != 0 {
		t.Fatalf("precondition: expected no binding rows, got %d", count)
	}

	assertBindings(t, getPoolAgent(t, agentID).RuntimeBindings, runtimeID)
}

// failStatementTxStarter begins real transactions that answer one chosen sqlc
// statement (matched by its `-- name:` marker) with an injected error, so a test
// can prove a multi-statement handler transaction rolls back fully when a late
// write fails. Reads outside the transaction still hit the real pool.
type failStatementTxStarter struct {
	delegate *pgxpool.Pool
	failOn   string
}

func (s *failStatementTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failStatementTx{Tx: tx, failOn: s.failOn}, nil
}

type failStatementTx struct {
	pgx.Tx
	failOn string
}

func (t *failStatementTx) QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	if strings.Contains(query, t.failOn) {
		return errorRow{err: fmt.Errorf("injected failure on %s", t.failOn)}
	}
	return t.Tx.QueryRow(ctx, query, args...)
}

func (t *failStatementTx) Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	if strings.Contains(query, t.failOn) {
		return pgconn.CommandTag{}, fmt.Errorf("injected failure on %s", t.failOn)
	}
	return t.Tx.Exec(ctx, query, args...)
}

// TestUpdateAgent_PoolWriteFailureRollsBackProjection proves F2's atomicity: when
// the ordered-pool binding write fails mid-update, the whole transaction rolls
// back — the name projection and the runtime_id (priority-0) projection both
// keep their pre-update values, and the binding rows are untouched. Before F2
// the projection committed in its own transaction ahead of the pool write, so
// this failure left runtime_id pointing at a runtime no binding row backed
// (invariant I3 violation) and a rename that "took" while the pool did not.
func TestUpdateAgent_PoolWriteFailureRollsBackProjection(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rtA := createPoolRuntime(t, ctx, "f2-rollback-a", "anthropic")
	rtB := createPoolRuntime(t, ctx, "f2-rollback-b", "openai")

	agent := createPoolAgent(t, "f2-rollback-agent", map[string]any{
		"runtime_ids": []string{rtA, rtB},
		"visibility":  "private",
	})
	assertBindings(t, agent.RuntimeBindings, rtA, rtB)
	if agent.RuntimeID != rtA {
		t.Fatalf("precondition: runtime_id = %q, want priority-0 %q", agent.RuntimeID, rtA)
	}

	// Swap in a tx that fails the pool binding insert, then attempt an update
	// that changes BOTH the name projection and the pool order. The projection
	// write and the (now-failing) binding write must live or die together.
	failing := *testHandler
	failing.TxStarter = &failStatementTxStarter{delegate: testPool, failOn: "-- name: CreateAgentRuntimeBinding"}

	body := map[string]any{"name": "f2-rollback-agent-renamed", "runtime_ids": []string{rtB, rtA}}
	w := httptest.NewRecorder()
	failing.UpdateAgent(w, withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agent.ID, body), "id", agent.ID))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("UpdateAgent with injected pool-write failure: got %d, want 500: %s", w.Code, w.Body.String())
	}

	// Read back through the real handler: nothing changed.
	after := getPoolAgent(t, agent.ID)
	if after.Name != "f2-rollback-agent" {
		t.Fatalf("name projection = %q, want unchanged %q (rolled back)", after.Name, "f2-rollback-agent")
	}
	if after.RuntimeID != rtA {
		t.Fatalf("runtime_id projection = %q, want unchanged %q (rolled back)", after.RuntimeID, rtA)
	}
	assertBindings(t, after.RuntimeBindings, rtA, rtB)
}
