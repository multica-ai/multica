package dingtalk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// The same fixture is rendered through ChatMessageList in the frontend parity
// test. Expected canonical Markdown is literal, not built by the escape helper.
func TestInboundFromCallback_QuotedHTMLPreservesVisibleSource(t *testing.T) {
	data, err := os.ReadFile("testdata/quoted_markdown.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Source, Body string }
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			const current = "解释引用；当前 <mark>input</mark> 不变"
			cb.Text.Content = current
			cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "text", Content: botCallbackRepliedContent{Text: tc.Source}}
			msg, ok := inboundFromCallback(cb, "app")
			lines := strings.Split(tc.Body, "\n")
			for i, line := range lines {
				lines[i] = strings.TrimRight("> "+line, " ")
			}
			want := strings.Join(lines, "\n") + "\n\n" + current
			if !ok || msg.Text != want || msg.CommandText != current || !msg.HasSelectedContext {
				t.Fatalf("quote or current input changed: text=%q command=%q; want text=%q", msg.Text, msg.CommandText, want)
			}
		})
	}
}

func FuzzQuotedHTMLNormalizationIsStable(f *testing.F) {
	for _, source := range []string{
		"literal <tag> and `code <tag>`",
		"<script>\nif (a < b && c > d) {}\n</script>",
		"> - <div>\n>   \t<tag>\n>   </div>\n\nend",
		"| a | b |\n| - | - |\n| <tag> | `</tag>` |",
		"<tag\r\n attr=\"[Image]\">\r\n\r\n<https://example.test/>",
		"<tag> &lt;escaped&gt; <!-- comment -->",
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 16000 || !utf8.ValidString(source) {
			t.Skip()
		}
		got := escapeDingTalkQuotedHTML(source)
		if !utf8.ValidString(got) || escapeDingTalkQuotedHTML(got) != got {
			t.Fatalf("normalization corrupts UTF-8 or is not stable: source=%q got=%q", source, got)
		}
	})
}
