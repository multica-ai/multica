package handler

import "strings"

// docDiffCellBudget caps the LCS table walked by docLineStats. Edits trim to
// a small middle window, so real saves stay far below it; a full rewrite of a
// huge doc falls back to a multiset count, which is exact for pure adds and
// removes and only approximates reordered lines.
const docDiffCellBudget = 4_000_000

// docLineStats returns how many non-blank lines a write added and removed.
// It only feeds the "+N −M" summary on a version, so blank lines (paragraph
// spacing in Markdown) are ignored to keep the numbers meaningful.
func docLineStats(before, after string) (added, removed int) {
	a := nonBlankLines(before)
	b := nonBlankLines(after)

	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	if len(a) == 0 || len(b) == 0 {
		return len(b), len(a)
	}
	if len(a)*len(b) > docDiffCellBudget {
		return multisetLineStats(a, b)
	}
	common := lcsLength(a, b)
	return len(b) - common, len(a) - common
}

func nonBlankLines(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := raw[:0]
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func lcsLength(a, b []string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func multisetLineStats(a, b []string) (added, removed int) {
	counts := make(map[string]int, len(a))
	for _, line := range a {
		counts[line]++
	}
	for _, line := range b {
		if counts[line] > 0 {
			counts[line]--
		} else {
			added++
		}
	}
	for _, n := range counts {
		removed += n
	}
	return added, removed
}
