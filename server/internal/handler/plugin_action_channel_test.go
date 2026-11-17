package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

// fakePluginChannel stands in for a platform adapter. It records what the
// handler asked it to send and which installation config it was built from, so
// a test can prove the credentials came from the bound installation row and
// the message was addressed to the binding's channel_user_id.
type fakePluginChannel struct {
	mu    sync.Mutex
	built []channel.Config
	sent  []channel.OutboundMessage
	err   error
}

func (c *fakePluginChannel) Type() channel.Type               { return "telegram" }
func (c *fakePluginChannel) Connect(context.Context) error    { return nil }
func (c *fakePluginChannel) Disconnect(context.Context) error { return nil }
func (c *fakePluginChannel) Capabilities() channel.Capability { return channel.CapText }
func (c *fakePluginChannel) Send(_ context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return channel.SendResult{}, c.err
	}
	c.sent = append(c.sent, out)
	return channel.SendResult{MessageID: "m1"}, nil
}

// withPluginChannelRegistry swaps in a registry whose only platform is the
// fake "telegram" adapter, restoring the handler's registry afterwards.
func withPluginChannelRegistry(t *testing.T, fake *fakePluginChannel) {
	t.Helper()
	registry := channel.NewRegistry()
	registry.Register("telegram", func(cfg channel.Config) (channel.Channel, error) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.built = append(fake.built, cfg)
		return fake, nil
	})
	previous := testHandler.ChannelRegistry
	testHandler.ChannelRegistry = registry
	t.Cleanup(func() { testHandler.ChannelRegistry = previous })
}

// bindTestUserToTelegram creates an active telegram installation in the test
// workspace and links the test user to it under the given platform user id.
func bindTestUserToTelegram(t *testing.T, channelUserID string) string {
	t.Helper()
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "PluginChannelSendAgent", []byte("[]"))
	var installationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, status, installer_user_id)
		VALUES ($1, $2, 'telegram', '{"app_id":"424242","bot_username":"plugin_send_bot"}'::jsonb, 'active', $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&installationID); err != nil {
		t.Fatalf("create installation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_user_binding WHERE installation_id = $1`, installationID)
		testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, installationID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
		VALUES ($1, $2, $3, 'telegram', $4)
	`, testWorkspaceID, testUserID, installationID, channelUserID); err != nil {
		t.Fatalf("bind user: %v", err)
	}
	return installationID
}

func sendChannelRequest(installationID, channelType string, body any) *http.Request {
	return pluginActionRequest(http.MethodPost, "/channels/"+channelType+"/send", installationID, body,
		map[string]string{"channel_type": channelType})
}

func decodeProblem(t *testing.T, recorder *httptest.ResponseRecorder) publicapiv1.Problem {
	t.Helper()
	var problem publicapiv1.Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v body=%s", err, recorder.Body.String())
	}
	return problem
}

func TestSendPluginChannelMessageRequiresScope(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read"})
	withPluginChannelRegistry(t, &fakePluginChannel{})

	recorder := httptest.NewRecorder()
	testHandler.SendPluginChannelMessage(recorder, sendChannelRequest(installationID, "telegram",
		map[string]any{"user_id": testUserID, "text": "hello"}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("ungranted scope status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "channels:send") {
		t.Fatalf("refusal does not name the missing scope: %s", recorder.Body.String())
	}
}

func TestSendPluginChannelMessageRefusesUnknownOrUnconfiguredChannel(t *testing.T) {
	installationID := installPluginForAction(t, []string{"channels:send"})
	body := map[string]any{"user_id": testUserID, "text": "hello"}

	// No engine wired at all: the endpoint is off rather than half-working.
	previous := testHandler.ChannelRegistry
	testHandler.ChannelRegistry = nil
	t.Cleanup(func() { testHandler.ChannelRegistry = previous })
	recorder := httptest.NewRecorder()
	testHandler.SendPluginChannelMessage(recorder, sendChannelRequest(installationID, "telegram", body))
	if recorder.Code != http.StatusServiceUnavailable || decodeProblem(t, recorder).Code != "channels_unavailable" {
		t.Fatalf("nil registry status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// A platform this deployment did not register is indistinguishable from
	// one that does not exist.
	withPluginChannelRegistry(t, &fakePluginChannel{})
	recorder = httptest.NewRecorder()
	testHandler.SendPluginChannelMessage(recorder, sendChannelRequest(installationID, "pager", body))
	if recorder.Code != http.StatusNotFound || decodeProblem(t, recorder).Code != "channel_not_found" {
		t.Fatalf("unknown channel status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSendPluginChannelMessageValidatesBody(t *testing.T) {
	installationID := installPluginForAction(t, []string{"channels:send"})
	fake := &fakePluginChannel{}
	withPluginChannelRegistry(t, fake)

	for name, body := range map[string]any{
		"bad_user_id": map[string]any{"user_id": "not-a-uuid", "text": "hello"},
		"empty_text":  map[string]any{"user_id": testUserID, "text": "   "},
		"long_text":   map[string]any{"user_id": testUserID, "text": strings.Repeat("x", maxPluginChannelMessageChars+1)},
	} {
		recorder := httptest.NewRecorder()
		testHandler.SendPluginChannelMessage(recorder, sendChannelRequest(installationID, "telegram", body))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d body=%s", name, recorder.Code, recorder.Body.String())
		}
	}
	if len(fake.sent) != 0 {
		t.Fatalf("invalid requests reached the adapter: %+v", fake.sent)
	}
}

func TestSendPluginChannelMessageWithoutBindingIs404(t *testing.T) {
	installationID := installPluginForAction(t, []string{"channels:send"})
	fake := &fakePluginChannel{}
	withPluginChannelRegistry(t, fake)

	recorder := httptest.NewRecorder()
	testHandler.SendPluginChannelMessage(recorder, sendChannelRequest(installationID, "telegram",
		map[string]any{"user_id": testUserID, "text": "hello"}))
	if recorder.Code != http.StatusNotFound || decodeProblem(t, recorder).Code != "binding_not_found" {
		t.Fatalf("unbound member status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(fake.sent) != 0 {
		t.Fatalf("unbound member reached the adapter: %+v", fake.sent)
	}
}

func TestSendPluginChannelMessageDeliversToBoundMember(t *testing.T) {
	installationID := installPluginForAction(t, []string{"channels:send"})
	fake := &fakePluginChannel{}
	withPluginChannelRegistry(t, fake)
	channelInstallationID := bindTestUserToTelegram(t, "424242")

	// Session caller (a surface acting for the signed-in member).
	recorder := httptest.NewRecorder()
	testHandler.SendPluginChannelMessage(recorder, sendChannelRequest(installationID, "telegram",
		map[string]any{"user_id": testUserID, "text": "  Review needed on MUL-1  "}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("session send status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response publicapiv1.SendChannelMessageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Delivered || response.ChannelType != "telegram" {
		t.Fatalf("response = %+v", response)
	}

	// Install token (the plugin's own server, no person behind it): an event
	// hook must be able to page someone, so the plugin actor is not refused.
	token, err := testHandler.PluginService.IssueInstallToken(context.Background(), parseUUID(installationID))
	if err != nil {
		t.Fatalf("issue install token: %v", err)
	}
	recorder = httptest.NewRecorder()
	testHandler.SendPluginChannelMessage(recorder, pluginInstallTokenRequest(http.MethodPost, "/channels/telegram/send", token,
		map[string]any{"user_id": testUserID, "text": "from the hook"}, map[string]string{"channel_type": "telegram"}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("install-token send status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.sent) != 2 {
		t.Fatalf("adapter sends = %+v", fake.sent)
	}
	if fake.sent[0].ChatID != "424242" || fake.sent[0].Text != "Review needed on MUL-1" {
		t.Fatalf("first send addressed wrongly: %+v", fake.sent[0])
	}
	if fake.sent[1].ChatID != "424242" || fake.sent[1].Text != "from the hook" {
		t.Fatalf("second send addressed wrongly: %+v", fake.sent[1])
	}
	// The adapter was built from the BOUND installation's row, not from
	// anything the caller supplied.
	if len(fake.built) != 2 || uuidToString(fake.built[0].ID) != channelInstallationID ||
		!strings.Contains(string(fake.built[0].Raw), `"plugin_send_bot"`) || fake.built[0].Handler != nil {
		t.Fatalf("adapter built from the wrong config: %+v", fake.built)
	}
}

func TestSendPluginChannelMessageReportsPlatformFailure(t *testing.T) {
	installationID := installPluginForAction(t, []string{"channels:send"})
	fake := &fakePluginChannel{err: errors.New("telegram: 403 bot was blocked by the user")}
	withPluginChannelRegistry(t, fake)
	bindTestUserToTelegram(t, "424242")

	recorder := httptest.NewRecorder()
	testHandler.SendPluginChannelMessage(recorder, sendChannelRequest(installationID, "telegram",
		map[string]any{"user_id": testUserID, "text": "hello"}))
	if recorder.Code != http.StatusBadGateway || decodeProblem(t, recorder).Code != "delivery_failed" {
		t.Fatalf("platform failure status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// The platform's own error text stays in the log; the plugin gets the
	// stable code only.
	if strings.Contains(recorder.Body.String(), "blocked by the user") {
		t.Fatalf("platform error leaked to the caller: %s", recorder.Body.String())
	}
}
