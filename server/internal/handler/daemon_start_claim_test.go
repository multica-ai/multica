package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func startClaimFixture(t *testing.T, status string) (string, string, time.Time) {
	t.Helper()
	if testHandler == nil {
		t.Fatal("PostgreSQL is required for start claim tests")
	}
	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id=$1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	issueID := dbfx.Issue(t, "start claim ownership")
	generation := time.Date(2026, 9, 17, 9, 0, 0, 123456000, time.UTC)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": testRuntimeID,
		"status": status, "dispatched_at": generation,
	})
	return taskID, testRuntimeID, generation
}

func startClaimRequest(taskID, runtimeID string, generation time.Time) *http.Request {
	return withURLParam(newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/start", map[string]string{
		"runtime_id": runtimeID, "dispatched_at": generation.Format(time.RFC3339Nano),
	}, testWorkspaceID, "start-claim-test"), "taskId", taskID)
}

func TestStartClaimLostResponseAndOwnership(t *testing.T) {
	for _, state := range []string{"dispatched", "waiting_local_directory"} {
		t.Run(state, func(t *testing.T) {
			id, runtimeID, generation := startClaimFixture(t, state)
			// A stale generation on the same runtime cannot win the FIRST start.
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation.Add(-time.Microsecond))).Want(http.StatusConflict)
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, "00000000-0000-0000-0000-000000000001", generation)).Want(http.StatusConflict)
			// Commit the first request, then discard its response (lost acknowledgement).
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusOK)
			var before pgtype.Timestamptz
			dbfx.QueryRow(t, `SELECT started_at FROM agent_task_queue WHERE id=$1`, id).Scan(&before)
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusOK)
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation.Add(-time.Microsecond))).Want(http.StatusConflict)
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, "00000000-0000-0000-0000-000000000001", generation)).Want(http.StatusConflict)
			var after pgtype.Timestamptz
			dbfx.QueryRow(t, `SELECT started_at FROM agent_task_queue WHERE id=$1`, id).Scan(&after)
			if !before.Valid || !before.Time.Equal(after.Time) {
				t.Fatalf("replay changed started_at: %v -> %v", before, after)
			}
		})
	}
}

func TestStartClaimReclaimedGeneration(t *testing.T) {
	id, runtimeID, generation := startClaimFixture(t, "dispatched")
	// Reclaim refreshes dispatched_at even if the runtime stays the same.
	newGeneration := generation.Add(time.Microsecond)
	dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at=$2 WHERE id=$1`, id, newGeneration)
	testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusConflict)
	testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, newGeneration)).Want(http.StatusOK)
	testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusConflict)
}

func TestStartClaimConcurrentReplay(t *testing.T) {
	id, runtimeID, generation := startClaimFixture(t, "dispatched")
	const workers = 12
	results := make(chan *httptest.ResponseRecorder, workers)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			w := httptest.NewRecorder()
			testHandler.StartTask(w, startClaimRequest(id, runtimeID, generation))
			results <- w
		}()
	}
	close(gate)
	wg.Wait()
	close(results)
	var started string
	for w := range results {
		if w.Code != http.StatusOK {
			t.Fatalf("concurrent start: %d %s", w.Code, w.Body.String())
		}
		var task AgentTaskResponse
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if task.StartedAt == nil {
			t.Fatal("missing started_at")
		}
		if started != "" && started != *task.StartedAt {
			t.Fatal("multiple start timestamps")
		}
		started = *task.StartedAt
	}
}

func TestStartClaimCancellationRace(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(fmt.Sprintf("replay=%t", replay), func(t *testing.T) {
			id, runtimeID, generation := startClaimFixture(t, "dispatched")
			if replay {
				testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusOK)
			}
			ctx := context.Background()
			tx, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx, `UPDATE agent_task_queue SET status='cancelled' WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				testHandler.StartTask(w, startClaimRequest(id, runtimeID, generation))
				result <- w
			}()
			// Observe the actual PostgreSQL row-lock wait before committing cancellation.
			deadline := time.Now().Add(5 * time.Second)
			for {
				var blocked bool
				dbfx.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '-- name: LockAgentTaskStartClaim%')`).Scan(&blocked)
				if blocked {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("start did not wait on cancellation lock")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case w := <-result:
				if w.Code != http.StatusConflict {
					t.Fatalf("cancelled start: %d %s", w.Code, w.Body.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("start blocked after cancellation committed")
			}
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusConflict)
			var status string
			dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, id).Scan(&status)
			if status != "cancelled" {
				t.Fatalf("cancellation overwritten: %s", status)
			}
		})
	}
}

func TestStartClaimInvalidBodiesAndLegacy(t *testing.T) {
	id, runtimeID, generation := startClaimFixture(t, "dispatched")
	for _, body := range []any{
		map[string]string{"runtime_id": runtimeID},
		map[string]string{"dispatched_at": generation.Format(time.RFC3339Nano)},
		map[string]string{"runtime_id": "invalid", "dispatched_at": generation.Format(time.RFC3339Nano)},
		map[string]string{"runtime_id": runtimeID, "dispatched_at": generation.Add(time.Nanosecond).Format(time.RFC3339Nano)},
	} {
		req := withURLParam(newDaemonTokenRequest("POST", "/start", body, testWorkspaceID, "legacy-test"), "taskId", id)
		testutil.Call(t, testHandler.StartTask, req).Want(http.StatusBadRequest)
	}
	for _, code := range []int{http.StatusOK, http.StatusBadRequest} {
		req := withURLParam(newDaemonTokenRequest("POST", "/start", nil, testWorkspaceID, "legacy-test"), "taskId", id)
		testutil.Call(t, testHandler.StartTask, req).Want(code)
	}
}

func TestStartClaimWirePrecision(t *testing.T) {
	id, runtimeID, generation := startClaimFixture(t, "dispatched")
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	req := newDaemonTokenRequest("POST", "/claim", nil, testWorkspaceID, "claim-wire-test")
	// Exercise non-UTC database timestamp locations even on a UTC CI runner.
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("UTC+08", 8*60*60), time.FixedZone("UTC-07", -7*60*60)} {
		t.Run(zone.String(), func(t *testing.T) {
			claimed := task
			claimed.DispatchedAt.Time = task.DispatchedAt.Time.In(zone)
			resp, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, &claimed, runtime, runtimeID, testWorkspaceID)
			if failure != nil {
				t.Fatalf("build claim: %+v", failure)
			}
			want := generation.UTC().Format(time.RFC3339Nano)
			if !resp.StartClaimSupported || resp.DispatchedAt == nil {
				t.Fatalf("missing claim timestamp or capability: supported=%t timestamp=%v", resp.StartClaimSupported, resp.DispatchedAt)
			}
			if *resp.DispatchedAt != want {
				t.Fatalf("claim timestamp = %q, want canonical UTC with microseconds %q", *resp.DispatchedAt, want)
			}
		})
	}
}
