package execenv

import (
	"os"
	"path/filepath"
	"strings"
)

// Read-only views over what Prepare / InjectRuntimeConfig put on disk, for the
// per-run context receipt (MUL-9013). None of these create, write or record
// anything: they only describe where a source went so the receipt can name it.

// RuntimeConfigRelPath returns the workdir-relative path of the provider's
// runtime config file (CLAUDE.md, AGENTS.md, QWEN.md, ...), or "" when the
// provider has no file target and the brief can only travel inline.
func RuntimeConfigRelPath(workDir, provider string) string {
	path := runtimeConfigPath(workDir, provider)
	if path == "" {
		return ""
	}
	if rel, err := filepath.Rel(workDir, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}

// ExistingRuntimeConfigContent returns the part of the provider's runtime
// config file that Multica does not manage: what the repository (or the user)
// already had in CLAUDE.md / AGENTS.md before the brief was appended. Empty
// when the file is missing, holds only the managed block, or cannot be read.
func ExistingRuntimeConfigContent(workDir, provider string) string {
	path := runtimeConfigPath(workDir, provider)
	if path == "" {
		return ""
	}
	// A missing or unreadable file both mean "nothing pre-existing we can
	// report"; the receipt is diagnostic and must not fail the run.
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	start, end, ok := locateMarkerBlock(content)
	if !ok {
		return content
	}
	pre := strings.TrimSuffix(content[:start], runtimeManagedSeparator)
	return pre + content[end:]
}

// SkillDeliveryLocation names where this provider's skills are materialized,
// for display only. Workdir-relative for runtimes that discover skills from
// the project tree; a per-task home for the two that read them from there.
func SkillDeliveryLocation(workDir, provider string) string {
	switch provider {
	case "codex":
		return "$CODEX_HOME/skills"
	case "hermes":
		return "$HERMES_HOME/skills"
	}
	dir := skillsDirPath(workDir, provider)
	if rel, err := filepath.Rel(workDir, dir); err == nil {
		return filepath.ToSlash(rel)
	}
	return dir
}
