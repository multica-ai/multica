package dingtalk

import "strings"

// newCommandPrefix is the literal fresh-session token. Matching mirrors the
// engine's /issue rules: case-sensitive and token-bounded, so "/newness" or
// "/New" do not trigger a reset.
const newCommandPrefix = "/new"

// freshSessionCommand is the normalized fresh-start directive extracted from a
// DingTalk inbound message. Body is the user prompt with the directive removed.
type freshSessionCommand struct {
	Body string
}

// parseFreshSessionCommand extracts a first-line /new command from a message
// body. DingTalk has no threads, so a conversation is one perpetual session;
// /new is the only way for a member to drop accumulated context and start over.
// Matching follows the /issue command rules: case-sensitive, token-bounded, and
// only the first non-empty line can be a command. That means /new and /issue are
// mutually exclusive on the same first line.
func parseFreshSessionCommand(body string) (*freshSessionCommand, bool) {
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

	trimmed := strings.TrimLeft(lines[firstIdx], " \t")
	if !strings.HasPrefix(trimmed, newCommandPrefix) {
		return nil, false
	}

	rest := trimmed[len(newCommandPrefix):]
	if rest != "" {
		if r0 := rest[0]; r0 != ' ' && r0 != '\t' {
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
	return &freshSessionCommand{Body: stripped}, true
}
