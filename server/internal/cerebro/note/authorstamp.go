package note

// FIR-2810 review (Jesper, 2026-07-11): a stale-base save whose ONLY change
// is author-code stamps the system inserted must not open the merge dialog —
// nothing a human wrote can be lost by accepting it. Every other stale-base
// change, including the user's own edits from another tab or device, still
// conflicts.

import (
	"regexp"
	"strings"
)

// authorStampRe matches a system-inserted author-code stamp at the start of a
// markdown line's content: optional list/quote decoration, then a bold short
// all-caps token and a space — "**JEH** tekst", "- **JEH** tekst",
// "1. **JEH** tekst". The decoration is captured so stripping preserves it.
var authorStampRe = regexp.MustCompile(`^([ \t]*(?:>[ \t]*)?(?:[-*+•][ \t]+|\d+[.)][ \t]+)?)\*\*[A-ZÆØÅ]{2,4}\*\* `)

// bodyOnlyAddsAuthorStamps reports whether incoming is exactly the server
// body with author-code stamps ADDED on some lines and nothing else changed.
// Stamp removals count as real edits: forgiving them could silently drop a
// stamp (or a hand-typed bold token) someone else just saved.
func bodyOnlyAddsAuthorStamps(serverBody, incoming string) bool {
	if incoming == serverBody {
		return false // a no-op save is handled before the conflict check
	}
	serverLines := strings.Split(serverBody, "\n")
	incomingLines := strings.Split(incoming, "\n")
	if len(serverLines) != len(incomingLines) {
		return false
	}
	for i := range incomingLines {
		if incomingLines[i] == serverLines[i] {
			continue
		}
		stripped := authorStampRe.ReplaceAllString(incomingLines[i], "$1")
		if stripped == incomingLines[i] || stripped != serverLines[i] {
			return false
		}
	}
	return true
}
