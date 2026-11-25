package telegram

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"
)

func assertValidTelegramHTML(t *testing.T, rendered string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader("<root>" + rendered + "</root>"))
	var stack []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid Telegram HTML %q: %v", rendered, err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			name := el.Name.Local
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			switch name {
			case "root":
			case "b", "i", "s", "a", "code", "pre":
				if parent == "code" {
					t.Fatalf("nested tag <%s> inside <code> in %q", name, rendered)
				}
				if parent == "pre" && name != "code" {
					t.Fatalf("nested tag <%s> inside <pre> in %q", name, rendered)
				}
				if name == "a" {
					for _, ancestor := range stack {
						if ancestor == "a" {
							t.Fatalf("nested <a> inside <a> in %q", rendered)
						}
					}
				}
				if name == "code" {
					for _, attr := range el.Attr {
						if attr.Name.Local == "class" && strings.ContainsAny(attr.Value, " \t\r\n") {
							t.Fatalf("code class attribute contains whitespace %q in %q", attr.Value, rendered)
						}
					}
				}
			default:
				t.Fatalf("unexpected tag <%s> in %q", name, rendered)
			}
			stack = append(stack, name)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

func TestFormatHTMLStrictNestingAndEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bold italic triple stars nests cleanly",
			in:   "***bold italic***",
			want: "<b><i>bold italic</i></b>",
		},
		{
			name: "italic nested inside bold closing together",
			in:   "**bold *and italic***",
			want: "<b>bold <i>and italic</i></b>",
		},
		{
			name: "bold nested inside italic closing together",
			in:   "*italic **and bold***",
			want: "<i>italic <b>and bold</b></i>",
		},
		{
			name: "bold closing inside italic opening together",
			in:   "***bold** and italic*",
			want: "<i><b>bold</b> and italic</i>",
		},
		{
			name: "italic closing inside bold opening together",
			in:   "***italic* and bold**",
			want: "<b><i>italic</i> and bold</b>",
		},
		{
			name: "crossing bold and strikethrough stays validly nested",
			in:   "**bold ~~both** strike~~",
			want: "<b>bold ~~both</b> strike~~",
		},
		{
			name: "crossing italic and strikethrough stays validly nested",
			in:   "*italic ~~both* strike~~",
			want: "<i>italic ~~both</i> strike~~",
		},
		{
			name: "space-surrounded asterisks are not italic",
			in:   "2 * 3 * 4 = 24",
			want: "2 * 3 * 4 = 24",
		},
		{
			name: "markdown link with balanced parentheses in URL",
			in:   "[Go](https://en.wikipedia.org/wiki/Go_(programming_language))",
			want: `<a href="https://en.wikipedia.org/wiki/Go_(programming_language)">Go</a>`,
		},
		{
			name: "markdown link with inline code and bold inside label",
			in:   "[**read** `doc`](https://example.com/a)",
			want: `<a href="https://example.com/a"><b>read</b> <code>doc</code></a>`,
		},
		{
			name: "multi-backtick inline code preserves inner backticks",
			in:   "use ``a `b` c`` here",
			want: "use <code>a `b` c</code> here",
		},
		{
			name: "inline triple-backtick code span does not open fenced block",
			in:   "```foo``` and **bold**",
			want: "<code>foo</code> and <b>bold</b>",
		},
		{
			name: "four-backtick fence preserves inner triple-backtick fence",
			in:   "````markdown\n```go\nx < 1\n```\n````\nafter **bold**",
			want: "<pre><code class=\"language-markdown\">```go\nx &lt; 1\n```</code></pre>\nafter <b>bold</b>",
		},
		{
			name: "closing fence with info string stays inside code block",
			in:   "```\n```go\n```\nafter",
			want: "<pre>```go</pre>\nafter",
		},
		{
			name: "fence info string uses first language token only",
			in:   "```go title=\"main.go\"\nx < 1\n```",
			want: `<pre><code class="language-go">x &lt; 1</code></pre>`,
		},
		{
			name: "tilde code fence renders pre block",
			in:   "~~~sh\necho a & b\n~~~",
			want: `<pre><code class="language-sh">echo a &amp; b</code></pre>`,
		},
		{
			name: "unterminated fence closes cleanly mid-stream",
			in:   "```go\nfmt.Println(1 < 2)",
			want: `<pre><code class="language-go">fmt.Println(1 &lt; 2)</code></pre>`,
		},
		{
			name: "CRLF input leaves no carriage returns in HTML",
			in:   "# Title\r\n- **item**\r\n```go\r\nx := 1\r\n```\r\n",
			want: "<b>Title</b>\n• <b>item</b>\n<pre><code class=\"language-go\">x := 1</code></pre>",
		},
		{
			name: "literal placeholder-like strings do not collide with code or link spans",
			in:   "before \x00CODE\x00 \x00LINK\x00 and `real` and [x](https://e.co)",
			want: "before \x00CODE\x00 \x00LINK\x00 and <code>real</code> and <a href=\"https://e.co\">x</a>",
		},
		{
			name: "bold containing code span with stars stays balanced",
			in:   "**bold `x ** y` end**",
			want: "<b>bold <code>x ** y</code> end</b>",
		},
		{
			name: "unclosed inner italic does not break enclosing bold",
			in:   "**bold *unclosed**",
			want: "<b>bold *unclosed</b>",
		},
		{
			name: "unclosed inner bold does not break enclosing italic",
			in:   "*italic **unclosed*",
			want: "<i>italic **unclosed</i>",
		},
		{
			name: "crossing strikethrough and bold stays validly nested",
			in:   "~~strike **cross~~ bold**",
			want: "<s>strike **cross</s> bold**",
		},
		{
			name: "angle-bracketed link URL with spaces",
			in:   "[doc](<https://example.com/a b>)",
			want: `<a href="https://example.com/a b">doc</a>`,
		},
		{
			name: "link label containing code span with closing bracket",
			in:   "[`a]b`](https://example.com)",
			want: `<a href="https://example.com"><code>a]b</code></a>`,
		},
		{
			name: "nested markdown link inside link label does not emit nested anchor tags",
			in:   "[see [inner](https://a.example)](https://b.example)",
			want: `<a href="https://b.example">see [inner](https://a.example)</a>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatHTML(tc.in)
			if !strings.Contains(tc.in, "\x00") {
				assertValidTelegramHTML(t, got)
			}
			if got != tc.want {
				t.Errorf("formatHTML(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}

func unfinishedMarkdownLine(targetLen int) string {
	const pattern = "***a [b ~~c `d "
	n := (targetLen + len(pattern) - 1) / len(pattern)
	return strings.Repeat(pattern, n)[:targetLen]
}

func distinctBackticksLine(targetLen int) string {
	var b strings.Builder
	for n := 1; b.Len()+n+2 <= targetLen; n++ {
		b.WriteString(strings.Repeat("`", n))
		b.WriteString("a ")
	}
	return b.String()
}

func TestFormatHTMLUnfinishedMarkdown(t *testing.T) {
	const maxRenderTime = 100 * time.Millisecond

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "mixed unfinished emphasis links strike and code",
			in:   unfinishedMarkdownLine(3500),
		},
		{
			name: "repeated unclosed single stars",
			in:   strings.Repeat("*a ", 1167),
			want: strings.Repeat("*a ", 1167),
		},
		{
			name: "repeated unclosed triple stars with links",
			in:   strings.Repeat("***a [b ", 438),
			want: strings.Repeat("***a [b ", 438),
		},
		{
			name: "repeated unclosed strikethrough with links",
			in:   strings.Repeat("~~a [b ", 500),
			want: strings.Repeat("~~a [b ", 500),
		},
		{
			name: "repeated unclosed distinct-length backtick runs",
			in:   distinctBackticksLine(3500),
			want: distinctBackticksLine(3500),
		},
		{
			name: "repeated unclosed link destinations",
			in:   strings.Repeat("[a](b", 700),
			want: strings.Repeat("[a](b", 700),
		},
		{
			name: "repeated unclosed angle-bracket destinations",
			in:   strings.Repeat("[a](<b", 584),
			want: strings.Repeat("[a](&lt;b", 584),
		},
		{
			name: "repeated escaped open brackets with single closing bracket",
			in:   strings.Repeat(`\[`, 1750) + "]",
			want: strings.Repeat(`\[`, 1750) + "]",
		},
		{
			name: "unclosed bold before many closed italics",
			in:   strings.Repeat("**a ", 437) + strings.Repeat("*b* ", 438),
			want: strings.Repeat("**a ", 437) + strings.Repeat("<i>b</i> ", 438),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			got := formatHTML(tc.in)
			if elapsed := time.Since(start); elapsed > maxRenderTime {
				t.Fatalf("formatHTML took %v, want at most %v", elapsed, maxRenderTime)
			}
			assertValidTelegramHTML(t, got)
			if tc.want != "" && got != tc.want {
				t.Errorf("formatHTML(%s) mismatch:\n got len=%d\nwant len=%d", tc.name, len(got), len(tc.want))
			}
		})
	}
}

func BenchmarkFormatHTMLUnfinishedMarkdown(b *testing.B) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "mixed_3500", in: unfinishedMarkdownLine(3500)},
		{name: "stars_links_3500", in: strings.Repeat("*a [b ", 584)[:3500]},
		{name: "triple_stars_links_3500", in: strings.Repeat("***a [b ", 438)[:3500]},
		{name: "unclosed_link_dest_3500", in: strings.Repeat("[a](b", 700)},
		{name: "unclosed_strike_3500", in: strings.Repeat("~~a ", 875)},
		{name: "bold_before_italics_3500", in: strings.Repeat("**a ", 437) + strings.Repeat("*b* ", 438)},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = formatHTML(tc.in)
			}
		})
	}
}
