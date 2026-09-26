package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// These tests pin the claim-generation fence on the terminal callbacks: the
// dispatched_at the server issued on the claim is compared inside the very
// UPDATE that performs the terminal transition, so a report produced by an
// older claim generation can never settle a later reclaim of the same task id.
// They run against the real database because the guarantee lives in that SQL.

const terminalFenceDaemonID = "terminal-fence-daemon"

// fencedTerminalTask seeds one task in the given non-terminal status, with a
// claim generation already stamped on the row, and returns the task id and the
// exact generation the server issued for it.
func fencedTerminalTask(t *testing.T, status string, extra testutil.Cols) (string, time.Time) {
	t.Helper()

	var agentID, runtimeID string
	dbfx.QueryRow(t, `SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID)

	cols := testutil.Cols{
		"runtime_id": runtimeID,
		// The daemon access check resolves the task's workspace through one of
		// its source links, so the fixture needs a real issue behind the task.
		"issue_id":      dbfx.Issue(t, "terminal fence "+status),
		"status":        status,
		"dispatched_at": testutil.Raw("now()"),
	}
	if status == "running" {
		cols["started_at"] = testutil.Raw("now()")
	}
	for key, value := range extra {
		cols[key] = value
	}
	taskID := dbfx.Task(t, agentID, cols)

	var generation time.Time
	dbfx.QueryRow(t, `SELECT dispatched_at FROM agent_task_queue WHERE id = $1`, taskID).Scan(&generation)
	return taskID, generation
}

// postTerminalCallback drives the real handler for one terminal callback.
func postTerminalCallback(t *testing.T, taskID, endpoint string, body map[string]any) *httptest.ResponseRecorder {
	return postTerminalCallbackTo(t, "/api/daemon/tasks/"+taskID+"/"+endpoint, endpoint, body)
}

// postFencedTerminalCallback drives the versioned terminal route, which requires
// the claim generation and has no unfenced mode.
func postFencedTerminalCallback(t *testing.T, taskID, endpoint string, body map[string]any) *httptest.ResponseRecorder {
	return postTerminalCallbackTo(t, "/api/daemon/v2/tasks/"+taskID+"/"+endpoint, endpoint, body)
}

func postTerminalCallbackTo(t *testing.T, path, endpoint string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, path, body,
		testWorkspaceID, terminalFenceDaemonID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", pathTaskID(path))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if endpoint == "complete" {
		if strings.Contains(path, "/v2/") {
			testHandler.CompleteTaskV2(w, req)
		} else {
			testHandler.CompleteTask(w, req)
		}
	} else {
		if strings.Contains(path, "/v2/") {
			testHandler.FailTaskV2(w, req)
		} else {
			testHandler.FailTask(w, req)
		}
	}
	return w
}

// pathTaskID pulls the task id out of a terminal callback path.
func pathTaskID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}

// reclaimDispatchedTask issues a fresh claim generation through the production
// stale-dispatch reclaim query — the only reclaim path for a row that never
// reached StartTask (status dispatched, started_at NULL). The row is aged into
// the stale window first; the generation comes back from the real UPDATE, it
// is never rewritten by test SQL.
func reclaimDispatchedTask(t *testing.T, taskID string) time.Time {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&runtimeID)
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET status = 'online', last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("refresh runtime for reclaim: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET dispatched_at = now() - interval '10 minutes', prepare_lease_expires_at = now() - interval '1 minute' WHERE id = $1`, taskID); err != nil {
		t.Fatalf("age dispatch for reclaim: %v", err)
	}
	reclaimed, err := testHandler.Queries.ReclaimStaleDispatchedTaskForRuntime(ctx, db.ReclaimStaleDispatchedTaskForRuntimeParams{
		RuntimeID:         parseUUID(runtimeID),
		ClaimRecoverySecs: 30,
		PrepareLeaseSecs:  60,
		RuntimeStaleSecs:  service.RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		t.Fatalf("reclaim stale dispatch: %v", err)
	}
	if uuidToString(reclaimed.ID) != taskID {
		t.Fatalf("reclaimed task = %s, want %s", uuidToString(reclaimed.ID), taskID)
	}
	return reclaimed.DispatchedAt.Time
}

func taskRowState(t *testing.T, taskID string) (status string, generation time.Time, result []byte, completedAt *time.Time) {
	t.Helper()
	dbfx.QueryRow(t, `SELECT status, dispatched_at, result, completed_at FROM agent_task_queue WHERE id = $1`, taskID).
		Scan(&status, &generation, &result, &completedAt)
	return status, generation, result, completedAt
}

func responseCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body %q: %v", w.Body.String(), err)
	}
	code, _ := body["code"].(string)
	return code
}

// TestTerminalCallbackFenceIsTheUpdateAndNotAPriorLookup documents the TOCTOU
// shape: the generation the callback carries was read from the row before the
// reclaim, so any "GET the row, compare, then POST" implementation would have
// seen a live row and let the stale report through. Only the comparison inside
// the terminal UPDATE rejects it. It runs the primary regression on the real
// lifecycle: dispatched (G1), still before StartTask, then a production
// stale-dispatch reclaim issues G2, and the persisted G1 failure replays
// against a row G2 owns.
func TestTerminalCallbackFenceIsTheUpdateAndNotAPriorLookup(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	taskID, generation := fencedTerminalTask(t, "dispatched", nil)
	// The reclaim lands after the daemon has its generation in hand and before
	// its callback reaches the terminal UPDATE.
	reclaimed := reclaimDispatchedTask(t, taskID)

	w := postFencedTerminalCallback(t, taskID, "fail", map[string]any{
		"error":                  "runtime went offline before the run started",
		"expected_dispatched_at": generation.UTC().Format(time.RFC3339Nano),
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("reclaim race: got %d: %s, want 409", w.Code, w.Body.String())
	}
	if code := responseCode(t, w); code != protocol.DaemonTaskClaimGenerationMismatchCode {
		t.Fatalf("conflict code = %q, want %q", code, protocol.DaemonTaskClaimGenerationMismatchCode)
	}
	status, current, result, _ := taskRowState(t, taskID)
	if status != "dispatched" || !current.Equal(reclaimed) || result != nil {
		t.Fatalf("row changed under the race: status=%s generation=%s result=%s", status, current, result)
	}

	// The reclaim that owns the row can still settle it with its own generation.
	if w := postFencedTerminalCallback(t, taskID, "fail", map[string]any{
		"error":                  "failure of the new claim",
		"expected_dispatched_at": reclaimed.UTC().Format(time.RFC3339Nano),
	}); w.Code != http.StatusOK {
		t.Fatalf("new claim could not settle its own row: got %d: %s", w.Code, w.Body.String())
	}
}

// TestFencedTerminalEndpointContract owns the generation contract. The versioned
// route is the only surface a current daemon sends generation-aware reports to,
// so the semantics live here: the generation is mandatory, it is compared inside
// the terminal UPDATE, and every outcome the daemon must distinguish has a stable
// shape.
func TestFencedTerminalEndpointContract(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("complete", func(t *testing.T) {
		t.Run("matching generation completes and replays idempotently", func(t *testing.T) {
			taskID, generation := fencedTerminalTask(t, "running", nil)
			body := map[string]any{
				"output":                 "done",
				"expected_dispatched_at": generation.UTC().Format(time.RFC3339Nano),
			}
			if w := postFencedTerminalCallback(t, taskID, "complete", body); w.Code != http.StatusOK {
				t.Fatalf("fenced complete: got %d: %s", w.Code, w.Body.String())
			}
			// The response was lost; the daemon replays the same fenced report.
			if w := postFencedTerminalCallback(t, taskID, "complete", body); w.Code != http.StatusOK {
				t.Fatalf("fenced replay: got %d: %s, want idempotent 200", w.Code, w.Body.String())
			}
			if status, _, _, _ := taskRowState(t, taskID); status != "completed" {
				t.Fatalf("status = %q, want completed", status)
			}
		})

		t.Run("stale generation conflicts and leaves the reclaim alone", func(t *testing.T) {
			taskID, generation := fencedTerminalTask(t, "dispatched", nil)
			reclaimed := reclaimDispatchedTask(t, taskID)
			// The reclaim owns the row now and reaches running through StartTask.
			if _, err := testHandler.TaskService.StartTask(context.Background(), parseUUID(taskID)); err != nil {
				t.Fatalf("start reclaimed task: %v", err)
			}
			w := postFencedTerminalCallback(t, taskID, "complete", map[string]any{
				"output":                 "old claim result",
				"expected_dispatched_at": generation.UTC().Format(time.RFC3339Nano),
			})
			if w.Code != http.StatusConflict {
				t.Fatalf("stale fenced complete: got %d: %s, want 409", w.Code, w.Body.String())
			}
			if code := responseCode(t, w); code != protocol.DaemonTaskClaimGenerationMismatchCode {
				t.Fatalf("conflict code = %q, want %q", code, protocol.DaemonTaskClaimGenerationMismatchCode)
			}
			status, current, result, _ := taskRowState(t, taskID)
			if status != "running" || !current.Equal(reclaimed) || result != nil {
				t.Fatalf("reclaim was mutated: status=%s generation=%s result=%s", status, current, result)
			}
		})

		// The server-terminal case that is NOT idempotent: the row was settled by
		// a later claim, so acknowledging the older report as "already finalized"
		// would tell the daemon its stale result had landed.
		t.Run("stale generation after a newer settlement conflicts", func(t *testing.T) {
			taskID, generation := fencedTerminalTask(t, "dispatched", nil)
			reclaimed := reclaimDispatchedTask(t, taskID)
			if _, err := testHandler.TaskService.StartTask(context.Background(), parseUUID(taskID)); err != nil {
				t.Fatalf("start reclaimed task: %v", err)
			}
			if w := postFencedTerminalCallback(t, taskID, "complete", map[string]any{
				"output":                 "result of the new claim",
				"expected_dispatched_at": reclaimed.UTC().Format(time.RFC3339Nano),
			}); w.Code != http.StatusOK {
				t.Fatalf("new claim could not settle its own row: got %d: %s", w.Code, w.Body.String())
			}
			w := postFencedTerminalCallback(t, taskID, "complete", map[string]any{
				"output":                 "result of the old claim",
				"expected_dispatched_at": generation.UTC().Format(time.RFC3339Nano),
			})
			if w.Code != http.StatusConflict {
				t.Fatalf("stale complete after a newer settlement: got %d: %s, want 409", w.Code, w.Body.String())
			}
			status, current, result, _ := taskRowState(t, taskID)
			if status != "completed" || !current.Equal(reclaimed) {
				t.Fatalf("settled row changed: status=%s generation=%s", status, current)
			}
			if !json.Valid(result) || !strings.Contains(string(result), "result of the new claim") {
				t.Fatalf("settled result was overwritten: %s", result)
			}
		})

		t.Run("cancelled row keeps its outcome", func(t *testing.T) {
			taskID, generation := fencedTerminalTask(t, "running", nil)
			if _, err := testPool.Exec(context.Background(),
				"UPDATE agent_task_queue SET status = 'cancelled', completed_at = now() WHERE id = $1", taskID); err != nil {
				t.Fatalf("cancel task: %v", err)
			}
			w := postFencedTerminalCallback(t, taskID, "complete", map[string]any{
				"output":                 "late result",
				"expected_dispatched_at": generation.UTC().Format(time.RFC3339Nano),
			})
			if w.Code != http.StatusOK {
				t.Fatalf("cancelled replay: got %d: %s, want idempotent 200", w.Code, w.Body.String())
			}
			if status, _, _, _ := taskRowState(t, taskID); status != "cancelled" {
				t.Fatalf("status = %q, want cancelled", status)
			}
		})

		t.Run("rejects an absent or malformed generation", func(t *testing.T) {
			taskID, _ := fencedTerminalTask(t, "running", nil)
			if w := postFencedTerminalCallback(t, taskID, "complete", map[string]any{"output": "done"}); w.Code != http.StatusBadRequest {
				t.Fatalf("missing generation: got %d: %s, want 400", w.Code, w.Body.String())
			}
			if w := postFencedTerminalCallback(t, taskID, "complete", map[string]any{
				"output":                 "done",
				"expected_dispatched_at": "yesterday",
			}); w.Code != http.StatusBadRequest {
				t.Fatalf("malformed generation: got %d: %s, want 400", w.Code, w.Body.String())
			}
			if status, _, _, _ := taskRowState(t, taskID); status != "running" {
				t.Fatalf("status = %q, want running (untouched)", status)
			}
		})
	})

	t.Run("fail", func(t *testing.T) {
		for _, status := range []string{"dispatched", "running", "waiting_local_directory"} {
			t.Run("matching generation fails a "+status+" task", func(t *testing.T) {
				taskID, generation := fencedTerminalTask(t, status, nil)
				w := postFencedTerminalCallback(t, taskID, "fail", map[string]any{
					"error":                  "provider failed",
					"failure_reason":         "agent_error.process_failure",
					"expected_dispatched_at": generation.UTC().Format(time.RFC3339Nano),
				})
				if w.Code != http.StatusOK {
					t.Fatalf("fenced fail: got %d: %s", w.Code, w.Body.String())
				}
				if got, _, _, _ := taskRowState(t, taskID); got != "failed" {
					t.Fatalf("status = %q, want failed", got)
				}
			})
		}

		t.Run("stale generation conflicts and leaves the reclaim alone", func(t *testing.T) {
			taskID, generation := fencedTerminalTask(t, "dispatched", nil)
			reclaimed := reclaimDispatchedTask(t, taskID)
			w := postFencedTerminalCallback(t, taskID, "fail", map[string]any{
				"error":                  "old claim failed",
				"expected_dispatched_at": generation.UTC().Format(time.RFC3339Nano),
			})
			if w.Code != http.StatusConflict {
				t.Fatalf("stale fenced fail: got %d: %s, want 409", w.Code, w.Body.String())
			}
			status, current, result, completedAt := taskRowState(t, taskID)
			if status != "dispatched" || !current.Equal(reclaimed) || result != nil || completedAt != nil {
				t.Fatalf("reclaim was mutated: status=%s generation=%s result=%s", status, current, result)
			}
		})

		t.Run("rejects an absent or malformed generation", func(t *testing.T) {
			taskID, _ := fencedTerminalTask(t, "dispatched", nil)
			if w := postFencedTerminalCallback(t, taskID, "fail", map[string]any{"error": "failed"}); w.Code != http.StatusBadRequest {
				t.Fatalf("missing generation: got %d: %s, want 400", w.Code, w.Body.String())
			}
			if w := postFencedTerminalCallback(t, taskID, "fail", map[string]any{
				"error":                  "failed",
				"expected_dispatched_at": "yesterday",
			}); w.Code != http.StatusBadRequest {
				t.Fatalf("malformed generation: got %d: %s, want 400", w.Code, w.Body.String())
			}
			if got, _, _, _ := taskRowState(t, taskID); got != "dispatched" {
				t.Fatalf("status = %q, want dispatched (untouched)", got)
			}
		})
	})

	// The daemon must tell a real absence from a replica that has no fenced
	// route, so a missing task answers with the stable code instead of the
	// ordinary unstructured 404 an unknown path produces.
	t.Run("missing task carries the structured code", func(t *testing.T) {
		missingTaskID := "00000000-0000-0000-0000-0000000000ff"
		for _, endpoint := range []string{"complete", "fail"} {
			body := map[string]any{
				"error":                  "gone",
				"expected_dispatched_at": time.Now().UTC().Format(time.RFC3339Nano),
			}
			w := postFencedTerminalCallback(t, missingTaskID, endpoint, body)
			if w.Code != http.StatusNotFound {
				t.Fatalf("fenced %s for a missing task: got %d: %s, want 404", endpoint, w.Code, w.Body.String())
			}
			if code := responseCode(t, w); code != protocol.DaemonTaskNotFoundCode {
				t.Fatalf("missing-task code = %q, want %q", code, protocol.DaemonTaskNotFoundCode)
			}
		}
	})
}

// TestLegacyTerminalEndpointsStayCompatible is the only legacy generation
// concern left: an installed daemon that predates the fence keeps working
// without any generation field. Generation semantics belong to the versioned
// route above.
func TestLegacyTerminalEndpointsStayCompatible(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("complete without a generation settles", func(t *testing.T) {
		taskID, _ := fencedTerminalTask(t, "running", nil)
		if w := postTerminalCallback(t, taskID, "complete", map[string]any{"output": "old daemon"}); w.Code != http.StatusOK {
			t.Fatalf("legacy complete: got %d: %s", w.Code, w.Body.String())
		}
		if status, _, _, _ := taskRowState(t, taskID); status != "completed" {
			t.Fatalf("status = %q, want completed", status)
		}
	})

	t.Run("fail without a generation settles", func(t *testing.T) {
		taskID, _ := fencedTerminalTask(t, "running", nil)
		if w := postTerminalCallback(t, taskID, "fail", map[string]any{"error": "old daemon"}); w.Code != http.StatusOK {
			t.Fatalf("legacy fail: got %d: %s", w.Code, w.Body.String())
		}
		if status, _, _, _ := taskRowState(t, taskID); status != "failed" {
			t.Fatalf("status = %q, want failed", status)
		}
	})
}

// TestClaimPayloadRoundTripsClaimGeneration pins the other half of the fence:
// the generation the daemon echoes must be the one the claim payload carried.
// A claim payload truncated to seconds would make every fenced callback miss
// its own claim whenever the reclaim or the callback landed in a later second.
func TestClaimPayloadRoundTripsClaimGeneration(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := dbfx.Runtime(t, "claim generation runtime", nil)
	agentID := dbfx.Agent(t, "claim generation agent", runtimeID)

	seedQueuedTask := func(t *testing.T, label string) string {
		t.Helper()
		return dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID,
			"issue_id":   dbfx.Issue(t, "claim generation "+label),
			"status":     "queued",
		})
	}
	// Both claim paths must advertise the fence capability AND round-trip the
	// generation exactly: the daemon can only echo what it actually received, and
	// a claim that advertises fencing without a usable timestamp would be a
	// contract violation.
	assertClaim := func(t *testing.T, taskID, dispatchedAt string, fence bool) {
		t.Helper()
		if !fence {
			t.Fatal("claim did not advertise terminal_report_generation_fence_v1")
		}
		var stored time.Time
		if err := testPool.QueryRow(ctx, `SELECT dispatched_at FROM agent_task_queue WHERE id = $1`, taskID).Scan(&stored); err != nil {
			t.Fatalf("read claimed generation: %v", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, dispatchedAt)
		if err != nil {
			t.Fatalf("claim dispatched_at %q is not RFC3339Nano: %v", dispatchedAt, err)
		}
		if !parsed.Equal(stored) {
			t.Fatalf("claim dispatched_at = %s, stored = %s: the daemon cannot echo what it never received",
				parsed.UTC(), stored.UTC())
		}
		if truncated := stored.Truncate(time.Second); !truncated.Equal(stored) && parsed.Equal(truncated) {
			t.Fatalf("claim dispatched_at = %s lost the sub-second precision of %s", parsed.UTC(), stored.UTC())
		}
	}

	t.Run("batch claim", func(t *testing.T) {
		taskID := seedQueuedTask(t, "batch")
		w := postBatchClaim(t, testWorkspaceID, []string{runtimeID}, 1)
		if w.Code != http.StatusOK {
			t.Fatalf("batch claim: got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Tasks []struct {
				ID              string `json:"id"`
				DispatchedAt    string `json:"dispatched_at"`
				GenerationFence bool   `json:"terminal_report_generation_fence_v1"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode claim response: %v", err)
		}
		if len(resp.Tasks) != 1 || resp.Tasks[0].ID != taskID {
			t.Fatalf("claim returned %+v, want task %s", resp.Tasks, taskID)
		}
		assertClaim(t, taskID, resp.Tasks[0].DispatchedAt, resp.Tasks[0].GenerationFence)
	})

	t.Run("single runtime claim", func(t *testing.T) {
		taskID := seedQueuedTask(t, "single")
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
			testWorkspaceID, terminalFenceDaemonID)
		req = withURLParam(req, "runtimeId", runtimeID)
		w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
		var resp struct {
			Task *struct {
				ID              string `json:"id"`
				DispatchedAt    string `json:"dispatched_at"`
				GenerationFence bool   `json:"terminal_report_generation_fence_v1"`
			} `json:"task"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode claim response: %v", err)
		}
		if resp.Task == nil || resp.Task.ID != taskID {
			t.Fatalf("claim returned %+v, want task %s", resp.Task, taskID)
		}
		assertClaim(t, taskID, resp.Task.DispatchedAt, resp.Task.GenerationFence)
	})
}
