package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

func TestNewDingTalkFactory_RejectsMissingSecret(t *testing.T) {
	cfg, _ := json.Marshal(installConfig{AppID: "appkey-1"})
	factory := newDingTalkFactory(ChannelDeps{Decrypt: testBox(t).Open})
	if _, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg}); err == nil {
		t.Error("an installation with no app secret must fail to build")
	}
}
