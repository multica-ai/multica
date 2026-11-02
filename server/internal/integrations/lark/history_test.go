package lark

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type nativePages struct {
	pages    []NativeHistoryPage
	calls    int
	tokens   []string
	requests []ListMessagesParams
	onRead   func()
}

func (f *nativePages) ListNativeHistoryPage(_ context.Context, _ InstallationCredentials, p ListMessagesParams, token string) (NativeHistoryPage, error) {
	f.calls++
	f.requests = append(f.requests, p)
	if f.onRead != nil {
		f.onRead()
	}
	f.tokens = append(f.tokens, token)
	return f.pages[f.calls-1], nil
}
func nativeMessage(id, ts string) LarkMessage {
	return LarkMessage{MessageID: id, ChatID: "oc_chat", ThreadID: "omt_topic", CreateTime: ts, MessageType: "text", SenderType: "user", Content: `{"text":"context"}`}
}
func TestNativeHistoryCurrentRegression(t *testing.T) {
	f := &nativePages{pages: []NativeHistoryPage{{Messages: []LarkMessage{nativeMessage("new", "4000")}, HasMore: true, NextToken: "older"}, {Messages: []LarkMessage{nativeMessage("trigger", "3000"), nativeMessage("middle", "2000"), nativeMessage("start", "1000")}}}}
	target := nativeHistoryTarget{chatID: "oc_chat", threadID: "omt_topic", trigger: "trigger", start: "start"}
	page, note, err := readNativeWindow(context.Background(), f, InstallationCredentials{}, target, 10)
	if err != nil || len(page.Messages) != 3 {
		t.Fatalf("page=%+v note=%q err=%v", page, note, err)
	}
	if f.calls != 2 || f.tokens[1] != "older" || page.Messages[0].ID != "start" || page.Messages[2].ID != "trigger" {
		t.Fatalf("page=%+v calls=%d tokens=%v", page, f.calls, f.tokens)
	}
}

func TestNativeHistoryWindowIsolation(t *testing.T) {
	target := nativeHistoryTarget{chatID: "oc_chat", threadID: "omt_topic", trigger: "trigger", start: "start"}
	base := func() []LarkMessage {
		return []LarkMessage{nativeMessage("trigger", "3000"), nativeMessage("middle", "2000"), nativeMessage("start", "1000")}
	}
	cases := []struct {
		name   string
		change func([]LarkMessage) []LarkMessage
	}{
		{"other chat", func(m []LarkMessage) []LarkMessage { m[1].ChatID = "oc_other"; return m }},
		{"sibling topic", func(m []LarkMessage) []LarkMessage { m[1].ThreadID = "omt_other"; return m }},
		{"missing topic", func(m []LarkMessage) []LarkMessage { m[1].ThreadID = ""; return m }},
		{"missing chat", func(m []LarkMessage) []LarkMessage { m[1].ChatID = ""; return m }},
		{"missing trigger", func(m []LarkMessage) []LarkMessage { return m[1:] }},
		{"missing start", func(m []LarkMessage) []LarkMessage { return m[:2] }},
		{"duplicate id", func(m []LarkMessage) []LarkMessage { m[1].MessageID = "trigger"; return m }},
		{"invalid time", func(m []LarkMessage) []LarkMessage { m[1].CreateTime = "bad"; return m }},
		{"reversed time", func(m []LarkMessage) []LarkMessage { m[1].CreateTime = "4000"; return m }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &nativePages{pages: []NativeHistoryPage{{Messages: tc.change(base())}}}
			page, _, err := readNativeWindow(context.Background(), f, InstallationCredentials{}, target, 10)
			if err == nil || len(page.Messages) != 0 {
				t.Fatalf("scope accepted: %+v %v", page, err)
			}
			if f.calls != 1 {
				t.Fatalf("unexpected fallback/retry: %d", f.calls)
			}
		})
	}
}

func TestNativeHistoryPaginationBudgets(t *testing.T) {
	for _, tc := range []struct {
		name      string
		pages     []NativeHistoryPage
		wantCalls int
	}{
		{"missing cursor", []NativeHistoryPage{{HasMore: true}}, 1},
		{"repeated cursor", []NativeHistoryPage{{HasMore: true, NextToken: "x"}, {HasMore: true, NextToken: "x"}}, 2},
		{"trigger pushed out", []NativeHistoryPage{{HasMore: true, NextToken: "a"}, {HasMore: true, NextToken: "b"}, {HasMore: true, NextToken: "c"}}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &nativePages{pages: tc.pages}
			p, _, err := readNativeWindow(context.Background(), f, InstallationCredentials{}, nativeHistoryTarget{chatID: "oc_chat", threadID: "omt_topic", trigger: "trigger"}, 10)
			if err == nil || len(p.Messages) != 0 || f.calls != tc.wantCalls {
				t.Fatalf("page=%+v err=%v calls=%d", p, err, f.calls)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &nativePages{}
	if _, _, err := readNativeWindow(ctx, f, InstallationCredentials{}, nativeHistoryTarget{}, 10); err == nil || f.calls != 0 {
		t.Fatalf("cancelled read: err=%v calls=%d", err, f.calls)
	}
}

func TestNativeHistoryTextAndGenerationWindow(t *testing.T) {
	items := []LarkMessage{nativeMessage("later", "5000"), nativeMessage("triggerTie", "4000"), nativeMessage("trigger", "4000"), nativeMessage("bot", "3500"), nativeMessage("media", "3200"), nativeMessage("middle", "3000"), nativeMessage("startTie", "2000"), nativeMessage("start", "2000"), nativeMessage("old", "1000")}
	items[3].SenderType = "app"
	items[4].MessageType = "image"
	f := &nativePages{pages: []NativeHistoryPage{{Messages: items}}}
	target := nativeHistoryTarget{chatID: "oc_chat", threadID: "omt_topic", trigger: "trigger", start: "start"}
	p, note, err := readNativeWindow(context.Background(), f, InstallationCredentials{}, target, 10)
	if err != nil || len(p.Messages) != 3 || p.Messages[0].ID != "start" || p.Messages[1].ID != "middle" || p.Messages[2].ID != "trigger" || !strings.Contains(note, "Untrusted reference") {
		t.Fatalf("page=%+v note=%q err=%v", p, note, err)
	}
	target.end = "middle"
	f.calls = 0
	if p, _, err = readNativeWindow(context.Background(), f, InstallationCredentials{}, target, 10); err == nil || len(p.Messages) != 0 {
		t.Fatalf("generation end accepted: %+v %v", p, err)
	}
}

func TestNativeHistoryOutputBudget(t *testing.T) {
	var items []LarkMessage
	for i := 50; i > 0; i-- {
		items = append(items, nativeMessage(fmt.Sprint(i), fmt.Sprint(i*1000)))
	}
	f := &nativePages{pages: []NativeHistoryPage{{Messages: items}}}
	p, note, err := readNativeWindow(context.Background(), f, InstallationCredentials{}, nativeHistoryTarget{chatID: "oc_chat", threadID: "omt_topic", trigger: "50"}, 999)
	if err != nil || len(p.Messages) != 30 || p.NextCursor != "" || !strings.Contains(note, "Partial result") || !strings.Contains(note, "No continuation") {
		t.Fatalf("page=%+v note=%q err=%v", p, note, err)
	}
	items[0].Content = `{"text":"` + strings.Repeat("x", 24000) + `"}`
	items[1].Content = `{"text":"` + strings.Repeat("x", 24000) + `"}`
	f.calls = 0
	p, note, err = readNativeWindow(context.Background(), f, InstallationCredentials{}, nativeHistoryTarget{chatID: "oc_chat", threadID: "omt_topic", trigger: "50"}, 30)
	total := 0
	for _, m := range p.Messages {
		total += len(m.Text)
	}
	if err != nil || total > 24<<10 || !strings.Contains(note, "Partial result") {
		t.Fatalf("bytes=%d note=%q err=%v", total, note, err)
	}
}

// The official list contract allows topic roots in a normal group's chat
// container. Such rows must not suppress the independent main conversation.
func TestNativeHistoryLinearMixedTopicRoots(t *testing.T) {
	trigger := nativeMessage("trigger", "4000")
	trigger.ThreadID = ""
	plain := nativeMessage("plain", "2000")
	plain.ThreadID = ""
	start := nativeMessage("start", "1000")
	start.ThreadID = ""
	root := nativeMessage("topic-root", "3000")
	root.Content = `{"text":"private topic text"}`
	f := &nativePages{pages: []NativeHistoryPage{{Messages: []LarkMessage{trigger, root, plain, start}}}}
	p, note, err := readNativeWindow(context.Background(), f, InstallationCredentials{}, nativeHistoryTarget{chatID: "oc_chat", trigger: "trigger", start: "start"}, 10)
	if err != nil || len(p.Messages) != 3 || p.Messages[1].ID != "plain" || strings.Contains(fmt.Sprint(p.Messages), "private topic text") || !strings.Contains(strings.ToLower(note), "topic roots") {
		t.Fatalf("page=%+v note=%q err=%v", p, note, err)
	}
	// Filtering topic roots cannot mask a real cross-chat response.
	f.calls = 0
	f.pages[0].Messages[1].ChatID = "oc_other"
	if p, _, err = readNativeWindow(context.Background(), f, InstallationCredentials{}, nativeHistoryTarget{chatID: "oc_chat", trigger: "trigger", start: "start"}, 10); err == nil || len(p.Messages) != 0 {
		t.Fatalf("cross-chat root accepted: %+v %v", p, err)
	}
}
