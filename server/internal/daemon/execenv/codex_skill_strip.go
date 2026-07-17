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
