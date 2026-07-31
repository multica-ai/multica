package engine

import "strings"

const freshSessionCommandPrefix = "/new"

// ParseFreshSessionCommand extracts a first-line /new command from a channel
// message. It returns the user prompt with the directive removed. The command
// is shared product behavior: every channel that reaches Router gets the same
// fresh-session affordance without reimplementing parsing in its adapter.
//
// Matching follows the /issue command rules: case-sensitive, token-bounded,
// and only the first non-empty line can be a command. That means /new and
// /issue are mutually exclusive on the same first line.
func ParseFreshSessionCommand(body string) (string, bool) {
	lines := strings.Split(body, "\n")

	firstIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			firstIdx = i
			break
		}
	}
	if firstIdx == -1 {
		return "", false
	}

	trimmed := strings.TrimLeft(lines[firstIdx], " \t")
	if !strings.HasPrefix(trimmed, freshSessionCommandPrefix) {
		return "", false
	}

	rest := trimmed[len(freshSessionCommandPrefix):]
	if rest != "" {
		if r0 := rest[0]; r0 != ' ' && r0 != '\t' {
			return "", false
		}
	}

	bodyParts := make([]string, 0, 2)
	if firstLineBody := strings.TrimSpace(rest); firstLineBody != "" {
		bodyParts = append(bodyParts, firstLineBody)
	}
	if firstIdx+1 < len(lines) {
		bodyParts = append(bodyParts, strings.Join(lines[firstIdx+1:], "\n"))
	}
	return strings.TrimRight(strings.Join(bodyParts, "\n"), " \t\n"), true
}
