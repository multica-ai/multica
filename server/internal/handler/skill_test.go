package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func ptrString(value string) *string {
	return &value
}

// --- Gitee test helpers ---

func newGiteeFixtureClient(t *testing.T, handler http.HandlerFunc) (*http.Client, *[]string) {
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
				"gitee.com": {},
			},
		},
	}, &requests
}

// --- Gitee tests ---

func TestDetectImportSource_RecognizesGitee(t *testing.T) {
	src, _, err := detectImportSource("https://gitee.com/acme/skill")
	if err != nil {
		t.Fatalf("detectImportSource: %v", err)
	}
	if src != sourceGitee {
		t.Fatalf("source = %v, want sourceGitee", src)
	}
}

func TestDetectImportSource_RecognizesGiteeWithoutScheme(t *testing.T) {
	src, normalized, err := detectImportSource("gitee.com/acme/skill")
	if err != nil {
		t.Fatalf("detectImportSource: %v", err)
	}
	if src != sourceGitee {
		t.Fatalf("source = %v, want sourceGitee", src)
	}
	if normalized != "https://gitee.com/acme/skill" {
		t.Fatalf("normalized = %q, want https://gitee.com/acme/skill", normalized)
	}
}

func TestDetectImportSource_RecognizesGiteeSSHURL(t *testing.T) {
	src, normalized, err := detectImportSource("git@gitee.com:acme/skill.git")
	if err != nil {
		t.Fatalf("detectImportSource: %v", err)
	}
	if src != sourceGitee {
		t.Fatalf("source = %v, want sourceGitee", src)
	}
	if normalized != "https://gitee.com/acme/skill" {
		t.Fatalf("normalized = %q, want https://gitee.com/acme/skill", normalized)
	}
}

func TestDetectImportSource_RecognizesGitHubSSHURL(t *testing.T) {
	src, normalized, err := detectImportSource("git@github.com:acme/skill.git")
	if err != nil {
		t.Fatalf("detectImportSource: %v", err)
	}
	if src != sourceGitHub {
		t.Fatalf("source = %v, want sourceGitHub", src)
	}
	if normalized != "https://github.com/acme/skill" {
		t.Fatalf("normalized = %q, want https://github.com/acme/skill", normalized)
	}
}

func TestFetchFromGitee_TreeURLImportsSkillDirectory(t *testing.T) {
	client, requests := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		host := r.Header.Get("X-Test-Original-Host")
		if host != "gitee.com" {
			http.NotFound(w, r)
			return
		}
		switch {
		// Ref validation
		case r.URL.Path == "/api/v5/repos/acme/skills/commits/main":
			w.Write([]byte(`{"sha":"deadbeef"}`))
		// Contents listing
		case r.URL.Path == "/api/v5/repos/acme/skills/contents/my-skill":
			if got := r.URL.Query().Get("ref"); got != "main" {
				t.Fatalf("contents ref = %q, want main", got)
			}
			writeJSON(w, http.StatusOK, []githubContentEntry{
				{
					Name: "helper.py",
					Path: "my-skill/helper.py",
					Type: "file",
				},
			})
		// Raw SKILL.md
		case r.URL.Path == "/acme/skills/raw/main/my-skill/SKILL.md":
			w.Write([]byte("---\nname: my-skill\ndescription: test skill\n---\nbody"))
		// Raw helper.py
		case r.URL.Path == "/acme/skills/raw/main/my-skill/helper.py":
			w.Write([]byte("print('helper')"))
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitee(client, "https://gitee.com/acme/skills/tree/main/my-skill", "")
	if err != nil {
		t.Fatalf("fetchFromGitee: %v", err)
	}
	if result.name != "my-skill" {
		t.Fatalf("name = %q, want my-skill", result.name)
	}
	if result.description != "test skill" {
		t.Fatalf("description = %q, want test skill", result.description)
	}
	gotPaths := importedFilePaths(result.files)
	wantPaths := []string{"helper.py"}
	if !equalStrings(gotPaths, wantPaths) {
		t.Fatalf("files = %v, want %v", gotPaths, wantPaths)
	}
	origin := result.origin
	if origin == nil || origin["type"] != "gitee" {
		t.Fatalf("origin = %v, want type=gitee", origin)
	}
	if origin["ref"] != "main" || origin["path"] != "my-skill" {
		t.Fatalf("origin ref/path mismatch: %v", origin)
	}
	if !containsString(*requests, "gitee.com /api/v5/repos/acme/skills/contents/my-skill?ref=main") {
		t.Fatalf("expected contents listing, got %v", *requests)
	}
}

func TestFetchFromGitee_RepoRootResolvesDefaultBranch(t *testing.T) {
	client, requests := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		host := r.Header.Get("X-Test-Original-Host")
		if host != "gitee.com" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/v5/repos/alice/single-skill":
			writeJSON(w, http.StatusOK, map[string]any{"default_branch": "master"})
		case "/api/v5/repos/alice/single-skill/contents":
			writeJSON(w, http.StatusOK, []githubContentEntry{})
		case "/alice/single-skill/raw/master/SKILL.md":
			w.Write([]byte("---\nname: single-skill\ndescription: one\n---\nroot body"))
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitee(client, "https://gitee.com/alice/single-skill", "")
	if err != nil {
		t.Fatalf("fetchFromGitee: %v", err)
	}
	if result.name != "single-skill" {
		t.Fatalf("name = %q, want single-skill", result.name)
	}
	if !containsString(*requests, "gitee.com /api/v5/repos/alice/single-skill") {
		t.Fatalf("expected default branch probe, got %v", *requests)
	}
}

func TestFetchFromGitee_MissingSKILLmdReturnsActionableError(t *testing.T) {
	client, _ := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		host := r.Header.Get("X-Test-Original-Host")
		if host != "gitee.com" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/v5/repos/bob/no-skill":
			writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
		default:
			http.NotFound(w, r)
		}
	})

	_, err := fetchFromGitee(client, "https://gitee.com/bob/no-skill", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "SKILL.md not found") {
		t.Fatalf("error = %v, want SKILL.md not found hint", err)
	}
	if !strings.Contains(err.Error(), "gitee.com") {
		t.Fatalf("error = %v, want gitee.com in the hint", err)
	}
}

func TestFetchFromGitee_BlobURLImportsSpecificSkill(t *testing.T) {
	client, _ := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		host := r.Header.Get("X-Test-Original-Host")
		if host != "gitee.com" {
			http.NotFound(w, r)
			return
		}
		switch {
		case r.URL.Path == "/api/v5/repos/acme/skills/commits/main":
			w.Write([]byte(`{"sha":"abc123"}`))
		case r.URL.Path == "/api/v5/repos/acme/skills/contents/tools/pptx":
			writeJSON(w, http.StatusOK, []githubContentEntry{})
		case r.URL.Path == "/acme/skills/raw/main/tools/pptx/SKILL.md":
			w.Write([]byte("---\nname: pptx\ndescription: pptx tools\n---\nbody"))
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitee(client, "https://gitee.com/acme/skills/blob/main/tools/pptx/SKILL.md", "")
	if err != nil {
		t.Fatalf("fetchFromGitee: %v", err)
	}
	if result.name != "pptx" {
		t.Fatalf("name = %q, want pptx", result.name)
	}
	origin := result.origin
	if origin["type"] != "gitee" || origin["path"] != "tools/pptx" {
		t.Fatalf("origin mismatch: %v", origin)
	}
}

func TestFetchFromGitee_SendsEnvAuthHeaderWhenTokenSet(t *testing.T) {
	var authHeaders []string
	client, _ := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		if r.URL.Path == "/api/v5/repos/x/y" {
			writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			return
		}
		if r.URL.Path == "/x/y/raw/main/SKILL.md" {
			w.Write([]byte("---\nname: y\n---\nbody"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v5/") {
			writeJSON(w, http.StatusOK, []githubContentEntry{})
			return
		}
		http.NotFound(w, r)
	})

	t.Setenv("GITEE_TOKEN", "test-token-123")
	_, err := fetchFromGitee(client, "https://gitee.com/x/y", "")
	if err != nil {
		t.Fatalf("fetchFromGitee: %v", err)
	}
	for i, gotAuth := range authHeaders {
		if gotAuth != "token test-token-123" {
			t.Fatalf("request %d Authorization = %q, want 'token test-token-123'", i, gotAuth)
		}
	}
}

func TestFetchFromGitee_SendsRequestTokenToAPIAndRawRequests(t *testing.T) {
	var authHeaders []string
	client, _ := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/v5/repos/x/y":
			writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
		case "/x/y/raw/main/SKILL.md":
			w.Write([]byte("---\nname: y\n---\nbody"))
		case "/api/v5/repos/x/y/contents":
			writeJSON(w, http.StatusOK, []githubContentEntry{
				{Name: "helper.md", Path: "helper.md", Type: "file"},
			})
		case "/x/y/raw/main/helper.md":
			w.Write([]byte("helper"))
		default:
			http.NotFound(w, r)
		}
	})

	t.Setenv("GITEE_TOKEN", "env-token")
	result, err := fetchFromGitee(client, "https://gitee.com/x/y", "request-token")
	if err != nil {
		t.Fatalf("fetchFromGitee: %v", err)
	}
	if got := importedFilePaths(result.files); !equalStrings(got, []string{"helper.md"}) {
		t.Fatalf("files = %v, want [helper.md]", got)
	}
	for i, gotAuth := range authHeaders {
		if gotAuth != "token request-token" {
			t.Fatalf("request %d Authorization = %q, want 'token request-token'", i, gotAuth)
		}
	}
}

func TestFetchFromGitee_UsesContentsAPIForPrivateRepoFileBody(t *testing.T) {
	var rawSkillRequested bool
	client, _ := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v5/repos/private/repo":
			if got := r.Header.Get("Authorization"); got != "token request-token" {
				t.Fatalf("repo Authorization = %q, want request token", got)
			}
			writeJSON(w, http.StatusOK, map[string]any{"default_branch": "master"})
		case "/api/v5/repos/private/repo/contents/SKILL.md":
			if got := r.URL.Query().Get("ref"); got != "master" {
				t.Fatalf("contents ref = %q, want master", got)
			}
			if got := r.Header.Get("Authorization"); got != "token request-token" {
				t.Fatalf("contents Authorization = %q, want request token", got)
			}
			writeJSON(w, http.StatusOK, githubContentEntry{
				Name:     "SKILL.md",
				Path:     "SKILL.md",
				Type:     "file",
				Content:  ptrString(base64.StdEncoding.EncodeToString([]byte("---\nname: private-repo\n---\nbody"))),
				Encoding: "base64",
			})
		case "/api/v5/repos/private/repo/contents":
			writeJSON(w, http.StatusOK, []githubContentEntry{})
		case "/private/repo/raw/master/SKILL.md":
			rawSkillRequested = true
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitee(client, "https://gitee.com/private/repo", "request-token")
	if err != nil {
		t.Fatalf("fetchFromGitee: %v", err)
	}
	if result.name != "private-repo" {
		t.Fatalf("name = %q, want private-repo", result.name)
	}
	if rawSkillRequested {
		t.Fatal("raw SKILL.md should not be requested when contents API returns file content")
	}
}

func TestFetchFromGitee_PrivateRepoWithoutTokenReturnsActionableError(t *testing.T) {
	client, _ := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v5/repos/private/repo" {
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	})

	_, err := fetchFromGitee(client, "https://gitee.com/private/repo", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errGiteeAuthRequired) {
		t.Fatalf("error = %v, want errGiteeAuthRequired", err)
	}
	if !strings.Contains(err.Error(), "Gitee 个人访问令牌") {
		t.Fatalf("error = %v, want token guidance", err)
	}
}

func TestFetchFromGitee_PrivateRepoTreeURLWithoutTokenReturnsActionableError(t *testing.T) {
	client, _ := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v5/repos/private/repo/commits/main", "/api/v5/repos/private/repo":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	_, err := fetchFromGitee(client, "https://gitee.com/private/repo/tree/main/skill", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errGiteeAuthRequired) {
		t.Fatalf("error = %v, want errGiteeAuthRequired", err)
	}
}

func TestFetchFromGitee_DefaultBranchFallsBackToMaster(t *testing.T) {
	client, _ := newGiteeFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		host := r.Header.Get("X-Test-Original-Host")
		if host != "gitee.com" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/v5/repos/alice/repo":
			// Simulate API failure
			w.WriteHeader(http.StatusInternalServerError)
		case "/alice/repo/raw/master/SKILL.md":
			w.Write([]byte("---\nname: repo\n---\nbody"))
		case "/api/v5/repos/alice/repo/contents":
			writeJSON(w, http.StatusOK, []githubContentEntry{})
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromGitee(client, "https://gitee.com/alice/repo", "")
	if err != nil {
		t.Fatalf("fetchFromGitee: %v", err)
	}
	if result.name != "repo" {
		t.Fatalf("name = %q, want repo", result.name)
	}
}

func TestBatchImportSkillsOverwrite_PreservesSkillIDAndAgentAssociation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	var existingSkillID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, 'Batch Import Overwrite', 'Old description', '# Old', '{}'::jsonb, $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&existingSkillID); err != nil {
		t.Fatalf("create existing skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, existingSkillID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO skill_file (skill_id, path, content)
		VALUES ($1, 'old.md', 'old file')
	`, existingSkillID); err != nil {
		t.Fatalf("create existing skill file: %v", err)
	}
	agentID := createHandlerTestAgent(t, "Batch Import Overwrite Agent", []byte("[]"))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_skill (agent_id, skill_id)
		VALUES ($1, $2)
	`, agentID, existingSkillID); err != nil {
		t.Fatalf("attach skill to agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/skills/batch-import", map[string]any{
		"skills": []map[string]any{
			{
				"name":        "Batch Import Overwrite",
				"description": "Updated description",
				"content":     "# Updated",
				"overwrite":   true,
				"files": []map[string]any{
					{"path": "new.md", "content": "new file"},
				},
			},
		},
	})
	testHandler.BatchImportSkills(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("BatchImportSkills: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp BatchImportSkillsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Created) != 1 {
		t.Fatalf("created len = %d, want 1", len(resp.Created))
	}
	if len(resp.Skipped) != 0 {
		t.Fatalf("skipped = %v, want empty", resp.Skipped)
	}
	if resp.Created[0].ID != existingSkillID {
		t.Fatalf("skill id changed: got %s, want %s", resp.Created[0].ID, existingSkillID)
	}
	if resp.Created[0].Description != "Updated description" {
		t.Fatalf("description = %q", resp.Created[0].Description)
	}

	var attachedCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_skill
		WHERE agent_id = $1 AND skill_id = $2
	`, agentID, existingSkillID).Scan(&attachedCount); err != nil {
		t.Fatalf("count agent_skill rows: %v", err)
	}
	if attachedCount != 1 {
		t.Fatalf("agent_skill association was not preserved")
	}

	var fileContent string
	if err := testPool.QueryRow(ctx, `
		SELECT content
		FROM skill_file
		WHERE skill_id = $1 AND path = 'new.md'
	`, existingSkillID).Scan(&fileContent); err != nil {
		t.Fatalf("load replacement file: %v", err)
	}
	if fileContent != "new file" {
		t.Fatalf("replacement file content = %q", fileContent)
	}
	if count := countSkillFiles(t, existingSkillID); count != 1 {
		t.Fatalf("expected one replacement file, got %d", count)
	}
}
