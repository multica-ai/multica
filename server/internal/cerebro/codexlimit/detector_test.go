package codexlimit

import (
	"strings"
	"testing"
)

func TestIsProviderLimitOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "bare codex limit reply",
			in:   "You've hit your usage limit. Try again at 18:00.",
			want: true,
		},
		{
			name: "codex limit reply with absolute date",
			in:   "You've hit your usage limit. Try again at 2026-05-27 18:00:00 (Europe/Copenhagen).",
			want: true,
		},
		{
			name: "codex limit reply, alternate phrasing",
			in:   "Usage limit reached. Please retry later.",
			want: true,
		},
		{
			name: "anthropic-style monthly cap (kept for cross-runtime reuse)",
			in:   "You've hit your org's monthly usage limit.",
			want: true,
		},
		{
			name: "empty output is not a limit reply",
			in:   "",
			want: false,
		},
		{
			name: "whitespace-only output is not a limit reply",
			in:   "   \n  \t",
			want: false,
		},
		{
			name: "long answer that merely mentions usage limit is real work",
			in:   strings.Repeat("This report explains why a workspace can hit its monthly usage limit without immediately pausing a runtime. ", 8),
			want: false,
		},
		{
			name: "ordinary completion message",
			in:   "Finished the layout updates and verified the screenshots.",
			want: false,
		},
		{
			name: "short note about a 429 is not a provider-limit reply",
			in:   "Got a 429 from the GitHub API while checking CI.",
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsProviderLimitOutput(tc.in); got != tc.want {
				t.Fatalf("IsProviderLimitOutput(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
