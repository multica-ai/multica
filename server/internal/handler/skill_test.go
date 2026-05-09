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

func TestFetchFromSkillsSh_ResolvesAliasedSkillNamesViaFrontmatter(t *testing.T) {
	client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/vercel-labs/agent-skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/vercel-labs/agent-skills/git/trees/main":
				if got := r.URL.Query().Get("recursive"); got != "1" {
					t.Fatalf("tree recursive = %q, want 1", got)
				}
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree: []githubTreeEntry{
						{Path: "skills/composition-patterns/SKILL.md", Type: "blob"},
						{Path: "skills/react-best-practices/SKILL.md", Type: "blob"},
					},
				})
			case "/repos/vercel-labs/agent-skills/contents/skills/composition-patterns":
				if got := r.URL.Query().Get("ref"); got != "main" {
					t.Fatalf("resolved dir ref = %q, want main", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "rules.md",
						Path:        "skills/composition-patterns/rules.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/vercel-labs/agent-skills/main/skills/composition-patterns/rules.md",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/vercel-labs/agent-skills/main/skills/composition-patterns/SKILL.md":
				w.Write([]byte("---\nname: vercel-composition-patterns\ndescription: aliased skill\n---\ncontent"))
			case "/vercel-labs/agent-skills/main/skills/react-best-practices/SKILL.md":
				w.Write([]byte("---\nname: vercel-react-best-practices\n---\ncontent"))
			case "/vercel-labs/agent-skills/main/skills/composition-patterns/rules.md":
				w.Write([]byte("rules"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromSkillsSh(client, "https://skills.sh/vercel-labs/agent-skills/vercel-composition-patterns")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}

	if result.name != "vercel-composition-patterns" {
		t.Fatalf("name = %q, want vercel-composition-patterns", result.name)
	}
	gotPaths := importedFilePaths(result.files)
	wantPaths := []string{"rules.md"}
	if !equalStrings(gotPaths, wantPaths) {
		t.Fatalf("files = %v, want %v", gotPaths, wantPaths)
	}
	if !containsString(*requests, "api.github.com /repos/vercel-labs/agent-skills/git/trees/main?recursive=1") {
		t.Fatalf("expected fallback tree lookup, got requests %v", *requests)
	}
	for _, request := range *requests {
		if request == "raw.githubusercontent.com /vercel-labs/agent-skills/main/skills/react-best-practices/SKILL.md" {
			t.Fatalf("unexpected non-matching fallback fetch: %v", *requests)
		}
	}
}

func TestFetchFromSkillsSh_ResolvesRootLevelSkillMd(t *testing.T) {
	client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/alchaincyf/huashu-design":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "master"})
			case "/repos/alchaincyf/huashu-design/git/trees/master":
				if got := r.URL.Query().Get("recursive"); got != "1" {
					t.Fatalf("tree recursive = %q, want 1", got)
				}
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree: []githubTreeEntry{
						{Path: "README.md", Type: "blob"},
						{Path: "SKILL.md", Type: "blob"},
						{Path: "assets", Type: "tree"},
						{Path: "assets/logo.png", Type: "blob"},
					},
				})
			case "/repos/alchaincyf/huashu-design/contents":
				if got := r.URL.Query().Get("ref"); got != "master" {
					t.Fatalf("root contents ref = %q, want master", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "README.md",
						Path:        "README.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/alchaincyf/huashu-design/master/README.md",
					},
					{
						Name:        "SKILL.md",
						Path:        "SKILL.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/alchaincyf/huashu-design/master/SKILL.md",
					},
					{
						Name: "assets",
						Path: "assets",
						Type: "dir",
						URL:  "https://api.github.com/repos/alchaincyf/huashu-design/contents/assets?ref=master",
					},
				})
			case "/repos/alchaincyf/huashu-design/contents/assets":
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "logo.png",
						Path:        "assets/logo.png",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/alchaincyf/huashu-design/master/assets/logo.png",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/alchaincyf/huashu-design/master/SKILL.md":
				w.Write([]byte("---\nname: huashu-design\ndescription: hi-fi HTML prototypes\n---\nbody"))
			case "/alchaincyf/huashu-design/master/README.md":
				w.Write([]byte("# Readme"))
			case "/alchaincyf/huashu-design/master/assets/logo.png":
				w.Write([]byte("PNGBYTES"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromSkillsSh(client, "https://skills.sh/alchaincyf/huashu-design/huashu-design")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}
	if result.name != "huashu-design" {
		t.Fatalf("name = %q, want huashu-design", result.name)
	}
	if !strings.HasPrefix(result.content, "---\nname: huashu-design") {
		t.Fatalf("SKILL.md content not populated, got %q", result.content)
	}
	gotPaths := importedFilePaths(result.files)
	wantPaths := []string{"README.md", "assets/logo.png"}
	if !equalStrings(gotPaths, wantPaths) {
		t.Fatalf("files = %v, want %v", gotPaths, wantPaths)
	}
	if !containsString(*requests, "api.github.com /repos/alchaincyf/huashu-design/contents?ref=master") {
		t.Fatalf("expected root contents listing, got %v", *requests)
	}
}

func TestFetchFromSkillsSh_RootSkillMdFastPathSkipsFrontmatterMismatch(t *testing.T) {
	// Multi-skill repo with an unrelated root SKILL.md (skill "other") plus a
	// subdir skill "wanted". URL requests "wanted". The fast-path must reject
	// the root SKILL.md on frontmatter mismatch and fall through to the tree
	// fallback, which then resolves "wanted" correctly.
	client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/multi":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/multi/git/trees/main":
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree: []githubTreeEntry{
						{Path: "SKILL.md", Type: "blob"},
						{Path: "extras/wanted/SKILL.md", Type: "blob"},
					},
				})
			case "/repos/acme/multi/contents/extras/wanted":
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "ref.md",
						Path:        "extras/wanted/ref.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/multi/main/extras/wanted/ref.md",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/multi/main/SKILL.md":
				w.Write([]byte("---\nname: other\n---\ncontent"))
			case "/acme/multi/main/extras/wanted/SKILL.md":
				w.Write([]byte("---\nname: wanted\ndescription: the right one\n---\ncontent"))
			case "/acme/multi/main/extras/wanted/ref.md":
				w.Write([]byte("ref"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromSkillsSh(client, "https://skills.sh/acme/multi/wanted")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}
	if result.name != "wanted" {
		t.Fatalf("name = %q, want wanted (root SKILL.md must not hijack the mismatched request)", result.name)
	}
	gotPaths := importedFilePaths(result.files)
	wantPaths := []string{"ref.md"}
	if !equalStrings(gotPaths, wantPaths) {
		t.Fatalf("files = %v, want %v", gotPaths, wantPaths)
	}
	if !containsString(*requests, "api.github.com /repos/acme/multi/git/trees/main?recursive=1") {
		t.Fatalf("expected tree fallback to run after fast-path frontmatter miss, got %v", *requests)
	}
}

func TestFetchFromSkillsSh_ReturnsActionableErrorForTruncatedTrees(t *testing.T) {
	client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/git/trees/main":
				if got := r.URL.Query().Get("recursive"); got != "1" {
					t.Fatalf("tree recursive = %q, want 1", got)
				}
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree: []githubTreeEntry{
						{Path: "skills/deploy-to-vercel/SKILL.md", Type: "blob"},
					},
					Truncated: true,
				})
			case "/repos/acme/skills/contents/skills":
				if got := r.URL.Query().Get("ref"); got != "main" {
					t.Fatalf("skills ref = %q, want main", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "SKILL.md",
						Path:        "skills/deploy-to-vercel/SKILL.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/skills/main/skills/deploy-to-vercel/SKILL.md",
					},
				})
			case "/repos/acme/skills/contents/.claude/skills":
				http.NotFound(w, r)
			case "/repos/acme/skills/contents/plugin/skills":
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/deploy-to-vercel/SKILL.md":
				w.Write([]byte("---\nname: deploy-to-vercel\n---\ncontent"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	_, err := fetchFromSkillsSh(client, "https://skills.sh/acme/skills/vercel-composition-patterns")
	if err == nil {
		t.Fatal("expected error for truncated tree fallback miss")
	}
	if !strings.Contains(err.Error(), "tree is too large to scan exhaustively") {
		t.Fatalf("error = %q, want actionable truncated-tree message", err.Error())
	}
	if !containsString(*requests, "api.github.com /repos/acme/skills/contents/skills?ref=main") {
		t.Fatalf("expected conventional prefix listing, got %v", *requests)
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
