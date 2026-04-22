package handler

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFetchFromSkillsSh_UsesEntryURLForNestedDirectories(t *testing.T) {
	client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/contents/skills/pptx":
				if got := r.URL.Query().Get("ref"); got != "main" {
					t.Fatalf("top-level ref = %q, want main", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "editing.md",
						Path:        "skills/pptx/editing.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/skills/main/skills/pptx/editing.md",
					},
					{
						Name: "scripts",
						Path: "skills/pptx/scripts",
						Type: "dir",
						URL:  "https://api.github.com/repos/acme/skills/contents/skills/pptx/scripts?ref=main",
					},
				})
			case "/repos/acme/skills/contents/skills/pptx/scripts":
				if got := r.URL.Query().Get("ref"); got != "main" {
					t.Fatalf("scripts ref = %q, want main", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "add_slide.py",
						Path:        "skills/pptx/scripts/add_slide.py",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/skills/main/skills/pptx/scripts/add_slide.py",
					},
					{
						Name: "office",
						Path: "skills/pptx/scripts/office",
						Type: "dir",
						URL:  "https://api.github.com/repos/acme/skills/contents/skills/pptx/scripts/office?ref=main",
					},
				})
			case "/repos/acme/skills/contents/skills/pptx/scripts/office":
				if got := r.URL.Query().Get("ref"); got != "main" {
					t.Fatalf("office ref = %q, want main", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "foo.py",
						Path:        "skills/pptx/scripts/office/foo.py",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/skills/main/skills/pptx/scripts/office/foo.py",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/pptx/SKILL.md":
				w.Write([]byte("---\nname: PPTX\n---\ncontent"))
			case "/acme/skills/main/skills/pptx/editing.md":
				w.Write([]byte("editing"))
			case "/acme/skills/main/skills/pptx/scripts/add_slide.py":
				w.Write([]byte("print('slide')"))
			case "/acme/skills/main/skills/pptx/scripts/office/foo.py":
				w.Write([]byte("print('office')"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromSkillsSh(client, "https://skills.sh/acme/skills/pptx")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}

	gotPaths := importedFilePaths(result.files)
	wantPaths := []string{"editing.md", "scripts/add_slide.py", "scripts/office/foo.py"}
	if !equalStrings(gotPaths, wantPaths) {
		t.Fatalf("files = %v, want %v", gotPaths, wantPaths)
	}
	if !containsString(*requests, "api.github.com /repos/acme/skills/contents/skills/pptx/scripts?ref=main") {
		t.Fatalf("expected scripts directory to be fetched via entry.URL, got requests %v", *requests)
	}
	if containsString(*requests, "api.github.com /repos/acme/skills/contents/skills/pptx?ref=main/scripts") {
		t.Fatalf("saw buggy query-appended request: %v", *requests)
	}
}

func TestFetchFromSkillsSh_FallbackDoesNotDoubleEscapeDirectoryNames(t *testing.T) {
	client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/contents/skills/pptx":
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name: "my dir",
						Path: "skills/pptx/my dir",
						Type: "dir",
					},
				})
			case "/repos/acme/skills/contents/skills/pptx/my dir":
				if got := r.URL.Query().Get("ref"); got != "main" {
					t.Fatalf("fallback ref = %q, want main", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "note.md",
						Path:        "skills/pptx/my dir/note.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/skills/main/skills/pptx/my%20dir/note.md",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/pptx/SKILL.md":
				w.Write([]byte("---\nname: PPTX\n---\ncontent"))
			case "/acme/skills/main/skills/pptx/my dir/note.md":
				w.Write([]byte("note"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromSkillsSh(client, "https://skills.sh/acme/skills/pptx")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}

	gotPaths := importedFilePaths(result.files)
	wantPaths := []string{"my dir/note.md"}
	if !equalStrings(gotPaths, wantPaths) {
		t.Fatalf("files = %v, want %v", gotPaths, wantPaths)
	}
	if !containsString(*requests, "api.github.com /repos/acme/skills/contents/skills/pptx/my%20dir?ref=main") {
		t.Fatalf("expected fallback request with single escaping, got %v", *requests)
	}
	for _, request := range *requests {
		if strings.Contains(request, "%2520") {
			t.Fatalf("unexpected double-escaped request: %v", *requests)
		}
	}
}

func TestFetchFromSkillsSh_LogsSubdirectoryFailures(t *testing.T) {
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/contents/skills/pptx":
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name: "scripts",
						Path: "skills/pptx/scripts",
						Type: "dir",
						URL:  "https://api.github.com/repos/acme/skills/contents/skills/pptx/scripts?ref=main",
					},
				})
			case "/repos/acme/skills/contents/skills/pptx/scripts":
				http.Error(w, "missing", http.StatusNotFound)
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/pptx/SKILL.md":
				w.Write([]byte("---\nname: PPTX\n---\ncontent"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() {
		slog.SetDefault(prev)
	})

	result, err := fetchFromSkillsSh(client, "https://skills.sh/acme/skills/pptx")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}
	if len(result.files) != 0 {
		t.Fatalf("expected no files when subdirectory listing fails, got %v", importedFilePaths(result.files))
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "skills.sh import: failed to list subdirectory") {
		t.Fatalf("expected warning log, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "status=404") {
		t.Fatalf("expected status in warning log, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "skills/pptx/scripts?ref=main") {
		t.Fatalf("expected subdirectory URL in warning log, got %q", logOutput)
	}
}

func TestFetchFromSkillsSh_AnthropicPptxIntegration(t *testing.T) {
	if os.Getenv("MULTICA_RUN_SKILLS_SH_INTEGRATION") == "" {
		t.Skip("set MULTICA_RUN_SKILLS_SH_INTEGRATION=1 to run live GitHub integration test")
	}

	result, err := fetchFromSkillsSh(&http.Client{Timeout: 30 * time.Second}, "https://skills.sh/anthropics/skills/pptx")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}

	gotPaths := importedFilePaths(result.files)
	for _, want := range []string{
		"scripts/__init__.py",
		"scripts/add_slide.py",
		"scripts/clean.py",
		"scripts/thumbnail.py",
	} {
		if !containsString(gotPaths, want) {
			t.Fatalf("missing %q in %v", want, gotPaths)
		}
	}
}

type rewriteGitHubTransport struct {
	target *url.URL
	base   http.RoundTripper
	hosts  map[string]struct{}
}

func (t *rewriteGitHubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if _, ok := t.hosts[clone.URL.Host]; ok {
		headers := clone.Header.Clone()
		headers.Set("X-Test-Original-Host", req.URL.Host)
		clone.Header = headers
		clone.URL.Scheme = t.target.Scheme
		clone.URL.Host = t.target.Host
		clone.Host = t.target.Host
	}
	return t.base.RoundTrip(clone)
}

func newGitHubFixtureClient(t *testing.T, handler http.HandlerFunc) (*http.Client, *[]string) {
	t.Helper()

	var (
		mu       sync.Mutex
		requests []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Header.Get("X-Test-Original-Host")+" "+r.URL.RequestURI())
		mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	return &http.Client{
		Transport: &rewriteGitHubTransport{
			target: target,
			base:   http.DefaultTransport,
			hosts: map[string]struct{}{
				"api.github.com":            {},
				"raw.githubusercontent.com": {},
			},
		},
	}, &requests
}

func importedFilePaths(files []importedFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.path)
	}
	sort.Strings(paths)
	return paths
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
