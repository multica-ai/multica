package lark

import "strings"

const (
	newCommandPrefix = "/new"
)

// FreshSessionCommand is the normalized fresh-start directive extracted from a
// Lark inbound message. Body is the user prompt with the directive removed.
type FreshSessionCommand struct {
	Body string
}

// parseFreshSessionCommand extracts a first-line /new command from a message
// body. Matching follows the /issue command rules: case-sensitive,
// token-bounded, and only the first non-empty line can be a command. That
// means /new and /issue are mutually exclusive on the same first line.
func parseFreshSessionCommand(body string) (*FreshSessionCommand, bool) {
	lines := strings.Split(body, "\n")

	firstIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			firstIdx = i
			break
		}
	}
	if firstIdx == -1 {
		return nil, false
	}

	first := lines[firstIdx]
	trimmed := strings.TrimLeft(first, " \t")
	prefix, ok := matchedFreshPrefix(trimmed)
	if !ok {
		return nil, false
	}

	rest := trimmed[len(prefix):]
	if rest != "" {
		r0 := rest[0]
		if r0 != ' ' && r0 != '\t' {
			return nil, false
		}
	}

	bodyParts := make([]string, 0, 2)
	if firstLineBody := strings.TrimSpace(rest); firstLineBody != "" {
		bodyParts = append(bodyParts, firstLineBody)
	}
	if firstIdx+1 < len(lines) {
		bodyParts = append(bodyParts, strings.Join(lines[firstIdx+1:], "\n"))
	}
	stripped := strings.TrimRight(strings.Join(bodyParts, "\n"), " \t\n")
	return &FreshSessionCommand{Body: stripped}, true
}

func matchedFreshPrefix(line string) (string, bool) {
	switch {
	case strings.HasPrefix(line, newCommandPrefix):
		return newCommandPrefix, true
	default:
		return "", false
	}
}
