package slack

import "testing"

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
