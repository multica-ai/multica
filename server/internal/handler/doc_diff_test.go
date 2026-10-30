package handler

import "testing"

func TestDocLineStats(t *testing.T) {
	cases := []struct {
		name           string
		before, after  string
		added, removed int
	}{
		{"create", "", "# Title\n\nbody", 2, 0},
		{"clear", "a\nb", "", 0, 2},
		{"unchanged", "a\n\nb", "a\n\nb", 0, 0},
		{"blank lines ignored", "a\nb", "a\n\n\nb", 0, 0},
		{"edit one line", "a\nb\nc", "a\nB\nc", 1, 1},
		{"insert middle", "a\nc", "a\nb\nc", 1, 0},
		{"remove and add", "a\nb\nc\nd", "a\nc\nd\ne", 1, 1},
		{"crlf", "a\r\nb", "a\nb", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			added, removed := docLineStats(tc.before, tc.after)
			if added != tc.added || removed != tc.removed {
				t.Fatalf("docLineStats = +%d -%d, want +%d -%d", added, removed, tc.added, tc.removed)
			}
		})
	}
}

func TestMultisetLineStats(t *testing.T) {
	added, removed := multisetLineStats([]string{"a", "b", "b"}, []string{"b", "c"})
	if added != 1 || removed != 2 {
		t.Fatalf("multisetLineStats = +%d -%d, want +1 -2", added, removed)
	}
}
