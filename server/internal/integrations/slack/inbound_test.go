package slack

import (
	"encoding/json"
	"testing"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func eventsAPI(inner any) slackevents.EventsAPIEvent {
	return slackevents.EventsAPIEvent{
		TeamID:   "T1",
		APIAppID: "A1",
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Data: inner,
		},
	}
}

// translateMessage runs the message-event translation as the AppConnector does:
// resolve the team's bot user id, then normalize.
func translateMessage(botUserID string, e slackevents.EventsAPIEvent, m *slackevents.MessageEvent) (channel.InboundMessage, bool) {
	return inboundFromMessage(e, m, botUserID, compileMentionRe(botUserID))
}

func translateAppMention(botUserID string, e slackevents.EventsAPIEvent, m *slackevents.AppMentionEvent) (channel.InboundMessage, bool) {
	return inboundFromAppMention(e, m, botUserID, compileMentionRe(botUserID))
}

func TestInboundFromMessage_DM(t *testing.T) {
	msg, ok := translateMessage("UBOT", eventsAPI(nil), &slackevents.MessageEvent{
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
	if msg.CommandText != "hello bot" {
		t.Errorf("CommandText = %q, want normalized original text", msg.CommandText)
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
	msg, ok := translateMessage("UBOT", eventsAPI(nil), &slackevents.MessageEvent{
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
	if msg.CommandText != "create an issue" {
		t.Errorf("CommandText = %q, want the same pre-rewrite text", msg.CommandText)
	}
}

func TestInboundFromMessage_ChannelNoMention(t *testing.T) {
	msg, ok := translateMessage("UBOT", eventsAPI(nil), &slackevents.MessageEvent{
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
	msg, ok := translateMessage("UBOT", eventsAPI(nil), &slackevents.MessageEvent{
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
			if _, ok := translateMessage("UBOT", eventsAPI(nil), tc.m); ok {
				t.Errorf("%s should not be ingested", tc.name)
			}
		})
	}
}

func TestInboundFromAppMention(t *testing.T) {
	msg, ok := translateAppMention("UBOT", eventsAPI(nil), &slackevents.AppMentionEvent{
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
	if _, ok := translateAppMention("UBOT", eventsAPI(nil), &slackevents.AppMentionEvent{User: "UBOT", Channel: "C1", TimeStamp: "1.9"}); ok {
		t.Error("bot's own mention should be skipped")
	}
}

// Files are read off the normalized Msg, which slack-go's custom unmarshaller
// populates from the top-level payload for brand-new messages. That behaviour
// is the whole feature's foundation, so this drives the real decoder rather
// than hand-building MessageEvent.Message — a struct literal would keep
// passing if slack-go ever stopped normalizing.
func TestInboundFromMessage_FileShare(t *testing.T) {
	payload := `{
	  "token": "z", "team_id": "T1", "api_app_id": "A1", "type": "event_callback", "event_id": "Ev1",
	  "event": {
	    "type": "message", "subtype": "file_share",
	    "channel": "D123", "channel_type": "im",
	    "user": "UALICE", "text": "here is the doc", "ts": "1700000000.000800",
	    "files": [
	      {"id": "F1", "name": "report.pdf", "mimetype": "application/pdf", "size": 42,
	       "url_private": "https://files.slack.com/files-pri/T1-F1/report.pdf",
	       "url_private_download": "https://files.slack.com/files-pri/T1-F1/download/report.pdf"},
	      {"id": "F2", "name": "shot.png", "mimetype": "image/png",
	       "url_private": "https://files.slack.com/files-pri/T1-F2/shot.png"},
	      {"id": "F3", "name": "external.doc"},
	      {"id": "F4", "name": "spec.gdoc", "mode": "external",
	       "url_private": "https://docs.google.com/document/d/abc/edit"}
	    ]
	  }
	}`
	e, err := slackevents.ParseEvent(json.RawMessage(payload), slackevents.OptionNoVerifyToken())
	if err != nil {
		t.Fatalf("parse event: %v", err)
	}
	inner, isMessage := e.InnerEvent.Data.(*slackevents.MessageEvent)
	if !isMessage {
		t.Fatalf("inner event = %T, want *slackevents.MessageEvent", e.InnerEvent.Data)
	}
	msg, ok := translateMessage("UBOT", e, inner)
	if !ok {
		t.Fatal("file_share message should be ingestable")
	}
	var raw slackRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if len(raw.Files) != 2 {
		t.Fatalf("raw files = %+v, want only F1 and F2 (no URL, and off-Slack, are dropped)", raw.Files)
	}
	if raw.Files[0].ID != "F1" || raw.Files[0].Name != "report.pdf" || raw.Files[0].Mimetype != "application/pdf" || raw.Files[0].Size != 42 {
		t.Errorf("file[0] = %+v", raw.Files[0])
	}
	if raw.Files[0].DownloadURL != "https://files.slack.com/files-pri/T1-F1/download/report.pdf" {
		t.Errorf("file[0] url = %q, want url_private_download", raw.Files[0].DownloadURL)
	}
	if raw.Files[1].ID != "F2" || raw.Files[1].DownloadURL != "https://files.slack.com/files-pri/T1-F2/shot.png" {
		t.Errorf("file[1] = %+v, want url_private fallback", raw.Files[1])
	}
}

// A message whose only file lives off Slack must not claim media at all: the
// resolver could never fetch it, and pretending otherwise defers the run and
// reserves a storage key for a download that always fails.
func TestInboundFromMessage_ExternalFilesCarryNoMedia(t *testing.T) {
	msg, ok := translateMessage("UBOT", eventsAPI(nil), &slackevents.MessageEvent{
		User:        "UALICE",
		Text:        "shared a doc",
		SubType:     "file_share",
		Channel:     "D123",
		ChannelType: "im",
		TimeStamp:   "1700000000.001000",
		Message: &slack.Msg{
			Files: []slack.File{
				{ID: "F1", Name: "spec.gdoc", Mode: "external", URLPrivate: "https://docs.google.com/document/d/abc/edit"},
			},
		},
	})
	if !ok {
		t.Fatal("the message itself is still ingestable as text")
	}
	var raw slackRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if len(raw.Files) != 0 {
		t.Errorf("raw files = %+v, want none for an off-Slack file", raw.Files)
	}
	resolver := NewMediaResolver(nil, newFakeMediaStorage(), &fakeMediaLedger{}, nil)
	if resolver.HasMedia(msg) {
		t.Error("HasMedia = true for a message whose only file is unfetchable")
	}
}

func TestInboundFromAppMention_Files(t *testing.T) {
	msg, ok := translateAppMention("UBOT", eventsAPI(nil), &slackevents.AppMentionEvent{
		User:      "UALICE",
		Text:      "<@UBOT> look at this",
		Channel:   "C123",
		TimeStamp: "1700000000.000900",
		Files: []slack.File{
			{ID: "F9", Name: "log.txt", Mimetype: "text/plain", URLPrivateDownload: "https://files.slack.com/files-pri/T1-F9/download/log.txt"},
		},
	})
	if !ok {
		t.Fatal("app_mention with files should be ingestable")
	}
	var raw slackRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if len(raw.Files) != 1 || raw.Files[0].ID != "F9" {
		t.Errorf("raw files = %+v, want the mention's file", raw.Files)
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
	// A multi-party DM is a group: plain chatter must NOT be addressed to bot.
	msg, ok := translateMessage("UBOT", eventsAPI(nil), &slackevents.MessageEvent{
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

func TestDecodeCredentials(t *testing.T) {
	// app_id holds the team_id routing key; the bot token is stored as base64
	// plaintext here (nil Decrypter = identity).
	raw := json.RawMessage(`{
		"app_id": "T1",
		"bot_user_id": "UBOT",
		"bot_token_encrypted": "eG94Yi1ib3Q="
	}`)
	creds, err := decodeCredentials(raw, nil)
	if err != nil {
		t.Fatalf("decodeCredentials: %v", err)
	}
	if creds.TeamID != "T1" || creds.BotUserID != "UBOT" {
		t.Errorf("creds = %+v", creds)
	}
	if creds.BotToken != "xoxb-bot" {
		t.Errorf("bot token = %q", creds.BotToken)
	}
	if _, err := decodeCredentials(nil, nil); err == nil {
		t.Error("empty config should error")
	}
}
