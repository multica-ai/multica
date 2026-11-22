package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestListRuntimeLocalMcpServersCodexRedactsDetails(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("CODEX_HOME", "")
	configDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `[mcp_servers.fetch]
command = "uvx"
args = ["mcp-server-fetch", "--token", "secret"]

[mcp_servers.docs]
url = "https://secret.example/mcp"
enabled = false
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, supported, err := listRuntimeLocalMcpServers("codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 2 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
	if servers[0].Name != "docs" || servers[0].Transport != "http" || servers[0].Enabled {
		t.Fatalf("docs summary = %#v", servers[0])
	}
	if servers[1].Name != "fetch" || servers[1].Transport != "stdio" || !servers[1].Enabled {
		t.Fatalf("fetch summary = %#v", servers[1])
	}
}

func TestListRuntimeLocalMcpServersClaudeMissingConfig(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	servers, supported, err := listRuntimeLocalMcpServers("claude", "")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 0 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
}

func TestListRuntimeLocalMcpServersOmpReadsNativeConfig(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	configDir := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"mcpServers":{"native":{"command":"native-server","enabled":false}}}`
	if err := os.WriteFile(filepath.Join(configDir, "mcp.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, supported, err := listRuntimeLocalMcpServers("omp", "")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 1 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
	if servers[0].Name != "native" || servers[0].Transport != "stdio" || servers[0].Enabled {
		t.Fatalf("native summary = %#v", servers[0])
	}
}

func TestListRuntimeLocalMcpServersClaudeEnabledPlugin(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	installPath := writeTestClaudePlugin(t, home, "paper-desktop@paper", "paper-desktop", true)
	config := `{"mcpServers":{"paper":{"type":"http","url":"http://127.0.0.1:29979/mcp"}}}`
	if err := os.WriteFile(filepath.Join(installPath, "mcp.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, supported, err := listRuntimeLocalMcpServers("claude", "")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 1 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
	if servers[0].Name != "paper" || servers[0].Transport != "http" || !servers[0].Enabled {
		t.Fatalf("plugin MCP summary = %#v", servers[0])
	}
	if servers[0].Source != "Claude Plugin · paper-desktop" {
		t.Fatalf("plugin MCP source = %q", servers[0].Source)
	}
}

func TestListRuntimeLocalMcpServersUnknownProvider(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	servers, supported, err := listRuntimeLocalMcpServers("future-runtime", "")
	if err != nil {
		t.Fatal(err)
	}
	if supported || len(servers) != 0 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
}

func TestListRuntimeLocalMcpServersPi(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"mcpServers":{"native":{"command":"native-server","enabled":false}}}`
	if err := os.WriteFile(filepath.Join(configDir, "mcp.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, supported, err := listRuntimeLocalMcpServers("pi", "")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 1 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
	if servers[0].Name != "native" || servers[0].Transport != "stdio" || servers[0].Enabled {
		t.Fatalf("native summary = %#v", servers[0])
	}
}

func TestMergeRuntimeAndAgentMcpConfigPiLeavesUserFileAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "mcp.json"), []byte(`{"mcpServers":{"runtime-only":{"command":"runtime-server"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	merged, err := mergeRuntimeAndAgentMcpConfig("pi", "", json.RawMessage(`{"mcpServers":{"agent":{"command":"agent-server"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document.McpServers["runtime-only"]; ok {
		t.Fatalf("user server was copied into the task config: %#v", document.McpServers)
	}
	if document.McpServers["agent"]["command"] != "agent-server" {
		t.Fatalf("merged servers = %#v", document.McpServers)
	}
}

func TestMergeRuntimeAndAgentMcpConfigOmpUsesAgentConfig(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	merged, err := mergeRuntimeAndAgentMcpConfig("omp", "", json.RawMessage(`{"mcpServers":{"agent":{"command":"agent-server"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.McpServers) != 1 || document.McpServers["agent"]["command"] != "agent-server" {
		t.Fatalf("merged servers = %#v", document.McpServers)
	}
}

func TestMergeRuntimeAndAgentMcpConfigClaudeCombinesAndAgentWins(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	runtimeConfig := `{"mcpServers":{"runtime-only":{"command":"runtime-cmd","env":{"TOKEN":"local-secret"}},"shared":{"command":"runtime-shared"}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(runtimeConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	installPath := writeTestClaudePlugin(t, home, "paper-desktop@paper", "paper-desktop", true)
	if err := os.WriteFile(filepath.Join(installPath, "mcp.json"), []byte(`{"mcpServers":{"paper":{"type":"http","url":"http://127.0.0.1:29979/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	merged, err := mergeRuntimeAndAgentMcpConfig("claude", "", json.RawMessage(`{"mcpServers":{"shared":{"command":"agent-shared"},"agent-only":{"url":"https://agent.example/mcp"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.McpServers) != 4 {
		t.Fatalf("merged servers = %#v", document.McpServers)
	}
	if got := document.McpServers["shared"]["command"]; got != "agent-shared" {
		t.Fatalf("shared command = %#v, want agent-shared", got)
	}
	if got := document.McpServers["runtime-only"]["command"]; got != "runtime-cmd" {
		t.Fatalf("runtime-only command = %#v", got)
	}
	if got := document.McpServers["runtime-only"]["env"].(map[string]any)["TOKEN"]; got != "local-secret" {
		t.Fatalf("runtime secret was not preserved locally: %#v", got)
	}
	if got := document.McpServers["paper"]["url"]; got != "http://127.0.0.1:29979/mcp" {
		t.Fatalf("plugin server url = %#v", got)
	}
}

func TestMergeRuntimeAndAgentMcpConfigCodexNormalizesHeaders(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("CODEX_HOME", "")
	configDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `[mcp_servers.docs]
url = "https://runtime.example/mcp"
http_headers = { Authorization = "Bearer local-secret" }

[mcp_servers.fetch]
command = "uvx"
args = ["mcp-server-fetch"]
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	merged, err := mergeRuntimeAndAgentMcpConfig("codex", "", json.RawMessage(`{"mcpServers":{"agent":{"command":"node","args":["agent.js"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.McpServers) != 3 {
		t.Fatalf("merged servers = %#v", document.McpServers)
	}
	if got := document.McpServers["docs"]["type"]; got != "http" {
		t.Fatalf("docs type = %#v", got)
	}
	headers := document.McpServers["docs"]["headers"].(map[string]any)
	if got := headers["Authorization"]; got != "Bearer local-secret" {
		t.Fatalf("docs Authorization = %#v", got)
	}
}

func TestMergeRuntimeAndAgentMcpConfigNullKeepsNativeInheritance(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage(" null ")} {
		merged, err := mergeRuntimeAndAgentMcpConfig("claude", "", raw)
		if err != nil {
			t.Fatal(err)
		}
		if string(merged) != string(raw) {
			t.Fatalf("merged %q = %q", string(raw), string(merged))
		}
	}
}

func TestCodeArtsMcpConfigLoadsJSONCAndAgentWins(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("USERPROFILE", home)
	configDir := filepath.Join(home, ".codeartsdoer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{
  // CodeArts uses the OpenCode-compatible top-level key.
  "mcp": {
    "runtime-only": {"command": "runtime-cmd"},
    "shared": {"url": "https://runtime.example/mcp"},
  },
}`
	if err := os.WriteFile(filepath.Join(configDir, "codearts_cli.jsonc"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	merged, err := mergeRuntimeAndAgentMcpConfig("codearts", "", json.RawMessage(`{"mcp":{"shared":{"command":"agent-shared"},"agent-only":{"command":"agent-cmd"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.McpServers) != 3 {
		t.Fatalf("merged servers = %#v", document.McpServers)
	}
	if got := document.McpServers["shared"]["command"]; got != "agent-shared" {
		t.Fatalf("shared command = %#v, want agent-shared", got)
	}

	servers, supported, err := listRuntimeLocalMcpServers("codearts", "")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 2 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
}

func TestCodeArtsMcpConfigPrefersJSONOverJSONC(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".codeartsdoer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(configDir, "codearts_cli.json")
	jsoncPath := filepath.Join(configDir, "codearts_cli.jsonc")
	if err := os.WriteFile(jsoncPath, []byte(`{"mcp":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := codeArtsUserConfigPath(home); got != jsoncPath {
		t.Fatalf("JSONC-only path = %q, want %q", got, jsoncPath)
	}
	if err := os.WriteFile(jsonPath, []byte(`{"mcp":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := codeArtsUserConfigPath(home); got != jsonPath {
		t.Fatalf("JSON path = %q, want %q", got, jsonPath)
	}
}

// CodeBuddy resolves its own config: `$CODEBUDDY_CONFIG_DIR` (default
// `~/.codebuddy`), user-scope MCP from the first existing of
// `<configDir>/.mcp.json` -> `<configDir>/mcp.json` -> `~/.codebuddy.json`,
// parsed as JSONC. `~/.claude.json` is never consulted (MUL-5846).
func TestCodebuddyUserMcpConfigPathPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEBUDDY_CONFIG_DIR", "")
	configDir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path string) {
		if err := os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Nothing on disk: the caller must still get a stable path so the read
	// fails with ErrNotExist rather than the provider looking unsupported.
	if got, want := codebuddyUserMcpConfigPath(home), filepath.Join(configDir, ".mcp.json"); got != want {
		t.Fatalf("no files: got %q, want %q", got, want)
	}

	legacy := filepath.Join(home, ".codebuddy.json")
	write(legacy)
	if got := codebuddyUserMcpConfigPath(home); got != legacy {
		t.Fatalf("legacy only: got %q, want %q", got, legacy)
	}

	plain := filepath.Join(configDir, "mcp.json")
	write(plain)
	if got := codebuddyUserMcpConfigPath(home); got != plain {
		t.Fatalf("mcp.json must win over ~/.codebuddy.json: got %q", got)
	}

	dotted := filepath.Join(configDir, ".mcp.json")
	write(dotted)
	if got := codebuddyUserMcpConfigPath(home); got != dotted {
		t.Fatalf(".mcp.json must win over mcp.json: got %q", got)
	}
}

func TestCodebuddyUserMcpConfigPathHonorsConfigDirEnv(t *testing.T) {
	home := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("CODEBUDDY_CONFIG_DIR", configDir)
	want := filepath.Join(configDir, ".mcp.json")
	if err := os.WriteFile(want, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := codebuddyUserMcpConfigPath(home); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestListRuntimeLocalMcpServersCodebuddyReadsItsOwnConfig(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("CODEBUDDY_CONFIG_DIR", "")
	configDir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers":{"claude-only":{"command":"claude-cmd"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// JSONC: a comment and a trailing comma must not lose the server.
	jsonc := "{\n  // user scope\n  \"mcpServers\": {\n    \"buddy\": { \"command\": \"buddy-cmd\", },\n  },\n}\n"
	if err := os.WriteFile(filepath.Join(configDir, ".mcp.json"), []byte(jsonc), 0o600); err != nil {
		t.Fatal(err)
	}
	// A Claude plugin server must not be attributed to CodeBuddy either:
	// CodeBuddy plugins live under <configDir>/plugins, not ~/.claude/plugins.
	installPath := writeTestClaudePlugin(t, home, "paper-desktop@paper", "paper-desktop", true)
	if err := os.WriteFile(filepath.Join(installPath, "mcp.json"), []byte(`{"mcpServers":{"paper":{"type":"http","url":"http://127.0.0.1:29979/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, supported, err := listRuntimeLocalMcpServers("codebuddy", "")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 1 || servers[0].Name != "buddy" {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
}

// CodeBuddy loads its own user/project/local scopes on every launch, so the
// daemon must NOT pre-merge them: doing so would duplicate the CLI's own work
// while losing scope precedence and the project-scope approval gate.
func TestMergeRuntimeAndAgentMcpConfigCodebuddyIsPassthrough(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("CODEBUDDY_CONFIG_DIR", "")
	configDir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".mcp.json"), []byte(`{"mcpServers":{"local-only":{"command":"local-cmd"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	agentConfig := json.RawMessage(`{"mcpServers":{"agent-only":{"command":"agent-cmd"}}}`)
	merged, err := mergeRuntimeAndAgentMcpConfig("codebuddy", "", agentConfig)
	if err != nil {
		t.Fatal(err)
	}
	if string(merged) != string(agentConfig) {
		t.Fatalf("codebuddy merge must be a passthrough, got %s", merged)
	}
}

func TestStripJSONC(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]any
	}{
		{"line comment", "{\n// hi\n\"a\":1\n}", map[string]any{"a": float64(1)}},
		{"block comment", "{/* hi */\"a\":1}", map[string]any{"a": float64(1)}},
		{"trailing comma object", `{"a":1,}`, map[string]any{"a": float64(1)}},
		{"trailing comma nested", `{"a":{"b":1,},}`, map[string]any{"a": map[string]any{"b": float64(1)}}},
		{
			"comment-like text inside a string is preserved",
			`{"a":"http://x//y, /* not a comment */"}`,
			map[string]any{"a": "http://x//y, /* not a comment */"},
		},
		{"escaped quote in string", `{"a":"say \"//\" ok"}`, map[string]any{"a": `say "//" ok`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stripped, err := stripJSONC([]byte(tc.in))
			if err != nil {
				t.Fatalf("stripJSONC %q: %v", tc.in, err)
			}
			var got map[string]any
			if err := json.Unmarshal(stripped, &got); err != nil {
				t.Fatalf("unmarshal %q -> %q: %v", tc.in, stripped, err)
			}
			if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

// A trailing comma that is NOT trailing must survive: blanking it would merge
// two array elements into one.
func TestStripJSONCKeepsSeparatorCommas(t *testing.T) {
	stripped, err := stripJSONC([]byte(`["a","b",]`))
	if err != nil {
		t.Fatal(err)
	}
	var got []any
	if err := json.Unmarshal(stripped, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v, want [a b]", got)
	}
}

// Repairing only a genuine trailing comma keeps malformed input malformed —
// a config the CLI would reject must not be silently accepted here.
func TestStripJSONCLeavesMalformedInputInvalid(t *testing.T) {
	for _, in := range []string{`[1,,,]`, `{"a":1,,}`, `[1,,2]`} {
		stripped, err := stripJSONC([]byte(in))
		if err != nil {
			continue
		}
		var got any
		if err := json.Unmarshal(stripped, &got); err == nil {
			t.Fatalf("%q must stay invalid, got %#v", in, got)
		}
	}
}

// Comments and the dropped comma are blanked, never deleted, so a parse-error
// offset still points at the byte the user wrote.
func TestStripJSONCPreservesByteLength(t *testing.T) {
	for _, in := range []string{
		"{\n  // c\n  \"a\": 1,\n}\n",
		`{/* block */"a":1,}`,
		`{"a":"// not a comment"}`,
		`{/**/"a":1}`,
		"{}",
	} {
		stripped, err := stripJSONC([]byte(in))
		if err != nil {
			t.Fatalf("stripJSONC %q: %v", in, err)
		}
		if got, want := len(stripped), len(in); got != want {
			t.Fatalf("%q: length %d, want %d (%q)", in, got, want, stripped)
		}
	}
}

// A file CodeBuddy itself rejects must not produce an inventory listing. The
// CLI answers "No MCP servers configured" for both of these; blanking an
// unterminated comment to EOF would instead have turned the first into valid
// JSON, and letting the opener's `*` double as the closer's would have made
// `/*/` a complete comment.
func TestStripJSONCRejectsUnterminatedBlockComment(t *testing.T) {
	cases := map[string]string{
		"unterminated":              `{"mcpServers":{}} /* oops`,
		"opener not closer":         `/*/ {"mcpServers":{}}`,
		"unterminated after opener": `/*`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if stripped, err := stripJSONC([]byte(in)); err == nil {
				t.Fatalf("%q must fail to parse, got %q", in, stripped)
			}
		})
	}
}

// The same input must reach the caller as a parse error, so the inventory
// surfaces the problem rather than silently reporting zero servers.
func TestListRuntimeLocalMcpServersCodebuddyRejectsUnterminatedComment(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("CODEBUDDY_CONFIG_DIR", "")
	configDir := filepath.Join(home, ".codebuddy")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := `{"mcpServers":{"buddy":{"command":"buddy-cmd"}}} /* oops`
	if err := os.WriteFile(filepath.Join(configDir, ".mcp.json"), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := listRuntimeLocalMcpServers("codebuddy", ""); err == nil {
		t.Fatal("expected a parse error for a config CodeBuddy itself rejects")
	}
}

func TestListRuntimeLocalMcpServersKimi(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("KIMI_CODE_HOME", "")
	kimiHome := filepath.Join(home, ".kimi-code")
	if err := os.MkdirAll(kimiHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kimiHome, "mcp.json"), []byte(`{"mcpServers":{"paper":{"url":"https://paper.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, supported, err := listRuntimeLocalMcpServers("kimi", "")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 1 || servers[0].Name != "paper" {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
}

// Preserve both layouts when the OpenCode CLI version is unavailable. A
// nested config must still list the individual servers, including servers
// whose names are also valid legacy entry fields.
func TestOpencodeNativeMcpServers(t *testing.T) {
	cases := []struct {
		name    string
		cfgJSON string
		wantOK  bool
		want    []string
	}{
		{
			name:    "v2 timeout defaults are not a server",
			cfgJSON: `{"mcp":{"timeout":{"catalog":5000}}}`,
			wantOK:  true,
		},
		{
			name:    "v1 server named timeout is still a server",
			cfgJSON: `{"mcp":{"timeout":{"type":"local","command":["fixture"]}}}`,
			wantOK:  true,
			want:    []string{"timeout"},
		},
		{
			name:    "v2 nested under mcp.servers",
			cfgJSON: `{"mcp":{"servers":{"github":{"type":"remote","url":"https://g.example/mcp"},"grafana":{"type":"local","command":["grafana-mcp"]}}}}`,
			wantOK:  true,
			want:    []string{"github", "grafana"},
		},
		{
			name:    "v1 flat under mcp",
			cfgJSON: `{"mcp":{"github":{"type":"remote","url":"https://g.example/mcp"},"grafana":{"type":"local","command":["grafana-mcp"]}}}`,
			wantOK:  true,
			want:    []string{"github", "grafana"},
		},
		{
			name:    "v1 local server named servers",
			cfgJSON: `{"mcp":{"servers":{"type":"local","command":["local-mcp"]},"github":{"type":"remote","url":"https://g.example/mcp"}}}`,
			wantOK:  true,
			want:    []string{"servers", "github"},
		},
		{
			name:    "v1 remote server named servers",
			cfgJSON: `{"mcp":{"servers":{"type":"remote","url":"https://g.example/mcp"}}}`,
			wantOK:  true,
			want:    []string{"servers"},
		},
		{
			name:    "v1 enabled override named servers",
			cfgJSON: `{"mcp":{"servers":{"enabled":false}}}`,
			wantOK:  true,
			want:    []string{"servers"},
		},
		{
			name:    "v2 server names matching entry fields",
			cfgJSON: `{"mcp":{"servers":{"type":{"type":"local","command":["a"]},"url":{"type":"local","command":["b"]},"command":{"type":"local","command":["c"]},"enabled":{"type":"local","command":["d"]},"disabled":{"type":"local","command":["e"]}}}}`,
			wantOK:  true,
			want:    []string{"type", "url", "command", "enabled", "disabled"},
		},
		{
			name:    "no mcp key",
			cfgJSON: `{"$schema":"https://opencode.ai/config.json"}`,
			wantOK:  false,
			want:    nil,
		},
		{
			name:    "mcp is not a map",
			cfgJSON: `{"mcp":"oops"}`,
			wantOK:  false,
			want:    nil,
		},
		{
			name:    "v2 empty servers",
			cfgJSON: `{"mcp":{"servers":{}}}`,
			wantOK:  true,
			want:    nil,
		},
		{
			name:    "v1 empty mcp",
			cfgJSON: `{"mcp":{}}`,
			wantOK:  true,
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg map[string]any
			if err := json.Unmarshal([]byte(tc.cfgJSON), &cfg); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			got := opencodeMcpServers(cfg, "mcp", false)
			ok := got != nil
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got=%#v)", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d servers %#v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for _, name := range tc.want {
				if _, exists := got[name]; !exists {
					t.Fatalf("server %q missing: %#v", name, got)
				}
			}
		})
	}
}

// Both OpenCode layouts must yield actual server names before task-local merging.
func TestLoadRuntimeMcpServerConfigsOpencode(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   []string
	}{
		{
			name: "v2 nested config parses real server names",
			config: `{"$schema":"https://opencode.ai/config.json","mcp":{"servers":{` +
				`"github":{"type":"remote","url":"https://g.example/mcp","headers":{"Authorization":"{env:GITHUB_MCP_TOKEN}"}},` +
				`"grafana":{"type":"local","command":["grafana-mcp","start"]}}}}`,
			want: []string{"github", "grafana"},
		},
		{
			name:   "v1 flat config still parses",
			config: `{"mcp":{"github":{"type":"remote","url":"https://g.example/mcp"},"grafana":{"type":"local","command":["grafana-mcp","start"]}}}`,
			want:   []string{"github", "grafana"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			redirectTestHome(t, t.TempDir())
			configDir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configDir)
			opencodeDir := filepath.Join(configDir, "opencode")
			if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(opencodeDir, "opencode.json"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}

			servers, supported, err := loadRuntimeMcpServerConfigs("opencode", "")
			if err != nil {
				t.Fatal(err)
			}
			if !supported {
				t.Fatal("opencode reported unsupported")
			}
			if _, misparsed := servers["servers"]; misparsed {
				t.Fatalf("config was mis-parsed as a literal %q server: %#v", "servers", servers)
			}
			if len(servers) != len(tc.want) {
				t.Fatalf("got %d servers, want %d: %#v", len(servers), len(tc.want), servers)
			}
			for _, name := range tc.want {
				if _, ok := servers[name]; !ok {
					t.Fatalf("server %q missing: %#v", name, servers)
				}
			}
		})
	}
}

func TestMergeRuntimeAndAgentMcpConfigOpencodeDisabledServers(t *testing.T) {
	cases := []struct {
		name        string
		agentConfig string
		want        []string
	}{
		{
			name:        "disabled runtime servers never reach the task",
			agentConfig: `{"mcpServers":{"managed":{"command":"managed-mcp"}}}`,
			want:        []string{"local-on", "remote-on", "managed"},
		},
		{
			name:        "explicit agent entry still wins a same-name collision",
			agentConfig: `{"mcpServers":{"local-off":{"command":"managed-override"}}}`,
			want:        []string{"local-on", "remote-on", "local-off"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeOpencodeRuntimeConfig(t, `{"servers":{
				"local-off":{"type":"local","command":["disabled-local"],"disabled":true},
				"remote-off":{"type":"remote","url":"https://disabled.example.invalid/mcp","disabled":true},
				"local-on":{"type":"local","command":["enabled-local"]},
				"remote-on":{"type":"remote","url":"https://enabled.example.invalid/mcp","disabled":false}
			}}`)

			inventory, supported, err := listRuntimeLocalMcpServers("opencode", "2.2.0")
			if err != nil {
				t.Fatal(err)
			}
			if !supported || len(inventory) != 4 {
				t.Fatalf("supported=%v inventory=%#v", supported, inventory)
			}
			for _, server := range inventory {
				wantEnabled := server.Name == "local-on" || server.Name == "remote-on"
				if server.Enabled != wantEnabled {
					t.Errorf("inventory server %q enabled=%v, want %v", server.Name, server.Enabled, wantEnabled)
				}
			}

			merged, err := mergeRuntimeAndAgentMcpConfig("opencode", "2.2.0", json.RawMessage(tc.agentConfig))
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Servers map[string]map[string]any `json:"mcpServers"`
			}
			if err := json.Unmarshal(merged, &document); err != nil {
				t.Fatal(err)
			}
			if len(document.Servers) != len(tc.want) {
				t.Fatalf("task servers=%s, want only %v", merged, tc.want)
			}
			for _, name := range tc.want {
				if _, ok := document.Servers[name]; !ok {
					t.Errorf("task server %q missing: %s", name, merged)
				}
			}
			if entry, ok := document.Servers["local-off"]; ok && entry["command"] != "managed-override" {
				t.Fatalf("agent override lost: %#v", entry)
			}
		})
	}
}

// The same shape detection must hold on the public inventory path that feeds
// the agent capabilities endpoint.
func TestListRuntimeLocalMcpServersOpencode(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   []string
	}{
		{
			name: "v2 nested config lists real server names",
			config: `{"mcp":{"servers":{` +
				`"github":{"type":"remote","url":"https://g.example/mcp","disabled":false},` +
				`"grafana":{"type":"local","command":["grafana-mcp","start"]}}}}`,
			want: []string{"github", "grafana"},
		},
		{
			name:   "v1 flat config lists real server names",
			config: `{"mcp":{"github":{"type":"remote","url":"https://g.example/mcp"},"grafana":{"type":"local","command":["grafana-mcp","start"]}}}`,
			want:   []string{"github", "grafana"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			redirectTestHome(t, t.TempDir())
			configDir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configDir)
			opencodeDir := filepath.Join(configDir, "opencode")
			if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(opencodeDir, "opencode.json"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}

			servers, supported, err := listRuntimeLocalMcpServers("opencode", "")
			if err != nil {
				t.Fatal(err)
			}
			if !supported {
				t.Fatal("opencode reported unsupported")
			}
			if len(servers) != len(tc.want) {
				t.Fatalf("got %d servers, want %d: %#v", len(servers), len(tc.want), servers)
			}
			byName := make(map[string]runtimeLocalMcpServerSummary, len(servers))
			for _, server := range servers {
				byName[server.Name] = server
			}
			for _, name := range tc.want {
				if _, ok := byName[name]; !ok {
					t.Fatalf("server %q missing: %#v", name, servers)
				}
			}
		})
	}
}

// kimi merges <KIMI_CODE_HOME>/mcp.json with the ephemeral `mcpServers` array
// the ACP backend sends in session/new, and a duplicate name spawns the server
// twice. So kimi is inventory-only: the task-local merge must leave the agent's
// config untouched.
func TestMergeRuntimeAndAgentMcpConfigKimiIsPassthrough(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	t.Setenv("KIMI_CODE_HOME", "")
	kimiHome := filepath.Join(home, ".kimi-code")
	if err := os.MkdirAll(kimiHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kimiHome, "mcp.json"), []byte(`{"mcpServers":{"paper":{"url":"https://paper.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	agentConfig := json.RawMessage(`{"mcpServers":{"agent-only":{"command":"agent-cmd"}}}`)
	merged, err := mergeRuntimeAndAgentMcpConfig("kimi", "", agentConfig)
	if err != nil {
		t.Fatal(err)
	}
	if string(merged) != string(agentConfig) {
		t.Fatalf("kimi merge must be a passthrough, got %s", merged)
	}
}

func TestOpenCodeGlobalMCPReadsJSONC(t *testing.T) {
	tests := []struct {
		name           string
		cliVersion     string
		base           string
		overlay        string
		wantURL        string
		wantKey        string
		wantOffInTask  bool
		wantOffEnabled bool
	}{
		{
			name:       "v1 keeps fields from the JSON base",
			cliVersion: "1.19.4",
			base:       `{"mcp":{"keep":{"type":"remote","url":"https://base.example/mcp","headers":{"Test-Key":"fixture"}},"off":{"type":"local","command":["fixture"]}}}`,
			overlay: `{"mcp":{
				// Override the URL and keep the other fields from the base.
				"keep":{"type":"remote","url":"https://overlay.example/mcp"},
				"off":{"enabled":false},
				"added":{"type":"local","command":["fixture"]},
			}}`,
			wantURL: "https://overlay.example/mcp", wantKey: "fixture", wantOffInTask: true,
		},
		{
			name:       "v2 replaces named servers",
			cliVersion: "opencode v2.2.0",
			base:       `{"mcp":{"servers":{"keep":{"type":"remote","url":"https://base.example/mcp","headers":{"Test-Key":"fixture"}},"off":{"type":"local","command":["fixture"]}}}}`,
			overlay: `{"mcp":{"timeout":{"catalog":5000},
				"added":{"type":"local","command":["fixture"]},
				"keep":{"type":"remote","url":"https://legacy.example/mcp"},"servers":{
				// Replace this server without inheriting the old headers.
				"keep":{"type":"remote","url":"https://overlay.example/mcp"},
				"off":{"type":"local","command":["fixture"],"disabled":true},
			}}}`,
			wantURL: "https://overlay.example/mcp",
		},
		{
			name:       "v2 replaces legacy entries and ignores enabled-only overrides",
			cliVersion: "opencode v2.2.0",
			base:       `{"mcp":{"keep":{"type":"remote","url":"https://base.example/mcp","headers":{"Test-Key":"fixture"}},"off":{"type":"local","command":["fixture"]}}}`,
			overlay: `{"mcp":{
				"keep":{"type":"remote","url":"https://overlay.example/mcp"},
				"off":{"enabled":false},
				"ignored":{"enabled":true},
				"added":{"type":"local","command":["fixture"]},
			}}`,
			wantURL: "https://overlay.example/mcp", wantOffInTask: true, wantOffEnabled: true,
		},
		{
			name:       "v2 JSONC alone skips legacy overrides and disables typed legacy servers",
			cliVersion: "2.2.0",
			overlay: `{"mcp":{
				"keep":{"type":"remote","url":"https://overlay.example/mcp"},
				"off":{"type":"local","command":["fixture"],"enabled":false},
				"ignored":{"enabled":false},
				"added":{"type":"local","command":["fixture"]},
			}}`,
			wantURL: "https://overlay.example/mcp",
		},
		{
			name: "unknown version JSONC alone uses legacy rules",
			overlay: `{"mcp":{
				// A standalone global JSONC file.
				"keep":{"type":"remote","url":"https://overlay.example/mcp"},
				"off":{"type":"local","command":["fixture"],"enabled":false},
				"added":{"type":"local","command":["fixture"]},
			}}`,
			wantURL: "https://overlay.example/mcp", wantOffInTask: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			redirectTestHome(t, t.TempDir())
			configHome := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configHome)
			dir := filepath.Join(configHome, "opencode")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for filename, body := range map[string]string{"opencode.json": tc.base, "opencode.jsonc": tc.overlay} {
				if body != "" {
					if err := os.WriteFile(filepath.Join(dir, filename), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			inventory, supported, err := listRuntimeLocalMcpServers("opencode", tc.cliVersion)
			if err != nil || !supported {
				t.Fatalf("list MCP inventory: supported=%v, err=%v", supported, err)
			}
			if len(inventory) != 3 || inventory[0].Name != "added" || inventory[1].Name != "keep" || inventory[2].Name != "off" || inventory[2].Enabled != tc.wantOffEnabled {
				t.Fatalf("inventory did not reflect the JSONC layer: %+v", inventory)
			}

			merged, err := mergeRuntimeAndAgentMcpConfig("opencode", tc.cliVersion, json.RawMessage(`{"mcpServers":{"managed":{"command":"managed-server"}}}`))
			if err != nil {
				t.Fatal(err)
			}
			var taskConfig struct {
				Servers map[string]any `json:"mcpServers"`
			}
			if err := json.Unmarshal(merged, &taskConfig); err != nil {
				t.Fatal(err)
			}
			servers := taskConfig.Servers
			if servers["managed"] == nil || servers["added"] == nil || servers["ignored"] != nil {
				t.Fatalf("unexpected task servers: %+v", servers)
			}
			if _, hasOff := servers["off"]; hasOff != tc.wantOffInTask {
				t.Fatalf("task config contains server \"off\": %v, want %v", hasOff, tc.wantOffInTask)
			}
			keep, ok := servers["keep"].(map[string]any)
			if !ok || keep["url"] != tc.wantURL {
				t.Fatalf("unexpected merged server: %+v", keep)
			}
			headers, _ := keep["headers"].(map[string]any)
			gotKey, _ := headers["Test-Key"].(string)
			if gotKey != tc.wantKey {
				t.Fatalf("header = %q, want %q", gotKey, tc.wantKey)
			}
		})
	}
}
