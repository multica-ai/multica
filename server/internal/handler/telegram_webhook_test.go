package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeTelegramChannelHandler is the test-only channelHandler seam:
// TelegramWebhook is exercised as a pure HTTP handler here, without standing
// up a real engine.Router + resolver set, by recording whatever
// InboundMessage it is handed.
type fakeTelegramChannelHandler struct {
	mu    sync.Mutex
	calls []channel.InboundMessage
	err   error
}

func (f *fakeTelegramChannelHandler) Handle(_ context.Context, msg channel.InboundMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, msg)
	return f.err
}

func (f *fakeTelegramChannelHandler) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeTelegramChannelHandler) lastCall() channel.InboundMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// insertTelegramWebhookInstallation writes a channel_installation row
// directly (no FK constraints on this table, per repo convention) so the
// webhook test doesn't need to go through the full BYO install flow with a
// mock Telegram API. Random agent/installer UUIDs are fine — the webhook
// path never dereferences them.
func insertTelegramWebhookInstallation(t *testing.T, botID, webhookSecret, botUsername, status string) {
	t.Helper()
	ctx := context.Background()

	cfg, err := json.Marshal(map[string]string{
		"app_id":              botID,
		"bot_username":        botUsername,
		"bot_token_encrypted": "",
		"webhook_secret":      webhookSecret,
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	var agentID, installerID pgtype.UUID
	if err := agentID.Scan("11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("scan agent id: %v", err)
	}
	installerID = agentID

	var instID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, status, installer_user_id)
		VALUES ($1, $2, 'telegram', $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, agentID, cfg, status, installerID).Scan(&instID); err != nil {
		t.Fatalf("insert telegram installation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, instID)
	})
}

func validTelegramUpdateBody(chatID int64) []byte {
	body := map[string]any{
		"update_id": 1,
		"message": map[string]any{
			"message_id": 42,
			"from":       map[string]any{"id": 999, "is_bot": false, "username": "someone"},
			"chat":       map[string]any{"id": chatID, "type": "private"},
			"text":       "/issue Ship it",
		},
	}
	b, _ := json.Marshal(body)
	return b
}

func TestTelegramWebhook_ValidSecretRoutesToEngine(t *testing.T) {
	botID := "tw-valid-bot"
	insertTelegramWebhookInstallation(t, botID, "correct-secret", "acme_bot", "active")

	fake := &fakeTelegramChannelHandler{}
	h := &Handler{Queries: db.New(testPool), testChannelHandler: fake}

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/telegram/"+botID, bytes.NewReader(validTelegramUpdateBody(555)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "correct-secret")
	req = withURLParam(req, "botId", botID)
	w := httptest.NewRecorder()

	h.TelegramWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if fake.callCount() != 1 {
		t.Fatalf("router call count = %d, want 1", fake.callCount())
	}
	msg := fake.lastCall()
	if msg.Source.ChannelType != channel.Type("telegram") {
		t.Fatalf("Source.ChannelType = %q, want telegram", msg.Source.ChannelType)
	}
	if msg.Source.ChatID != "555" {
		t.Fatalf("Source.ChatID = %q, want 555", msg.Source.ChatID)
	}
}

func TestTelegramWebhook_WrongSecretDoesNotRoute(t *testing.T) {
	botID := "tw-wrong-secret-bot"
	insertTelegramWebhookInstallation(t, botID, "correct-secret", "acme_bot", "active")

	fake := &fakeTelegramChannelHandler{}
	h := &Handler{Queries: db.New(testPool), testChannelHandler: fake}

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/telegram/"+botID, bytes.NewReader(validTelegramUpdateBody(555)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong-secret")
	req = withURLParam(req, "botId", botID)
	w := httptest.NewRecorder()

	h.TelegramWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if fake.callCount() != 0 {
		t.Fatalf("router call count = %d, want 0", fake.callCount())
	}
}

func TestTelegramWebhook_UnknownBotIDDoesNotRoute(t *testing.T) {
	fake := &fakeTelegramChannelHandler{}
	h := &Handler{Queries: db.New(testPool), testChannelHandler: fake}

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/telegram/does-not-exist", bytes.NewReader(validTelegramUpdateBody(555)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "anything")
	req = withURLParam(req, "botId", "does-not-exist")
	w := httptest.NewRecorder()

	h.TelegramWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if fake.callCount() != 0 {
		t.Fatalf("router call count = %d, want 0", fake.callCount())
	}
}

func TestTelegramWebhook_MalformedJSONDoesNotRouteOrPanic(t *testing.T) {
	botID := "tw-malformed-bot"
	insertTelegramWebhookInstallation(t, botID, "correct-secret", "acme_bot", "active")

	fake := &fakeTelegramChannelHandler{}
	h := &Handler{Queries: db.New(testPool), testChannelHandler: fake}

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/telegram/"+botID, bytes.NewReader([]byte("{not json")))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "correct-secret")
	req = withURLParam(req, "botId", botID)
	w := httptest.NewRecorder()

	h.TelegramWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if fake.callCount() != 0 {
		t.Fatalf("router call count = %d, want 0", fake.callCount())
	}
}

func TestTelegramWebhook_InactiveInstallationDoesNotRoute(t *testing.T) {
	botID := "tw-revoked-bot"
	insertTelegramWebhookInstallation(t, botID, "correct-secret", "acme_bot", "revoked")

	fake := &fakeTelegramChannelHandler{}
	h := &Handler{Queries: db.New(testPool), testChannelHandler: fake}

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/telegram/"+botID, bytes.NewReader(validTelegramUpdateBody(555)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "correct-secret")
	req = withURLParam(req, "botId", botID)
	w := httptest.NewRecorder()

	h.TelegramWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if fake.callCount() != 0 {
		t.Fatalf("router call count = %d, want 0", fake.callCount())
	}
}
