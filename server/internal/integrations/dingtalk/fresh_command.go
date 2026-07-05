package dingtalk

import "strings"

// /new fresh-session command, mirroring lark/fresh_command.go: matching
// follows the /issue rules — case-sensitive, token-bounded, and only the
// first non-empty line can be a command. The directive is stripped from
// the body; the remainder (possibly empty) is the actual prompt.

const newCommandPrefix = "/new"

// parseFreshSessionCommand extracts a first-line /new command from a
// message body. Returns the body with the directive removed and whether
// the command matched.
func parseFreshSessionCommand(body string) (string, bool) {
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
	if !strings.HasPrefix(trimmed, newCommandPrefix) {
		return "", false
	}
	rest := trimmed[len(newCommandPrefix):]
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
