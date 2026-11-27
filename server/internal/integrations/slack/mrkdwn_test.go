package slack

import (
	"strings"
	"testing"
)

func TestFormatMrkdwn(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain unchanged", "just a reply", "just a reply"},
		{"empty", "", ""},
		{"bold", "**bold**", "*bold*"},
		{"italic star to underscore", "*italic*", "_italic_"},
		{"underscore italic kept", "_italic_", "_italic_"},
		{"bold italic", "***both***", "*_both_*"},
		{"strikethrough", "~~gone~~", "~gone~"},
		{"header to bold", "## Title", "*Title*"},
		{"header strips inner bold", "### **Big**", "*Big*"},
		{"markdown link", "see [docs](https://x.com/a)", "see <https://x.com/a|docs>"},
		{"image link untouched", "![alt](https://x.com/i.png)", "![alt](https://x.com/i.png)"},
		{"inline code protected", "use `**not bold**` here", "use `**not bold**` here"},
		{"existing slack mention untouched", "hi <@U123>", "hi <@U123>"},
		{"ampersand and angles escaped", "a & b < c > d", "a &amp; b &lt; c &gt; d"},
		{"blockquote preserved", "> quoted line", "> quoted line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatMrkdwn(tc.in); got != tc.want {
				t.Errorf("formatMrkdwn(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatMrkdwn_FencedCodeProtected(t *testing.T) {
	in := "before\n```\n**stars** and [x](y) stay literal\n```\nafter **bold**"
	got := formatMrkdwn(in)
	want := "before\n```\n**stars** and [x](y) stay literal\n```\nafter *bold*"
	if got != want {
		t.Errorf("fenced code must be protected while outside text converts:\n got=%q\nwant=%q", got, want)
	}
}

func TestFormatMrkdwn_LinkInsideBold(t *testing.T) {
	// A link nested in bold: the link converts and survives the bold pass.
	got := formatMrkdwn("**see [docs](https://x.com)**")
	want := "*see <https://x.com|docs>*"
	if got != want {
		t.Errorf("formatMrkdwn nested link+bold = %q, want %q", got, want)
	}
}

func TestFormatMrkdwn_LinkControlCharsEscaped(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		want       string
		idempotent bool
	}{
		{
			name:       "greater than in label",
			in:         "see [a > b](https://x.com)",
			want:       "see <https://x.com|a &gt; b>",
			idempotent: true,
		},
		{
			name:       "less than in label",
			in:         "see [a < b](https://x.com)",
			want:       "see <https://x.com|a &lt; b>",
			idempotent: true,
		},
		{
			name:       "ampersand in url and label",
			in:         "[R&D](https://x.com/search?a=1&b=2)",
			want:       "<https://x.com/search?a=1&amp;b=2|R&amp;D>",
			idempotent: true,
		},
		{
			name:       "pre-escaped entity not double escaped",
			in:         "[a &gt; b](https://x.com/?a=1&amp;b=2)",
			want:       "<https://x.com/?a=1&amp;b=2|a &gt; b>",
			idempotent: true,
		},
		{
			name:       "pipe in url encoded",
			in:         "[query](https://x.com/?q=a|b)",
			want:       "<https://x.com/?q=a%7Cb|query>",
			idempotent: true,
		},
		{
			name:       "inline code with angle brackets inside link label",
			in:         "[check `a > b`](https://x.com)",
			want:       "<https://x.com|check `a &gt; b`>",
			idempotent: true,
		},
		{
			name: "link with control chars inside bold",
			in:   "**see [R&D > ops](https://x.com/?a=1&b=2)**",
			want: "*see <https://x.com/?a=1&amp;b=2|R&amp;D &gt; ops>*",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatMrkdwn(tc.in)
			if got != tc.want {
				t.Errorf("formatMrkdwn(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if tc.idempotent {
				if twice := formatMrkdwn(got); twice != got {
					t.Errorf("formatMrkdwn not idempotent:\n once=%q\ntwice=%q", got, twice)
				}
			}
		})
	}
}

func TestFormatMrkdwn_MultiBacktickAndFencedCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "double backtick inline code with inner backticks and bold",
			in:   "use ``a `**bold**` b`` and **outside**",
			want: "use ``a `**bold**` b`` and *outside*",
		},
		{
			name: "inline triple backtick span at line start",
			in:   "```**not bold**``` and **bold**",
			want: "```**not bold**``` and *bold*",
		},
		{
			name: "four backtick outer fence protects inner three backtick fence",
			in:   "````markdown\n```go\n**not bold**\n```\n````\n## After",
			want: "````markdown\n```go\n**not bold**\n```\n````\n*After*",
		},
		{
			name: "tilde fence protects bold and strikethrough",
			in:   "~~~go\n**not bold** ~~not strike~~\n~~~\n> quote",
			want: "~~~go\n**not bold** ~~not strike~~\n~~~\n> quote",
		},
		{
			name: "unclosed fenced code block protects to EOF",
			in:   "```go\n**not bold**\n[link](https://x.com)",
			want: "```go\n**not bold**\n[link](https://x.com)",
		},
		{
			name: "unmatched backtick does not swallow next paragraph",
			in:   "typo `here\n\n**bold** and `code`",
			want: "typo `here\n\n*bold* and `code`",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatMrkdwn(tc.in); got != tc.want {
				t.Errorf("formatMrkdwn(%q) =\n got=%q\nwant=%q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatMrkdwn_SentinelLiteralInInput(t *testing.T) {
	in := "literal \x00SL0\x00 and `code` and **bold**"
	want := "literal \x00SL0\x00 and `code` and *bold*"
	if got := formatMrkdwn(in); got != want {
		t.Errorf("formatMrkdwn(%q) = %q, want %q", in, got, want)
	}
}

func BenchmarkFormatMrkdwn_ManyPlaceholders(b *testing.B) {
	in := strings.Repeat("`a` **b** ", 2000)
	b.ResetTimer()
	for b.Loop() {
		_ = formatMrkdwn(in)
	}
}

func BenchmarkFormatMrkdwn_UnclosedBackticks(b *testing.B) {
	var sb strings.Builder
	for i := 1; i <= 200; i++ {
		sb.WriteString(strings.Repeat("`", i))
		sb.WriteByte('x')
	}
	in := sb.String()
	b.ResetTimer()
	for b.Loop() {
		_ = formatMrkdwn(in)
	}
}
