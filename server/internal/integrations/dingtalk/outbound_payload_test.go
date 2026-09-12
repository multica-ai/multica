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
		name       string
		answer     string
		standalone bool
	}{
		{name: "combined", answer: "short answer"},
		{name: "standalone source", answer: strings.Repeat("a", 30000), standalone: true},
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
				if i == 0 && tc.standalone && param.Text != prefix {
					t.Fatalf("expected standalone source chunk, got %q", param.Text)
				}
				body.WriteString(param.Text)
			}
			if body.String() != prefix+tc.answer {
				t.Fatal("quoted delivery lost or duplicated source/answer bytes")
			}
		})
	}
}
