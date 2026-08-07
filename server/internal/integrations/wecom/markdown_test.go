package wecom

import (
	"strings"
	"testing"
)

// TestBreakLinkAdjacency covers the whole contract: the only edit is a space
// between "]" and "(", nothing else in the string moves, and no backslash is
// ever produced — a backslash is what the previous mechanism emitted and what
// the live tenant showed to be unusable.
func TestBreakLinkAdjacency(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary bracketed title is untouched", "[Bug] 登录失败", "[Bug] 登录失败"},
		{"parens alone are untouched", "修复 (见 issue 12)", "修复 (见 issue 12)"},
		{"a lone bracket is untouched", "开了一个 [ 没关", "开了一个 [ 没关"},
		{"exclamation alone is untouched", "紧急!!!", "紧急!!!"},
		{"member backslash is left exactly as written", `修复路径 C:\`, `修复路径 C:\`},
		{"link", "[click here](http://evil.example)", "[click here] (http://evil.example)"},
		{"image", "![img](http://evil.example/x.png)", "![img] (http://evil.example/x.png)"},
		{"nested brackets", "[a[b]](http://evil.example)", "[a[b]] (http://evil.example)"},
		{"back to back", "](](", "] (] ("},
		{"two links", "[a](x) and [b](y)", "[a] (x) and [b] (y)"},
		{"backslash in front of the pair does not help the member", `x\](u)`, `x\] (u)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := breakLinkAdjacency(tc.in)
			if got != tc.want {
				t.Fatalf("breakLinkAdjacency(%q):\n got %q\nwant %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "](") {
				t.Fatalf("%q still holds an adjacent \"](\" — a link can still form", got)
			}
			if !strings.Contains(tc.in, `\`) && strings.Contains(got, `\`) {
				t.Fatalf("emitted a backslash for %q: %q — WeCom reads \"\\[\" as a math delimiter", tc.in, got)
			}
		})
	}
}

// TestBreakLinkAdjacencyIsIdempotent: the output contains no "](", so a second
// pass must be a no-op. Matters because the same text can reach more than one
// builder, and a mechanism that kept inserting spaces would drift.
func TestBreakLinkAdjacencyIsIdempotent(t *testing.T) {
	for _, in := range []string{"[a](b)", "](](", "[Bug] 登录失败", "![i](u)"} {
		once := breakLinkAdjacency(in)
		if twice := breakLinkAdjacency(once); twice != once {
			t.Fatalf("second pass changed %q: %q → %q", in, once, twice)
		}
	}
}
