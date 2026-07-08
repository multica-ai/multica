// Package skill provides shared utilities for working with SKILL.md files.
package skill

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Keeping the trailing newline inside group 1 matters: yaml.v3's `|` clip
// chomping only preserves a final newline when the input itself contains one.
var frontmatterPattern = regexp.MustCompile(`(?s)\A---\r?\n(.*?\r?\n)---`)

// ParseSkillFrontmatter extracts name and description from the YAML frontmatter
// block of a SKILL.md file. Returns empty strings when the frontmatter is
// absent or malformed so callers can keep treating missing metadata as a
// non-fatal condition, matching the behaviour of the legacy line-based parser.
//
// Values are decoded into a generic map and coerced per key (scalars via their
// literal form, sequences/mappings via JSON) rather than unmarshalled into a
// string struct. This means a structured value in one field never discards a
// valid sibling key, and the coercion mirrors the TS parseFrontmatter in
// packages/core/skills/frontmatter.ts so both sides agree on the same input.
func ParseSkillFrontmatter(content string) (name, description string) {
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}
	match := frontmatterPattern.FindStringSubmatch(content)
	if match == nil {
		return "", ""
	}

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(match[1]), &fm); err != nil {
		return "", ""
	}
	return coerceFrontmatterValue(fm["name"]), coerceFrontmatterValue(fm["description"])
}

// coerceFrontmatterValue renders a decoded YAML value as a string, mirroring the
// TS side: nil becomes empty, strings pass through, other scalars use their
// literal form, and structured values (sequences/mappings) are JSON-encoded.
func coerceFrontmatterValue(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'g', -1, 64)
	default:
		encoded, err := json.Marshal(val)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}
