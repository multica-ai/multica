package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
			case "/repos/acme/skills/git/trees/main":
				// Truncated tree: resolution still succeeds by frontmatter, but
				// enumeration falls through to the per-directory crawl exercised
				// by this test.
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree:      []githubTreeEntry{{Path: "skills/pptx/SKILL.md", Type: "blob"}},
					Truncated: true,
				})
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
				w.Write([]byte("---\nname: pptx\n---\ncontent"))
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

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/pptx")
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
			case "/repos/acme/skills/git/trees/main":
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree:      []githubTreeEntry{{Path: "skills/pptx/SKILL.md", Type: "blob"}},
					Truncated: true,
				})
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
				w.Write([]byte("---\nname: pptx\n---\ncontent"))
			case "/acme/skills/main/skills/pptx/my dir/note.md":
				w.Write([]byte("note"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/pptx")
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
			case "/repos/acme/skills/git/trees/main":
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree:      []githubTreeEntry{{Path: "skills/pptx/SKILL.md", Type: "blob"}},
					Truncated: true,
				})
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
				w.Write([]byte("---\nname: pptx\n---\ncontent"))
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

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/pptx")
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
						{Path: "skills/composition-patterns/rules.md", Type: "blob", Size: 5},
						{Path: "skills/react-best-practices/SKILL.md", Type: "blob"},
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

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/vercel-labs/agent-skills/vercel-composition-patterns")
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
		t.Fatalf("expected primary tree lookup, got requests %v", *requests)
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
						{Path: "README.md", Type: "blob", Size: 8},
						{Path: "SKILL.md", Type: "blob", Size: 40},
						{Path: "assets", Type: "tree"},
						{Path: "assets/logo.png", Type: "blob", Size: 8},
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

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/alchaincyf/huashu-design/huashu-design")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}
	if result.name != "huashu-design" {
		t.Fatalf("name = %q, want huashu-design", result.name)
	}
	if !strings.HasPrefix(result.content, "---\nname: huashu-design") {
		t.Fatalf("SKILL.md content not populated, got %q", result.content)
	}
	// assets/logo.png is intentionally dropped by the binary-extension guard —
	// PG TEXT columns can't store image bytes, and agents never read them as
	// text. With tree-based enumeration the .png is filtered out from the tree
	// metadata, so it is never even downloaded.
	gotPaths := importedFilePaths(result.files)
	wantPaths := []string{"README.md"}
	if !equalStrings(gotPaths, wantPaths) {
		t.Fatalf("files = %v, want %v", gotPaths, wantPaths)
	}
	if !containsString(*requests, "api.github.com /repos/alchaincyf/huashu-design/git/trees/master?recursive=1") {
		t.Fatalf("expected recursive tree fetch, got %v", *requests)
	}
	for _, request := range *requests {
		if request == "raw.githubusercontent.com /alchaincyf/huashu-design/master/assets/logo.png" {
			t.Fatalf("binary asset must not be downloaded, got %v", *requests)
		}
	}
}

func TestFetchFromSkillsSh_PrefersMostSpecificDirOverCollidingRoot(t *testing.T) {
	// Multi-skill repo whose root SKILL.md name collides with the URL slug
	// ("wanted") — the real api-gateway-skill scenario — plus a deeper subdir
	// skill also named "wanted". Tree-first resolution must pick the most
	// specific matching directory (extras/wanted) instead of
	// letting the repo-root SKILL.md hijack the request — this is the
	// api-gateway-skill root-collision that used to make the importer crawl the
	// whole repository.
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
						{Path: "extras/wanted/ref.md", Type: "blob", Size: 3},
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/multi/main/SKILL.md":
				// Root SKILL.md name collides byte-for-byte with the URL slug —
				// the exact api-gateway-skill scenario. The most-specific
				// (deeper) match must still win over this root.
				w.Write([]byte("---\nname: wanted\n---\ncontent"))
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

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/multi/wanted")
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
		t.Fatalf("expected recursive tree lookup for skill resolution, got %v", *requests)
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

	_, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/vercel-composition-patterns")
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

// A skill whose tree exceeds the file-count cap must fail fast from the tree
// metadata alone, before any supporting file is downloaded. This is the
// api-gateway case: an over-limit skill returns a clear error in one tree call
// instead of crawling hundreds of directories until the reverse proxy times
// out (504).
func TestFetchFromSkillsSh_TreeFileCountCapFailsFastWithoutDownloads(t *testing.T) {
	tree := []githubTreeEntry{{Path: "skills/foo/SKILL.md", Type: "blob"}}
	for i := 0; i < maxImportFileCount+1; i++ {
		tree = append(tree, githubTreeEntry{
			Path: fmt.Sprintf("skills/foo/ref-%d.md", i),
			Type: "blob",
			Size: 4,
		})
	}

	client, requests := newGitHubFixtureClient(t, treeOnlySkillFixture(tree))

	_, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/foo")
	if err == nil {
		t.Fatal("expected file-count cap error")
	}
	if !isCapError(err) || !strings.Contains(err.Error(), "file limit") {
		t.Fatalf("error = %q, want file-count cap error", err.Error())
	}
	for _, req := range *requests {
		if strings.Contains(req, "ref-") {
			t.Fatalf("no supporting file should be downloaded before the cap check, saw %q", req)
		}
	}
}

// The total-byte cap is likewise enforced from tree sizes before any download.
func TestFetchFromSkillsSh_TreeTotalByteCapFailsFast(t *testing.T) {
	tree := []githubTreeEntry{{Path: "skills/foo/SKILL.md", Type: "blob"}}
	for i := 0; i < 9; i++ { // 9 MiB total > 8 MiB bundle cap, each at the per-file cap
		tree = append(tree, githubTreeEntry{
			Path: fmt.Sprintf("skills/foo/big-%d.md", i),
			Type: "blob",
			Size: maxImportFileSize,
		})
	}

	client, requests := newGitHubFixtureClient(t, treeOnlySkillFixture(tree))

	_, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/foo")
	if err == nil {
		t.Fatal("expected total-byte cap error")
	}
	if !isCapError(err) || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("error = %q, want total-byte cap error", err.Error())
	}
	for _, req := range *requests {
		if strings.Contains(req, "big-") {
			t.Fatalf("no supporting file should be downloaded before the cap check, saw %q", req)
		}
	}
}

// A skill under the raised 256-file cap imports its full supporting-file set
// from the tree via concurrent downloads, in a stable path order regardless of
// completion timing.
func TestFetchFromSkillsSh_TreeEnumerationImportsManyFiles(t *testing.T) {
	const count = 40
	tree := []githubTreeEntry{{Path: "skills/foo/SKILL.md", Type: "blob"}}
	for i := 0; i < count; i++ {
		tree = append(tree, githubTreeEntry{
			Path: fmt.Sprintf("skills/foo/ref-%02d.md", i),
			Type: "blob",
			Size: 3,
		})
	}

	client, _ := newGitHubFixtureClient(t, treeOnlySkillFixture(tree))

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/foo")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}
	if len(result.files) != count {
		t.Fatalf("imported %d files, want %d", len(result.files), count)
	}
	for i := 1; i < len(result.files); i++ {
		if result.files[i-1].path >= result.files[i].path {
			t.Fatalf("files not in stable sorted order: %q then %q", result.files[i-1].path, result.files[i].path)
		}
	}
}

// A skill sitting at a conventional path (skills/<name>/) must still import
// even when its frontmatter name doesn't byte-match the URL slug (e.g.
// `name: Foo` for slug `foo`). Tree-first resolution first tries frontmatter
// matching, then falls back to accepting the conventional path — restoring the
// pre-tree importer's lenient semantics without re-introducing the root
// collision.
func TestFetchFromSkillsSh_TreeAcceptsConventionalPathDespiteNameMismatch(t *testing.T) {
	tree := []githubTreeEntry{
		{Path: "skills/foo/SKILL.md", Type: "blob"},
		{Path: "skills/foo/ref.md", Type: "blob", Size: 3},
	}
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/git/trees/main":
				writeJSON(w, http.StatusOK, githubTreeResponse{Tree: tree})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/foo/SKILL.md":
				w.Write([]byte("---\nname: Foo\ndescription: display name\n---\nbody"))
			case "/acme/skills/main/skills/foo/ref.md":
				w.Write([]byte("ref"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/foo")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh: %v", err)
	}
	if result.name != "Foo" {
		t.Fatalf("name = %q, want Foo (frontmatter display name)", result.name)
	}
	if !equalStrings(importedFilePaths(result.files), []string{"ref.md"}) {
		t.Fatalf("files = %v, want [ref.md]", importedFilePaths(result.files))
	}
}

// If the overall import context is cancelled mid-download, the import must
// abort with an error rather than silently dropping the remaining files and
// reporting success with a half-populated bundle.
func TestFetchFromSkillsSh_ContextCancelledMidDownloadAborts(t *testing.T) {
	tree := []githubTreeEntry{
		{Path: "skills/foo/SKILL.md", Type: "blob"},
		{Path: "skills/foo/ref.md", Type: "blob", Size: 3},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/git/trees/main":
				writeJSON(w, http.StatusOK, githubTreeResponse{Tree: tree})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/foo/SKILL.md":
				w.Write([]byte("---\nname: foo\n---\nbody"))
			case "/acme/skills/main/skills/foo/ref.md":
				// Simulate the overall deadline firing during the download
				// phase: cancel the import context, then block until the
				// request is torn down so fetchRawFile observes the cancel.
				cancel()
				<-r.Context().Done()
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	_, err := fetchFromSkillsSh(ctx, client, "https://skills.sh/acme/skills/foo")
	if err == nil {
		t.Fatal("expected import to abort on cancellation, not silently drop files")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %q, want a propagated context.Canceled", err.Error())
	}
}

// When the tree fetch fails (e.g. GitHub API rate limiting) for the real
// api-gateway-skill shape — a same-named repo-root SKILL.md plus the actual
// skill nested at a non-conventional path — the importer must NOT fall back to
// resolving the repository root and "succeed" with the wrong SKILL.md and zero
// files. It must return a retryable error.
func TestFetchFromSkillsSh_TreeFetchFailureReturnsRetryableError(t *testing.T) {
	var rootProbed bool
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/maton/api-gateway-skill":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/maton/api-gateway-skill/git/trees/main":
				// Rate limited.
				http.Error(w, "rate limited", http.StatusForbidden)
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			if r.URL.Path == "/maton/api-gateway-skill/main/SKILL.md" {
				rootProbed = true
				w.Write([]byte("---\nname: api-gateway\n---\nroot"))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/maton/api-gateway-skill/api-gateway")
	if err == nil {
		t.Fatalf("expected a retryable error on tree fetch failure, got skill %+v", result)
	}
	if !errors.Is(err, errImportSourceUnavailable) {
		t.Fatalf("error = %q, want errImportSourceUnavailable", err.Error())
	}
	if rootProbed {
		t.Fatal("the colliding repo-root SKILL.md must not be probed/selected when the tree is unavailable")
	}
}

// A skill at a conventional path with a display-name frontmatter must import
// identically whether or not GitHub truncated the tree — the truncated branch
// must also honor path-based acceptance (Elon review point 3).
func TestFetchFromSkillsSh_TruncatedTreeAcceptsConventionalPathDespiteNameMismatch(t *testing.T) {
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/git/trees/main":
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree:      []githubTreeEntry{{Path: "skills/foo/SKILL.md", Type: "blob"}},
					Truncated: true,
				})
			case "/repos/acme/skills/contents/skills/foo":
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "ref.md",
						Path:        "skills/foo/ref.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/skills/main/skills/foo/ref.md",
					},
				})
			default:
				// skills/.claude/skills/plugin conventional-prefix listings 404.
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/foo/SKILL.md":
				w.Write([]byte("---\nname: Foo\ndescription: display name\n---\nbody"))
			case "/acme/skills/main/skills/foo/ref.md":
				w.Write([]byte("ref"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	result, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/foo")
	if err != nil {
		t.Fatalf("fetchFromSkillsSh (truncated): %v", err)
	}
	if result.name != "Foo" {
		t.Fatalf("name = %q, want Foo", result.name)
	}
	if !equalStrings(importedFilePaths(result.files), []string{"ref.md"}) {
		t.Fatalf("files = %v, want [ref.md]", importedFilePaths(result.files))
	}
}

// The crawl fallback (used by the truncated-tree branch) must also treat a
// mid-download cancellation as fatal, not silently drop the remaining files.
func TestFetchFromSkillsSh_CrawlFallbackContextCancelledAborts(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/git/trees/main":
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree:      []githubTreeEntry{{Path: "skills/foo/SKILL.md", Type: "blob"}},
					Truncated: true,
				})
			case "/repos/acme/skills/contents/skills/foo":
				writeJSON(w, http.StatusOK, []githubContentEntry{
					{
						Name:        "ref.md",
						Path:        "skills/foo/ref.md",
						Type:        "file",
						DownloadURL: "https://raw.githubusercontent.com/acme/skills/main/skills/foo/ref.md",
					},
				})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch r.URL.Path {
			case "/acme/skills/main/skills/foo/SKILL.md":
				w.Write([]byte("---\nname: foo\n---\nbody"))
			case "/acme/skills/main/skills/foo/ref.md":
				cancel()
				<-r.Context().Done()
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})

	_, err := fetchFromSkillsSh(ctx, client, "https://skills.sh/acme/skills/foo")
	if err == nil {
		t.Fatal("expected crawl fallback to abort on cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %q, want context.Canceled", err.Error())
	}
}

// ClawHub imports must likewise abort when the context is cancelled during a
// supporting-file download rather than reporting a half-populated success.
func TestFetchFromClawHub_ContextCancelledMidDownloadAborts(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	slug := "review-helper"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/skills/"+slug:
			writeJSON(w, http.StatusOK, map[string]any{
				"skill": map[string]any{
					"slug": slug, "displayName": slug, "summary": "s",
					"tags": map[string]string{"latest": "1.0.0"},
				},
			})
		case r.URL.Path == "/api/v1/skills/"+slug+"/versions/1.0.0":
			writeJSON(w, http.StatusOK, map[string]any{
				"version": map[string]any{
					"version": "1.0.0",
					"files": []map[string]any{
						{"path": "SKILL.md", "size": 16},
						{"path": "ref.md", "size": 8},
					},
				},
			})
		case r.URL.Path == "/api/v1/skills/"+slug+"/file":
			switch r.URL.Query().Get("path") {
			case "SKILL.md":
				w.Write([]byte("# Imported\n"))
			case "ref.md":
				cancel()
				<-r.Context().Done()
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	prev := clawHubAPIBase
	clawHubAPIBase = srv.URL + "/api/v1"
	t.Cleanup(func() { clawHubAPIBase = prev; srv.Close() })

	_, err := fetchFromClawHub(ctx, &http.Client{}, "https://clawhub.ai/acme/"+slug)
	if err == nil {
		t.Fatal("expected ClawHub import to abort on cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %q, want context.Canceled", err.Error())
	}
}

// A cancellation that lands while the top-level contents listing body is being
// read (200 headers received, decode fails with "context canceled") must abort
// the crawl, not be swallowed into a valid-SKILL.md/zero-files "success".
func TestFetchFromSkillsSh_CrawlListingDecodeCancelledAborts(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client, _ := newGitHubFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/git/trees/main":
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree:      []githubTreeEntry{{Path: "skills/foo/SKILL.md", Type: "blob"}},
					Truncated: true,
				})
			case "/repos/acme/skills/contents/skills/foo":
				// Flush 200 headers so the client sees a valid response, then
				// cancel and block: the JSON decode of the body is torn down
				// mid-stream and returns "context canceled".
				w.WriteHeader(http.StatusOK)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				cancel()
				<-r.Context().Done()
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

	_, err := fetchFromSkillsSh(ctx, client, "https://skills.sh/acme/skills/foo")
	if err == nil {
		t.Fatal("expected crawl listing decode to abort on cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %q, want context.Canceled", err.Error())
	}
}

func TestImportFetchErrorResponse(t *testing.T) {
	capErr := fmt.Errorf("%w: import bundle would contain 999 files", errImportCapExceeded)
	if status, _ := importFetchErrorResponse(context.Background(), capErr); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("cap error status = %d, want 413", status)
	}
	if status, _ := importFetchErrorResponse(context.Background(), context.DeadlineExceeded); status != http.StatusGatewayTimeout {
		t.Fatalf("deadline error status = %d, want 504", status)
	}
	unavailErr := fmt.Errorf("%w: could not read tree", errImportSourceUnavailable)
	if status, _ := importFetchErrorResponse(context.Background(), unavailErr); status != http.StatusServiceUnavailable {
		t.Fatalf("source-unavailable status = %d, want 503", status)
	}
	if status, _ := importFetchErrorResponse(context.Background(), fmt.Errorf("boom")); status != http.StatusBadGateway {
		t.Fatalf("generic error status = %d, want 502", status)
	}
}

// treeOnlySkillFixture serves the default branch, one recursive tree, the
// skills/foo/SKILL.md body, and a stub body for every skills/foo/* supporting
// file. It is the minimal fixture for exercising the tree-first primary path.
func treeOnlySkillFixture(tree []githubTreeEntry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case "/repos/acme/skills":
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case "/repos/acme/skills/git/trees/main":
				writeJSON(w, http.StatusOK, githubTreeResponse{Tree: tree})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			switch {
			case r.URL.Path == "/acme/skills/main/skills/foo/SKILL.md":
				w.Write([]byte("---\nname: foo\n---\nbody"))
			case strings.HasPrefix(r.URL.Path, "/acme/skills/main/skills/foo/"):
				w.Write([]byte("x"))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}
}

func TestFetchFromSkillsSh_AnthropicPptxIntegration(t *testing.T) {
	if os.Getenv("MULTICA_RUN_SKILLS_SH_INTEGRATION") == "" {
		t.Skip("set MULTICA_RUN_SKILLS_SH_INTEGRATION=1 to run live GitHub integration test")
	}

	result, err := fetchFromSkillsSh(t.Context(), &http.Client{Timeout: 30 * time.Second}, "https://skills.sh/anthropics/skills/pptx")
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

	result, err := fetchFromGitHub(t.Context(), client, "https://github.com/anthropics/skills/tree/main/document-skills/pptx")
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

	result, err := fetchFromGitHub(t.Context(), client, "https://github.com/alice/single-skill")
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

	_, err := fetchFromGitHub(t.Context(), client, "https://github.com/alice/multi")
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

	result, err := fetchFromGitHub(t.Context(), client, "https://github.com/acme/skills/blob/main/skills/foo/SKILL.md")
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

// --- Raw file auth header host gating ---

// The GitHub token must reach raw.githubusercontent.com (so private-repo
// SKILL.md / file downloads authenticate) but must never be sent to the
// non-GitHub hosts (clawhub.ai, skills.sh) that share fetchRawFile.
func TestNewRawFileRequest_AttachesGitHubTokenOnlyForRawGitHubHost(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret-token")

	cases := []struct {
		name     string
		url      string
		wantAuth string
	}{
		{
			name:     "raw github host authenticates",
			url:      "https://raw.githubusercontent.com/acme/private/main/skills/foo/SKILL.md",
			wantAuth: "Bearer secret-token",
		},
		{
			name:     "clawhub host never receives the token",
			url:      "https://clawhub.ai/api/skills/foo/file?path=SKILL.md",
			wantAuth: "",
		},
		{
			name:     "skills.sh host never receives the token",
			url:      "https://skills.sh/acme/foo/SKILL.md",
			wantAuth: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := newRawFileRequest(t.Context(), tc.url)
			if err != nil {
				t.Fatalf("newRawFileRequest(%q): %v", tc.url, err)
			}
			if got := req.Header.Get("Authorization"); got != tc.wantAuth {
				t.Fatalf("Authorization = %q, want %q", got, tc.wantAuth)
			}
		})
	}
}

func TestNewRawFileRequest_NoAuthHeaderWhenTokenUnset(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	req, err := newRawFileRequest(t.Context(), "https://raw.githubusercontent.com/acme/private/main/SKILL.md")
	if err != nil {
		t.Fatalf("newRawFileRequest: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty when GITHUB_TOKEN is unset", got)
	}
}

// --- Bundle / file size cap tests ---

func TestFetchRawFile_ReturnsErrorOnOversizedFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("a"), maxImportFileSize+1024))
	}))
	t.Cleanup(server.Close)

	_, err := fetchRawFile(t.Context(), &http.Client{}, server.URL+"/big.bin")
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
	_, err := fetchFromGitHub(t.Context(), client, "https://github.com/acme/skills/tree/main/foo")
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
			case "/repos/acme/skills/git/trees/main":
				// Truncated so enumeration goes through the crawl fallback,
				// which enforces the per-file cap during download.
				writeJSON(w, http.StatusOK, githubTreeResponse{
					Tree:      []githubTreeEntry{{Path: "skills/foo/SKILL.md", Type: "blob"}},
					Truncated: true,
				})
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
	_, err := fetchFromSkillsSh(t.Context(), client, "https://skills.sh/acme/skills/foo")
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
	result, err := fetchFromGitHub(t.Context(), client, "https://github.com/acme/skills/tree/release/v2/skills/foo")
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
	_, err := fetchFromGitHub(t.Context(), client, "https://github.com/acme/skills/tree/nope/skills/foo")
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
	result, err := fetchFromGitHub(t.Context(), client, "https://github.com/anthropics/skills/tree/main/skills/pptx")
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
	if _, err := fetchFromGitHub(t.Context(), client, "https://github.com/acme/skills/tree/main/skills/foo"); err != nil {
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
