package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
)

func ptrInt32(value int32) *int32 { return &value }

func TestValidateClientUsageRuntime(t *testing.T) {
	tests := []struct {
		name    string
		probe   clientUsageRuntimeProbe
		wantErr bool
	}{
		{name: "probe error", probe: clientUsageRuntimeProbe{ProbeResult: "error"}},
		{name: "successful empty config", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(0), ProviderSummary: map[string]int{}, OnlineCount: ptrInt32(0), OfflineCount: ptrInt32(0)}},
		{name: "successful mixed config", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(3), ProviderSummary: map[string]int{"claude": 2, "codex": 1}, OnlineCount: ptrInt32(1), OfflineCount: ptrInt32(2)}},
		{name: "error with counts", probe: clientUsageRuntimeProbe{ProbeResult: "error", RuntimeCount: ptrInt32(0)}, wantErr: true},
		{name: "missing success fields", probe: clientUsageRuntimeProbe{ProbeResult: "success"}, wantErr: true},
		{name: "state total mismatch", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(2), ProviderSummary: map[string]int{"codex": 2}, OnlineCount: ptrInt32(1), OfflineCount: ptrInt32(0)}, wantErr: true},
		{name: "provider total mismatch", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(2), ProviderSummary: map[string]int{"codex": 1}, OnlineCount: ptrInt32(1), OfflineCount: ptrInt32(1)}, wantErr: true},
		{name: "unsafe provider", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(1), ProviderSummary: map[string]int{"Codex Pro": 1}, OnlineCount: ptrInt32(1), OfflineCount: ptrInt32(0)}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateClientUsageRuntime(tt.probe)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateClientUsageRuntime() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeClientUsageOS(t *testing.T) {
	if got := normalizeClientUsageOS(" MacOS "); got != "macos" {
		t.Fatalf("normalizeClientUsageOS() = %q, want macos", got)
	}
	if got := normalizeClientUsageOS("Darwin 24.4"); got != "unknown" {
		t.Fatalf("normalizeClientUsageOS() = %q, want unknown", got)
	}
}

func TestUpsertClientUsageKeepsRuntimeSnapshotOnActivityRefresh(t *testing.T) {
	const installID = "8d98d7db-4d40-4505-bc49-16b76db32721"
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM client_usage_daily WHERE user_id = $1 AND install_id = $2`, testUserID, installID)
	})

	report := func(body any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newRequest(http.MethodPost, "/api/client-usage", body)
		req.Header.Set("X-Client-Platform", "desktop")
		req.Header.Set("X-Client-Version", "0.1.0")
		req.Header.Set("X-Client-OS", "macos")
		req = req.WithContext(middleware.SetClientMetadata(req.Context(), "desktop", "0.1.0", "macos"))
		testHandler.UpsertClientUsage(w, req)
		return w
	}

	w := report(map[string]any{
		"install_id": installID,
		"runtime": map[string]any{
			"probe_result": "success", "runtime_count": 1,
			"provider_summary": map[string]int{"codex": 1},
			"online_count":     1, "offline_count": 0,
		},
	})
	if w.Code != http.StatusNoContent {
		t.Fatalf("runtime report status = %d: %s", w.Code, w.Body.String())
	}
	if w = report(map[string]any{"install_id": installID}); w.Code != http.StatusNoContent {
		t.Fatalf("activity refresh status = %d: %s", w.Code, w.Body.String())
	}

	var rowCount, runtimeCount, onlineCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*), max(runtime_count), max(online_count)
		FROM client_usage_daily
		WHERE user_id = $1 AND client_type = 'desktop' AND install_id = $2
	`, testUserID, installID).Scan(&rowCount, &runtimeCount, &onlineCount); err != nil {
		t.Fatal(err)
	}
	if rowCount != 1 || runtimeCount != 1 || onlineCount != 1 {
		t.Fatalf("daily row = count %d runtime %d online %d", rowCount, runtimeCount, onlineCount)
	}
}
