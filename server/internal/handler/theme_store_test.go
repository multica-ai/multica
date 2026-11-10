package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/themestore"
)

type themeStoreTransport func(*http.Request) (*http.Response, error)

func (f themeStoreTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func themeStoreRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/theme-store/themes", h.ListStoreThemes)
	r.Get("/api/theme-store/themes/{themeId}/package", h.GetStoreThemePackage)
	r.Get("/api/theme-store/themes/{themeId}/screenshots/{mode}", h.GetStoreThemeScreenshot)
	return r
}

func TestThemeStoreRoutesAreOffWithoutAStore(t *testing.T) {
	router := themeStoreRouter(&Handler{})
	for _, path := range []string{
		"/api/theme-store/themes",
		"/api/theme-store/themes/sample/package",
		"/api/theme-store/themes/sample/screenshots/light",
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: want 404 with the store off, got %d", path, w.Code)
		}
	}
}

func TestThemeStoreRoutes(t *testing.T) {
	zip := []byte("PK theme")
	sum := sha256.Sum256(zip)
	var upstream atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.Add(1)
		switch r.URL.Path {
		case "/index.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"themes": []any{map[string]any{
				"id": "sample", "name": "Sample", "author": "Multica", "repo": "multica-ai/multica-theme-sample",
				"modes": []string{"light", "dark"}, "version": "1.0.0", "tag": "v1.0.0",
				"package":     map[string]any{"url": "https://registry.test/theme.zip", "sha256": hex.EncodeToString(sum[:]), "size": len(zip)},
				"screenshots": map[string]string{"light": "shots/light.png"},
			}}})
		case "/theme.zip":
			_, _ = w.Write(zip)
		case "/shots/light.png":
			_, _ = w.Write([]byte("png"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &http.Client{Transport: themeStoreTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "registry.test" {
			req = req.Clone(req.Context())
			req.URL.Scheme, req.URL.Host = "http", strings.TrimPrefix(server.URL, "http://")
		}
		return http.DefaultTransport.RoundTrip(req)
	})}
	store, err := themestore.New(themestore.Config{IndexURL: server.URL + "/index.json", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	router := themeStoreRouter(&Handler{ThemeStore: store})
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	w := get("/api/theme-store/themes")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Themes []ThemeStoreTheme `json:"themes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Themes) != 1 || list.Themes[0].Version != "1.0.0" || strings.Join(list.Themes[0].Screenshots, ",") != "light" {
		t.Fatalf("list = %+v", list.Themes)
	}
	if strings.Contains(w.Body.String(), "registry.test") || strings.Contains(w.Body.String(), "shots/") {
		t.Fatalf("download URLs and screenshot paths must stay on the server: %s", w.Body.String())
	}

	w = get("/api/theme-store/themes/sample/package")
	if w.Code != http.StatusOK || w.Body.String() != string(zip) || w.Header().Get("X-Theme-Version") != "1.0.0" {
		t.Fatalf("package: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	w = get("/api/theme-store/themes/sample/screenshots/light")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("screenshot: %d %v", w.Code, w.Header())
	}
	if w := get("/api/theme-store/themes/sample/screenshots/dark"); w.Code != http.StatusNotFound {
		t.Fatalf("missing screenshot: want 404, got %d", w.Code)
	}
	if w := get("/api/theme-store/themes/unlisted/package"); w.Code != http.StatusNotFound {
		t.Fatalf("unlisted theme: want 404, got %d", w.Code)
	}
	if n := upstream.Load(); n != 3 {
		t.Fatalf("upstream requests = %d, want 3 (index, zip, screenshot)", n)
	}
}

func TestThemeStoreRegistryDownIsABadGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer server.Close()
	store, err := themestore.New(themestore.Config{IndexURL: server.URL + "/index.json"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	themeStoreRouter(&Handler{ThemeStore: store}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/theme-store/themes", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", w.Code)
	}
}
