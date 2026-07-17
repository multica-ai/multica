package execenv

import (
	"bytes"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// stripTaskIrrelevantCodexConfigEntries removes user-level skill config and
// marketplace update sources from copied config.toml content.
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
// Multica materializes the agent's assigned skills directly under the per-task
// CODEX_HOME/skills. User-level `[marketplaces.*]` entries are also unsafe in a
// task copy because Codex probes or clones each git source before the first
// turn. `[plugins.*]` entries remain intact so installed plugins still load
// from the shared plugin cache exposed by prepareCodexHomeWithOpts.
//
// Source ranges preserve comments and every unrelated expression byte-for-byte
// while supporting all legal TOML key forms, including dotted keys, quoted
// tables, inline root values, and array tables.
func stripTaskIrrelevantCodexConfigEntries(content []byte) ([]byte, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return content, nil
	}

	var decoded map[string]any
	if err := toml.Unmarshal(content, &decoded); err != nil {
		return nil, fmt.Errorf("parse config.toml before task sanitization: %w", err)
	}

	var parser unstable.Parser
	parser.Reset(content)
	var currentTable []string
	ranges := make([]tomlByteRange, 0, 16)
	for parser.NextExpression() {
		expr := parser.Expression()
		keys := tomlNodeKeys(expr)
		switch expr.Kind {
		case unstable.Table, unstable.ArrayTable:
			currentTable = keys
			if isTaskIrrelevantCodexConfigPath(keys) {
				r, err := tomlTableHeaderLineRange(content, expr)
				if err != nil {
					return nil, fmt.Errorf("locate task-irrelevant config table: %w", err)
				}
				r.start = precedingBlankLinesStart(content, r.start)
				ranges = append(ranges, r)
			}
		case unstable.KeyValue:
			fullKey := make([]string, 0, len(currentTable)+len(keys))
			fullKey = append(fullKey, currentTable...)
			fullKey = append(fullKey, keys...)
			if len(fullKey) == 1 && fullKey[0] == "skills" && expr.Value().Kind == unstable.InlineTable {
				inlineRanges, removeExpression, err := taskIrrelevantInlineSkillsRanges(content, expr)
				if err != nil {
					return nil, fmt.Errorf("locate inline skills.config: %w", err)
				}
				if removeExpression {
					ranges = append(ranges, tomlExpressionLineRange(content, expr))
				} else {
					ranges = append(ranges, inlineRanges...)
				}
				continue
			}
			if isTaskIrrelevantCodexConfigPath(fullKey) {
				ranges = append(ranges, tomlExpressionLineRange(content, expr))
			}
		}
	}
	if err := parser.Error(); err != nil {
		return nil, fmt.Errorf("parse config.toml task sanitization expressions: %w", err)
	}
	stripped, err := removeTOMLByteRanges(content, ranges)
	if err != nil {
		return nil, fmt.Errorf("remove task-irrelevant config expressions: %w", err)
	}
	if err := toml.Unmarshal(stripped, &decoded); err != nil {
		return nil, fmt.Errorf("validate config.toml after task sanitization: %w", err)
	}
	if len(ranges) > 0 {
		stripped = bytes.TrimLeft(stripped, "\n")
		stripped = bytes.TrimRight(stripped, "\n")
		if len(stripped) > 0 {
			stripped = append(stripped, '\n')
		}
	}
	return stripped, nil
}

// taskIrrelevantInlineSkillsRanges locates config entries inside a top-level
// inline skills table. Removing the whole expression is safe only when every
// entry belongs to skills.config; otherwise the returned ranges preserve the
// remaining inline table byte-for-byte.
func taskIrrelevantInlineSkillsRanges(content []byte, expression *unstable.Node) ([]tomlByteRange, bool, error) {
	table := expression.Value()
	entries := make([]*unstable.Node, 0, 4)
	targets := make([]bool, 0, 4)
	targetCount := 0
	for it := table.Children(); it.Next(); {
		entry := it.Node()
		if entry.Kind != unstable.KeyValue {
			return nil, false, fmt.Errorf("inline skills table contains %s instead of key-value", entry.Kind)
		}
		keys := tomlNodeKeys(entry)
		isTarget := len(keys) > 0 && keys[0] == "config"
		entries = append(entries, entry)
		targets = append(targets, isTarget)
		if isTarget {
			targetCount++
		}
	}
	if targetCount == 0 {
		return nil, false, nil
	}
	if targetCount == len(entries) {
		return nil, true, nil
	}

	closingBrace, err := tomlInlineTableClosingBrace(content, expression, table)
	if err != nil {
		return nil, false, err
	}
	ranges := make([]tomlByteRange, 0, targetCount)
	for index, entry := range entries {
		if !targets[index] {
			continue
		}
		start, err := firstTOMLKeyOffset(content, entry)
		if err != nil {
			return nil, false, err
		}
		if index+1 < len(entries) {
			end, err := firstTOMLKeyOffset(content, entries[index+1])
			if err != nil {
				return nil, false, err
			}
			ranges = append(ranges, tomlByteRange{start: start, end: end})
			continue
		}

		// The last entry has no following key whose offset can delimit it.
		// Include its preceding separator and stop immediately before the
		// inline table's closing brace.
		separator := start
		for separator > 0 && (content[separator-1] == ' ' || content[separator-1] == '\t') {
			separator--
		}
		if separator == 0 || content[separator-1] != ',' {
			return nil, false, fmt.Errorf("last inline skills.config entry has no preceding comma")
		}
		end := closingBrace
		for end > start && (content[end-1] == ' ' || content[end-1] == '\t') {
			end--
		}
		ranges = append(ranges, tomlByteRange{start: separator - 1, end: end})
	}
	return ranges, false, nil
}

func firstTOMLKeyOffset(content []byte, keyValue *unstable.Node) (int, error) {
	it := keyValue.Key()
	if !it.Next() {
		return 0, fmt.Errorf("inline key-value has no key")
	}
	offset := int(it.Node().Raw.Offset)
	if offset < 0 || offset > len(content) {
		return 0, fmt.Errorf("inline key offset is outside config")
	}
	return offset, nil
}

// tomlInlineTableClosingBrace derives the value boundary from go-toml's full
// KeyValue source range instead of duplicating TOML string grammar locally.
func tomlInlineTableClosingBrace(content []byte, expression, table *unstable.Node) (int, error) {
	start := int(table.Raw.Offset)
	if start < 0 || start >= len(content) || content[start] != '{' {
		return 0, fmt.Errorf("inline table opening brace is outside config")
	}
	end := int(expression.Raw.Offset) + int(expression.Raw.Length)
	if end <= start || end > len(content) || content[end-1] != '}' {
		return 0, fmt.Errorf("inline table closing brace is outside key-value range")
	}
	return end - 1, nil
}

func tomlExpressionLineRange(content []byte, expr *unstable.Node) tomlByteRange {
	start := int(expr.Raw.Offset)
	end := start + int(expr.Raw.Length)
	start = bytes.LastIndexByte(content[:start], '\n') + 1
	if end < len(content) {
		if newline := bytes.IndexByte(content[end:], '\n'); newline >= 0 {
			end += newline + 1
		}
	}
	return tomlByteRange{start: start, end: end}
}

func precedingBlankLinesStart(content []byte, start int) int {
	for start > 0 {
		lineEnd := start - 1
		lineStart := bytes.LastIndexByte(content[:lineEnd], '\n') + 1
		if len(bytes.TrimSpace(content[lineStart:lineEnd])) != 0 {
			break
		}
		start = lineStart
	}
	return start
}

func isTaskIrrelevantCodexConfigPath(keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	if keys[0] == "marketplaces" {
		return true
	}
	return len(keys) >= 2 && keys[0] == "skills" && keys[1] == "config"
}

// sanitizeCopiedCodexConfig rewrites the per-task config.toml in place,
// dropping user-level skill config and marketplace update sources inherited
// from the shared `~/.codex/config.toml`. Installed plugin entries remain so
// they can load from the shared plugin cache. No-op if the file doesn't exist
// or doesn't change.
func sanitizeCopiedCodexConfig(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config.toml: %w", err)
	}
	stripped, err := stripTaskIrrelevantCodexConfigEntries(data)
	if err != nil {
		return err
	}
	if bytes.Equal(stripped, data) {
		return nil
	}
	if err := os.WriteFile(configPath, stripped, 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}
