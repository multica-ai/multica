// Package mention provides utilities for expanding issue identifier references
// (e.g. MUL-117) into clickable mention links in markdown content.
package mention

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueResolver looks up an issue by workspace and number.
// Implemented by db.Queries.
type IssueResolver interface {
	GetIssueByNumber(ctx context.Context, arg db.GetIssueByNumberParams) (db.Issue, error)
}

// PrefixResolver looks up a workspace to get its issue prefix.
type PrefixResolver interface {
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}

// Resolver combines both interfaces needed for mention expansion.
type Resolver interface {
	IssueResolver
	PrefixResolver
}

// ExpandIssueIdentifiers scans markdown content for bare issue identifier
// patterns (e.g. MUL-117) and replaces them with mention links:
// [MUL-117](mention://issue/<uuid>)
//
// It skips identifiers that are:
//   - Already inside a markdown link: [MUL-117](...)
//   - Inside inline code: `MUL-117`
//   - Inside fenced code blocks: ```...```
func ExpandIssueIdentifiers(ctx context.Context, resolver Resolver, workspaceID pgtype.UUID, content string) string {
	// Get the workspace prefix.
	ws, err := resolver.GetWorkspace(ctx, workspaceID)
	if err != nil || ws.IssuePrefix == "" {
		return content
	}
	prefix := ws.IssuePrefix

	// Build a regex that matches the workspace prefix followed by a hyphen and number.
	// Use word boundaries to avoid matching inside longer strings.
	// The prefix is escaped in case it contains regex-special characters.
	pattern := regexp.MustCompile(`(?:^|(?:\W))` + `(` + regexp.QuoteMeta(prefix) + `-(\d+))` + `(?:\W|$)`)

	// First, identify regions to skip: fenced code blocks and inline code.
	skipRegions := findSkipRegions(content)

	// Find all matches and process from right to left (to preserve offsets).
	allMatches := pattern.FindAllStringSubmatchIndex(content, -1)
	if len(allMatches) == 0 {
		return content
	}

	// Build a set of replacements (offset → replacement string).
	type replacement struct {
		start, end int
		text       string
	}
	var replacements []replacement

	for _, match := range allMatches {
		// match[2:4] is the full identifier (e.g. "MUL-117")
		// match[4:6] is the number part (e.g. "117")
		identStart, identEnd := match[2], match[3]
		numStr := content[match[4]:match[5]]

		// Skip if inside a code region.
		if inSkipRegion(identStart, skipRegions) {
			continue
		}

		// Skip if already inside a markdown link: check if preceded by [
		// or followed by ](...).
		if isInsideMarkdownLink(content, identStart, identEnd) {
			continue
		}

		num, err := strconv.Atoi(numStr)
		if err != nil || num <= 0 {
			continue
		}

		// Look up the issue.
		issue, err := resolver.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
			WorkspaceID: workspaceID,
			Number:      int32(num),
		})
		if err != nil {
			continue // Issue doesn't exist — leave as-is.
		}

		identifier := content[identStart:identEnd]
		issueID := uuidToString(issue.ID)
		mentionLink := fmt.Sprintf("[%s](mention://issue/%s)", identifier, issueID)

		replacements = append(replacements, replacement{
			start: identStart,
			end:   identEnd,
			text:  mentionLink,
		})
	}

	if len(replacements) == 0 {
		return content
	}

	// Apply replacements from right to left to preserve offsets.
	result := content
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		result = result[:r.start] + r.text + result[r.end:]
	}

	return result
}

// skipRegion represents a region of text that should not be modified.
type skipRegion struct {
	start, end int
}

// findSkipRegions identifies fenced code blocks (```) and inline code (`)
// regions in the content.
func findSkipRegions(content string) []skipRegion {
	var regions []skipRegion

	// Fenced code blocks: ```...```
	fenceRe := regexp.MustCompile("(?m)^```[^`]*\n[\\s\\S]*?\n```")
	for _, loc := range fenceRe.FindAllStringIndex(content, -1) {
		regions = append(regions, skipRegion{loc[0], loc[1]})
	}

	// Inline code: `...` (but not inside fenced blocks — already handled).
	inlineRe := regexp.MustCompile("`[^`\n]+`")
	for _, loc := range inlineRe.FindAllStringIndex(content, -1) {
		regions = append(regions, skipRegion{loc[0], loc[1]})
	}

	return regions
}

// inSkipRegion checks if a position falls within any skip region.
func inSkipRegion(pos int, regions []skipRegion) bool {
	for _, r := range regions {
		if pos >= r.start && pos < r.end {
			return true
		}
	}
	return false
}

// isInsideMarkdownLink checks if the text at [start:end] is already part of
// a markdown link like [MUL-117](mention://...) or [text](url).
func isInsideMarkdownLink(content string, start, end int) bool {
	// Check if preceded by '[' (part of link text).
	if start > 0 {
		before := strings.TrimRight(content[:start], " ")
		if len(before) > 0 && before[len(before)-1] == '[' {
			return true
		}
	}
	// Check if followed by '](', indicating it's the link text of a markdown link.
	after := content[end:]
	if strings.HasPrefix(after, "](") {
		return true
	}
	// Check if we're inside the URL part of a link: ...](mention://issue/...).
	// Look backwards for ]( pattern.
	idx := strings.LastIndex(content[:start], "](")
	if idx >= 0 {
		// Check that we haven't passed a closing ) yet.
		between := content[idx:start]
		if !strings.Contains(between, ")") {
			return true
		}
	}
	return false
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
