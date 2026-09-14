package dingtalk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSender_FullTitleUsesAnswerAcrossTransports(t *testing.T) {
	const answer = "**333**: see the [result](https://example.test/result?token=private-link).\n\nThe second paragraph remains in the complete answer body."
	for _, tc := range []struct {
		name, conversationType string
		quote                  string
	}{
		{name: "private", conversationType: convTypeP2P},
		{name: "proactive group", conversationType: convTypeGroup, quote: "# Original question"},
		{name: "bounded quote", conversationType: convTypeGroup, quote: strings.Repeat("Original question\n", 1200)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDingtalkSendServer(t)
			s := newTestSender(NewClient(nil, d.srv.URL))
			target := sendTarget{ConversationType: tc.conversationType, ConversationID: "group", StaffID: "staff-7", QuoteText: tc.quote}
			if _, err := s.send(context.Background(), target, answer); err != nil {
				t.Fatal(err)
			}
			if len(d.sendBodies) == 0 {
				t.Fatal("no accepted message")
			}
			var delivered strings.Builder
			for i, body := range d.sendBodies {
				var param markdownParam
				if err := json.Unmarshal([]byte(body["msgParam"].(string)), &param); err != nil {
					t.Fatal(err)
				}
				wantTitle := answer
				if !strings.Contains(param.Text, answer) {
					wantTitle = param.Text
				}
				if param.Title != wantTitle {
					t.Errorf("chunk %d title = %q, want complete answer chunk %q", i, param.Title, wantTitle)
				}
				delivered.WriteString(param.Text)
			}
			if !strings.Contains(delivered.String(), answer) {
				t.Fatal("title generation changed the reply body or link destination")
			}
		})
	}
}

// The oracle is the request accepted by the transport, not the chunker's raw
// string lengths. This catches both a large title and JSON escape expansion.
func TestSender_SerializedMarkdownBudgetPreservesAnswer(t *testing.T) {
	for _, transport := range []string{"group", "private"} {
		for _, tc := range []struct{ name, answer, quote string }{
			{"reviewer example", strings.Repeat("a", 12000), ""},
			{"old body boundary", strings.Repeat("b", 16000), ""},
			{"documented limit", strings.Repeat("c", 15000), ""},
			{"multibyte", strings.Repeat("界🚀", 6000), ""},
			{"JSON escaping", strings.Repeat("<&>\"\\", 6000), ""},
			{"control bytes", strings.Repeat("\x01", 16000), ""},
			{"source quote", strings.Repeat("answer\n", 3000), strings.Repeat("Question <>&\"\n", 1200)},
		} {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				d := newDingtalkSendServer(t)
				target := sendTarget{ConversationType: convTypeGroup, ConversationID: "group", StaffID: "staff", QuoteText: tc.quote}
				if transport == "private" {
					target.ConversationType = convTypeP2P
				}
				if _, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), target, tc.answer); err != nil {
					t.Fatal(err)
				}
				var delivered strings.Builder
				remainingQuote := ""
				if transport != "private" {
					remainingQuote = prependMarkdownQuote("", tc.quote)
				}
				for i, body := range d.sendBodies {
					var param markdownParam
					raw := body["msgParam"].(string)
					if len(raw) > 15000 {
						t.Fatalf("chunk %d msgParam exceeds payload budget: %d", i, len(raw))
					}
					if err := json.Unmarshal([]byte(raw), &param); err != nil {
						t.Fatal(err)
					}
					if !utf8.ValidString(param.Title) || !utf8.ValidString(param.Text) {
						t.Fatalf("chunk %d has invalid UTF-8", i)
					}

					answerPart := param.Text
					if remainingQuote != "" {
						if strings.HasPrefix(remainingQuote, param.Text) {
							remainingQuote = strings.TrimPrefix(remainingQuote, param.Text)
						} else {
							answerPart = strings.TrimPrefix(answerPart, remainingQuote)
							remainingQuote = ""
						}
					}
					if param.Title != answerPart {
						t.Fatalf("chunk %d title lost answer bytes or included presentation metadata", i)
					}
					delivered.WriteString(param.Text)
				}
				want := tc.answer
				if transport != "private" {
					want = prependMarkdownQuote(want, tc.quote)
				}
				if delivered.String() != want {
					t.Fatalf("answer or quote was lost, duplicated or changed: received %d bytes, want %d", delivered.Len(), len(want))
				}
			})
		}
	}
}

func TestSender_FullTitlesSplitAndPreserveCompleteAnswer(t *testing.T) {
	first, second := strings.Repeat("a", 6000), strings.Repeat("b", 6000)
	d := newDingtalkSendServer(t)
	target := sendTarget{ConversationType: convTypeGroup, ConversationID: "group"}
	if _, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), target, first+"\n\n"+second); err != nil {
		t.Fatal(err)
	}
	if len(d.sendBodies) < 2 {
		t.Fatalf("sent %d messages; duplicated title/text must force a split", len(d.sendBodies))
	}
	var titles strings.Builder
	for _, body := range d.sendBodies {
		var param markdownParam
		if err := json.Unmarshal([]byte(body["msgParam"].(string)), &param); err != nil {
			t.Fatal(err)
		}
		if param.Title != param.Text {
			t.Fatal("title does not contain its complete answer chunk")
		}
		titles.WriteString(param.Title)
	}
	if titles.String() != first+"\n\n"+second {
		t.Fatal("full titles lost or duplicated answer bytes")
	}
}

func TestSender_FullCodeTitlesRemainQuotableAcrossChunks(t *testing.T) {
	code := strings.Repeat("const available = primary || fallback;\n", 700)
	for _, transport := range []string{"group", "private"} {
		t.Run(transport, func(t *testing.T) {
			d := newDingtalkSendServer(t)
			target := sendTarget{ConversationType: convTypeGroup, ConversationID: "group"}
			if transport == "private" {
				target.ConversationType = convTypeP2P
				target.StaffID = "staff"
			}
			if _, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), target, "```js\n"+code+"```\n"); err != nil {
				t.Fatal(err)
			}
			if len(d.sendBodies) < 2 {
				t.Fatal("long fenced answer did not split")
			}
			var recovered strings.Builder
			for i, body := range d.sendBodies {
				var param markdownParam
				wire := []byte(body["msgParam"].(string))
				if err := json.Unmarshal(wire, &param); err != nil {
					t.Fatal(err)
				}
				if len(wire) > 15000 || param.Title != param.Text || !strings.HasPrefix(param.Title, "```js\n") {
					t.Fatalf("chunk %d has an incomplete code title or exceeds wire budget", i)
				}
				closing := "\n```" // Synthetic close on all non-final chunks.
				if i == len(d.sendBodies)-1 {
					closing = "```\n" // Original closing line.
				}
				if !strings.HasSuffix(param.Title, closing) {
					t.Fatalf("chunk %d lost its closing fence", i)
				}
				recovered.WriteString(strings.TrimSuffix(strings.TrimPrefix(param.Title, "```js\n"), closing))
				cb := textCallback(target.ConversationType, true)
				cb.Text.Content = "explain"
				cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "text", Content: botCallbackRepliedContent{Text: param.Title}}
				msg, ok := inboundFromCallback(cb, "app")
				want := "> " + strings.ReplaceAll(strings.TrimSpace(param.Title), "\n", "\n> ") + "\n\nexplain"
				want = strings.ReplaceAll(want, "\n> \n", "\n>\n")
				if !ok || msg.Text != want || msg.CommandText != "explain" {
					t.Fatalf("chunk %d was lost or rewritten on native quote callback", i)
				}
			}
			if recovered.String() != code {
				t.Fatal("splitting lost, duplicated or altered source code")
			}
		})
	}
}

// Simulate a provider callback carrying the sent title. This proves local
// round-trip handling, not provider acceptance or actual client callback bytes.
func TestSender_TitleQuoteRoundTripPreservesCompleteMarkdown(t *testing.T) {
	const answer = "第一段判断。\n\n第二段有 <tag> 和 [资料](https://example.test/a?x=1&y=2)。\n\n```js\nconst available = primary || fallback;\n```\n\n第三段结论。"
	const instruction = "请解释第二段"
	for _, conversationType := range []string{convTypeGroup, convTypeP2P} {
		d := newDingtalkSendServer(t)
		target := sendTarget{ConversationType: conversationType, ConversationID: "group", StaffID: "staff"}
		if _, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), target, answer); err != nil {
			t.Fatal(err)
		}
		if len(d.sendBodies) != 1 {
			t.Fatalf("short answer split into %d messages", len(d.sendBodies))
		}
		var param markdownParam
		if err := json.Unmarshal([]byte(d.sendBodies[0]["msgParam"].(string)), &param); err != nil {
			t.Fatal(err)
		}
		cb := textCallback(conversationType, true)
		cb.Text.Content = instruction
		cb.Text.IsReplyMsg = true
		cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "text", MsgId: "quoted-answer", Content: botCallbackRepliedContent{Text: param.Title}}
		msg, ok := inboundFromCallback(cb, "test-app")
		want := "> 第一段判断。\n>\n> 第二段有 &lt;tag&gt; 和 [资料](https://example.test/a?x=1&y=2)。\n>\n> ```js\n> const available = primary || fallback;\n> ```\n>\n> 第三段结论。\n\n" + instruction
		if !ok || msg.Text != want || msg.CommandText != instruction {
			t.Fatalf("unexpected selected context or instruction: ok=%v, text=%q, command=%q", ok, msg.Text, msg.CommandText)
		}
	}
}
