package dingtalk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Expected wire text is literal: do not build it with the escaping or preview
// helpers under test. A local HTTP receiver cannot certify DingTalk rendering.
func TestSender_QuotedURLsPreserveSourceBytes(t *testing.T) {
	for _, tc := range []struct{ name, quote, want string }{
		{"client screenshot", "原文引用测试：hello-world a_b *literal* [label](https://example.com/a_(b)) \\ test\n只回复“符号测试完成”。", "> 原文引用测试：hello\\-world a\\_b \\*literal\\* \\[label\\](https://example.com/a_(b)) \\\\ test  \n> 只回复“符号测试完成”。\n\n---\n\nanswer"},
		{"reserved characters", "https://example.com/a_b-c+d!e*f?x=a_b+c&y=%28v%29#part_2", "> https://example.com/a_b-c+d!e*f?x=a_b+c&y=%28v%29#part_2\n\n---\n\nanswer"},
		{"multiple URLs and prose", "a_b https://example.com/a_b *literal* http://example.org/c_d end_x", "> a\\_b https://example.com/a_b \\*literal\\* http://example.org/c_d end\\_x\n\n---\n\nanswer"},
		{"scheme casing and www", "HTTPS://EXAMPLE.COM/A_B www.example.com/a_b", "> HTTPS://EXAMPLE.COM/A_B www.example.com/a_b\n\n---\n\nanswer"},
		{"IPv6 and query brackets", "http://[::1]:8080/a_b?keys[]=x_y", "> http://[::1]:8080/a_b?keys[]=x_y\n\n---\n\nanswer"},
		{"closing delimiters", "(https://example.com/a_(b)) [x_y]", "> (https://example.com/a_(b)) \\[x\\_y\\]\n\n---\n\nanswer"},
		{"adjacent literal link", "https://example.com/a_(b))[x_y](elsewhere)", "> https://example.com/a_(b))\\[x\\_y\\](elsewhere)\n\n---\n\nanswer"},
		{"adjacent URLs", "https://example.com/a_(b))[x_y](https://example.org/c_d)", "> https://example.com/a_(b))\\[x\\_y\\](https://example.org/c_d)\n\n---\n\nanswer"},
		{"unicode and line boundaries", "https://example.com/路径_a\u3000x_y\nhttp://example.org/b_c", "> https://example.com/路径_a\u3000x\\_y  \n> http://example.org/b_c\n\n---\n\nanswer"},
		{"backslash boundary", "https://example.com/a_b\\ *literal*", "> https://example.com/a_b\\\\ \\*literal\\*\n\n---\n\nanswer"},
		{"plain text", "hello-world a_b *literal* [label](relative) \\ test", "> hello\\-world a\\_b \\*literal\\* \\[label\\](relative) \\\\ test\n\n---\n\nanswer"},
		{"paragraphs", "第一段。\n\n第二段：https://example.com/a_b", "> 第一段。  \n>   \n> 第二段：https://example.com/a_b\n\n---\n\nanswer"},
	} {
		for _, transport := range []string{"group", "private"} {
			t.Run(tc.name+"/"+transport, func(t *testing.T) {
				d := newDingtalkSendServer(t)
				d.failFirstSendAuth = transport == "rejected webhook"
				target := sendTarget{ConversationType: convTypeGroup, ConversationID: "group", QuoteText: tc.quote}
				if transport == "private" {
					target.ConversationType = convTypeP2P
					target.StaffID = "staff"
				}
				if _, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), target, "answer"); err != nil {
					t.Fatal(err)
				}
				if len(d.sendBodies) != 1 {
					t.Fatalf("accepted %d messages, want one", len(d.sendBodies))
				}
				body := d.sendBodies[0]
				var param markdownParam
				if raw, ok := body["msgParam"].(string); ok {
					if err := json.Unmarshal([]byte(raw), &param); err != nil {
						t.Fatal(err)
					}
				} else {
					markdown := body["markdown"].(map[string]any)
					param = markdownParam{Title: markdown["title"].(string), Text: markdown["text"].(string)}
				}
				want := tc.want
				if transport == "private" {
					want = "answer"
				}
				if param.Text != want || param.Title != defaultMarkdownTitle {
					t.Fatalf("wire Markdown = %#v; want title=answer, text=%q", param, want)
				}
			})
		}
	}
}

func FuzzQuotedURLPreservesReservedCharacters(f *testing.F) {
	for _, seed := range []string{"a_b", "-+!*#", "?x=a_b+c&y=%28v%29#part_2", "a/b:c;d,e@f$g=h~i"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, tail string) {
		if len(tail) == 0 || len(tail) > 180 {
			t.Skip()
		}
		// These are URL characters, independent of Markdown or the production
		// span finder. Delimiter nesting and Unicode have explicit wire cases.
		const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~:/?#@!$&'*+,;=%"
		if strings.ContainsFunc(tail, func(r rune) bool { return !strings.ContainsRune(alphabet, r) }) {
			t.Skip()
		}
		address := "https://example.com/" + tail
		want := "a\\_b " + address + " \\*literal\\*"
		if got := escapeMarkdownQuoteText("a_b " + address + " *literal*"); got != want {
			t.Fatalf("URL or surrounding literal text changed: got=%q want=%q", got, want)
		}
	})
}

func TestQuotePreview_DoesNotPublishPartialURL(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"exact budget", "https://example.com/" + strings.Repeat("a", 236), "https://example.com/" + strings.Repeat("a", 236)},
		{"URL alone exceeds budget", "https://example.com/" + strings.Repeat("a", 237), "..."},
		{"URL crosses cutoff", strings.Repeat("x", 230) + " https://example.com/a_long_path_here", strings.Repeat("x", 230) + "\n..."},
		{"URL starts at cutoff", strings.Repeat("x", 251) + " https://example.com/a_b", strings.Repeat("x", 251) + "\n..."},
		{"complete URL before cutoff", "https://example.com/a_b " + strings.Repeat("x", 240), "https://example.com/a_b " + strings.Repeat("x", 228) + "\n..."},
		{"literal CRLF paragraphs", "First\r\n \t\r\nLater", "First\n \t\nLater"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := quotePreview(tc.body)
			if got != tc.want || quotePreview(got) != got {
				t.Fatalf("preview=%q, want=%q (idempotent)", got, tc.want)
			}
		})
	}
}

func TestQuotedWebURLBoundaries(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"", ""},
		{"https://", "https://"},
		{"prefix_https://example.com/a_b", "prefix\\_https://example.com/a_b"},
		{"https://example.com/a_b\tplain_x", "https://example.com/a_b\tplain\\_x"},
		{"https://example.com/a_b\x01plain_x", "https://example.com/a_b\x01plain\\_x"},
		{"https://example.com/a_b\"plain_x", "https://example.com/a_b\"plain\\_x"},
		{"https://example.com/a_b`plain_x`", "https://example.com/a_b\\`plain\\_x\\`"},
		{"<https://example.com/a_b>plain_x", "<https://example.com/a_b\\>plain\\_x"},
		{"https://example.com/a_(b]plain_x", "https://example.com/a_(b\\]plain\\_x"},
	} {
		if got := escapeMarkdownQuoteText(tc.source); got != tc.want {
			t.Errorf("quote=%q got=%q want=%q", tc.source, got, tc.want)
		}
	}
}
