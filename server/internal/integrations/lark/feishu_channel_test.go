package lark

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	last             SendTextParams
	msgID            string
	downloadCalls    []DownloadResourceParams
	downloaded       DownloadedResource
	downloadErr      error
	downloadedByKey  map[string]DownloadedResource
	downloadErrByKey map[string]error
}

func (f *fakeSender) SendTextMessage(_ context.Context, p SendTextParams) (string, error) {
	f.last = p
	return f.msgID, nil
}

func (f *fakeSender) DownloadMessageResource(_ context.Context, _ InstallationCredentials, p DownloadResourceParams) (DownloadedResource, error) {
	return f.download(p)
}

func (f *fakeSender) DownloadMessageResourceStream(_ context.Context, _ InstallationCredentials, p DownloadResourceParams) (DownloadedResourceStream, error) {
	got, err := f.download(p)
	if err != nil {
		return DownloadedResourceStream{}, err
	}
	return DownloadedResourceStream{
		Body:        io.NopCloser(bytes.NewReader(got.Data)),
		ContentType: got.ContentType,
		Filename:    got.Filename,
		SizeBytes:   got.SizeBytes,
	}, nil
}

func (f *fakeSender) download(p DownloadResourceParams) (DownloadedResource, error) {
	f.downloadCalls = append(f.downloadCalls, p)
	if err := f.downloadErrByKey[p.FileKey]; err != nil {
		return DownloadedResource{}, err
	}
	if got, ok := f.downloadedByKey[p.FileKey]; ok {
		return got, nil
	}
	return f.downloaded, f.downloadErr
}

type fakeMediaStorage struct {
	uploads []fakeMediaUpload
	deleted []string
	err     error
}

func (s *fakeMediaStorage) Delete(_ context.Context, key string) {
	s.deleted = append(s.deleted, key)
}

func (s *fakeMediaStorage) ObjectURL(key string) string {
	return "https://cdn.example.test/" + key
}

type fakeMediaUpload struct {
	key         string
	data        []byte
	sizeBytes   int64
	streamed    bool
	contentType string
	filename    string
}

func (s *fakeMediaStorage) Upload(_ context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.uploads = append(s.uploads, fakeMediaUpload{key: key, data: append([]byte(nil), data...), contentType: contentType, filename: filename})
	return "https://cdn.example.test/" + key, nil
}

func (s *fakeMediaStorage) UploadStream(_ context.Context, key string, data io.Reader, sizeBytes int64, contentType string, filename string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	body, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	s.uploads = append(s.uploads, fakeMediaUpload{key: key, data: body, sizeBytes: sizeBytes, streamed: true, contentType: contentType, filename: filename})
	return "https://cdn.example.test/" + key, nil
}

// fakeMediaLedger records intent rows. ownedKeys marks keys the reconciler
// owns ('deleting'): the resolver must skip them entirely.
type fakeMediaLedger struct {
	records   []engine.RecordPendingMediaObjectParams
	ownedKeys map[string]bool
	ownAll    bool
	err       error
}

func (l *fakeMediaLedger) RecordPendingMediaObject(_ context.Context, p engine.RecordPendingMediaObjectParams) (bool, error) {
	if l.err != nil {
		return false, l.err
	}
	l.records = append(l.records, p)
	if l.ownAll || l.ownedKeys[p.StorageKey] {
		return false, nil
	}
	return true, nil
}

func ownAllKeys(l *fakeMediaLedger) map[string]bool {
	l.ownAll = true
	return l.ownedKeys
}

type fakeCreds struct{ secret string }

func (f fakeCreds) DecryptAppSecret(_ Installation) (string, error) { return f.secret, nil }

func testMediaInstallation(t *testing.T) engine.ResolvedInstallation {
	t.Helper()
	return engine.ResolvedInstallation{
		ID:          uuidFromString(t, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		WorkspaceID: uuidFromString(t, "11111111-1111-1111-1111-111111111111"),
		Platform:    Installation{AppID: "cli_app", Region: "feishu"},
	}
}

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

func TestFeishuMediaResolver_HasMedia(t *testing.T) {
	resolver := NewFeishuMediaResolver(&fakeSender{}, fakeCreds{secret: "plain"}, &fakeMediaStorage{}, &fakeMediaLedger{}, newDiscardLogger())
	cases := []struct {
		name string
		lm   InboundMessage
		want bool
	}{
		{"text", InboundMessage{MessageID: "om_t", MessageType: "text", Body: "hello", Content: `{"text":"hello"}`}, false},
		{"image", InboundMessage{MessageID: "om_i", MessageType: "image", Body: "[Image]", Content: `{"image_key":"img_k"}`}, true},
		{"video", InboundMessage{MessageID: "om_v", MessageType: "media", Body: "[Video]", Content: `{"file_key":"file_k"}`}, true},
		{"post with image", InboundMessage{MessageID: "om_p", MessageType: "post",
			Content: `{"content":[[{"tag":"img","image_key":"img_post"}]]}`}, true},
		{"post text only", InboundMessage{MessageID: "om_pt", MessageType: "post",
			Content: `{"content":[[{"tag":"text","text":"plain"}]]}`}, false},
		{"image missing key", InboundMessage{MessageID: "om_bad", MessageType: "image", Content: `{}`}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolver.HasMedia(channelMessageFromLark(tc.lm)); got != tc.want {
				t.Fatalf("HasMedia = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFeishuMediaResolver_RecordsIntentBeforeUpload pins the ledger ordering:
// the pending row is durable BEFORE the PUT, carries the URL the attachment
// will hold, and identifies the message the reconciler checks against.
func TestFeishuMediaResolver_RecordsIntentBeforeUpload(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{Data: []byte{1}, ContentType: "image/png", SizeBytes: 1}}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	lm := InboundMessage{
		MessageID:   "om_intent",
		MessageType: "image",
		Body:        "[Image]",
		Content:     `{"image_key":"img_intent"}`,
	}
	messageID := uuidFromString(t, "33333333-3333-4333-8333-333333333333")
	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), messageID, channelMessageFromLark(lm))
	if len(ledger.records) != 1 || len(storage.uploads) != 1 || len(got.MediaRefs) != 1 {
		t.Fatalf("records=%d uploads=%d refs=%d, want 1/1/1", len(ledger.records), len(storage.uploads), len(got.MediaRefs))
	}
	rec := ledger.records[0]
	if rec.StorageKey != storage.uploads[0].key {
		t.Fatalf("intent key %q != uploaded key %q", rec.StorageKey, storage.uploads[0].key)
	}
	if rec.StorageURL != got.MediaRefs[0].StorageURL || rec.StorageURL != storage.ObjectURL(rec.StorageKey) {
		t.Fatalf("intent url %q must match the ref/attachment url %q", rec.StorageURL, got.MediaRefs[0].StorageURL)
	}
	if rec.ChatMessageID != messageID {
		t.Fatalf("intent message id = %v, want %v", rec.ChatMessageID, messageID)
	}
	if !rec.WorkspaceID.Valid || !rec.InstallationID.Valid {
		t.Fatalf("intent must carry workspace and installation ids: %+v", rec)
	}
}

// A key the reconciler owns must not be uploaded at all — the state-guarded
// upsert refuses to resurrect it and the resolver skips the resource.
func TestFeishuMediaResolver_ReconcilerOwnedKeySkipsUpload(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{Data: []byte{1}, ContentType: "image/png", SizeBytes: 1}}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{ownedKeys: map[string]bool{}}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	lm := InboundMessage{
		MessageID:   "om_owned",
		MessageType: "image",
		Body:        "[Image]",
		Content:     `{"image_key":"img_owned"}`,
	}
	// Every key is owned by the reconciler in this fake.
	ledger.ownedKeys = ownAllKeys(ledger)

	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(lm))
	if len(storage.uploads) != 0 || len(sender.downloadCalls) != 0 {
		t.Fatalf("owned key must skip download+upload entirely: uploads=%d downloads=%d", len(storage.uploads), len(sender.downloadCalls))
	}
	if len(got.MediaRefs) != 0 {
		t.Fatalf("owned key must yield no refs: %+v", got.MediaRefs)
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
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
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
	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(lm))
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
	if !up.streamed || up.sizeBytes != 3 {
		t.Fatalf("known-length resource did not stream with its declared size: %+v", up)
	}
	if !strings.Contains(up.key, "workspaces/11111111-1111-1111-1111-111111111111/lark/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/") {
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

func TestFeishuMediaResolver_UnknownLengthUsesBufferedUpload(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{
		Data:        []byte{1, 2, 3},
		ContentType: "image/png",
		SizeBytes:   0,
	}}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	lm := InboundMessage{
		MessageID:   "om_unknown_length",
		MessageType: "image",
		Body:        "[Image]",
		Content:     `{"image_key":"img_unknown_length"}`,
	}
	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(lm))
	if len(storage.uploads) != 1 || storage.uploads[0].streamed {
		t.Fatalf("unknown-length resource must use buffered Upload: %+v", storage.uploads)
	}
	if len(got.MediaRefs) != 1 || got.MediaRefs[0].SizeBytes != 3 {
		t.Fatalf("buffered upload size not recorded: %+v", got.MediaRefs)
	}
}

func TestFeishuMediaResolver_AttachesPostEmbeddedImageMediaRef(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{
		Data:        []byte{4, 5, 6},
		ContentType: "image/png",
		Filename:    "post-image.png",
		SizeBytes:   3,
	}}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	rawPost := `{"content":[[{"tag":"img","image_key":"img_post_key"}],[{"tag":"text","text":"识别一下图片"}]]}`
	lm := InboundMessage{
		EventID:      "evt-post-image",
		AppID:        "cli_app",
		ChatID:       "oc_dm",
		ChatType:     ChatTypeP2P,
		MessageID:    "om_post_image",
		SenderOpenID: "ou_user",
		MessageType:  "post",
		Body:         flattenPostContent(rawPost),
		Content:      rawPost,
	}
	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(lm))
	if got.Text != "[Image]\n识别一下图片" {
		t.Fatalf("message text = %q, want post placeholder plus text", got.Text)
	}
	if len(sender.downloadCalls) != 1 {
		t.Fatalf("download calls = %d, want 1", len(sender.downloadCalls))
	}
	call := sender.downloadCalls[0]
	if call.MessageID != "om_post_image" || call.FileKey != "img_post_key" || call.Type != "image" {
		t.Fatalf("download params wrong: %+v", call)
	}
	if len(storage.uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(storage.uploads))
	}
	if len(got.MediaRefs) != 1 {
		t.Fatalf("media refs = %+v, want 1", got.MediaRefs)
	}
	ref := got.MediaRefs[0]
	if ref.Type != channel.MsgTypeImage || ref.Filename != "post-image.png" || ref.MimeType != "image/png" ||
		ref.SizeBytes != 3 || ref.StorageURL == "" || ref.StorageKey == "" {
		t.Fatalf("post image ref wrong: %+v", ref)
	}
}

func TestFeishuMediaResolver_AttachesPostEmbeddedVideoMediaRef(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{
		Data:        []byte("mp4"),
		ContentType: "video/mp4",
		Filename:    "demo.mp4",
		SizeBytes:   3,
	}}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	rawPost := `{"content":[[{"tag":"text","text":"看一下视频"},{"tag":"media","file_key":"file_post_key","file_name":"demo.mp4"}]]}`
	lm := InboundMessage{
		EventID:      "evt-post-video",
		AppID:        "cli_app",
		ChatID:       "oc_dm",
		ChatType:     ChatTypeP2P,
		MessageID:    "om_post_video",
		SenderOpenID: "ou_user",
		MessageType:  "post",
		Body:         flattenPostContent(rawPost),
		Content:      rawPost,
	}
	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(lm))
	if len(sender.downloadCalls) != 1 {
		t.Fatalf("download calls = %d, want 1", len(sender.downloadCalls))
	}
	call := sender.downloadCalls[0]
	if call.MessageID != "om_post_video" || call.FileKey != "file_post_key" || call.Type != "file" {
		t.Fatalf("download params wrong: %+v", call)
	}
	if len(got.MediaRefs) != 1 || got.MediaRefs[0].Type != channel.MsgTypeVideo || got.MediaRefs[0].Filename != "demo.mp4" {
		t.Fatalf("post video ref wrong: %+v", got.MediaRefs)
	}
}

func TestFeishuMediaResolver_AttachesVideoMediaRef(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{
		Data:        []byte("mp4"),
		ContentType: "video/mp4",
		SizeBytes:   3,
	}}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
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
	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(lm))
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

func TestFeishuMediaResolver_RetryReusesObjectKey(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{
		Data:        []byte{1, 2, 3},
		ContentType: "image/png",
		Filename:    "shot.png",
	}}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	lm := InboundMessage{
		MessageID:   "om_retry",
		MessageType: "image",
		Body:        "[Image]",
		Content:     `{"image_key":"img_retry"}`,
	}

	for range 2 {
		got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
			uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), channelMessageFromLark(lm))
		if len(got.MediaRefs) != 1 {
			t.Fatalf("media refs = %+v, want 1", got.MediaRefs)
		}
	}
	if len(storage.uploads) != 2 {
		t.Fatalf("uploads = %d, want 2 retry attempts", len(storage.uploads))
	}
	if storage.uploads[0].key != storage.uploads[1].key {
		t.Fatalf("retry object keys differ: %q vs %q", storage.uploads[0].key, storage.uploads[1].key)
	}
}

func TestFeishuMediaResolver_DownloadFailurePreservesMessage(t *testing.T) {
	sender := &fakeSender{downloadErr: errors.New("download unavailable")}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	lm := InboundMessage{
		MessageID:   "om_download_failure",
		MessageType: "image",
		Body:        "[Image]",
		Content:     `{"image_key":"img_failure"}`,
	}
	before := channelMessageFromLark(lm)

	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), before)
	if got.Text != before.Text || len(got.MediaRefs) != 0 {
		t.Fatalf("download failure changed message: before=%+v after=%+v", before, got)
	}
	if len(storage.uploads) != 0 {
		t.Fatalf("download failure uploaded %d objects", len(storage.uploads))
	}
}

func TestFeishuMediaResolver_UploadFailurePreservesMessage(t *testing.T) {
	sender := &fakeSender{downloaded: DownloadedResource{Data: []byte{1}, ContentType: "image/png"}}
	storage := &fakeMediaStorage{err: errors.New("storage unavailable")}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	lm := InboundMessage{
		MessageID:   "om_upload_failure",
		MessageType: "image",
		Body:        "[Image]",
		Content:     `{"image_key":"img_failure"}`,
	}
	before := channelMessageFromLark(lm)

	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), before)
	if got.Text != before.Text || len(got.MediaRefs) != 0 {
		t.Fatalf("upload failure changed message: before=%+v after=%+v", before, got)
	}
	// NO inline delete: the store may still be processing the PUT, and a
	// DELETE issued now could reorder with it. The intent row (recorded
	// before the upload) is the reclaim path — the reconciler settles it.
	if len(storage.deleted) != 0 {
		t.Fatalf("upload failure must not delete inline, got %v", storage.deleted)
	}
	if len(ledger.records) != 1 {
		t.Fatalf("intent must be recorded before the failed upload, records=%d", len(ledger.records))
	}
}

func TestFeishuMediaResolver_PostPartialFailureKeepsTextAndSuccessfulMedia(t *testing.T) {
	sender := &fakeSender{
		downloadedByKey: map[string]DownloadedResource{
			"img_ok": {Data: []byte{1}, ContentType: "image/png", Filename: "ok.png"},
		},
		downloadErrByKey: map[string]error{"video_failed": errors.New("video unavailable")},
	}
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewFeishuMediaResolver(sender, fakeCreds{secret: "plain"}, storage, ledger, newDiscardLogger())
	rawPost := `{"content":[[{"tag":"text","text":"inspect"},{"tag":"img","image_key":"img_ok"},{"tag":"media","file_key":"video_failed","file_name":"failed.mp4"}]]}`
	lm := InboundMessage{
		MessageID:   "om_partial",
		MessageType: "post",
		Body:        flattenPostContent(rawPost),
		Content:     rawPost,
	}
	before := channelMessageFromLark(lm)

	got := resolver.ResolveMedia(context.Background(), testMediaInstallation(t), engine.ResolvedIdentity{},
		uuidFromString(t, "22222222-2222-2222-2222-222222222222"), uuidFromString(t, "33333333-3333-4333-8333-333333333333"), before)
	if got.Text != before.Text {
		t.Fatalf("partial failure changed text: got %q want %q", got.Text, before.Text)
	}
	if len(sender.downloadCalls) != 2 || len(storage.uploads) != 1 || len(got.MediaRefs) != 1 {
		t.Fatalf("partial failure result: downloads=%d uploads=%d refs=%+v", len(sender.downloadCalls), len(storage.uploads), got.MediaRefs)
	}
	if got.MediaRefs[0].Type != channel.MsgTypeImage || got.MediaRefs[0].Filename != "ok.png" {
		t.Fatalf("successful media ref lost: %+v", got.MediaRefs[0])
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
