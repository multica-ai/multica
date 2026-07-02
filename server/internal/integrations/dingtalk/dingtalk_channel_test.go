package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func noopHandler(_ context.Context, _ channel.InboundMessage) error { return nil }

func TestNewDingTalkFactory_DecryptsAndBuilds(t *testing.T) {
	box := testBox(t)
	sealed, _ := box.Seal([]byte("plain-secret"))
	cfg, _ := json.Marshal(installConfig{
		AppID:              "appkey-1",
		RobotCode:          "appkey-1",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})

	factory := newDingTalkFactory(ChannelDeps{Decrypt: box.Open})
	built, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	dc, ok := built.(*dingtalkChannel)
	if !ok {
		t.Fatalf("built channel is not a *dingtalkChannel: %T", built)
	}
	if dc.appKey != "appkey-1" || dc.appID != "appkey-1" || dc.robotCode != "appkey-1" {
		t.Errorf("identity fields wrong: %+v", dc)
	}
	if dc.appSecret != "plain-secret" {
		t.Errorf("app secret = %q, want decrypted plain-secret", dc.appSecret)
	}
	if dc.Type() != TypeDingTalk {
		t.Errorf("Type() = %q", dc.Type())
	}
	if dc.Capabilities()&channel.CapText == 0 {
		t.Error("channel must declare text capability")
	}
}

// A "/new /issue …" combo must NOT be diverted to the quick-create processor:
// the /new prefix set ForceFresh, and diverting would drop it and leave the
// session un-rotated. onMessage lets it reach the engine handler instead, which
// rotates the session and still creates the issue.
func TestOnMessage_ForceFreshIssueReachesEngine(t *testing.T) {
	var handled *channel.InboundMessage
	c := &dingtalkChannel{
		appID:   "appkey-1",
		handler: func(_ context.Context, m channel.InboundMessage) error { handled = &m; return nil },
		// A non-nil processor would normally divert an addressed /issue; the
		// ForceFresh guard must short-circuit before it is ever consulted.
		issueCmd: &IssueCommandProcessor{},
		logger:   slog.Default(),
	}

	data := &botCallbackData{
		ConversationId:   "cid-1",
		ConversationType: convTypeP2P,
		SenderStaffId:    "staff-1",
		MsgId:            "msg-1",
		Msgtype:          "text",
		Text:             botCallbackText{Content: "/new /issue login broken"},
	}
	if err := c.onMessage(context.Background(), data); err != nil {
		t.Fatalf("onMessage: %v", err)
	}
	if handled == nil {
		t.Fatal("ForceFresh /issue must reach the engine handler, not be diverted")
	}
	if !handled.ForceFresh {
		t.Error("ForceFresh must be preserved on the message handed to the engine")
	}
	if handled.Text != "/issue login broken" {
		t.Errorf("engine body = %q, want %q", handled.Text, "/issue login broken")
	}
}

func TestNewDingTalkFactory_RejectsMissingSecret(t *testing.T) {
	cfg, _ := json.Marshal(installConfig{AppID: "appkey-1"})
	factory := newDingTalkFactory(ChannelDeps{Decrypt: testBox(t).Open})
	if _, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg}); err == nil {
		t.Error("an installation with no app secret must fail to build")
	}
}
