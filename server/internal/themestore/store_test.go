package themestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type registry struct {
	server   *httptest.Server
	zip      []byte
	png      []byte
	index    func(base string) any
	requests map[string]*atomic.Int32
	fail     atomic.Bool
}

func newRegistry(t *testing.T) *registry {
	t.Helper()
	r := &registry{
		zip:      []byte("PK fake theme zip"),
		png:      []byte("\x89PNG fake"),
		requests: map[string]*atomic.Int32{},
	}
	for _, p := range []string{"/index.json", "/release/theme.zip", "/screenshots/sample/light.png"} {
		r.requests[p] = &atomic.Int32{}
	}
	r.index = func(base string) any {
		return map[string]any{"schema": 1, "themes": []any{r.entry(base)}}
	}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if counter, ok := r.requests[req.URL.Path]; ok {
			counter.Add(1)
		}
		if r.fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		switch req.URL.Path {
		case "/index.json":
			_ = json.NewEncoder(w).Encode(r.index(r.server.URL))
		case "/release/theme.zip":
			_, _ = w.Write(r.zip)
		case "/screenshots/sample/light.png":
			_, _ = w.Write(r.png)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *registry) entry(base string) map[string]any {
	sum := sha256.Sum256(r.zip)
	return map[string]any{
		"id":          "sample",
		"name":        "Sample",
		"author":      "Multica",
		"repo":        "multica-ai/multica-theme-sample",
		"description": "A sample theme.",
		"modes":       []string{"light", "dark"},
		"version":     "1.0.0",
		"tag":         "v1.0.0",
		"package": map[string]any{
			// httptest serves plain http; the store requires https, so the
			// test client below rewrites https://registry.test to the server.
			"url":    "https://registry.test/release/theme.zip",
			"sha256": hex.EncodeToString(sum[:]),
			"size":   len(r.zip),
		},
		"screenshots": map[string]string{"light": "screenshots/sample/light.png", "dark": "../secret.png"},
	}
}

// rewrite sends https://registry.test requests to the httptest server.
type rewrite struct{ target string }

func (rt rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "registry.test" {
		req = req.Clone(req.Context())
		req.URL.Scheme = "http"
		req.URL.Host = rt.target[len("http://"):]
	}
	return http.DefaultTransport.RoundTrip(req)
}

func newStore(t *testing.T, r *registry, now *time.Time) *Store {
	t.Helper()
	store, err := New(Config{
		IndexURL:   r.server.URL + "/index.json",
		HTTPClient: &http.Client{Transport: rewrite{target: r.server.URL}},
		Now:        func() time.Time { return *now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestThemesCachesTheIndexAndServesStaleCopyWhenTheRegistryIsDown(t *testing.T) {
	r := newRegistry(t)
	now := time.Unix(0, 0)
	store := newStore(t, r, &now)
	ctx := context.Background()

	themes, err := store.Themes(ctx)
	if err != nil || len(themes) != 1 || themes[0].ID != "sample" {
		t.Fatalf("Themes = %+v, %v", themes, err)
	}
	if got := themes[0].Screenshots; len(got) != 1 || got["light"] == "" {
		t.Fatalf("screenshot paths outside the registry must be dropped, got %v", got)
	}
	if _, err := store.Themes(ctx); err != nil {
		t.Fatal(err)
	}
	if n := r.requests["/index.json"].Load(); n != 1 {
		t.Fatalf("index fetched %d times within the TTL, want 1", n)
	}

	now = now.Add(defaultIndexTTL + time.Second)
	r.fail.Store(true)
	themes, err = store.Themes(ctx)
	if err != nil || len(themes) != 1 {
		t.Fatalf("stale index must be served while the registry is down: %+v, %v", themes, err)
	}
	if _, err := store.Themes(ctx); err != nil {
		t.Fatal(err)
	}
	if n := r.requests["/index.json"].Load(); n != 2 {
		t.Fatalf("index fetched %d times, want 2: one refresh, then back off", n)
	}
}

func TestThemesFailsWithoutACachedIndex(t *testing.T) {
	r := newRegistry(t)
	r.fail.Store(true)
	now := time.Unix(0, 0)
	if _, err := newStore(t, r, &now).Themes(context.Background()); !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream, got %v", err)
	}
}

func TestPackageIsVerifiedAndCached(t *testing.T) {
	r := newRegistry(t)
	now := time.Unix(0, 0)
	store := newStore(t, r, &now)
	ctx := context.Background()

	theme, data, err := store.Package(ctx, "sample")
	if err != nil || string(data) != string(r.zip) || theme.Version != "1.0.0" {
		t.Fatalf("Package = %q, %v", data, err)
	}
	if _, _, err := store.Package(ctx, "sample"); err != nil {
		t.Fatal(err)
	}
	if n := r.requests["/release/theme.zip"].Load(); n != 1 {
		t.Fatalf("package fetched %d times, want 1", n)
	}
	if _, _, err := store.Package(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unlisted id: want ErrNotFound, got %v", err)
	}
}

func TestPackageRejectsAReleaseThatChangedAfterTheCheck(t *testing.T) {
	r := newRegistry(t)
	index := r.index
	r.index = func(base string) any {
		v := index(base).(map[string]any)
		r.zip = []byte("PK replaced after the registry checked it")
		return v
	}
	now := time.Unix(0, 0)
	if _, _, err := newStore(t, r, &now).Package(context.Background(), "sample"); !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream for a mismatched sha256, got %v", err)
	}
}

func TestScreenshot(t *testing.T) {
	r := newRegistry(t)
	now := time.Unix(0, 0)
	store := newStore(t, r, &now)
	data, kind, err := store.Screenshot(context.Background(), "sample", "light")
	if err != nil || string(data) != string(r.png) || kind != "image/png" {
		t.Fatalf("Screenshot = %q %q %v", data, kind, err)
	}
	if _, _, err := store.Screenshot(context.Background(), "sample", "dark"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dropped screenshot: want ErrNotFound, got %v", err)
	}
}

func TestValidThemeRejectsMalformedEntries(t *testing.T) {
	r := newRegistry(t)
	base := func() Theme {
		raw, _ := json.Marshal(r.entry("https://registry.test"))
		var theme Theme
		_ = json.Unmarshal(raw, &theme)
		return theme
	}
	if _, ok := validTheme(base()); !ok {
		t.Fatal("the sample entry must be valid")
	}
	cases := map[string]func(*Theme){
		"id":           func(t *Theme) { t.ID = "Bad ID" },
		"repo":         func(t *Theme) { t.Repo = "not a repo" },
		"http package": func(t *Theme) { t.Package.URL = "http://example.com/theme.zip" },
		"sha256":       func(t *Theme) { t.Package.SHA256 = "abc" },
		"size":         func(t *Theme) { t.Package.Size = MaxPackageBytes + 1 },
		"modes":        func(t *Theme) { t.Modes = []string{"sepia"} },
		"name":         func(t *Theme) { t.Name = " " },
	}
	for name, mutate := range cases {
		theme := base()
		mutate(&theme)
		if _, ok := validTheme(theme); ok {
			t.Errorf("%s: malformed entry accepted", name)
		}
	}
}

func TestValidThemeCountsCharactersNotBytes(t *testing.T) {
	r := newRegistry(t)
	raw, _ := json.Marshal(r.entry("https://registry.test"))
	var theme Theme
	_ = json.Unmarshal(raw, &theme)

	theme.Name = strings.Repeat("亚麻", 30)
	theme.Author = strings.Repeat("作者", 30)
	theme.Description = strings.Repeat("温暖纸色与柔和的文字。", 27)
	if _, ok := validTheme(theme); !ok {
		t.Fatal("a 60-character Chinese name and author and a 297-character description must be valid")
	}

	theme.Description = strings.Repeat("温暖纸色与柔和的文字。", 28)
	if _, ok := validTheme(theme); ok {
		t.Fatal("a 308-character description must be rejected")
	}

	theme.Description = ""
	theme.Name = strings.Repeat("🎨", 31)
	if _, ok := validTheme(theme); ok {
		t.Fatal("31 emoji are 62 UTF-16 code units, over the registry's 60")
	}
}

func TestThemesKeepsChineseEntries(t *testing.T) {
	r := newRegistry(t)
	index := r.index
	r.index = func(base string) any {
		v := index(base).(map[string]any)
		entry := v["themes"].([]any)[0].(map[string]any)
		entry["name"] = "亚麻"
		entry["description"] = strings.Repeat("温暖纸色与柔和的文字。", 10)
		return v
	}
	now := time.Unix(0, 0)
	themes, err := newStore(t, r, &now).Themes(context.Background())
	if err != nil || len(themes) != 1 || themes[0].Name != "亚麻" {
		t.Fatalf("Themes = %+v, %v", themes, err)
	}
}

func TestLRUEvictsLeastRecentlyUsed(t *testing.T) {
	c := newLRU(10)
	c.put("a", make([]byte, 4))
	c.put("b", make([]byte, 4))
	c.get("a")
	c.put("c", make([]byte, 4))
	if _, ok := c.get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if _, ok := c.get("a"); !ok {
		t.Fatal("a was used recently and must stay")
	}
	c.put("huge", make([]byte, 11))
	if _, ok := c.get("huge"); ok {
		t.Fatal("an entry larger than the cache must not be stored")
	}
}
