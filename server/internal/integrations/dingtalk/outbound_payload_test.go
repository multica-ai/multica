package dingtalk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// Assert the serialized request and reconstructed body, including escape
// expansion that is invisible to a raw UTF-8 body-length check.
func TestSenderProactivePayloadBudgetPreservesBody(t *testing.T) {
	for _, answer := range []string{
		strings.Repeat("<&>\"\\", 6000),
		strings.Repeat("界🚀", 6000),
		"# " + strings.Repeat("heading", 2200),
		strings.Repeat("\x01", 16000),
	} {
		for _, kind := range []string{convTypeGroup, convTypeP2P} {
			d := newDingtalkSendServer(t)
			target := sendTarget{ConversationType: kind, ConversationID: "group", StaffID: "staff"}
			if _, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), target, answer); err != nil {
				t.Fatal(err)
			}
			var reconstructed strings.Builder
			for _, body := range d.sendBodies {
				raw := body["msgParam"].(string)
				var param markdownParam
				if err := json.Unmarshal([]byte(raw), &param); err != nil {
					t.Fatal(err)
				}
				if len(raw) > 15000 || !utf8.ValidString(param.Text) || !utf8.ValidString(param.Title) {
					t.Fatalf("invalid serialized payload: %d bytes", len(raw))
				}
				reconstructed.WriteString(param.Text)
			}
			if reconstructed.String() != answer {
				t.Fatal("chunking lost or duplicated answer bytes")
			}
		}
	}
}

func TestSenderQuotedChunksPreserveOneSourceAndWholeAnswer(t *testing.T) {
	const prefix = "> question\n\n---\n\n"
	for _, tc := range []struct {
		name   string
		answer string
	}{
		{name: "combined", answer: "short answer"},
		{name: "long single line", answer: strings.Repeat("a", 30000)},
		{name: "14000 multi-line", answer: strings.Repeat("answer line\n", 1273)},
		{name: "30000 multi-line", answer: strings.Repeat("answer line\n", 2728)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDingtalkSendServer(t)
			target := sendTarget{ConversationType: convTypeGroup, ConversationID: "group", QuoteText: "question"}
			if _, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), target, tc.answer); err != nil {
				t.Fatal(err)
			}
			var body strings.Builder
			for i, request := range d.sendBodies {
				raw := request["msgParam"].(string)
				var param markdownParam
				if err := json.Unmarshal([]byte(raw), &param); err != nil {
					t.Fatal(err)
				}
				if len(raw) > 15000 || !utf8.ValidString(param.Text) {
					t.Fatalf("invalid serialized quoted payload: %d bytes", len(raw))
				}
				if i == 0 && (!strings.HasPrefix(param.Text, prefix) || strings.TrimSpace(strings.TrimPrefix(param.Text, prefix)) == "") {
					t.Fatalf("expected source with nonempty answer, got %q", param.Text)
				}
				body.WriteString(param.Text)
			}
			if body.String() != prefix+tc.answer {
				t.Fatal("quoted delivery lost or duplicated source/answer bytes")
			}
		})
	}
}

func TestQuotedFirstChunkReservesPrefixForFencedAnswer(t *testing.T) {
	for _, line := range []string{strings.Repeat("界", 100) + "\n", strings.Repeat("界", 6000) + "\n"} {
		code := strings.Repeat(line, 30)
		chunks, err := replyMarkdownChunks("```go\n"+code+"```\n", "source")
		if err != nil || len(chunks) < 2 {
			t.Fatalf("chunking: %d %v", len(chunks), err)
		}
		prefix := prependMarkdownQuote("", "source")
		var recovered strings.Builder
		for i, chunk := range chunks {
			body := chunk.text
			if i == 0 {
				if !strings.HasPrefix(body, prefix) {
					t.Fatal("first answer has no source")
				}
				body = strings.TrimPrefix(body, prefix)
			}
			if chunk.title != body || !strings.HasPrefix(body, "```go\n") || isBlankChunk(body) {
				t.Fatalf("first/title/fence invariant lost: %q", body)
			}
			raw, _ := json.Marshal(markdownParam{Title: chunk.title, Text: chunk.text})
			if len(raw) > markdownPayloadByteBudget {
				t.Fatal("payload exceeds limit")
			}
			body = strings.TrimPrefix(body, "```go\n")
			if strings.HasSuffix(body, "```\n") {
				body = strings.TrimSuffix(body, "```\n")
			} else {
				body = strings.TrimSuffix(body, "\n```")
			}
			recovered.WriteString(body)
		}
		if recovered.String() != code {
			t.Fatal("fenced answer changed")
		}
	}
}

func TestSenderEscapedAnswerWithMultilineQuote(t *testing.T) {
	for _, quote := range []string{strings.Repeat("a\n", 128), strings.Repeat("*\n", 128)} {
		t.Run(quote[:1], func(t *testing.T) {
			d := newDingtalkSendServer(t)
			answer := strings.Repeat("\x01", 16000)
			target := sendTarget{ConversationType: convTypeGroup, ConversationID: "group", QuoteText: quote}
			if _, err := newTestSender(NewClient(nil, d.srv.URL)).send(context.Background(), target, answer); err != nil {
				t.Fatal(err)
			}
			prefix := prependMarkdownQuote("", quote)
			var recovered strings.Builder
			for i, sent := range d.sendBodies {
				raw := sent["msgParam"].(string)
				var param markdownParam
				if err := json.Unmarshal([]byte(raw), &param); err != nil {
					t.Fatal(err)
				}
				body := param.Text
				if i == 0 {
					if !strings.HasPrefix(body, prefix) {
						t.Fatal("first answer lost source attribution")
					}
					body = strings.TrimPrefix(body, prefix)
				}
				if len(raw) > markdownPayloadByteBudget || body == "" || param.Title != body {
					t.Fatal("wire budget, nonempty answer, or full title invariant violated")
				}
				recovered.WriteString(body)
			}
			if recovered.String() != answer {
				t.Fatal("answer bytes were lost or duplicated")
			}
		})
	}
}
