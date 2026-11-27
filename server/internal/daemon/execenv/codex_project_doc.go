package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Codex and Antigravity read the workdir AGENTS.md through a hard size cap.
// Codex defaults to 32 KiB (project_doc_max_bytes / AGENTS_MD_MAX_BYTES) and
// silently truncates with no marker in the model-visible text. Antigravity
// (agy) caps each rule file at 24,000 bytes and inserts a <truncated N bytes>
// marker. See #11480.
const (
	antigravityRuleFileCap         = 24000
	codexDefaultProjectDocMaxBytes = 32768
	// codexRaisedProjectDocMaxBytes is the per-task Codex cap Multica writes
	// when the user has not already set project_doc_max_bytes. It leaves
	// room for the platform brief plus user-controlled context; Antigravity's
	// cap cannot be raised, so section order and the truncation guard still
	// have to keep behavior-critical rules in the surviving prefix.
	codexRaisedProjectDocMaxBytes = 131072
)

var projectDocMaxBytesLineRe = regexp.MustCompile(`^[ \t]*project_doc_max_bytes[ \t]*=[ \t]*(\S+)`)

// topLevelProjectDocMaxBytes reports a project_doc_max_bytes assignment that
// appears before the first TOML table header. A key under [profiles.*] (or
// any other table) is ignored so a profile-only setting does not look like a
// top-level cap and skip Multica's raise, and so the warning uses the same
// value Codex actually reads from the root table.
func topLevelProjectDocMaxBytes(config string) (int, bool) {
	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			return 0, false
		}
		m := projectDocMaxBytesLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		raw := strings.Trim(m[1], `"'`)
		if i := strings.IndexAny(raw, " \t#"); i >= 0 {
			raw = raw[:i]
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			// The key is present at the top level; leave it alone even if
			// the value is not a plain integer.
			return 0, true
		}
		return n, true
	}
	return 0, false
}

func effectiveCodexProjectDocMaxBytes(configPath string) int {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return codexDefaultProjectDocMaxBytes
	}
	if n, ok := topLevelProjectDocMaxBytes(string(data)); ok && n > 0 {
		return n
	}
	return codexDefaultProjectDocMaxBytes
}

func runtimeInstructionFileCap(provider string, ctx TaskContextForEnv) int {
	switch provider {
	case "antigravity":
		return antigravityRuleFileCap
	case "codex":
		if ctx.CodexHome != "" {
			return effectiveCodexProjectDocMaxBytes(filepath.Join(ctx.CodexHome, "config.toml"))
		}
		return codexDefaultProjectDocMaxBytes
	default:
		return 0
	}
}

func warnIfRuntimeInstructionFileExceedsCap(path, provider string, ctx TaskContextForEnv) {
	capBytes := runtimeInstructionFileCap(provider, ctx)
	if capBytes <= 0 {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Size() <= int64(capBytes) {
		return
	}
	logger := ctx.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("execenv: runtime instruction file exceeds the runtime's size cap; trailing sections may be dropped",
		"path", path,
		"provider", provider,
		"bytes", info.Size(),
		"cap", capBytes,
	)
}

// ensureCodexProjectDocMaxBytes writes project_doc_max_bytes into the per-task
// config.toml when the key is absent, so Codex does not silently truncate
// AGENTS.md at the 32 KiB default. A value already present at the top level —
// copied from the user's shared ~/.codex/config.toml, or written by a previous
// prepare — is left untouched. A key that only appears inside a table, such as
// [profiles.work], does not count as a top-level setting.
func ensureCodexProjectDocMaxBytes(configPath string, logger *slog.Logger) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config.toml: %w", err)
	}
	existing := string(data)
	if _, ok := topLevelProjectDocMaxBytes(existing); ok {
		return nil
	}

	line := "project_doc_max_bytes = " + strconv.Itoa(codexRaisedProjectDocMaxBytes) + "\n"
	existing = strings.TrimLeft(existing, "\n")
	if existing == "" {
		existing = line
	} else {
		existing = line + "\n" + existing
	}

	if err := os.WriteFile(configPath, []byte(existing), 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	if logger != nil {
		logger.Info("codex project_doc_max_bytes: set per-task cap",
			"config_path", configPath,
			"bytes", codexRaisedProjectDocMaxBytes,
		)
	}
	return nil
}
