package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeSharedBundledMarketplace materializes a minimal openai-bundled
// marketplace under sharedHome, the way ChatGPT Desktop does before a task
// runs.
func writeSharedBundledMarketplace(t *testing.T, sharedHome string) (bundledRoot, marketplaceDir string) {
	t.Helper()
	bundledRoot = filepath.Join(sharedHome, ".tmp", "bundled-marketplaces")
	marketplaceDir = filepath.Join(bundledRoot, "openai-bundled")
	if err := os.MkdirAll(filepath.Join(marketplaceDir, ".agents", "plugins"), 0o755); err != nil {
		t.Fatalf("create shared marketplace dir: %v", err)
	}
	manifest := `{"name":"openai-bundled","plugins":[{"name":"browser","source":{"source":"local","path":"./plugins/browser"}}]}`
	if err := os.WriteFile(filepath.Join(marketplaceDir, ".agents", "plugins", "marketplace.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write shared marketplace manifest: %v", err)
	}
	return bundledRoot, marketplaceDir
}

// writeTaskConfigWithMarketplaces writes a copied-style config.toml whose
// marketplaces tables mirror what ChatGPT Desktop leaves in a real shared
// home: one bundled local source inside the shared home, one local source
// outside it, and unrelated surrounding content that must survive untouched.
func writeTaskConfigWithMarketplaces(t *testing.T, codexHome, sharedBundledRoot, outsideSource string) string {
	t.Helper()
	config := `model = "gpt-6.2-sol"

[mcp_servers.node_repl]
command = "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl"

[marketplaces.openai-bundled]
source_type = "local"
source = "` + filepath.Join(sharedBundledRoot, "openai-bundled") + `"

[marketplaces.openai-primary-runtime]
source_type = "local"
source = "` + outsideSource + `"

[plugins."browser@openai-bundled"]
enabled = false

[plugins."codex-app-tools@openai-bundled"]
enabled = true
`
	path := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("write task config: %v", err)
	}
	return path
}

func TestExposeSharedCodexBundledMarketplacesLinksAndRepointsSource(t *testing.T) {
	sharedHome := t.TempDir()
	codexHome := t.TempDir()
	bundledRoot, _ := writeSharedBundledMarketplace(t, sharedHome)
	outside := filepath.Join(t.TempDir(), "codex-runtimes", "primary-runtime")
	configPath := writeTaskConfigWithMarketplaces(t, codexHome, bundledRoot, outside)

	if err := exposeSharedCodexBundledMarketplaces(codexHome, sharedHome); err != nil {
		t.Fatalf("exposeSharedCodexBundledMarketplaces: %v", err)
	}

	link := filepath.Join(codexHome, ".tmp", "bundled-marketplaces")
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("task bundled marketplaces link: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("task bundled marketplaces is not a symlink: mode %v", fi.Mode())
	}
	if target, err := os.Readlink(link); err != nil || target != bundledRoot {
		t.Fatalf("link target = %q, err %v; want %q", target, err, bundledRoot)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	config := string(data)

	wantSource := "source = " + strconv.Quote(filepath.Join(codexHome, ".tmp", "bundled-marketplaces", "openai-bundled"))
	if !strings.Contains(config, wantSource) {
		t.Fatalf("config does not contain repointed bundled source %s:\n%s", wantSource, config)
	}
	if !strings.Contains(config, "source = "+strconv.Quote(outside)) {
		t.Fatalf("config lost the outside marketplace source:\n%s", config)
	}
	// Non-marketplace content must survive the patch verbatim.
	if !strings.Contains(config, `[plugins."browser@openai-bundled"]`) {
		t.Fatalf("config lost unrelated sections:\n%s", config)
	}
	// The linked marketplace must resolve to the shared manifest, so the CLI
	// actually finds the plugins behind the repointed source.
	if _, err := os.Stat(filepath.Join(link, "openai-bundled", ".agents", "plugins", "marketplace.json")); err != nil {
		t.Fatalf("linked marketplace manifest not reachable: %v", err)
	}
}

func TestExposeSharedCodexBundledMarketplacesAbsentSharedHomeIsNoop(t *testing.T) {
	sharedHome := t.TempDir()
	codexHome := t.TempDir()
	bundledRoot := filepath.Join(sharedHome, ".tmp", "bundled-marketplaces")
	configPath := writeTaskConfigWithMarketplaces(t, codexHome, bundledRoot, filepath.Join(t.TempDir(), "elsewhere"))

	if err := exposeSharedCodexBundledMarketplaces(codexHome, sharedHome); err != nil {
		t.Fatalf("exposeSharedCodexBundledMarketplaces: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(codexHome, ".tmp")); !os.IsNotExist(err) {
		t.Fatalf("task .tmp should not be created when the shared home has no bundled marketplaces: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "source = "+strconv.Quote(filepath.Join(bundledRoot, "openai-bundled"))) {
		t.Fatalf("config source should stay untouched when there is nothing to expose:\n%s", data)
	}
}

func TestExposeSharedCodexBundledMarketplacesIdempotentAndRebuildsStaleLink(t *testing.T) {
	sharedHome := t.TempDir()
	codexHome := t.TempDir()
	bundledRoot, _ := writeSharedBundledMarketplace(t, sharedHome)
	configPath := writeTaskConfigWithMarketplaces(t, codexHome, bundledRoot, filepath.Join(t.TempDir(), "elsewhere"))
	wantSource := "source = " + strconv.Quote(filepath.Join(codexHome, ".tmp", "bundled-marketplaces", "openai-bundled"))

	if err := exposeSharedCodexBundledMarketplaces(codexHome, sharedHome); err != nil {
		t.Fatalf("first expose: %v", err)
	}

	// A stale link (wrong target) must be rebuilt, and a re-run must be a
	// no-op on the already-correct state — prepare runs on every reuse.
	stale := filepath.Join(codexHome, ".tmp", "bundled-marketplaces")
	if err := os.Remove(stale); err != nil {
		t.Fatalf("remove link for stale test: %v", err)
	}
	if err := os.Symlink(t.TempDir(), stale); err != nil {
		t.Fatalf("create stale link: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := exposeSharedCodexBundledMarketplaces(codexHome, sharedHome); err != nil {
			t.Fatalf("expose run %d: %v", i, err)
		}
		if target, err := os.Readlink(stale); err != nil || target != bundledRoot {
			t.Fatalf("run %d: link target = %q, err %v; want %q", i, target, err, bundledRoot)
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("run %d: read config: %v", i, err)
		}
		if !strings.Contains(string(data), wantSource) {
			t.Fatalf("run %d: config lost repointed source:\n%s", i, data)
		}
	}
	if got := strings.Count(string(mustRead(t, configPath)), wantSource); got != 1 {
		t.Fatalf("config contains repointed source %d times, want 1", got)
	}
}

func TestRewriteSharedBundledMarketplaceSourcesSkipsMissingAndLiteralSources(t *testing.T) {
	sharedHome := t.TempDir()
	codexHome := t.TempDir()
	bundledRoot, _ := writeSharedBundledMarketplace(t, sharedHome)

	// A source pointing at a shared bundled path that no longer exists must
	// stay untouched (EvalSymlinks fails), and a literal-string source must
	// still be repointed.
	config := `[marketplaces.gone]
source_type = "local"
source = "` + filepath.Join(bundledRoot, "vanished") + `"

[marketplaces.literal]
source = '` + filepath.Join(bundledRoot, "openai-bundled") + `'
`
	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := rewriteSharedBundledMarketplaceSources(configPath, bundledRoot, filepath.Join(codexHome, ".tmp", "bundled-marketplaces")); err != nil {
		t.Fatalf("rewriteSharedBundledMarketplaceSources: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "source = "+strconv.Quote(filepath.Join(bundledRoot, "vanished"))) {
		t.Fatalf("missing shared source must stay untouched:\n%s", got)
	}
	if !strings.Contains(got, "source = "+strconv.Quote(filepath.Join(codexHome, ".tmp", "bundled-marketplaces", "openai-bundled"))) {
		t.Fatalf("literal-string source must be repointed:\n%s", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// TestExposeSharedCodexBundledMarketplacesSpellingVariants covers the valid
// TOML spellings a user config can legally use: trailing comments on the
// table header and on the source line, and a quoted key. Every variant must
// resolve the same way the CLI resolves it — full parse for discovery, line
// splice for the rewrite — and produce a config whose source points into the
// task home.
func TestExposeSharedCodexBundledMarketplacesSpellingVariants(t *testing.T) {
	for _, tc := range []struct {
		name, header, sourceLineFormat string
	}{
		{"canonical", "[marketplaces.openai-bundled]", "source = %q"},
		{"source_comment", "[marketplaces.openai-bundled]", "source = %q # Desktop bundle"},
		{"header_comment", "[marketplaces.openai-bundled] # Desktop bundle", "source = %q"},
		{"quoted_source_key", "[marketplaces.openai-bundled]", `"source" = %q`},
		{"literal_string_value", "[marketplaces.openai-bundled]", "source = '%s'"},
		{"comment_and_quoted_key", `[marketplaces.openai-bundled] # comment`, `"source" = %q # trailing`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sharedHome := t.TempDir()
			codexHome := t.TempDir()
			bundledRoot, _ := writeSharedBundledMarketplace(t, sharedHome)
			sharedSource := filepath.Join(bundledRoot, "openai-bundled")

			var sourceLine string
			if strings.Contains(tc.sourceLineFormat, "%s") {
				sourceLine = fmt.Sprintf(tc.sourceLineFormat, sharedSource)
			} else {
				sourceLine = fmt.Sprintf(tc.sourceLineFormat, sharedSource)
			}
			config := tc.header + "\nsource_type = \"local\"\n" + sourceLine + "\n"
			configPath := filepath.Join(codexHome, "config.toml")
			if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			if err := exposeSharedCodexBundledMarketplaces(codexHome, sharedHome); err != nil {
				t.Fatalf("exposeSharedCodexBundledMarketplaces: %v", err)
			}

			// The key spelling varies per case (bare or quoted), so assert on
			// the repointed value itself, not on a fixed `source = ` prefix.
			wantSource := strconv.Quote(filepath.Join(codexHome, ".tmp", "bundled-marketplaces", "openai-bundled"))
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			if !strings.Contains(string(data), wantSource) {
				t.Fatalf("config does not contain repointed source %s:\n%s", wantSource, data)
			}
			if strings.Contains(string(data), strconv.Quote(sharedSource)) {
				t.Fatalf("config still carries the shared-home source:\n%s", data)
			}
			// The trailing comment after the value must survive the splice.
			if strings.Contains(sourceLine, "#") && !strings.Contains(string(data), "#") {
				t.Fatalf("trailing comment was dropped by the rewrite:\n%s", data)
			}
		})
	}
}

// A task home whose .tmp was swapped for a symlink (a reused home a previous
// task could write) must make the expose step fail loudly instead of
// following the interior link with MkdirAll/RemoveAll/createDirLink.
func TestExposeSharedCodexBundledMarketplacesRefusesSymlinkedTmp(t *testing.T) {
	sharedHome := t.TempDir()
	codexHome := t.TempDir()
	bundledRoot, _ := writeSharedBundledMarketplace(t, sharedHome)
	configPath := writeTaskConfigWithMarketplaces(t, codexHome, bundledRoot, filepath.Join(t.TempDir(), "elsewhere"))

	outside := t.TempDir()
	outsideMarker := filepath.Join(outside, "probe")
	if err := os.WriteFile(outsideMarker, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	// Swap the .tmp component itself for a link that points outside the task
	// home. A symlinked leaf is safe (Remove only ever unlinks it), but the
	// interior component would make MkdirAll/RemoveAll/createDirLink operate
	// outside the home, so the expose step must refuse it outright.
	if err := os.Symlink(outside, filepath.Join(codexHome, ".tmp")); err != nil {
		t.Fatalf("plant symlinked .tmp: %v", err)
	}

	if err := exposeSharedCodexBundledMarketplaces(codexHome, sharedHome); err == nil {
		t.Fatalf("expected the symlinked interior component to be refused")
	}
	if _, err := os.Stat(outsideMarker); err != nil {
		t.Fatalf("outside probe vanished — the interior link was followed: %v", err)
	}
	// The task config must be untouched by the refused attempt.
	if data, err := os.ReadFile(configPath); err != nil || !strings.Contains(string(data), "openai-bundled") {
		t.Fatalf("config changed by refused expose: %v %s", err, data)
	}
}

// A codexHome that is itself a symlink must be refused, not written through.
func TestExposeSharedCodexBundledMarketplacesRefusesSymlinkedHome(t *testing.T) {
	sharedHome := t.TempDir()
	writeSharedBundledMarketplace(t, sharedHome)
	realHome := t.TempDir()
	linkedHome := filepath.Join(t.TempDir(), "linked-home")
	if err := os.Symlink(realHome, linkedHome); err != nil {
		t.Fatalf("create linked home: %v", err)
	}

	if err := exposeSharedCodexBundledMarketplaces(linkedHome, sharedHome); err == nil {
		t.Fatalf("expected a symlinked codex home to be refused")
	}
}

// TestExposeSharedCodexBundledMarketplacesStructuralVariants covers the
// marketplace tables spelled with top-level dotted
// keys, inline tables, and multi-line literal values must all be repointed,
// and a multi-line MCP env string that happens to contain `source = "..."`
// text must survive byte-for-byte.
func TestExposeSharedCodexBundledMarketplacesStructuralVariants(t *testing.T) {
	for _, tc := range []struct {
		name, configFormat string
	}{
		{"dotted_keys", "marketplaces.openai-bundled.source_type = \"local\"\nmarketplaces.openai-bundled.source = %q\n"},
		{"inline_table", "[marketplaces]\nopenai-bundled = { source_type = \"local\", source = %q }\n"},
		{"multiline_literal", "[marketplaces.openai-bundled]\nsource_type = \"local\"\nsource = '''\n%s\n'''\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sharedHome := t.TempDir()
			codexHome := t.TempDir()
			bundledRoot, _ := writeSharedBundledMarketplace(t, sharedHome)
			sharedSource := filepath.Join(bundledRoot, "openai-bundled")

			config := fmt.Sprintf(tc.configFormat, sharedSource)
			configPath := filepath.Join(codexHome, "config.toml")
			if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			if err := exposeSharedCodexBundledMarketplaces(codexHome, sharedHome); err != nil {
				t.Fatalf("exposeSharedCodexBundledMarketplaces: %v", err)
			}

			wantSource := strconv.Quote(filepath.Join(codexHome, ".tmp", "bundled-marketplaces", "openai-bundled"))
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			if !strings.Contains(string(data), wantSource) {
				t.Fatalf("config does not contain repointed source %s:\n%s", wantSource, data)
			}
			if strings.Contains(string(data), strconv.Quote(sharedSource)) {
				t.Fatalf("config still carries the shared-home source:\n%s", data)
			}
		})
	}
}

// The rewrite must only touch marketplace source values. A multi-line MCP env
// string whose CONTENT lines read `source = "<shared path>"` is one string
// AST node under mcp_servers — its bytes must survive untouched even though
// its text matches the stale source line-by-line.
func TestRewriteSparesNonMarketplaceMultilineValues(t *testing.T) {
	sharedHome := t.TempDir()
	codexHome := t.TempDir()
	bundledRoot, _ := writeSharedBundledMarketplace(t, sharedHome)
	sharedSource := filepath.Join(bundledRoot, "openai-bundled")

	config := `[marketplaces.openai-bundled]
source_type = "local"
source = "` + sharedSource + `"

[mcp_servers.rewrite_probe_fixture]
command = "/usr/bin/true"
[mcp_servers.rewrite_probe_fixture.env]
REWRITE_PROBE_SENTINEL = '''
source = "` + sharedSource + `"
'''
`
	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := rewriteSharedBundledMarketplaceSources(configPath, bundledRoot, filepath.Join(codexHome, ".tmp", "bundled-marketplaces")); err != nil {
		t.Fatalf("rewriteSharedBundledMarketplaceSources: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "source = "+strconv.Quote(filepath.Join(codexHome, ".tmp", "bundled-marketplaces", "openai-bundled"))) {
		t.Fatalf("marketplace source was not repointed:\n%s", got)
	}
	if !strings.Contains(got, "source = "+strconv.Quote(sharedSource)) {
		t.Fatalf("unrelated MCP env content was altered:\n%s", got)
	}
	if strings.Count(got, strconv.Quote(sharedSource)) != 1 {
		t.Fatalf("expected exactly one untouched shared-source occurrence (the sentinel):\n%s", got)
	}
}

// A nested sub-table under a marketplace (marketplaces.<name>.metadata.source)
// is unrelated content that happens to share the root — only the exact
// three-segment marketplace source field is repointed.
func TestRewriteSparesNestedMarketplaceSubtableSource(t *testing.T) {
	sharedHome := t.TempDir()
	codexHome := t.TempDir()
	bundledRoot, _ := writeSharedBundledMarketplace(t, sharedHome)
	sharedSource := filepath.Join(bundledRoot, "openai-bundled")
	artwork := filepath.Join(bundledRoot, "artwork")
	if err := os.MkdirAll(artwork, 0o755); err != nil {
		t.Fatalf("create artwork dir: %v", err)
	}

	config := `[marketplaces.openai-bundled]
source_type = "local"
source = "` + sharedSource + `"

[marketplaces.openai-bundled.metadata]
source = "` + artwork + `"
`
	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := rewriteSharedBundledMarketplaceSources(configPath, bundledRoot, filepath.Join(codexHome, ".tmp", "bundled-marketplaces")); err != nil {
		t.Fatalf("rewriteSharedBundledMarketplaceSources: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "source = "+strconv.Quote(filepath.Join(codexHome, ".tmp", "bundled-marketplaces", "openai-bundled"))) {
		t.Fatalf("marketplace source was not repointed:\n%s", got)
	}
	if !strings.Contains(got, "source = "+strconv.Quote(artwork)) {
		t.Fatalf("nested metadata source must keep its shared path:\n%s", got)
	}
	if strings.Count(got, strconv.Quote(artwork)) != 1 {
		t.Fatalf("nested source must appear exactly once, unmodified:\n%s", got)
	}
}
