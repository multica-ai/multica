package mattermost

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

const (
	testBotID       = "botuserid0000000000000000"
	testBotUsername = "multica"
	testAppID       = "mm.example.com:" + testBotID
)

func defaultParams() inboundParams {
	return inboundParams{appID: testAppID, botUserID: testBotID, botUsername: testBotUsername}
}

func textPost(message string) Post {
	return Post{
		ID:        "post1",
		UserID:    "humanuser000000000000000",
		ChannelID: "chan1",
		Message:   message,
	}
}

func channelData() postedData {
	return postedData{ChannelType: "O", SenderName: "@alice", TeamID: "team1"}
}

func dmData() postedData {
	return postedData{ChannelType: "D", SenderName: "@alice"}
}

// Suppression: everything the core must never see.
func TestInboundFromPostedSuppression(t *testing.T) {
	tests := []struct {
		name string
		post Post
		data postedData
	}{
		{
			name: "the bot's own post",
			post: Post{ID: "p", UserID: testBotID, ChannelID: "c", Message: "hi"},
			data: channelData(),
		},
		{
			name: "another bot's post",
			post: Post{ID: "p", UserID: "other", ChannelID: "c", Message: "hi",
				Props: map[string]any{"from_bot": "true"}},
			data: channelData(),
		},
		{
			name: "an incoming webhook",
			post: Post{ID: "p", UserID: "other", ChannelID: "c", Message: "hi",
				Props: map[string]any{"from_webhook": "true"}},
			data: channelData(),
		},
		{
			name: "a bot flagged with a real JSON boolean",
			post: Post{ID: "p", UserID: "other", ChannelID: "c", Message: "hi",
				Props: map[string]any{"from_bot": true}},
			data: channelData(),
		},
		{
			name: "a system join message",
			post: Post{ID: "p", UserID: "other", ChannelID: "c", Message: "joined",
				Type: "system_join_channel"},
			data: channelData(),
		},
		{
			name: "a post with no author",
			post: Post{ID: "p", ChannelID: "c", Message: "hi"},
			data: channelData(),
		},
		{
			name: "an unknown channel type",
			post: textPost("hi"),
			data: postedData{ChannelType: "X"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := inboundFromPosted(tc.post, tc.data, defaultParams()); ok {
				t.Fatal("message reached the core, want suppressed")
			}
		})
	}
}

func TestInboundFromPostedChatTypes(t *testing.T) {
	tests := []struct {
		channelType string
		want        channel.ChatType
		addressed   bool
	}{
		{channelType: "D", want: channel.ChatTypeP2P, addressed: true},
		{channelType: "O", want: channel.ChatTypeGroup, addressed: false},
		{channelType: "P", want: channel.ChatTypeGroup, addressed: false},
		{channelType: "G", want: channel.ChatTypeGroup, addressed: false},
	}
	for _, tc := range tests {
		t.Run(tc.channelType, func(t *testing.T) {
			msg, ok := inboundFromPosted(textPost("hello"), postedData{ChannelType: tc.channelType}, defaultParams())
			if !ok {
				t.Fatal("message suppressed, want ingested")
			}
			if msg.Source.ChatType != tc.want {
				t.Errorf("ChatType = %q, want %q", msg.Source.ChatType, tc.want)
			}
			// A group message with no mention is ingested but NOT addressed;
			// the engine decides what to do with that.
			if msg.AddressedToBot != tc.addressed {
				t.Errorf("AddressedToBot = %v, want %v", msg.AddressedToBot, tc.addressed)
			}
		})
	}
}

func TestInboundFromPostedFieldMapping(t *testing.T) {
	post := textPost("@multica please look at this")
	post.RootID = "root9"
	msg, ok := inboundFromPosted(post, channelData(), defaultParams())
	if !ok {
		t.Fatal("message suppressed, want ingested")
	}
	// Mattermost post ids are server-unique, so event id and message id are the
	// same value with no compositing.
	if msg.EventID != "post1" || msg.MessageID != "post1" {
		t.Errorf("EventID/MessageID = %q/%q, want post1/post1", msg.EventID, msg.MessageID)
	}
	if msg.Source.ChannelType != TypeMattermost {
		t.Errorf("ChannelType = %q, want %q", msg.Source.ChannelType, TypeMattermost)
	}
	if msg.Source.ChatID != "chan1" {
		t.Errorf("ChatID = %q, want chan1", msg.Source.ChatID)
	}
	if msg.Source.SenderID != "humanuser000000000000000" || msg.Source.SenderStableID != msg.Source.SenderID {
		t.Errorf("sender ids = %q/%q, want them equal", msg.Source.SenderID, msg.Source.SenderStableID)
	}
	if msg.Source.ThreadID != "root9" {
		t.Errorf("ThreadID = %q, want root9", msg.Source.ThreadID)
	}
	if msg.ReplyTo == nil || msg.ReplyTo.RootID != "root9" {
		t.Errorf("ReplyTo = %+v, want the thread root", msg.ReplyTo)
	}
	if msg.Type != channel.MsgTypeText {
		t.Errorf("Type = %q, want text", msg.Type)
	}
	if msg.Text != "please look at this" {
		t.Errorf("Text = %q, want the mention stripped", msg.Text)
	}

	var raw mattermostRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		t.Fatalf("Raw is not decodable: %v", err)
	}
	// Raw carries the routing key; the installation resolver reads it back.
	if raw.AppID != testAppID {
		t.Errorf("Raw.AppID = %q, want %q", raw.AppID, testAppID)
	}
	if raw.EventType != eventPosted {
		t.Errorf("Raw.EventType = %q, want %q", raw.EventType, eventPosted)
	}
	// sender_name arrives as "@alice"; the @ is dropped so it reads as a name.
	if raw.SenderName != "alice" {
		t.Errorf("Raw.SenderName = %q, want alice", raw.SenderName)
	}
}

// A top-level post has no thread anchor, and must not invent one.
func TestInboundFromPostedTopLevelHasNoReplyContext(t *testing.T) {
	msg, ok := inboundFromPosted(textPost("@multica hi"), channelData(), defaultParams())
	if !ok {
		t.Fatal("message suppressed")
	}
	if msg.Source.ThreadID != "" {
		t.Errorf("ThreadID = %q, want empty", msg.Source.ThreadID)
	}
	if msg.ReplyTo != nil {
		t.Errorf("ReplyTo = %+v, want nil", msg.ReplyTo)
	}
}

func TestInboundFromPostedAddressing(t *testing.T) {
	tests := []struct {
		name              string
		message           string
		rootAuthoredByBot bool
		want              bool
	}{
		{name: "explicit mention", message: "@multica hello", want: true},
		{name: "mention mid-sentence", message: "hey @multica look", want: true},
		{name: "mention with trailing period", message: "thanks @multica.", want: true},
		{name: "mention case-insensitive", message: "@MultiCa hi", want: true},
		{name: "no mention", message: "just chatting", want: false},
		// A near-miss username must not address the bot.
		{name: "longer username", message: "@multicabot hi", want: false},
		{name: "hyphenated username", message: "@multica-staging hi", want: false},
		// An email address is not a mention.
		{name: "email address", message: "write to me@multica.com", want: false},
		{name: "reply in a bot-rooted thread", message: "and one more thing", rootAuthoredByBot: true, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := defaultParams()
			p.rootAuthoredByBot = tc.rootAuthoredByBot
			msg, ok := inboundFromPosted(textPost(tc.message), channelData(), p)
			if !ok {
				t.Fatal("message suppressed")
			}
			if msg.AddressedToBot != tc.want {
				t.Fatalf("AddressedToBot for %q = %v, want %v", tc.message, msg.AddressedToBot, tc.want)
			}
		})
	}
}

// @channel and @all notify everyone, which is exactly why addressing reads the
// text rather than Mattermost's "mentions" array.
func TestInboundFromPostedIgnoresBroadcastMentions(t *testing.T) {
	for _, message := range []string{"@channel standup in 5", "@all please review", "@here quick question"} {
		msg, ok := inboundFromPosted(textPost(message), channelData(), defaultParams())
		if !ok {
			t.Fatalf("%q suppressed", message)
		}
		if msg.AddressedToBot {
			t.Fatalf("%q addressed the bot, want ignored", message)
		}
	}
}

func TestStripBotMentions(t *testing.T) {
	tests := []struct{ in, want string }{
		{"@multica hello", "hello"},
		{"hey @multica look at this", "hey look at this"},
		{"@multica", ""},
		{"thanks @multica.", "thanks"},
		{"@multica @multica twice", "twice"},
		// Another user's mention survives: only the bot's own name is noise.
		{"@multica ask @alice about it", "ask @alice about it"},
		{"@multicabot stays", "@multicabot stays"},
		{"no mention here", "no mention here"},
	}
	for _, tc := range tests {
		if got := stripBotMentions(tc.in, testBotUsername); got != tc.want {
			t.Errorf("stripBotMentions(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// An empty bot username disables stripping rather than mangling the text.
	if got := stripBotMentions("@multica hi", ""); got != "@multica hi" {
		t.Errorf("stripBotMentions with no username = %q, want the input unchanged", got)
	}
}

// /clear and /issue must survive mention stripping so the engine's shared
// command parser still sees them.
func TestInboundFromPostedCommands(t *testing.T) {
	t.Run("fresh session command", func(t *testing.T) {
		msg, ok := inboundFromPosted(textPost("@multica /clear"), channelData(), defaultParams())
		if !ok {
			t.Fatal("message suppressed")
		}
		if !msg.ForceFresh {
			t.Error("ForceFresh = false, want true for /clear")
		}
		if msg.CommandText != "/clear" {
			t.Errorf("CommandText = %q, want /clear", msg.CommandText)
		}
	})
	t.Run("issue command reaches CommandText", func(t *testing.T) {
		msg, ok := inboundFromPosted(textPost("@multica /issue Fix the login bug"), channelData(), defaultParams())
		if !ok {
			t.Fatal("message suppressed")
		}
		if msg.CommandText != "/issue Fix the login bug" {
			t.Errorf("CommandText = %q, want the command intact", msg.CommandText)
		}
		if !isAddressedIssueCommand(msg) {
			t.Error("isAddressedIssueCommand = false, want true")
		}
	})
	t.Run("unaddressed issue command is not the bot's business", func(t *testing.T) {
		msg, ok := inboundFromPosted(textPost("/issue not for the bot"), channelData(), defaultParams())
		if !ok {
			t.Fatal("message suppressed")
		}
		if isAddressedIssueCommand(msg) {
			t.Error("isAddressedIssueCommand = true for an unaddressed command, want false")
		}
	})
}

func TestClassifyPost(t *testing.T) {
	tests := []struct {
		name string
		post Post
		want channel.MsgType
	}{
		{name: "text", post: Post{Message: "hi"}, want: channel.MsgTypeText},
		// Text wins over a tag-along attachment: dropping the message because
		// an image rode with it would be worse than ignoring the image.
		{name: "text with files", post: Post{Message: "look", FileIDs: []string{"f1"}}, want: channel.MsgTypeText},
		{name: "files only", post: Post{FileIDs: []string{"f1"}}, want: channel.MsgTypeFile},
		{name: "whitespace only", post: Post{Message: "   "}, want: channel.MsgTypeUnknown},
		{name: "empty", post: Post{}, want: channel.MsgTypeUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyPost(tc.post); got != tc.want {
				t.Fatalf("classifyPost = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnrichWithQuotedPost(t *testing.T) {
	quoted := Post{ID: "quoted1", Message: "the original claim"}
	got := enrichWithQuotedPost("is this right?", quoted, "Alice")
	for _, want := range []string{
		`message_id="quoted1"`,
		`sender="Alice"`,
		`type="text"`,
		"the original claim",
		"is this right?",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("enriched text missing %q:\n%s", want, got)
		}
	}
	// The quote comes first so the instruction reads as a response to it.
	if strings.Index(got, "the original claim") > strings.Index(got, "is this right?") {
		t.Error("instruction precedes the quote, want the quote first")
	}

	// A missing sender or empty body still produces a well-formed block rather
	// than a half-filled one.
	got = enrichWithQuotedPost("", Post{ID: "q2"}, "")
	if !strings.Contains(got, `sender="Unknown user"`) {
		t.Errorf("missing sender not defaulted:\n%s", got)
	}
	if !strings.Contains(got, "[empty or non-text message]") {
		t.Errorf("empty body not described:\n%s", got)
	}
}

func TestDecodePosted(t *testing.T) {
	t.Run("well-formed", func(t *testing.T) {
		// The post arrives as a JSON string inside the data object — that is
		// Mattermost's wire format, and the double decode is the point of this
		// test.
		inner := `{"id":"p1","user_id":"u1","channel_id":"c1","message":"hi","root_id":"r1"}`
		raw, err := json.Marshal(map[string]any{
			"post":         inner,
			"channel_type": "O",
			"sender_name":  "@alice",
			"team_id":      "t1",
		})
		if err != nil {
			t.Fatal(err)
		}
		post, data, ok := decodePosted(raw)
		if !ok {
			t.Fatal("decodePosted = false, want true")
		}
		if post.ID != "p1" || post.ChannelID != "c1" || post.RootID != "r1" || post.Message != "hi" {
			t.Errorf("post = %+v, want the inner fields", post)
		}
		if data.ChannelType != "O" || data.TeamID != "t1" {
			t.Errorf("data = %+v, want the outer fields", data)
		}
	})

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "not an object", raw: `"nope"`},
		{name: "missing post", raw: `{"channel_type":"O"}`},
		{name: "blank post", raw: `{"post":"   ","channel_type":"O"}`},
		{name: "post is not json", raw: `{"post":"{broken","channel_type":"O"}`},
		{name: "post has no id", raw: `{"post":"{\"channel_id\":\"c1\"}","channel_type":"O"}`},
		{name: "post has no channel", raw: `{"post":"{\"id\":\"p1\"}","channel_type":"O"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := decodePosted(json.RawMessage(tc.raw)); ok {
				t.Fatal("decodePosted = true, want false")
			}
		})
	}
}
