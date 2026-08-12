package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recover-orphans must DRAIN every orphaned task for a runtime, not just the first
// page (MUL-4332 review round 3, point 1). Registration flips the runtime back
// online, so the offline sweep will not reap anything a single capped call leaves
// behind — the daemon therefore pages until has_more is false. We seed
// orphanRecoveryBatchSize + 1 orphans (one past a single page) and drive the real
// handler exactly as the daemon client does: thread the keyset cursor from each
// response into the next request. All of them must end failed, each with one
// task.failed event, across exactly two pages.
func TestRecoverOrphansDrainsPastBatchLimit(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	ctx := context.Background()

	// A dedicated runtime + agent in the handler-test workspace so the daemon token
	// (scoped to testWorkspaceID) authorizes recover-orphans for it, isolated from
	// the shared fixture runtime.
	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'orphan-drain-runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id`, testWorkspaceID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'orphan-drain-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() {
		// agent_runtime → agent → agent_task_queue cascade on delete cleans the rows;
		// task.failed events carry no FK, so drop them explicitly first.
		testPool.Exec(context.Background(),
			`DELETE FROM domain_event WHERE subject_id IN (SELECT id FROM agent_task_queue WHERE runtime_id = $1)`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// Seed one past a single page. attempt=1/max=2 with no issue/chat means the row
	// is NOT retry-eligible (retryEligible needs an issue or chat), so the shared
	// post-fail pipeline neither auto-retries nor resets an issue — the assertion is
	// about pagination alone. A single INSERT gives them all the same created_at, so
	// this also exercises the keyset id tiebreaker.
	total := orphanRecoveryBatchSize + 1
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		SELECT $1, $2, 'running', 0 FROM generate_series(1, $3)`, agentID, runtimeID, total); err != nil {
		t.Fatalf("seed orphaned tasks: %v", err)
	}

	// Drive the real handler in a drain loop, threading the cursor like the daemon.
	// paginate:true is the current daemon's capability signal — without it the server
	// treats the caller as a legacy client and drains every page itself in one call
	// (that legacy path is covered by TestRecoverOrphansLegacyClientDrainsServerSide).
	const daemonID = "recover-drain-test"
	path := fmt.Sprintf("/api/daemon/runtimes/%s/recover-orphans", runtimeID)
	var body any = map[string]any{"paginate": true}
	totalFailed, pages := 0, 0
	for {
		pages++
		if pages > 10 {
			t.Fatalf("drain did not terminate after %d pages", pages)
		}
		w := httptest.NewRecorder()
		req := withURLParam(newDaemonTokenRequest(http.MethodPost, path, body, testWorkspaceID, daemonID), "runtimeId", runtimeID)
		testHandler.RecoverOrphanedTasks(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("page %d: status %d: %s", pages, w.Code, w.Body.String())
		}
		var resp RecoverOrphansResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("page %d decode: %v", pages, err)
		}
		totalFailed += resp.Orphaned
		if !resp.HasMore {
			break
		}
		if resp.NextCursorCreatedAt == "" || resp.NextCursorID == "" {
			t.Fatalf("page %d: has_more=true but empty cursor", pages)
		}
		body = map[string]any{"paginate": true, "cursor_created_at": resp.NextCursorCreatedAt, "cursor_id": resp.NextCursorID}
	}

	if pages != 2 {
		t.Errorf("pages = %d, want 2 (a full page of %d, then the final 1)", pages, orphanRecoveryBatchSize)
	}
	if totalFailed != total {
		t.Errorf("failed across drain = %d, want %d", totalFailed, total)
	}

	// No orphan is left selectable, and each failed task has exactly one event.
	var remaining int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue
		 WHERE runtime_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')`,
		runtimeID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining orphans = %d, want 0 (drain must reach the row past the first page)", remaining)
	}
	var events int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM domain_event
		 WHERE type = 'task.failed' AND subject_id IN (SELECT id FROM agent_task_queue WHERE runtime_id = $1)`,
		runtimeID).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != total {
		t.Errorf("task.failed events = %d, want %d (fact ⇔ event, one per failed row)", events, total)
	}
}

// A LEGACY daemon (one that predates paging) POSTs {} exactly once and ignores the
// response body, so it can never thread the keyset cursor across pages. With 501+
// orphans on a re-registered runtime — which registration has already flipped back
// `online`, so the offline sweep won't reap the tail — the server must therefore
// drain every page itself in one call (MUL-4332 review round 4, point 2). We seed
// orphanRecoveryBatchSize + 1 orphans, POST a single {} request with NO paginate
// capability, and assert all of them end failed with one event each — none leak past
// the first server page.
func TestRecoverOrphansLegacyClientDrainsServerSide(t *testing.T) {
	if testPool == nil {
		t.Skip("database unavailable")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'orphan-legacy-runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id`, testWorkspaceID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'orphan-legacy-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM domain_event WHERE subject_id IN (SELECT id FROM agent_task_queue WHERE runtime_id = $1)`, runtimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// One past a single server page (see TestRecoverOrphansDrainsPastBatchLimit for
	// why attempt/max with no issue keeps the post-fail pipeline a no-op here).
	total := orphanRecoveryBatchSize + 1
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		SELECT $1, $2, 'running', 0 FROM generate_series(1, $3)`, agentID, runtimeID, total); err != nil {
		t.Fatalf("seed orphaned tasks: %v", err)
	}

	// Legacy request: {} body, no paginate capability, called exactly once.
	const daemonID = "recover-legacy-test"
	path := fmt.Sprintf("/api/daemon/runtimes/%s/recover-orphans", runtimeID)
	w := httptest.NewRecorder()
	req := withURLParam(newDaemonTokenRequest(http.MethodPost, path, map[string]any{}, testWorkspaceID, daemonID), "runtimeId", runtimeID)
	testHandler.RecoverOrphanedTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp RecoverOrphansResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.HasMore {
		t.Errorf("legacy drain returned has_more=true; the server must fully drain in one call (a legacy client cannot page)")
	}
	if resp.Orphaned != total {
		t.Errorf("orphaned = %d, want %d (single legacy call must drain past the first server page)", resp.Orphaned, total)
	}

	var remaining int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue
		 WHERE runtime_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')`,
		runtimeID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining orphans = %d, want 0 (legacy client can't page, so nothing may be left behind)", remaining)
	}
	var events int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM domain_event
		 WHERE type = 'task.failed' AND subject_id IN (SELECT id FROM agent_task_queue WHERE runtime_id = $1)`,
		runtimeID).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != total {
		t.Errorf("task.failed events = %d, want %d (fact ⇔ event, one per failed row)", events, total)
	}
}
