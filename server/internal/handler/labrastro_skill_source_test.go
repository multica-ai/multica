package handler

import (
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type labrastroSourceFixture struct {
	Owner                       string            `json:"owner"`
	Repo                        string            `json:"repo"`
	Commit                      string            `json:"commit"`
	TreeSHA                     string            `json:"tree_sha"`
	Tree                        []githubTreeEntry `json:"tree"`
	Files                       map[string]string `json:"files"`
	mu                          sync.Mutex
	requests, active, maxActive int
	paths                       []string
	truncated                   bool
	failPath                    string
	delay                       time.Duration
}
type labrastroFixtureTransport struct{ f *labrastroSourceFixture }

func (tr labrastroFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f := tr.f
	f.mu.Lock()
	f.requests++
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.paths = append(f.paths, r.URL.Path+"?"+r.URL.RawQuery)
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.active--; f.mu.Unlock() }()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	status := 200
	body := ""
	base := "/repos/" + f.Owner + "/" + f.Repo
	switch {
	case r.URL.Host == "api.github.com" && r.URL.Path == base:
		body = `{"default_branch":"main"}`
	case r.URL.Host == "api.github.com" && (r.URL.Path == base+"/commits/main" || r.URL.Path == base+"/commits/"+f.Commit || r.URL.Path == base+"/commits/v2"):
		b, _ := json.Marshal(map[string]any{"sha": f.Commit, "commit": map[string]any{"tree": map[string]string{"sha": f.TreeSHA}}})
		body = string(b)
	case r.URL.Host == "api.github.com" && strings.HasPrefix(r.URL.Path, base+"/git/trees/"):
		id := strings.TrimPrefix(r.URL.Path, base+"/git/trees/")
		dir := ""
		found := id == f.TreeSHA || id == "main" || id == f.Commit || id == "v2"
		for _, e := range f.Tree {
			if e.Type == "tree" && e.SHA == id {
				dir = e.Path
				found = true
			}
		}
		if !found {
			status = 404
			break
		}
		entries := []githubTreeEntry{}
		for _, e := range f.Tree {
			if !labrastroInside(e.Path, dir) {
				continue
			}
			rel := e.Path
			if dir != "" {
				rel = strings.TrimPrefix(e.Path, dir+"/")
			}
			if r.URL.Query().Get("recursive") != "1" && strings.Contains(rel, "/") {
				continue
			}
			e.Path = rel
			entries = append(entries, e)
		}
		b, _ := json.Marshal(githubTreeResponse{Tree: entries, Truncated: f.truncated && dir == ""})
		body = string(b)
	case r.URL.Host == "api.github.com" && (r.URL.Path == base+"/contents" || strings.HasPrefix(r.URL.Path, base+"/contents/")):
		ref := r.URL.Query().Get("ref")
		if ref != "main" && ref != "v2" && ref != f.Commit {
			status = 404
			break
		}
		dir := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, base+"/contents"), "/")
		found := dir == ""
		entries := []githubContentEntry{}
		for _, e := range f.Tree {
			if e.Type == "tree" && e.Path == dir {
				found = true
			}
			parent := path.Dir(e.Path)
			if parent == "." {
				parent = ""
			}
			if parent != dir {
				continue
			}
			entry := githubContentEntry{Name: path.Base(e.Path), Path: e.Path, URL: buildGitHubContentsURL(f.Owner, f.Repo, e.Path, ref)}
			switch e.Type {
			case "tree":
				entry.Type = "dir"
			case "blob":
				entry.Type = "file"
				entry.DownloadURL = buildRawGitHubURL("https://raw.githubusercontent.com/"+f.Owner+"/"+f.Repo+"/"+ref, e.Path)
			default:
				continue
			}
			entries = append(entries, entry)
		}
		if !found {
			status = 404
			break
		}
		b, _ := json.Marshal(entries)
		body = string(b)
	case r.URL.Host == "raw.githubusercontent.com":
		prefix := "/" + f.Owner + "/" + f.Repo + "/"
		p := strings.TrimPrefix(r.URL.Path, prefix)
		_, p, _ = strings.Cut(p, "/")
		if f.failPath != "" && p == f.failPath {
			status = 503
			break
		}
		var ok bool
		body, ok = f.Files[p]
		if !ok {
			status = 404
		}
	default:
		status = 404
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
func (f *labrastroSourceFixture) client() *http.Client {
	return &http.Client{Transport: labrastroFixtureTransport{f}}
}
func (f *labrastroSourceFixture) install(t *testing.T) {
	t.Helper()
	prev := http.DefaultTransport
	http.DefaultTransport = labrastroFixtureTransport{f}
	t.Cleanup(func() { http.DefaultTransport = prev })
}
func (f *labrastroSourceFixture) url() string { return "https://github.com/" + f.Owner + "/" + f.Repo }
func labrastroTestFixture(files map[string]string) *labrastroSourceFixture {
	f := &labrastroSourceFixture{Owner: "fixture", Repo: "skills", Files: files}
	f.rebuild()
	return f
}
func (f *labrastroSourceFixture) rebuild() {
	sha := func(s string) string { sum := sha1.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	f.Commit = sha(labrastroJSONHash(f.Files))
	f.TreeSHA = sha("root" + f.Commit)
	f.Tree = nil
	dirs := map[string]bool{}
	for p, body := range f.Files {
		f.Tree = append(f.Tree, githubTreeEntry{Path: p, Type: "blob", Mode: "100644", Size: int64(len(body)), SHA: labrastroGitBlobSHA([]byte(body))})
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	for d := range dirs {
		f.Tree = append(f.Tree, githubTreeEntry{Path: d, Type: "tree", Mode: "040000", SHA: sha(d + f.Commit)})
	}
	sort.Slice(f.Tree, func(i, j int) bool { return f.Tree[i].Path < f.Tree[j].Path })
}
func labrastroReadSourceFixture(t *testing.T, name string) *labrastroSourceFixture {
	t.Helper()
	file, err := os.Open("testdata/labrastro-skill-packages/" + name + ".json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	var f labrastroSourceFixture
	if err = json.NewDecoder(gz).Decode(&f); err != nil {
		t.Fatal(err)
	}
	return &f
}

func TestLabrastroPinnedSourceFixtures(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		count, defaults, shared int
	}{{"mattpocock", 37, 27, 0}, {"addyosmani", 25, 25, 11}} {
		t.Run(tc.name, func(t *testing.T) {
			f := labrastroReadSourceFixture(t, tc.name)
			f.delay = time.Millisecond
			runtime.GC()
			before := runtime.MemStats{}
			runtime.ReadMemStats(&before)
			stop, sampled := make(chan struct{}), make(chan uint64)
			go func() {
				peak := before.HeapAlloc
				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				for {
					stats := runtime.MemStats{}
					runtime.ReadMemStats(&stats)
					if stats.HeapAlloc > peak {
						peak = stats.HeapAlloc
					}
					select {
					case <-stop:
						sampled <- peak - before.HeapAlloc
						return
					case <-ticker.C:
					}
				}
			}()
			defer func() { close(stop); t.Logf("sampled_peak_heap_delta=%d bytes (1ms sampling)", <-sampled) }()
			start := time.Now()
			ctx, cancel := context.WithTimeout(t.Context(), importFetchTimeout)
			defer cancel()
			src, err := newLabrastroSkillSource(ctx, f.client(), f.url(), t.Name())
			if err != nil {
				t.Fatal(err)
			}
			cs, ds, err := src.candidates(ctx)
			if err != nil {
				t.Fatal(err)
			}
			selected, shared, largestBundle := 0, 0, 0
			for _, c := range cs {
				if c.State == "failed" {
					t.Fatalf("%s failed: %+v", c.Path, c.Diagnostics)
				}
				if c.DefaultSelected {
					selected++
				}
				if len(c.SharedFiles) > 0 {
					shared++
				}
				if c.Bytes > largestBundle {
					largestBundle = c.Bytes
				}
			}
			if len(cs) != tc.count || selected != tc.defaults || shared != tc.shared {
				t.Fatalf("got candidates/defaults/shared %d/%d/%d, want %d/%d/%d; diagnostics=%+v", len(cs), selected, shared, tc.count, tc.defaults, tc.shared, ds)
			}
			if src.treeRequests != 1 || src.blobs.bytes > labrastroBlobCacheBytes || f.maxActive > treeDownloadConcurrency {
				t.Fatalf("unbounded scan: trees=%d cache=%d concurrent=%d", src.treeRequests, src.blobs.bytes, f.maxActive)
			}
			after := runtime.MemStats{}
			runtime.ReadMemStats(&after)
			t.Logf("snapshot=%s candidates=%d requests=%d trees=%d peak_downloads=%d cache_bytes=%d largest_bundle_bytes=%d total_alloc_delta=%d elapsed=%s", f.Commit, len(cs), f.requests, src.treeRequests, f.maxActive, src.blobs.bytes, largestBundle, after.TotalAlloc-before.TotalAlloc, time.Since(start))
		})
	}
}

func TestLabrastroScopedTreeAndManifest(t *testing.T) {
	f := labrastroTestFixture(map[string]string{"skills/demo/SKILL.md": "---\nname: demo\n---\n[shared](../../references/a.md)", "references/a.md": "shared", "outside/SKILL.md": "outside", ".claude-plugin/plugin.json": `{"skills":"./skills"}`})
	src, err := newLabrastroSkillSource(t.Context(), f.client(), f.url()+"/tree/main/skills", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	cs, _, err := src.candidates(t.Context())
	if err != nil || len(cs) != 1 || len(cs[0].SharedFiles) != 1 {
		t.Fatalf("scoped scan: %+v %v", cs, err)
	}
	for _, p := range f.paths {
		if strings.Contains(p, "/git/trees/"+f.TreeSHA+"?recursive=1") {
			t.Fatalf("subdirectory scan requested the full repository tree: %v", f.paths)
		}
	}
	f.Files[".claude-plugin/plugin.json"] = `{"skills":["../escape"]}`
	f.rebuild()
	src, err = newLabrastroSkillSource(t.Context(), f.client(), f.url(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	cs, ds, err := src.candidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.DefaultSelected {
			t.Fatal("invalid manifest silently selected candidates")
		}
	}
	if len(ds) != 1 || ds[0].Code != "manifest_invalid" {
		t.Fatalf("missing manifest diagnostic: %+v", ds)
	}
}
func TestLabrastroSourceFailureAndTimeout(t *testing.T) {
	f := labrastroTestFixture(map[string]string{"skills/demo/SKILL.md": "[a](../../references/a.md)", "references/a.md": "shared"})
	f.truncated = true
	if _, err := newLabrastroSkillSource(t.Context(), f.client(), f.url(), t.Name()); err == nil {
		t.Fatal("truncated tree accepted")
	}
	f.truncated = false
	src, err := newLabrastroSkillSource(t.Context(), f.client(), f.url(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	f.failPath = "references/a.md"
	cs, _, err := src.candidates(t.Context())
	if err != nil || len(cs) != 1 || cs[0].State != "failed" || cs[0].Diagnostics[0].Code != "required_reference_unavailable" {
		t.Fatalf("missing required reference was accepted: %+v %v", cs, err)
	}
	f.delay = time.Second
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if _, err := newLabrastroSkillSource(ctx, f.client(), f.url(), t.Name()); err == nil {
		t.Fatal("cancelled scan accepted")
	}
}
func TestLabrastroSingleImportAndRefreshReferences(t *testing.T) {
	f := labrastroReadSourceFixture(t, "addyosmani")
	for _, source := range []string{f.url() + "/tree/main/skills/shipping-and-launch", "https://skills.sh/" + f.Owner + "/" + f.Repo + "/shipping-and-launch"} {
		kind, normalized, err := detectImportSource(source)
		if err != nil {
			t.Fatal(err)
		}
		originType := "github"
		if kind == sourceSkillsSh {
			originType = "skills_sh"
		}
		s, err := fetchImportedSkillFromOrigin(t.Context(), f.client(), skillOriginRef{Type: originType, SourceURL: normalized})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(s.content, "_shared/references/") {
			t.Fatalf("%s omitted shared references", source)
		}
		if s.origin["path"] != "skills/shipping-and-launch" {
			t.Fatalf("unresolved origin: %v", s.origin)
		}
	}
	s, err := fetchFromGitHub(t.Context(), f.client(), f.url()+"/tree/main/skills/security-and-hardening")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range s.files {
		if file.path == "references/hardening-patterns.md" {
			found = strings.Contains(file.content, "../_shared/references/security-checklist.md")
		}
	}
	if !found {
		t.Fatal("second-level reference was not rewritten relative to its own file")
	}
}

func TestLabrastroManifestOptionalSkills(t *testing.T) {
	for _, tc := range []struct {
		name, manifest string
		selected       int
		invalid        bool
	}{
		{"absent", "", 2, false},
		{"no skills", `{"name":"x","version":"1.0.0"}`, 2, false},
		{"empty object", `{}`, 2, false},
		{"empty list", `{"skills":[]}`, 0, false},
		{"directory", `{"skills":"./skills"}`, 1, false},
		{"list", `{"skills":["./extra/beta"]}`, 1, false},
		{"null manifest", `null`, 0, true},
		{"null skills", `{"skills":null}`, 0, true},
		{"null list item", `{"skills":[null]}`, 0, true},
		{"object", `{"skills":{}}`, 0, true},
		{"escape", `{"skills":["../outside"]}`, 0, true},
		{"malformed", `{"name":`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"skills/alpha/SKILL.md": "alpha", "extra/beta/SKILL.md": "beta"}
			if tc.manifest != "" {
				files[".claude-plugin/plugin.json"] = tc.manifest
			}
			f := labrastroTestFixture(files)
			src, err := newLabrastroSkillSource(t.Context(), f.client(), f.url(), t.Name())
			if err != nil {
				t.Fatal(err)
			}
			candidates, diags, err := src.candidates(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			selected := 0
			for _, c := range candidates {
				if c.DefaultSelected {
					selected++
				}
			}
			if selected != tc.selected || (len(diags) > 0) != tc.invalid || tc.invalid && diags[0].Code != "manifest_invalid" {
				t.Fatalf("selected=%d diagnostics=%+v", selected, diags)
			}
		})
	}
}
