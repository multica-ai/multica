package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// Lark-handler unit tests focus on the no-config short-circuits —
// verifying that a self-host deployment without MULTICA_LARK_SECRET_KEY
// does NOT serve revoke / redeem / install, and that list degrades
// gracefully to an empty response so the Integrations tab still
// renders. Happy-path flows (begin device-flow + poll status; token
// mint + redeem) need a real DB and land alongside the WS hub
// integration tests in a follow-up commit.

func TestRevokeLarkInstallation_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/x/lark/installations/y", nil)
	w := httptest.NewRecorder()
	h.RevokeLarkInstallation(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestRedeemLarkBindingToken_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/lark/binding/redeem", strings.NewReader(`{"token":"x"}`))
	w := httptest.NewRecorder()
	h.RedeemLarkBindingToken(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestBeginLarkInstall_NotConfigured(t *testing.T) {
	// When the device-flow registration service is nil (no at-rest
	// key, or the stub APIClient is the only one wired), the begin
	// endpoint must short-circuit to 503 — silently returning a
	// "configured: false" envelope would hide a real misconfiguration
	// from the operator. The UI hides the bind button in that case
	// so this should not be reached through the normal flow.
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/x/lark/install/begin?agent_id=y", nil)
	w := httptest.NewRecorder()
	h.BeginLarkInstall(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetLarkInstallStatus_NotConfigured(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/lark/install/sess_y/status", nil)
	w := httptest.NewRecorder()
	h.GetLarkInstallStatus(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestListLarkInstallations_NotConfiguredReturnsEmpty(t *testing.T) {
	// Listing is intentionally a "soft" endpoint: when lark is not
	// configured we return an empty list + configured:false rather
	// than a 503, so the Integrations tab renders normally with a
	// "not connected" empty state instead of an error banner.
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/lark/installations", nil)
	w := httptest.NewRecorder()
	h.ListLarkInstallations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Installations    []any `json:"installations"`
		Configured       bool  `json:"configured"`
		InstallSupported bool  `json:"install_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Configured {
		t.Fatalf("configured should be false when LarkInstallations is nil")
	}
	if resp.InstallSupported {
		t.Fatalf("install_supported should be false when LarkInstallations is nil")
	}
	if len(resp.Installations) != 0 {
		t.Fatalf("expected empty installations list, got %d", len(resp.Installations))
	}
}

// TestListLarkInstallations_StubClientReportsInstallNotSupported pins
// the front-half of the "don't expose a doomed install flow"
// guarantee: even when the at-rest key + registration service are set,
// install_supported flips false if the underlying APIClient is the
// stub. The stub cannot complete the post-poll GetBotInfo call that
// finalizes a device-flow install, so the UI must hide install entry
// points until a real client is wired.
func TestListLarkInstallations_StubClientReportsInstallNotSupported(t *testing.T) {
	stubLogger := slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil))
	h := &Handler{
		LarkAPIClient: lark.NewStubAPIClient(stubLogger),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/lark/installations", nil)
	w := httptest.NewRecorder()
	h.ListLarkInstallations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Configured       bool `json:"configured"`
		InstallSupported bool `json:"install_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.InstallSupported {
		t.Fatalf("install_supported must be false while only stub APIClient is wired")
	}
}

// TestListLarkInstallations_NotConfigured_HardCodedInstallSupportedFalse
// pins the invariant for the early-return branch: when
// LarkInstallations is nil (the deployment has no at-rest encryption
// key wired), the response MUST return both configured:false AND
// install_supported:false regardless of what APIClient is in place.
// A real APIClient on a not-configured deployment must not flip
// install_supported via the APIClient path — that path is not
// consulted in the early-return branch.
func TestListLarkInstallations_NotConfigured_HardCodedInstallSupportedFalse(t *testing.T) {
	stubLogger := slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil))
	h := &Handler{
		LarkInstallations: nil, // triggers the not-configured early return.
		LarkAPIClient:     lark.NewStubAPIClient(stubLogger),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/x/lark/installations", nil)
	w := httptest.NewRecorder()
	h.ListLarkInstallations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Configured       bool `json:"configured"`
		InstallSupported bool `json:"install_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Configured {
		t.Fatalf("configured must be false when LarkInstallations is nil")
	}
	if resp.InstallSupported {
		t.Fatalf("install_supported must be false in the early-return branch even with a non-nil APIClient")
	}
}

func TestListLarkInstallations_NotificationEventTypesField(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var workspaceID, userID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4) RETURNING id
`, "Lark List Events", "lark-list-events-"+suffix, "handler list test", "LLE").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "Lark List User", "lark-list-"+suffix+"@multica.ai").Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO lark_installation (
    workspace_id, app_id, app_secret_encrypted, bot_open_id,
    installer_user_id, installation_kind, notification_event_types
) VALUES ($1, $2, $3, $4, $5, 'notification', $6)
`, workspaceID, "cli_list_"+suffix, []byte("encrypted"), "ou_list_"+suffix, userID, []string{"mentioned", "task_failed"}); err != nil {
		t.Fatalf("create notification installation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatalf("create secret box: %v", err)
	}
	installationService, err := lark.NewInstallationService(testHandler.Queries, box)
	if err != nil {
		t.Fatalf("create installation service: %v", err)
	}
	h := &Handler{LarkInstallations: installationService}
	router := chi.NewRouter()
	router.Get("/api/workspaces/{id}/lark/installations", h.ListLarkInstallations)
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/lark/installations", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Installations []LarkInstallationResponse `json:"installations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Installations) != 1 {
		t.Fatalf("installations length = %d, want 1", len(body.Installations))
	}
	want := []string{"mentioned", "task_failed"}
	if fmt.Sprint(body.Installations[0].NotificationEventTypes) != fmt.Sprint(want) {
		t.Fatalf("notification_event_types = %v, want %v", body.Installations[0].NotificationEventTypes, want)
	}
}

func TestUpdateLarkNotificationEvents(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "Lark Notification Events", "lark-notification-events-"+suffix, "handler tests", "LNE").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	users := make(map[string]string)
	for _, role := range []string{"owner", "admin", "member"} {
		var userID string
		if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "Lark "+role, fmt.Sprintf("lark-%s-%s@multica.ai", role, suffix)).Scan(&userID); err != nil {
			t.Fatalf("create %s user: %v", role, err)
		}
		users[role] = userID
		if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)
`, workspaceID, userID, role); err != nil {
			t.Fatalf("create %s membership: %v", role, err)
		}
	}

	var installationID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO lark_installation (
    workspace_id, app_id, app_secret_encrypted, bot_open_id,
    installer_user_id, installation_kind, notification_event_types
) VALUES ($1, $2, $3, $4, $5, 'notification', $6)
RETURNING id
`, workspaceID, "cli_"+suffix, []byte("encrypted"), "ou_"+suffix, users["owner"], []string{"mentioned"}).Scan(&installationID); err != nil {
		t.Fatalf("create notification installation: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		for _, userID := range users {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
		}
	})

	router := chi.NewRouter()
	router.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
			r.Put("/lark/notification-events", testHandler.UpdateLarkNotificationEvents)
		})
	})

	exercise := func(t *testing.T, role, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID+"/lark/notification-events", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", users[role])
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	loadPersisted := func(t *testing.T) []string {
		t.Helper()
		var eventTypes []string
		if err := testPool.QueryRow(ctx, `
SELECT notification_event_types FROM lark_installation WHERE id = $1
`, installationID).Scan(&eventTypes); err != nil {
			t.Fatalf("load persisted notification event types: %v", err)
		}
		return eventTypes
	}

	for _, role := range []string{"owner", "admin"} {
		t.Run(role+" success", func(t *testing.T) {
			rec := exercise(t, role, `{"event_types":["task_failed","mentioned","mentioned"]}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var got LarkInstallationResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			want := []string{"mentioned", "task_failed"}
			if fmt.Sprint(got.NotificationEventTypes) != fmt.Sprint(want) {
				t.Fatalf("notification_event_types = %v, want %v", got.NotificationEventTypes, want)
			}
			if persisted := loadPersisted(t); fmt.Sprint(persisted) != fmt.Sprint(want) {
				t.Fatalf("persisted notification_event_types = %v, want %v", persisted, want)
			}
		})
	}

	t.Run("member forbidden", func(t *testing.T) {
		before := loadPersisted(t)
		if rec := exercise(t, "member", `{"event_types":["mentioned"]}`); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
		}
		if after := loadPersisted(t); fmt.Sprint(after) != fmt.Sprint(before) {
			t.Fatalf("member request changed persisted events: before=%v after=%v", before, after)
		}
	})

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing event_types", body: `{}`},
		{name: "null event_types", body: `{"event_types":null}`},
		{name: "unknown field", body: `{"event_types":["mentioned"],"extra":true}`},
		{name: "trailing JSON value", body: `{"event_types":["mentioned"]}{"event_types":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := loadPersisted(t)
			if rec := exercise(t, "admin", tc.body); rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if after := loadPersisted(t); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatalf("invalid request changed persisted events: before=%v after=%v", before, after)
			}
		})
	}

	t.Run("unknown event rejected", func(t *testing.T) {
		if rec := exercise(t, "admin", `{"event_types":["unknown_event"]}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("empty list succeeds", func(t *testing.T) {
		rec := exercise(t, "admin", `{"event_types":[]}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var got LarkInstallationResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if got.NotificationEventTypes == nil || len(got.NotificationEventTypes) != 0 {
			t.Fatalf("notification_event_types = %#v, want non-nil empty", got.NotificationEventTypes)
		}
		if persisted := loadPersisted(t); persisted == nil || len(persisted) != 0 {
			t.Fatalf("persisted notification_event_types = %#v, want non-nil empty", persisted)
		}
	})

	t.Run("no active notification bot", func(t *testing.T) {
		if _, err := testPool.Exec(ctx, `UPDATE lark_installation SET status = 'revoked' WHERE id = $1`, installationID); err != nil {
			t.Fatalf("revoke notification installation: %v", err)
		}
		if rec := exercise(t, "owner", `{"event_types":["mentioned"]}`); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
		}
	})
}
