package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ensureCodexTrustedProjectConfig marks the daemon-managed workdir as trusted
// in the per-task CODEX_HOME/config.toml. This allows Codex to load
// project-local .codex config/hooks/exec policies from repositories prepared by
// the after_create hook without mutating the user's global ~/.codex/config.toml.
func ensureCodexTrustedProjectConfig(configPath, projectDir string) error {
	if strings.TrimSpace(projectDir) == "" {
		return nil
	}
	absProjectDir, err := filepath.Abs(projectDir)
	if err == nil {
		projectDir = absProjectDir
	}

	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config.toml: %w", err)
	}
	existing := string(data)
	quotedProjectDir := tomlBasicString(projectDir)
	projectHeader := "[projects." + quotedProjectDir + "]"

	lines := strings.Split(existing, "\n")
	headerRe := regexp.MustCompile(`^\s*\[\s*projects\s*\.\s*` + regexp.QuoteMeta(quotedProjectDir) + `\s*\]\s*(?:#.*)?$`)
	trustLineRe := regexp.MustCompile(`^\s*trust_level\s*=`)

	headerIndex := -1
	for i, line := range lines {
		if headerRe.MatchString(line) {
			headerIndex = i
			break
		}
	}

	var updated string
	if headerIndex >= 0 {
		endIndex := len(lines)
		for i := headerIndex + 1; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "[") {
				endIndex = i
				break
			}
		}
		for i := headerIndex + 1; i < endIndex; i++ {
			if !trustLineRe.MatchString(lines[i]) {
				continue
			}
			if strings.TrimSpace(lines[i]) == `trust_level = "trusted"` {
				return nil
			}
			lines[i] = `trust_level = "trusted"`
			updated = strings.Join(lines, "\n")
			break
		}
		if updated == "" {
			out := make([]string, 0, len(lines)+1)
			out = append(out, lines[:headerIndex+1]...)
			out = append(out, `trust_level = "trusted"`)
			out = append(out, lines[headerIndex+1:]...)
			updated = strings.Join(out, "\n")
		}
	} else {
		updated = strings.TrimRight(existing, "\n")
		if updated != "" {
			updated += "\n\n"
		}
		updated += projectHeader + "\ntrust_level = \"trusted\"\n"
	}

	if updated == existing {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}

func tomlBasicString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
