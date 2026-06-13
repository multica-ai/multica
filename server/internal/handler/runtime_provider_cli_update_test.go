package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReportProviderCLIUpdateResultRejectsLateReportAfterTimeout(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryProviderCLIUpdateStore()
	oldStore := testHandler.ProviderCLIUpdateStore
	testHandler.ProviderCLIUpdateStore = store
	t.Cleanup(func() { testHandler.ProviderCLIUpdateStore = oldStore })

	req, err := store.Create(ctx, testRuntimeID, "opencode", "apply", "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	claimed, err := store.PopPending(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if claimed == nil || claimed.Status != UpdateRunning {
		t.Fatalf("expected running update, got %+v", claimed)
	}
	aged := time.Now().Add(-(updateRunningTimeout + time.Second))
	claimed.RunStartedAt = &aged
	if got, err := store.Get(ctx, req.ID); err != nil {
		t.Fatalf("get: %v", err)
	} else if got.Status != UpdateTimeout {
		t.Fatalf("status = %s, want timeout", got.Status)
	}

	w := httptest.NewRecorder()
	reportReq := withURLParams(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+testRuntimeID+"/provider-cli-update/"+req.ID+"/result", map[string]any{
			"status": "completed",
			"output": `{"status":"pending_verify"}`,
		}, testWorkspaceID, "provider-cli-update-daemon"),
		"runtimeId", testRuntimeID,
		"updateId", req.ID,
	)
	testHandler.ReportProviderCLIUpdateResult(w, reportReq)
	if w.Code != http.StatusConflict {
		t.Fatalf("ReportProviderCLIUpdateResult: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get after report: %v", err)
	}
	if got.Status != UpdateTimeout {
		t.Fatalf("late completed report changed status to %s, want timeout", got.Status)
	}
}
