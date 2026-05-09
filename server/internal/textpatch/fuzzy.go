// Package textpatch implements fuzzy find-and-replace for text content.
//
// Ported from hermes-agent/tools/fuzzy_match.py. Uses a multi-strategy
// matching chain to accommodate whitespace and indentation variations
// common in LLM-generated text.
package textpatch

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrNotFound       = errors.New("could not find a match for the search text")
	ErrAmbiguous      = errors.New("multiple matches found; provide more context to make the search text unique")
	ErrEmptySearch    = errors.New("search text cannot be empty")
	ErrIdentical      = errors.New("search and replacement text are identical")
)

// Result holds the outcome of a fuzzy find-and-replace operation.
type Result struct {
	Content  string // the new content after replacement
	Strategy string // name of the strategy that matched
}

// FuzzyReplace finds oldText in content using a chain of increasingly
// fuzzy strategies and replaces the first (and only) match with newText.
// Returns ErrNotFound if no strategy matches and ErrAmbiguous if more
// than one match is found.
func FuzzyReplace(content, oldText, newText string) (Result, error) {
	if oldText == "" {
		return Result{}, ErrEmptySearch
	}
	if oldText == newText {
		return Result{}, ErrIdentical
	}

	type strategy struct {
		name string
		fn   func(content, pattern string) []match
	}

	strategies := []strategy{
		{"exact", strategyExact},
		{"line_trimmed", strategyLineTrimmed},
		{"whitespace_normalized", strategyWhitespaceNormalized},
		{"indentation_flexible", strategyIndentationFlexible},
	}

	for _, s := range strategies {
		matches := s.fn(content, oldText)
		if len(matches) == 0 {
			continue
		}
		if len(matches) > 1 {
			return Result{}, fmt.Errorf("%w: found %d matches using strategy %q",
				ErrAmbiguous, len(matches), s.name)
		}
		m := matches[0]
		result := content[:m.start] + newText + content[m.end:]
		return Result{Content: result, Strategy: s.name}, nil
	}

	return Result{}, ErrNotFound
}

// match represents a (start, end) byte position in the content.
type match struct {
	start, end int
}

// strategyExact finds all exact occurrences of pattern in content.
func strategyExact(content, pattern string) []match {
	var matches []match
	start := 0
	for {
		idx := strings.Index(content[start:], pattern)
		if idx == -1 {
			break
		}
		pos := start + idx
		matches = append(matches, match{pos, pos + len(pattern)})
		start = pos + 1
	}
	return matches
}

// strategyLineTrimmed strips leading/trailing whitespace from each line
// before matching, then maps back to original positions.
func strategyLineTrimmed(content, pattern string) []match {
	contentLines := strings.Split(content, "\n")
	patternLines := strings.Split(pattern, "\n")

	normContent := make([]string, len(contentLines))
	for i, l := range contentLines {
		normContent[i] = strings.TrimSpace(l)
	}
	normPattern := make([]string, len(patternLines))
	for i, l := range patternLines {
		normPattern[i] = strings.TrimSpace(l)
	}

	return findNormalizedLineMatches(contentLines, normContent, normPattern, len(content))
}

// strategyWhitespaceNormalized collapses runs of spaces/tabs to a single
// space before matching.
var wsCollapseRe = regexp.MustCompile(`[ \t]+`)

func strategyWhitespaceNormalized(content, pattern string) []match {
	normContent := wsCollapseRe.ReplaceAllString(content, " ")
	normPattern := wsCollapseRe.ReplaceAllString(pattern, " ")

	normMatches := strategyExact(normContent, normPattern)
	if len(normMatches) == 0 {
		return nil
	}

	return mapNormalizedPositions(content, normContent, normMatches)
}

// strategyIndentationFlexible strips all leading whitespace from lines.
func strategyIndentationFlexible(content, pattern string) []match {
	contentLines := strings.Split(content, "\n")
	patternLines := strings.Split(pattern, "\n")

	normContent := make([]string, len(contentLines))
	for i, l := range contentLines {
		normContent[i] = strings.TrimLeft(l, " \t")
	}
	normPattern := make([]string, len(patternLines))
	for i, l := range patternLines {
		normPattern[i] = strings.TrimLeft(l, " \t")
	}

	return findNormalizedLineMatches(contentLines, normContent, normPattern, len(content))
}

// findNormalizedLineMatches searches for a block of normalized pattern lines
// within normalized content lines and maps matches back to byte positions
// in the original content lines.
func findNormalizedLineMatches(
	origLines, normLines, normPatternLines []string,
	contentLen int,
) []match {
	numPattern := len(normPatternLines)
	if numPattern == 0 || len(normLines) < numPattern {
		return nil
	}

	normPatternJoined := strings.Join(normPatternLines, "\n")
	var matches []match

	for i := 0; i <= len(normLines)-numPattern; i++ {
		block := strings.Join(normLines[i:i+numPattern], "\n")
		if block == normPatternJoined {
			startPos := lineStartOffset(origLines, i)
			endPos := lineEndOffset(origLines, i+numPattern-1, contentLen)
			matches = append(matches, match{startPos, endPos})
		}
	}
	return matches
}

// lineStartOffset returns the byte offset of line i within the original
// content (lines joined by "\n").
func lineStartOffset(lines []string, i int) int {
	offset := 0
	for j := 0; j < i; j++ {
		offset += len(lines[j]) + 1 // +1 for "\n"
	}
	return offset
}

// lineEndOffset returns the byte offset just past line i (inclusive of
// the line content but not the trailing newline, unless it's the last line).
func lineEndOffset(lines []string, i, contentLen int) int {
	offset := lineStartOffset(lines, i) + len(lines[i])
	if offset > contentLen {
		return contentLen
	}
	return offset
}

// mapNormalizedPositions maps match positions from a whitespace-normalized
// string back to the original string.
func mapNormalizedPositions(original, normalized string, normMatches []match) []match {
	// Build mapping: for each byte in original, what's the corresponding
	// byte in normalized.
	origToNorm := make([]int, len(original)+1)
	oi, ni := 0, 0
	for oi < len(original) && ni < len(normalized) {
		origToNorm[oi] = ni
		if original[oi] == normalized[ni] {
			oi++
			ni++
		} else if (original[oi] == ' ' || original[oi] == '\t') && normalized[ni] == ' ' {
			oi++
			if oi < len(original) && original[oi] != ' ' && original[oi] != '\t' {
				ni++
			}
		} else if original[oi] == ' ' || original[oi] == '\t' {
			origToNorm[oi] = ni
			oi++
		} else {
			oi++
			ni++
		}
	}
	for ; oi <= len(original); oi++ {
		origToNorm[oi] = ni
	}

	// Inverse map: norm pos → first orig pos
	normToOrigStart := make(map[int]int)
	normToOrigEnd := make(map[int]int)
	for op, np := range origToNorm[:len(original)] {
		if _, ok := normToOrigStart[np]; !ok {
			normToOrigStart[np] = op
		}
		normToOrigEnd[np] = op
	}

	var results []match
	for _, nm := range normMatches {
		origStart, ok := normToOrigStart[nm.start]
		if !ok {
			continue
		}
		origEnd := origStart + (nm.end - nm.start)
		if end, ok := normToOrigEnd[nm.end-1]; ok {
			origEnd = end + 1
		}
		// Expand to include trailing whitespace that was collapsed
		for origEnd < len(original) && (original[origEnd] == ' ' || original[origEnd] == '\t') {
			origEnd++
		}
		if origEnd > len(original) {
			origEnd = len(original)
		}
		results = append(results, match{origStart, origEnd})
	}
	return results
}
