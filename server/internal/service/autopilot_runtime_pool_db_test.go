package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestSelectPoolRuntimeAgainstDB pins the breaker-aware selector (SE-37711 /
// SE-37664) against a real database: selectPoolRuntime must walk the agent's
// ordered bindings, read each runtime's provider circuit, and return the first
// runtime that is both ready and not held. It exercises the DB wiring the pure
// selectFallbackRuntime / circuitHeldAt unit tests cannot — the binding order
// query, the GetRuntimeProviderCircuit read, and the pgx.ErrNoRows "no row means
// closed" path — across the outcomes the dispatch callers branch on:
//
//   - primary healthy -> selected primary (I1: no fallback when unnecessary);
//   - primary held by an open circuit, secondary healthy -> fallover (I6);
//   - every ready binding held -> fallbackAllHeld with the earliest reset (I9);
//   - an open circuit whose reset_at has elapsed -> primary eligible again (I7).
func TestSelectPoolRuntimeAgainstDB(t *testing.T) {
	pool := sharedTestPool(t)
	ctx := context.Background()
	svc := &AutopilotService{Queries: db.New(pool)}

	suffix := time.Now().UnixNano()
	userID := insertBindingTeardownRow(t, pool, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Pool Select", fmt.Sprintf("pool-select-%d@multica.ai", suffix))
	workspaceID := insertBindingTeardownRow(t, pool, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'PSL') RETURNING id`,
		"Pool Select", fmt.Sprintf("pool-select-%d", suffix))
	execBindingTeardown(t, pool, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	// insertCircuit opens a circuit for a runtime's provider (the fixture
	// provider is binding_teardown_test) at a given state and reset instant.
	insertCircuit := func(runtimeID, state string, resetAt time.Time) {
		t.Helper()
		var reset any
		if !resetAt.IsZero() {
			reset = resetAt
		}
		execBindingTeardown(t, pool, `
			INSERT INTO runtime_provider_circuit (workspace_id, runtime_id, provider, state, reset_at, opened_at)
			VALUES ($1, $2, 'binding_teardown_test', $3, $4, now())`,
			workspaceID, runtimeID, state, reset)
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM runtime_provider_circuit WHERE runtime_id = $1`, runtimeID)
		})
	}
	agentRow := func(agentID string) db.Agent {
		t.Helper()
		a, err := svc.Queries.GetAgent(ctx, util.MustParseUUID(agentID))
		if err != nil {
			t.Fatalf("load agent %s: %v", agentID, err)
		}
		return a
	}

	future := time.Now().UTC().Add(2 * time.Hour)
	past := time.Now().UTC().Add(-2 * time.Hour)

	t.Run("primary healthy is selected without fallback", func(t *testing.T) {
		r0 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "A r0")
		r1 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "A r1")
		agent := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "agent A", r0)
		insertBinding(t, pool, workspaceID, agent, r0, 0)
		insertBinding(t, pool, workspaceID, agent, r1, 1)

		decision, _, err := svc.selectPoolRuntime(ctx, agentRow(agent))
		if err != nil {
			t.Fatalf("selectPoolRuntime: %v", err)
		}
		if decision.Outcome != fallbackSelected || util.UUIDToString(decision.Chosen.RuntimeID) != r0 {
			t.Fatalf("outcome=%v chosen=%s, want selected r0=%s", decision.Outcome, util.UUIDToString(decision.Chosen.RuntimeID), r0)
		}
	})

	t.Run("held primary falls over to the healthy secondary", func(t *testing.T) {
		r0 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "B r0")
		r1 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "B r1")
		agent := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "agent B", r0)
		insertBinding(t, pool, workspaceID, agent, r0, 0)
		insertBinding(t, pool, workspaceID, agent, r1, 1)
		insertCircuit(r0, "open", future) // primary held; r1 has no circuit row (closed)

		decision, _, err := svc.selectPoolRuntime(ctx, agentRow(agent))
		if err != nil {
			t.Fatalf("selectPoolRuntime: %v", err)
		}
		if decision.Outcome != fallbackSelected || util.UUIDToString(decision.Chosen.RuntimeID) != r1 {
			t.Fatalf("outcome=%v chosen=%s, want selected r1=%s", decision.Outcome, util.UUIDToString(decision.Chosen.RuntimeID), r1)
		}
	})

	t.Run("all held reports the earliest known reset", func(t *testing.T) {
		r0 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "C r0")
		r1 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "C r1")
		agent := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "agent C", r0)
		insertBinding(t, pool, workspaceID, agent, r0, 0)
		insertBinding(t, pool, workspaceID, agent, r1, 1)
		earlier := time.Now().UTC().Add(1 * time.Hour)
		insertCircuit(r0, "open", future)
		insertCircuit(r1, "open", earlier)

		decision, _, err := svc.selectPoolRuntime(ctx, agentRow(agent))
		if err != nil {
			t.Fatalf("selectPoolRuntime: %v", err)
		}
		if decision.Outcome != fallbackAllHeld || decision.HeldCount != 2 {
			t.Fatalf("outcome=%v held=%d, want allHeld/2", decision.Outcome, decision.HeldCount)
		}
		if !decision.EarliestKnown || decision.EarliestReset.Sub(earlier).Abs() > time.Second {
			t.Fatalf("earliest=%v known=%v, want ~%v", decision.EarliestReset, decision.EarliestKnown, earlier)
		}
	})

	t.Run("open circuit past its reset makes the primary eligible again", func(t *testing.T) {
		r0 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "D r0")
		r1 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "D r1")
		agent := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "agent D", r0)
		insertBinding(t, pool, workspaceID, agent, r0, 0)
		insertBinding(t, pool, workspaceID, agent, r1, 1)
		insertCircuit(r0, "open", past) // window elapsed -> not held, primary is the natural probe

		decision, _, err := svc.selectPoolRuntime(ctx, agentRow(agent))
		if err != nil {
			t.Fatalf("selectPoolRuntime: %v", err)
		}
		if decision.Outcome != fallbackSelected || util.UUIDToString(decision.Chosen.RuntimeID) != r0 {
			t.Fatalf("outcome=%v chosen=%s, want selected r0=%s (reset elapsed)", decision.Outcome, util.UUIDToString(decision.Chosen.RuntimeID), r0)
		}
	})

	// insertCircuitReason opens a circuit carrying a failure class, so the DB
	// path proves the selector reads runtime_provider_circuit.reason and applies
	// the F3 auth-no-switch rule off it.
	insertCircuitReason := func(runtimeID, state, reason string, resetAt time.Time) {
		t.Helper()
		var reset any
		if !resetAt.IsZero() {
			reset = resetAt
		}
		execBindingTeardown(t, pool, `
			INSERT INTO runtime_provider_circuit (workspace_id, runtime_id, provider, state, reason, reset_at, opened_at)
			VALUES ($1, $2, 'binding_teardown_test', $3, $4, $5, now())`,
			workspaceID, runtimeID, state, reason, reset)
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM runtime_provider_circuit WHERE runtime_id = $1`, runtimeID)
		})
	}

	t.Run("auth-held primary skips with no fallback to a healthy secondary", func(t *testing.T) {
		r0 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "E r0")
		r1 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "E r1")
		agent := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "agent E", r0)
		insertBinding(t, pool, workspaceID, agent, r0, 0)
		insertBinding(t, pool, workspaceID, agent, r1, 1)
		insertCircuitReason(r0, "open", circuitClassAuth, future) // primary auth-held; r1 healthy

		decision, _, err := svc.selectPoolRuntime(ctx, agentRow(agent))
		if err != nil {
			t.Fatalf("selectPoolRuntime: %v", err)
		}
		if decision.Outcome != fallbackAuthHeld {
			t.Fatalf("outcome=%v, want auth held (no fallback to r1=%s)", decision.Outcome, r1)
		}
		if !decision.EarliestKnown || decision.EarliestReset.Sub(future).Abs() > time.Second {
			t.Fatalf("earliest=%v known=%v, want ~%v", decision.EarliestReset, decision.EarliestKnown, future)
		}
	})

	t.Run("quota-held primary still falls over to the healthy secondary", func(t *testing.T) {
		r0 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "F r0")
		r1 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "F r1")
		agent := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "agent F", r0)
		insertBinding(t, pool, workspaceID, agent, r0, 0)
		insertBinding(t, pool, workspaceID, agent, r1, 1)
		insertCircuitReason(r0, "open", circuitClassQuota, future) // primary quota-held; r1 healthy

		decision, _, err := svc.selectPoolRuntime(ctx, agentRow(agent))
		if err != nil {
			t.Fatalf("selectPoolRuntime: %v", err)
		}
		if decision.Outcome != fallbackSelected || util.UUIDToString(decision.Chosen.RuntimeID) != r1 {
			t.Fatalf("outcome=%v chosen=%s, want selected r1=%s (quota falls through)", decision.Outcome, util.UUIDToString(decision.Chosen.RuntimeID), r1)
		}
	})
}
