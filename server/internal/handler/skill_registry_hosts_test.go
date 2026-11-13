package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func registryFixture(t *testing.T, zipMode string) (*httptest.Server, *int) {
	t.Helper()
	calls := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/clawhub.json":
			json.NewEncoder(w).Encode(map[string]string{"apiBase": "/custom/api"})
		case r.URL.Path == "/custom/api/skills/example--skill":
			*calls++
			json.NewEncoder(w).Encode(map[string]any{"skill": map[string]any{"slug": "example--skill", "displayName": "Example", "summary": "A skill"}, "latestVersion": map[string]string{"version": "1.0"}})
		case r.URL.Path == "/custom/api/skills/example--skill/versions/1.0":
			if zipMode == "listing" {
				http.Error(w, "unsupported", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"version": map[string]any{"files": []map[string]string{{"path": "SKILL.md"}, {"path": "notes.txt"}}}})
		case r.URL.Path == "/custom/api/skills/example--skill/file":
			if zipMode == "file" {
				http.Error(w, "unsupported", http.StatusBadRequest)
				return
			}
			if r.URL.Query().Get("path") == "SKILL.md" {
				w.Write([]byte("---\nname: Example\n---\nbody"))
			} else {
				w.Write([]byte("notes"))
			}
		case r.URL.Path == "/custom/api/download":
			if r.URL.Query().Get("slug") != "example--skill" {
				t.Errorf("wrong slug: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/zip")
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			f, _ := zw.Create("SKILL.md")
			f.Write([]byte("---\nname: Example\n---\narchive body"))
			f, _ = zw.Create("notes.txt")
			f.Write([]byte("notes"))
			zw.Close()
			w.Write(buf.Bytes())
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	return srv, calls
}

func TestRegistryWebURLSlug(t *testing.T) {
	t.Setenv("MULTICA_SKILL_REGISTRY_HOSTS", "registry.example.com")
	for _, tc := range []struct {
		name, raw, want string
		invalid         bool
	}{
		{"global", "https://registry.example.com/space/global/grilling", "grilling", false},
		{"namespace", "https://registry.example.com/space/github/ask-matt", "github--ask-matt", false},
		{"single", "https://registry.example.com/grilling", "grilling", false},
		{"owner", "https://registry.example.com/owner/grilling", "grilling", false},
		{"unlisted", "https://other.example.com/space/github/ask-matt", "", true},
		{"extra", "https://registry.example.com/space/github/ask-matt/more", "", true},
		{"empty", "https://registry.example.com/space//ask-matt", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseClawHubSlug(tc.raw)
			if (err != nil) != tc.invalid || (!tc.invalid && got != tc.want) {
				t.Fatalf("slug=%q error=%v, want %q invalid=%v", got, err, tc.want, tc.invalid)
			}
		})
	}
}

func TestRegistryHostImportAndRefresh(t *testing.T) {
	for _, mode := range []string{"files", "listing", "file"} {
		t.Run(mode, func(t *testing.T) {
			srv, calls := registryFixture(t, mode)
			defer srv.Close()
			host, _ := url.Parse(srv.URL)
			raw := srv.URL + "/owner/example--skill"
			t.Setenv("MULTICA_SKILL_REGISTRY_HOSTS", host.Host)
			source, normalized, err := detectImportSource(raw)
			if err != nil || source != sourceClawHub || normalized != raw {
				t.Fatalf("detect: %v %q %v", source, normalized, err)
			}
			imported, err := fetchFromClawHub(context.Background(), srv.Client(), normalized)
			if err != nil {
				t.Fatal(err)
			}
			want := "body"
			if mode != "files" {
				want = "archive body"
			}
			if !strings.Contains(imported.content, want) || len(imported.files) != 1 || imported.files[0].path != "notes.txt" {
				t.Fatalf("import: %#v", imported)
			}
			if imported.origin["source_url"] != raw || imported.origin["slug"] != "example--skill" {
				t.Fatalf("origin: %#v", imported.origin)
			}
			refreshed, err := fetchImportedSkillFromOrigin(context.Background(), srv.Client(), skillOriginRef{Type: "clawhub", SourceURL: raw})
			if err != nil || refreshed.origin["source_url"] != raw || *calls != 2 {
				t.Fatalf("refresh: %#v %v calls=%d", refreshed, err, *calls)
			}
		})
	}
}

func TestRegistryEndpointOverrideImportAndRefresh(t *testing.T) {
	for _, mode := range []string{"files", "listing", "file"} {
		t.Run(mode, func(t *testing.T) {
			srv, calls := registryFixture(t, mode)
			defer srv.Close()
			raw := "https://registry.example.com/space/example/skill"
			t.Setenv("MULTICA_SKILL_REGISTRY_HOSTS", "registry.example.com="+srv.URL)
			source, normalized, err := detectImportSource(raw)
			if err != nil || source != sourceClawHub || normalized != raw {
				t.Fatalf("detect: %v %q %v", source, normalized, err)
			}
			imported, err := fetchFromClawHub(context.Background(), srv.Client(), raw)
			if err != nil || imported.origin["slug"] != "example--skill" || imported.origin["source_url"] != raw {
				t.Fatalf("import: %#v %v", imported, err)
			}
			refreshed, err := fetchImportedSkillFromOrigin(context.Background(), srv.Client(), skillOriginRef{Type: "clawhub", SourceURL: raw})
			if err != nil || refreshed.origin["source_url"] != raw || *calls != 2 {
				t.Fatalf("refresh: %#v %v calls=%d", refreshed, err, *calls)
			}
		})
	}
}

func TestRegistryEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{"http://registry.example.com/path", "http://user@registry.example.com", "file:///tmp/skill", "http://registry.example.com?x=1"} {
		t.Setenv("MULTICA_SKILL_REGISTRY_HOSTS", "registry.example.com="+endpoint)
		if registryHostAllowed("registry.example.com") {
			t.Fatalf("accepted invalid endpoint %q", endpoint)
		}
	}
}

func TestRegistryDiscoveryFallbackAndRedirect(t *testing.T) {
	t.Setenv("MULTICA_SKILL_REGISTRY_HOSTS", "registry.example.com")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/clawhub.json" {
			http.Error(w, "unavailable", http.StatusNotFound)
			return
		}
		if r.URL.Path == "/api/v1/download" {
			http.Redirect(w, r, "https://registry.example.com.evil.test/archive", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	t.Setenv("MULTICA_SKILL_REGISTRY_HOSTS", parsed.Host)
	client := registryHTTPClient(upstream.Client(), parsed)
	if got := registryAPIBase(context.Background(), client, parsed); got != upstream.URL+"/api/v1" {
		t.Fatalf("fallback: %s", got)
	}
	if _, err := fetchRegistryArchive(context.Background(), client, upstream.URL+"/api/v1", "skill"); err == nil || !strings.Contains(err.Error(), "leaves configured origin") {
		t.Fatalf("cross-host redirect: %v", err)
	}
}

func TestRegistryArchiveSameOriginRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/download" {
			http.Redirect(w, r, "/archive", http.StatusFound)
			return
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, _ := zw.Create("SKILL.md")
		f.Write([]byte("---\nname: Example\n---\nbody"))
		zw.Close()
		w.Write(buf.Bytes())
	}))
	defer srv.Close()
	parsed, _ := url.Parse(srv.URL)
	skill, err := fetchRegistryArchive(context.Background(), registryHTTPClient(srv.Client(), parsed), srv.URL+"/api/v1", "skill")
	if err != nil || !strings.Contains(skill.content, "body") {
		t.Fatalf("redirect: %#v %v", skill, err)
	}
}

func TestRegistryHostAllowlist(t *testing.T) {
	t.Setenv("MULTICA_SKILL_REGISTRY_HOSTS", "registry.example.com")
	if _, _, err := detectImportSource("https://registry.example.com.evil.test/owner/skill"); err == nil || !strings.Contains(err.Error(), "unsupported source") {
		t.Fatalf("unlisted host: %v", err)
	}
	t.Setenv("MULTICA_SKILL_REGISTRY_HOSTS", "")
	if _, _, err := detectImportSource("https://registry.example.com/owner/skill"); err == nil {
		t.Fatal("default must reject")
	}
	if src, _, err := detectImportSource("https://clawhub.ai/owner/skill"); err != nil || src != sourceClawHub {
		t.Fatalf("existing clawhub: %v %v", src, err)
	}
}
