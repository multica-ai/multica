package qianwen

import "testing"

func TestDeriveQianwenSessionTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "first meaningful line and markdown",
			query: "\n  ## 修复登录失败 [日志](https://example.test/log)  \n忽略后续说明",
			want:  "修复登录失败 日志",
		},
		{
			name:  "collapse whitespace",
			query: "inspect\t  the   build",
			want:  "inspect the build",
		},
		{
			name:  "unicode rune cap",
			query: "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一",
			want:  "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九…",
		},
		{
			name:  "formatting only falls back in session service",
			query: "  ### ***  ",
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := deriveQianwenSessionTitle(tc.query); got != tc.want {
				t.Fatalf("deriveQianwenSessionTitle(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}
