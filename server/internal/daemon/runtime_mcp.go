package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// runtimeLocalMcpServerSummary is the intentionally non-secret inventory
// shown in Agent capabilities. Never add command arguments, URLs, headers, or
// environment values here: this payload leaves the user's machine.
type runtimeLocalMcpServerSummary struct {
	Name      string `json:"name"`
	Transport string `json:"transport,omitempty"`
	Source    string `json:"source,omitempty"`
	Enabled   bool   `json:"enabled"`
}

// mergeRuntimeAndAgentMcpConfig builds the task-local MCP configuration used
// when an agent has MCP servers managed by Multica. Runtime servers are the
// base layer and the agent's entries win on a same-name collision. The merge
// happens inside the local daemon so runtime URLs, headers, commands, and env
// values never need to leave the machine.
//
// A nil/null agent config keeps the provider's native inheritance path intact.
// A present config (including an empty mcpServers map) opts into the merged,
// task-local config so adding one managed server no longer disables unrelated
// runtime servers.
func mergeRuntimeAndAgentMcpConfig(provider string, agentConfig json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(agentConfig)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return agentConfig, nil
	}

	runtimeServers, supported, err := loadRuntimeMcpServerConfigs(provider)
	if err != nil {
		return nil, err
	}
	if !supported {
		return agentConfig, nil
	}

	var agentDocument map[string]any
	if err := json.Unmarshal(trimmed, &agentDocument); err != nil {
		return nil, fmt.Errorf("parse agent MCP config: %w", err)
	}
	agentServers := map[string]any{}
	if servers, ok := nestedRuntimeMcpMap(agentDocument, "mcpServers"); ok {
		agentServers = servers
	} else if provider == "opencode" {
		// Older OpenCode agents may store the provider-native top-level `mcp`
		// map. Its individual entries can still flow through the existing
		// OpenCode adapter when placed under the canonical mcpServers envelope.
		if servers, ok := nestedRuntimeMcpMap(agentDocument, "mcp"); ok {
			agentServers = servers
		}
	}

	merged := make(map[string]any, len(runtimeServers)+len(agentServers))
	for name, entry := range runtimeServers {
		merged[name] = entry
	}
	for name, entry := range agentServers {
		merged[name] = entry
	}

	raw, err := json.Marshal(map[string]any{"mcpServers": merged})
	if err != nil {
		return nil, fmt.Errorf("marshal merged MCP config: %w", err)
	}
	return raw, nil
}

// codebuddyUserMcpConfigPath returns the user-scope MCP config file CodeBuddy
// actually reads.
//
// CodeBuddy is a Claude Code fork but resolves its own config, so `~/.claude.json`
// is never consulted. Its config directory is `$CODEBUDDY_CONFIG_DIR`, defaulting
// to `~/.codebuddy`, and the user-scope MCP file is the FIRST of these that
// exists — the list is a fallback chain, not a merge:
//
//	<configDir>/.mcp.json   (what `codebuddy mcp add --scope user` writes)
//	<configDir>/mcp.json
//	~/.codebuddy.json
//
// When none exist the first candidate is returned so the caller's read fails
// with os.ErrNotExist and is treated as "no runtime servers", matching the
// other providers. Verified against CodeBuddy 2.x by writing each file in turn
// and reading back `codebuddy mcp list`.
func codebuddyUserMcpConfigPath(home string) string {
	configDir := strings.TrimSpace(os.Getenv("CODEBUDDY_CONFIG_DIR"))
	if configDir == "" {
		configDir = filepath.Join(home, ".codebuddy")
	}
	candidates := []string{
		filepath.Join(configDir, ".mcp.json"),
		filepath.Join(configDir, "mcp.json"),
		filepath.Join(home, ".codebuddy.json"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return candidates[0]
}

// unmarshalRuntimeMcpConfig decodes one runtime's config file. "jsonc" is JSON
// with comments and trailing commas, which CodeBuddy accepts in its MCP files —
// parsing those as strict JSON would drop every server behind a single `//`.
func unmarshalRuntimeMcpConfig(raw []byte, format string) (map[string]any, error) {
	var cfg map[string]any
	switch format {
	case "toml":
		if err := toml.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse runtime MCP config: %w", err)
		}
	case "jsonc":
		stripped, err := stripJSONC(raw)
		if err != nil {
			return nil, fmt.Errorf("parse runtime MCP config: %w", err)
		}
		if err := json.Unmarshal(stripped, &cfg); err != nil {
			return nil, fmt.Errorf("parse runtime MCP config: %w", err)
		}
	default:
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse runtime MCP config: %w", err)
		}
	}
	return cfg, nil
}

// stripJSONC rewrites JSONC into strict JSON: `//` and `/* */` comments become
// spaces and a single trailing comma before `}` / `]` is dropped. String
// literals are copied verbatim, so a `//` or a comma inside a command argument
// survives.
//
// Output is always the same length as the input — comments and the dropped
// comma are blanked, never deleted — so a parse-error offset still points at
// the byte the user actually wrote.
//
// Only the LAST comma before a closer is blanked, so genuinely malformed input
// stays malformed: `[1,,,]` does not silently become `[1]`. This is a
// pragmatic subset of JSONC covering what CodeBuddy's MCP files use in
// practice; anything it cannot repair is reported as a parse error rather than
// guessed at.
//
// An unterminated `/*` is an error rather than a blank-to-EOF, so the Agent >
// MCP tab cannot list servers out of a file CodeBuddy itself rejects. Verified:
// the CLI reports "No MCP servers configured" for both an unterminated comment
// and the `/*/` near-miss, where the opener's `*` must not double as the
// closer's.
func stripJSONC(raw []byte) ([]byte, error) {
	out := make([]byte, 0, len(raw))
	// Offset into out of the one comma still eligible for removal, or -1.
	// Reset by any value token, so only a genuinely trailing comma is dropped.
	lastComma := -1
	inString := false
	escaped := false

	for i := 0; i < len(raw); i++ {
		c := raw[i]

		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}

		switch {
		case c == '"':
			inString = true
			lastComma = -1
			out = append(out, c)
		case c == '/' && i+1 < len(raw) && raw[i+1] == '/':
			// Blank through end of line, preserving the newline itself.
			for i < len(raw) && raw[i] != '\n' {
				out = append(out, ' ')
				i++
			}
			if i < len(raw) {
				out = append(out, raw[i])
			}
		case c == '/' && i+1 < len(raw) && raw[i+1] == '*':
			// Consume the opener first so `/*/` cannot reuse its own `*` as the
			// closer's, then blank the body, newlines included, so line numbers
			// and the total byte count both survive.
			out = append(out, ' ', ' ')
			i += 2
			closed := false
			for i < len(raw) {
				if raw[i] == '*' && i+1 < len(raw) && raw[i+1] == '/' {
					out = append(out, ' ', ' ')
					i++ // consume '*'; the loop's i++ consumes '/'
					closed = true
					break
				}
				if raw[i] == '\n' {
					out = append(out, '\n')
				} else {
					out = append(out, ' ')
				}
				i++
			}
			if !closed {
				return nil, errors.New("unterminated block comment")
			}
		case c == ',':
			lastComma = len(out)
			out = append(out, c)
		case c == '}' || c == ']':
			if lastComma >= 0 {
				out[lastComma] = ' '
			}
			lastComma = -1
			out = append(out, c)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			out = append(out, c)
		default:
			lastComma = -1
			out = append(out, c)
		}
	}
	return out, nil
}

// loadRuntimeMcpServerConfigs returns full, secret-bearing runtime MCP entries
// for task-local merging. Callers must never send the result to the server or
// logs; the public capabilities endpoint continues to use the redacted summary
// type above.
func loadRuntimeMcpServerConfigs(provider string) (map[string]any, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, fmt.Errorf("resolve user home: %w", err)
	}

	var path, key, format string
	switch provider {
	case "claude":
		path, key, format = filepath.Join(home, ".claude.json"), "mcpServers", "json"
	// codebuddy is deliberately absent. CodeBuddy loads its own user, project
	// and local scopes on every launch (codebuddy.go never passes
	// --strict-mcp-config), and a managed entry already wins a same-name
	// collision, so the daemon pre-merging them would only duplicate what the
	// CLI does natively — while losing the scope precedence and the approval
	// gate that protects project-scope servers.
	case "codex":
		codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if codexHome == "" {
			codexHome = filepath.Join(home, ".codex")
		}
		path, key, format = filepath.Join(codexHome, "config.toml"), "mcp_servers", "toml"
	case "cursor":
		path, key, format = filepath.Join(home, ".cursor", "mcp.json"), "mcpServers", "json"
	case "opencode":
		configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		path, key, format = filepath.Join(configHome, "opencode", "opencode.json"), "mcp", "json"
	case "openclaw":
		path = strings.TrimSpace(os.Getenv("CLAWDBOT_CONFIG_PATH"))
		if path == "" {
			stateDir := strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR"))
			if stateDir == "" {
				stateDir = filepath.Join(home, ".openclaw")
			}
			path = filepath.Join(stateDir, "openclaw.json")
		}
		key, format = "mcp.servers", "json"
	default:
		return map[string]any{}, false, nil
	}

	servers := map[string]any{}
	raw, err := os.ReadFile(path)
	if err == nil {
		cfg, err := unmarshalRuntimeMcpConfig(raw, format)
		if err != nil {
			return nil, true, err
		}
		if configured, ok := nestedRuntimeMcpMap(cfg, key); ok {
			for name, entry := range configured {
				servers[name] = normalizeRuntimeMcpEntry(provider, entry)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, true, fmt.Errorf("read runtime MCP config: %w", err)
	}

	if provider == "claude" {
		// User configuration has the same precedence Claude uses: plugin
		// servers only fill names not already defined by the user.
		for name, entry := range loadClaudePluginMcpServerConfigs(home) {
			if _, exists := servers[name]; !exists {
				servers[name] = entry
			}
		}
	}
	return servers, true, nil
}

func normalizeRuntimeMcpEntry(provider string, value any) any {
	entry, ok := value.(map[string]any)
	if !ok || provider != "codex" {
		return value
	}
	// Multica's canonical remote shape calls these `headers`; Codex stores
	// them as `http_headers`. Keep the original key as well so less common
	// Codex-specific settings round-trip through renderCodexMcpServersBlock.
	if headers, ok := entry["http_headers"]; ok {
		if _, exists := entry["headers"]; !exists {
			entry["headers"] = headers
		}
	}
	if _, hasURL := entry["url"]; hasURL {
		if _, hasType := entry["type"]; !hasType {
			entry["type"] = "http"
		}
	}
	return entry
}

func loadClaudePluginMcpServerConfigs(home string) map[string]any {
	out := map[string]any{}
	for _, plugin := range listEnabledClaudePlugins(home) {
		manifest, _ := readClaudePluginManifest(plugin.InstallPath)
		paths := claudePluginComponentPaths(
			plugin.InstallPath,
			manifest.MCPServers,
			filepath.Join(plugin.InstallPath, ".mcp.json"),
		)
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var cfg map[string]any
			if json.Unmarshal(raw, &cfg) != nil {
				continue
			}
			servers, ok := nestedRuntimeMcpMap(cfg, "mcpServers")
			if !ok {
				continue
			}
			for name, entry := range servers {
				if _, exists := out[name]; !exists {
					out[name] = entry
				}
			}
		}
	}
	return out
}

func listRuntimeLocalMcpServers(provider string) ([]runtimeLocalMcpServerSummary, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, fmt.Errorf("resolve user home: %w", err)
	}

	var path, key, source string
	var format string
	switch provider {
	case "claude":
		path, key, source, format = filepath.Join(home, ".claude.json"), "mcpServers", "User config", "json"
	case "codebuddy":
		path, key, source, format = codebuddyUserMcpConfigPath(home), "mcpServers", "User config", "jsonc"
	case "kimi":
		// Inventory only — kimi is deliberately absent from
		// loadRuntimeMcpServerConfigs. `kimi acp` merges this file with the
		// ephemeral `mcpServers` we send in session/new, so merging it in
		// again would spawn every user server twice.
		kimiHome := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME"))
		if kimiHome == "" {
			kimiHome = filepath.Join(home, ".kimi-code")
		}
		path, key, source, format = filepath.Join(kimiHome, "mcp.json"), "mcpServers", "User config", "json"
	case "codex":
		codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if codexHome == "" {
			codexHome = filepath.Join(home, ".codex")
		}
		path, key, source, format = filepath.Join(codexHome, "config.toml"), "mcp_servers", "User config", "toml"
	case "cursor":
		path, key, source, format = filepath.Join(home, ".cursor", "mcp.json"), "mcpServers", "User config", "json"
	case "opencode":
		configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		path, key, source, format = filepath.Join(configHome, "opencode", "opencode.json"), "mcp", "User config", "json"
	case "openclaw":
		path = strings.TrimSpace(os.Getenv("CLAWDBOT_CONFIG_PATH"))
		if path == "" {
			stateDir := strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR"))
			if stateDir == "" {
				stateDir = filepath.Join(home, ".openclaw")
			}
			path = filepath.Join(stateDir, "openclaw.json")
		}
		key, source, format = "mcp.servers", "User config", "json"
	case "omp":
		// Inventory scope: omp discovers servers from a multi-level precedence
		// chain (.omp/mcp.json, .omp/.mcp.json, profile/user-level configs, and
		// third-party tool configs such as .claude.json, .cursor/mcp.json,
		// .vscode/mcp.json, and project-root mcp.json/.mcp.json). This inventory
		// reads only ~/.omp/agent/mcp.json — the user-scope entry point — so a
		// user who clears every server in the UI may still inherit servers from
		// lower-precedence sources. This matches the simplification other
		// providers already make and avoids sending full tool-chain state over
		// the wire.
		path, key, source, format = filepath.Join(home, ".omp", "agent", "mcp.json"), "mcpServers", "User config", "json"
	default:
		return []runtimeLocalMcpServerSummary{}, false, nil
	}

	out := make([]runtimeLocalMcpServerSummary, 0)
	raw, err := os.ReadFile(path)
	if err == nil {
		cfg, err := unmarshalRuntimeMcpConfig(raw, format)
		if err != nil {
			return nil, true, err
		}
		if servers, ok := nestedRuntimeMcpMap(cfg, key); ok {
			out = append(out, runtimeMcpSummaries(servers, source)...)
		}
	} else if !os.IsNotExist(err) {
		return nil, true, fmt.Errorf("read runtime MCP config: %w", err)
	}

	if provider == "claude" {
		out = append(out, listClaudePluginMcpServers(home)...)
	}

	// User configuration wins on a same-name collision. Plugin entries are
	// appended afterwards and only fill names the user config did not define.
	deduped := make([]runtimeLocalMcpServerSummary, 0, len(out))
	seen := make(map[string]bool)
	for _, server := range out {
		if seen[server.Name] {
			continue
		}
		seen[server.Name] = true
		deduped = append(deduped, server)
	}
	out = deduped
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, true, nil
}

func runtimeMcpSummaries(servers map[string]any, source string) []runtimeLocalMcpServerSummary {
	out := make([]runtimeLocalMcpServerSummary, 0, len(servers))
	for name, value := range servers {
		entry, ok := value.(map[string]any)
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		enabled := true
		if value, ok := entry["enabled"].(bool); ok {
			enabled = value
		}
		if value, ok := entry["disabled"].(bool); ok && value {
			enabled = false
		}
		out = append(out, runtimeLocalMcpServerSummary{
			Name:      name,
			Transport: runtimeMcpTransport(entry),
			Source:    source,
			Enabled:   enabled,
		})
	}
	return out
}

func listClaudePluginMcpServers(home string) []runtimeLocalMcpServerSummary {
	out := make([]runtimeLocalMcpServerSummary, 0)
	for _, plugin := range listEnabledClaudePlugins(home) {
		manifest, _ := readClaudePluginManifest(plugin.InstallPath)
		paths := claudePluginComponentPaths(
			plugin.InstallPath,
			manifest.MCPServers,
			filepath.Join(plugin.InstallPath, ".mcp.json"),
		)
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var cfg map[string]any
			if json.Unmarshal(raw, &cfg) != nil {
				continue
			}
			servers, ok := nestedRuntimeMcpMap(cfg, "mcpServers")
			if !ok {
				continue
			}
			out = append(out, runtimeMcpSummaries(servers, "Claude Plugin · "+plugin.Name)...)
		}
	}
	return out
}

func nestedRuntimeMcpMap(cfg map[string]any, path string) (map[string]any, bool) {
	current := cfg
	parts := strings.Split(path, ".")
	for index, part := range parts {
		value, exists := current[part]
		if !exists {
			return nil, false
		}
		mapped, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		if index == len(parts)-1 {
			return mapped, true
		}
		current = mapped
	}
	return nil, false
}

func runtimeMcpTransport(entry map[string]any) string {
	kind, _ := entry["type"].(string)
	switch strings.ToLower(kind) {
	case "local", "stdio":
		return "stdio"
	case "remote", "http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	}
	if _, ok := entry["command"]; ok {
		return "stdio"
	}
	if _, ok := entry["url"]; ok {
		return "http"
	}
	return "unknown"
}
