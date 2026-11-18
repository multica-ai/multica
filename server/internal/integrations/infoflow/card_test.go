package infoflow

import (
	"context"
	"errors"
	"testing"

	"github.com/dolfly/infoflow/bot/dynamiccard"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type cardCall struct {
	target    sendTarget
	markdown  string
	asText    bool
	asDynamic bool
}

// cardFakeSender records how a reply was rendered.
type cardFakeSender struct {
	mu    chan struct{}
	calls []cardCall
	err   error
}

func (f *cardFakeSender) Send(_ context.Context, target sendTarget, markdown string) (cardDelivery, error) {
	f.calls = append(f.calls, cardCall{target: target, markdown: markdown, asDynamic: true})
	if f.err != nil {
		return cardDelivery{}, f.err
	}
	return cardDelivery{Route: cardRouteDynamic, CardID: "card-1", MessageID: "dm-1"}, nil
}

func TestWantsCard(t *testing.T) {
	// A plain answer stays a plain message; anything with Markdown structure a
	// Markdown message would render goes through the card route.
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"plain prose", "把 router.go 里的 registration 补上就行。", false},
		{"plain english", "The fix is in the router wiring.", false},
		{"empty", "", false},
		{"whitespace", "   \n\t", false},
		{"inline code only", "请用 pnpm test 跑一下", false},
		{"fenced code", "改这里：\n```go\nfmt.Println(1)\n```", true},
		{"bullet list", "要点：\n- 第一项\n- 第二项", true},
		{"heading", "## 根因\n字段为空", true},
		{"blockquote", "> 提示", true},
		{"markdown table", "| a | b |\n| --- | --- |\n| 1 | 2 |", true},
		{"ordered list", "步骤：\n1. 改代码\n2. 跑测试", true},
		{"link", "见 [文档](https://example.com)", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wantsCard(tc.content); got != tc.want {
				t.Errorf("wantsCard(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestReplySender_UsesCardForMarkdown(t *testing.T) {
	cards := &cardFakeSender{}
	replies := replySender{
		cards: cards,
		text: func(context.Context, sendTarget, string) (channel.SendResult, error) {
			t.Fatal("a Markdown answer must not be sent as a plain text message")
			return channel.SendResult{}, nil
		},
	}
	sent, err := replies.send(context.Background(), sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u:user-1"}, "## 结论\n已修好")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.MessageID != "dm-1" {
		t.Errorf("MessageID = %q", sent.MessageID)
	}
	if len(cards.calls) != 1 {
		t.Fatalf("card calls = %d, want one", len(cards.calls))
	}
	if cards.calls[0].target.ChatID != "u:user-1" {
		t.Errorf("target = %+v", cards.calls[0].target)
	}
}

func TestReplySender_UsesTextForPlainContent(t *testing.T) {
	cards := &cardFakeSender{}
	replies := replySender{
		cards: cards,
		text: func(_ context.Context, _ sendTarget, content string) (channel.SendResult, error) {
			return channel.SendResult{MessageID: "tm-1"}, nil
		},
	}
	sent, err := replies.send(context.Background(), sendTarget{ChatType: channel.ChatTypeGroup, ChatID: "g:2"}, "好的，这就改。")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.MessageID != "tm-1" {
		t.Errorf("MessageID = %q, want the text route's id", sent.MessageID)
	}
	if len(cards.calls) != 0 {
		t.Errorf("card calls = %d, want none for plain content", len(cards.calls))
	}
}

func TestReplySender_RejectsEmptyReply(t *testing.T) {
	replies := replySender{
		cards: &cardFakeSender{},
		text: func(context.Context, sendTarget, string) (channel.SendResult, error) {
			t.Fatal("an empty reply must not be sent")
			return channel.SendResult{}, nil
		},
	}
	if _, err := replies.send(context.Background(), sendTarget{}, "   "); err == nil {
		t.Fatal("expected an error for an empty reply")
	}
}

func TestCardSender_DynamicRouteWins(t *testing.T) {
	// The audited robot gets a real card; the card is the answer, not a stub.
	var dynamicTarget, markdownTarget *sendTarget
	sender := sdkCardSender{
		sendDynamic: func(_ context.Context, target sendTarget, markdown string) (dynamiccard.CardRef, error) {
			dynamicTarget = &target
			return dynamiccard.CardRef{CardID: "card-9", MessageID: "dm-9"}, nil
		},
		sendMarkdown: func(_ context.Context, target sendTarget, _ string) (channel.SendResult, error) {
			markdownTarget = &target
			return channel.SendResult{MessageID: "md-9"}, nil
		},
	}
	delivery, err := sender.Send(context.Background(), sendTarget{ChatType: channel.ChatTypeGroup, ChatID: "g:9"}, "| a | b |")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Route != cardRouteDynamic {
		t.Errorf("Route = %q, want %q", delivery.Route, cardRouteDynamic)
	}
	if delivery.CardID != "card-9" || delivery.MessageID != "dm-9" {
		t.Errorf("delivery = %+v", delivery)
	}
	if dynamicTarget == nil || dynamicTarget.ChatID != "g:9" {
		t.Errorf("dynamic target = %+v", dynamicTarget)
	}
	if markdownTarget != nil {
		t.Error("the markdown fallback must not run when the card is accepted")
	}
}

func TestCardSender_FallsBackWhenTheCardIsRefused(t *testing.T) {
	// An unaudited robot gets a rejection, not a blank: the answer goes out as a
	// Markdown message instead, and the delivery says which route served it.
	sender := sdkCardSender{
		sendDynamic: func(context.Context, sendTarget, string) (dynamiccard.CardRef, error) {
			return dynamiccard.CardRef{}, errors.New("robot is not allowed to send dynamic cards")
		},
		sendMarkdown: func(context.Context, sendTarget, string) (channel.SendResult, error) {
			return channel.SendResult{MessageID: "md-1"}, nil
		},
	}
	delivery, err := sender.Send(context.Background(), sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u:1"}, "## 结论")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivery.Route != cardRouteMarkdown {
		t.Errorf("Route = %q, want %q", delivery.Route, cardRouteMarkdown)
	}
	if delivery.MessageID != "md-1" {
		t.Errorf("MessageID = %q", delivery.MessageID)
	}
}

func TestCardSender_FallbackFailureCarriesTheCardReason(t *testing.T) {
	sender := sdkCardSender{
		sendDynamic: func(context.Context, sendTarget, string) (dynamiccard.CardRef, error) {
			return dynamiccard.CardRef{}, errors.New("card refused")
		},
		sendMarkdown: func(context.Context, sendTarget, string) (channel.SendResult, error) {
			return channel.SendResult{}, errors.New("markdown refused too")
		},
	}
	_, err := sender.Send(context.Background(), sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u:1"}, "**bold**")
	if err == nil {
		t.Fatal("expected an error when both routes fail")
	}
	if !containsAll(err.Error(), "card refused", "markdown refused too") {
		t.Errorf("error = %q, want both reasons", err)
	}
}

func TestCardSender_RejectsEmptyContent(t *testing.T) {
	sender := sdkCardSender{
		sendDynamic: func(context.Context, sendTarget, string) (dynamiccard.CardRef, error) {
			t.Fatal("an empty card must not be sent")
			return dynamiccard.CardRef{}, nil
		},
		sendMarkdown: func(context.Context, sendTarget, string) (channel.SendResult, error) {
			t.Fatal("an empty card must not fall back to a message")
			return channel.SendResult{}, nil
		},
	}
	if _, err := sender.Send(context.Background(), sendTarget{}, "  "); err == nil {
		t.Fatal("expected an error for empty card content")
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		found := false
		for i := 0; i+len(n) <= len(haystack); i++ {
			if haystack[i:i+len(n)] == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
