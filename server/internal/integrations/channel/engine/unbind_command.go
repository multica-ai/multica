package engine

import "strings"

// unbindCommandToken is the literal command. Like /issue, the match is
// exact and case-sensitive — cross-platform product behavior that lives in
// the shared engine, not in any one adapter.
const unbindCommandToken = "/unbind"

// ParseUnbindCommand reports whether a chat-message body is the /unbind
// command: the first non-empty line is exactly `/unbind` (surrounding
// whitespace ignored). The command takes no arguments — `/unbind now` or a
// sentence mentioning /unbind inline does not qualify, mirroring the
// strictness of ParseIssueCommand.
func ParseUnbindCommand(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return trimmed == unbindCommandToken
	}
	return false
}
