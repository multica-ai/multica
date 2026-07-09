package lark

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// fakeSender embeds the APIClient interface (nil) and overrides only
// SendTextMessage — the single method feishuChannel.Send calls.
type fakeSender struct {
	APIClient
	last          SendTextParams
	msgID         string
	downloadCalls []DownloadResourceParams
	downloaded    DownloadedResource
}

func (f *fakeSender) SendTextMessage(_ context.Context, p SendTextParams) (string, error) {
	f.last = p
	return f.msgID, nil
}

func (f *fakeSender) DownloadMessageResource(_ context.Context, _ InstallationCredentials, p DownloadResourceParams) (DownloadedResource, error) {
	f.downloadCalls = append(f.downloadCalls, p)
	return f.downloaded, nil
}

func (f *fakeSender) DownloadMessageResourceStream(_ context.Context, _ InstallationCredentials, p DownloadResourceParams) (DownloadedResourceStream, error) {
	f.downloadCalls = append(f.downloadCalls, p)
	return DownloadedResourceStream{
		Body:        io.NopCloser(bytes.NewReader(f.downloaded.Data)),
		ContentType: f.downloaded.ContentType,
		Filename:    f.downloaded.Filename,
		SizeBytes:   f.downloaded.SizeBytes,
	}, nil
}

type fakeMediaStorage struct {
	uploads []fakeMediaUpload
}

type fakeMediaUpload struct {
	key         string
	data        []byte
	contentType string
	filename    string
}

func (s *fakeMediaStorage) Upload(_ context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	s.uploads = append(s.uploads, fakeMediaUpload{key: key, data: append([]byte(nil), data...), contentType: contentType, filename: filename})
	return "https://cdn.example.test/" + key, nil
}

func (s *fakeMediaStorage) UploadStream(_ context.Context, key string, data io.Reader, contentType string, filename string) (string, error) {
	body, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	s.uploads = append(s.uploads, fakeMediaUpload{key: key, data: body, contentType: contentType, filename: filename})
	return "https://cdn.example.test/" + key, nil
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

func TestFeishuMediaResolver_AttachesImageMediaRef(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{
		Data:        []byte{1, 2, 3},
		ContentType: "image/png",
		Filename:    "from-header.png",
		SizeBytes:   3,
	}}
	storage := &fakeMediaStorage{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, newDiscardLogger())
	lm := InboundMessage{
		EventID:      "evt-image",
		AppID:        "cli_app",
		ChatID:       "oc_dm",
		ChatType:     ChatTypeP2P,
		MessageID:    "om_image",
		SenderOpenID: "ou_user",
		MessageType:  "image",
		Body:         "[Image]",
		Content:      `{"image_key":"img_v3_key"}`,
	}
	got := resolver.ResolveMedia(context.Background(), engine.ResolvedInstallation{
		WorkspaceID: uuidFromString(t, "11111111-1111-1111-1111-111111111111"),
		Platform:    Installation{AppID: "cli_app", Region: "feishu"},
	}, engine.ResolvedIdentity{}, uuidFromString(t, "22222222-2222-2222-2222-222222222222"), channelMessageFromLark(lm))
	if len(sender.downloadCalls) != 1 {
		t.Fatalf("download calls = %d, want 1", len(sender.downloadCalls))
	}
	call := sender.downloadCalls[0]
	if call.MessageID != "om_image" || call.FileKey != "img_v3_key" || call.Type != "image" {
		t.Fatalf("download params wrong: %+v", call)
	}
	if len(storage.uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(storage.uploads))
	}
	up := storage.uploads[0]
	if up.contentType != "image/png" || up.filename != "from-header.png" || string(up.data) != string([]byte{1, 2, 3}) {
		t.Fatalf("upload metadata wrong: %+v", up)
	}
	if !strings.Contains(up.key, "workspaces/11111111-1111-1111-1111-111111111111/lark/") {
		t.Fatalf("upload key should be workspace-scoped, got %q", up.key)
	}
	if len(got.MediaRefs) != 1 {
		t.Fatalf("media refs = %+v, want 1", got.MediaRefs)
	}
	ref := got.MediaRefs[0]
	if ref.Type != channel.MsgTypeImage || ref.Filename != "from-header.png" || ref.MimeType != "image/png" ||
		ref.SizeBytes != 3 || ref.StorageURL == "" || ref.StorageKey == "" {
		t.Fatalf("media ref wrong: %+v", ref)
	}
}

func TestFeishuMediaResolver_AttachesVideoMediaRef(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{
		Data:        []byte("mp4"),
		ContentType: "video/mp4",
		SizeBytes:   3,
	}}
	storage := &fakeMediaStorage{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, newDiscardLogger())
	lm := InboundMessage{
		EventID:      "evt-video",
		AppID:        "cli_app",
		ChatID:       "oc_dm",
		ChatType:     ChatTypeP2P,
		MessageID:    "om_video",
		SenderOpenID: "ou_user",
		MessageType:  "media",
		Body:         "[Video]",
		Content:      `{"file_key":"file_v3_key","file_name":"clip.mp4"}`,
	}
	got := resolver.ResolveMedia(context.Background(), engine.ResolvedInstallation{
		WorkspaceID: uuidFromString(t, "11111111-1111-1111-1111-111111111111"),
		Platform:    Installation{AppID: "cli_app", Region: "feishu"},
	}, engine.ResolvedIdentity{}, uuidFromString(t, "22222222-2222-2222-2222-222222222222"), channelMessageFromLark(lm))
	if len(sender.downloadCalls) != 1 {
		t.Fatalf("download calls = %d, want 1", len(sender.downloadCalls))
	}
	call := sender.downloadCalls[0]
	if call.MessageID != "om_video" || call.FileKey != "file_v3_key" || call.Type != "file" {
		t.Fatalf("download params wrong: %+v", call)
	}
	if len(got.MediaRefs) != 1 || got.MediaRefs[0].Type != channel.MsgTypeVideo || got.MediaRefs[0].Filename != "clip.mp4" {
		t.Fatalf("video ref wrong: %+v", got.MediaRefs)
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
