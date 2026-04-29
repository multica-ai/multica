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
	if err := syncMcpServersToml(path, raw, nil); err != nil {
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
	if err := syncMcpServersToml(path, raw, nil); err != nil {
		t.Fatalf("first append: %v", err)
	}

	updated := json.RawMessage(`{"mcpServers": {"fs": {"command": "v2"}}}`)
	if err := syncMcpServersToml(path, updated, nil); err != nil {
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
	if err := syncMcpServersToml(path, raw, nil); err != nil {
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
	if err := syncMcpServersToml(path, []byte(evil), nil); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// Second append with clean input must fully evict the evil block.
	clean := []byte(`{"mcpServers":{"s":{"command":"ok"}}}`)
	if err := syncMcpServersToml(path, clean, nil); err != nil {
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

	if err := syncMcpServersToml(path, nil, nil); err != nil {
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
	if err := syncMcpServersToml(path, prior, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Clear mcp_config (nil / empty).
	if err := syncMcpServersToml(path, nil, nil); err != nil {
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
	if err := syncMcpServersToml(path, prior, nil); err != nil {
		t.Fatalf("add managed block: %v", err)
	}
	if err := syncMcpServersToml(path, nil, nil); err != nil {
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

func TestSyncMcpServersTomlStripsCollidingSectionHeader(t *testing.T) {
	t.Parallel()

	// User's global config defines [mcp_servers.fs]; agent's mcp_config also
	// renders `fs`. TOML 1.0 rejects duplicate table definitions — merged file
	// must contain only the managed `fs` and preserve unrelated user entries.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	userConfig := `model = "o3"

[mcp_servers.fs]
command = "user-mcp-fs"
args = ["/home/me"]

[mcp_servers.gh]
command = "gh-mcp"
`
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "daemon-mcp-fs", "args": ["/workspace"]}}}`)
	if err := syncMcpServersToml(path, raw, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "user-mcp-fs") {
		t.Errorf("colliding user [mcp_servers.fs] survived: %s", s)
	}
	if !strings.Contains(s, "daemon-mcp-fs") {
		t.Errorf("managed mcp_servers.fs missing: %s", s)
	}
	if !strings.Contains(s, "[mcp_servers.gh]") || !strings.Contains(s, "gh-mcp") {
		t.Errorf("unrelated user [mcp_servers.gh] was stripped: %s", s)
	}
	if !strings.Contains(s, `model = "o3"`) {
		t.Errorf("unrelated user top-level key was stripped: %s", s)
	}
	// Exactly one `[mcp_servers.fs]` header in the output.
	if count := strings.Count(s, "[mcp_servers.fs]"); count != 1 {
		t.Errorf("expected exactly one [mcp_servers.fs] header, got %d: %s", count, s)
	}
}

func TestSyncMcpServersTomlStripsCollidingDottedKey(t *testing.T) {
	t.Parallel()

	// User's global config uses dotted-key form `mcp_servers.fs.command = "..."`.
	// Still collides with a managed `[mcp_servers.fs]` — TOML rejects the mix.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	userConfig := `mcp_servers.fs.command = "user-mcp-fs"
mcp_servers.fs.args = ["/home/me"]
mcp_servers.gh.command = "gh-mcp"

model = "o3"
`
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "daemon-mcp-fs"}}}`)
	if err := syncMcpServersToml(path, raw, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "user-mcp-fs") {
		t.Errorf("colliding mcp_servers.fs dotted key survived: %s", s)
	}
	if !strings.Contains(s, "daemon-mcp-fs") {
		t.Errorf("managed mcp_servers.fs missing: %s", s)
	}
	if !strings.Contains(s, `mcp_servers.gh.command = "gh-mcp"`) {
		t.Errorf("unrelated mcp_servers.gh.command dotted key was stripped: %s", s)
	}
	if !strings.Contains(s, `model = "o3"`) {
		t.Errorf("unrelated user top-level key was stripped: %s", s)
	}
}

func TestSyncMcpServersTomlNoCollisionPreservesUserEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	userConfig := `[mcp_servers.gh]
command = "gh-mcp"
`
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "daemon-mcp-fs"}}}`)
	if err := syncMcpServersToml(path, raw, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "[mcp_servers.gh]") || !strings.Contains(s, "gh-mcp") {
		t.Errorf("user [mcp_servers.gh] was stripped: %s", s)
	}
	if !strings.Contains(s, "[mcp_servers.fs]") || !strings.Contains(s, "daemon-mcp-fs") {
		t.Errorf("managed [mcp_servers.fs] missing: %s", s)
	}
}

func TestSyncMcpServersTomlStripsCollidingQuotedKey(t *testing.T) {
	t.Parallel()

	// User's global config uses the double-quoted form `[mcp_servers."fs"]`
	// for a name that would also parse as bare. Must still collide.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	userConfig := `[mcp_servers."fs"]
command = "user-mcp-fs"
`
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "daemon-mcp-fs"}}}`)
	if err := syncMcpServersToml(path, raw, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "user-mcp-fs") {
		t.Errorf(`colliding [mcp_servers."fs"] survived: %s`, s)
	}
}

func TestSyncMcpServersTomlStripsSilentMergeSubtable(t *testing.T) {
	t.Parallel()

	// Silent-merge regression: user has `[mcp_servers.fs.env]` sub-table for
	// env vars, agent's mcp_config sets only `command` (no inline `env`).
	// Without the family-aware strip, TOML's implicit sub-table rule folds
	// `env.FOO` into the daemon-authorized fs server with no parse error —
	// agent MCP process ends up running with env vars the daemon never
	// authorized. Merged output must NOT contain the user's FOO anywhere.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	userConfig := `[mcp_servers.fs.env]
USER_INJECTED = "leaked-from-user-global"

[mcp_servers.fs]
command = "user-fs"
`
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "daemon-fs"}}}`)
	if err := syncMcpServersToml(path, raw, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "USER_INJECTED") {
		t.Errorf("user-injected env var leaked through: %s", s)
	}
	if strings.Contains(s, "leaked-from-user-global") {
		t.Errorf("user-injected env value leaked through: %s", s)
	}
	if strings.Contains(s, `command = "user-fs"`) {
		t.Errorf("user's colliding fs.command survived: %s", s)
	}
	if !strings.Contains(s, `command = "daemon-fs"`) {
		t.Errorf("managed fs.command missing: %s", s)
	}
}

func TestSyncMcpServersTomlStripsArbitrarySubtable(t *testing.T) {
	t.Parallel()

	// Any sub-table under a blocked name must be stripped, not just `.env` —
	// this keeps the fix future-proof against new sub-keys Codex may accept.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	userConfig := `[mcp_servers.fs.headers]
Authorization = "Bearer user-token"

[mcp_servers.fs.auth.oauth]
client_id = "user-client"

[other_section]
preserved = true
`
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "daemon-fs"}}}`)
	if err := syncMcpServersToml(path, raw, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "Bearer user-token") {
		t.Errorf("user .headers sub-table survived: %s", s)
	}
	if strings.Contains(s, "user-client") {
		t.Errorf("user .auth.oauth nested sub-table survived: %s", s)
	}
	if !strings.Contains(s, "[other_section]") || !strings.Contains(s, "preserved = true") {
		t.Errorf("unrelated [other_section] was stripped: %s", s)
	}
}

func TestSyncMcpServersTomlStripsParentSectionBody(t *testing.T) {
	t.Parallel()

	// User's global config uses `[mcp_servers]` parent section with dotted
	// keys inside: `fs.command = "..."` folds into `mcp_servers.fs.command`
	// at parse time and collides the same way an explicit section would.
	// Inline-table form `fs = { command = "..." }` is the same pattern.
	// Non-blocked first-segment keys (e.g. `gh.command`) must survive.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	userConfig := `[mcp_servers]
fs.command = "user-fs"
fs.args = ["/home/me"]
gh.command = "gh-mcp"
other = { command = "other-mcp" }
`
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "daemon-fs"}}}`)
	if err := syncMcpServersToml(path, raw, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "user-fs") {
		t.Errorf(`colliding fs.command in [mcp_servers] body survived: %s`, s)
	}
	if strings.Contains(s, "/home/me") {
		t.Errorf(`colliding fs.args in [mcp_servers] body survived: %s`, s)
	}
	if !strings.Contains(s, `gh.command = "gh-mcp"`) {
		t.Errorf(`non-colliding gh.command was stripped: %s`, s)
	}
	if !strings.Contains(s, `other = { command = "other-mcp" }`) {
		t.Errorf(`non-colliding inline-table other was stripped: %s`, s)
	}
	if !strings.Contains(s, `command = "daemon-fs"`) {
		t.Errorf(`managed fs.command missing: %s`, s)
	}
}

func TestSyncMcpServersTomlStripsInlineTableInParentSection(t *testing.T) {
	t.Parallel()

	// `[mcp_servers] fs = { command = "..." }` — inline-table form of the
	// parent-section-body case. Must also be stripped.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	userConfig := `[mcp_servers]
fs = { command = "user-fs", args = ["/u"] }
`
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := json.RawMessage(`{"mcpServers": {"fs": {"command": "daemon-fs"}}}`)
	if err := syncMcpServersToml(path, raw, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "user-fs") {
		t.Errorf(`colliding inline-table fs = { ... } survived: %s`, data)
	}
}

func TestRenderMcpServersSkipsNonStdioTransport(t *testing.T) {
	t.Parallel()

	// HTTP/SSE-transport MCP servers carry `url` instead of `command`. Codex
	// config.toml only supports stdio transport, so we skip these entries
	// rather than emit a bare `[mcp_servers.<name>]` table that would be
	// rejected or load with no config.
	raw := json.RawMessage(`{"mcpServers": {
		"remote": {"url": "https://example.com/mcp", "type": "sse"},
		"local": {"command": "mcp-local"}
	}}`)
	names, out, err := renderMcpServersWithNames(raw, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(names) != 1 || names[0] != "local" {
		t.Errorf("expected only [local], got %v", names)
	}
	if strings.Contains(out, "remote") {
		t.Errorf("non-stdio server leaked into output: %s", out)
	}
	if !strings.Contains(out, "[mcp_servers.local]") {
		t.Errorf("stdio server missing from output: %s", out)
	}
}
