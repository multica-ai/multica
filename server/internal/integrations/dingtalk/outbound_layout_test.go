package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// Inspect rendered quotes from real outgoing requests. GFM checks link and
// line-break boundaries; it does not certify DingTalk client rendering.
func TestSender_SourceQuotePreservesLinksAndLineBreaks(t *testing.T) {
	for _, tc := range []struct{ name, quote, want string }{
		{"link parentheses", "[标签](https://example.com)\n普通 (括号)\n\n省略段落", "<blockquote>\n<p>[标签](<a href=\"https://example.com\">https://example.com</a>)<br>\n普通 (括号)</p>\n<p>省略段落</p>\n</blockquote>"},
		{"literal multiline", "第一行 a-b C:\\tmp\\a\n第二行 *literal*\n\nOmitted paragraph", "<blockquote>\n<p>第一行 a-b C:\\tmp\\a<br>\n第二行 *literal*</p>\n<p>Omitted paragraph</p>\n</blockquote>"},
		{"single line", "question", "<blockquote>\n<p>question</p>\n</blockquote>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDingtalkSendServer(t)
			target := sendTarget{ConversationType: convTypeGroup, ConversationID: "group", QuoteText: tc.quote}
			if _, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), target, "**answer**"); err != nil {
				t.Fatal(err)
			}
			if len(d.sendBodies) != 1 {
				t.Fatalf("sent %d messages", len(d.sendBodies))
			}
			var param markdownParam
			if err := json.Unmarshal([]byte(d.sendBodies[0]["msgParam"].(string)), &param); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(param.Text, `\(`) || strings.Contains(param.Text, `\)`) {
				t.Fatalf("unnecessary URL escapes: %q", param.Text)
			}
			var rendered bytes.Buffer
			if err := goldmark.New(goldmark.WithExtensions(extension.GFM)).Convert([]byte(param.Text), &rendered); err != nil {
				t.Fatal(err)
			}
			parts := strings.SplitN(rendered.String(), "\n<hr>\n", 2)
			if len(parts) != 2 || parts[0] != tc.want || !strings.Contains(parts[1], "<strong>answer</strong>") {
				t.Fatalf("quote or answer changed:\n%s", rendered.String())
			}
		})
	}
}
