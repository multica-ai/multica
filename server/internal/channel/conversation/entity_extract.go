// Package conversation owns provider-neutral message entity extraction helpers.
//
// Responsibilities:
//   - Detect issue identifiers embedded in channel message text.
//   - Convert detected identifiers into message entity references.
//   - Keep extraction deterministic by deduplicating in first-seen order.
//
// Boundaries:
//   - Does not validate that an identifier exists in the issue table.
//   - Does not call external services or infer entities beyond text matches.
package conversation

import (
	"regexp"
	"strings"
)

// issueKeyRe matches issue identifiers embedded in natural-language text.
// Format: 2-5 letters, hyphen, positive integer without leading zeros.
var issueKeyRe = regexp.MustCompile(`(?i)\b[A-Z]{2,5}-[1-9][0-9]*\b`)

// ExtractIssueEntityRefs scans text for issue identifiers and returns message
// entity references.
//
// Parameters:
//   - workspaceID: optional workspace id attached to each entity ref.
//   - text: free-form channel message text.
//   - role: entity role; defaults to mentioned when empty.
//
// Returns:
//   - issue entity references, deduplicated while preserving first-seen order.
func ExtractIssueEntityRefs(workspaceID, text, role string) []EntityRef {
	matches := issueKeyRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	if strings.TrimSpace(role) == "" {
		role = EntityRoleMentioned
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]EntityRef, 0, len(matches))
	for _, match := range matches {
		key := strings.ToUpper(strings.TrimSpace(match))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, EntityRef{
			WorkspaceID: workspaceID,
			EntityType:  EntityTypeIssue,
			EntityKey:   key,
			Display:     key,
			Role:        role,
		})
	}
	return out
}
