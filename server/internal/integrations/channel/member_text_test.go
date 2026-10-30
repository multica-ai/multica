package channel

import "testing"

func TestBreakMarkdownLinkAdjacency(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "inline link with nested parentheses",
			in:   "[重置密码](https://evil.example/reset_(now))",
			want: "[重置密码] (https://evil.example/reset_(now))",
		},
		{
			name: "image-like syntax",
			in:   "![重置密码](https://evil.example/image.png)",
			want: "![重置密码] (https://evil.example/image.png)",
		},
		{
			name: "reference definition unchanged",
			in:   "[重置密码]: https://evil.example\n\n[重置密码]",
			want: "[重置密码]: https://evil.example\n\n[重置密码]",
		},
		{name: "ordinary CJK title", in: "修复登录失败", want: "修复登录失败"},
		{name: "ordinary English title", in: "Fix login failure", want: "Fix login failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := BreakMarkdownLinkAdjacency(tc.in)
			if got != tc.want {
				t.Fatalf("BreakMarkdownLinkAdjacency(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if twice := BreakMarkdownLinkAdjacency(got); twice != got {
				t.Fatalf("second pass changed the guarded text:\n once %q\ntwice %q", got, twice)
			}
		})
	}
}
