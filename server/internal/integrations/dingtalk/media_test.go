package dingtalk

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
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	pngBytes  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 64)...)
	jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...)
	svgBytes  = []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
)

type fakeStorage struct {
	mu      sync.Mutex
	uploads map[string][]byte
}

func newFakeStorage() *fakeStorage { return &fakeStorage{uploads: map[string][]byte{}} }

func (f *fakeStorage) Upload(_ context.Context, key string, data []byte, _ string, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads[key] = append([]byte(nil), data...)
	return f.ObjectURL(key), nil
}
func (f *fakeStorage) Delete(context.Context, string)             {}
func (f *fakeStorage) DeleteObject(context.Context, string) error { return nil }
func (f *fakeStorage) DeleteKeys(context.Context, []string)       {}
func (f *fakeStorage) KeyFromURL(string) string                   { return "" }
func (f *fakeStorage) ObjectURL(key string) string                { return "https://cdn.example/" + key }
func (f *fakeStorage) CdnDomain() string                          { return "" }
func (f *fakeStorage) GetReader(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

type fakeLedger struct {
	mu      sync.Mutex
	records []engine.RecordPendingMediaObjectParams
}

func (f *fakeLedger) RecordPendingMediaObject(_ context.Context, p engine.RecordPendingMediaObjectParams) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, p)
	return true, nil
}

type mediaTestEnv struct {
	resolver *mediaResolver
	store    *fakeStorage
	ledger   *fakeLedger
	resolves *atomic.Int32
}

func newMediaTestEnv(t *testing.T, files map[string][]byte) *mediaTestEnv {
	t.Helper()
	var resolves atomic.Int32
	fileHost := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Path, "/f/")
		data, ok := files[code]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(fileHost.Close)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case accessTokenPath:
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
		case messageFilesDownloadPath:
			resolves.Add(1)
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			code := body["downloadCode"]
			if _, ok := files[code]; !ok {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = fmt.Fprintf(w, `{"downloadUrl":%q}`, fileHost.URL+"/f/"+code)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(api.Close)
	store := newFakeStorage()
	ledger := &fakeLedger{}
	resolver := NewMediaResolver(NewClient(nil, api.URL), nil, store, ledger, nil).(*mediaResolver)
	resolver.fetch.Transport = fileHost.Client().Transport
	return &mediaTestEnv{resolver: resolver, store: store, ledger: ledger, resolves: &resolves}
}

func mediaFixture(resources ...dingtalkMediaResource) (engine.ResolvedInstallation, pgtype.UUID, channel.InboundMessage) {
	cfg, _ := json.Marshal(installConfig{
		AppID:              "app-key",
		RobotCode:          "robot-1",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret")),
	})
	var ws, instID, messageID pgtype.UUID
	ws.Bytes[0], instID.Bytes[0], messageID.Bytes[0] = 0xAB, 0xCD, 0xEF
	ws.Valid, instID.Valid, messageID.Valid = true, true, true
	raw, _ := json.Marshal(dingtalkRawEvent{AppID: "app-key", Media: resources})
	inst := engine.ResolvedInstallation{
		ID: instID, WorkspaceID: ws,
		Platform: db.ChannelInstallation{Config: cfg},
	}
	return inst, messageID, channel.InboundMessage{MessageID: "dt-msg", Type: channel.MsgTypeImage, Text: "[Image]", Raw: raw}
}

func TestMediaResolver_HappyPathAndIntentLedger(t *testing.T) {
	env := newMediaTestEnv(t, map[string][]byte{"c1": pngBytes, "c2": jpegBytes})
	inst, messageID, msg := mediaFixture(dingtalkMediaResource{Ref: "c1"}, dingtalkMediaResource{Ref: "c2"})
	if !env.resolver.HasMedia(msg) {
		t.Fatal("HasMedia=false")
	}
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 2 {
		t.Fatalf("media refs = %+v", got.MediaRefs)
	}
	if got.MediaRefs[0].MimeType != "image/png" || got.MediaRefs[1].MimeType != "image/jpeg" {
		t.Fatalf("mime types = %q/%q", got.MediaRefs[0].MimeType, got.MediaRefs[1].MimeType)
	}
	if len(env.ledger.records) != 2 || len(env.store.uploads) != 2 {
		t.Fatalf("intents/uploads = %d/%d", len(env.ledger.records), len(env.store.uploads))
	}
	for _, ref := range got.MediaRefs {
		if !strings.HasPrefix(ref.StorageKey, "workspaces/ab000000-0000-0000-0000-000000000000/dingtalk/") {
			t.Fatalf("unexpected storage key %q", ref.StorageKey)
		}
	}
}

func TestMediaResolver_AltFallback(t *testing.T) {
	env := newMediaTestEnv(t, map[string][]byte{"alt": pngBytes})
	inst, messageID, msg := mediaFixture(dingtalkMediaResource{Ref: "expired", Alt: "alt"})
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 1 || env.resolves.Load() != 2 {
		t.Fatalf("refs/resolves = %d/%d", len(got.MediaRefs), env.resolves.Load())
	}
}

func TestMediaResolver_AltFailurePreservesPrimaryError(t *testing.T) {
	env := newMediaTestEnv(t, nil)
	_, _, err := env.resolver.fetchResource(context.Background(), credentials{
		AppKey: "app-key", AppSecret: "secret", RobotCode: "robot-1",
	}, dingtalkMediaResource{Ref: "primary", Alt: "fallback"})
	if err == nil || !strings.Contains(err.Error(), "primary media reference") || !strings.Contains(err.Error(), "fallback media reference") {
		t.Fatalf("combined media error = %v", err)
	}
	if env.resolves.Load() != 2 {
		t.Fatalf("resolve calls = %d, want 2", env.resolves.Load())
	}
}

func TestMediaResolver_RejectsSVGButLeavesIntentForReconciler(t *testing.T) {
	env := newMediaTestEnv(t, map[string][]byte{"svg": svgBytes})
	inst, messageID, msg := mediaFixture(dingtalkMediaResource{Ref: "svg"})
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 0 || len(env.store.uploads) != 0 || len(env.ledger.records) != 1 {
		t.Fatalf("refs/uploads/intents = %d/%d/%d", len(got.MediaRefs), len(env.store.uploads), len(env.ledger.records))
	}
}

func TestMediaResolver_TooManyImagesSkipsNetwork(t *testing.T) {
	env := newMediaTestEnv(t, nil)
	resources := make([]dingtalkMediaResource, maxImagesPerMessage+1)
	for i := range resources {
		resources[i].Ref = fmt.Sprintf("c%d", i)
	}
	inst, messageID, msg := mediaFixture(resources...)
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 0 || env.resolves.Load() != 0 || len(env.ledger.records) != 0 {
		t.Fatalf("unexpected work: refs=%d resolves=%d intents=%d", len(got.MediaRefs), env.resolves.Load(), len(env.ledger.records))
	}
}

func TestMediaResolver_DownloadErrorDoesNotExposeSignedURL(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	server.Close()
	resolver := &mediaResolver{fetch: client}
	_, _, err := resolver.fetchBytes(context.Background(), server.URL+"/image?token=supersecret")
	if err == nil {
		t.Fatal("expected the closed server download to fail")
	}
	if strings.Contains(err.Error(), "supersecret") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("download error leaked signed URL: %v", err)
	}
}

func TestMediaResolver_AcceptsProviderIssuedHTTPURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pngBytes)
	}))
	defer server.Close()
	resolver := NewMediaResolver(nil, nil, nil, nil, nil).(*mediaResolver)
	// Bypass the production public-network dialer only for this loopback test;
	// fetchBytes still exercises the real URL validation and response handling.
	resolver.fetch.Transport = server.Client().Transport
	data, contentType, err := resolver.fetchBytes(context.Background(), server.URL+"/image?token=supersecret")
	if err != nil || contentType != "image/png" || !bytes.Equal(data, pngBytes) {
		t.Fatalf("HTTP download: type=%q bytes=%d err=%v", contentType, len(data), err)
	}
}

func TestMediaResolver_RejectsUnsupportedDownloadURLScheme(t *testing.T) {
	resolver := &mediaResolver{fetch: http.DefaultClient}
	_, _, err := resolver.fetchBytes(context.Background(), "ftp://files.example/image?token=supersecret")
	if err == nil || strings.Contains(err.Error(), "supersecret") || strings.Contains(err.Error(), "files.example") {
		t.Fatalf("unsupported URL error = %v", err)
	}
}

func TestMediaResolver_ProductionDialerBlocksLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pngBytes)
	}))
	defer server.Close()
	resolver := NewMediaResolver(nil, nil, nil, nil, nil).(*mediaResolver)
	_, _, err := resolver.fetchBytes(context.Background(), server.URL+"/image?token=supersecret")
	if err == nil || !strings.Contains(err.Error(), "blocked non-public download target") || strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("loopback download error = %v", err)
	}
}

func TestIsPublicDownloadAddress(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{address: "8.8.8.8", want: true},
		{address: "2606:4700:4700::1111", want: true},
		{address: "64:ff9b::808:808", want: true},
		{address: "127.0.0.1"},
		{address: "10.0.0.1"},
		{address: "169.254.169.254"},
		{address: "100.64.0.1"},
		{address: "192.0.2.1"},
		{address: "::1"},
		{address: "fd00::1"},
		{address: "fe80::1"},
		{address: "64:ff9b::7f00:1"},
		{address: "64:ff9b:1::7f00:1"},
		{address: "2001::ffff:7f00:1"},
		{address: "2001:db8::1"},
		{address: "2002:7f00:1::"},
	}
	for _, tc := range tests {
		if got := isPublicDownloadAddress(netip.MustParseAddr(tc.address)); got != tc.want {
			t.Errorf("isPublicDownloadAddress(%s) = %v, want %v", tc.address, got, tc.want)
		}
	}
}

func TestMediaResolver_RejectsCrossOriginHTTPRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pngBytes)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/image?token=supersecret", http.StatusFound)
	}))
	defer source.Close()
	resolver := NewMediaResolver(nil, nil, nil, nil, nil).(*mediaResolver)
	resolver.fetch.Transport = source.Client().Transport
	_, _, err := resolver.fetchBytes(context.Background(), source.URL+"/image")
	if err == nil || !strings.Contains(err.Error(), "cross-origin HTTP") || strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("cross-origin redirect error = %v", err)
	}
}

func TestMediaResolver_AllowsSameOriginHTTPRedirectWithoutReferer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/image", http.StatusFound)
			return
		}
		if got := r.Header.Get("Referer"); got != "" {
			t.Errorf("redirect leaked signed URL through Referer: %q", got)
		}
		_, _ = w.Write(pngBytes)
	}))
	defer server.Close()
	resolver := NewMediaResolver(nil, nil, nil, nil, nil).(*mediaResolver)
	resolver.fetch.Transport = server.Client().Transport
	data, contentType, err := resolver.fetchBytes(context.Background(), server.URL+"/start?token=supersecret")
	if err != nil || contentType != "image/png" || !bytes.Equal(data, pngBytes) {
		t.Fatalf("same-origin redirect: type=%q bytes=%d err=%v", contentType, len(data), err)
	}
}

func TestMediaResolver_RejectsHTTPSDowngradeRedirect(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://files.example/image?token=supersecret", http.StatusFound)
	}))
	defer server.Close()
	resolver := NewMediaResolver(nil, nil, nil, nil, nil).(*mediaResolver)
	resolver.fetch.Transport = server.Client().Transport
	_, _, err := resolver.fetchBytes(context.Background(), server.URL+"/image")
	if err == nil || strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("downgrade redirect error = %v", err)
	}
}
