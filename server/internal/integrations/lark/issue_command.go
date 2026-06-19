package lark

import "strings"

// issueCommandPrefix is the literal command token. We match exactly —
// `/Issue` or `/ISSUE` do NOT trigger creation. The case sensitivity
// is product-intentional: it avoids accidentally promoting messages
// that mention "/issue" inline in a sentence.
const issueCommandPrefix = "/issue"

// parseIssueCommand extracts an /issue command from a chat-message body.
// It returns (cmd, true) when the message qualifies and the caller
// should dispatch to IssueService.Create; (nil, false) when the body
// is not an issue command. The bool is redundant when cmd != nil but
// makes call sites easier to read.
//
// Recognized shapes (matching MUL-2671 §2.3):
//
//   - `/issue <title>`            → Title = "<title>", Description = ""
//   - `/issue <title>\n<rest...>` → Title = "<title>", Description = "<rest>"
//                                   (multi-line: first line is title, the
//                                   remainder, joined back with newlines,
//                                   becomes description)
//   - `/issue` (alone, no title)  → Title = "", Description = ""
//                                   (the caller is expected to fall back
//                                   to the previous user message; the
//                                   parser does not do that lookup
//                                   itself because it has no DB access)
//
// Only the first non-empty line is considered: a message body that
// begins with blank lines and then `/issue ...` still qualifies. A
// body whose first non-empty line is anything other than the literal
// prefix is not an issue command, even if `/issue` appears later.
func parseIssueCommand(body string) (*IssueCommand, bool) {
	lines := strings.Split(body, "\n")

	// Skip leading blank lines to find the first content line. A user
	// who copy-pastes from a doc and ends up with a leading newline
	// should not have their command silently ignored.
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
	if !strings.HasPrefix(trimmed, issueCommandPrefix) {
		return nil, false
	}

	// What follows the prefix on the same line must be either the end
	// of the line or a whitespace character. This rejects words like
	// "/issuetracker" — `/issue` must be a token, not a prefix of one.
	rest := trimmed[len(issueCommandPrefix):]
	if rest != "" {
		r0 := rest[0]
		if r0 != ' ' && r0 != '\t' {
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
