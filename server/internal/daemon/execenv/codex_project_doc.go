package execenv

import (
	"fmt"
	"log/slog"
	"os"
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

var projectDocMaxBytesLineRe = regexp.MustCompile(`(?m)^[ \t]*project_doc_max_bytes[ \t]*=[ \t]*\S+`)

func runtimeInstructionFileCap(provider string) int {
	switch provider {
	case "antigravity":
		return antigravityRuleFileCap
	case "codex":
		return codexRaisedProjectDocMaxBytes
	default:
		return 0
	}
}

func warnIfRuntimeInstructionFileExceedsCap(path, provider string) {
	capBytes := runtimeInstructionFileCap(provider)
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
	slog.Warn("execenv: runtime instruction file exceeds the runtime's size cap; trailing sections may be dropped",
		"path", path,
		"provider", provider,
		"bytes", info.Size(),
		"cap", capBytes,
	)
}

// ensureCodexProjectDocMaxBytes writes project_doc_max_bytes into the per-task
// config.toml when the key is absent, so Codex does not silently truncate
// AGENTS.md at the 32 KiB default. A value already present — copied from the
// user's shared ~/.codex/config.toml, or written by a previous prepare — is
// left untouched.
func ensureCodexProjectDocMaxBytes(configPath string, logger *slog.Logger) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config.toml: %w", err)
	}
	existing := string(data)
	if projectDocMaxBytesLineRe.MatchString(existing) {
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
