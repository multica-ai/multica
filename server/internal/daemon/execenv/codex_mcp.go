package execenv

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
)

// syncMcpServersToml reconciles the multica-managed [mcp_servers.*] block in
// the given config.toml against the agent's current MCP config JSON
// ({"mcpServers": {name: {command, args, env}}}). Any prior multica-managed
// block is removed first; a fresh block is written only if raw has content.
//
// This is the one-shot state sync API for per-task config.toml. On Prepare
// the file came from `~/.codex/config.toml` copy; on Reuse it persists from
// the previous task. Either way, calling this function with the current
// mcp_config guarantees the file reflects the agent's present state — a task
// whose mcp_config was cleared between runs no longer sees previously
// authorized servers. The file is created if absent.
//
// Scope note: the daemon authorizes per-agent MCP servers via the managed
// block; user-managed [mcp_servers.*] entries in the copied global config
// survive UNLESS their name collides with one we render. TOML 1.0 rejects
// duplicate key definitions (the `toml` crate that Codex uses errors on
// load), so collisions are stripped from the user copy before the managed
// block is appended — this is required for the generated file to parse.
func syncMcpServersToml(configPath string, raw json.RawMessage, logger *slog.Logger) error {
	existing, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read codex config.toml: %w", err)
	}

	const (
		beginMarker = "# BEGIN multica-managed mcp_servers"
		endMarker   = "# END multica-managed mcp_servers"
	)

	body := stripMarkedBlock(string(existing), beginMarker, endMarker)

	if len(raw) == 0 {
		// Nothing to append. If nothing else was in the file, don't create it;
		// otherwise write back the stripped remainder so a cleared mcp_config
		// on Reuse actually removes the prior block.
		if len(body) == 0 && len(existing) == 0 {
			return nil
		}
		if string(existing) == body {
			return nil // nothing changed
		}
		if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
			return fmt.Errorf("write codex config.toml: %w", err)
		}
		return nil
	}

	names, rendered, err := renderMcpServersWithNames(raw, logger)
	if err != nil {
		return err
	}

	// Strip any user-managed [mcp_servers.<name>] sections or
	// mcp_servers.<name>.* dotted keys for names the managed block will
	// redefine — TOML 1.0 rejects duplicate table definitions, so the
	// merged file must not contain both.
	body = stripUserMcpServerEntries(body, names, logger)

	if !strings.HasSuffix(body, "\n") && len(body) > 0 {
		body += "\n"
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString(beginMarker)
	b.WriteString("\n")
	b.WriteString(rendered)
	b.WriteString(endMarker)
	b.WriteString("\n")

	if err := os.WriteFile(configPath, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write codex config.toml: %w", err)
	}
	return nil
}

// stripMarkedBlock removes a line-anchored multica-managed block from s.
// The BEGIN marker must start at the file start or follow a newline; the END
// marker must follow a newline and precede a newline or end-of-string. This
// prevents a user-controlled value that contains the literal marker text from
// confusing the marker search on re-append, because quoteTomlString escapes
// real newlines inside string values to the literal two-char sequence `\n`.
func stripMarkedBlock(s, begin, end string) string {
	b := findLineAnchored(s, begin, 0)
	if b < 0 {
		return s
	}
	afterBegin := b + len(begin)
	// End marker must be on its own line — so preceded by '\n' somewhere
	// after the BEGIN marker's trailing newline.
	e := findLineAnchored(s, end, afterBegin)
	if e < 0 {
		return s
	}
	cut := e + len(end)
	tail := s[cut:]
	if strings.HasPrefix(tail, "\n") {
		tail = tail[1:]
	}
	return s[:b] + tail
}

// findLineAnchored returns the index of the first occurrence of marker in s
// at or after pos, where marker either starts at pos 0 or is preceded by '\n'.
// Returns -1 if no line-anchored occurrence exists.
func findLineAnchored(s, marker string, pos int) int {
	for pos <= len(s) {
		idx := strings.Index(s[pos:], marker)
		if idx < 0 {
			return -1
		}
		abs := pos + idx
		if abs == 0 || s[abs-1] == '\n' {
			return abs
		}
		pos = abs + 1
	}
	return -1
}

// renderMcpServersToml is a thin wrapper for callers (and tests) that only
// need the rendered body.
func renderMcpServersToml(raw json.RawMessage) (string, error) {
	_, rendered, err := renderMcpServersWithNames(raw, nil)
	return rendered, err
}

// renderMcpServersWithNames translates the Claude-shaped MCP config JSON into
// a Codex-shaped TOML fragment and returns the sorted set of server names
// rendered into it. Entries without a `command` field (e.g. HTTP/SSE-transport
// MCP servers carrying a `url`) are skipped with a warning — Codex currently
// only supports stdio-transport MCP servers via config.toml.
func renderMcpServersWithNames(raw json.RawMessage, logger *slog.Logger) ([]string, string, error) {
	type serverEntry struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Cwd     string            `json:"cwd"`
		URL     string            `json:"url"`
		Type    string            `json:"type"`
	}
	var parsed struct {
		McpServers map[string]serverEntry `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", fmt.Errorf("parse mcp config: %w", err)
	}

	names := make([]string, 0, len(parsed.McpServers))
	for name, srv := range parsed.McpServers {
		if srv.Command == "" {
			if logger != nil {
				logger.Warn("execenv: skipping non-stdio MCP server (Codex config.toml supports stdio transport only)",
					"name", name, "type", srv.Type, "url_set", srv.URL != "")
			}
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for i, name := range names {
		srv := parsed.McpServers[name]
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[mcp_servers.%s]\n", quoteTomlKey(name))
		if srv.Command != "" {
			fmt.Fprintf(&b, "command = %s\n", quoteTomlString(srv.Command))
		}
		if len(srv.Args) > 0 {
			b.WriteString("args = [")
			for i, a := range srv.Args {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(quoteTomlString(a))
			}
			b.WriteString("]\n")
		}
		if len(srv.Env) > 0 {
			envKeys := make([]string, 0, len(srv.Env))
			for k := range srv.Env {
				envKeys = append(envKeys, k)
			}
			sort.Strings(envKeys)
			b.WriteString("env = {")
			for i, k := range envKeys {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, " %s = %s", quoteTomlKey(k), quoteTomlString(srv.Env[k]))
			}
			b.WriteString(" }\n")
		}
		if srv.Cwd != "" {
			fmt.Fprintf(&b, "cwd = %s\n", quoteTomlString(srv.Cwd))
		}
	}
	return names, b.String(), nil
}

// quoteTomlKey returns a TOML-safe key, quoted when it contains characters
// that would not parse as a bare key.
func quoteTomlKey(k string) string {
	if len(k) == 0 {
		return `""`
	}
	for _, r := range k {
		isBare := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-'
		if !isBare {
			return quoteTomlString(k)
		}
	}
	return k
}

// quoteTomlString returns a TOML basic string literal for s. Escape rules
// match the TOML spec: backslash, double-quote, and ASCII control chars are
// escaped; all other Unicode passes through verbatim.
func quoteTomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			// TOML basic strings reject raw U+0000-U+001F and U+007F.
			if r < 0x20 || r == 0x7F {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// userMcpServerSectionRe matches a top-level section header of the form
// `[mcp_servers.<name>]`, capturing <name> either as a bare key (group 1) or
// a double-quoted key (group 2). Whitespace is allowed inside the brackets
// per TOML. Single-quoted (literal-string) keys are not matched — they're
// permitted by TOML but vanishingly rare in practice.
var userMcpServerSectionRe = regexp.MustCompile(
	`^[ \t]*\[[ \t]*mcp_servers[ \t]*\.[ \t]*(?:([A-Za-z0-9_-]+)|"((?:[^"\\]|\\.)*)")[ \t]*\][ \t]*$`,
)

// userMcpServerDottedKeyRe matches a top-level dotted-key assignment of the
// form `mcp_servers.<name>.<anything> = ...`, capturing <name> the same way
// as userMcpServerSectionRe.
var userMcpServerDottedKeyRe = regexp.MustCompile(
	`^[ \t]*mcp_servers[ \t]*\.[ \t]*(?:([A-Za-z0-9_-]+)|"((?:[^"\\]|\\.)*)")[ \t]*\.[^=\s]+[ \t]*=`,
)

// stripUserMcpServerEntries removes top-level `[mcp_servers.<name>]` sections
// and standalone `mcp_servers.<name>.*` dotted-key lines from src for any
// <name> in blocked. Needed because the copied user config.toml can define
// entries whose names collide with those rendered by the managed block, and
// TOML 1.0 rejects duplicate table / key definitions — without this strip,
// the merged file fails to load and Codex refuses to start the task.
//
// Name matching uses the TOML-key semantics both writers share: bare keys
// and double-quoted basic strings. For the double-quoted form, `\"` and
// `\\` escapes are unquoted before comparison; other escapes are left as-is
// (Codex/toml crate canonicalizes identically).
func stripUserMcpServerEntries(src string, blocked []string, logger *slog.Logger) string {
	if len(blocked) == 0 || src == "" {
		return src
	}
	blockedSet := make(map[string]bool, len(blocked))
	for _, n := range blocked {
		blockedSet[n] = true
	}

	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	inBlockedSection := false
	strippedSections := 0
	strippedDotted := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			// Entering a new section — reset blocked-section state.
			if m := userMcpServerSectionRe.FindStringSubmatch(line); m != nil {
				name := m[1]
				if name == "" {
					name = unescapeTomlBasic(m[2])
				}
				if blockedSet[name] {
					inBlockedSection = true
					strippedSections++
					continue
				}
			}
			inBlockedSection = false
			out = append(out, line)
			continue
		}
		if inBlockedSection {
			continue
		}
		if m := userMcpServerDottedKeyRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			if name == "" {
				name = unescapeTomlBasic(m[2])
			}
			if blockedSet[name] {
				strippedDotted++
				continue
			}
		}
		out = append(out, line)
	}
	if logger != nil && (strippedSections > 0 || strippedDotted > 0) {
		logger.Info("execenv: stripped colliding user mcp_servers entries from copied codex config.toml",
			"sections", strippedSections, "dotted_keys", strippedDotted, "names", blocked)
	}
	return strings.Join(out, "\n")
}

// unescapeTomlBasic decodes the two escape sequences we need for key
// comparison in double-quoted TOML basic strings: `\"` and `\\`. Other
// escapes are preserved as-written so they round-trip identically to how
// the TOML parser would interpret them — which is fine for equality
// comparison against names we renderMcpServersWithNames emits.
func unescapeTomlBasic(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			next := s[i+1]
			if next == '"' || next == '\\' {
				b.WriteByte(next)
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
