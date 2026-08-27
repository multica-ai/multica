package engine

import "strings"

const freshSessionCommandPrefix = "/clear"
const newChatCommandPrefix = "/new"

// ControlCommandKind identifies the shared session-control directive on the
// first non-empty line of an inbound channel message. Adapters may normalize
// transport details (for example, a platform bot mention or rich-media
// layout), but only Router applies these product semantics.
type ControlCommandKind uint8

const (
	ControlCommandFreshSession ControlCommandKind = iota + 1
	ControlCommandNewChat
)

// ControlCommand is one parsed channel session-control directive.
type ControlCommand struct {
	Kind ControlCommandKind
	Body string
}

// ParseControlCommand classifies /clear and /new through one parser. Their
// inbound syntax is intentionally identical; the only distinction is the
// product action Router applies after normalization.
func ParseControlCommand(body string) (ControlCommand, bool) {
	if parsed, ok := parseLeadingCommand(body, newChatCommandPrefix); ok {
		return ControlCommand{Kind: ControlCommandNewChat, Body: parsed}, true
	}
	if parsed, ok := parseLeadingCommand(body, freshSessionCommandPrefix); ok {
		return ControlCommand{Kind: ControlCommandFreshSession, Body: parsed}, true
	}
	return ControlCommand{}, false
}

// ParseFreshSessionCommand extracts a first-line /clear command from a channel
// message. It returns the user prompt with the directive removed. The command
// is shared product behavior: every channel that reaches Router gets the same
// fresh-session affordance without reimplementing parsing in its adapter.
//
// Matching follows the /issue command rules: case-sensitive, token-bounded,
// and only the first non-empty line can be a command. That means /clear and
// /issue are mutually exclusive on the same first line.
func ParseFreshSessionCommand(body string) (string, bool) {
	command, ok := ParseControlCommand(body)
	if !ok || command.Kind != ControlCommandFreshSession {
		return "", false
	}
	return command.Body, true
}

// ParseNewChatCommand extracts the shared /new command. Parsing intentionally
// matches /clear so adapters only normalize transport details; route rotation is
// always owned by the shared engine.
func ParseNewChatCommand(body string) (string, bool) {
	command, ok := ParseControlCommand(body)
	if !ok || command.Kind != ControlCommandNewChat {
		return "", false
	}
	return command.Body, true
}

func parseLeadingCommand(body, prefix string) (string, bool) {
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
	if !strings.HasPrefix(trimmed, prefix) {
		return "", false
	}

	rest := trimmed[len(prefix):]
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
