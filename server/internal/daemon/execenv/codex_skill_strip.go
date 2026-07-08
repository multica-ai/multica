package execenv

import (
	"fmt"
	"os"
	"strings"
)

// stripSkillsConfigEntries removes every `[[skills.config]]` array-of-tables
// block from the given config.toml content.
//
// Background: Codex Desktop writes one `[[skills.config]]` entry per skill it
// knows about — file-backed skills get a `path = "..."` field, while
// plugin-backed skills (e.g. `name = "superpowers:brainstorming"`) only get a
// `name`. Codex CLI 0.114's TOML deserializer treats `path` as a required
// field, so it rejects the plugin entries with `missing field path` and
// refuses to start. Multica copies the user's `~/.codex/config.toml` verbatim
// into each task's isolated codex-home, which propagates the broken entries
// into the per-task config and blocks `codex thread/start`.
//
// Stripping the whole `[[skills.config]]` array sidesteps the issue: Multica
// writes the agent's currently assigned skills directly to
// `codex-home/skills/<name>/SKILL.md`, and Codex auto-discovers them from
// that directory. The user-level skill registry is irrelevant to a per-task
// run, so dropping it is both safe and the right scope of isolation.
//
// Lines outside `[[skills.config]]` blocks are preserved untouched.
func stripSkillsConfigEntries(content string) string {
	if !strings.Contains(content, "[[skills.config]]") {
		return content
	}

	return stripTOMLBlocks(content, func(header string) bool {
		return header == "skills.config"
	})
}

// stripCodexPluginRuntimeConfig removes plugin marketplace/runtime tables from
// a copied Codex config.toml. This is only used when
// MULTICA_CODEX_EXPOSE_PLUGIN_CACHE disables plugin cache exposure; keeping
// plugin tables without the matching cache makes Codex try to load plugins
// from absent per-task paths on every cold start.
func stripCodexPluginRuntimeConfig(content string) string {
	return stripTOMLBlocks(content, func(header string) bool {
		return header == "plugins" ||
			header == "marketplaces" ||
			strings.HasPrefix(header, "plugins.") ||
			strings.HasPrefix(header, "marketplaces.")
	})
}

// stripCodexUserMCPRuntimeConfig removes MCP server tables copied from the
// host user's Codex config. Managed MCP from the agent profile is written
// later by pkg/agent/codex.go, so this only affects implicit inheritance.
func stripCodexUserMCPRuntimeConfig(content string) string {
	return stripTOMLBlocks(content, func(header string) bool {
		return header == "mcp_servers" || strings.HasPrefix(header, "mcp_servers.")
	})
}

func stripTOMLBlocks(content string, dropHeader func(string) bool) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	dropping := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "[") {
			header, ok := tomlHeaderName(trimmed)
			if ok && dropHeader(header) {
				dropping = true
				continue
			}
			dropping = false
			out = append(out, line)
			continue
		}

		if dropping {
			continue
		}
		out = append(out, line)
	}

	stripped := strings.Join(out, "\n")
	// Collapse the trailing blank-line cluster that the removal can leave
	// behind so repeated copies don't grow the file unboundedly.
	stripped = strings.TrimRight(stripped, "\n") + "\n"
	if strings.TrimSpace(stripped) == "" {
		return ""
	}
	return stripped
}

func tomlHeaderName(trimmedLine string) (string, bool) {
	if strings.HasPrefix(trimmedLine, "[[") && strings.Contains(trimmedLine, "]]") {
		end := strings.Index(trimmedLine, "]]")
		return strings.TrimSpace(trimmedLine[2:end]), true
	}
	if strings.HasPrefix(trimmedLine, "[") && strings.Contains(trimmedLine, "]") {
		end := strings.Index(trimmedLine, "]")
		return strings.TrimSpace(trimmedLine[1:end]), true
	}
	return "", false
}

// sanitizeCopiedCodexConfig rewrites the per-task config.toml in place,
// dropping `[[skills.config]]` entries inherited from the shared
// `~/.codex/config.toml`. No-op if the file doesn't exist or doesn't change.
func sanitizeCopiedCodexConfig(configPath string) error {
	return sanitizeCopiedCodexConfigWithOptions(configPath, false, false)
}

func sanitizeCopiedCodexConfigWithOptions(configPath string, stripPluginRuntime bool, stripUserMCP bool) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config.toml: %w", err)
	}
	stripped := stripSkillsConfigEntries(string(data))
	if stripPluginRuntime {
		stripped = stripCodexPluginRuntimeConfig(stripped)
	}
	if stripUserMCP {
		stripped = stripCodexUserMCPRuntimeConfig(stripped)
	}
	if stripped == string(data) {
		return nil
	}
	if err := os.WriteFile(configPath, []byte(stripped), 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}
