package util

import (
	"strconv"
	"strings"
	"testing"
)

func TestCountLineDiff(t *testing.T) {
	tests := []struct {
		name           string
		old, new       string
		added, removed int
	}{
		{name: "identical", old: "a\nb", new: "a\nb"},
		{name: "both empty", old: "", new: ""},
		{name: "seed from empty", old: "", new: "a\nb\nc", added: 3},
		{name: "cleared", old: "a\nb", new: "", removed: 2},
		{name: "append one line", old: "a\nb", new: "a\nb\nc", added: 1},
		{name: "delete middle", old: "a\nb\nc", new: "a\nc", removed: 1},
		{name: "modify one line", old: "a\nb\nc", new: "a\nB\nc", added: 1, removed: 1},
		{name: "whole rewrite", old: "a\nb\nc", new: "x\ny", added: 2, removed: 3},
		// A trailing newline is not an extra line — the editor's autosave adds
		// and drops it freely and that must not read as a change.
		{name: "trailing newline only", old: "a\nb", new: "a\nb\n"},
		{name: "single blank line", old: "\n", new: "", removed: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			added, removed := CountLineDiff(tc.old, tc.new)
			if added != tc.added || removed != tc.removed {
				t.Errorf("CountLineDiff(%q, %q) = (+%d, -%d), want (+%d, -%d)",
					tc.old, tc.new, added, removed, tc.added, tc.removed)
			}
		})
	}
}

// A moved block must not read as "everything changed": LCS keeps the common
// lines, so only the relocated ones count.
func TestCountLineDiffMovedLine(t *testing.T) {
	added, removed := CountLineDiff("a\nb\nc\nd", "b\nc\nd\na")
	if added != 1 || removed != 1 {
		t.Errorf("got (+%d, -%d), want (+1, -1)", added, removed)
	}
}

// Past lineDiffMaxLines the exact walk is skipped; the approximation must still
// return sane counts rather than panicking or reporting a full rewrite.
func TestCountLineDiffOversizedFallsBack(t *testing.T) {
	var b strings.Builder
	for i := 0; i < lineDiffMaxLines+10; i++ {
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	old := b.String()
	new := old + "extra\n"

	added, removed := CountLineDiff(old, new)
	if added != 1 || removed != 0 {
		t.Errorf("got (+%d, -%d), want (+1, -0)", added, removed)
	}
}
