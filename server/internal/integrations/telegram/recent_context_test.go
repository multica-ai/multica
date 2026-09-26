package telegram

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// groupUpdate builds one human message in a supergroup, optionally inside a
// forum topic and optionally replying to another message.
func groupUpdate(id int64, from *User, text string, reply *Message, topic int64) Update {
	m := &Message{
		MessageID:      id,
		From:           from,
		Chat:           Chat{ID: -100200, Type: "supergroup"},
		Text:           text,
		ReplyToMessage: reply,
	}
	if topic != 0 {
		m.IsTopicMessage = true
		m.MessageThreadID = topic
	}
	return Update{UpdateID: id, Message: m}
}

func TestRecentContextBufferKeepsWindowPerChatAndTopic(t *testing.T) {
	b := newRecentContextBuffer(3)
	for i := int64(1); i <= 5; i++ {
		b.Record(-1, 0, recentEntry{MessageID: i, Sender: "Ada", Text: fmt.Sprintf("m%d", i)})
	}
	b.Record(-1, 7, recentEntry{MessageID: 100, Sender: "Bob", Text: "topic only"})
	b.Record(-2, 0, recentEntry{MessageID: 200, Sender: "Cy", Text: "other chat"})

	got := b.Snapshot(-1, 0)
	if len(got) != 3 || got[0].MessageID != 3 || got[2].MessageID != 5 {
		t.Fatalf("window should keep the newest 3 oldest-first, got %+v", got)
	}
	if got := b.Snapshot(-1, 0, 4); len(got) != 2 || got[0].MessageID != 3 || got[1].MessageID != 5 {
		t.Fatalf("exclude should drop id 4, got %+v", got)
	}
	if got := b.Snapshot(-1, 7); len(got) != 1 || got[0].Text != "topic only" {
		t.Fatalf("topic ring must be isolated from the chat ring, got %+v", got)
	}
	if got := b.Snapshot(-3, 0); got != nil {
		t.Fatalf("unknown chat should snapshot nil, got %+v", got)
	}

	var nilBuf *recentContextBuffer
	nilBuf.Record(-1, 0, recentEntry{MessageID: 1})
	if got := nilBuf.Snapshot(-1, 0); got != nil {
		t.Fatalf("nil buffer must be inert, got %+v", got)
	}
	if newRecentContextBuffer(0) != nil {
		t.Fatal("size 0 must disable the buffer")
	}
}

func TestRecentContextBufferEvictsLeastRecentlyUpdatedChat(t *testing.T) {
	b := newRecentContextBuffer(2)
	for i := int64(1); i <= maxRecentContextChats; i++ {
		b.Record(i, 0, recentEntry{MessageID: 1, Text: "x"})
	}
	// Touch chat 1 so chat 2 becomes the stalest, then overflow by one.
	b.Record(1, 0, recentEntry{MessageID: 2, Text: "y"})
	b.Record(maxRecentContextChats+1, 0, recentEntry{MessageID: 1, Text: "z"})
	if got := b.Snapshot(2, 0); got != nil {
		t.Fatalf("stalest chat should be evicted, got %+v", got)
	}
	if got := b.Snapshot(1, 0); len(got) != 2 {
		t.Fatalf("recently touched chat must survive, got %+v", got)
	}
	if got := b.Snapshot(maxRecentContextChats+1, 0); len(got) != 1 {
		t.Fatalf("new chat must be recorded, got %+v", got)
	}
}

func TestRecentContextBufferTruncatesLongMessages(t *testing.T) {
	b := newRecentContextBuffer(1)
	long := strings.Repeat("字", maxRecentContextTextRunes+5)
	b.Record(-1, 0, recentEntry{MessageID: 1, Sender: "Ada", Text: long})
	got := b.Snapshot(-1, 0)
	if len(got) != 1 || len([]rune(got[0].Text)) != maxRecentContextTextRunes+1 || !strings.HasSuffix(got[0].Text, "…") {
		t.Fatalf("text should be cut to %d runes plus an ellipsis, got %d runes", maxRecentContextTextRunes, len([]rune(got[0].Text)))
	}
}

func TestRecentEntryFromMessageRendersMediaPlaceholder(t *testing.T) {
	e := recentEntryFromMessage(&Message{MessageID: 4, From: &User{ID: 1, FirstName: "Ada"}, Photo: []PhotoSize{{FileID: "p"}}})
	if e.Sender != "Ada" || e.Text != "[Image]" {
		t.Fatalf("media without caption should render its placeholder, got %+v", e)
	}
	e = recentEntryFromMessage(&Message{MessageID: 5, Photo: []PhotoSize{{FileID: "p"}}, Caption: "see this"})
	if e.Sender != "Unknown user" || e.Text != "[Image]\nsee this" {
		t.Fatalf("the placeholder should lead the caption and a missing sender should fall back, got %+v", e)
	}
	e = recentEntryFromMessage(&Message{MessageID: 6, From: &User{ID: 1, FirstName: "Ada"}, Sticker: &struct{}{}})
	if e.Text != "[unknown message]" {
		t.Fatalf("a kind without a file should keep the typed placeholder, got %+v", e)
	}
}

func TestInboundGroupMentionInlinesRecentContext(t *testing.T) {
	ada := &User{ID: 111, FirstName: "Ada"}
	bob := &User{ID: 222, FirstName: "Bob"}
	b := newRecentContextBuffer(DefaultRecentContextSize)
	b.Record(-100200, 0, recentEntry{MessageID: 1, Sender: "Ada", Text: "deploy is failing on staging"})
	b.Record(-100200, 0, recentEntry{MessageID: 2, Sender: "Bob", Text: "I think it's the env file"})
	// The trigger itself is in the ring too (dispatch records every group
	// message) and must be filtered out of its own context.
	b.Record(-100200, 0, recentEntry{MessageID: 3, Sender: "Ada", Text: "@my_bot what should we check first?"})

	msg, ok := inboundFromUpdateWithContext(groupUpdate(3, ada, "@my_bot what should we check first?", nil, 0), 999, "my_bot", b)
	if !ok || !msg.AddressedToBot {
		t.Fatalf("mention must be addressed: ok=%v msg=%+v", ok, msg)
	}
	want := "<recent_context count=\"2\">\n[Ada]: deploy is failing on staging\n[Bob]: I think it's the env file\n</recent_context>\n\nwhat should we check first?"
	if msg.Text != want {
		t.Fatalf("Text =\n%s\nwant\n%s", msg.Text, want)
	}
	if msg.CommandText != "what should we check first?" {
		t.Fatalf("CommandText must stay the bare instruction, got %q", msg.CommandText)
	}
	if msg.HasSelectedContext {
		t.Fatal("ambient recent context is not selected context")
	}

	// Reply-to-human + mention: quoted parent is rendered once, as
	// <quoted_message>, and dropped from the recent window; composition is
	// recent → quoted → own.
	quoted := &Message{MessageID: 2, From: bob, Text: "I think it's the env file"}
	msg, ok = inboundFromUpdateWithContext(groupUpdate(3, ada, "@my_bot is he right?", quoted, 0), 999, "my_bot", b)
	if !ok || !msg.HasSelectedContext {
		t.Fatalf("quoted mention must be selected context: ok=%v msg=%+v", ok, msg)
	}
	recentIdx := strings.Index(msg.Text, "<recent_context count=\"1\">")
	quotedIdx := strings.Index(msg.Text, "<quoted_message")
	ownIdx := strings.Index(msg.Text, "is he right?")
	if recentIdx != 0 || quotedIdx < recentIdx || ownIdx < quotedIdx {
		t.Fatalf("blocks out of order:\n%s", msg.Text)
	}
	if strings.Count(msg.Text, "I think it's the env file") != 1 {
		t.Fatalf("quoted parent must not repeat inside recent context:\n%s", msg.Text)
	}
}

func TestInboundRecentContextScopedToTopic(t *testing.T) {
	ada := &User{ID: 111, FirstName: "Ada"}
	b := newRecentContextBuffer(DefaultRecentContextSize)
	b.Record(-100200, 0, recentEntry{MessageID: 1, Sender: "Ada", Text: "general chatter"})
	b.Record(-100200, 42, recentEntry{MessageID: 2, Sender: "Bob", Text: "topic chatter"})

	msg, ok := inboundFromUpdateWithContext(groupUpdate(3, ada, "@my_bot summarize", nil, 42), 999, "my_bot", b)
	if !ok || !strings.Contains(msg.Text, "[Bob]: topic chatter") || strings.Contains(msg.Text, "general chatter") {
		t.Fatalf("topic mention must only see its own topic:\n%s", msg.Text)
	}
}

func TestInboundRecentContextSkipped(t *testing.T) {
	ada := &User{ID: 111, FirstName: "Ada"}
	b := newRecentContextBuffer(DefaultRecentContextSize)
	b.Record(-100200, 0, recentEntry{MessageID: 1, Sender: "Bob", Text: "earlier"})
	b.Record(555, 0, recentEntry{MessageID: 1, Sender: "Bob", Text: "never buffered in practice"})

	cases := []struct {
		name string
		u    Update
	}{
		{"unaddressed group message", groupUpdate(3, ada, "plain chatter", nil, 0)},
		{"/new starts a chat without ambient context", groupUpdate(3, ada, "@my_bot /new fresh topic", nil, 0)},
		{"p2p", Update{UpdateID: 3, Message: &Message{MessageID: 3, From: ada, Chat: Chat{ID: 555, Type: "private"}, Text: "hi"}}},
	}
	for _, c := range cases {
		msg, ok := inboundFromUpdateWithContext(c.u, 999, "my_bot", b)
		if !ok {
			t.Fatalf("%s: expected ok", c.name)
		}
		if strings.Contains(msg.Text, "<recent_context") {
			t.Fatalf("%s: must not carry recent context, got %q", c.name, msg.Text)
		}
	}
	// /clear keeps the window: it resets agent memory, not the conversation.
	msg, _ := inboundFromUpdateWithContext(groupUpdate(3, ada, "@my_bot /clear again", nil, 0), 999, "my_bot", b)
	if !msg.ForceFresh || !strings.Contains(msg.Text, "[Bob]: earlier") {
		t.Fatalf("/clear should keep recent context, got forceFresh=%v text=%q", msg.ForceFresh, msg.Text)
	}
	// nil source (feature disabled) leaves an addressed message untouched.
	msg, _ = inboundFromUpdateWithContext(groupUpdate(3, ada, "@my_bot hello", nil, 0), 999, "my_bot", nil)
	if msg.Text != "hello" {
		t.Fatalf("disabled buffer must not alter text, got %q", msg.Text)
	}
}

func TestDispatchBuffersGroupMessagesForLaterMentions(t *testing.T) {
	var seen []channel.InboundMessage
	c := &telegramChannel{
		botID: 999, botUsername: "my_bot",
		handler: func(_ context.Context, msg channel.InboundMessage) error {
			seen = append(seen, msg)
			return nil
		},
		logger: testLogger(),
		recent: newRecentContextBuffer(DefaultRecentContextSize),
	}
	ada := &User{ID: 111, FirstName: "Ada"}
	bob := &User{ID: 222, FirstName: "Bob"}
	ctx := context.Background()

	// Unaddressed chatter still reaches the handler (Router audits the drop)
	// and is buffered. Unaddressed media is buffered as a placeholder and
	// never sent an "unsupported" notice.
	if err := c.dispatch(ctx, groupUpdate(1, ada, "deploy is failing on staging", nil, 0)); err != nil {
		t.Fatal(err)
	}
	if err := c.dispatch(ctx, Update{UpdateID: 2, Message: &Message{MessageID: 2, From: bob, Chat: Chat{ID: -100200, Type: "supergroup"}, Photo: []PhotoSize{{FileID: "p"}}}}); err != nil {
		t.Fatal(err)
	}
	// Another bot's message is never buffered.
	if err := c.dispatch(ctx, groupUpdate(3, &User{ID: 5, IsBot: true, FirstName: "Other"}, "beep", nil, 0)); err != nil {
		t.Fatal(err)
	}
	if err := c.dispatch(ctx, groupUpdate(4, ada, "@my_bot what should we check first?", nil, 0)); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0].AddressedToBot || !seen[1].AddressedToBot {
		t.Fatalf("handler calls = %+v", seen)
	}
	want := "<recent_context count=\"2\">\n[Ada]: deploy is failing on staging\n[Bob]: [Image]\n</recent_context>\n\nwhat should we check first?"
	if seen[1].Text != want {
		t.Fatalf("mention Text =\n%s\nwant\n%s", seen[1].Text, want)
	}
	// The mention itself is now part of the window for the next one.
	if err := c.dispatch(ctx, groupUpdate(5, bob, "@my_bot and then?", nil, 0)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen[2].Text, "[Ada]: @my_bot what should we check first?") {
		t.Fatalf("previous mention should appear as members saw it:\n%s", seen[2].Text)
	}
}
