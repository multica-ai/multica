package execenv

import (
	"encoding/json"
	"fmt"
	"os"
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
// Scope note: only the multica-managed block is authoritative. Any
// [mcp_servers.*] entries the user has in their own ~/.codex/config.toml
// survive the copy and are still loaded by codex unless their name collides
// with one we render (in which case the later TOML definition wins). This
// matches the design brief from #1111 — we authorize per-agent servers, not
// strip everything the user configured globally.
func syncMcpServersToml(configPath string, raw json.RawMessage) error {
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

	rendered, err := renderMcpServersToml(raw)
	if err != nil {
		return err
	}

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

// renderMcpServersToml translates the Claude-shaped MCP config JSON into a
// Codex-shaped TOML fragment with one [mcp_servers.<name>] table per server.
func renderMcpServersToml(raw json.RawMessage) (string, error) {
	var parsed struct {
		McpServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			Cwd     string            `json:"cwd"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("parse mcp config: %w", err)
	}

	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
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
	return b.String(), nil
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
