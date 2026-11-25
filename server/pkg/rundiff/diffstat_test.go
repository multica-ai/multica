package rundiff

import (
	"strings"
	"testing"
)

func TestDiffLineCounts(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name          string
		before, after *string
		ins, del      int64
		ok            bool
	}{
		{"identical", s("a\nb\n"), s("a\nb\n"), 0, 0, true},
		{"trailing newline does not add a line", s("a\n"), s("a\n"), 0, 0, true},
		{"no trailing newline", s("a\nb"), s("a\nb"), 0, 0, true},
		{"one line edited", s("a\nb\nc\n"), s("a\nX\nc\n"), 1, 1, true},
		{"lines appended", s("a\n"), s("a\nb\nc\n"), 2, 0, true},
		{"lines removed", s("a\nb\nc\n"), s("a\n"), 0, 2, true},
		{"prefix and suffix trim keeps counts exact", s("x\nkeep\nmiddle\ny\n"), s("x\nkeep\nMIDDLE\ny\n"), 1, 1, true},
		{"reorder counts as del plus ins", s("a\nb\n"), s("b\na\n"), 1, 1, true},
		{"duplicate lines stay line-based", s("a\na\n"), s("a\n"), 0, 1, true},
		{"crlf consistent on both sides", s("a\r\nb\r\n"), s("a\r\nB\r\n"), 1, 1, true},
		{"empty to content", nil, s("a\n"), 1, 0, true},
		{"content to empty", s("a\nb\n"), nil, 0, 2, true},
		{"both sides missing", nil, nil, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ins, del, ok := diffLineCounts(tc.before, tc.after)
			if !ok {
				if tc.ok {
					t.Fatalf("unexpected budget failure")
				}
				return
			}
			if ins != tc.ins || del != tc.del {
				t.Fatalf("got ins=%d del=%d, want ins=%d del=%d", ins, del, tc.ins, tc.del)
			}
		})
	}
}

func TestDiffLineCountsBudgetExhausted(t *testing.T) {
	// Two large dissimilar sides blow the cell budget: no count is published
	// rather than a wrong one.
	line := strings.Repeat("x", 8) + "\n"
	var b strings.Builder
	var a strings.Builder
	for i := 0; i < 20000; i++ {
		b.WriteString(line)
		a.WriteString(strings.Repeat("y", 8) + "\n")
	}
	before, after := b.String(), a.String()
	if _, _, ok := diffLineCounts(&before, &after); ok {
		t.Fatal("budget-exceeding diff reported success")
	}
}
