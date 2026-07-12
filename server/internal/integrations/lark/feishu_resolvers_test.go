package lark

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func binderUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[0] = b
	u.Valid = true
	return u
}

// fakeChatSession records the inputs the Feishu binder maps onto the shared
// engine.ChatSession, so the (platform-specific) mapping is unit-tested.
type fakeChatSession struct {
	ensureIn engine.EnsureSessionInput
	appendIn engine.AppendInput
}

func (f *fakeChatSession) EnsureSession(_ context.Context, in engine.EnsureSessionInput) (pgtype.UUID, error) {
	f.ensureIn = in
	return binderUUID(42), nil
}

func (f *fakeChatSession) AppendUserMessage(_ context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	f.appendIn = in
	return engine.AppendResult{}, nil
}

func TestFeishuSessionBinder_EnsureSessionMapping(t *testing.T) {
	f := &fakeChatSession{}
	b := &feishuSessionBinder{session: f}

	if _, err := b.EnsureSession(context.Background(), engine.EnsureSessionParams{
		Installation: engine.ResolvedInstallation{ID: binderUUID(1), WorkspaceID: binderUUID(2), AgentID: binderUUID(3)},
		Sender:       binderUUID(7),
		Message:      channel.InboundMessage{Source: channel.Source{ChatID: "oc_chat", ChatType: channel.ChatTypeGroup}},
	}); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	got := f.ensureIn
	if got.BindingKey != "oc_chat" {
		t.Errorf("BindingKey = %q, want the chat id (plain group: one session per chat)", got.BindingKey)
	}
	if len(got.BindingConfig) != 0 {
		t.Errorf("plain group must not set BindingConfig (chat id is the real chat): %q", got.BindingConfig)
	}
	if got.WorkspaceID != binderUUID(2) || got.AgentID != binderUUID(3) || got.InstallationID != binderUUID(1) ||
		got.Sender != binderUUID(7) || got.ChatType != channel.ChatTypeGroup {
		t.Errorf("ensure mapping wrong: %+v", got)
	}
}

// TestFeishuSessionBinder_TopicMessageIsolatesByThread pins the topic-group
// session-isolation contract (the Slack channel:threadRoot model): a message
// inside a Lark topic (thread_id present) keys the session by chat+thread so
// two @bot topics in one group do not collapse into one transcript, and the
// real chat id rides the binding config for the outbound path.
func TestFeishuSessionBinder_TopicMessageIsolatesByThread(t *testing.T) {
	f := &fakeChatSession{}
	b := &feishuSessionBinder{session: f}

	if _, err := b.EnsureSession(context.Background(), engine.EnsureSessionParams{
		Installation: engine.ResolvedInstallation{ID: binderUUID(1), WorkspaceID: binderUUID(2), AgentID: binderUUID(3)},
		Sender:       binderUUID(7),
		Message: channel.InboundMessage{Source: channel.Source{
			ChatID: "oc_chat", ChatType: channel.ChatTypeGroup, ThreadID: "omt_topic1",
		}},
	}); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	got := f.ensureIn
	if got.BindingKey != "oc_chat:omt_topic1" {
		t.Errorf("BindingKey = %q, want chat:thread composite (topic isolation)", got.BindingKey)
	}
	var cfg larkBindingConfig
	if err := json.Unmarshal(got.BindingConfig, &cfg); err != nil || cfg.ChatID != "oc_chat" {
		t.Errorf("BindingConfig must carry the real chat id, got %q (err=%v)", got.BindingConfig, err)
	}
}

// TestLarkSessionRouting unit-tests the pure key-derivation contract.
func TestLarkSessionRouting(t *testing.T) {
	cases := []struct {
		name       string
		src        channel.Source
		wantKey    string
		wantConfig bool
	}{
		{"p2p", channel.Source{ChatID: "oc_dm", ChatType: channel.ChatTypeP2P}, "oc_dm", false},
		// p2p never has topics; a stray thread id must not split the DM session.
		{"p2p with stray thread", channel.Source{ChatID: "oc_dm", ChatType: channel.ChatTypeP2P, ThreadID: "omt_x"}, "oc_dm", false},
		{"plain group", channel.Source{ChatID: "oc_g", ChatType: channel.ChatTypeGroup}, "oc_g", false},
		{"topic group", channel.Source{ChatID: "oc_g", ChatType: channel.ChatTypeGroup, ThreadID: "omt_1"}, "oc_g:omt_1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, config := larkSessionRouting(channel.InboundMessage{Source: tc.src})
			if key != tc.wantKey {
				t.Errorf("bindingKey = %q, want %q", key, tc.wantKey)
			}
			if tc.wantConfig != (len(config) > 0) {
				t.Errorf("config presence = %v, want %v (%q)", len(config) > 0, tc.wantConfig, config)
			}
		})
	}
}

func TestFeishuSessionBinder_AppendUsesUnenrichedCommandBody(t *testing.T) {
	f := &fakeChatSession{}
	b := &feishuSessionBinder{session: f}

	// Raw carries the original lark InboundMessage; CommandBody is the user's
	// un-enriched text used for /issue parsing (Body has quoted context inlined).
	raw, _ := json.Marshal(InboundMessage{CommandBody: "/issue Real intent"})
	if _, err := b.AppendMessage(context.Background(), engine.AppendParams{
		SessionID:      binderUUID(1),
		Sender:         binderUUID(7),
		InstallationID: binderUUID(2),
		ClaimToken:     binderUUID(9),
		Message: channel.InboundMessage{
			MessageID: "om_1",
			Text:      "> quoted context\n/issue Real intent",
			Source:    channel.Source{ChatID: "oc", ThreadID: "th_1"},
			Raw:       raw,
		},
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	got := f.appendIn
	if got.CommandText != "/issue Real intent" {
		t.Errorf("CommandText must be the un-enriched CommandBody from Raw, got %q", got.CommandText)
	}
	if got.Body != "> quoted context\n/issue Real intent" {
		t.Errorf("Body must be the (enriched) Message.Text, got %q", got.Body)
	}
	if got.MessageID != "om_1" || got.ThreadID != "th_1" || got.SessionID != binderUUID(1) ||
		got.Sender != binderUUID(7) || got.InstallationID != binderUUID(2) || got.ClaimToken != binderUUID(9) {
		t.Errorf("append mapping wrong: %+v", got)
	}
}
