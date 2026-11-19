package handler

import (
	"context"
	"encoding/json"
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

func TestHeartbeatOffersUpdatesAsDeferrable(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	store := withUpdateStore(t)
	req := mustCreateUpdate(t, store, testRuntimeID)

	ack, _, err := testHandler.processHeartbeat(context.Background(), testRuntimeID, true)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if ack.PendingUpdate == nil || ack.PendingUpdate.ID != req.ID {
		t.Fatalf("pending update = %+v, want %s", ack.PendingUpdate, req.ID)
	}
	if !ack.PendingUpdate.Deferrable {
		t.Fatal("offer must be deferrable so new daemons queue instead of failing")
	}
}
