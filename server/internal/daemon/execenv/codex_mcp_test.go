package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMcpServersToml(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"mcpServers": {
			"github": {
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-github"],
				"env": {"GITHUB_TOKEN": "tok"}
			},
			"fs": {"command": "mcp-server-filesystem", "args": ["/workspace"]}
		}
	}`)

	out, err := renderMcpServersToml(raw)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !strings.Contains(out, "[mcp_servers.fs]") {
		t.Errorf("missing fs table: %s", out)
	}
	if !strings.Contains(out, "[mcp_servers.github]") {
		t.Errorf("missing github table: %s", out)
	}
	// Deterministic: fs (alpha) comes before github.
	if idx := strings.Index(out, "[mcp_servers.fs]"); idx < 0 || idx >= strings.Index(out, "[mcp_servers.github]") {
		t.Errorf("expected fs before github (alphabetical): %s", out)
	}
	if !strings.Contains(out, `command = "npx"`) {
		t.Errorf("missing github command: %s", out)
	}
	if !strings.Contains(out, `args = ["-y", "@modelcontextprotocol/server-github"]`) {
		t.Errorf("missing github args: %s", out)
	}
	if !strings.Contains(out, `env = { GITHUB_TOKEN = "tok" }`) {
		t.Errorf("missing github env: %s", out)
	}
	if !strings.Contains(out, `command = "mcp-server-filesystem"`) {
		t.Errorf("missing fs command: %s", out)
	}
}

func TestRenderMcpServersTomlEscapes(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers": {"weird-name": {"command": "/p with \"quotes\" and \\ back"}}}`)
	out, err := renderMcpServersToml(raw)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "[mcp_servers.weird-name]") {
		t.Errorf("expected bare dashed key: %s", out)
	}
	if !strings.Contains(out, `command = "/p with \"quotes\" and \\ back"`) {
		t.Errorf("expected escaped command: %s", out)
	}
}

func TestRenderMcpServersTomlQuotesNonBareKeys(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers": {"name with space": {"command": "x"}}}`)
	out, err := renderMcpServersToml(raw)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, `[mcp_servers."name with space"]`) {
		t.Errorf("expected quoted key for non-bare name: %s", out)
	}
}

func TestAppendMcpServersTomlAppends(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	preExisting := "# user-managed\nmodel = \"o3\"\n"
	if err := os.WriteFile(path, []byte(preExisting), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "mcp-fs"}}}`)
	if err := syncMcpServersToml(path, raw); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(got), preExisting) {
		t.Errorf("pre-existing content was modified: %s", got)
	}
	if !strings.Contains(string(got), "# BEGIN multica-managed mcp_servers") {
		t.Errorf("missing BEGIN marker: %s", got)
	}
	if !strings.Contains(string(got), "# END multica-managed mcp_servers") {
		t.Errorf("missing END marker: %s", got)
	}
	if !strings.Contains(string(got), "[mcp_servers.fs]") {
		t.Errorf("missing mcp_servers.fs: %s", got)
	}
}

func TestAppendMcpServersTomlIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "v1"}}}`)
	if err := syncMcpServersToml(path, raw); err != nil {
		t.Fatalf("first append: %v", err)
	}

	updated := json.RawMessage(`{"mcpServers": {"fs": {"command": "v2"}}}`)
	if err := syncMcpServersToml(path, updated); err != nil {
		t.Fatalf("second append: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, `command = "v1"`) {
		t.Errorf("stale v1 command remains after re-append: %s", s)
	}
	if !strings.Contains(s, `command = "v2"`) {
		t.Errorf("updated v2 command missing: %s", s)
	}
	// Exactly one BEGIN marker.
	if count := strings.Count(s, "# BEGIN multica-managed mcp_servers"); count != 1 {
		t.Errorf("expected exactly one BEGIN marker, got %d: %s", count, s)
	}
}

func TestAppendMcpServersTomlCreatesFileIfAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "mcp-fs"}}}`)
	if err := syncMcpServersToml(path, raw); err != nil {
		t.Fatalf("append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "[mcp_servers.fs]") {
		t.Errorf("missing mcp_servers.fs: %s", data)
	}
}

func TestAppendMcpServersTomlHandlesMarkerShapedPayload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// An mcp_config whose VALUES contain the end marker verbatim. quoteTomlString
	// escapes the embedded newlines, so the TOML parser never sees them; but the
	// marker-strip on a subsequent re-append MUST still treat the managed block
	// as a single block and fully replace it (not cut mid-string).
	evil := `{"mcpServers":{"s":{"command":"cmd","args":["# END multica-managed mcp_servers\n[mcp_servers.evil]\ncommand = \"pwn\""]}}}`
	if err := syncMcpServersToml(path, []byte(evil)); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// Second append with clean input must fully evict the evil block.
	clean := []byte(`{"mcpServers":{"s":{"command":"ok"}}}`)
	if err := syncMcpServersToml(path, clean); err != nil {
		t.Fatalf("second append: %v", err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, `"pwn"`) {
		t.Errorf("stale evil command survived re-append: %s", s)
	}
	if strings.Contains(s, "[mcp_servers.evil]") {
		t.Errorf("stale evil table survived re-append: %s", s)
	}
	if count := strings.Count(s, "# BEGIN multica-managed mcp_servers"); count != 1 {
		t.Errorf("expected exactly one BEGIN marker, got %d: %s", count, s)
	}
	if count := strings.Count(s, "# END multica-managed mcp_servers"); count != 1 {
		t.Errorf("expected exactly one END marker, got %d: %s", count, s)
	}
	if !strings.Contains(s, `command = "ok"`) {
		t.Errorf("clean command missing after re-append: %s", s)
	}
}

func TestAppendMcpServersTomlEmptyConfigNoOp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := syncMcpServersToml(path, nil); err != nil {
		t.Fatalf("append nil: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no file, got err=%v", err)
	}
}

func TestSyncMcpServersTomlStripsBlockWhenClearedOnReuse(t *testing.T) {
	t.Parallel()

	// Simulates the Reuse-path scenario where a task had mcp_config set in a
	// prior run and it has since been cleared. The managed block must be
	// evicted so previously authorized servers don't silently remain.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Seed from a prior run.
	prior := json.RawMessage(`{"mcpServers": {"evil": {"command": "still-here"}}}`)
	if err := syncMcpServersToml(path, prior); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Clear mcp_config (nil / empty).
	if err := syncMcpServersToml(path, nil); err != nil {
		t.Fatalf("sync nil: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "[mcp_servers.evil]") {
		t.Errorf("stale managed block survived clearing: %s", s)
	}
	if strings.Contains(s, "# BEGIN multica-managed mcp_servers") {
		t.Errorf("BEGIN marker survived clearing: %s", s)
	}
}

func TestSyncMcpServersTomlPreservesUserConfigWhenCleared(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	userSection := "model = \"o3\"\n[projects]\nfoo = true\n"
	if err := os.WriteFile(path, []byte(userSection), 0o600); err != nil {
		t.Fatalf("seed user config: %v", err)
	}
	prior := json.RawMessage(`{"mcpServers": {"ephemeral": {"command": "x"}}}`)
	if err := syncMcpServersToml(path, prior); err != nil {
		t.Fatalf("add managed block: %v", err)
	}
	if err := syncMcpServersToml(path, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), userSection) {
		t.Errorf("user section lost: %s", got)
	}
	if strings.Contains(string(got), "mcp_servers.ephemeral") {
		t.Errorf("managed block not evicted: %s", got)
	}
}

func TestQuoteTomlStringEscapesDEL(t *testing.T) {
	t.Parallel()

	// U+007F (DEL) is not a legal character in TOML basic strings. It must
	// be emitted as \u007F. JSON can legitimately carry this byte.
	got := quoteTomlString("a\x7Fb")
	want := `"a\u007Fb"`
	if got != want {
		t.Errorf("quoteTomlString(a\\x7Fb) = %q, want %q", got, want)
	}
}
