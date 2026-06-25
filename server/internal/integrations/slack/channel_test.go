package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func testChannel(botUserID string) *slackChannel {
	return newSlackChannel(credentials{TeamID: "T1", BotUserID: botUserID}, nil, nil, nil)
}

func eventsAPI(inner any) slackevents.EventsAPIEvent {
	return slackevents.EventsAPIEvent{
		TeamID:   "T1",
		APIAppID: "A1",
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Data: inner,
		},
	}
}

func TestInboundFromMessage_DM(t *testing.T) {
	c := testChannel("UBOT")
	e := eventsAPI(nil)
	msg, ok := c.inboundFromMessage(e, &slackevents.MessageEvent{
		User:        "UALICE",
		Text:        "hello bot",
		Channel:     "D123",
		ChannelType: "im",
		TimeStamp:   "1700000000.000100",
	})
	if !ok {
		t.Fatal("expected DM message to be ingestable")
	}
	if msg.Source.ChatType != channel.ChatTypeP2P {
		t.Errorf("ChatType = %q, want p2p", msg.Source.ChatType)
	}
	if !msg.AddressedToBot {
		t.Error("DM should always be addressed to bot")
	}
	if msg.Source.ChannelType != TypeSlack {
		t.Errorf("ChannelType = %q, want slack", msg.Source.ChannelType)
	}
	if msg.MessageID != "1700000000.000100" || msg.EventID != msg.MessageID {
		t.Errorf("MessageID/EventID = %q/%q, want the ts", msg.MessageID, msg.EventID)
	}
	if msg.Source.SenderID != "UALICE" || msg.Source.ChatID != "D123" {
		t.Errorf("sender/chat = %q/%q", msg.Source.SenderID, msg.Source.ChatID)
	}
	if msg.Text != "hello bot" {
		t.Errorf("Text = %q", msg.Text)
	}
	// team_id must be in Raw so the installation resolver can route.
	var raw slackRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if raw.TeamID != "T1" || raw.EventType != "message" {
		t.Errorf("raw = %+v", raw)
	}
}

func TestInboundFromMessage_ChannelMention(t *testing.T) {
	c := testChannel("UBOT")
	msg, ok := c.inboundFromMessage(eventsAPI(nil), &slackevents.MessageEvent{
		User:        "UALICE",
		Text:        "<@UBOT> create an issue",
		Channel:     "C123",
		ChannelType: "channel",
		TimeStamp:   "1700000000.000200",
	})
	if !ok {
		t.Fatal("expected channel message to be ingestable")
	}
	if msg.Source.ChatType != channel.ChatTypeGroup {
		t.Errorf("ChatType = %q, want group", msg.Source.ChatType)
	}
	if !msg.AddressedToBot {
		t.Error("channel message mentioning the bot should be addressed to bot")
	}
	if msg.Text != "create an issue" {
		t.Errorf("Text = %q, want mention stripped", msg.Text)
	}
}

func TestInboundFromMessage_ChannelNoMention(t *testing.T) {
	c := testChannel("UBOT")
	msg, ok := c.inboundFromMessage(eventsAPI(nil), &slackevents.MessageEvent{
		User:        "UALICE",
		Text:        "just chatting with the team",
		Channel:     "C123",
		ChannelType: "channel",
		TimeStamp:   "1700000000.000300",
	})
	if !ok {
		t.Fatal("a non-mention channel message is still ingested; the engine group filter drops it")
	}
	if msg.AddressedToBot {
		t.Error("channel message without a mention must not be addressed to bot")
	}
}

func TestInboundFromMessage_ThreadReply(t *testing.T) {
	c := testChannel("UBOT")
	msg, ok := c.inboundFromMessage(eventsAPI(nil), &slackevents.MessageEvent{
		User:            "UALICE",
		Text:            "<@UBOT> follow up",
		Channel:         "C123",
		ChannelType:     "channel",
		TimeStamp:       "1700000000.000500",
		ThreadTimeStamp: "1700000000.000400",
	})
	if !ok {
		t.Fatal("thread reply should be ingestable")
	}
	if msg.Source.ThreadID != "1700000000.000400" {
		t.Errorf("ThreadID = %q", msg.Source.ThreadID)
	}
	if msg.ReplyTo == nil || msg.ReplyTo.MessageID != "1700000000.000400" {
		t.Errorf("ReplyTo = %+v, want the thread root", msg.ReplyTo)
	}
}

func TestInboundFromMessage_SkipsBotAndOwnAndEdits(t *testing.T) {
	c := testChannel("UBOT")
	cases := []struct {
		name string
		m    *slackevents.MessageEvent
	}{
		{"own message", &slackevents.MessageEvent{User: "UBOT", Text: "hi", Channel: "D1", ChannelType: "im", TimeStamp: "1.1"}},
		{"other bot", &slackevents.MessageEvent{User: "UX", BotID: "B1", Text: "hi", Channel: "C1", TimeStamp: "1.2"}},
		{"bot_message subtype", &slackevents.MessageEvent{SubType: "bot_message", Text: "hi", Channel: "C1", TimeStamp: "1.3"}},
		{"edit", &slackevents.MessageEvent{User: "UALICE", SubType: "message_changed", Text: "hi", Channel: "C1", TimeStamp: "1.4"}},
		{"delete", &slackevents.MessageEvent{User: "UALICE", SubType: "message_deleted", Channel: "C1", TimeStamp: "1.5"}},
		{"empty user", &slackevents.MessageEvent{Text: "hi", Channel: "C1", TimeStamp: "1.6"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := c.inboundFromMessage(eventsAPI(nil), tc.m); ok {
				t.Errorf("%s should not be ingested", tc.name)
			}
		})
	}
}

func TestInboundFromAppMention(t *testing.T) {
	c := testChannel("UBOT")
	msg, ok := c.inboundFromAppMention(eventsAPI(nil), &slackevents.AppMentionEvent{
		User:      "UALICE",
		Text:      "<@UBOT> hi",
		Channel:   "C123",
		TimeStamp: "1700000000.000700",
	})
	if !ok {
		t.Fatal("app_mention should be ingestable")
	}
	if msg.Source.ChatType != channel.ChatTypeGroup || !msg.AddressedToBot {
		t.Errorf("app_mention must be a group message addressed to bot: %+v", msg.Source)
	}
	if msg.Text != "hi" {
		t.Errorf("Text = %q, want mention stripped", msg.Text)
	}
	// The bot's own app_mention echo (BotID set) must be skipped.
	if _, ok := c.inboundFromAppMention(eventsAPI(nil), &slackevents.AppMentionEvent{User: "UBOT", Channel: "C1", TimeStamp: "1.9"}); ok {
		t.Error("bot's own mention should be skipped")
	}
}

func TestCapabilitiesAndType(t *testing.T) {
	c := testChannel("UBOT")
	if c.Type() != TypeSlack {
		t.Errorf("Type = %q", c.Type())
	}
	caps := c.Capabilities()
	if !caps.Has(channel.CapText) || !caps.Has(channel.CapThreadReply) {
		t.Errorf("capabilities = %s, want text + thread_reply", caps)
	}
	// Capabilities the Send path cannot fulfil yet must NOT be declared.
	for _, cap := range []channel.Capability{channel.CapRichCard, channel.CapAttachment, channel.CapMessageEdit} {
		if caps.Has(cap) {
			t.Errorf("capability %s must not be declared until implemented", cap)
		}
	}
}

func TestSlackChatType(t *testing.T) {
	cases := []struct {
		channelID, channelType string
		want                   channel.ChatType
	}{
		{"D123", "im", channel.ChatTypeP2P},
		{"G123", "mpim", channel.ChatTypeGroup}, // multi-party DM is a group
		{"C123", "channel", channel.ChatTypeGroup},
		{"C123", "private_channel", channel.ChatTypeGroup},
		{"D999", "", channel.ChatTypeP2P}, // fallback by id prefix
		{"C999", "", channel.ChatTypeGroup},
	}
	for _, tc := range cases {
		if got := slackChatType(tc.channelID, tc.channelType); got != tc.want {
			t.Errorf("slackChatType(%q,%q) = %q, want %q", tc.channelID, tc.channelType, got, tc.want)
		}
	}
}

func TestMpimRequiresMention(t *testing.T) {
	c := testChannel("UBOT")
	// A multi-party DM is a group: plain chatter must NOT be addressed to bot.
	msg, ok := c.inboundFromMessage(eventsAPI(nil), &slackevents.MessageEvent{
		User: "UALICE", Text: "team lunch?", Channel: "G123", ChannelType: "mpim", TimeStamp: "1.1",
	})
	if !ok {
		t.Fatal("mpim message should still be ingested (engine group filter decides)")
	}
	if msg.Source.ChatType != channel.ChatTypeGroup {
		t.Errorf("mpim ChatType = %q, want group", msg.Source.ChatType)
	}
	if msg.AddressedToBot {
		t.Error("plain mpim chatter must not be addressed to bot")
	}
}

func TestDispatchEventsAPI_PropagatesHandlerError(t *testing.T) {
	wantErr := errors.New("db down")
	calls := 0
	c := newSlackChannel(credentials{TeamID: "T1", BotUserID: "UBOT"}, nil, func(_ context.Context, _ channel.InboundMessage) error {
		calls++
		return wantErr
	}, nil)

	e := eventsAPI(&slackevents.MessageEvent{User: "UALICE", Text: "hi", Channel: "D1", ChannelType: "im", TimeStamp: "1.1"})
	if err := c.dispatchEventsAPI(context.Background(), e); !errors.Is(err, wantErr) {
		t.Errorf("dispatchEventsAPI error = %v, want %v (infra error must propagate to Connect→Supervisor)", err, wantErr)
	}
	if calls != 1 {
		t.Errorf("handler called %d times, want 1", calls)
	}

	// A non-ingestable event (the bot's own message) must not reach the handler
	// and must not error.
	calls = 0
	skip := eventsAPI(&slackevents.MessageEvent{User: "UBOT", Text: "echo", Channel: "D1", ChannelType: "im", TimeStamp: "1.2"})
	if err := c.dispatchEventsAPI(context.Background(), skip); err != nil {
		t.Errorf("skipped event should not error: %v", err)
	}
	if calls != 0 {
		t.Errorf("handler called %d times for skipped event, want 0", calls)
	}
}

func TestDecodeCredentials(t *testing.T) {
	// app_id holds the team_id routing key; tokens stored as base64 plaintext
	// here (nil Decrypter = identity).
	raw := json.RawMessage(`{
		"app_id": "T1",
		"bot_user_id": "UBOT",
		"bot_token_encrypted": "eG94Yi1ib3Q=",
		"app_token_encrypted": "eGFwcC1hcHA="
	}`)
	creds, err := decodeCredentials(raw, nil)
	if err != nil {
		t.Fatalf("decodeCredentials: %v", err)
	}
	if creds.TeamID != "T1" || creds.BotUserID != "UBOT" {
		t.Errorf("creds = %+v", creds)
	}
	if creds.BotToken != "xoxb-bot" || creds.AppToken != "xapp-app" {
		t.Errorf("tokens = %q / %q", creds.BotToken, creds.AppToken)
	}
	if _, err := decodeCredentials(nil, nil); err == nil {
		t.Error("empty config should error")
	}
}

func TestChunkMessage(t *testing.T) {
	if got := chunkMessage("short", 100); len(got) != 1 || got[0] != "short" {
		t.Errorf("short message should be one chunk: %v", got)
	}
	long := make([]rune, 250)
	for i := range long {
		long[i] = 'a'
	}
	chunks := chunkMessage(string(long), 100)
	if len(chunks) != 3 {
		t.Fatalf("250 runes / 100 = 3 chunks, got %d", len(chunks))
	}
	if len([]rune(chunks[0])) != 100 || len([]rune(chunks[2])) != 50 {
		t.Errorf("chunk sizes wrong: %d / %d", len([]rune(chunks[0])), len([]rune(chunks[2])))
	}
}

func TestSend(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C123","ts":"1700000000.111111"}`))
	}))
	defer srv.Close()

	api := slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/"))
	c := newSlackChannel(credentials{TeamID: "T1"}, api, nil, nil)

	res, err := c.Send(context.Background(), channel.OutboundMessage{
		ChatID:   "C123",
		Text:     "reply body",
		ThreadID: "1700000000.000400",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.MessageID != "1700000000.111111" {
		t.Errorf("MessageID = %q", res.MessageID)
	}
	if gotForm.Get("channel") != "C123" || gotForm.Get("text") != "reply body" {
		t.Errorf("posted channel/text = %q / %q", gotForm.Get("channel"), gotForm.Get("text"))
	}
	if gotForm.Get("thread_ts") != "1700000000.000400" {
		t.Errorf("thread_ts = %q, want the inbound thread", gotForm.Get("thread_ts"))
	}
}

// TestSend_AppliesMrkdwn guards the wiring: Send must run the agent's Markdown
// through formatMrkdwn before posting, so Slack renders it instead of showing
// literal markup. (The converter itself is covered in mrkdwn_test.go.)
func TestSend_AppliesMrkdwn(t *testing.T) {
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotText = r.PostForm.Get("text")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C1","ts":"1.1"}`))
	}))
	defer srv.Close()

	api := slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/"))
	c := newSlackChannel(credentials{TeamID: "T1"}, api, nil, nil)

	if _, err := c.Send(context.Background(), channel.OutboundMessage{
		ChatID: "C1",
		Text:   "**bold** see [docs](http://x.com)",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotText != "*bold* see <http://x.com|docs>" {
		t.Errorf("Send must convert Markdown to mrkdwn before posting, got %q", gotText)
	}
}

func TestOutboundThreadTS(t *testing.T) {
	if got := outboundThreadTS(channel.OutboundMessage{ReplyTo: "111.1", ThreadID: "222.2"}); got != "111.1" {
		t.Errorf("explicit ReplyTo should win: %q", got)
	}
	if got := outboundThreadTS(channel.OutboundMessage{ThreadID: "222.2"}); got != "222.2" {
		t.Errorf("thread fallback: %q", got)
	}
	if got := outboundThreadTS(channel.OutboundMessage{}); got != "" {
		t.Errorf("top-level send has no thread: %q", got)
	}
}
