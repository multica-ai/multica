package execenv

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Background
//
// Codex records which hook handlers a user has trusted in config.toml under
// `[hooks.state."<source-id>"]` tables, keyed by the *source path* of the
// hooks.json that declared them, e.g.:
//
//	[hooks.state."/Users/u/.codex/hooks.json:pre_tool_use:0:0"]
//	trusted_hash = "sha256:..."
//
// Users trust their shared ~/.codex/hooks.json. But a Multica per-task session
// loads codex-home/hooks.json (a symlink/copy at a different absolute path), so
// Codex computes a different source ID and treats the hooks as untrusted —
// silently not loading them. To keep the user's trust decision effective we
// mirror every shared-hooks trust block onto the per-task hooks.json path.
//
// The remap is line-based and prefix-scoped (design D6): it only touches blocks
// whose key begins with `<sharedHooksPath>:` (an absolute path + colon), which
// is why plugin trust keys like `plugin@local:hooks/codex-hooks.json:...` are
// excluded by construction — they never start with that absolute path. A full
// TOML round-trip would reorder keys and drop the user's comments, so the text
// is edited in place and then validated once before writing (D3).

type codexHookTrustBlock struct {
	suffix string
	body   []string
}

type codexHookTrustSyncResult struct {
	SharedHooksCount int
	MappedHooksCount int
	StaleHooksCount  int
	Changed          bool
}

var hooksStateTableHeaderRe = regexp.MustCompile(`^\s*\[\s*hooks\s*\.\s*state\s*\.\s*"((?:\\.|[^"\\])*)"\s*\]\s*(?:#.*)?$`)

// syncCodexHookTrustState maps trusted shared ~/.codex/hooks.json handlers into
// the per-task CODEX_HOME/hooks.json source IDs that Codex actually evaluates
// at startup. Thin wrapper kept for callers that don't need the counts.
func syncCodexHookTrustState(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath string) error {
	_, err := syncCodexHookTrustStateWithResult(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath)
	return err
}

// syncCodexHookTrustStateWithResult performs the remap and reports how much it
// did. It is idempotent: every run first strips all previously mapped task
// blocks, then rebuilds them from the shared config's current state — so the
// per-task trust state always equals "the shared state, re-keyed", with no
// duplication and no lingering stale blocks.
//
// D3 (correctness red line): before writing, the final content is validated
// with toml.Unmarshal. If the edit would produce an unparseable config.toml,
// nothing is written and an error is returned — a corrupt daemon-managed config
// is downgraded to a fail-loud no-op rather than silently breaking the session.
func syncCodexHookTrustStateWithResult(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath string) (codexHookTrustSyncResult, error) {
	var result codexHookTrustSyncResult

	taskData, err := os.ReadFile(taskConfigPath)
	if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("read per-task config.toml: %w", err)
	}
	originalTaskContent := string(taskData)

	// Always drop any previously mapped task-hooks blocks first — this is the
	// pivot that makes reruns idempotent and refreshes/removes stale state.
	taskContent, staleCount := removeHooksStateBlocksWithCount(originalTaskContent, taskHooksPath)
	result.StaleHooksCount = staleCount

	// Only rebuild mappings when BOTH the shared source and the per-task target
	// are real hooks.json files. If either is missing (source removed, or the
	// optional exposure cleared the per-task copy) we leave the blocks cleared.
	if regularFileExists(sharedHooksPath) && regularFileExists(taskHooksPath) {
		sharedData, err := os.ReadFile(sharedConfigPath)
		if err != nil && !os.IsNotExist(err) {
			return result, fmt.Errorf("read shared config.toml: %w", err)
		}
		blocks := extractHooksStateBlocks(string(sharedData), sharedHooksPath)
		result.SharedHooksCount = len(blocks)
		result.MappedHooksCount = len(blocks)
		taskContent = appendMappedHooksStateBlocks(taskContent, taskHooksPath, blocks)
	}

	if taskContent == originalTaskContent {
		return result, nil
	}

	// D3: never write a config.toml the CLI's parser would reject. Validate the
	// whole edited document; on failure keep the original file untouched.
	if err := validateTOML(taskContent); err != nil {
		return result, fmt.Errorf("hook trust remap would corrupt per-task config.toml, leaving it unchanged: %w", err)
	}

	if err := os.WriteFile(taskConfigPath, []byte(taskContent), 0o644); err != nil {
		return result, fmt.Errorf("write per-task config.toml: %w", err)
	}
	result.Changed = true
	return result, nil
}

// validateTOML reports whether content parses as TOML. An empty document is
// valid.
func validateTOML(content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	var sink map[string]any
	return toml.Unmarshal([]byte(content), &sink)
}

func regularFileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// extractHooksStateBlocks returns every `[hooks.state."<sourcePath>:..."]` block
// keyed under sourcePath, capturing each block's body and the key suffix after
// sourcePath so it can be re-keyed onto another path.
func extractHooksStateBlocks(content, sourcePath string) []codexHookTrustBlock {
	prefix := sourcePath + ":"
	lines := splitLinesKeepingEndings(content)
	blocks := make([]codexHookTrustBlock, 0)
	for i := 0; i < len(lines); {
		key, ok := hooksStateKey(lines[i])
		if !ok || !strings.HasPrefix(key, prefix) {
			i++
			continue
		}

		start := i + 1
		i = start
		for i < len(lines) && !isTOMLTableHeader(lines[i]) {
			i++
		}
		body := append([]string(nil), lines[start:i]...)
		blocks = append(blocks, codexHookTrustBlock{
			suffix: strings.TrimPrefix(key, sourcePath),
			body:   body,
		})
	}
	return blocks
}

// removeHooksStateBlocks strips every `[hooks.state."<sourcePath>:..."]` block.
func removeHooksStateBlocks(content, sourcePath string) string {
	updated, _ := removeHooksStateBlocksWithCount(content, sourcePath)
	return updated
}

// removeHooksStateBlocksWithCount is removeHooksStateBlocks plus the number of
// blocks removed (surfaced as the stale count for logging).
func removeHooksStateBlocksWithCount(content, sourcePath string) (string, int) {
	prefix := sourcePath + ":"
	lines := splitLinesKeepingEndings(content)
	out := make([]string, 0, len(lines))
	removed := 0
	for i := 0; i < len(lines); {
		key, ok := hooksStateKey(lines[i])
		if !ok || !strings.HasPrefix(key, prefix) {
			out = append(out, lines[i])
			i++
			continue
		}

		removed++
		i++
		for i < len(lines) && !isTOMLTableHeader(lines[i]) {
			i++
		}
	}
	return strings.Join(out, ""), removed
}

// appendMappedHooksStateBlocks appends blocks re-keyed onto taskHooksPath at the
// end of content. Caller must have already removed the old task-hooks blocks.
func appendMappedHooksStateBlocks(content, taskHooksPath string, blocks []codexHookTrustBlock) string {
	if len(blocks) == 0 {
		return content
	}

	var b strings.Builder
	trimmed := strings.TrimRight(content, "\n")
	if trimmed != "" {
		b.WriteString(trimmed)
		b.WriteString("\n\n")
	}

	for _, block := range blocks {
		b.WriteString("[hooks.state.")
		b.WriteString(quoteTOMLBasicString(taskHooksPath + block.suffix))
		b.WriteString("]\n")
		body := strings.Trim(strings.Join(block.body, ""), "\n")
		if body != "" {
			b.WriteString(body)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

// hooksStateKey returns the decoded source-id key of a `[hooks.state."..."]`
// header line, or ok=false if the line is not such a header.
func hooksStateKey(line string) (string, bool) {
	matches := hooksStateTableHeaderRe.FindStringSubmatch(line)
	if matches == nil {
		return "", false
	}
	key, err := strconv.Unquote(`"` + matches[1] + `"`)
	if err != nil {
		return "", false
	}
	return key, true
}

func quoteTOMLBasicString(value string) string {
	return strconv.Quote(value)
}

func isTOMLTableHeader(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "[")
}

func splitLinesKeepingEndings(content string) []string {
	if content == "" {
		return nil
	}
	return strings.SplitAfter(content, "\n")
}
