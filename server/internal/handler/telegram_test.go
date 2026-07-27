package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/telegram"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// telegramTestBox is a fixed test key so tests don't depend on env config.
func telegramTestBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 7)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

// telegramMockServer stands in for the Telegram Bot API (getMe/setWebhook/
// deleteWebhook) so RegisterBYO/Revoke can run against a real InstallService
// (real DB-backed queries, per this package's handler test convention) without
// calling out to the real Telegram network.
func telegramMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			w.Write([]byte(`{"ok":true,"result":{"id":123456789,"username":"acme_bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/setWebhook"):
			w.Write([]byte(`{"ok":true,"result":true}`))
		case strings.HasSuffix(r.URL.Path, "/deleteWebhook"):
			w.Write([]byte(`{"ok":true,"result":true}`))
		default:
			w.Write([]byte(`{"ok":false,"description":"unknown method"}`))
		}
	}))
}

// newTelegramTestInstallService builds a real *telegram.InstallService bound
// to the shared test pool, with its Telegram API base pointed at a local mock
// server so RegisterBYO/Revoke exercise the real DB path without a live bot.
func newTelegramTestInstallService(t *testing.T) *telegram.InstallService {
	t.Helper()
	srv := telegramMockServer(t)
	t.Cleanup(srv.Close)

	queries := db.New(testPool)
	svc, err := telegram.NewInstallService(queries, testPool, telegramTestBox(t), "https://public.example.test", nil)
	if err != nil {
		t.Fatalf("telegram.NewInstallService: %v", err)
	}
	svc.SetAPIBaseForTest(srv.URL)
	return svc
}

// TestRegisterTelegramBYO_NotConfigured guards the 503 path when the
// installation encryption key is not configured (TelegramInstall == nil).
func TestRegisterTelegramBYO_NotConfigured(t *testing.T) {
	h := &Handler{}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/telegram/install/byo?agent_id=11111111-1111-1111-1111-111111111111", RegisterTelegramBYORequest{
		BotToken: "123456789:AAExampleSecretToken",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	h.RegisterTelegramBYO(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRegisterTelegramBYO_MissingAgentID guards the 400 path when agent_id is
// absent from the query string.
func TestRegisterTelegramBYO_MissingAgentID(t *testing.T) {
	h := &Handler{TelegramInstall: newTelegramTestInstallService(t)}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/telegram/install/byo", RegisterTelegramBYORequest{
		BotToken: "123456789:AAExampleSecretToken",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	h.RegisterTelegramBYO(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRegisterTelegramBYO_Success verifies a successful BYO install returns
// 200 with bot_username populated and broadcasts telegram_installation:created
// so every open client (not just the installer's tab) invalidates its
// installations query, mirroring Slack's equivalent regression guard.
func TestRegisterTelegramBYO_Success(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Telegram BYO Agent", nil)
	bus := events.New()
	h := &Handler{
		Queries:         db.New(testPool),
		Bus:             bus,
		TelegramInstall: newTelegramTestInstallService(t),
	}

	var got events.Event
	fired := 0
	bus.Subscribe(protocol.EventTelegramInstallationCreated, func(e events.Event) {
		got = e
		fired++
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/telegram/install/byo?agent_id="+agentID, RegisterTelegramBYORequest{
		BotToken: "123456789:AAExampleSecretToken",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	h.RegisterTelegramBYO(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp TelegramInstallationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.BotUsername != "acme_bot" {
		t.Errorf("bot_username = %q, want acme_bot", resp.BotUsername)
	}
	if resp.AgentID != agentID {
		t.Errorf("agent_id = %q, want %q", resp.AgentID, agentID)
	}
	if resp.WorkspaceID != testWorkspaceID {
		t.Errorf("workspace_id = %q, want %q", resp.WorkspaceID, testWorkspaceID)
	}

	if fired != 1 {
		t.Fatalf("expected telegram_installation:created published once, got %d", fired)
	}
	if got.WorkspaceID != testWorkspaceID || got.ActorType != "user" {
		t.Errorf("event envelope = %+v", got)
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok || payload["id"] != resp.ID {
		t.Errorf("payload = %v, want installation id %s", got.Payload, resp.ID)
	}

	t.Cleanup(func() {
		testPool.Exec(req.Context(), `DELETE FROM channel_installation WHERE id = $1`, resp.ID)
	})
}

// TestRevokeTelegramInstallation_ForeignWorkspace guards the workspace-scoped
// lookup: an installation id from a different workspace must 404, not leak
// existence or allow cross-workspace revocation.
func TestRevokeTelegramInstallation_ForeignWorkspace(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Telegram Revoke Agent", nil)
	h := &Handler{
		Queries:         db.New(testPool),
		Bus:             events.New(),
		TelegramInstall: newTelegramTestInstallService(t),
	}

	// Install in the real test workspace.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/telegram/install/byo?agent_id="+agentID, RegisterTelegramBYORequest{
		BotToken: "123456789:AAExampleSecretToken",
	})
	req = withURLParam(req, "id", testWorkspaceID)
	h.RegisterTelegramBYO(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup install: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var installed TelegramInstallationResponse
	if err := json.NewDecoder(w.Body).Decode(&installed); err != nil {
		t.Fatalf("decode install response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(req.Context(), `DELETE FROM channel_installation WHERE id = $1`, installed.ID)
	})

	// Create a foreign workspace and attempt to revoke the installation from
	// there — the installation exists, but not in THIS workspace's scope.
	const foreignWorkspaceID = "99999999-9999-9999-9999-999999999999"
	testPool.Exec(req.Context(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	if _, err := testPool.Exec(req.Context(), `
		INSERT INTO workspace (id, name, slug, description, issue_prefix)
		VALUES ($1, 'Foreign Telegram Workspace', 'foreign-telegram-ws', '', 'FTW')
	`, foreignWorkspaceID); err != nil {
		t.Fatalf("insert foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(req.Context(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})

	rw := httptest.NewRecorder()
	rreq := newRequest("DELETE", "/api/workspaces/"+foreignWorkspaceID+"/telegram/installations/"+installed.ID, nil)
	rreq = withURLParams(rreq, "id", foreignWorkspaceID, "installationId", installed.ID)
	h.RevokeTelegramInstallation(rw, rreq)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for foreign-workspace revoke, got %d: %s", rw.Code, rw.Body.String())
	}
}
