package engine

import "testing"

func TestDeriveChatTitle(t *testing.T) {
	tests := []struct{ input, want string }{
		{"\n#  发布检查\n后续", "发布检查"},
		{"[部署文档](https://example.com) **失败**", "部署文档 失败"},
		{"![架构图](https://example.com/a.png)", "架构图"},
		{"一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三", "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九…"},
		{"12345678901234567890123456 890123", "12345678901234567890123456 89…"},
	}
	for _, tc := range tests {
		if got := DeriveChatTitle(tc.input); got != tc.want {
			t.Errorf("DeriveChatTitle(%q)=%q want %q", tc.input, got, tc.want)
		}
	}
}

func TestDeriveFirstMessageTitle_MediaPlaceholderWaitsForFilename(t *testing.T) {
	if got := deriveFirstMessageTitle("[Image]\n[File]", true); got != "" {
		t.Fatalf("placeholder-only media title = %q, want empty until media metadata is available", got)
	}
	if got := deriveFirstMessageTitle("[Image]\n点评一下", true); got != "点评一下" {
		t.Fatalf("media-before-text title = %q, want user text", got)
	}
	if got := deriveFirstMessageTitle("Inspect this\n[Image]", true); got != "Inspect this" {
		t.Fatalf("text plus media title = %q, want user text", got)
	}
	if got := deriveFirstMessageTitle("Use [Image] literally\n[Image]", true); got != "Use [Image] literally" {
		t.Fatalf("inline literal placeholder title = %q, want user text preserved", got)
	}
	if got := deriveFirstMessageTitle("[Image]", false); got != "[Image]" {
		t.Fatalf("literal text title = %q, want the user-authored text", got)
	}
}

func TestChatTitleSourceConsumesOnlyAppliedFreshDirective(t *testing.T) {
	for _, tc := range []struct {
		name, body, command, want string
		fresh                     bool
	}{
		{"enriched fresh", "<quoted_message>history</quoted_message>\n\nanswer", "/clear answer", "answer", true},
		{"nested fresh", "/clear answer", "/clear /clear answer", "/clear answer", true},
		{"new body is literal", "<recent_context>history</recent_context>\n\n/clear answer", "/clear answer", "/clear answer", false},
		{"native fresh", "<recent_context>history</recent_context>\n\nanswer", "answer", "answer", true},
		{"media fresh", "[Image]", "/clear", "", true},
		{"missing current text", "body fallback", "  ", "body fallback", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatTitleSource(tc.body, tc.command, tc.fresh); got != tc.want {
				t.Fatalf("title source = %q, want %q", got, tc.want)
			}
		})
	}
}
