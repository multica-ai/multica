package comments

import (
	"strings"
	"testing"
)

func TestDeriveTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain first line", "Det her er en kommentar.\nAnden linje skal ignoreres.", "Det her er en kommentar."},
		{"strips leading blank lines", "\n\n\nFørste rigtige linje", "Første rigtige linje"},
		{"collapses agent mention", "[@Tine](mention://agent/abc) kig lige på det", "@Tine kig lige på det"},
		{"collapses issue mention", "Linket [JEH-42](mention://issue/uuid-here) er relevant", "Linket JEH-42 er relevant"},
		{"keeps external markdown links intact", "se [docs](https://example.com)", "se [docs](https://example.com)"},
		{"empty content yields empty title", "   \n\t\n   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveTitle(tc.in)
			if got != tc.want {
				t.Fatalf("deriveTitle(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDeriveTitleTruncatesLongLine(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := deriveTitle(long)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis on long title, got %q", got)
	}
	if r := []rune(got); len(r) > 101 {
		t.Fatalf("truncated title too long: %d runes", len(r))
	}
}

func TestCollapseMentionLinks(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hej [@Sara](mention://member/1) og [@Bot](mention://agent/2)", "hej @Sara og @Bot"},
		{"[JEH-1](mention://issue/abc) er færdig", "JEH-1 er færdig"},
		{"se [docs](https://example.com)", "se [docs](https://example.com)"},
		{"unclosed [bracket without paren", "unclosed [bracket without paren"},
		{"empty", "empty"},
	}
	for _, tc := range cases {
		got := collapseMentionLinks(tc.in)
		if got != tc.want {
			t.Errorf("collapseMentionLinks(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
		}
	}
}
