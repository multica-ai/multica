package util

import "strings"

// lineDiffMaxLines bounds the LCS table walk. Descriptions are prose measured in
// tens of lines; anything past this is pathological input (a pasted log, a
// generated dump) and not worth a quadratic walk. Past the bound we fall back to
// a multiset comparison, which is exact for the common "whole file replaced"
// shape and only loses precision on reordered duplicates.
const lineDiffMaxLines = 4000

// CountLineDiff returns how many lines were added and removed going from `old`
// to `new`, using the same longest-common-subsequence semantics the frontend's
// jsdiff renderer uses. Counts are what the timeline badge shows, so both sides
// must agree on them.
//
// A trailing newline does not count as an extra empty line: "a\n" and "a" are
// the same single line.
func CountLineDiff(old, new string) (added, removed int) {
	if old == new {
		return 0, 0
	}
	oldLines := splitLines(old)
	newLines := splitLines(new)

	if len(oldLines) > lineDiffMaxLines || len(newLines) > lineDiffMaxLines {
		return countLineDiffApprox(oldLines, newLines)
	}

	common := lcsLength(oldLines, newLines)
	return len(newLines) - common, len(oldLines) - common
}

// splitLines splits on "\n" and drops the empty element a trailing newline
// produces. An empty string is zero lines, not one.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		// The input was exactly "\n" — one empty line.
		return []string{""}
	}
	return strings.Split(s, "\n")
}

// lcsLength returns the length of the longest common subsequence of two line
// slices. Only two rows of the DP table are kept, so memory is O(min(n, m))
// rather than O(n*m) — the table itself is never needed because we want the
// length, not the alignment.
func lcsLength(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	// Iterate with the shorter slice as the row so the rows stay small.
	if len(b) > len(a) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else if prev[j] >= curr[j-1] {
				curr[j] = prev[j]
			} else {
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// countLineDiffApprox compares line multisets. Every line present in both (up to
// multiplicity) is treated as unchanged; the remainder is added or removed. This
// ignores order, so a pure reordering reads as "no change" — acceptable for the
// oversized inputs it is reserved for.
func countLineDiffApprox(oldLines, newLines []string) (added, removed int) {
	counts := make(map[string]int, len(oldLines))
	for _, line := range oldLines {
		counts[line]++
	}
	common := 0
	for _, line := range newLines {
		if counts[line] > 0 {
			counts[line]--
			common++
		}
	}
	return len(newLines) - common, len(oldLines) - common
}
