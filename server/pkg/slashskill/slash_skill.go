// Package slashskill parses the durable Markdown marker emitted by Multica's
// slash-command editor. Both the claim handler and the daemon prompt consume
// this package so UI selection, task authorization, and prompt disclosure use
// one parser rather than security-sensitive lookalikes.
package slashskill

import (
	"regexp"
	"strings"
)

// MaxSelectedPerPayload is the server-owned ceiling for one task-scoped
// Skill selection. The UI mirrors this value for display, while every backend
// admission path uses it as the authoritative cap.
const MaxSelectedPerPayload = 20

var markerRE = regexp.MustCompile(
	`\[/((?:[^\]\\]|\\.)+)\]\(slash://skill/([^)]+)\)`,
)

type Ref struct {
	Label string
	ID    string
}

// Extract returns distinct Skill refs in first-occurrence order.
func Extract(markdown string) []Ref {
	matches := markerRE.FindAllStringSubmatch(markdown, -1)
	seen := make(map[string]struct{}, len(matches))
	refs := make([]Ref, 0, len(matches))

	for _, match := range matches {
		id := match[2]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		label := strings.ReplaceAll(match[1], `\[`, "[")
		label = strings.ReplaceAll(label, `\]`, "]")
		refs = append(refs, Ref{Label: label, ID: id})
	}

	return refs
}
