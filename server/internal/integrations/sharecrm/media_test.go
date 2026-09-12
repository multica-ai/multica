package sharecrm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
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

func (f *fakeStorage) ObjectURL(key string) string { return "https://cdn.example/" + key }

type fakeLedger struct {
	mu      sync.Mutex
	records []engine.RecordPendingMediaObjectParams
	owned   bool
}

func (f *fakeLedger) RecordPendingMediaObject(_ context.Context, p engine.RecordPendingMediaObjectParams) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, p)
	if !f.owned {
		return false, nil
	}
	return true, nil
}

func TestHasMedia_FromRawImages(t *testing.T) {
	raw, _ := json.Marshal(sharecrmRawEvent{
		AppID: "app-1",
		Images: []botImageRef{{
			URL:      "https://img.example/sign",
			Filename: "a.jpg",
		}},
	})
	r := &sharecrmMediaResolver{}
	if !r.HasMedia(channel.InboundMessage{Raw: raw}) {
		t.Fatal("HasMedia=false")
	}
}

func TestHasMedia_Empty(t *testing.T) {
	raw, _ := json.Marshal(sharecrmRawEvent{AppID: "app-1"})
	r := &sharecrmMediaResolver{}
	if r.HasMedia(channel.InboundMessage{Raw: raw}) {
		t.Fatal("HasMedia=true")
	}
}

func TestMediaResolver_HappyPathAndIntentLedger(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.png":
			_, _ = w.Write(pngBytes)
		case "/b.jpg":
			_, _ = w.Write(jpegBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	store := newFakeStorage()
	ledger := &fakeLedger{owned: true}
	resolver := NewMediaResolver(store, ledger, nil).(*sharecrmMediaResolver)
	resolver.http.Transport = server.Client().Transport

	inst, messageID, msg := mediaFixture(server.URL+"/a.png?token=supersecret", server.URL+"/b.jpg")
	got := resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 2 {
		t.Fatalf("media refs = %+v", got.MediaRefs)
	}
	if got.MediaRefs[0].MimeType != "image/png" || got.MediaRefs[1].MimeType != "image/jpeg" {
		t.Fatalf("mime types = %q/%q", got.MediaRefs[0].MimeType, got.MediaRefs[1].MimeType)
	}
	if got.MediaRefs[0].InlinePlaceholder != sharecrmImagePlaceholder || got.MediaRefs[0].InlineIndex != 0 ||
		got.MediaRefs[1].InlinePlaceholder != sharecrmImagePlaceholder || got.MediaRefs[1].InlineIndex != 1 {
		t.Fatalf("inline positions = %+v", got.MediaRefs)
	}
	if len(ledger.records) != 2 || len(store.uploads) != 2 {
		t.Fatalf("intents/uploads = %d/%d", len(ledger.records), len(store.uploads))
	}
	for _, ref := range got.MediaRefs {
		if !strings.HasPrefix(ref.StorageKey, "workspaces/ab000000-0000-0000-0000-000000000000/sharecrm/") {
			t.Fatalf("unexpected storage key %q", ref.StorageKey)
		}
	}
}

func TestMediaResolver_RejectsSVG(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(svgBytes)
	}))
	t.Cleanup(server.Close)
	store := newFakeStorage()
	ledger := &fakeLedger{owned: true}
	resolver := NewMediaResolver(store, ledger, nil).(*sharecrmMediaResolver)
	resolver.http.Transport = server.Client().Transport
	inst, messageID, msg := mediaFixture(server.URL + "/x.svg")
	got := resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 0 {
		t.Fatalf("svg leaked through: %+v", got.MediaRefs)
	}
}

func TestMediaResolver_DownloadErrorDoesNotExposeSignedURL(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	server.Close()
	resolver := &sharecrmMediaResolver{http: client}
	_, _, err := resolver.fetchBytes(context.Background(), server.URL+"/image?token=supersecret")
	if err == nil {
		t.Fatal("expected the closed server download to fail")
	}
	if strings.Contains(err.Error(), "supersecret") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("download error leaked signed URL: %v", err)
	}
}

func TestMediaResolver_RejectsUnsupportedDownloadURLScheme(t *testing.T) {
	resolver := &sharecrmMediaResolver{http: http.DefaultClient}
	_, _, err := resolver.fetchBytes(context.Background(), "ftp://files.example/image?token=supersecret")
	if err == nil || strings.Contains(err.Error(), "supersecret") || strings.Contains(err.Error(), "files.example") {
		t.Fatalf("unsupported URL error = %v", err)
	}
}

func TestMediaResolver_ProductionDialerBlocksLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pngBytes)
	}))
	t.Cleanup(server.Close)
	resolver := NewMediaResolver(nil, nil, nil).(*sharecrmMediaResolver)
	_, _, err := resolver.fetchBytes(context.Background(), server.URL+"/image?token=supersecret")
	if err == nil || !strings.Contains(err.Error(), "blocked non-public download target") || strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("loopback download error = %v", err)
	}
}

func TestMediaResolver_RejectsCrossOriginHTTPRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pngBytes)
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/image?token=supersecret", http.StatusFound)
	}))
	t.Cleanup(source.Close)
	resolver := NewMediaResolver(nil, nil, nil).(*sharecrmMediaResolver)
	resolver.http.Transport = source.Client().Transport
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
	t.Cleanup(server.Close)
	resolver := NewMediaResolver(nil, nil, nil).(*sharecrmMediaResolver)
	resolver.http.Transport = server.Client().Transport
	data, contentType, err := resolver.fetchBytes(context.Background(), server.URL+"/start?token=supersecret")
	if err != nil || contentType != "image/png" || !bytes.Equal(data, pngBytes) {
		t.Fatalf("same-origin redirect: type=%q bytes=%d err=%v", contentType, len(data), err)
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
		{address: "2001:db8::1"},
	}
	for _, tc := range tests {
		if got := isPublicDownloadAddress(netip.MustParseAddr(tc.address)); got != tc.want {
			t.Errorf("isPublicDownloadAddress(%s) = %v, want %v", tc.address, got, tc.want)
		}
	}
}

func TestShareCRMChannel_DeclaresAttachmentCapability(t *testing.T) {
	ch := &sharecrmChannel{}
	if ch.Capabilities()&channel.CapAttachment == 0 {
		t.Fatal("inbound images require CapAttachment")
	}
}

func mediaFixture(urls ...string) (engine.ResolvedInstallation, pgtype.UUID, channel.InboundMessage) {
	images := make([]botImageRef, len(urls))
	for i, rawURL := range urls {
		images[i] = botImageRef{URL: rawURL, Filename: "img"}
	}
	var ws, instID, messageID pgtype.UUID
	ws.Bytes[0], instID.Bytes[0], messageID.Bytes[0] = 0xAB, 0xCD, 0xEF
	ws.Valid, instID.Valid, messageID.Valid = true, true, true
	raw, _ := json.Marshal(sharecrmRawEvent{AppID: "app-1", Images: images})
	inst := engine.ResolvedInstallation{ID: instID, WorkspaceID: ws}
	body := inboundImagePlaceholders(len(urls))
	return inst, messageID, channel.InboundMessage{
		MessageID: "sc-msg",
		Type:      channel.MsgTypeImage,
		Text:      body,
		Raw:       raw,
	}
}
