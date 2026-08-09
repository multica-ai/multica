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
//     (the Router returns a usage result; it never infers a title from history)
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

// issueDescriptionFromCommandBody removes the /issue directive line from the
// full normalized message while preserving the layout that follows it. Unlike
// CommandText, the full body may still contain adapter-generated inline media
// placeholders. Keeping those placeholders in the initial issue description
// gives the detached media binder stable positions to materialize later.
//
// Content before the directive is deliberately excluded. Some adapters enrich
// Body with quoted context while keeping CommandText as the user's own command;
// copying that prefix into the issue would change the existing command contract.
func issueDescriptionFromCommandBody(body, commandText, fallback string) string {
	_, end, ok := issueCommandLineBounds(body, commandText)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(body[end:])
}

type textLineBounds struct {
	start int
	end   int
}

// issueCommandLineBounds locates the actual user-authored /issue directive in
// the normalized body. Adapters may prepend enriched history containing older
// /issue lines, so matching the first token-bounded directive is insufficient.
// CommandText is the un-enriched command source: count identical directive
// lines there, then select the corresponding first occurrence in the body's
// user-authored suffix. This also remains stable when the description repeats
// the exact directive line.
func issueCommandLineBounds(body, commandText string) (start, end int, ok bool) {
	expected, ok := firstIssueCommandLine(commandText)
	if !ok {
		return 0, 0, false
	}
	commandOccurrences := countMatchingLines(commandText, expected)
	candidates := matchingLineBounds(body, expected)
	target := len(candidates) - commandOccurrences
	if target < 0 || target >= len(candidates) {
		return 0, 0, false
	}
	return candidates[target].start, candidates[target].end, true
}

func firstIssueCommandLine(commandText string) (string, bool) {
	for _, line := range strings.Split(commandText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, issueCommandPrefix) {
			rest := trimmed[len(issueCommandPrefix):]
			if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
				return trimmed, true
			}
		}
		if trimmed != "" {
			return "", false
		}
	}
	return "", false
}

func countMatchingLines(text, expected string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == expected {
			count++
		}
	}
	return count
}

func matchingLineBounds(body, expected string) []textLineBounds {
	bounds := make([]textLineBounds, 0, 1)
	for offset := 0; offset <= len(body); {
		lineEnd := strings.IndexByte(body[offset:], '\n')
		next := len(body)
		line := body[offset:]
		if lineEnd >= 0 {
			lineEnd += offset
			line = body[offset:lineEnd]
			next = lineEnd + 1
		}

		if strings.TrimSpace(line) == expected {
			bounds = append(bounds, textLineBounds{start: offset, end: next})
		}
		if lineEnd < 0 {
			break
		}
		offset = next
	}
	return bounds
}
