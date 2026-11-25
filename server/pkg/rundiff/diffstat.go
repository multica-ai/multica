package rundiff

import (
	"strings"
)

// Line-count statistics for a captured file change. Counts are computed at
// finish from the same before/after texts that are attached to the File, so
// the numbers always describe exactly what the diff view renders — including
// redaction, which substitutes in place and preserves line structure. A side
// that is binary, oversized, or otherwise not attached yields no trustworthy
// count: the caller leaves the file's stats unset and marks the collection
// totals incomplete instead of guessing.

// splitLines splits s into lines. A trailing newline does not produce a final
// empty line, matching git's counting of a file whose last line is terminated.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	out := strings.Split(s, "\n")
	return out
}

// lcsLength returns the length of the longest common subsequence of a and b
// using the two-row dynamic program. ok=false reports that the pairwise cell
// budget was exhausted (two large dissimilar sides); the caller then has no
// trustworthy count. Callers must trim common prefixes/suffixes first so a
// localized edit inside a large file only pays for the changed middle.
func lcsLength(a, b []string, budget int64) (int, bool) {
	if int64(len(a))*int64(len(b)) > budget {
		return 0, false
	}
	if len(a) == 0 || len(b) == 0 {
		return 0, true
	}
	prev := make([]int32, len(b)+1)
	cur := make([]int32, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return int(prev[len(b)]), true
}

// diffLineCounts counts inserted and deleted lines between the attached
// before and after bodies (either side may be nil for an added or deleted
// file). ok=false means at least one side is missing or the diff budget was
// exhausted; the caller must not publish a count in that case.
func diffLineCounts(before, after *string) (ins, del int64, ok bool) {
	switch {
	case before == nil && after == nil:
		return 0, 0, false
	case before == nil:
		return int64(len(splitLines(*after))), 0, true
	case after == nil:
		return 0, int64(len(splitLines(*before))), true
	}
	a := splitLines(*before)
	b := splitLines(*after)
	// Trim the common prefix and suffix so the DP covers only the changed
	// middle of an otherwise similar pair of files.
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	a = a[p : len(a)-s]
	b = b[p : len(b)-s]
	lcs, ok := lcsLength(a, b, maxDiffStatCells)
	if !ok {
		return 0, 0, false
	}
	return int64(len(b) - lcs), int64(len(a) - lcs), true
}
