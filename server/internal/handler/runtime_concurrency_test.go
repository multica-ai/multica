package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestUpdateAgentRuntime_MaxConcurrentTasks covers the runtime-wide cap
// (GitHub #9626): a PATCH sets it, JSON null clears it, an absent field
// leaves it unchanged, out-of-range / non-integer values are rejected before
// any write, and a plain member cannot change someone else's runtime.
func TestUpdateAgentRuntime_MaxConcurrentTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)

	decode := func(t *testing.T, body []byte) AgentRuntimeResponse {
		t.Helper()
		var resp AgentRuntimeResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	w := patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("empty PATCH: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decode(t, w.Body.Bytes()); resp.MaxConcurrentTasks != nil {
		t.Fatalf("new runtime should have no cap, got %d", *resp.MaxConcurrentTasks)
	}

	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"max_concurrent_tasks": 1})
	if w.Code != http.StatusOK {
		t.Fatalf("set cap: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decode(t, w.Body.Bytes()); resp.MaxConcurrentTasks == nil || *resp.MaxConcurrentTasks != 1 {
		t.Fatalf("cap after set: got %v, want 1", resp.MaxConcurrentTasks)
	}

	// A PATCH for another field must not touch the cap.
	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"custom_name": "GPU box"})
	if w.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decode(t, w.Body.Bytes()); resp.MaxConcurrentTasks == nil || *resp.MaxConcurrentTasks != 1 {
		t.Fatalf("cap after unrelated PATCH: got %v, want 1", resp.MaxConcurrentTasks)
	}

	for _, bad := range []any{0, 51, -1, 1.5, "2", true} {
		w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"max_concurrent_tasks": bad})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("cap=%v: expected 400, got %d: %s", bad, w.Code, w.Body.String())
		}
	}

	w = patchRuntimeCustomName(plainMemberID, runtimeID, map[string]any{"max_concurrent_tasks": 5})
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = patchRuntimeCustomName(runtimeOwnerID, runtimeID, map[string]any{"max_concurrent_tasks": nil})
	if w.Code != http.StatusOK {
		t.Fatalf("clear cap: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decode(t, w.Body.Bytes()); resp.MaxConcurrentTasks != nil {
		t.Fatalf("cap after clear: got %d, want null", *resp.MaxConcurrentTasks)
	}
}

// TestUpdateAgentRuntime_MaxConcurrentTasksWaitsForInFlightClaim: the cap
// update must not commit while a claim on the same runtime holds the claim
// lock, so once PATCH returns no claim can still commit against the old cap.
func TestUpdateAgentRuntime_MaxConcurrentTasksWaitsForInFlightClaim(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID, _, _ := runtimeVisibilityFixture(t)
	rtUUID := util.MustParseUUID(runtimeID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	claimTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer claimTx.Rollback(context.Background())
	if err := db.New(claimTx).LockRuntimeClaimCapacity(ctx, rtUUID); err != nil {
		t.Fatalf("lock: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := testHandler.updateRuntimeMaxConcurrentTasks(ctx, rtUUID, pgtype.Int4{Int32: 1, Valid: true})
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("cap update returned (err=%v) while a claim held the runtime lock", err)
	case <-time.After(300 * time.Millisecond):
	}
	var visible pgtype.Int4
	if err := testPool.QueryRow(ctx, `SELECT max_concurrent_tasks FROM agent_runtime WHERE id = $1`, rtUUID).Scan(&visible); err != nil {
		t.Fatalf("read cap: %v", err)
	}
	if visible.Valid {
		t.Fatalf("cap %d visible before the in-flight claim finished", visible.Int32)
	}

	if err := claimTx.Commit(ctx); err != nil {
		t.Fatalf("commit claim tx: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cap update: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("cap update did not finish after the claim committed")
	}
}
