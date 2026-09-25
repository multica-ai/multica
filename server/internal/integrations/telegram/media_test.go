package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---- inbound translation ----

func TestInboundPhotoWithCaptionCarriesMediaAndPlaceholder(t *testing.T) {
	u := Update{UpdateID: 1, Message: &Message{
		MessageID: 7, From: &User{ID: 111, FirstName: "Ada"}, Chat: Chat{ID: 555, Type: "private"},
		Caption: "what is this",
		Photo: []PhotoSize{
			{FileID: "small", FileUniqueID: "u-small", Width: 90, Height: 60},
			{FileID: "large", FileUniqueID: "u-large", Width: 1280, Height: 853, FileSize: 120000},
		},
	}}
	msg, ok := inboundFromUpdate(u, 999, "my_bot")
	if !ok {
		t.Fatal("photo must be ingested")
	}
	if msg.Type != channel.MsgTypeImage {
		t.Fatalf("type = %q", msg.Type)
	}
	if msg.Text != "[Image]\nwhat is this" || msg.CommandText != "what is this" {
		t.Fatalf("text=%q command=%q", msg.Text, msg.CommandText)
	}
	raw, err := decodeTelegramRaw(msg)
	if err != nil || raw.Media == nil {
		t.Fatalf("raw media missing: %+v err=%v", raw, err)
	}
	if raw.Media.FileID != "large" || raw.Media.Kind != channel.MsgTypeImage || raw.Media.Placeholder != "[Image]" || raw.Media.FileSize != 120000 {
		t.Fatalf("media = %+v, want the largest rendition", *raw.Media)
	}
	if raw.Media.PlaceholderIndex != 0 {
		t.Fatalf("a p2p photo has nothing ahead of its placeholder, index = %d", raw.Media.PlaceholderIndex)
	}
}

// nthOccurrence scans the way the engine does (channel/engine/session.go
// nthSubstringIndex): non-overlapping, left to right, zero-based.
func nthOccurrence(s, marker string, n int) int {
	offset := 0
	for i := 0; ; i++ {
		found := strings.Index(s[offset:], marker)
		if found < 0 {
			return -1
		}
		found += offset
		if i == n {
			return found
		}
		offset = found + len(marker)
	}
}

// A member who typed the marker in the recent window must not receive the
// sender's file: the index the resolver carries skips their literal.
func TestInboundPlaceholderIndexSkipsLiteralsInRecentContext(t *testing.T) {
	var handled []channel.InboundMessage
	c := &telegramChannel{
		botID: 999, botUsername: "my_bot", acceptsMedia: true,
		handler: func(_ context.Context, msg channel.InboundMessage) error { handled = append(handled, msg); return nil },
		logger:  testLogger(),
		recent:  newRecentContextBuffer(DefaultRecentContextSize),
	}
	ada := &User{ID: 111, FirstName: "Ada"}
	bob := &User{ID: 222, FirstName: "Bob"}
	ctx := context.Background()
	if err := c.dispatch(ctx, groupUpdate(1, ada, "I pasted [Image] in the doc, not here", nil, 0)); err != nil {
		t.Fatal(err)
	}
	photo := groupUpdate(2, bob, "", nil, 0)
	photo.Message.Caption = "@my_bot what is wrong here?"
	photo.Message.CaptionEntities = []MessageEntity{{Type: "mention", Offset: 0, Length: 7}}
	photo.Message.Photo = []PhotoSize{{FileID: "p1"}}
	if err := c.dispatch(ctx, photo); err != nil {
		t.Fatal(err)
	}
	if len(handled) != 2 || !handled[1].AddressedToBot {
		t.Fatalf("handler calls = %+v", handled)
	}
	msg := handled[1]
	raw, _ := decodeTelegramRaw(msg)
	if raw.Media == nil || raw.Media.PlaceholderIndex != 1 {
		t.Fatalf("media = %+v, want placeholder index 1 (Ada's literal is occurrence 0)", raw.Media)
	}
	contextEnd := strings.Index(msg.Text, "</recent_context>")
	if contextEnd < 0 {
		t.Fatalf("no recent context in\n%s", msg.Text)
	}
	if pos := nthOccurrence(msg.Text, raw.Media.Placeholder, raw.Media.PlaceholderIndex); pos < contextEnd {
		t.Fatalf("occurrence %d is inside the context block (at %d, block ends %d):\n%s", raw.Media.PlaceholderIndex, pos, contextEnd, msg.Text)
	}
	if pos := nthOccurrence(msg.Text, raw.Media.Placeholder, 0); pos > contextEnd {
		t.Fatalf("Ada's literal should be occurrence 0, found at %d:\n%s", pos, msg.Text)
	}
}

// Same for an explicitly quoted human message that contains the marker.
func TestInboundPlaceholderIndexSkipsLiteralsInQuotedMessage(t *testing.T) {
	quoted := &Message{MessageID: 9, From: &User{ID: 111, FirstName: "Ada"}, Text: "the doc says [Image] where the diagram goes"}
	u := groupUpdate(10, &User{ID: 222, FirstName: "Bob"}, "", quoted, 0)
	u.Message.Caption = "@my_bot like this?"
	u.Message.CaptionEntities = []MessageEntity{{Type: "mention", Offset: 0, Length: 7}}
	u.Message.Photo = []PhotoSize{{FileID: "p1"}}

	msg, ok := inboundFromUpdate(u, 999, "my_bot")
	if !ok || !msg.HasSelectedContext {
		t.Fatalf("ok=%v selected=%v", ok, msg.HasSelectedContext)
	}
	raw, _ := decodeTelegramRaw(msg)
	if raw.Media == nil || raw.Media.PlaceholderIndex != 1 {
		t.Fatalf("media = %+v, want placeholder index 1", raw.Media)
	}
	quoteEnd := strings.Index(msg.Text, "</quoted_message>")
	if pos := nthOccurrence(msg.Text, raw.Media.Placeholder, raw.Media.PlaceholderIndex); quoteEnd < 0 || pos < quoteEnd {
		t.Fatalf("occurrence %d is not on Bob's line (at %d, quote ends %d):\n%s", raw.Media.PlaceholderIndex, pos, quoteEnd, msg.Text)
	}
}

func TestInboundMediaKindsAndPlaceholders(t *testing.T) {
	for _, tc := range []struct {
		name        string
		m           Message
		wantType    channel.MsgType
		wantText    string
		wantName    string
		wantNoMedia bool
	}{
		{name: "document", m: Message{Document: &FileRef{FileID: "d", FileName: "report.pdf", MimeType: "application/pdf"}},
			wantType: channel.MsgTypeFile, wantText: "[File: report.pdf]", wantName: "report.pdf"},
		{name: "video", m: Message{Video: &FileRef{FileID: "v", MimeType: "video/mp4"}},
			wantType: channel.MsgTypeVideo, wantText: "[Video]"},
		{name: "animation", m: Message{Animation: &FileRef{FileID: "a", MimeType: "video/mp4"}},
			wantType: channel.MsgTypeVideo, wantText: "[Animation]"},
		{name: "video note", m: Message{VideoNote: &FileRef{FileID: "vn"}},
			wantType: channel.MsgTypeVideo, wantText: "[Video note]"},
		{name: "voice", m: Message{Voice: &FileRef{FileID: "vo", MimeType: "audio/ogg"}},
			wantType: channel.MsgTypeAudio, wantText: "[Voice message]"},
		{name: "audio", m: Message{Audio: &FileRef{FileID: "au", FileName: "song.mp3", MimeType: "audio/mpeg"}},
			wantType: channel.MsgTypeAudio, wantText: "[Audio]", wantName: "song.mp3"},
		{name: "sticker", m: Message{Sticker: &struct{}{}}, wantType: channel.MsgTypeUnknown, wantText: "", wantNoMedia: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.m
			m.MessageID, m.From, m.Chat = 7, &User{ID: 111}, Chat{ID: 555, Type: "private"}
			msg, ok := inboundFromUpdate(Update{UpdateID: 1, Message: &m}, 999, "my_bot")
			if !ok {
				t.Fatal("expected ok")
			}
			if msg.Type != tc.wantType || msg.Text != tc.wantText {
				t.Fatalf("type=%q text=%q", msg.Type, msg.Text)
			}
			raw, _ := decodeTelegramRaw(msg)
			if tc.wantNoMedia {
				if raw.Media != nil {
					t.Fatalf("unexpected media %+v", *raw.Media)
				}
				return
			}
			if raw.Media == nil || raw.Media.FileName != tc.wantName || raw.Media.Placeholder != tc.wantText {
				t.Fatalf("media = %+v", raw.Media)
			}
		})
	}
}

func TestInboundControlCommandWithPhotoKeepsMediaTurn(t *testing.T) {
	u := Update{UpdateID: 1, Message: &Message{
		MessageID: 7, From: &User{ID: 111}, Chat: Chat{ID: 555, Type: "private"},
		Caption: "/new", Photo: []PhotoSize{{FileID: "p"}},
	}}
	msg, ok := inboundFromUpdate(u, 999, "my_bot")
	if !ok || msg.CommandText != "/new" || msg.Text != "[Image]" {
		t.Fatalf("ok=%v command=%q text=%q", ok, msg.CommandText, msg.Text)
	}
}

func TestDispatchForwardsPhotoToEngine(t *testing.T) {
	var handled []channel.InboundMessage
	c := &telegramChannel{
		botID: 999, botUsername: "my_bot",
		acceptsMedia: true,
		api:          newBotAPI("http://127.0.0.1:1", "123:secret", nil),
		handler:      func(_ context.Context, m channel.InboundMessage) error { handled = append(handled, m); return nil },
		logger:       testLogger(),
	}
	u := Update{UpdateID: 1, Message: &Message{
		MessageID: 21, From: &User{ID: 3}, Chat: Chat{ID: 42, Type: "private"},
		Photo: []PhotoSize{{FileID: "p1"}},
	}}
	if err := c.dispatch(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if len(handled) != 1 || handled[0].Text != "[Image]" {
		t.Fatalf("handled = %+v", handled)
	}
}

// ---- media resolver ----

// fakeObjectStore is the one object store fake both directions share: what
// Upload wrote is what GetReader hands back, keyed under https://cdn.example/.
type fakeObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	names   map[string]string
	types   map[string]string
}

func newFakeObjectStore(objects map[string][]byte) *fakeObjectStore {
	if objects == nil {
		objects = map[string][]byte{}
	}
	return &fakeObjectStore{objects: objects, names: map[string]string{}, types: map[string]string{}}
}

func (f *fakeObjectStore) Upload(_ context.Context, key string, data []byte, contentType, filename string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = append([]byte(nil), data...)
	f.names[key] = filename
	f.types[key] = contentType
	return f.ObjectURL(key), nil
}

func (f *fakeObjectStore) ObjectURL(key string) string { return "https://cdn.example/" + key }

func (f *fakeObjectStore) KeyFromURL(rawURL string) string {
	return strings.TrimPrefix(rawURL, "https://cdn.example/")
}

func (f *fakeObjectStore) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("no object %q", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type fakeMediaLedger struct {
	records []engine.RecordPendingMediaObjectParams
	refuse  bool
}

func (f *fakeMediaLedger) RecordPendingMediaObject(_ context.Context, p engine.RecordPendingMediaObjectParams) (bool, error) {
	f.records = append(f.records, p)
	return !f.refuse, nil
}

// fakeBotFiles serves getFile and the file download host for one token.
type fakeBotFiles struct {
	files        map[string]fakeBotFile // file_id -> file
	getFileCalls int
	downloads    []string
	// notices records the sendMessage calls: the fetch-failure notice.
	notices []sendMessageParams
}

type fakeBotFile struct {
	path        string
	contentType string
	data        []byte
}

func (f *fakeBotFiles) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/getFile"):
		f.getFileCalls++
		var body struct {
			FileID string `json:"file_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		file, ok := f.files[body.FileID]
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: file not found"}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"file_id":%q,"file_size":%d,"file_path":%q}}`, body.FileID, len(file.data), file.path)
	case strings.HasPrefix(r.URL.Path, "/file/bot123:secret/"):
		rel := strings.TrimPrefix(r.URL.Path, "/file/bot123:secret/")
		f.downloads = append(f.downloads, rel)
		for _, file := range f.files {
			if file.path == rel {
				if file.contentType != "" {
					w.Header().Set("Content-Type", file.contentType)
				}
				_, _ = w.Write(file.data)
				return
			}
		}
		http.NotFound(w, r)
	case strings.HasSuffix(r.URL.Path, "/sendMessage"):
		var p sendMessageParams
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.notices = append(f.notices, p)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9,"chat":{"id":42,"type":"private"}}}`))
	default:
		http.Error(w, "unexpected "+r.URL.Path, http.StatusTeapot)
	}
}

func mediaResolverFixture(t *testing.T, media *inboundMedia) (engine.ResolvedInstallation, pgtype.UUID, channel.InboundMessage) {
	t.Helper()
	cfg, _ := json.Marshal(installConfig{AppID: "999", BotTokenEncrypted: base64.StdEncoding.EncodeToString([]byte("123:secret"))})
	var ws, instID, chatMessageID pgtype.UUID
	ws.Bytes[0], instID.Bytes[0], chatMessageID.Bytes[0] = 0xAB, 0xCD, 0xEF
	ws.Valid, instID.Valid, chatMessageID.Valid = true, true, true
	raw, _ := json.Marshal(telegramRawEvent{BotID: "999", EventType: "message", Media: media})
	inst := engine.ResolvedInstallation{ID: instID, WorkspaceID: ws, Platform: db.ChannelInstallation{Config: cfg}}
	text := "hello"
	if media != nil {
		text = media.Placeholder
	}
	return inst, chatMessageID, channel.InboundMessage{MessageID: "42:7", Source: channel.Source{ChatID: "42"}, Type: channel.MsgTypeText, Text: text, Raw: raw}
}

func TestMediaResolverHasMedia(t *testing.T) {
	r := NewMediaResolver(nil, newFakeObjectStore(nil), &fakeMediaLedger{}, "", nil, testLogger())
	_, _, with := mediaResolverFixture(t, &inboundMedia{Kind: channel.MsgTypeImage, FileID: "p", Placeholder: "[Image]"})
	_, _, without := mediaResolverFixture(t, nil)
	if !r.HasMedia(with) || r.HasMedia(without) {
		t.Fatalf("HasMedia with=%v without=%v", r.HasMedia(with), r.HasMedia(without))
	}
}

func TestMediaResolverIngestsPhoto(t *testing.T) {
	jpeg := []byte("\xff\xd8\xff\xe0fake-jpeg")
	bot := &fakeBotFiles{files: map[string]fakeBotFile{"p1": {path: "photos/file_9.jpg", data: jpeg}}}
	srv := httptest.NewServer(http.HandlerFunc(bot.serve))
	defer srv.Close()
	store, ledger := newFakeObjectStore(nil), &fakeMediaLedger{}
	r := NewMediaResolver(nil, store, ledger, srv.URL, srv.Client(), testLogger())
	inst, chatMessageID, msg := mediaResolverFixture(t, &inboundMedia{Kind: channel.MsgTypeImage, FileID: "p1", FileUniqueID: "u1", MimeType: "image/jpeg", Placeholder: "[Image]", PlaceholderIndex: 1})

	got := r.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 1 {
		t.Fatalf("media refs = %+v", got.MediaRefs)
	}
	ref := got.MediaRefs[0]
	if ref.Type != channel.MsgTypeImage || ref.MimeType != "image/jpeg" || ref.Filename != "file_9.jpg" || ref.SizeBytes != int64(len(jpeg)) || ref.InlinePlaceholder != "[Image]" || ref.InlineIndex != 1 {
		t.Fatalf("ref = %+v", ref)
	}
	if !strings.HasPrefix(ref.StorageKey, "workspaces/") || !strings.Contains(ref.StorageKey, "/telegram/") || ref.StorageURL != store.ObjectURL(ref.StorageKey) {
		t.Fatalf("key/url = %q %q", ref.StorageKey, ref.StorageURL)
	}
	if !bytes.Equal(store.objects[ref.StorageKey], jpeg) {
		t.Fatal("uploaded bytes differ from the download")
	}
	if len(ledger.records) != 1 || ledger.records[0].StorageKey != ref.StorageKey || ledger.records[0].ChatMessageID != chatMessageID {
		t.Fatalf("ledger = %+v", ledger.records)
	}
	if bot.getFileCalls != 1 || len(bot.downloads) != 1 || bot.downloads[0] != "photos/file_9.jpg" {
		t.Fatalf("bot calls: getFile=%d downloads=%v", bot.getFileCalls, bot.downloads)
	}
}

// The fake file host sets no Content-Type, so net/http sniffs one for the
// response; this covers the "declared mime type absent" path end to end.
// mediaContentType's own byte sniff is covered directly below.
func TestMediaResolverKeepsSenderFilenameWithoutDeclaredType(t *testing.T) {
	pdf := []byte("%PDF-1.4 body")
	bot := &fakeBotFiles{files: map[string]fakeBotFile{"d1": {path: "documents/file_3", data: pdf}}}
	srv := httptest.NewServer(http.HandlerFunc(bot.serve))
	defer srv.Close()
	store := newFakeObjectStore(nil)
	r := NewMediaResolver(nil, store, &fakeMediaLedger{}, srv.URL, srv.Client(), testLogger())
	inst, chatMessageID, msg := mediaResolverFixture(t, &inboundMedia{Kind: channel.MsgTypeFile, FileID: "d1", FileName: "../report", Placeholder: "[File: report]"})

	got := r.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 1 {
		t.Fatalf("media refs = %+v", got.MediaRefs)
	}
	if ref := got.MediaRefs[0]; ref.Filename != "report.pdf" || ref.MimeType != "application/pdf" {
		t.Fatalf("ref = %+v", ref)
	}
}

func TestMediaContentTypePrecedence(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 16))
	for _, tc := range []struct{ declared, response, want string }{
		{"image/webp", "image/png", "image/webp"},
		{"", "image/png", "image/png"},
		{"", "application/octet-stream", "image/png"},
		{"", "", "image/png"},
	} {
		if got := mediaContentType(tc.declared, tc.response, png); got != tc.want {
			t.Errorf("mediaContentType(%q, %q) = %q, want %q", tc.declared, tc.response, got, tc.want)
		}
	}
}

func TestMediaResolverRefusesFailuresWithoutTouchingStorage(t *testing.T) {
	bot := &fakeBotFiles{files: map[string]fakeBotFile{}}
	srv := httptest.NewServer(http.HandlerFunc(bot.serve))
	defer srv.Close()
	for _, tc := range []struct {
		name         string
		media        *inboundMedia
		refuseLedger bool
		wantLedger   int
	}{
		{name: "oversize declared up front", media: &inboundMedia{Kind: channel.MsgTypeFile, FileID: "big", FileSize: maxBotDownloadBytes + 1, Placeholder: "[File: big]"}},
		{name: "getFile fails", media: &inboundMedia{Kind: channel.MsgTypeImage, FileID: "missing", Placeholder: "[Image]"}},
		{name: "reconciler owns the key", media: &inboundMedia{Kind: channel.MsgTypeImage, FileID: "p1", Placeholder: "[Image]"}, refuseLedger: true, wantLedger: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot.files["p1"] = fakeBotFile{path: "photos/p1.jpg", data: []byte("x")}
			before := len(bot.notices)
			store, ledger := newFakeObjectStore(nil), &fakeMediaLedger{refuse: tc.refuseLedger}
			r := NewMediaResolver(nil, store, ledger, srv.URL, srv.Client(), testLogger())
			inst, chatMessageID, msg := mediaResolverFixture(t, tc.media)
			got := r.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
			if len(got.MediaRefs) != 0 || got.Text != msg.Text {
				t.Fatalf("message must come back unchanged: %+v", got)
			}
			if len(store.objects) != 0 || len(ledger.records) != tc.wantLedger {
				t.Fatalf("uploads=%d ledger=%d", len(store.objects), len(ledger.records))
			}
			if len(bot.notices) != before+1 {
				t.Fatalf("notices = %+v, want one more than %d", bot.notices, before)
			}
			if got := bot.notices[before]; got.Text != msgMediaUnavailable || got.ChatID != 42 || got.ReplyParameters == nil || got.ReplyParameters.MessageID != 7 {
				t.Fatalf("notice = %+v", got)
			}
		})
	}
}

// ---- multipart client ----

type multipartRecord struct {
	method   string
	fields   map[string]string
	part     string
	filename string
	partType string
	data     []byte
}

func recordMultipart(t *testing.T, r *http.Request) multipartRecord {
	t.Helper()
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		t.Fatalf("parse multipart: %v", err)
	}
	rec := multipartRecord{method: r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], fields: map[string]string{}}
	for k, v := range r.MultipartForm.Value {
		rec.fields[k] = v[0]
	}
	for name, headers := range r.MultipartForm.File {
		f, err := headers[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		rec.data, _ = io.ReadAll(f)
		_ = f.Close()
		rec.part, rec.filename, rec.partType = name, headers[0].Filename, headers[0].Header.Get("Content-Type")
	}
	return rec
}

func TestSendMediaPostsMultipart(t *testing.T) {
	var got multipartRecord
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = recordMultipart(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":8,"chat":{"id":-42,"type":"supergroup"}}}`))
	}))
	defer srv.Close()
	m, err := newBotAPI(srv.URL, "123:secret", srv.Client()).SendMedia(context.Background(), sendMediaParams{
		ChatID: -42, MessageThreadID: 8, Field: "photo", Filename: "shot.png", ContentType: "image/png", Data: []byte("png-bytes"),
	})
	if err != nil || m.MessageID != 8 {
		t.Fatalf("message=%+v err=%v", m, err)
	}
	if got.method != "sendPhoto" || got.fields["chat_id"] != "-42" || got.fields["message_thread_id"] != "8" {
		t.Fatalf("request = %+v", got)
	}
	if got.part != "photo" || got.filename != "shot.png" || got.partType != "image/png" || string(got.data) != "png-bytes" {
		t.Fatalf("part = %+v", got)
	}
}

// ---- attachment hop ----

// attachmentOnlyQueries answers the one query the hop runs; everything else on
// outboundQueries is unreachable from sendAttachments and stays nil.
type attachmentOnlyQueries struct {
	outboundQueries
	rows []db.Attachment
	err  error
	// failFirst makes that many lookups return err before rows are served;
	// zero with a non-nil err fails every lookup. calls counts them all.
	failFirst int
	calls     int
}

func (q *attachmentOnlyQueries) ListAttachmentsByChatMessage(context.Context, db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error) {
	q.calls++
	if q.err != nil && (q.failFirst == 0 || q.calls <= q.failFirst) {
		return nil, q.err
	}
	return q.rows, nil
}

func attachmentRow(id byte, url, name, contentType string, size int64) db.Attachment {
	return db.Attachment{ID: telegramTestUUID(id), Url: url, Filename: name, ContentType: contentType, SizeBytes: size}
}

// fakeBotUploads records every send and answers each method per the table.
type fakeBotUploads struct {
	mu       sync.Mutex
	sends    []multipartRecord
	messages []string
	reject   map[string]string // method -> error description (400)
}

func (f *fakeBotUploads) serve(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		if method == "sendMessage" || method == "editMessageText" {
			var body sendMessageParams
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.messages = append(f.messages, body.Text)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":3,"chat":{"id":42,"type":"private"}}}`))
			return
		}
		rec := recordMultipart(t, r)
		f.sends = append(f.sends, rec)
		if desc, refused := f.reject[method]; refused {
			_, _ = fmt.Fprintf(w, `{"ok":false,"error_code":400,"description":%q}`, desc)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":4,"chat":{"id":42,"type":"private"}}}`))
	}
}

func newAttachmentOutbound(t *testing.T, bot *fakeBotUploads, q outboundQueries, objects map[string][]byte) (*Outbound, replyTarget) {
	t.Helper()
	srv := httptest.NewServer(bot.serve(t))
	t.Cleanup(srv.Close)
	o := NewOutbound(q, nil, srv.URL, srv.Client(), testLogger())
	o.EnableFileDelivery(newFakeObjectStore(objects))
	o.spawn = func(f func()) { f() }
	o.wait = func(context.Context, time.Duration) error { return nil }
	return o, replyTarget{chatID: 42, threadID: 8, botToken: "123:secret"}
}

// The lookup is a side-effect-free read: a failure is retried, and only one
// that keeps failing is reported — in words that do not presume a file existed.
func TestDeliverAttachmentsTellsUserWhenTheLookupKeepsFailing(t *testing.T) {
	bot := &fakeBotUploads{}
	q := &attachmentOnlyQueries{err: errors.New("database unavailable")}
	o, target := newAttachmentOutbound(t, bot, q, nil)
	o.spawn = func(func()) { t.Fatal("nothing is known to deliver") }

	o.deliverAttachments(context.Background(), attachmentReply(target))

	if q.calls != attachmentLookupAttempts {
		t.Fatalf("lookup attempts = %d, want %d", q.calls, attachmentLookupAttempts)
	}
	if len(bot.sends) != 0 || len(bot.messages) != 1 || bot.messages[0] != attachmentLookupFailedText {
		t.Fatalf("sends=%+v messages=%q", bot.sends, bot.messages)
	}
}

func TestDeliverAttachmentsRetriesATransientLookupFailure(t *testing.T) {
	bot := &fakeBotUploads{}
	q := &attachmentOnlyQueries{err: errors.New("database unavailable"), failFirst: attachmentLookupAttempts - 1,
		rows: []db.Attachment{attachmentRow(1, "https://cdn.example/k/shot.png", "shot.png", "image/png", 3)}}
	o, target := newAttachmentOutbound(t, bot, q, map[string][]byte{"k/shot.png": []byte("png")})

	o.deliverAttachments(context.Background(), attachmentReply(target))

	if q.calls != attachmentLookupAttempts {
		t.Fatalf("lookup attempts = %d, want %d", q.calls, attachmentLookupAttempts)
	}
	if len(bot.sends) != 1 || bot.sends[0].method != "sendPhoto" || len(bot.messages) != 0 {
		t.Fatalf("sends=%+v messages=%q", bot.sends, bot.messages)
	}
}

func TestSendAttachmentsPicksMethodByContentType(t *testing.T) {
	bot := &fakeBotUploads{}
	o, target := newAttachmentOutbound(t, bot, &attachmentOnlyQueries{rows: []db.Attachment{
		attachmentRow(1, "https://cdn.example/k/shot", "shot.png", "image/png", 3),
		attachmentRow(2, "https://cdn.example/k/report.pdf", "report.pdf", "application/pdf", 3),
		attachmentRow(3, "https://cdn.example/k/clip.mp4", "clip.mp4", "video/mp4", 3),
		attachmentRow(4, "https://cdn.example/k/notes", "notes", "text/plain", 3),
	}}, map[string][]byte{
		"k/shot": []byte("png"), "k/report.pdf": []byte("pdf"), "k/clip.mp4": []byte("mp4"), "k/notes": []byte("txt"),
	})

	o.deliverAttachments(context.Background(), attachmentReply(target))

	want := []struct{ method, part, filename string }{
		{"sendPhoto", "photo", "shot.png"},
		{"sendDocument", "document", "report.pdf"},
		{"sendVideo", "video", "clip.mp4"},
		{"sendDocument", "document", "notes.txt"},
	}
	if len(bot.sends) != len(want) {
		t.Fatalf("sends = %+v", bot.sends)
	}
	for i, w := range want {
		got := bot.sends[i]
		if got.method != w.method || got.part != w.part || got.filename != w.filename || got.fields["chat_id"] != "42" || got.fields["message_thread_id"] != "8" {
			t.Errorf("send %d = %+v, want %+v", i, got, w)
		}
	}
	if len(bot.messages) != 0 {
		t.Fatalf("no failure notice expected, got %q", bot.messages)
	}
}

func TestSendAttachmentsFallsBackToDocumentOnPhotoRejection(t *testing.T) {
	bot := &fakeBotUploads{reject: map[string]string{"sendPhoto": "Bad Request: IMAGE_PROCESS_FAILED"}}
	o, target := newAttachmentOutbound(t, bot, &attachmentOnlyQueries{rows: []db.Attachment{
		attachmentRow(1, "https://cdn.example/k/odd.png", "odd.png", "image/png", 3),
	}}, map[string][]byte{"k/odd.png": []byte("png")})

	o.deliverAttachments(context.Background(), attachmentReply(target))

	if len(bot.sends) != 2 || bot.sends[0].method != "sendPhoto" || bot.sends[1].method != "sendDocument" || bot.sends[1].filename != "odd.png" {
		t.Fatalf("sends = %+v", bot.sends)
	}
	if len(bot.messages) != 0 {
		t.Fatalf("a delivered fallback needs no notice, got %q", bot.messages)
	}
}

func TestSendAttachmentsTellsUserAboutFilesThatDidNotArrive(t *testing.T) {
	bot := &fakeBotUploads{reject: map[string]string{"sendDocument": "Bad Request: file is too big"}}
	o, target := newAttachmentOutbound(t, bot, &attachmentOnlyQueries{rows: []db.Attachment{
		attachmentRow(1, "https://cdn.example/k/ok.png", "ok.png", "image/png", 3),
		attachmentRow(2, "https://cdn.example/k/missing.pdf", "missing.pdf", "application/pdf", 3),
		attachmentRow(3, "https://cdn.example/k/refused.pdf", "refused.pdf", "application/pdf", 3),
	}}, map[string][]byte{"k/ok.png": []byte("png"), "k/refused.pdf": []byte("pdf")})

	o.deliverAttachments(context.Background(), attachmentReply(target))

	if len(bot.sends) != 2 || bot.sends[0].method != "sendPhoto" || bot.sends[1].method != "sendDocument" {
		t.Fatalf("sends = %+v", bot.sends)
	}
	if len(bot.messages) != 1 || bot.messages[0] != attachmentNoticeText {
		t.Fatalf("notice = %q", bot.messages)
	}
}

// attachmentReply is a settled reply whose assistant message the fake queries
// answer for.
func attachmentReply(target replyTarget) *terminalReply {
	return &terminalReply{event: events.Event{WorkspaceID: "00000000-0000-0000-0000-000000000008",
		Payload: protocol.ChatDonePayload{MessageID: "00000000-0000-0000-0000-000000000009"}}, target: &target}
}

func TestDeliverAttachmentsSpawnsNothingWithoutStorageOrRows(t *testing.T) {
	bot := &fakeBotUploads{}
	o, target := newAttachmentOutbound(t, bot, &attachmentOnlyQueries{}, nil)
	o.spawn = func(func()) { t.Fatal("spawned a delivery with nothing to deliver") }
	o.deliverAttachments(context.Background(), attachmentReply(target))

	o.objects = nil
	o.deliverAttachments(context.Background(), attachmentReply(target))
	if len(bot.sends) != 0 || len(bot.messages) != 0 {
		t.Fatalf("unexpected traffic: sends=%+v messages=%q", bot.sends, bot.messages)
	}
}

// Admission is decided before the spawn: with every slot busy, a reply that
// has files is shed with the notice rather than parked behind a goroutine.
func TestDeliverAttachmentsShedsWhenEverySlotIsBusy(t *testing.T) {
	for range maxConcurrentAttachmentDeliveries {
		attachmentSlots <- struct{}{}
	}
	defer func() {
		for range maxConcurrentAttachmentDeliveries {
			<-attachmentSlots
		}
	}()
	bot := &fakeBotUploads{}
	o, target := newAttachmentOutbound(t, bot, &attachmentOnlyQueries{rows: []db.Attachment{
		attachmentRow(1, "https://cdn.example/k/shot.png", "shot.png", "image/png", 3),
	}}, map[string][]byte{"k/shot.png": []byte("png")})
	o.spawn = func(func()) { t.Fatal("a shed delivery must not be spawned") }

	o.deliverAttachments(context.Background(), attachmentReply(target))

	if len(bot.sends) != 0 || len(bot.messages) != 1 || bot.messages[0] != attachmentNoticeText {
		t.Fatalf("sends=%+v messages=%q", bot.sends, bot.messages)
	}
}

func TestOutboundMediaHelpers(t *testing.T) {
	for _, tc := range []struct {
		contentType string
		size        int
		want        string
	}{
		{"image/jpeg", 10, "photo"},
		{"image/png; charset=binary", 10, "photo"},
		{"image/svg+xml", 10, "document"},
		{"image/png", maxOutboundPhotoBytes + 1, "document"},
		{"video/mp4", 10, "video"},
		{"video/quicktime", 10, "document"},
		{"audio/mpeg", 10, "audio"},
		{"application/pdf", 10, "document"},
		{"", 10, "document"},
	} {
		if got := outboundMediaField(tc.contentType, tc.size); got != tc.want {
			t.Errorf("outboundMediaField(%q, %d) = %q, want %q", tc.contentType, tc.size, got, tc.want)
		}
	}
	for _, tc := range []struct{ name, contentType, want string }{
		{"shot.png", "image/png", "shot.png"},
		{"", "image/jpeg", "attachment.jpg"},
		{"../etc/passwd", "text/plain", "passwd.txt"},
		{"notes", "text/plain", "notes.txt"},
	} {
		if got := outboundMediaName(tc.name, tc.contentType); got != tc.want {
			t.Errorf("outboundMediaName(%q, %q) = %q, want %q", tc.name, tc.contentType, got, tc.want)
		}
	}
}

// TestFinalReplyDeliversBoundAttachmentsAfterText runs the real delivery state
// machine: the answer lands first, then the files the agent bound to it.
func TestFinalReplyDeliversBoundAttachmentsAfterText(t *testing.T) {
	q := newTelegramOutboundQueries(t)
	q.channelOrigin = true
	q.attachments = []db.Attachment{attachmentRow(1, "https://cdn.example/k/shot.png", "shot.png", "image/png", 3)}
	bot := &fakeBotUploads{}
	o, _ := newAttachmentOutbound(t, bot, q, map[string][]byte{"k/shot.png": []byte("png")})

	done := telegramTestEvent()
	done.WorkspaceID = "00000000-0000-0000-0000-000000000008"
	done.Payload = protocol.ChatDonePayload{
		TaskID: done.TaskID, ChatSessionID: done.ChatSessionID,
		MessageID: "00000000-0000-0000-0000-000000000009", Content: "here is the screenshot",
	}
	if err := sendTerminalReplySynchronouslyForTest(context.Background(), o, done); err != nil {
		t.Fatalf("finish chat: %v", err)
	}
	if len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "here is the screenshot") {
		t.Fatalf("text messages = %q", bot.messages)
	}
	if len(bot.sends) != 1 || bot.sends[0].method != "sendPhoto" || bot.sends[0].filename != "shot.png" {
		t.Fatalf("sends = %+v", bot.sends)
	}
}

// TestEmptyReplyStillDeliversBoundAttachments covers an agent that said nothing
// and bound a file instead: the file is the whole reply.
func TestEmptyReplyStillDeliversBoundAttachments(t *testing.T) {
	q := newTelegramOutboundQueries(t)
	q.channelOrigin = true
	q.attachments = []db.Attachment{attachmentRow(1, "https://cdn.example/k/report.pdf", "report.pdf", "application/pdf", 3)}
	bot := &fakeBotUploads{}
	o, _ := newAttachmentOutbound(t, bot, q, map[string][]byte{"k/report.pdf": []byte("pdf")})

	done := telegramTestEvent()
	done.WorkspaceID = "00000000-0000-0000-0000-000000000008"
	done.Payload = protocol.ChatDonePayload{
		TaskID: done.TaskID, ChatSessionID: done.ChatSessionID,
		MessageID: "00000000-0000-0000-0000-000000000009", Content: "",
	}
	// enqueueTerminalReply files an empty completion as a close, not an answer.
	reply := &terminalReply{event: done, kind: terminalKindClose, settleReason: "empty_reply"}
	for {
		result := o.sendNextTerminalRequest(context.Background(), reply)
		if result.err != nil {
			t.Fatalf("close turn: %v", result.err)
		}
		if result.done {
			o.cleanupTerminalReply(reply)
			break
		}
	}
	if len(bot.messages) != 0 {
		t.Fatalf("no text expected, got %q", bot.messages)
	}
	if len(bot.sends) != 1 || bot.sends[0].method != "sendDocument" {
		t.Fatalf("sends = %+v", bot.sends)
	}
}

// Without object storage the loop keeps the old contract: media gets the
// unsupported notice, and never a placeholder the agent cannot see behind.
func TestDispatchKeepsUnsupportedNoticeWithoutStorage(t *testing.T) {
	var notices []sendMessageParams
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body sendMessageParams
		_ = json.NewDecoder(r.Body).Decode(&body)
		notices = append(notices, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer srv.Close()
	c := &telegramChannel{
		botID: 999, botUsername: "my_bot", api: newBotAPI(srv.URL, "123:secret", srv.Client()),
		handler: func(context.Context, channel.InboundMessage) error {
			t.Fatal("media reached the handler without storage")
			return nil
		},
		logger: testLogger(),
	}
	u := Update{UpdateID: 1, Message: &Message{
		MessageID: 21, From: &User{ID: 3}, Chat: Chat{ID: 42, Type: "private"},
		Caption: "look", Photo: []PhotoSize{{FileID: "p1"}},
	}}
	if err := c.dispatch(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || notices[0].Text != msgUnsupportedType || notices[0].ReplyParameters == nil || notices[0].ReplyParameters.MessageID != 21 {
		t.Fatalf("notices = %+v", notices)
	}
}

// fileOnlyCompletion is a chat:done with no text and a bound file.
func fileOnlyCompletion(e events.Event, taskID string) events.Event {
	done := e
	done.TaskID = taskID
	done.WorkspaceID = "00000000-0000-0000-0000-000000000008"
	done.Payload = protocol.ChatDonePayload{TaskID: taskID, ChatSessionID: e.ChatSessionID, MessageID: "00000000-0000-0000-0000-000000000009"}
	return done
}

func enableTestFileDelivery(o *Outbound, q *review8545Queries) {
	q.routing.attachments = []db.Attachment{attachmentRow(1, "https://cdn.example/k/report.pdf", "report.pdf", "application/pdf", 3)}
	o.EnableFileDelivery(newFakeObjectStore(map[string][]byte{"k/report.pdf": []byte("pdf")}))
	o.spawn = func(f func()) { f() }
}

// A file-only reply whose chat:done arrives twice — a replay, here on another
// replica — sends its files once: the second close finds the turn ended.
func TestReview8545PostgresDuplicateEmptyCompletionSendsFilesOnce(t *testing.T) {
	bot := &auditBot{}
	a, q, c, e := review8545Setup(t, bot)
	enableTestFileDelivery(a, q)
	done := fileOnlyCompletion(e, e.TaskID)
	a.enqueueTerminalReply(done)
	auditDrain(t, a, c, done.ChatSessionID)

	b := review8545Second(a)
	enableTestFileDelivery(b, q)
	b.enqueueTerminalReply(done)
	auditDrain(t, b, c, done.ChatSessionID)

	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.uploads != 1 || len(bot.messages) != 0 {
		t.Fatalf("uploads=%d messages=%v methods=%v", bot.uploads, bot.messages, bot.methods)
	}
}

// A file-only completion belonging to an attempt the retry chain has moved
// past must not send its files: the retry owns the turn and answers it.
func TestReview8545PostgresSupersededAttemptDoesNotSendFiles(t *testing.T) {
	bot := &auditBot{}
	o, q, c, e := review8545Setup(t, bot)
	enableTestFileDelivery(o, q)
	oldTask := e.TaskID
	retryTask := util.UUIDToString(review8545ID())
	seedRetryChain(t, oldTask, retryTask)

	// The retry takes the turn and starts streaming.
	o.handleTaskMessage(telegramPartialEvent(retryTask, "retry streaming"))

	// The attempt it superseded completes late, with a file bound.
	o.enqueueTerminalReply(fileOnlyCompletion(e, oldTask))
	auditDrain(t, o, c, e.ChatSessionID)

	retry := e
	retry.TaskID = retryTask
	retry.Payload = protocol.ChatDonePayload{TaskID: retryTask, ChatSessionID: e.ChatSessionID, Content: "retry complete answer"}
	o.enqueueTerminalReply(retry)
	auditDrain(t, o, c, retry.ChatSessionID)

	review8545MessageCount(t, bot, 1)
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.uploads != 0 {
		t.Fatalf("superseded attempt sent its files: uploads=%d methods=%v", bot.uploads, bot.methods)
	}
	if bot.messages[1] != "retry complete answer" {
		t.Fatalf("retry's answer not delivered: %q methods=%v", bot.messages[1], bot.methods)
	}
}
