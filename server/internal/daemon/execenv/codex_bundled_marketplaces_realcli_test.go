//go:build agentintegration

package execenv

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: the marketplace-source rewrite must handle every TOML spelling
// Codex itself accepts — trailing comments on the header and on the source
// line, quoted keys, top-level dotted keys, inline tables, multi-line string
// values, and nested/unrelated `source` fields that must survive untouched.
// This
// drives the REAL Codex CLI (executes a user-installed agent binary), so it
// stays behind the agentintegration tag and the real-agent smoke gate:
//
//	(cd server && MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./internal/daemon/execenv -run TestCodexBundledMarketplaceSourceRewriteRealCLI -count=1 -v)
//
// The default-test assertions for the same spellings live in
// codex_bundled_marketplaces_test.go (TestExposeSharedCodexBundledMarketplacesQASpellingVariants).

func TestCodexBundledMarketplaceSourceRewriteRealCLI(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("real-agent smoke test; set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run against the installed Codex CLI")
	}
	codex, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name, configFmt, preservedFmt string
	}{
		{"canonical", "[marketplaces.openai-bundled]\nsource_type = \"local\"\nsource = %q\n", ""},
		{"source_comment", "[marketplaces.openai-bundled]\nsource_type = \"local\"\nsource = %q # Desktop bundle\n", ""},
		{"header_comment", "[marketplaces.openai-bundled] # Desktop bundle\nsource_type = \"local\"\nsource = %q\n", ""},
		{"quoted_source_key", "[marketplaces.openai-bundled]\nsource_type = \"local\"\n\"source\" = %q\n", ""},
		{"dotted_keys", "marketplaces.openai-bundled.source_type = \"local\"\nmarketplaces.openai-bundled.source = %q\n", ""},
		{"inline_table", "[marketplaces]\nopenai-bundled = { source_type = \"local\", source = %q }\n", ""},
		{"multiline_literal", "[marketplaces.openai-bundled]\nsource_type = \"local\"\nsource = '''\n%s'''\n", ""},
		{"unrelated_multiline_value", "[marketplaces.openai-bundled]\nsource_type = \"local\"\nsource = %q\n", "[mcp_servers.rewrite_probe_fixture]\ncommand = \"/usr/bin/true\"\n[mcp_servers.rewrite_probe_fixture.env]\nREWRITE_PROBE_SENTINEL = '''\nsource = %q\n'''\n"},
		{"nested_source_preserved", "[marketplaces.openai-bundled]\nsource_type = \"local\"\nsource = %q\n", "[marketplaces.openai-bundled.metadata]\nsource = %q\n"},
		{"external_local_marketplace", "[marketplaces.openai-bundled]\nsource_type = \"local\"\nsource = %q\n", "[marketplaces.rewrite-independent]\nsource_type = \"local\"\nsource = %q\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			shared := filepath.Join(root, "shared")
			bundled := filepath.Join(shared, ".tmp", "bundled-marketplaces", "openai-bundled")
			writeQAFile(t, filepath.Join(bundled, ".agents", "plugins", "marketplace.json"),
				`{"name":"openai-bundled","plugins":[{"name":"browser","source":{"source":"local","path":"./plugins/browser"}}]}`)
			writeQAFile(t, filepath.Join(bundled, "plugins", "browser", ".codex-plugin", "plugin.json"),
				`{"name":"browser","version":"1.0.0","description":"marketplace discovery fixture"}`)
			config := fmt.Sprintf(tc.configFmt, bundled)
			external := filepath.Join(root, "external")
			if tc.name == "external_local_marketplace" {
				writeQAFile(t, filepath.Join(external, ".agents", "plugins", "marketplace.json"),
					`{"name":"rewrite-independent","plugins":[]}`)
			}

			preserved := ""
			if tc.preservedFmt != "" {
				preserved = fmt.Sprintf(tc.preservedFmt, bundled)
				if tc.name == "nested_source_preserved" {
					metadata := filepath.Join(filepath.Dir(bundled), "artwork")
					if err := os.MkdirAll(metadata, 0o755); err != nil {
						t.Fatal(err)
					}
					preserved = fmt.Sprintf(tc.preservedFmt, metadata)
				}
				if tc.name == "external_local_marketplace" {
					preserved = fmt.Sprintf(tc.preservedFmt, external)
				}
				config += "\n" + preserved
			}
			writeQAFile(t, filepath.Join(shared, "config.toml"), config)
			list := func(home string) (bool, string) {
				t.Helper()
				cmd := exec.Command(codex, "plugin", "marketplace", "list")
				cmd.Dir = filepath.Dir(home)
				cmd.Env = append(os.Environ(), "CODEX_HOME="+home)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("Codex CLI failed: %v\n%s", err, out)
				}
				return strings.Contains(string(out), "openai-bundled"), strings.ReplaceAll(strings.TrimSpace(string(out)), filepath.Dir(home), "<fixture>")
			}
			sharedFound, sharedOutput := list(shared)
			if !sharedFound {
				t.Fatalf("control failed, valid config at shared home should expose bundle: %s", sharedOutput)
			}
			t.Setenv("CODEX_HOME", shared)
			task := filepath.Join(filepath.Dir(shared), "task")
			opts := CodexHomeOptions{GOOS: "darwin", CodexVersion: "0.169.1"}
			for attempt := 1; attempt <= 2; attempt++ {
				if err := prepareCodexHomeWithOpts(task, opts, logger); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(task, ".tmp", "bundled-marketplaces")
				_, linkErr := os.Readlink(link)
				taskConfig, err := os.ReadFile(filepath.Join(task, "config.toml"))
				if err != nil {
					t.Fatal(err)
				}
				found, output := list(task)
				t.Logf("prepare %d: task link exists=%t, source points into task=%t, openai-bundled discovered=%t, CLI=%s",
					attempt, linkErr == nil, strings.Contains(string(taskConfig), link), found, output)
				if preserved != "" && !strings.Contains(string(taskConfig), preserved) {
					marker := "[marketplaces.openai-bundled.metadata]\n"
					_, rest, _ := strings.Cut(string(taskConfig), marker)
					actual, _, _ := strings.Cut(rest, "\n")
					t.Errorf("prepare %d: unrelated TOML content changed; expected %s; got %s",
						attempt,
						strings.ReplaceAll(strings.TrimSpace(strings.TrimPrefix(preserved, marker)), root, "<fixture>"),
						strings.ReplaceAll(actual, root, "<fixture>"))
				}
				if tc.name == "external_local_marketplace" && !strings.Contains(output, "rewrite-independent") {
					t.Errorf("independent marketplace disappeared: %s", output)
				}
				if !found {
					t.Errorf("expected the task to discover the same bundled marketplace that Codex accepted in the shared home")
				}
			}
			after, err := os.ReadFile(filepath.Join(shared, "config.toml"))
			if err != nil || string(after) != config {
				t.Fatalf("shared config changed during task preparation: %v", err)
			}
		})
	}
}

func writeQAFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
