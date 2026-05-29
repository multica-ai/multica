// Package skill provides shared utilities for working with SKILL.md files.
package skill

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Keeping the trailing newline inside group 1 matters: yaml.v3's `|` clip
// chomping only preserves a final newline when the input itself contains one.
var frontmatterPattern = regexp.MustCompile(`(?s)\A---\r?\n(.*?\r?\n)---`)

// ParseFrontmatter extracts name and description from the YAML frontmatter
// block of a SKILL.md file. Returns empty strings when the frontmatter is
// absent or malformed so callers can keep treating missing metadata as a
// non-fatal condition, matching the behaviour of the legacy line-based parser.
func ParseFrontmatter(content string) (name, description string) {
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}
	match := frontmatterPattern.FindStringSubmatch(content)
	if match == nil {
		return "", ""
	}

	var fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(match[1]), &fm); err != nil {
		return "", ""
	}
	return fm.Name, fm.Description
}
