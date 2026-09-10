package wecom

// media_ingest_test.go — the resolver, driven against a real HTTP server
// serving really-encrypted bytes. Nothing here stubs the download or the
// decrypt: the test server encrypts a known payload with the same algorithm
// WeCom uses, and the assertion is that exactly those bytes reach storage.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// ---- fakes for the two durable seams ----

type storedObject struct {
	key         string
	data        []byte
	contentType string
	filename    string
}

type fakeMediaStorage struct {
	mu      sync.Mutex
	uploads []storedObject
	err     error
}

func (s *fakeMediaStorage) Upload(_ context.Context, key string, data []byte, contentType, filename string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	s.uploads = append(s.uploads, storedObject{key: key, data: append([]byte{}, data...), contentType: contentType, filename: filename})
	return s.ObjectURL(key), nil
}

func (s *fakeMediaStorage) ObjectURL(key string) string { return "https://objects.invalid/" + key }

func (s *fakeMediaStorage) stored() []storedObject {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]storedObject{}, s.uploads...)
}

// fakeStreamStorage is fakeMediaStorage plus UploadStream, so the memory-flat
// path is exercised by the same assertions as the buffered one.
type fakeStreamStorage struct{ fakeMediaStorage }

func (s *fakeStreamStorage) UploadStream(_ context.Context, key string, data io.Reader, sizeBytes int64, contentType, filename string) (string, error) {
	body, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	if int64(len(body)) != sizeBytes {
		return "", io.ErrUnexpectedEOF
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploads = append(s.uploads, storedObject{key: key, data: body, contentType: contentType, filename: filename})
	return s.ObjectURL(key), nil
}

type fakeMediaLedger struct {
	mu      sync.Mutex
	records []engine.RecordPendingMediaObjectParams
	ok      bool
	err     error
	// seenUploads snapshots how many objects were already in storage when
	// each intent row was written, so the ordering can be asserted.
	seenUploads []int
	storage     *fakeMediaStorage
}

func newFakeMediaLedger(storage *fakeMediaStorage) *fakeMediaLedger {
	return &fakeMediaLedger{ok: true, storage: storage}
}

func (l *fakeMediaLedger) RecordPendingMediaObject(_ context.Context, p engine.RecordPendingMediaObjectParams) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, p)
	if l.storage != nil {
		l.seenUploads = append(l.seenUploads, len(l.storage.stored()))
	}
	return l.ok, l.err
}

// notifierWithLiveSocket is the production notifier — a real sendersRegistry
// — holding a wsSender over a recording connection, so a failure notice is
// asserted as the aibot frame it actually is.
func notifierWithLiveSocket(id pgtype.UUID) (*sendersRegistry, *recordingConn) {
	conn := &recordingConn{}
	reg := newSendersRegistry()
	reg.set(id, newWSSender(conn, slog.Default()))
	return reg, conn
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func uuidOf(n byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[15] = n
	return u
}

// ---- harness ----

// cosServer stands in for the Tencent COS address a callback points at: it
// serves one encrypted payload, with whatever Content-Disposition the test
// wants.
func cosServer(t *testing.T, plaintext []byte, disposition string) *httptest.Server {
	t.Helper()
	ciphertext := encryptLikeWecom(t, testAESKey, plaintext)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if disposition != "" {
			w.Header().Set("Content-Disposition", disposition)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(ciphertext)
	}))
}

// mediaMessage builds the envelope the Router hands the resolver, going
// through the real frame decoder so the test cannot drift from the wire.
func mediaMessage(t *testing.T, msgType string, body map[string]any) channel.InboundMessage {
	t.Helper()
	full := map[string]any{
		"msgid":    "MSGID-MEDIA",
		"aibotid":  "wb-1",
		"chattype": "single",
		"chatid":   "T-alex",
		"from":     map[string]any{"userid": "T-alex"},
		"msgtype":  msgType,
	}
	for k, v := range body {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	var mc aibotMsgCallback
	if err := json.Unmarshal(raw, &mc); err != nil {
		t.Fatalf("decode callback: %v", err)
	}
	text, ok := mc.ownText()
	if !ok {
		t.Fatalf("callback of type %q is not routable; the fixture is wrong", msgType)
	}
	return channelMessageFromCallback("wb-1", "", mc, text, "req-1")
}

func mediaInstallation() engine.ResolvedInstallation {
	return engine.ResolvedInstallation{
		ID:          uuidOf(1),
		WorkspaceID: uuidOf(2),
		AgentID:     uuidOf(3),
		Active:      true,
		Platform:    Installation{ID: uuidOf(1), BotID: "wb-1"},
	}
}

// ---- HasMedia ----

func TestHasMediaLooksOnlyAtWhatIsAlreadyInHand(t *testing.T) {
	t.Parallel()
	r := NewMediaResolver(&fakeMediaStorage{}, newFakeMediaLedger(nil), nil, testLogger())
	cases := []struct {
		name    string
		msgType string
		body    map[string]any
		want    bool
	}{
		{"text", "text", map[string]any{"text": map[string]any{"content": "hi"}}, false},
		{"image", "image", map[string]any{"image": map[string]any{"url": "https://cos.invalid/a", "aeskey": testAESKey}}, true},
		{"file", "file", map[string]any{"file": map[string]any{"url": "https://cos.invalid/b", "aeskey": testAESKey}}, true},
		{"video", "video", map[string]any{"video": map[string]any{"url": "https://cos.invalid/c", "aeskey": testAESKey}}, true},
		{"mixed with a picture", "mixed", map[string]any{"mixed": map[string]any{"msg_item": []any{
			map[string]any{"msgtype": "text", "text": map[string]any{"content": "look"}},
			map[string]any{"msgtype": "image", "image": map[string]any{"url": "https://cos.invalid/d", "aeskey": testAESKey}},
		}}}, true},
		{"mixed, text only", "mixed", map[string]any{"mixed": map[string]any{"msg_item": []any{
			map[string]any{"msgtype": "text", "text": map[string]any{"content": "look"}},
		}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := r.HasMedia(mediaMessage(t, tc.msgType, tc.body)); got != tc.want {
				t.Fatalf("HasMedia = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasMediaSaysNoWhenThereIsNoUrlToFetch(t *testing.T) {
	t.Parallel()
	r := NewMediaResolver(&fakeMediaStorage{}, newFakeMediaLedger(nil), nil, testLogger())
	// A body with an aeskey and no url is not something we can download; it
	// must not buy the message a media deadline and a deferred agent run.
	mc := aibotMsgCallback{MsgType: "image"}
	mc.Image = mediaBody{AESKey: testAESKey}
	msg := channelMessageFromCallback("wb-1", "", mc, "[Image]", "req-1")
	if r.HasMedia(msg) {
		t.Fatal("HasMedia said yes to a body with no url")
	}
}

// ---- the whole ingest ----

// newTestResolver builds the production resolver with the guarded client
// swapped for one that also permits loopback, so the httptest server the test
// runs is reachable while the guard's own decision stays under test in
// media_guard_test.go.
func newTestResolver(storage mediaStorage, ledger engine.MediaIntentLedger, senders *sendersRegistry) *wecomMediaResolver {
	r := NewMediaResolver(storage, ledger, senders, testLogger()).(*wecomMediaResolver)
	r.http = testMediaClient()
	return r
}

// TestResolveMediaStoresTheDecryptedFile is the load-bearing one: real HTTP,
// real AES, and the bytes in storage are the bytes the user sent. Run over
// both storage backends, because the streaming one takes a different path
// through decryptToFile.
func TestResolveMediaStoresTheDecryptedFile(t *testing.T) {
	t.Parallel()
	plaintext := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("pixels", 500))
	srv := cosServer(t, plaintext, `attachment; filename*=UTF-8''%E5%AD%A3%E6%8A%A5.png`)
	defer srv.Close()

	backends := map[string]func() (mediaStorage, *fakeMediaStorage){
		"buffered": func() (mediaStorage, *fakeMediaStorage) {
			s := &fakeMediaStorage{}
			return s, s
		},
		"streaming": func() (mediaStorage, *fakeMediaStorage) {
			s := &fakeStreamStorage{}
			return s, &s.fakeMediaStorage
		},
	}
	for name, build := range backends {
		t.Run(name, func(t *testing.T) {
			storage, inspect := build()
			r := newTestResolver(storage, newFakeMediaLedger(inspect), nil)

			msg := mediaMessage(t, "image", map[string]any{
				"image": map[string]any{"url": srv.URL, "aeskey": testAESKey},
			})
			got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)

			if len(got.MediaRefs) != 1 {
				t.Fatalf("MediaRefs = %d, want 1", len(got.MediaRefs))
			}
			uploads := inspect.stored()
			if len(uploads) != 1 {
				t.Fatalf("uploads = %d, want 1", len(uploads))
			}
			if string(uploads[0].data) != string(plaintext) {
				t.Fatalf("stored %d bytes, want the %d decrypted bytes the user sent", len(uploads[0].data), len(plaintext))
			}
			ref := got.MediaRefs[0]
			if ref.Type != channel.MsgTypeImage {
				t.Errorf("ref type = %v, want image", ref.Type)
			}
			if ref.Filename != "季报.png" {
				t.Errorf("filename = %q, want the Content-Disposition name", ref.Filename)
			}
			if ref.MimeType != "image/png" {
				t.Errorf("mime = %q, want image/png from the .png extension", ref.MimeType)
			}
			if ref.SizeBytes != int64(len(plaintext)) {
				t.Errorf("size = %d, want the decrypted length %d", ref.SizeBytes, len(plaintext))
			}
			if ref.StorageKey != uploads[0].key || ref.StorageURL != inspect.ObjectURL(ref.StorageKey) {
				t.Errorf("ref does not point at what was uploaded: %+v", ref)
			}
			if !strings.Contains(ref.StorageKey, "wecom") {
				t.Errorf("storage key %q does not name the channel", ref.StorageKey)
			}
		})
	}
}

// TestResolveMediaRecordsIntentBeforeUploading pins the ordering the whole
// no-inline-delete design rests on: nothing is written to the object store
// until a ledger row exists to cover it. Same invariant lark states at
// lark/media_ingest.go's RecordPendingMediaObject call.
func TestResolveMediaRecordsIntentBeforeUploading(t *testing.T) {
	t.Parallel()
	srv := cosServer(t, []byte("payload"), "")
	defer srv.Close()

	storage := &fakeMediaStorage{}
	ledger := newFakeMediaLedger(storage)
	r := newTestResolver(storage, ledger, nil)

	msg := mediaMessage(t, "file", map[string]any{
		"file": map[string]any{"url": srv.URL, "aeskey": testAESKey},
	})
	got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)

	if len(ledger.records) != 1 {
		t.Fatalf("intent rows = %d, want 1", len(ledger.records))
	}
	if ledger.seenUploads[0] != 0 {
		t.Fatal("the intent row was written after the upload; a crash in between would leave an uncovered object")
	}
	rec := ledger.records[0]
	if rec.StorageKey != storage.stored()[0].key {
		t.Errorf("intent key %q != uploaded key %q", rec.StorageKey, storage.stored()[0].key)
	}
	if rec.StorageURL != got.MediaRefs[0].StorageURL {
		t.Errorf("intent url %q != attachment url %q", rec.StorageURL, got.MediaRefs[0].StorageURL)
	}
	if rec.ChatMessageID != uuidOf(5) || rec.WorkspaceID != uuidOf(2) || rec.InstallationID != uuidOf(1) {
		t.Errorf("intent row is not scoped to the message: %+v", rec)
	}
}

// TestResolveMediaSkipsAKeyTheReconcilerOwns: ok=false means the row has left
// 'pending'. Uploading anyway would resurrect an object that is being deleted.
func TestResolveMediaSkipsAKeyTheReconcilerOwns(t *testing.T) {
	t.Parallel()
	srv := cosServer(t, []byte("payload"), "")
	defer srv.Close()

	storage := &fakeMediaStorage{}
	ledger := newFakeMediaLedger(storage)
	ledger.ok = false
	r := newTestResolver(storage, ledger, nil)

	msg := mediaMessage(t, "image", map[string]any{
		"image": map[string]any{"url": srv.URL, "aeskey": testAESKey},
	})
	got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
	if len(storage.stored()) != 0 {
		t.Fatal("uploaded into a key the reconciler owns")
	}
	if len(got.MediaRefs) != 0 {
		t.Fatal("produced a ref for an object that was never written")
	}
}

// TestResolveMediaKeepsGoingWhenOneAttachmentFails: a 图文混排 with a good
// picture and an expired one must still deliver the good one.
func TestResolveMediaKeepsGoingWhenOneAttachmentFails(t *testing.T) {
	t.Parallel()
	good := cosServer(t, []byte("the good picture"), `attachment; filename="ok.png"`)
	defer good.Close()
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer gone.Close()

	storage := &fakeMediaStorage{}
	senders, conn := notifierWithLiveSocket(uuidOf(1))
	r := newTestResolver(storage, newFakeMediaLedger(storage), senders)

	msg := mediaMessage(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []any{
		map[string]any{"msgtype": "text", "text": map[string]any{"content": "these two"}},
		map[string]any{"msgtype": "image", "image": map[string]any{"url": gone.URL, "aeskey": testAESKey}},
		map[string]any{"msgtype": "image", "image": map[string]any{"url": good.URL, "aeskey": testAESKey}},
	}}})
	got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)

	if len(got.MediaRefs) != 1 {
		t.Fatalf("MediaRefs = %d, want the one that downloaded", len(got.MediaRefs))
	}
	if got.MediaRefs[0].Filename != "ok.png" {
		t.Errorf("kept the wrong attachment: %+v", got.MediaRefs[0])
	}
	if len(conn.frames) != 1 {
		t.Fatalf("the sender was told %d times, want exactly one notice", len(conn.frames))
	}
}

// TestResolveMediaTellsTheSenderWhatWentWrong: too big and did-not-arrive get
// different wording, because they need different things done about them. Two
// failures of the same kind are still one notice.
//
// Without a notice the failure is invisible in the worst way: the stored body
// still says [Image], so the agent answers as if it had been shown a picture
// it never received.
func TestResolveMediaTellsTheSenderWhatWentWrong(t *testing.T) {
	t.Parallel()
	oversize := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "209715200")
		w.WriteHeader(http.StatusOK)
	}))
	defer oversize.Close()
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()

	noticeText := func(t *testing.T, conn *recordingConn) string {
		t.Helper()
		if len(conn.frames) != 1 {
			t.Fatalf("notices = %d, want 1", len(conn.frames))
		}
		body := conn.sendBody(t, 0)
		md, ok := body["markdown"].(map[string]any)
		if !ok {
			t.Fatalf("notice body has no markdown: %#v", body)
		}
		if body["chatid"] != "T-alex" || body["chat_type"] != float64(chatTypeSingleInt) {
			t.Fatalf("notice went to %#v, want the chat the attachment came from", body)
		}
		content, _ := md["content"].(string)
		return content
	}

	t.Run("one notice names the size limit", func(t *testing.T) {
		storage := &fakeMediaStorage{}
		senders, conn := notifierWithLiveSocket(uuidOf(1))
		r := newTestResolver(storage, newFakeMediaLedger(storage), senders)
		msg := mediaMessage(t, "file", map[string]any{
			"file": map[string]any{"url": oversize.URL, "aeskey": testAESKey},
		})
		r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
		if got := noticeText(t, conn); got != mediaTooLargeNotice {
			t.Fatalf("notice = %q, want the too-large wording", got)
		}
	})

	t.Run("two failures of one kind are one notice", func(t *testing.T) {
		storage := &fakeMediaStorage{}
		senders, conn := notifierWithLiveSocket(uuidOf(1))
		r := newTestResolver(storage, newFakeMediaLedger(storage), senders)
		msg := mediaMessage(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []any{
			map[string]any{"msgtype": "image", "image": map[string]any{"url": expired.URL, "aeskey": testAESKey}},
			map[string]any{"msgtype": "image", "image": map[string]any{"url": expired.URL, "aeskey": testAESKey}},
		}}})
		r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
		if got := noticeText(t, conn); got != mediaUnreadableNotice {
			t.Fatalf("notice = %q, want the unreadable wording once", got)
		}
	})

	t.Run("nothing is said when everything worked", func(t *testing.T) {
		srv := cosServer(t, []byte("fine"), "")
		defer srv.Close()
		storage := &fakeMediaStorage{}
		senders, conn := notifierWithLiveSocket(uuidOf(1))
		r := newTestResolver(storage, newFakeMediaLedger(storage), senders)
		msg := mediaMessage(t, "image", map[string]any{
			"image": map[string]any{"url": srv.URL, "aeskey": testAESKey},
		})
		r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
		if len(conn.frames) != 0 {
			t.Fatalf("sent %d notices for a message that worked", len(conn.frames))
		}
	})

	t.Run("a group notice goes to the room, not the sender", func(t *testing.T) {
		storage := &fakeMediaStorage{}
		senders, conn := notifierWithLiveSocket(uuidOf(1))
		r := newTestResolver(storage, newFakeMediaLedger(storage), senders)
		msg := mediaMessage(t, "image", map[string]any{
			"chattype": "group",
			"chatid":   "ROOM_1",
			"image":    map[string]any{"url": expired.URL, "aeskey": testAESKey},
		})
		r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
		if len(conn.frames) != 1 {
			t.Fatalf("notices = %d, want 1", len(conn.frames))
		}
		body := conn.sendBody(t, 0)
		if body["chatid"] != "ROOM_1" || body["chat_type"] != float64(chatTypeGroupInt) {
			t.Fatalf("group notice addressed %#v, want the room as chat_type 2", body)
		}
	})
}

// TestResolveMediaRefusesAnUndecryptablePayload: a wrong or missing key must
// never put ciphertext in the object store labelled as a picture.
func TestResolveMediaRefusesAnUndecryptablePayload(t *testing.T) {
	t.Parallel()
	srv := cosServer(t, []byte("secret report"), "")
	defer srv.Close()

	for _, tc := range []struct {
		name   string
		aeskey string
	}{
		{"no key on the frame", ""},
		{"a key that is not the one it was encrypted with", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage := &fakeMediaStorage{}
			r := newTestResolver(storage, newFakeMediaLedger(storage), nil)
			mc := aibotMsgCallback{MsgID: "MSGID-BAD", ChatID: "T-alex", ChatType: "single", MsgType: "image"}
			mc.Image = mediaBody{URL: srv.URL, AESKey: tc.aeskey}
			msg := channelMessageFromCallback("wb-1", "", mc, "[Image]", "req-1")

			got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
			if len(storage.stored()) != 0 {
				t.Fatal("stored bytes it could not decrypt")
			}
			if len(got.MediaRefs) != 0 {
				t.Fatal("produced a ref for an object that was never written")
			}
		})
	}
}

// TestResolveMediaNamesAnUnnamedFile: COS does not always send a
// Content-Disposition. The name then has to come from somewhere, and two
// attachments in one message must not collide on it.
func TestResolveMediaNamesAnUnnamedFile(t *testing.T) {
	t.Parallel()
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR" + strings.Repeat("x", 64))
	srv := cosServer(t, png, "")
	defer srv.Close()

	storage := &fakeMediaStorage{}
	r := newTestResolver(storage, newFakeMediaLedger(storage), nil)
	msg := mediaMessage(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []any{
		map[string]any{"msgtype": "image", "image": map[string]any{"url": srv.URL, "aeskey": testAESKey}},
		map[string]any{"msgtype": "image", "image": map[string]any{"url": srv.URL, "aeskey": testAESKey}},
	}}})
	got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)

	if len(got.MediaRefs) != 2 {
		t.Fatalf("MediaRefs = %d, want 2", len(got.MediaRefs))
	}
	first, second := got.MediaRefs[0], got.MediaRefs[1]
	if first.Filename == second.Filename {
		t.Fatalf("both attachments are called %q", first.Filename)
	}
	if first.StorageKey == second.StorageKey {
		t.Fatal("both attachments share a storage key; the second would overwrite the first")
	}
	if !strings.HasSuffix(first.Filename, ".png") {
		t.Errorf("filename %q does not carry the sniffed type", first.Filename)
	}
	if first.MimeType != "image/png" {
		t.Errorf("mime = %q, want image/png sniffed from the decrypted bytes", first.MimeType)
	}
}

// TestResolveMediaKeepsTheKeyPerChatMessage: the same WeCom message can be
// ingested twice once its dedup claim goes stale. Sharing a key would run the
// second ingest into the first one's ledger row — possibly a tombstone the
// intent upsert refuses, silently dropping the media. Lark keys the same way
// and says so at lark/media_ingest.go mediaObjectKey.
func TestResolveMediaKeepsTheKeyPerChatMessage(t *testing.T) {
	t.Parallel()
	srv := cosServer(t, []byte("same bytes"), "")
	defer srv.Close()

	keyFor := func(chatMessageID pgtype.UUID) string {
		storage := &fakeMediaStorage{}
		r := newTestResolver(storage, newFakeMediaLedger(storage), nil)
		msg := mediaMessage(t, "image", map[string]any{
			"image": map[string]any{"url": srv.URL, "aeskey": testAESKey},
		})
		got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), chatMessageID, msg)
		if len(got.MediaRefs) != 1 {
			t.Fatalf("MediaRefs = %d", len(got.MediaRefs))
		}
		return got.MediaRefs[0].StorageKey
	}
	if keyFor(uuidOf(5)) == keyFor(uuidOf(8)) {
		t.Fatal("two chat messages derived the same object key")
	}
	if keyFor(uuidOf(5)) != keyFor(uuidOf(5)) {
		t.Fatal("the key is not a function of the message; a retry would orphan the first object")
	}
}

// TestResolveMediaWithoutStorageLeavesThePlaceholder: a deployment with no
// object store must degrade to the text, not to a panic or a broken ref.
func TestResolveMediaWithoutStorageLeavesThePlaceholder(t *testing.T) {
	t.Parallel()
	r := newTestResolver(nil, nil, nil)
	msg := mediaMessage(t, "image", map[string]any{
		"image": map[string]any{"url": "https://cos.invalid/a", "aeskey": testAESKey},
	})
	got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
	if len(got.MediaRefs) != 0 {
		t.Fatal("produced refs with no storage configured")
	}
	if got.Text != "[Image]" {
		t.Fatalf("body = %q, want the placeholder left intact", got.Text)
	}
}

// TestResolveMediaSurvivesAConnectionThatWentAway: the notice needs a live
// socket and there may not be one. That must not take the ingest down with it.
func TestResolveMediaSurvivesAConnectionThatWentAway(t *testing.T) {
	t.Parallel()
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()

	storage := &fakeMediaStorage{}
	// A registry with no entry for this installation: mid-reconnect.
	r := newTestResolver(storage, newFakeMediaLedger(storage), newSendersRegistry())
	msg := mediaMessage(t, "image", map[string]any{
		"image": map[string]any{"url": expired.URL, "aeskey": testAESKey},
	})
	got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
	if len(got.MediaRefs) != 0 {
		t.Fatal("produced a ref for an attachment that never downloaded")
	}
}

// TestTheTraceRecordsTheDispositionThatArrived: media_download.go logs
// nothing on its own, and the one thing it learns that cannot be recovered
// later is what Content-Disposition the object came with — the URL it came
// from lapses in five minutes, so a filename questioned tomorrow can never be
// checked against the header that produced it. Under MULTICA_WECOM_TRACE the
// raw value goes to the log beside the name parsed out of it, on both ingest
// paths, and the pre-signed URL does not.
//
// No t.Parallel, for the reason trace_test.go gives: `tracing` is a
// package-level atomic and these assertions are about whether a line exists.
func TestTheTraceRecordsTheDispositionThatArrived(t *testing.T) {
	const (
		disposition = `attachment; filename="PC+D%26T+Strategy+2026.docx"`
		encoded     = "PC+D%26T+Strategy+2026.docx"
		decoded     = "PC D&T Strategy 2026.docx"
		signature   = "sig=AKIDSUPERSECRETSIGNATURE"
	)
	plaintext := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("pixels", 200))
	srv := cosServer(t, plaintext, disposition)
	defer srv.Close()
	mediaURL := srv.URL + "/object?" + signature

	// Both backends: the streaming path is the one production takes, and it
	// reads the headers through a different call than the buffered one.
	backends := map[string]func() mediaStorage{
		"buffered":  func() mediaStorage { return &fakeMediaStorage{} },
		"streaming": func() mediaStorage { return &fakeStreamStorage{} },
	}
	for name, build := range backends {
		t.Run(name, func(t *testing.T) {
			t.Run("off, and there is no line at all", func(t *testing.T) {
				withTrace(t, false)
				logged := resolveOneAttachment(t, build(), mediaURL)
				if strings.Contains(logged, "in.media") {
					t.Fatalf("the switch is off and the trace still wrote: %s", logged)
				}
			})

			t.Run("on, and the line carries the bytes", func(t *testing.T) {
				withTrace(t, true)
				logged := resolveOneAttachment(t, build(), mediaURL)
				if !strings.Contains(logged, "dir=in.media") {
					t.Fatalf("no in.media line was written: %s", logged)
				}
				if !strings.Contains(logged, encoded) {
					t.Fatalf("the header did not reach the log as it arrived; want %q in: %s", encoded, logged)
				}
				if !strings.Contains(logged, decoded) {
					t.Fatalf("the parsed name is not beside it; want %q in: %s", decoded, logged)
				}
				if !strings.Contains(logged, "msg_id=MSGID-MEDIA") {
					t.Fatalf("the line does not say which message it belongs to: %s", logged)
				}
				if strings.Contains(logged, signature) || strings.Contains(logged, srv.URL) {
					t.Fatalf("the pre-signed url reached the log: %s", logged)
				}
			})
		})
	}
}

// resolveOneAttachment runs one image through the resolver with a capturing
// logger and returns everything that was logged.
func resolveOneAttachment(t *testing.T, storage mediaStorage, url string) string {
	t.Helper()
	logger, buf := capturingLogger()
	inspect, _ := storage.(*fakeMediaStorage)
	if s, ok := storage.(*fakeStreamStorage); ok {
		inspect = &s.fakeMediaStorage
	}
	r := NewMediaResolver(storage, newFakeMediaLedger(inspect), nil, logger).(*wecomMediaResolver)
	r.http = testMediaClient()

	msg := mediaMessage(t, "image", map[string]any{
		"image": map[string]any{"url": url, "aeskey": testAESKey},
	})
	got := r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
	if len(got.MediaRefs) != 1 {
		t.Fatalf("MediaRefs = %d, want the attachment to have landed", len(got.MediaRefs))
	}
	if got.MediaRefs[0].Filename != "PC D&T Strategy 2026.docx" {
		t.Fatalf("stored filename = %q, want the decoded name", got.MediaRefs[0].Filename)
	}
	return buf.String()
}
