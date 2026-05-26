package execenv

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type codexHookTrustBlock struct {
	suffix string
	body   []string
}

var hooksStateTableHeaderRe = regexp.MustCompile(`^\s*\[\s*hooks\s*\.\s*state\s*\.\s*"((?:\\.|[^"\\])*)"\s*\]\s*(?:#.*)?$`)

// syncCodexHookTrustState maps trusted shared ~/.codex/hooks.json handlers
// into the per-task CODEX_HOME/hooks.json source IDs that Codex actually
// evaluates at startup.
func syncCodexHookTrustState(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath string) error {
	taskData, err := os.ReadFile(taskConfigPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read per-task config.toml: %w", err)
	}
	originalTaskContent := string(taskData)
	taskContent := removeHooksStateBlocks(originalTaskContent, taskHooksPath)

	if regularFileExists(sharedHooksPath) && regularFileExists(taskHooksPath) {
		sharedData, err := os.ReadFile(sharedConfigPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read shared config.toml: %w", err)
		}
		blocks := extractHooksStateBlocks(string(sharedData), sharedHooksPath)
		taskContent = appendMappedHooksStateBlocks(taskContent, taskHooksPath, blocks)
	}

	if taskContent == originalTaskContent {
		return nil
	}
	if err := os.WriteFile(taskConfigPath, []byte(taskContent), 0o644); err != nil {
		return fmt.Errorf("write per-task config.toml: %w", err)
	}
	return nil
}

func regularFileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

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

func removeHooksStateBlocks(content, sourcePath string) string {
	prefix := sourcePath + ":"
	lines := splitLinesKeepingEndings(content)
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		key, ok := hooksStateKey(lines[i])
		if !ok || !strings.HasPrefix(key, prefix) {
			out = append(out, lines[i])
			i++
			continue
		}

		i++
		for i < len(lines) && !isTOMLTableHeader(lines[i]) {
			i++
		}
	}
	return strings.Join(out, "")
}

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
