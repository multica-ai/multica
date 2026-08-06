package wecom

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// ---- test doubles (mirrors lark's feishu_channel_test.go fakes) ----

type fakeMediaUpload struct {
	key         string
	data        []byte
	sizeBytes   int64
	streamed    bool
	contentType string
	filename    string
}

type fakeMediaStorage struct {
	mu            sync.Mutex
	uploads       []fakeMediaUpload
	err           error
	sleep         time.Duration
	concurrent    int32
	maxConcurrent int32
}

func (s *fakeMediaStorage) ObjectURL(key string) string { return "https://cdn.example.test/" + key }

func (s *fakeMediaStorage) Upload(_ context.Context, key string, data []byte, contentType, filename string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.track()
	defer s.untrack()
	if s.sleep > 0 {
		time.Sleep(s.sleep)
	}
	s.mu.Lock()
	s.uploads = append(s.uploads, fakeMediaUpload{key: key, data: append([]byte(nil), data...), contentType: contentType, filename: filename})
	s.mu.Unlock()
	return s.ObjectURL(key), nil
}

func (s *fakeMediaStorage) UploadStream(_ context.Context, key string, data io.Reader, sizeBytes int64, contentType, filename string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.track()
	defer s.untrack()
	if s.sleep > 0 {
		time.Sleep(s.sleep)
	}
	body, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.uploads = append(s.uploads, fakeMediaUpload{key: key, data: body, sizeBytes: sizeBytes, streamed: true, contentType: contentType, filename: filename})
	s.mu.Unlock()
	return s.ObjectURL(key), nil
}

func (s *fakeMediaStorage) track() {
	n := atomic.AddInt32(&s.concurrent, 1)
	for {
		max := atomic.LoadInt32(&s.maxConcurrent)
		if n <= max || atomic.CompareAndSwapInt32(&s.maxConcurrent, max, n) {
			return
		}
	}
}

func (s *fakeMediaStorage) untrack() { atomic.AddInt32(&s.concurrent, -1) }

// fakeMediaLedger records intent rows. ownedKeys marks keys the reconciler
// owns ('deleting'): the resolver must skip them entirely without ever
// calling out to HTTP.
type fakeMediaLedger struct {
	mu        sync.Mutex
	records   []engine.RecordPendingMediaObjectParams
	ownedKeys map[string]bool
	err       error
}

func (l *fakeMediaLedger) RecordPendingMediaObject(_ context.Context, p engine.RecordPendingMediaObjectParams) (bool, error) {
	if l.err != nil {
		return false, l.err
	}
	l.mu.Lock()
	l.records = append(l.records, p)
	l.mu.Unlock()
	if l.ownedKeys[p.StorageKey] {
		return false, nil
	}
	return true, nil
}

func testMediaInst(t *testing.T) engine.ResolvedInstallation {
	t.Helper()
	return engine.ResolvedInstallation{
		ID:          util.MustParseUUID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		WorkspaceID: util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
	}
}

// encryptTestPayload is an independent reference AES-256-CBC + PKCS#7
// encryption (crypto/cipher directly), used to build server-side fixtures
// for ResolveMedia end-to-end tests. It intentionally does not call
// anything from crypto.go.
func encryptTestPayload(t *testing.T, key, plaintext []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	padLen := aesBlockSize - (len(plaintext) % aesBlockSize)
	padded := append(append([]byte{}, plaintext...), bytes.Repeat([]byte{byte(padLen)}, padLen)...)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:aesBlockSize]).CryptBlocks(ciphertext, padded)
	return ciphertext
}

// pngPlaintext is real PNG magic bytes followed by filler so
// http.DetectContentType sniffs "image/png".
func pngPlaintext(size int) []byte {
	sig := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	out := make([]byte, size)
	copy(out, sig)
	return out
}

func rawMediaMsg(t *testing.T, messageID string, media []wecomRawMediaItem) channel.InboundMessage {
	t.Helper()
	raw, err := json.Marshal(wecomRawEvent{AIBotID: "bot1", Media: media})
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	return channel.InboundMessage{MessageID: messageID, Raw: raw}
}

// ---- HasMedia (pure, no I/O) ----

func TestHasMedia(t *testing.T) {
	resolver := NewMediaResolver(MediaResolverConfig{})
	cases := []struct {
		name string
		msg  channel.InboundMessage
		want bool
	}{
		{"image", rawMediaMsg(t, "m1", []wecomRawMediaItem{{MsgType: "image", URL: "https://x/y", AESKey: "k"}}), true},
		{"file", rawMediaMsg(t, "m2", []wecomRawMediaItem{{MsgType: "file", URL: "https://x/y", AESKey: "k"}}), true},
		{"video", rawMediaMsg(t, "m3", []wecomRawMediaItem{{MsgType: "video", URL: "https://x/y", AESKey: "k"}}), true},
		{"mixed with image", rawMediaMsg(t, "m4", []wecomRawMediaItem{{MsgType: "image", URL: "https://x/y", AESKey: "k"}}), true},
		{"no media", rawMediaMsg(t, "m5", nil), false},
		{"malformed raw", channel.InboundMessage{MessageID: "m6", Raw: []byte("not json")}, false},
		{"empty raw", channel.InboundMessage{MessageID: "m7"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolver.HasMedia(tc.msg); got != tc.want {
				t.Fatalf("HasMedia() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---- pure validation helpers ----

func TestValidateHTTPSURL(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"https://media.example.test/f", false},
		{"http://media.example.test/f", true},
		{"ftp://media.example.test/f", true},
		{"", true},
		{"   ", true},
		{"https://", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			err := validateHTTPSURL(tc.in)
			if tc.wantErr && err == nil {
				t.Fatalf("validateHTTPSURL(%q) = nil, want error", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateHTTPSURL(%q) = %v, want nil", tc.in, err)
			}
		})
	}
}

func TestIsAllowedMediaIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", false},
		{"::1", false},
		{"10.0.0.5", false},
		{"172.16.0.5", false},
		{"192.168.1.5", false},
		{"169.254.1.1", false},
		{"fe80::1", false},
		{"0.0.0.0", false},
		{"::", false},
		{"224.0.0.1", false},
		{"100.64.0.1", false},
		{"100.63.255.255", true}, // just outside the CGNAT /10
		{"100.128.0.1", true},    // just outside the CGNAT /10
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"2606:4700:4700::1111", true},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("net.ParseIP(%q) failed", tc.ip)
			}
			if got := isAllowedMediaIP(ip); got != tc.want {
				t.Fatalf("isAllowedMediaIP(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// TestDialAllowedMediaAddr_RejectsDisallowedLiteralIP confirms disallowed
// literal IPs are rejected before any dial attempt: LookupIPAddr on an IP
// literal never touches the network, and a real *net.Dialer is safe to pass
// here because isAllowedMediaIP must short-circuit before DialContext is
// ever called.
func TestDialAllowedMediaAddr_RejectsDisallowedLiteralIP(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:9999", "10.0.0.5:9999", "169.254.1.1:9999", "[::1]:9999"} {
		t.Run(addr, func(t *testing.T) {
			_, err := dialAllowedMediaAddr(context.Background(), &net.Dialer{}, "tcp", addr)
			if !errors.Is(err, ErrMediaHostNotAllowed) {
				t.Fatalf("err = %v, want ErrMediaHostNotAllowed", err)
			}
		})
	}
}

func TestMediaCheckRedirect(t *testing.T) {
	httpsReq := &http.Request{URL: &url.URL{Scheme: "https", Host: "x"}}
	httpReq := &http.Request{URL: &url.URL{Scheme: "http", Host: "x"}}

	cases := []struct {
		name    string
		req     *http.Request
		via     int
		wantErr error
	}{
		{"first hop ok", httpsReq, 1, nil},
		{"second hop ok", httpsReq, 2, nil},
		{"third hop ok (max)", httpsReq, 3, nil},
		{"fourth hop rejected", httpsReq, 4, ErrTooManyMediaRedirects},
		{"non-https redirect rejected", httpReq, 1, ErrMediaSchemeNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			via := make([]*http.Request, tc.via)
			err := mediaCheckRedirect(tc.req, via)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSanitizeMediaErr(t *testing.T) {
	t.Run("url error", func(t *testing.T) {
		inner := errors.New("dial tcp 10.0.0.1:443: connect: refused")
		wrapped := &url.Error{Op: "Get", URL: "https://media.example.test/secret-aeskey-path", Err: inner}
		got := sanitizeMediaErr(wrapped)
		if got != inner {
			t.Fatalf("sanitizeMediaErr did not unwrap *url.Error: %v", got)
		}
		if strings.Contains(got.Error(), "media.example.test") {
			t.Fatalf("sanitized error still contains the URL: %v", got)
		}
	})

	t.Run("path error", func(t *testing.T) {
		secretPath := filepath.Join(t.TempDir(), "wecom-media-cipher-SECRET-TEMP-PATH")
		perr := &os.PathError{Op: "open", Path: secretPath, Err: os.ErrPermission}
		got := sanitizeMediaErr(fmt.Errorf("wecom: create ciphertext temp file: %w", perr))
		if strings.Contains(got.Error(), secretPath) {
			t.Fatalf("sanitized error still contains the temp path: %v", got)
		}
		if !strings.Contains(got.Error(), "create ciphertext temp file") {
			t.Fatalf("sanitized error lost outer context: %v", got)
		}
		if !strings.Contains(got.Error(), "open:") {
			t.Fatalf("sanitized error lost operation context: %v", got)
		}
	})

	t.Run("link error", func(t *testing.T) {
		oldPath := filepath.Join(t.TempDir(), "wecom-media-plain-OLD-SECRET")
		newPath := filepath.Join(t.TempDir(), "wecom-media-plain-NEW-SECRET")
		lerr := &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: os.ErrPermission}
		got := sanitizeMediaErr(lerr)
		if strings.Contains(got.Error(), oldPath) || strings.Contains(got.Error(), newPath) {
			t.Fatalf("sanitized error still contains link paths: %v", got)
		}
	})

	t.Run("plain error unchanged", func(t *testing.T) {
		plain := errors.New("plain failure")
		if sanitizeMediaErr(plain) != plain {
			t.Fatal("sanitizeMediaErr must pass through unrelated errors unchanged")
		}
	})
}

// ---- end-to-end ResolveMedia over a fake HTTPS server ----

// mediaTestServer serves a fixed payload (optionally through a chain of
// redirects, an explicit — possibly wrong — Content-Length, or forced
// chunked/unknown-length transfer) and counts requests received.
type mediaTestServer struct {
	*httptest.Server
	calls        atomic.Int64
	redirectHops int
	body         []byte
	declaredLen  int64 // >0 sets an explicit Content-Length header
	forceChunked bool  // flush before writing to force unknown-length transfer
}

func newMediaTestServer(t *testing.T, body []byte) *mediaTestServer {
	t.Helper()
	m := &mediaTestServer{body: body}
	m.Server = httptest.NewTLSServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.Server.Close)
	return m
}

func (m *mediaTestServer) handle(w http.ResponseWriter, r *http.Request) {
	m.calls.Add(1)
	if hop := r.URL.Query().Get("hop"); hop != "" {
		n, _ := strconv.Atoi(hop)
		if n < m.redirectHops {
			http.Redirect(w, r, fmt.Sprintf("%s?hop=%d", m.Server.URL, n+1), http.StatusFound)
			return
		}
	} else if m.redirectHops > 0 {
		http.Redirect(w, r, fmt.Sprintf("%s?hop=1", m.Server.URL), http.StatusFound)
		return
	}
	if m.declaredLen > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(m.declaredLen, 10))
	}
	w.WriteHeader(http.StatusOK)
	if m.forceChunked {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	_, _ = w.Write(m.body)
}

func (m *mediaTestServer) url() string { return m.Server.URL }

func newTestResolver(t *testing.T, srv *mediaTestServer, storage mediaStorage, ledger engine.MediaIntentLedger, logBuf *bytes.Buffer, tempDir string) engine.MediaResolver {
	t.Helper()
	var logger *slog.Logger
	if logBuf != nil {
		logger = slog.New(slog.NewTextHandler(logBuf, nil))
	}
	return NewMediaResolver(MediaResolverConfig{
		Storage:            storage,
		Ledger:             ledger,
		Logger:             logger,
		HTTPClient:         srv.Server.Client(),
		TempDir:            tempDir,
		MaxCiphertextBytes: 1 << 20, // 1 MiB is plenty for these small fixtures
	})
}

func TestResolveMedia_ImageFullPipeline(t *testing.T) {
	key := fixedKeyBytes(t)
	plaintext := pngPlaintext(300)
	srv := newMediaTestServer(t, encryptTestPayload(t, key, plaintext))
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	tempDir := t.TempDir()
	resolver := newTestResolver(t, srv, storage, ledger, nil, tempDir)

	msg := rawMediaMsg(t, "msg-1", []wecomRawMediaItem{{
		MsgType: "image",
		URL:     srv.url(),
		AESKey:  base64.StdEncoding.EncodeToString(key),
	}})

	chatMessageID := util.MustParseUUID("22222222-2222-2222-2222-222222222222")
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)

	if len(got.MediaRefs) != 1 {
		t.Fatalf("MediaRefs = %d, want 1: %+v", len(got.MediaRefs), got.MediaRefs)
	}
	ref := got.MediaRefs[0]
	if ref.Type != channel.MsgTypeImage {
		t.Fatalf("Type = %q, want image", ref.Type)
	}
	if ref.MimeType != "image/png" {
		t.Fatalf("MimeType = %q, want image/png", ref.MimeType)
	}
	if ref.SizeBytes != int64(len(plaintext)) {
		t.Fatalf("SizeBytes = %d, want %d", ref.SizeBytes, len(plaintext))
	}
	if ref.Filename == "" {
		t.Fatal("Filename must not be empty")
	}
	if len(storage.uploads) != 1 || !bytes.Equal(storage.uploads[0].data, plaintext) {
		t.Fatalf("uploaded data mismatch: %+v", storage.uploads)
	}
	if len(ledger.records) != 1 || ledger.records[0].ChatMessageID != chatMessageID {
		t.Fatalf("ledger did not record intent before upload: %+v", ledger.records)
	}
	assertDirEmpty(t, tempDir)
}

func TestResolveMedia_FileUsesProvidedFilename(t *testing.T) {
	key := fixedKeyBytes(t)
	plaintext := []byte("plain file body content for wecom media test")
	srv := newMediaTestServer(t, encryptTestPayload(t, key, plaintext))
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := newTestResolver(t, srv, storage, ledger, nil, t.TempDir())

	msg := rawMediaMsg(t, "msg-2", []wecomRawMediaItem{{
		MsgType:  "file",
		URL:      srv.url(),
		AESKey:   base64.StdEncoding.EncodeToString(key),
		Filename: "quarterly-report.pdf",
	}})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("33333333-3333-3333-3333-333333333333"), msg)

	if len(got.MediaRefs) != 1 {
		t.Fatalf("MediaRefs = %d, want 1", len(got.MediaRefs))
	}
	if got.MediaRefs[0].Filename != "quarterly-report.pdf" {
		t.Fatalf("Filename = %q, want quarterly-report.pdf", got.MediaRefs[0].Filename)
	}
	if got.MediaRefs[0].Type != channel.MsgTypeFile {
		t.Fatalf("Type = %q, want file", got.MediaRefs[0].Type)
	}
}

func TestResolveMedia_MixedMultipleImages(t *testing.T) {
	key := fixedKeyBytes(t)
	srv := newMediaTestServer(t, encryptTestPayload(t, key, pngPlaintext(64)))
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := newTestResolver(t, srv, storage, ledger, nil, t.TempDir())

	msg := rawMediaMsg(t, "msg-3", []wecomRawMediaItem{
		{MsgType: "image", URL: srv.url(), AESKey: base64.StdEncoding.EncodeToString(key)},
		{MsgType: "image", URL: srv.url(), AESKey: base64.StdEncoding.EncodeToString(key)},
	})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("44444444-4444-4444-4444-444444444444"), msg)

	if len(got.MediaRefs) != 2 {
		t.Fatalf("MediaRefs = %d, want 2: %+v", len(got.MediaRefs), got.MediaRefs)
	}
	if got.MediaRefs[0].StorageKey == got.MediaRefs[1].StorageKey {
		t.Fatal("two media items in one message must get distinct storage keys")
	}
}

func TestResolveMedia_RejectsNonHTTPSURLWithoutNetworkCall(t *testing.T) {
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := NewMediaResolver(MediaResolverConfig{Storage: storage, Ledger: ledger})

	msg := rawMediaMsg(t, "msg-4", []wecomRawMediaItem{{
		MsgType: "image",
		URL:     "http://media.example.test/should-not-be-fetched",
		AESKey:  base64.StdEncoding.EncodeToString(fixedKeyBytes(t)),
	}})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("55555555-5555-5555-5555-555555555555"), msg)

	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 for a rejected non-HTTPS URL", len(got.MediaRefs))
	}
	if len(storage.uploads) != 0 {
		t.Fatal("must not upload anything for a rejected URL")
	}
}

func TestResolveMedia_DefaultClientRejectsLoopbackHost(t *testing.T) {
	key := fixedKeyBytes(t)
	plaintext := pngPlaintext(32)
	srv := newMediaTestServer(t, encryptTestPayload(t, key, plaintext))
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	// No HTTPClient override: production's SSRF-hardened default client
	// must reject the loopback address the httptest server binds to.
	resolver := NewMediaResolver(MediaResolverConfig{Storage: storage, Ledger: ledger, TempDir: t.TempDir()})

	msg := rawMediaMsg(t, "msg-5", []wecomRawMediaItem{{
		MsgType: "image",
		URL:     srv.url(),
		AESKey:  base64.StdEncoding.EncodeToString(key),
	}})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("66666666-6666-6666-6666-666666666666"), msg)

	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0: the default client must refuse loopback", len(got.MediaRefs))
	}
	if len(storage.uploads) != 0 {
		t.Fatal("must not upload anything when the dial is rejected")
	}
}

func TestResolveMedia_RedirectLimit(t *testing.T) {
	key := fixedKeyBytes(t)
	plaintext := pngPlaintext(32)

	t.Run("within limit succeeds", func(t *testing.T) {
		srv := newMediaTestServer(t, encryptTestPayload(t, key, plaintext))
		srv.redirectHops = maxMediaRedirects
		storage := &fakeMediaStorage{}
		ledger := &fakeMediaLedger{}
		resolver := newTestResolver(t, srv, storage, ledger, nil, t.TempDir())
		msg := rawMediaMsg(t, "msg-6", []wecomRawMediaItem{{MsgType: "image", URL: srv.url(), AESKey: base64.StdEncoding.EncodeToString(key)}})
		got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("77777777-7777-7777-7777-777777777771"), msg)
		if len(got.MediaRefs) != 1 {
			t.Fatalf("MediaRefs = %d, want 1 within the redirect limit", len(got.MediaRefs))
		}
	})

	t.Run("exceeding limit fails", func(t *testing.T) {
		srv := newMediaTestServer(t, encryptTestPayload(t, key, plaintext))
		srv.redirectHops = maxMediaRedirects + 1
		storage := &fakeMediaStorage{}
		ledger := &fakeMediaLedger{}
		resolver := newTestResolver(t, srv, storage, ledger, nil, t.TempDir())
		msg := rawMediaMsg(t, "msg-7", []wecomRawMediaItem{{MsgType: "image", URL: srv.url(), AESKey: base64.StdEncoding.EncodeToString(key)}})
		got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("77777777-7777-7777-7777-777777777772"), msg)
		if len(got.MediaRefs) != 0 {
			t.Fatalf("MediaRefs = %d, want 0 beyond the redirect limit", len(got.MediaRefs))
		}
	})
}

func TestResolveMedia_OversizeDeclaredContentLength(t *testing.T) {
	key := fixedKeyBytes(t)
	srv := newMediaTestServer(t, encryptTestPayload(t, key, pngPlaintext(32)))
	srv.declaredLen = (1 << 20) + 100 // just over the test's 1 MiB cap
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := newTestResolver(t, srv, storage, ledger, nil, t.TempDir())

	msg := rawMediaMsg(t, "msg-8", []wecomRawMediaItem{{MsgType: "image", URL: srv.url(), AESKey: base64.StdEncoding.EncodeToString(key)}})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("88888888-8888-8888-8888-888888888881"), msg)

	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 for an oversize declared Content-Length", len(got.MediaRefs))
	}
	if len(storage.uploads) != 0 {
		t.Fatal("must not upload anything for an oversize declared Content-Length")
	}
	if srv.calls.Load() != 1 {
		t.Fatalf("calls = %d, want exactly 1 (the body must never be read)", srv.calls.Load())
	}
}

func TestResolveMedia_OversizeUnknownLength(t *testing.T) {
	// No declared Content-Length (forced chunked) and an actual body bigger
	// than the test's 1 MiB cap. The body does not need to be valid
	// ciphertext — the size check must reject it before decryption ever
	// runs.
	oversizeBody := bytes.Repeat([]byte{0xAB}, (1<<20)+1024)
	srv := newMediaTestServer(t, oversizeBody)
	srv.forceChunked = true
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	tempDir := t.TempDir()
	resolver := newTestResolver(t, srv, storage, ledger, nil, tempDir)

	msg := rawMediaMsg(t, "msg-9", []wecomRawMediaItem{{MsgType: "image", URL: srv.url(), AESKey: base64.StdEncoding.EncodeToString(fixedKeyBytes(t))}})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("88888888-8888-8888-8888-888888888882"), msg)

	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 for an unknown-length oversize body", len(got.MediaRefs))
	}
	assertDirEmpty(t, tempDir)
}

func TestResolveMedia_InvalidAESKeySkipsItem(t *testing.T) {
	srv := newMediaTestServer(t, []byte("irrelevant, must never be fetched"))
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	resolver := newTestResolver(t, srv, storage, ledger, nil, t.TempDir())

	msg := rawMediaMsg(t, "msg-10", []wecomRawMediaItem{{MsgType: "image", URL: srv.url(), AESKey: "not-a-valid-key"}})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("99999999-9999-9999-9999-999999999991"), msg)

	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 for an invalid aeskey", len(got.MediaRefs))
	}
	if srv.calls.Load() != 0 {
		t.Fatal("an invalid aeskey must be rejected before any HTTP call")
	}
}

func TestResolveMedia_CorruptCiphertextSkipsItemAndCleansUp(t *testing.T) {
	key := fixedKeyBytes(t)
	ciphertext := encryptTestPayload(t, key, pngPlaintext(64))
	ciphertext[len(ciphertext)-1] ^= 0xFF // corrupt the PKCS#7 pad byte
	srv := newMediaTestServer(t, ciphertext)
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{}
	tempDir := t.TempDir()
	resolver := newTestResolver(t, srv, storage, ledger, nil, tempDir)

	msg := rawMediaMsg(t, "msg-11", []wecomRawMediaItem{{MsgType: "image", URL: srv.url(), AESKey: base64.StdEncoding.EncodeToString(key)}})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("99999999-9999-9999-9999-999999999992"), msg)

	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 for corrupted padding", len(got.MediaRefs))
	}
	if len(ledger.records) != 1 {
		t.Fatalf("intent must still be recorded before the download attempt: %d records", len(ledger.records))
	}
	if len(storage.uploads) != 0 {
		t.Fatal("must not upload anything when decryption fails")
	}
	assertDirEmpty(t, tempDir)
}

func TestResolveMedia_LedgerOwnedKeySkipsDownload(t *testing.T) {
	key := fixedKeyBytes(t)
	srv := newMediaTestServer(t, encryptTestPayload(t, key, pngPlaintext(32)))
	storage := &fakeMediaStorage{}
	ledger := &fakeMediaLedger{ownedKeys: map[string]bool{}}
	resolver := newTestResolver(t, srv, storage, ledger, nil, t.TempDir())

	inst := testMediaInst(t)
	chatMessageID := util.MustParseUUID("aaaaaaaa-0000-4000-8000-000000000001")
	key0 := wecomMediaObjectKey(inst, chatMessageID, "msg-12", 0)
	ledger.ownedKeys[key0] = true

	msg := rawMediaMsg(t, "msg-12", []wecomRawMediaItem{{MsgType: "image", URL: srv.url(), AESKey: base64.StdEncoding.EncodeToString(key)}})
	got := resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)

	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 when the ledger owns the key", len(got.MediaRefs))
	}
	if srv.calls.Load() != 0 {
		t.Fatal("must not call out to HTTP when the intent ledger reports the key is owned by the reconciler")
	}
}

func TestResolveMedia_MissingDependenciesNoop(t *testing.T) {
	resolver := NewMediaResolver(MediaResolverConfig{}) // no Storage, no Ledger
	msg := rawMediaMsg(t, "msg-13", []wecomRawMediaItem{{MsgType: "image", URL: "https://media.example.test/x", AESKey: "k"}})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("bbbbbbbb-0000-4000-8000-000000000001"), msg)
	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 without storage/ledger", len(got.MediaRefs))
	}
}

func TestResolveMedia_NoMediaReturnsMessageUnchanged(t *testing.T) {
	resolver := NewMediaResolver(MediaResolverConfig{Storage: &fakeMediaStorage{}, Ledger: &fakeMediaLedger{}})
	msg := rawMediaMsg(t, "msg-14", nil)
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("cccccccc-0000-4000-8000-000000000001"), msg)
	if len(got.MediaRefs) != 0 {
		t.Fatal("MediaRefs must stay empty when Raw carries no media")
	}
}

// TestResolveMedia_ConcurrencyCap fires more concurrent downloads than the
// configured per-process cap and asserts the fake storage never observes
// more than that many simultaneous Upload calls.
func TestResolveMedia_ConcurrencyCap(t *testing.T) {
	key := fixedKeyBytes(t)
	srv := newMediaTestServer(t, encryptTestPayload(t, key, pngPlaintext(32)))
	storage := &fakeMediaStorage{sleep: 60 * time.Millisecond}
	ledger := &fakeMediaLedger{}
	resolver := NewMediaResolver(MediaResolverConfig{
		Storage:            storage,
		Ledger:             ledger,
		HTTPClient:         srv.Server.Client(),
		TempDir:            t.TempDir(),
		MaxCiphertextBytes: 1 << 20,
		Concurrency:        1,
	})

	const n = 4
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := rawMediaMsg(t, fmt.Sprintf("msg-concurrency-%d", i), []wecomRawMediaItem{{MsgType: "image", URL: srv.url(), AESKey: base64.StdEncoding.EncodeToString(key)}})
			resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID(fmt.Sprintf("dddddddd-0000-4000-8000-%012d", i)), msg)
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&storage.maxConcurrent); got > 1 {
		t.Fatalf("observed %d concurrent uploads, want at most the configured cap of 1", got)
	}
	if len(storage.uploads) != n {
		t.Fatalf("uploads = %d, want %d (cap must throttle, not drop)", len(storage.uploads), n)
	}
}

// TestResolveMedia_LogsNeverContainSecretsOrPlaintext exercises both a
// success and several failure paths with a capturing logger and asserts the
// aeskey, download URL, and plaintext content never appear in the log
// output (spec §5.6 item 6).
func TestResolveMedia_LogsNeverContainSecretsOrPlaintext(t *testing.T) {
	key := fixedKeyBytes(t)
	secretAESKey := base64.StdEncoding.EncodeToString(key)
	plaintext := []byte("TOP-SECRET-PLAINTEXT-MARKER-0xDEADBEEF")

	var logBuf bytes.Buffer

	// 1. Successful download + upload.
	okSrv := newMediaTestServer(t, encryptTestPayload(t, key, plaintext))
	okResolver := newTestResolver(t, okSrv, &fakeMediaStorage{}, &fakeMediaLedger{}, &logBuf, t.TempDir())
	msg := rawMediaMsg(t, "secret-msg-1", []wecomRawMediaItem{{MsgType: "image", URL: okSrv.url(), AESKey: secretAESKey}})
	okResolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("eeeeeeee-0000-4000-8000-000000000001"), msg)

	// 2. Corrupted ciphertext (decrypt failure logs a warning).
	badCipher := encryptTestPayload(t, key, plaintext)
	badCipher[0] ^= 0xFF
	badSrv := newMediaTestServer(t, badCipher)
	badResolver := newTestResolver(t, badSrv, &fakeMediaStorage{}, &fakeMediaLedger{}, &logBuf, t.TempDir())
	msg2 := rawMediaMsg(t, "secret-msg-2", []wecomRawMediaItem{{MsgType: "image", URL: badSrv.url(), AESKey: secretAESKey}})
	badResolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("eeeeeeee-0000-4000-8000-000000000002"), msg2)

	// 3. Transport-layer failure: net/http's Client.Do wraps this in a
	// *url.Error whose Error() embeds the request URL verbatim. Using a
	// URL that itself contains the secret proves neither the URL nor the
	// key trailing it leak, without depending on real network timing.
	secretURL := "https://media.example.test/very-secret-path?aeskey=" + secretAESKey
	failResolver := NewMediaResolver(MediaResolverConfig{
		Storage:            &fakeMediaStorage{},
		Ledger:             &fakeMediaLedger{},
		Logger:             slog.New(slog.NewTextHandler(&logBuf, nil)),
		HTTPClient:         &http.Client{Transport: failingTransport{}},
		TempDir:            t.TempDir(),
		MaxCiphertextBytes: 1 << 20,
	})
	msg3 := rawMediaMsg(t, "secret-msg-3", []wecomRawMediaItem{{MsgType: "image", URL: secretURL, AESKey: secretAESKey}})
	failResolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("eeeeeeee-0000-4000-8000-000000000003"), msg3)

	logged := logBuf.String()
	for _, secret := range []string{secretAESKey, string(plaintext), "media.example.test", "very-secret-path"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log output leaked a sensitive value %q:\n%s", secret, logged)
		}
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("dial tcp: lookup %s: no such host", req.URL.Host)
}

// TestResolveMedia_LogsNeverContainTempPath forces CreateTemp to fail by
// pointing TempDir at a regular file (not a directory) and asserts the
// attempted temp path never appears in warn logs (spec §5.6 item 6).
func TestResolveMedia_LogsNeverContainTempPath(t *testing.T) {
	tempRoot := t.TempDir()
	notADir, err := os.CreateTemp(tempRoot, "not-a-dir-*")
	if err != nil {
		t.Fatalf("CreateTemp marker file: %v", err)
	}
	secretPath := notADir.Name()
	if err := notADir.Close(); err != nil {
		t.Fatalf("close marker file: %v", err)
	}

	key := fixedKeyBytes(t)
	srv := newMediaTestServer(t, encryptTestPayload(t, key, pngPlaintext(32)))
	var logBuf bytes.Buffer
	resolver := newTestResolver(t, srv, &fakeMediaStorage{}, &fakeMediaLedger{}, &logBuf, secretPath)

	msg := rawMediaMsg(t, "temp-path-msg", []wecomRawMediaItem{{
		MsgType: "image",
		URL:     srv.url(),
		AESKey:  base64.StdEncoding.EncodeToString(key),
	}})
	got := resolver.ResolveMedia(context.Background(), testMediaInst(t), engine.ResolvedIdentity{}, pgtype.UUID{}, util.MustParseUUID("ffffffff-0000-4000-8000-000000000001"), msg)
	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 when temp file creation fails", len(got.MediaRefs))
	}

	logged := logBuf.String()
	for _, secret := range []string{secretPath, tempRoot, "wecom-media-cipher", "wecom-media-plain"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log output leaked temp path fragment %q:\n%s", secret, logged)
		}
	}
}

// assertDirEmpty fails the test if dir contains any entries, i.e. the
// resolver left a ciphertext or plaintext temp file behind.
func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("temp dir %s not empty, leftover files: %v", dir, names)
	}
}
