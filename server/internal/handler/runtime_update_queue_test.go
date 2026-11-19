package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// withUpdateStore gives one test a fresh in-memory update store.
func withUpdateStore(t *testing.T) UpdateStore {
	t.Helper()
	orig := testHandler.UpdateStore
	store := NewInMemoryUpdateStore()
	testHandler.UpdateStore = store
	t.Cleanup(func() { testHandler.UpdateStore = orig })
	return store
}

// reportUpdate drives ReportUpdateResult as the fixture runtime's daemon.
func reportUpdate(t *testing.T, updateID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := withURLParams(
		newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+testRuntimeID+"/update/"+updateID+"/result", body),
		"runtimeId", testRuntimeID,
		"updateId", updateID,
	)
	w := httptest.NewRecorder()
	testHandler.ReportUpdateResult(w, req)
	return w
}

func TestReportUpdateResult_LegacyBusyFailureStaysQueued(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)

	// What a daemon released before deferrable updates sends for a busy offer.
	w := reportUpdate(t, req.ID, map[string]any{"status": "failed", "error": protocol.RuntimeUpdateBusyError})
	if w.Code != http.StatusOK {
		t.Fatalf("report = %d: %s", w.Code, w.Body.String())
	}

	got := mustGetUpdate(t, store, req.ID)
	if got.Status != UpdateScheduled || got.Error != "" {
		t.Fatalf("update = %+v, want scheduled without an error", got)
	}
	if got.ActiveTasks != nil {
		t.Fatalf("active tasks = %d; an old daemon does not report a count", *got.ActiveTasks)
	}
}

func TestReportUpdateResult_ScheduledRecordsActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)

	w := reportUpdate(t, req.ID, map[string]any{"status": "scheduled", "active_tasks": 3})
	if w.Code != http.StatusOK {
		t.Fatalf("report = %d: %s", w.Code, w.Body.String())
	}

	got := mustGetUpdate(t, store, req.ID)
	if got.Status != UpdateScheduled || got.ActiveTasks == nil || *got.ActiveTasks != 3 {
		t.Fatalf("update = %+v, want scheduled with 3 active tasks", got)
	}
}

func TestReportUpdateResult_OtherFailuresStillFail(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)

	w := reportUpdate(t, req.ID, map[string]any{"status": "failed", "error": "brew upgrade failed: exit status 1"})
	if w.Code != http.StatusOK {
		t.Fatalf("report = %d: %s", w.Code, w.Body.String())
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateFailed || got.Error != "brew upgrade failed: exit status 1" {
		t.Fatalf("update = %+v, want failed", got)
	}
}

func TestReportUpdateResult_RejectsStartingACancelledUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)
	if _, err := store.Cancel(context.Background(), req.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	for _, status := range []string{"running", "scheduled"} {
		if w := reportUpdate(t, req.ID, map[string]any{"status": status}); w.Code != http.StatusConflict {
			t.Fatalf("%s report = %d, want 409 so the daemon drops it: %s", status, w.Code, w.Body.String())
		}
	}
	// A late terminal report is still accepted and ignored.
	if w := reportUpdate(t, req.ID, map[string]any{"status": "completed", "output": "Updated"}); w.Code != http.StatusOK {
		t.Fatalf("late completed report = %d: %s", w.Code, w.Body.String())
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
}

func TestReportUpdateResult_RunningStartsInstallation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)

	if w := reportUpdate(t, req.ID, map[string]any{"status": "running"}); w.Code != http.StatusOK {
		t.Fatalf("running report = %d: %s", w.Code, w.Body.String())
	}
	got := mustGetUpdate(t, store, req.ID)
	if got.Status != UpdateRunning || got.RunStartedAt == nil {
		t.Fatalf("update = %+v, want running with a start time", got)
	}
}

func TestCancelUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	cancel := func(updateID string) *httptest.ResponseRecorder {
		req := withURLParams(
			newRequest(http.MethodPost, "/api/runtimes/"+testRuntimeID+"/update/"+updateID+"/cancel", nil),
			"runtimeId", testRuntimeID,
			"updateId", updateID,
		)
		w := httptest.NewRecorder()
		testHandler.CancelUpdate(w, req)
		return w
	}

	queued := mustCreateUpdate(t, store, testRuntimeID)
	if _, err := store.Schedule(context.Background(), queued.ID, nil); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	w := cancel(queued.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel queued = %d: %s", w.Code, w.Body.String())
	}
	var body UpdateRequest
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != UpdateCancelled {
		t.Fatalf("status = %s, want cancelled", body.Status)
	}

	installing := mustCreateUpdate(t, store, testRuntimeID)
	if _, err := store.MarkRunning(context.Background(), installing.ID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if w := cancel(installing.ID); w.Code != http.StatusConflict {
		t.Fatalf("cancel installing = %d, want 409: %s", w.Code, w.Body.String())
	}

	if w := cancel("missing"); w.Code != http.StatusNotFound {
		t.Fatalf("cancel missing = %d, want 404", w.Code)
	}
}

func TestGetActiveUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	getActive := func() *UpdateRequest {
		t.Helper()
		req := withURLParam(
			newRequest(http.MethodGet, "/api/runtimes/"+testRuntimeID+"/update", nil),
			"runtimeId", testRuntimeID,
		)
		w := httptest.NewRecorder()
		testHandler.GetActiveUpdate(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get active = %d: %s", w.Code, w.Body.String())
		}
		var body struct {
			Update *UpdateRequest `json:"update"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.Update
	}

	if got := getActive(); got != nil {
		t.Fatalf("active = %+v, want none", got)
	}
	req := mustCreateUpdate(t, store, testRuntimeID)
	if _, err := store.Schedule(context.Background(), req.ID, nil); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	got := getActive()
	if got == nil || got.ID != req.ID || got.Status != UpdateScheduled {
		t.Fatalf("active = %+v, want the queued request", got)
	}
}

func TestHeartbeatOffersDeferrableOnlyWhenBothSidesQueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	tests := []struct {
		name           string
		queueWhenBusy  bool
		daemonDefers   bool
		wantDeferrable bool
	}{
		{name: "new client, new daemon", queueWhenBusy: true, daemonDefers: true, wantDeferrable: true},
		{name: "new client, old daemon", queueWhenBusy: true, daemonDefers: false},
		{name: "old client, new daemon", queueWhenBusy: false, daemonDefers: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := withUpdateStore(t)
			req, err := store.Create(context.Background(), testRuntimeID, "v1.2.3", "user-1", tc.queueWhenBusy)
			if err != nil {
				t.Fatalf("create: %v", err)
			}

			ack, _, err := testHandler.processHeartbeat(context.Background(), testRuntimeID,
				protocol.DaemonHeartbeatRequestPayload{RuntimeID: testRuntimeID, SupportsDeferredUpdate: tc.daemonDefers})
			if err != nil {
				t.Fatalf("heartbeat: %v", err)
			}
			if ack.PendingUpdate == nil || ack.PendingUpdate.ID != req.ID {
				t.Fatalf("pending update = %+v, want %s", ack.PendingUpdate, req.ID)
			}
			if ack.PendingUpdate.Deferrable != tc.wantDeferrable {
				t.Fatalf("deferrable = %v, want %v", ack.PendingUpdate.Deferrable, tc.wantDeferrable)
			}

			// A daemon on the fail-fast path installs even when its "running"
			// report is refused, so the request must not be cancellable while
			// that offer is out.
			_, err = store.Cancel(context.Background(), req.ID)
			if tc.wantDeferrable && err != nil {
				t.Fatalf("cancel after a deferrable offer: %v", err)
			}
			if !tc.wantDeferrable && !errors.Is(err, errUpdateBeingDelivered) {
				t.Fatalf("cancel during a fail-fast offer = %v, want errUpdateBeingDelivered", err)
			}
		})
	}
}

func TestReportUpdateResult_BusyFailureFailsForClientsThatCannotQueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req, err := store.Create(context.Background(), testRuntimeID, "v1.2.3", "user-1", false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if w := reportUpdate(t, req.ID, map[string]any{"status": "failed", "error": protocol.RuntimeUpdateBusyError}); w.Code != http.StatusOK {
		t.Fatalf("report = %d: %s", w.Code, w.Body.String())
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateFailed {
		t.Fatalf("status = %s; an installed client that cannot show a queue must keep the failure", got.Status)
	}
}

func TestActiveUpdateIsFoundThroughAnyRuntimeOfTheMachine(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	store := withUpdateStore(t)
	daemonID := "mul-9633-machine-" + randomID()
	var runtimeA, runtimeB string
	for i, dst := range []*string{&runtimeA, &runtimeB} {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
			)
			VALUES ($1, $2, $3, 'local', $4, 'online', 'machine test', '{}'::jsonb, $5, now())
			RETURNING id
		`, testWorkspaceID, daemonID, "Machine Runtime", []string{"machine_test_a", "machine_test_b"}[i], testUserID).Scan(dst); err != nil {
			t.Fatalf("create runtime: %v", err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE daemon_id = $1`, daemonID)
	})

	queued, err := store.Create(ctx, runtimeA, "v1.2.3", testUserID, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// The machine card now uses runtime B (A went offline); it still finds A's update.
	req := withURLParam(newRequest(http.MethodGet, "/api/runtimes/"+runtimeB+"/update", nil), "runtimeId", runtimeB)
	w := httptest.NewRecorder()
	testHandler.GetActiveUpdate(w, req)
	var body struct {
		Update *UpdateRequest `json:"update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %d %s: %v", w.Code, w.Body.String(), err)
	}
	if body.Update == nil || body.Update.ID != queued.ID || body.Update.RuntimeID != runtimeA {
		t.Fatalf("active = %+v, want the update queued through runtime A", body.Update)
	}

	// And a second update for the same machine is refused.
	initReq := withURLParam(newRequest(http.MethodPost, "/api/runtimes/"+runtimeB+"/update", map[string]any{
		"target_version":  "v1.2.3",
		"queue_when_busy": true,
	}), "runtimeId", runtimeB)
	initW := httptest.NewRecorder()
	testHandler.InitiateUpdate(initW, initReq)
	if initW.Code != http.StatusConflict {
		t.Fatalf("second update on the machine = %d, want 409: %s", initW.Code, initW.Body.String())
	}
}
