package dingtalk

import (
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
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Magic-number fixtures http.DetectContentType recognizes.
var (
	pngBytes  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 64)...)
	jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...)
	svgBytes  = []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
)

type fakeStorage struct {
	mu        sync.Mutex
	uploads   map[string][]byte
	uploadCT  map[string]string
	deleted   []string
	failOnKey string // substring match: Upload fails when key contains it
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{uploads: map[string][]byte{}, uploadCT: map[string]string{}}
}

func (f *fakeStorage) Upload(_ context.Context, key string, data []byte, contentType string, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOnKey != "" && strings.Contains(key, f.failOnKey) {
		return "", errors.New("fake upload failure")
	}
	f.uploads[key] = data
	f.uploadCT[key] = contentType
	return "https://cdn.example/" + key, nil
}
func (f *fakeStorage) Delete(_ context.Context, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, key)
}
func (f *fakeStorage) DeleteKeys(_ context.Context, keys []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, keys...)
}
func (f *fakeStorage) KeyFromURL(string) string { return "" }
func (f *fakeStorage) CdnDomain() string        { return "" }
func (f *fakeStorage) GetReader(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

// mediaTestEnv wires a fake DingTalk API (token + download resolve) and a fake
// file host. files maps downloadCode → response bytes; a code missing from the
// map resolves with HTTP 400 (like an expired code).
type mediaTestEnv struct {
	api      *httptest.Server
	files    *httptest.Server
	store    *fakeStorage
	ingester *mediaIngester
	resolves *atomic.Int32
}

func newMediaTestEnv(t *testing.T, files map[string][]byte) *mediaTestEnv {
	t.Helper()
	var resolves atomic.Int32
	fileHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Path, "/f/")
		// A code prefixed "err-" resolves fine but fails at download time (a
		// transient 5xx / expired signed link), so tests can exercise the
		// download-failure path separately from the resolve-failure path.
		if strings.HasPrefix(code, "err-") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
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
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
		case messageFilesDownloadPath:
			resolves.Add(1)
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			code := body["downloadCode"]
			if _, ok := files[code]; !ok {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":"invalidParameter.robotCode.downloadCode","message":"expired"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"downloadUrl":%q}`, fileHost.URL+"/f/"+code)
		default:
			t.Errorf("unexpected api path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(api.Close)

	store := newFakeStorage()
	ing := NewMediaIngester(NewClient(nil, api.URL), nil, store, nil)
	return &mediaTestEnv{api: api, files: fileHost, store: store, ingester: ing, resolves: &resolves}
}

func testIngestParams(media ...channel.PendingMedia) engine.IngestParams {
	cfg, _ := json.Marshal(installConfig{
		AppID:              "app-key",
		RobotCode:          "robot-1",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret")),
	})
	var ws pgtype.UUID
	ws.Bytes[0] = 0xAB
	ws.Valid = true
	return engine.IngestParams{
		Installation: engine.ResolvedInstallation{Platform: db.ChannelInstallation{Config: cfg}},
		WorkspaceID:  ws,
		Media:        media,
	}
}

func img(ref, alt string) channel.PendingMedia {
	return channel.PendingMedia{Kind: channel.MsgTypeImage, Ref: ref, Alt: alt}
}

func TestIngest_HappyPathTwoImages(t *testing.T) {
	env := newMediaTestEnv(t, map[string][]byte{"c1": pngBytes, "c2": jpegBytes})
	staged, err := env.ingester.Ingest(context.Background(), testIngestParams(img("c1", ""), img("c2", "")))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(staged) != 2 {
		t.Fatalf("staged = %d, want 2", len(staged))
	}
	if staged[0].Filename != "image-1.png" || staged[1].Filename != "image-2.jpg" {
		t.Errorf("filenames = %q / %q", staged[0].Filename, staged[1].Filename)
	}
	if staged[0].ContentType != "image/png" || staged[1].ContentType != "image/jpeg" {
		t.Errorf("content types = %q / %q", staged[0].ContentType, staged[1].ContentType)
	}
	wantPrefix := "workspaces/ab000000-0000-0000-0000-000000000000/"
	for i, sm := range staged {
		if !strings.HasPrefix(sm.StorageKey, wantPrefix) {
			t.Errorf("staged[%d].StorageKey = %q, want prefix %q", i, sm.StorageKey, wantPrefix)
		}
		if !sm.ID.Valid || sm.URL == "" || sm.SizeBytes == 0 {
			t.Errorf("staged[%d] incomplete: %+v", i, sm)
		}
	}
	if len(env.store.uploads) != 2 {
		t.Errorf("uploads = %d, want 2", len(env.store.uploads))
	}
}

func TestIngest_TooManyImages(t *testing.T) {
	env := newMediaTestEnv(t, map[string][]byte{})
	media := make([]channel.PendingMedia, maxImagesPerMessage+1)
	for i := range media {
		media[i] = img(fmt.Sprintf("c%d", i), "")
	}
	if _, err := env.ingester.Ingest(context.Background(), testIngestParams(media...)); err == nil {
		t.Fatal("expected an error over the image-count limit")
	}
	if env.resolves.Load() != 0 {
		t.Errorf("resolve calls = %d, want 0 (no network before the count check)", env.resolves.Load())
	}
}

func TestIngest_OversizeAborts(t *testing.T) {
	big := make([]byte, maxInboundImageBytes+1)
	copy(big, pngBytes)
	env := newMediaTestEnv(t, map[string][]byte{"c1": big})
	if _, err := env.ingester.Ingest(context.Background(), testIngestParams(img("c1", ""))); err == nil {
		t.Fatal("expected an error over the size limit")
	}
	if len(env.store.uploads) != 0 {
		t.Errorf("uploads = %d, want 0", len(env.store.uploads))
	}
}

func TestIngest_RejectsSVG(t *testing.T) {
	env := newMediaTestEnv(t, map[string][]byte{"c1": svgBytes})
	if _, err := env.ingester.Ingest(context.Background(), testIngestParams(img("c1", ""))); err == nil {
		t.Fatal("expected an error for a non-raster content type")
	}
	if len(env.store.uploads) != 0 {
		t.Errorf("uploads = %d, want 0", len(env.store.uploads))
	}
}

func TestIngest_AltCodeFallback(t *testing.T) {
	env := newMediaTestEnv(t, map[string][]byte{"alt-1": pngBytes})
	staged, err := env.ingester.Ingest(context.Background(), testIngestParams(img("dead-code", "alt-1")))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(staged) != 1 || staged[0].Filename != "image-1.png" {
		t.Fatalf("staged = %+v", staged)
	}
	if env.resolves.Load() != 2 {
		t.Errorf("resolve calls = %d, want 2 (primary then alt)", env.resolves.Load())
	}
}

// The primary code resolves but its download fails transiently; the secondary
// code must still be tried (a single 5xx must not drop the whole message).
func TestIngest_AltCodeFallbackOnDownloadFailure(t *testing.T) {
	env := newMediaTestEnv(t, map[string][]byte{"err-primary": pngBytes, "alt-good": pngBytes})
	staged, err := env.ingester.Ingest(context.Background(), testIngestParams(img("err-primary", "alt-good")))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(staged) != 1 || staged[0].Filename != "image-1.png" {
		t.Fatalf("staged = %+v", staged)
	}
	if env.resolves.Load() != 2 {
		t.Errorf("resolve calls = %d, want 2 (primary resolved+downloaded-failed, then alt)", env.resolves.Load())
	}
	if len(env.store.uploads) != 1 {
		t.Errorf("uploads = %d, want 1 (the alt image)", len(env.store.uploads))
	}
}

func TestIngest_AllOrNothing(t *testing.T) {
	// c2 resolves but serves SVG, so image 2 fails after image 1 uploaded.
	env := newMediaTestEnv(t, map[string][]byte{"c1": pngBytes, "c2": svgBytes})
	if _, err := env.ingester.Ingest(context.Background(), testIngestParams(img("c1", ""), img("c2", ""))); err == nil {
		t.Fatal("expected an error when one image fails")
	}
	env.store.mu.Lock()
	defer env.store.mu.Unlock()
	if len(env.store.uploads) == 0 {
		// Image 1 may have lost the race and never uploaded; that is fine —
		// the invariant is "no orphan object", checked below.
		return
	}
	if len(env.store.deleted) != len(env.store.uploads) {
		t.Errorf("deleted %d keys, uploaded %d — staged objects leaked", len(env.store.deleted), len(env.store.uploads))
	}
}
