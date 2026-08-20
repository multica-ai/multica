package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
)

func TestNormalizeClientUsageOS(t *testing.T) {
	if got := normalizeClientUsageOS(" MacOS "); got != "macos" {
		t.Fatalf("normalizeClientUsageOS() = %q, want macos", got)
	}
	if got := normalizeClientUsageOS("Darwin 24.4"); got != "unknown" {
		t.Fatalf("normalizeClientUsageOS() = %q, want unknown", got)
	}
}

func TestUpsertClientUsageRecordsWebActivity(t *testing.T) {
	const installID = "8d98d7db-4d40-4505-bc49-16b76db32721"
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM client_usage_daily WHERE user_id = $1 AND install_id = $2`, testUserID, installID)
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/client-usage", map[string]any{"install_id": installID})
	req = req.WithContext(middleware.SetClientMetadata(req.Context(), "web", "0.1.0", "macos"))
	testHandler.UpsertClientUsage(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("web activity status = %d: %s", w.Code, w.Body.String())
	}

	var rowCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM client_usage_daily
		WHERE user_id = $1 AND client_type = 'web' AND install_id = $2
	`, testUserID, installID).Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if rowCount != 1 {
		t.Fatalf("daily row count = %d, want 1", rowCount)
	}
}

func TestUpsertClientUsageRejectsRetiredRuntimePayload(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/client-usage", map[string]any{
		"install_id": "8d98d7db-4d40-4505-bc49-16b76db32721",
		"runtime":    map[string]any{"probe_result": "success"},
	})
	req = req.WithContext(middleware.SetClientMetadata(req.Context(), "web", "0.1.0", "macos"))
	testHandler.UpsertClientUsage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("retired runtime payload status = %d, want 400", w.Code)
	}
}
