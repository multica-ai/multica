package relatedcheck

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/duplicatecheck"
)

// RenderComment turns a result into the comment body posted on the issue.
// Returns "" when there is nothing new to say, so callers can stay silent on
// repeat checks instead of commenting on every assignment.
//
// App content is English by house rule, including this generated body.
func RenderComment(res Result) string {
	if len(res.Matches) == 0 || !res.HasNews() {
		return ""
	}

	var b strings.Builder
	if res.Duplicate != nil {
		b.WriteString("**This issue looks like a duplicate.**\n\n")
	} else {
		b.WriteString("**Related work already exists.**\n\n")
	}

	for _, m := range res.Matches {
		b.WriteString(fmt.Sprintf("- %s — %s: %s (%s)\n",
			capitalize(m.Verdict), IssueLink(m.Identifier, m.ID), m.Title, m.Status))
		if m.Reason != "" {
			b.WriteString(fmt.Sprintf("  %s\n", m.Reason))
		}
	}

	b.WriteString("\nLinked as related on both issues.")
	if res.Duplicate != nil {
		b.WriteString(" Check with the issue owner before starting work, so the same thing is not built twice.")
	}
	return b.String()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// IssueLink renders a clickable issue mention. Issue mentions have no side
// effect — unlike member and agent mentions, they notify nobody.
func IssueLink(identifier, id string) string {
	if identifier == "" {
		identifier = "issue"
	}
	return fmt.Sprintf("[%s](mention://issue/%s)", identifier, id)
}

// Summary is the one-line form used in CLI output and hook run records.
func Summary(res Result) string {
	if res.Skipped != "" {
		return "no check: " + res.Skipped
	}
	if len(res.Matches) == 0 {
		return fmt.Sprintf("no match (%d candidates scanned)", res.Candidates)
	}
	duplicates := 0
	for _, m := range res.Matches {
		if m.Verdict == string(duplicatecheck.VerdictDuplicate) {
			duplicates++
		}
	}
	return fmt.Sprintf("%d match(es), %d duplicate(s), %d new link(s)", len(res.Matches), duplicates, res.NewLinks)
}
