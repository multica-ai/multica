package lark

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// fakeSender embeds the APIClient interface (nil) and overrides only
// SendTextMessage — the single method feishuChannel.Send calls.
type fakeSender struct {
	APIClient
	last  SendTextParams
	msgID string
}

func (f *fakeSender) SendTextMessage(_ context.Context, p SendTextParams) (string, error) {
	f.last = p
	return f.msgID, nil
}

type fakeCreds struct{ secret string }

func (f fakeCreds) DecryptAppSecret(_ Installation) (string, error) { return f.secret, nil }

// feishuConfigJSON builds a channel_installation.config blob like migration 124
// backfills — the shape the Feishu factory decodes.
func feishuConfigJSON(t *testing.T, appID, region string) []byte {
	t.Helper()
	cfg := map[string]any{
		"app_id":               appID,
		"app_secret_encrypted": base64.StdEncoding.EncodeToString([]byte("sealed-secret")),
		"tenant_key":           "tk_1",
		"bot_open_id":          "ou_bot",
		"region":               region,
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return raw
}

func buildFeishuChannel(t *testing.T, deps FeishuChannelDeps, cfg channel.Config) *feishuChannel {
	t.Helper()
	ch, err := newFeishuFactory(deps)(cfg)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	fc, ok := ch.(*feishuChannel)
	if !ok {
		t.Fatalf("factory returned %T, want *feishuChannel", ch)
	}
	return fc
}

func TestFeishuFactory_DecodesConfigCredentials(t *testing.T) {
	cfg := channel.Config{Type: channel.TypeFeishu, Raw: feishuConfigJSON(t, "cli_app", "lark")}
	fc := buildFeishuChannel(t, FeishuChannelDeps{Connector: NewNoopConnector(nil)}, cfg)

	if fc.inst.AppID != "cli_app" {
		t.Fatalf("app_id = %q, want cli_app", fc.inst.AppID)
	}
	if fc.inst.Region != "lark" {
		t.Fatalf("region = %q, want lark", fc.inst.Region)
	}
	if !fc.inst.TenantKey.Valid || fc.inst.TenantKey.String != "tk_1" {
		t.Fatalf("tenant_key not decoded: %+v", fc.inst.TenantKey)
	}
	if len(fc.inst.AppSecretEncrypted) == 0 {
		t.Fatalf("app_secret_encrypted not decoded")
	}
	if fc.Type() != channel.TypeFeishu {
		t.Fatalf("Type() = %q", fc.Type())
	}
}

func TestFeishuFactory_MissingConnectorFails(t *testing.T) {
	_, err := newFeishuFactory(FeishuChannelDeps{})(channel.Config{Type: channel.TypeFeishu, Raw: feishuConfigJSON(t, "cli", "feishu")})
	if err == nil {
		t.Fatal("expected an error when the factory has no connector")
	}
}

func TestFeishuChannel_Capabilities(t *testing.T) {
	fc := &feishuChannel{}
	caps := fc.Capabilities()
	for _, want := range []channel.Capability{
		channel.CapText, channel.CapRichCard, channel.CapThreadReply,
		channel.CapQuoteReply, channel.CapAttachment, channel.CapTypingIndicator, channel.CapMessageEdit,
	} {
		if !caps.Has(want) {
			t.Fatalf("Capabilities missing %s", want)
		}
	}
	if caps.Has(channel.CapVoice) {
		t.Fatalf("Feishu adapter does not declare voice")
	}
}

func TestFeishuChannel_SendMapsTextAndReplyTarget(t *testing.T) {
	sender := &fakeSender{msgID: "om_sent"}
	fc := &feishuChannel{
		inst:   Installation{AppID: "cli", Region: "feishu"},
		sender: sender,
		creds:  fakeCreds{secret: "plain"},
	}
	res, err := fc.Send(context.Background(), channel.OutboundMessage{
		ChatID:   "oc_chat",
		Text:     "hi there",
		ReplyTo:  "om_parent",
		ThreadID: "t_1",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.MessageID != "om_sent" {
		t.Fatalf("MessageID = %q, want om_sent", res.MessageID)
	}
	if sender.last.ChatID != "oc_chat" || sender.last.Text != "hi there" {
		t.Fatalf("unexpected send params: %+v", sender.last)
	}
	if sender.last.InstallationID.AppID != "cli" || sender.last.InstallationID.AppSecret != "plain" {
		t.Fatalf("credentials not threaded into send: %+v", sender.last.InstallationID)
	}
	// ReplyTo present -> route through the reply endpoint, threaded.
	if sender.last.ReplyTarget.MessageID != "om_parent" || !sender.last.ReplyTarget.InThread {
		t.Fatalf("reply target mapping wrong: %+v", sender.last.ReplyTarget)
	}
}

func TestOutboundReplyTarget(t *testing.T) {
	if got := outboundReplyTarget(channel.OutboundMessage{}); got.IsSet() {
		t.Fatalf("no ReplyTo must yield an unset target, got %+v", got)
	}
	got := outboundReplyTarget(channel.OutboundMessage{ReplyTo: "om", ThreadID: ""})
	if got.MessageID != "om" || got.InThread {
		t.Fatalf("non-thread quote reply mapping wrong: %+v", got)
	}
}

func TestChannelMessageFromLark_NormalizesAndStashesRaw(t *testing.T) {
	lm := InboundMessage{
		EventID:           "evt",
		MessageID:         "om",
		AppID:             "cli",
		ChatID:            "oc",
		ChatType:          ChatTypeGroup,
		SenderOpenID:      "ou_user",
		Body:              "enriched body",
		CommandBody:       "/issue do it",
		MessageType:       "post",
		AddressedToBot:    true,
		ForceFreshSession: true,
		ParentID:          "om_parent",
		RootID:            "om_root",
		ThreadID:          "t_9",
	}
	cm := channelMessageFromLark(lm)

	if cm.EventID != "evt" || cm.MessageID != "om" || cm.Text != "enriched body" {
		t.Fatalf("scalar fields not mapped: %+v", cm)
	}
	if cm.Type != channel.MsgTypeText {
		t.Fatalf("post must normalize to text, got %q", cm.Type)
	}
	if cm.Source.ChannelType != channel.TypeFeishu || cm.Source.ChatID != "oc" ||
		cm.Source.ChatType != channel.ChatTypeGroup || cm.Source.SenderID != "ou_user" || cm.Source.ThreadID != "t_9" {
		t.Fatalf("source not mapped: %+v", cm.Source)
	}
	if !cm.AddressedToBot || !cm.ForceFresh {
		t.Fatalf("addressed/forcefresh not mapped: %+v", cm)
	}
	if cm.ReplyTo == nil || cm.ReplyTo.MessageID != "om_parent" || cm.ReplyTo.RootID != "om_root" {
		t.Fatalf("reply context not mapped: %+v", cm.ReplyTo)
	}
	// Raw must round-trip back to the original lark message (the boundary the
	// Feishu resolvers read app_id / command_body / event_type from).
	got, err := larkMsgFromRaw(cm)
	if err != nil {
		t.Fatalf("larkMsgFromRaw: %v", err)
	}
	if got.AppID != "cli" || got.CommandBody != "/issue do it" || got.MessageType != "post" {
		t.Fatalf("raw round-trip lost platform fields: %+v", got)
	}
}

func TestChannelMsgType(t *testing.T) {
	cases := map[string]channel.MsgType{
		"":              channel.MsgTypeText,
		"text":          channel.MsgTypeText,
		"post":          channel.MsgTypeText,
		"merge_forward": channel.MsgTypeText,
		"image":         channel.MsgTypeImage,
		"file":          channel.MsgTypeFile,
		"audio":         channel.MsgTypeAudio,
		"media":         channel.MsgTypeVideo,
		"sticker":       channel.MsgTypeUnknown,
	}
	for in, want := range cases {
		if got := channelMsgType(in); got != want {
			t.Fatalf("channelMsgType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDispatchResultFromEngine(t *testing.T) {
	res := dispatchResultFromEngine(engine.Result{
		Outcome:         engine.OutcomeNeedsBinding,
		Sender:          "ou_user",
		IssueIdentifier: "MUL-7",
	})
	if res.Outcome != OutcomeNeedsBinding {
		t.Fatalf("outcome not mapped: %q", res.Outcome)
	}
	if res.SenderOpenID != "ou_user" {
		t.Fatalf("sender not mapped: %q", res.SenderOpenID)
	}
	if res.IssueIdentifier != "MUL-7" {
		t.Fatalf("issue identifier not mapped: %q", res.IssueIdentifier)
	}
}
