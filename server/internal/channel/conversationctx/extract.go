package conversationctx

import (
	"regexp"
	"strings"
	"time"
)

// issueKeyRe matches issue identifiers embedded in natural-language text.
// Format: 2-5 letters, hyphen, positive integer (no leading zeros).
var issueKeyRe = regexp.MustCompile(`(?i)\b[A-Z]{2,5}-[1-9][0-9]*\b`)

// ExtractEntityKeys scans text for issue identifiers and returns them as
// EntityRef values. Duplicates are deduplicated while preserving first-seen
// order.
func ExtractEntityKeys(text string) []EntityRef {
	matches := issueKeyRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]EntityRef, 0, len(matches))
	now := time.Now()
	for _, match := range matches {
		key := strings.ToUpper(match)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, EntityRef{
			Key:         key,
			Type:        EntityTypeIssue,
			MentionedAt: now,
		})
	}
	return out
}
