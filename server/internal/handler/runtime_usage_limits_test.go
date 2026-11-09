package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func reportUsageLimits(t *testing.T, runtimeID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/usage-limits", body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	testHandler.ReportRuntimeUsageLimits(w, req)
	return w
}

func runtimeUsageLimits(t *testing.T, runtimeID string) (map[string]any, map[string]any) {
	t.Helper()
	rt, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(rt.Metadata, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	usage, _ := metadata["usage_limits"].(map[string]any)
	return metadata, usage
}

func TestReportRuntimeUsageLimitsSurvivesRegistration(t *testing.T) {
	ctx := context.Background()
	register := func(metadata string) string {
		t.Helper()
		row, err := testHandler.Queries.UpsertAgentRuntime(ctx, db.UpsertAgentRuntimeParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			DaemonID:    strToText("usage-limits-daemon"),
			Name:        "Claude (usage limits)",
			RuntimeMode: "local",
			Provider:    "claude",
			Status:      "online",
			DeviceInfo:  "usage-limits-host",
			Metadata:    []byte(metadata),
			OwnerID:     parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("register runtime: %v", err)
		}
		return uuidToString(row.ID)
	}
	runtimeID := register(`{"version":"2.1.318"}`)
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteAgentRuntime(context.Background(), parseUUID(runtimeID))
	})

	snapshot := map[string]any{
		"provider":   "claude",
		"account_id": "a1b2c3d4e5f60718",
		"plan":       "max",
		"device":     "usage-limits-host",
		"windows": []map[string]any{
			{"id": "five_hour", "window_minutes": 300, "used_percent": 42.5, "resets_at": "2026-11-09T18:00:00Z"},
			{"id": "seven_day", "window_minutes": 10080, "used_percent": 12, "resets_at": "2026-11-16T06:00:00Z"},
		},
		"updated_at": "2026-11-09T14:00:00Z",
		"checked_at": "2026-11-09T14:00:00Z",
		"source":     "probe",
	}
	if w := reportUsageLimits(t, runtimeID, snapshot); w.Code != http.StatusNoContent {
		t.Fatalf("report: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	_, usage := runtimeUsageLimits(t, runtimeID)
	windows, _ := usage["windows"].([]any)
	if usage["account_id"] != "a1b2c3d4e5f60718" || len(windows) != 2 {
		t.Fatalf("stored usage limits = %#v", usage)
	}

	// Re-registration replaces the rest of metadata but keeps the limits,
	// which describe the login rather than the registration.
	if again := register(`{"version":"2.1.319"}`); again != runtimeID {
		t.Fatalf("re-registration returned %s, want %s", again, runtimeID)
	}
	metadata, usage := runtimeUsageLimits(t, runtimeID)
	if metadata["version"] != "2.1.319" {
		t.Fatalf("registration metadata not applied: %#v", metadata)
	}
	if usage["plan"] != "max" {
		t.Fatalf("usage limits lost on registration: %#v", metadata)
	}
}

func TestReportRuntimeUsageLimitsRejectsInvalidSnapshots(t *testing.T) {
	_, before := runtimeUsageLimits(t, testRuntimeID)
	cases := []struct {
		name string
		body any
	}{
		{name: "provider_mismatch", body: map[string]any{"provider": "codex", "windows": []any{}}},
		{name: "too_many_windows", body: map[string]any{"provider": "handler_test_runtime", "windows": make([]map[string]any, maxUsageLimitsWindows+1)}},
		{name: "not_an_object", body: []string{"x"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if w := reportUsageLimits(t, testRuntimeID, tt.body); w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	if _, after := runtimeUsageLimits(t, testRuntimeID); after != nil && before == nil {
		t.Fatalf("rejected report was stored: %#v", after)
	}
}

func TestRefreshRuntimeUsageLimitsSendsHint(t *testing.T) {
	notifier := &pendingWorkRecorder{}
	original := testHandler.DaemonPendingWork
	testHandler.DaemonPendingWork = notifier
	t.Cleanup(func() { testHandler.DaemonPendingWork = original })

	req := newRequest(http.MethodPost, "/api/runtimes/"+testRuntimeID+"/usage-limits/refresh", nil)
	req = withURLParam(req, "runtimeId", testRuntimeID)
	w := httptest.NewRecorder()
	testHandler.RefreshRuntimeUsageLimits(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if len(notifier.hints) != 1 || notifier.hints[0] != testRuntimeID+":usage_limits" {
		t.Fatalf("hints = %v, want one usage_limits hint for the runtime", notifier.hints)
	}
}
