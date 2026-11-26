package execenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Antigravity (agy) reads every rule file, AGENTS.md included, only up to
// 24,000 bytes and drops the rest, so a long brief in AGENTS.md lost its
// trailing Workflow / Mentions / Output sections (GitHub #11480). agy has no
// setting to raise the cap, but the cap is per file: the brief is split into
// always-on workspace rule files under .agents/rules/, each below the cap.
const (
	// antigravityRuleFilePrefix namespaces the rule files the daemon owns.
	antigravityRuleFilePrefix = "multica-runtime-"
	// antigravityRuleFileMaxBytes keeps each file, frontmatter included,
	// safely under agy's 24,000-byte per-file cap.
	antigravityRuleFileMaxBytes = 23000
	// antigravityRuleFileMarker is the first body line of every file the
	// daemon writes; cleanup only deletes files that carry it.
	antigravityRuleFileMarker = "<!-- MULTICA-RUNTIME rule file (auto-managed; do not edit) -->"
	antigravityRuleFileHeader = "---\ntrigger: always_on\n---\n" + antigravityRuleFileMarker + "\n"
)

// runtimeBriefInRuleFiles reports whether the provider receives the runtime
// brief as split rule files instead of its instruction file.
func runtimeBriefInRuleFiles(provider string) bool {
	return provider == "antigravity"
}

func antigravityRulesDir(workDir string) string {
	return filepath.Join(workDir, ".agents", "rules")
}

// writeAntigravityRuleFiles writes brief as numbered always-on rule files.
// Callers remove earlier files first (cleanupAntigravityRuleFiles), so a
// shorter brief never leaves a stale trailing part behind.
func writeAntigravityRuleFiles(workDir, brief string) error {
	dir := antigravityRulesDir(workDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create antigravity rules dir: %w", err)
	}
	parts := splitBriefForRuleFiles(brief, antigravityRuleFileMaxBytes-len(antigravityRuleFileHeader)-1)
	for i, part := range parts {
		path := filepath.Join(dir, fmt.Sprintf("%s%02d.md", antigravityRuleFilePrefix, i+1))
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("refuse to overwrite %s: it is not a Multica rule file", path)
		}
		content := antigravityRuleFileHeader + part
		if !strings.HasSuffix(part, "\n") {
			content += "\n"
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write antigravity rule file: %w", err)
		}
	}
	return nil
}

// cleanupAntigravityRuleFiles deletes the rule files the daemon wrote, then
// the rules dir and its .agents parent if that left them empty.
func cleanupAntigravityRuleFiles(workDir string) error {
	dir := antigravityRulesDir(workDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read antigravity rules dir: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, antigravityRuleFilePrefix) || !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read antigravity rule file: %w", err)
		}
		if !strings.HasPrefix(string(data), antigravityRuleFileHeader) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove antigravity rule file: %w", err)
		}
	}
	// rmdir fails on a non-empty dir, which keeps anything the user put there.
	if os.Remove(dir) == nil {
		_ = os.Remove(filepath.Dir(dir))
	}
	return nil
}

// splitBriefForRuleFiles cuts brief into parts of at most maxBytes, breaking
// at Markdown headings so each section stays whole where it fits, then at line
// boundaries, and only as a last resort inside a line.
func splitBriefForRuleFiles(brief string, maxBytes int) []string {
	var parts []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	for _, section := range splitBriefSections(brief) {
		if cur.Len()+len(section) <= maxBytes {
			cur.WriteString(section)
			continue
		}
		flush()
		if len(section) <= maxBytes {
			cur.WriteString(section)
			continue
		}
		for _, line := range strings.SplitAfter(section, "\n") {
			for len(line) > maxBytes {
				flush()
				cut := maxBytes
				for cut > 0 && !utf8.RuneStart(line[cut]) {
					cut--
				}
				parts = append(parts, line[:cut])
				line = line[cut:]
			}
			if cur.Len()+len(line) > maxBytes {
				flush()
			}
			cur.WriteString(line)
		}
	}
	flush()
	return parts
}

// splitBriefSections splits brief before every `#`, `##` or `###` heading line
// outside a fenced code block.
func splitBriefSections(brief string) []string {
	var sections []string
	var cur strings.Builder
	inFence := false
	for _, line := range strings.SplitAfter(brief, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
		}
		if !inFence && cur.Len() > 0 && (strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ")) {
			sections = append(sections, cur.String())
			cur.Reset()
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		sections = append(sections, cur.String())
	}
	return sections
}
