package execenv

import (
	"fmt"
	"strings"
)

// Frozen status renderer from a9e82c797, before four lifecycle categories.
// Only the function and category-order identifiers are renamed. Keep the old
// bucketing logic independent of production ParseCategory and category order:
// testing only the latest renderer would miss new-server/old-daemon failures.
var legacyStatusCategoryOrder = []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}

func writeLegacyIssueStatusCommand(b *strings.Builder, ctx TaskContextForEnv) {
	if len(ctx.IssueStatuses) == 0 {
		b.WriteString("- `multica issue status <id> <status> [--no-start]` — flip status (todo / in_progress / in_review / done / blocked / backlog / cancelled).\n")
		return
	}
	byCategory := make(map[string][]IssueStatusForEnv, len(legacyStatusCategoryOrder))
	for _, s := range ctx.IssueStatuses {
		if sanitizeBriefCodeToken(s.Key) == "" {
			continue
		}
		byCategory[s.Category] = append(byCategory[s.Category], s)
	}
	b.WriteString("- `multica issue status <id> <status> [--no-start]` — flip status. This workspace's statuses by category — a custom status inherits its category's platform behavior in full:\n")
	builtInOnly := make([]string, 0, len(legacyStatusCategoryOrder))
	for _, category := range legacyStatusCategoryOrder {
		customs := byCategory[category]
		if len(customs) == 0 {
			builtInOnly = append(builtInOnly, "`"+category+"`")
			continue
		}
		fmt.Fprintf(b, "  - `%s`: `%s` (built-in)", category, category)
		for _, s := range customs {
			name := sanitizeNameForBriefMarkdown(s.Name)
			desc := sanitizeNameForBriefMarkdown(s.Description)
			fmt.Fprintf(b, ", `%s`", sanitizeBriefCodeToken(s.Key))
			switch {
			case name != "" && desc != "":
				fmt.Fprintf(b, " (%s — %s)", name, desc)
			case name != "":
				fmt.Fprintf(b, " (%s)", name)
			}
		}
		b.WriteString("\n")
	}
	if len(builtInOnly) > 0 {
		fmt.Fprintf(b, "  - Built-in key only: %s.\n", strings.Join(builtInOnly, ", "))
	}
	if ctx.IssueStatusesOmitted > 0 {
		fmt.Fprintf(b, "  - …and %d more custom statuses not listed; an invalid status errors with the full valid list.\n", ctx.IssueStatusesOmitted)
	}
}
