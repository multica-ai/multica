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
	if !strings.Contains(logOutput, "github import: failed to list subdirectory") {
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
	// assets/logo.png is intentionally dropped by addFile's binary-extension
	// guard — PG TEXT columns can't store image bytes, and agents never read
	// them as text. The directory is still walked (the listing request below
	// confirms it), but the .png never reaches result.files.
	gotPaths := importedFilePaths(result.files)
	wantPaths := []string{"README.md"}
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

// --- GitHub source tests ---

func TestParseGitHubURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		want    githubSpec
		wantErr bool
	}{
		{
			name: "repo root",
			url:  "https://github.com/acme/skill",
			want: githubSpec{owner: "acme", repo: "skill"},
		},
		{
			name: "repo root with .git suffix",
			url:  "https://github.com/acme/skill.git",
			want: githubSpec{owner: "acme", repo: "skill"},
		},
		{
			name: "tree URL with directory",
			url:  "https://github.com/anthropics/skills/tree/main/document-skills/pptx",
			want: githubSpec{owner: "anthropics", repo: "skills", ref: "main", skillDir: "document-skills/pptx"},
		},
		{
			name: "tree URL ref only",
			url:  "https://github.com/anthropics/skills/tree/main",
			want: githubSpec{owner: "anthropics", repo: "skills", ref: "main"},
		},
		{
			name: "blob URL pointing at SKILL.md",
			url:  "https://github.com/acme/skills/blob/main/skills/foo/SKILL.md",
			want: githubSpec{owner: "acme", repo: "skills", ref: "main", skillDir: "skills/foo"},
		},
		{
			name: "blob URL with URL-escaped path segment",
			url:  "https://github.com/acme/skills/blob/main/my%20dir/SKILL.md",
			want: githubSpec{owner: "acme", repo: "skills", ref: "main", skillDir: "my dir"},
		},
		{
			name:    "blob URL not pointing at SKILL.md",
			url:     "https://github.com/acme/skills/blob/main/skills/foo/README.md",
			wantErr: true,
		},
		{
			name:    "missing repo",
			url:     "https://github.com/acme",
			wantErr: true,
		},
		{
			name:    "unsupported segment",
			url:     "https://github.com/acme/skills/issues/1",
			wantErr: true,
		},
		{
			name:    "tree URL missing ref",
			url:     "https://github.com/acme/skills/tree/",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGitHubURL(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGitHubURL: %v", err)
			}
			if got.owner != tc.want.owner || got.repo != tc.want.repo ||
				got.ref != tc.want.ref || got.skillDir != tc.want.skillDir {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDetectImportSource_RecognizesGitHub(t *testing.T) {
	src, _, err := detectImportSource("https://github.com/acme/skill")
	if err != nil {
		t.Fatalf("detectImportSource: %v", err)
	}
	if src != sourceGitHub {
		t.Fatalf("source = %v, want sourceGitHub", src)
	}
}

func TestFetchFromGitHub_TreeURLImportsSkillDirectory(t *testing.T) {
	client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/anthropics/skills/commits/main":
				w.Write([]byte("deadbeef"))
			case "/repos/anthropics/skills/contents/document-skills/pptx":
				if got := r.URL.Query().Get("ref"); got != "main" {
					t.Fatalf("contents ref = %q, want main", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "editing.md",
						Path:        "document-skills/pptx/editing.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/anthropics/skills/main/document-skills/pptx/editing.md",
					},
					{
						Name: "scripts",
						Path: "document-skills/pptx/scripts",
						Type: "dir",
						URL:  "https://api.github.com/repos/anthropics/skills/contents/document-skills/pptx/scripts?ref=main",
					},
				})
			case "/repos/anthropics/skills/contents/document-skills/pptx/scripts":
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "add_slide.py",
						Path:        "document-skills/pptx/scripts/add_slide.py",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/anthropics/skills/main/document-skills/pptx/scripts/add_slide.py",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/anthropics/skills/main/document-skills/pptx/SKILL.md":
				w.Write([]byte("---\nname: pptx\ndescription: presentation tools\n---\nbody"))
			case "/anthropics/skills/main/document-skills/pptx/editing.md":
				w.Write([]byte("editing"))
			case "/anthropics/skills/main/document-skills/pptx/scripts/add_slide.py":
				w.Write([]byte("print('slide')"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitHub(client, "https://github.com/anthropics/skills/tree/main/document-skills/pptx")
	if err != nil {
		t.Fatalf("fetchFromGitHub: %v", err)
	}
	if result.name != "pptx" {
		t.Fatalf("name = %q, want pptx", result.name)
	}
	if result.description != "presentation tools" {
		t.Fatalf("description = %q, want presentation tools", result.description)
	}
	gotPaths := importedFilePaths(result.files)
	wantPaths := []string{"editing.md", "scripts/add_slide.py"}
	if !equalStrings(gotPaths, wantPaths) {
		t.Fatalf("files = %v (must be relative to skill dir), want %v", gotPaths, wantPaths)
	}
	// Verify the skill-relative path scheme: we never want supporting files
	// to keep the in-repo prefix (document-skills/pptx/...).
	for _, f := range result.files {
		if strings.HasPrefix(f.path, "document-skills/") {
			t.Fatalf("supporting file %q still carries skillDir prefix", f.path)
		}
	}
	origin := result.origin
	if origin == nil || origin["type"] != "github" {
		t.Fatalf("origin = %v, want type=github", origin)
	}
	if origin["ref"] != "main" || origin["path"] != "document-skills/pptx" {
		t.Fatalf("origin ref/path mismatch: %v", origin)
	}
	if !containsString(*requests, "api.github.com /repos/anthropics/skills/contents/document-skills/pptx?ref=main") {
		t.Fatalf("expected contents listing, got %v", *requests)
	}
}

func TestFetchFromGitHub_RepoRootResolvesDefaultBranch(t *testing.T) {
	client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/alice/single-skill":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "master"})
			case "/repos/alice/single-skill/contents":
				if got := r.URL.Query().Get("ref"); got != "master" {
					t.Fatalf("contents ref = %q, want master", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "README.md",
						Path:        "README.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/alice/single-skill/master/README.md",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/alice/single-skill/master/SKILL.md":
				w.Write([]byte("---\nname: single-skill\n---\nbody"))
			case "/alice/single-skill/master/README.md":
				w.Write([]byte("readme"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitHub(client, "https://github.com/alice/single-skill")
	if err != nil {
		t.Fatalf("fetchFromGitHub: %v", err)
	}
	if result.name != "single-skill" {
		t.Fatalf("name = %q, want single-skill", result.name)
	}
	gotPaths := importedFilePaths(result.files)
	if !equalStrings(gotPaths, []string{"README.md"}) {
		t.Fatalf("files = %v", gotPaths)
	}
	if !containsString(*requests, "api.github.com /repos/alice/single-skill") {
		t.Fatalf("expected default-branch lookup, got %v", *requests)
	}
}

func TestFetchFromGitHub_RepoRootMissingSKILLmdReturnsActionableError(t *testing.T) {
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			if r.URL.Path == "/repos/alice/multi" {
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
				return
			}
			http.NotFound(w, r)
		case "raw.githubusercontent.com":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	_, err := fetchFromGitHub(client, "https://github.com/alice/multi")
	if err == nil {
		t.Fatal("expected error for missing root SKILL.md")
	}
	if !strings.Contains(err.Error(), "tree/main/<skill-dir>") && !strings.Contains(err.Error(), "tree/main") {
		t.Fatalf("error should hint at /tree/{ref}/<skill-dir>, got %q", err.Error())
	}
}

func TestFetchFromGitHub_BlobURLImportsSpecificSkill(t *testing.T) {
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills/commits/main":
				w.Write([]byte("deadbeef"))
			case "/repos/acme/skills/contents/skills/foo":
				writeJSON(w, http.StatusOK, []githubContentEntry{})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			if r.URL.Path == "/acme/skills/main/skills/foo/SKILL.md" {
				w.Write([]byte("---\nname: foo\n---\nbody"))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitHub(client, "https://github.com/acme/skills/blob/main/skills/foo/SKILL.md")
	if err != nil {
		t.Fatalf("fetchFromGitHub: %v", err)
	}
	if result.name != "foo" {
		t.Fatalf("name = %q, want foo", result.name)
	}
	if result.origin["path"] != "skills/foo" {
		t.Fatalf("origin path = %v, want skills/foo", result.origin["path"])
	}
}

// --- Bundle / file size cap tests ---

func TestFetchRawFile_ReturnsErrorOnOversizedFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("a"), maxImportFileSize+1024))
	}))
	t.Cleanup(server.Close)

	_, err := fetchRawFile(&http.Client{}, server.URL+"/big.bin")
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("error = %q, want byte limit message", err.Error())
	}
	if !isCapError(err) {
		t.Fatalf("error %q must be classified as a cap error so callers fail-fast", err.Error())
	}
}

func TestImportedSkill_AddFileEnforcesBundleLimits(t *testing.T) {
	t.Run("file count", func(t *testing.T) {
		s := &importedSkill{}
		for i := 0; i < maxImportFileCount; i++ {
			if err := s.addFile("f", "x"); err != nil {
				t.Fatalf("addFile %d: %v", i, err)
			}
		}
		err := s.addFile("overflow", "x")
		if err == nil {
			t.Fatal("expected file count cap error")
		}
		if !isCapError(err) {
			t.Fatalf("error %q must be a cap error", err.Error())
		}
	})
	t.Run("total bytes", func(t *testing.T) {
		s := &importedSkill{}
		big := strings.Repeat("y", maxImportTotalSize)
		if err := s.addFile("a", big); err != nil {
			t.Fatalf("addFile at cap: %v", err)
		}
		err := s.addFile("b", "x")
		if err == nil {
			t.Fatal("expected total bytes cap error")
		}
		if !isCapError(err) {
			t.Fatalf("error %q must be a cap error", err.Error())
		}
	})
}

// fetchFromGitHub must FAIL the import (not just log+continue) when a
// supporting file exceeds the per-file cap — silently dropping the file
// would leave a skill bundle that looks valid to the user but is missing
// content.
func TestFetchFromGitHub_OversizedSupportingFileFailsImport(t *testing.T) {
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills/commits/main":
				w.Write([]byte("deadbeef"))
			case "/repos/acme/skills/contents/foo":
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "huge.bin",
						Path:        "foo/huge.bin",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/skills/main/foo/huge.bin",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/foo/SKILL.md":
				w.Write([]byte("---\nname: foo\n---\nbody"))
			case "/acme/skills/main/foo/huge.bin":
				w.Write(bytes.Repeat([]byte("z"), maxImportFileSize+512))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})
	_, err := fetchFromGitHub(client, "https://github.com/acme/skills/tree/main/foo")
	if err == nil {
		t.Fatal("expected oversized supporting file to fail the whole import")
	}
	if !strings.Contains(err.Error(), "huge.bin") || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("error %q should name the file and the cap", err.Error())
	}
}

// fetchFromSkillsSh has the same supporting-file loop and must also fail
// (not just warn) when one of those files exceeds the cap.
func TestFetchFromSkillsSh_OversizedSupportingFileFailsImport(t *testing.T) {
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/contents/skills/foo":
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "huge.bin",
						Path:        "skills/foo/huge.bin",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/skills/main/skills/foo/huge.bin",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/foo/SKILL.md":
				w.Write([]byte("---\nname: foo\n---\nbody"))
			case "/acme/skills/main/skills/foo/huge.bin":
				w.Write(bytes.Repeat([]byte("z"), maxImportFileSize+512))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})
	_, err := fetchFromSkillsSh(client, "https://skills.sh/acme/skills/foo")
	if err == nil {
		t.Fatal("expected oversized supporting file to fail the whole import")
	}
	if !strings.Contains(err.Error(), "huge.bin") {
		t.Fatalf("error %q should name the offending file", err.Error())
	}
}

// Slash-bearing refs (e.g. release/v2) are now resolved against the API
// instead of being silently parsed as ref="release", path="v2/...". The
// resolver must walk longest→shortest and pick the prefix the API
// confirms exists.
func TestFetchFromGitHub_ResolvesSlashRefAgainstAPI(t *testing.T) {
	client, requests := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills/commits/release/v2/skills/foo",
				"/repos/acme/skills/commits/release/v2/skills":
				http.NotFound(w, r)
			case "/repos/acme/skills/commits/release/v2":
				w.Write([]byte("deadbeef"))
			case "/repos/acme/skills/contents/skills/foo":
				if got := r.URL.Query().Get("ref"); got != "release/v2" {
					t.Fatalf("contents called with ref=%q, want release/v2", got)
				}
				writeJSON(w, http.StatusOK, []githubContentEntry{})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/release/v2/skills/foo/SKILL.md":
				w.Write([]byte("---\nname: foo\n---\nbody"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})
	result, err := fetchFromGitHub(client, "https://github.com/acme/skills/tree/release/v2/skills/foo")
	if err != nil {
		t.Fatalf("fetchFromGitHub: %v", err)
	}
	if result.origin["ref"] != "release/v2" {
		t.Fatalf("origin ref = %v, want release/v2", result.origin["ref"])
	}
	if result.origin["path"] != "skills/foo" {
		t.Fatalf("origin path = %v, want skills/foo", result.origin["path"])
	}
	// Sanity-check that the resolver actually probed in the expected order.
	if !containsString(*requests, "api.github.com /repos/acme/skills/commits/release/v2/skills/foo") {
		t.Fatalf("resolver should probe longest prefix first, requests=%v", *requests)
	}
}

// When none of the candidate refs resolve, fail with a clear error that
// names what was tried — do not silently fall back to using the first
// segment as the ref (the previous behavior, which would import the wrong
// branch / wrong path).
func TestFetchFromGitHub_UnresolvableRefFailsLoudly(t *testing.T) {
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			http.NotFound(w, r)
		case "raw.githubusercontent.com":
			t.Fatalf("must not hit raw.githubusercontent.com when ref unresolved: %s", r.URL.Path)
		default:
			http.NotFound(w, r)
		}
	})
	_, err := fetchFromGitHub(client, "https://github.com/acme/skills/tree/nope/skills/foo")
	if err == nil {
		t.Fatal("expected error when no candidate ref resolves")
	}
	if !strings.Contains(err.Error(), "could not resolve ref") {
		t.Fatalf("error %q should mention ref resolution failure", err.Error())
	}
}

// When the GitHub API responds 403 (rate-limited or auth-blocked) on the
// ref-resolution probe, the import should NOT fail outright. The optimistic
// single-segment split (ref = first segment, rest = path) is correct for
// the overwhelming majority of URLs, so we fall back to it and let the raw
// SKILL.md fetch be the source of truth. This covers the common case of
// self-hosted servers hitting GitHub's 60-req/hour unauthenticated limit.
func TestFetchFromGitHub_FallsBackOnAPIBlocked(t *testing.T) {
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			// Simulate rate-limit on every commits probe and on contents.
			if strings.HasPrefix(r.URL.Path, "/repos/anthropics/skills/commits/") {
				http.Error(w, "rate limit", http.StatusForbidden)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/repos/anthropics/skills/contents/") {
				http.Error(w, "rate limit", http.StatusForbidden)
				return
			}
			http.NotFound(w, r)
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/anthropics/skills/main/skills/pptx/SKILL.md":
				w.Write([]byte("---\nname: pptx\ndescription: PowerPoint skill\n---\nbody"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})
	result, err := fetchFromGitHub(client, "https://github.com/anthropics/skills/tree/main/skills/pptx")
	if err != nil {
		t.Fatalf("fetchFromGitHub: %v", err)
	}
	if result.origin["ref"] != "main" {
		t.Fatalf("origin ref = %v, want main (optimistic fallback)", result.origin["ref"])
	}
	if result.origin["path"] != "skills/pptx" {
		t.Fatalf("origin path = %v, want skills/pptx (optimistic fallback)", result.origin["path"])
	}
	if result.name != "pptx" {
		t.Fatalf("name = %q, want pptx", result.name)
	}
}

// GITHUB_TOKEN, when set, must be forwarded as a bearer token on every
// api.github.com request so self-hosted servers can avoid the 60-req/hour
// unauthenticated rate limit.
func TestFetchFromGitHub_SendsAuthHeaderWhenTokenSet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test_token_123")
	var (
		mu      sync.Mutex
		authHdr []string
	)
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Original-Host") == "api.github.com" {
			mu.Lock()
			authHdr = append(authHdr, r.Header.Get("Authorization"))
			mu.Unlock()
		}
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills/commits/main/skills/foo",
				"/repos/acme/skills/commits/main/skills":
				http.NotFound(w, r)
			case "/repos/acme/skills/commits/main":
				w.Write([]byte("deadbeef"))
			case "/repos/acme/skills/contents/skills/foo":
				writeJSON(w, http.StatusOK, []githubContentEntry{})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/foo/SKILL.md":
				w.Write([]byte("---\nname: foo\n---\nbody"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})
	if _, err := fetchFromGitHub(client, "https://github.com/acme/skills/tree/main/skills/foo"); err != nil {
		t.Fatalf("fetchFromGitHub: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(authHdr) == 0 {
		t.Fatal("expected at least one api.github.com request")
	}
	for i, h := range authHdr {
		if h != "Bearer ghp_test_token_123" {
			t.Fatalf("request %d Authorization = %q, want Bearer ghp_test_token_123", i, h)
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

// ---------------------------------------------------------------------------
// GitLab test helpers
// ---------------------------------------------------------------------------

// rewriteGitLabTransport rewrites requests targeting any of the listed hosts
// to a local test server, preserving the original host in X-Test-Original-Host.
type rewriteGitLabTransport struct {
	target *url.URL
	base   http.RoundTripper
	hosts  map[string]struct{}
}

func (t *rewriteGitLabTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

// newGitLabFixtureClient builds an *http.Client that routes requests for the
// given hosts to a local httptest.Server running handler.
//
// Because Go's net/http server decodes %2F in r.URL.Path (turning
// /api/v4/projects/acme%2Fskill into /api/v4/projects/acme/skill), the
// wrapper injects the raw, still-encoded path into the X-Test-Raw-Path header
// so that handlers can switch on the encoded form that the production code
// actually sends.
func newGitLabFixtureClient(t *testing.T, hosts []string, handler http.HandlerFunc) (*http.Client, *[]string) {
	t.Helper()
	var (
		mu       sync.Mutex
		requests []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawPath := r.URL.RawPath
		if rawPath == "" {
			rawPath = r.URL.Path
		}
		mu.Lock()
		requests = append(requests, r.Header.Get("X-Test-Original-Host")+" "+r.URL.RequestURI())
		mu.Unlock()
		r.Header.Set("X-Test-Raw-Path", rawPath)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	hostSet := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		hostSet[h] = struct{}{}
	}
	return &http.Client{
		Transport: &rewriteGitLabTransport{
			target: target,
			base:   http.DefaultTransport,
			hosts:  hostSet,
		},
	}, &requests
}

// ---------------------------------------------------------------------------
// parseGitLabURL
// ---------------------------------------------------------------------------

func TestParseGitLabURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		want    gitlabSpec
		wantErr bool
	}{
		{
			name: "raw URL pointing at SKILL.md",
			url:  "https://git.example.com/myorg/myrepo/-/raw/main/skills/myskill/SKILL.md",
			want: gitlabSpec{
				baseURL:   "https://git.example.com",
				owner:     "myorg",
				repo:      "myrepo",
				ref:       "main",
				skillDir:  "skills/myskill",
				skillFile: "skills/myskill/SKILL.md",
			},
		},
		{
			name: "root URL",
			url:  "https://gitlab.com/acme/myskill",
			want: gitlabSpec{baseURL: "https://gitlab.com", owner: "acme", repo: "myskill"},
		},
		{
			name: "root URL with .git suffix",
			url:  "https://gitlab.com/acme/myskill.git",
			want: gitlabSpec{baseURL: "https://gitlab.com", owner: "acme", repo: "myskill"},
		},
		{
			name: "self-hosted root URL",
			url:  "https://git.example.com/myorg/myrepo",
			want: gitlabSpec{baseURL: "https://git.example.com", owner: "myorg", repo: "myrepo"},
		},
		{
			name: "tree URL with ref and path",
			url:  "https://gitlab.com/acme/skills/-/tree/main/skills/pptx",
			want: gitlabSpec{baseURL: "https://gitlab.com", owner: "acme", repo: "skills", ref: "main", skillDir: "skills/pptx"},
		},
		{
			name: "tree URL with ref only (root)",
			url:  "https://gitlab.com/acme/skills/-/tree/main",
			want: gitlabSpec{baseURL: "https://gitlab.com", owner: "acme", repo: "skills", ref: "main"},
		},
		{
			name: "blob URL pointing at SKILL.md",
			url:  "https://gitlab.com/acme/skills/-/blob/main/skills/pptx/SKILL.md",
			want: gitlabSpec{baseURL: "https://gitlab.com", owner: "acme", repo: "skills", ref: "main", skillDir: "skills/pptx"},
		},
		{
			name:    "missing owner/repo",
			url:     "https://gitlab.com/acme",
			wantErr: true,
		},
		{
			name:    "blob URL pointing at non-SKILL.md file",
			url:     "https://gitlab.com/acme/skills/-/blob/main/README.md",
			wantErr: true,
		},
		{
			name:    "tree URL missing ref",
			url:     "https://gitlab.com/acme/skills/-/tree/",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGitLabURL(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGitLabURL: %v", err)
			}
			if got.baseURL != tc.want.baseURL || got.owner != tc.want.owner ||
				got.repo != tc.want.repo || got.ref != tc.want.ref || got.skillDir != tc.want.skillDir {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// detectImportSource — GitLab
// ---------------------------------------------------------------------------

func TestDetectImportSource_RecognizesGitLabCom(t *testing.T) {
	src, normalized, err := detectImportSource("https://gitlab.com/acme/skill")
	if err != nil {
		t.Fatalf("detectImportSource: %v", err)
	}
	if src != sourceGitLab {
		t.Fatalf("source = %v, want sourceGitLab", src)
	}
	if normalized != "https://gitlab.com/acme/skill" {
		t.Fatalf("normalized = %q, unexpected", normalized)
	}
}

func TestDetectImportSource_RecognizesGitLabComWithoutScheme(t *testing.T) {
	src, _, err := detectImportSource("gitlab.com/acme/skill")
	if err != nil {
		t.Fatalf("detectImportSource: %v", err)
	}
	if src != sourceGitLab {
		t.Fatalf("source = %v, want sourceGitLab", src)
	}
}

func TestDetectImportSource_RecognizesSelfHostedGitLab(t *testing.T) {
	src, normalized, err := detectImportSource("https://git.example.com/myorg/myrepo")
	if err != nil {
		t.Fatalf("detectImportSource: %v", err)
	}
	if src != sourceGitLab {
		t.Fatalf("source = %v, want sourceGitLab", src)
	}
	if normalized != "https://git.example.com/myorg/myrepo" {
		t.Fatalf("normalized = %q, unexpected", normalized)
	}
}

func TestDetectImportSource_SelfHostedGitLabWithoutScheme(t *testing.T) {
	src, _, err := detectImportSource("git.example.com/myorg/myrepo")
	if err != nil {
		t.Fatalf("detectImportSource: %v", err)
	}
	if src != sourceGitLab {
		t.Fatalf("source = %v, want sourceGitLab", src)
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — root repo (SKILL.md at repository root)
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_RootRepoImport(t *testing.T) {
	const (
		host  = "git.example.com"
		owner = "myorg"
		repo  = "myskill"
	)
	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Raw-Path") {
		case "/api/v4/projects/myorg%2Fmyskill":
			writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
		case "/api/v4/projects/myorg%2Fmyskill/repository/files/SKILL.md/raw":
			w.Write([]byte("---\nname: myskill\ndescription: my skill description\n---\ncontent"))
		case "/api/v4/projects/myorg%2Fmyskill/repository/tree":
			writeJSON(w, http.StatusOK, []gitlabTreeEntry{
				{Type: "blob", Name: "README.md", Path: "README.md"},
				{Type: "blob", Name: "helper.py", Path: "helper.py"},
			})
		case "/api/v4/projects/myorg%2Fmyskill/repository/files/README.md/raw":
			w.Write([]byte("readme"))
		case "/api/v4/projects/myorg%2Fmyskill/repository/files/helper.py/raw":
			w.Write([]byte("print('hi')"))
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitLab(client, "https://"+host+"/"+owner+"/"+repo, "")
	if err != nil {
		t.Fatalf("fetchFromGitLab: %v", err)
	}
	if result.name != "myskill" {
		t.Fatalf("name = %q, want myskill", result.name)
	}
	if result.description != "my skill description" {
		t.Fatalf("description = %q, unexpected", result.description)
	}
	wantFiles := []string{"README.md", "helper.py"}
	gotFiles := importedFilePaths(result.files)
	if !equalStrings(gotFiles, wantFiles) {
		t.Fatalf("files = %v, want %v", gotFiles, wantFiles)
	}
	if result.origin["type"] != "gitlab" {
		t.Fatalf("origin.type = %v, want gitlab", result.origin["type"])
	}
	if result.origin["base_url"] != "https://"+host {
		t.Fatalf("origin.base_url = %v, unexpected", result.origin["base_url"])
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — tree URL (skill in subdirectory)
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_TreeURLImportsSkillDirectory(t *testing.T) {
	const host = "gitlab.com"
	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Raw-Path") {
		case "/api/v4/projects/acme%2Fskills/repository/files/skills%2Fpptx%2FSKILL.md/raw":
			if r.URL.Query().Get("ref") != "main" {
				t.Fatalf("ref = %q, want main", r.URL.Query().Get("ref"))
			}
			w.Write([]byte("---\nname: pptx\ndescription: slides\n---\nbody"))
		case "/api/v4/projects/acme%2Fskills/repository/tree":
			if r.URL.Query().Get("path") != "skills/pptx" {
				writeJSON(w, http.StatusOK, []gitlabTreeEntry{})
				return
			}
			writeJSON(w, http.StatusOK, []gitlabTreeEntry{
				{Type: "blob", Name: "editing.md", Path: "skills/pptx/editing.md"},
				{Type: "blob", Name: "add_slide.py", Path: "skills/pptx/scripts/add_slide.py"},
			})
		case "/api/v4/projects/acme%2Fskills/repository/files/skills%2Fpptx%2Fediting.md/raw":
			w.Write([]byte("editing"))
		case "/api/v4/projects/acme%2Fskills/repository/files/skills%2Fpptx%2Fscripts%2Fadd_slide.py/raw":
			w.Write([]byte("print('slide')"))
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitLab(client, "https://"+host+"/acme/skills/-/tree/main/skills/pptx", "")
	if err != nil {
		t.Fatalf("fetchFromGitLab: %v", err)
	}
	if result.name != "pptx" {
		t.Fatalf("name = %q, want pptx", result.name)
	}
	wantFiles := []string{"editing.md", "scripts/add_slide.py"}
	gotFiles := importedFilePaths(result.files)
	if !equalStrings(gotFiles, wantFiles) {
		t.Fatalf("files = %v, want %v", gotFiles, wantFiles)
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — blob URL
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_BlobURLImportsSkillDirectory(t *testing.T) {
	const host = "gitlab.com"
	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Raw-Path") {
		case "/api/v4/projects/acme%2Fskills/repository/files/skills%2Ffoo%2FSKILL.md/raw":
			w.Write([]byte("---\nname: foo\n---\nbody"))
		case "/api/v4/projects/acme%2Fskills/repository/tree":
			writeJSON(w, http.StatusOK, []gitlabTreeEntry{})
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitLab(client, "https://"+host+"/acme/skills/-/blob/main/skills/foo/SKILL.md", "")
	if err != nil {
		t.Fatalf("fetchFromGitLab: %v", err)
	}
	if result.name != "foo" {
		t.Fatalf("name = %q, want foo", result.name)
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — default branch fallback to "main"
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_FallsBackToMainWhenAPIFails(t *testing.T) {
	const host = "git.example.com"
	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Raw-Path") {
		case "/api/v4/projects/myorg%2Fmyrepo":
			// Simulate API unavailable (private instance, no token)
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/api/v4/projects/myorg%2Fmyrepo/repository/files/SKILL.md/raw":
			if r.URL.Query().Get("ref") != "main" {
				t.Fatalf("expected fallback ref=main, got %q", r.URL.Query().Get("ref"))
			}
			w.Write([]byte("---\nname: fallback-skill\n---\ncontent"))
		case "/api/v4/projects/myorg%2Fmyrepo/repository/tree":
			writeJSON(w, http.StatusOK, []gitlabTreeEntry{})
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitLab(client, "https://"+host+"/myorg/myrepo", "")
	if err != nil {
		t.Fatalf("fetchFromGitLab: %v", err)
	}
	if result.name != "fallback-skill" {
		t.Fatalf("name = %q, want fallback-skill", result.name)
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — SKILL.md missing returns error
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_MissingSkillMdReturnsError(t *testing.T) {
	const host = "gitlab.com"
	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Raw-Path") {
		case "/api/v4/projects/acme%2Fempty/repository/files/SKILL.md/raw",
			"/api/v4/projects/acme%2Fempty/repository/files/skills%2FSKILL.md/raw",
			"/api/v4/projects/acme%2Fempty/repository/files/.claude%2Fskills%2FSKILL.md/raw",
			"/api/v4/projects/acme%2Fempty/repository/files/plugin%2Fskills%2FSKILL.md/raw":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	_, err := fetchFromGitLab(client, "https://"+host+"/acme/empty", "")
	if err == nil {
		t.Fatal("expected error for missing SKILL.md, got nil")
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — PRIVATE-TOKEN is forwarded from request parameter
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_ForwardsGitLabToken(t *testing.T) {
	const (
		host  = "gitlab.com"
		token = "glpat-test-token-456"
	)
	var (
		mu      sync.Mutex
		authHdr []string
	)
	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHdr = append(authHdr, r.Header.Get("PRIVATE-TOKEN"))
		mu.Unlock()
		switch r.Header.Get("X-Test-Raw-Path") {
		case "/api/v4/projects/acme%2Fskill/repository/files/SKILL.md/raw":
			w.Write([]byte("---\nname: token-skill\n---\nbody"))
		case "/api/v4/projects/acme%2Fskill/repository/tree":
			writeJSON(w, http.StatusOK, []gitlabTreeEntry{})
		default:
			http.NotFound(w, r)
		}
	})

	if _, err := fetchFromGitLab(client, "https://"+host+"/acme/skill", token); err != nil {
		t.Fatalf("fetchFromGitLab: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(authHdr) == 0 {
		t.Fatal("expected at least one request with PRIVATE-TOKEN header")
	}
	for i, h := range authHdr {
		if h != token {
			t.Fatalf("request %d PRIVATE-TOKEN = %q, want %q", i, h, token)
		}
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — SKILL.md skipped in file listing, LICENSE skipped
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_SkipsSkillMdAndLicenseInFileListing(t *testing.T) {
	const host = "gitlab.com"
	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		rawPath := r.Header.Get("X-Test-Raw-Path")
		switch rawPath {
		case "/api/v4/projects/acme%2Fskill":
			writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
		case "/api/v4/projects/acme%2Fskill/repository/files/SKILL.md/raw":
			w.Write([]byte("---\nname: myskill\n---\nbody"))
		case "/api/v4/projects/acme%2Fskill/repository/tree":
			writeJSON(w, http.StatusOK, []gitlabTreeEntry{
				{Type: "blob", Name: "SKILL.md", Path: "SKILL.md"},
				{Type: "blob", Name: "LICENSE", Path: "LICENSE"},
				{Type: "blob", Name: "LICENSE.md", Path: "LICENSE.md"},
				{Type: "blob", Name: "helper.py", Path: "helper.py"},
			})
		case "/api/v4/projects/acme%2Fskill/repository/files/helper.py/raw":
			w.Write([]byte("print('hi')"))
		case "/api/v4/projects/acme%2Fskill/repository/files/LICENSE/raw",
			"/api/v4/projects/acme%2Fskill/repository/files/LICENSE.md/raw":
			// These must never be fetched as supporting files.
			t.Errorf("supporting-file fetch must not happen for %s", rawPath)
			http.NotFound(w, r)
		default:
			// Candidate SKILL.md search probes (skills/, .claude/skills/, plugin/skills/)
			// are expected 404s — just return not found.
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitLab(client, "https://"+host+"/acme/skill", "")
	if err != nil {
		t.Fatalf("fetchFromGitLab: %v", err)
	}
	wantFiles := []string{"helper.py"}
	gotFiles := importedFilePaths(result.files)
	if !equalStrings(gotFiles, wantFiles) {
		t.Fatalf("files = %v, want %v", gotFiles, wantFiles)
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — file-size cap is enforced
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_FileCapAbortImport(t *testing.T) {
	const host = "gitlab.com"
	bigContent := strings.Repeat("x", maxImportFileSize+1)
	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Raw-Path") {
		case "/api/v4/projects/acme%2Fskill/repository/files/SKILL.md/raw":
			w.Write([]byte("---\nname: capskill\n---\nbody"))
		case "/api/v4/projects/acme%2Fskill/repository/tree":
			writeJSON(w, http.StatusOK, []gitlabTreeEntry{
				{Type: "blob", Name: "big.bin", Path: "big.bin"},
			})
		case "/api/v4/projects/acme%2Fskill/repository/files/big.bin/raw":
			w.Write([]byte(bigContent))
		default:
			http.NotFound(w, r)
		}
	})

	_, err := fetchFromGitLab(client, "https://"+host+"/acme/skill", "")
	if err == nil {
		t.Fatal("expected import cap error, got nil")
	}
	if !isCapError(err) {
		t.Fatalf("expected cap error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — raw URL (direct file link, real-world format)
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_RawURLImportsCorrectSkillDirectory(t *testing.T) {
	const host = "git.example.com"
	// URL mimics a real-world raw file link:
	// https://git.example.com/myorg/myrepo/-/raw/main/skills/myskill/SKILL.md
	rawURL := "https://" + host + "/myorg/myrepo/-/raw/main/skills/myskill/SKILL.md"
	skillPath := "skills/myskill"

	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Raw-Path") {
		case "/api/v4/projects/myorg%2Fmyrepo/repository/files/" +
			url.PathEscape(skillPath+"/SKILL.md") + "/raw":
			w.Write([]byte("---\nname: myskill\ndescription: my skill\n---\ncontent"))
		case "/api/v4/projects/myorg%2Fmyrepo/repository/tree":
			writeJSON(w, http.StatusOK, []gitlabTreeEntry{
				{Type: "blob", Name: "guide.md", Path: skillPath + "/guide.md"},
			})
		case "/api/v4/projects/myorg%2Fmyrepo/repository/files/" +
			url.PathEscape(skillPath+"/guide.md") + "/raw":
			w.Write([]byte("guide content"))
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitLab(client, rawURL, "")
	if err != nil {
		t.Fatalf("fetchFromGitLab: %v", err)
	}
	if result.name != "myskill" {
		t.Fatalf("name = %q, want myskill", result.name)
	}
	if result.origin["path"] != skillPath {
		t.Fatalf("origin.path = %q, want %q", result.origin["path"], skillPath)
	}
	wantFiles := []string{"guide.md"}
	gotFiles := importedFilePaths(result.files)
	if !equalStrings(gotFiles, wantFiles) {
		t.Fatalf("files = %v, want %v", gotFiles, wantFiles)
	}
}

// ---------------------------------------------------------------------------
// fetchFromGitLab — name falls back to repo name when frontmatter missing
// ---------------------------------------------------------------------------

func TestFetchFromGitLab_NameFallsBackToRepoName(t *testing.T) {
	const host = "gitlab.com"
	client, _ := newGitLabFixtureClient(t, []string{host}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Raw-Path") {
		case "/api/v4/projects/acme%2Fmy-awesome-skill/repository/files/SKILL.md/raw":
			w.Write([]byte("no frontmatter here"))
		case "/api/v4/projects/acme%2Fmy-awesome-skill/repository/tree":
			writeJSON(w, http.StatusOK, []gitlabTreeEntry{})
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitLab(client, "https://"+host+"/acme/my-awesome-skill", "")
	if err != nil {
		t.Fatalf("fetchFromGitLab: %v", err)
	}
	if result.name != "my-awesome-skill" {
		t.Fatalf("name = %q, want my-awesome-skill", result.name)
	}
}
