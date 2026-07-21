package engine

import "strings"

// issueCommandPrefix is the literal command token. We match exactly —
// `/Issue` or `/ISSUE` do NOT trigger creation. The case sensitivity is
// product-intentional: it avoids accidentally promoting messages that mention
// "/issue" inline in a sentence. This is cross-platform product behavior, so it
// lives in the shared engine rather than in any one adapter.
const issueCommandPrefix = "/issue"

// ParseIssueCommand extracts an /issue command from a chat-message body. It
// returns (cmd, true) when the message qualifies and the caller should dispatch
// to IssueService.Create; (nil, false) otherwise. Recognized shapes:
//
//   - `/issue <title>`            → Title = "<title>", Description = ""
//   - `/issue <title>\n<rest...>` → Title = "<title>", Description = "<rest>"
//   - `/issue` (alone, no title)  → Title = "", Description = ""
//     (the caller falls back to the previous user message; the parser does not
//     do that lookup itself because it has no DB access)
//
// Only the first non-empty line is considered: a body that begins with blank
// lines and then `/issue ...` still qualifies. A body whose first non-empty
// line is anything other than the literal prefix is not an issue command, even
// if `/issue` appears later. `/issue` must be a whole token, not a prefix of
// one ("/issuetracker" does not match).
func ParseIssueCommand(body string) (*IssueCommand, bool) {
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
	if !strings.HasPrefix(trimmed, issueCommandPrefix) {
		return nil, false
	}

	rest := trimmed[len(issueCommandPrefix):]
	if rest != "" {
		if r0 := rest[0]; r0 != ' ' && r0 != '\t' {
			return nil, false
		}
	}

	title := strings.TrimSpace(rest)
	description := ""
	if firstIdx+1 < len(lines) {
		description = strings.TrimRight(strings.Join(lines[firstIdx+1:], "\n"), " \t\n")
	}
	return &IssueCommand{Title: title, Description: description}, true
}

// titleFromPreviousMessage derives a title from a prior chat message, used for
// the bare `/issue` fallback. The previous message may itself be an `/issue …`
// invocation (two commands in a row), in which case stripping the prefix yields
// the real intent; otherwise the first non-empty line is the title.
func titleFromPreviousMessage(body string) string {
	if cmd, ok := ParseIssueCommand(body); ok {
		return cmd.Title
	}
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
