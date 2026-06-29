package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func uid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[0] = b
	u.Valid = true
	return u
}

type fakeOutboundQueries struct {
	binding    db.ChannelChatSessionBinding
	bindingErr error
	inst       db.ChannelInstallation
	instErr    error
}

func (f *fakeOutboundQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return f.binding, f.bindingErr
}

func (f *fakeOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.inst, f.instErr
}

type fakeSender struct {
	called int
	got    channel.OutboundMessage
}

func (f *fakeSender) Send(_ context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	f.called++
	f.got = out
	return channel.SendResult{MessageID: "1.1"}, nil
}

// slackInstallConfigJSON builds an installation config blob with base64 tokens
// (a nil Decrypter treats the decoded bytes as plaintext).
func slackInstallConfigJSON() []byte {
	b, _ := json.Marshal(map[string]string{
		"app_id":              "T1",
		"bot_user_id":         "UBOT",
		"bot_token_encrypted": base64.StdEncoding.EncodeToString([]byte("xoxb-test")),
	})
	return b
}

func newTestOutbound(q outboundQueries, fs *fakeSender) *Outbound {
	o := NewOutbound(q, nil, nil)
	o.newSender = func(credentials) replySender { return fs }
	return o
}

func chatDoneEvent(sessionID string, content string) events.Event {
	return events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: sessionID,
		Payload:       protocol.ChatDonePayload{Content: content},
	}
}

func TestOutbound_PostsReplyToBoundSlackChannel(t *testing.T) {
	q := &fakeOutboundQueries{
		// Composite isolation key; real channel + reply thread come from config /
		// last_thread_id.
		binding: db.ChannelChatSessionBinding{
			InstallationID: uid(1),
			ChannelChatID:  "C123:1111.0",
			Config:         []byte(`{"channel_id":"C123"}`),
			LastThreadID:   pgtype.Text{String: "1111.0", Valid: true},
		},
		inst: db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fs := &fakeSender{}
	o := newTestOutbound(q, fs)

	o.handleEvent(chatDoneEvent("00000000-0000-0000-0000-000000000001", "**all done**"))

	if fs.called != 1 {
		t.Fatalf("sender called %d times, want 1", fs.called)
	}
	if fs.got.ChatID != "C123" {
		t.Errorf("ChatID = %q, want the real channel from config (not the composite key)", fs.got.ChatID)
	}
	if fs.got.ThreadID != "1111.0" {
		t.Errorf("ThreadID = %q, want the recorded reply thread", fs.got.ThreadID)
	}
	if fs.got.Text != "**all done**" {
		t.Errorf("Text = %q, want the raw content (Send applies mrkdwn)", fs.got.Text)
	}
}

func TestOutbound_IgnoresNonSlackAndEmptyAndRevoked(t *testing.T) {
	const sid = "00000000-0000-0000-0000-000000000001"
	activeInst := db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()}
	boundBinding := db.ChannelChatSessionBinding{InstallationID: uid(1), ChannelChatID: "C1", Config: []byte(`{"channel_id":"C1"}`)}

	cases := []struct {
		name string
		q    *fakeOutboundQueries
		evt  events.Event
	}{
		{
			name: "no slack binding (Feishu / web session)",
			q:    &fakeOutboundQueries{bindingErr: pgx.ErrNoRows},
			evt:  chatDoneEvent(sid, "hi"),
		},
		{
			name: "empty completion content",
			q:    &fakeOutboundQueries{binding: boundBinding, inst: activeInst},
			evt:  chatDoneEvent(sid, ""),
		},
		{
			name: "revoked installation",
			q:    &fakeOutboundQueries{binding: boundBinding, inst: db.ChannelInstallation{ID: uid(1), Status: "revoked", Config: slackInstallConfigJSON()}},
			evt:  chatDoneEvent(sid, "hi"),
		},
		{
			name: "non-chat event (no session id)",
			q:    &fakeOutboundQueries{},
			evt:  chatDoneEvent("", "hi"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := &fakeSender{}
			newTestOutbound(tc.q, fs).handleEvent(tc.evt)
			if fs.called != 0 {
				t.Errorf("%s: sender must not be called, got %d", tc.name, fs.called)
			}
		})
	}
}
