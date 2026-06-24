package octo

import "strings"

const (
	newCommandPrefix = "/new"
)

// FreshSessionCommand is the normalized fresh-start directive extracted from an
// Octo inbound message. Body is the user prompt with the directive removed.
type FreshSessionCommand struct {
	Body string
}

// parseFreshSessionCommand extracts a first-line /new command from a message
// body. Matching is case-sensitive and token-bounded — `/new` and `/new foo`
// match, `/News` and `please /new foo` do not — and only the first non-empty
// line can carry a command. Leading blank lines are skipped so a forwarded
// or padded message still parses.
//
// Mirrors the Lark equivalent (server/internal/integrations/lark/fresh_command.go)
// so the two integrations stay product-identical on this directive.
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
