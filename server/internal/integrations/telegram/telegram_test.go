package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestParseBotID(t *testing.T) {
	cases := []struct {
		token  string
		want   string
		wantOK bool
	}{
		{"8983760937:AAExampleSecretPart", "8983760937", true},
		{" 12345:abc ", "12345", true},
		{"no-colon-token", "", false},
		{":empty-id", "", false},
		{"12345:", "", false},
		{"abc123:secret", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, err := parseBotID(c.token)
		if c.wantOK && (err != nil || got != c.want) {
			t.Errorf("parseBotID(%q) = %q, %v; want %q", c.token, got, err, c.want)
		}
		if !c.wantOK && err == nil {
			t.Errorf("parseBotID(%q) should fail", c.token)
		}
	}
}

func TestInboundFromUpdatePrivateText(t *testing.T) {
	u := Update{
		UpdateID: 42,
		Message: &Message{
			MessageID: 7,
			From:      &User{ID: 111, FirstName: "Ada", LastName: "L"},
			Chat:      Chat{ID: 555, Type: "private"},
			Text:      "hello there",
		},
	}
	msg, ok := inboundFromUpdate(u, 999, "my_bot")
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.EventID != "42" || msg.MessageID != "555:7" {
		t.Errorf("ids: event=%q message=%q", msg.EventID, msg.MessageID)
	}
	if msg.Source.ChatType != channel.ChatTypeP2P || !msg.AddressedToBot {
		t.Errorf("p2p should always be addressed: %+v", msg.Source)
	}
	if msg.Text != "hello there" || msg.CommandText != "hello there" || msg.Type != channel.MsgTypeText {
		t.Errorf("text=%q type=%q", msg.Text, msg.Type)
	}
	var raw telegramRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil || raw.BotID != "999" || raw.SenderName != "Ada L" {
		t.Errorf("raw = %+v, err %v", raw, err)
	}
}

func TestInboundFromUpdateGroupAddressing(t *testing.T) {
	base := func(text string, reply *Message) Update {
		return Update{
			UpdateID: 1,
			Message: &Message{
				MessageID:      2,
				From:           &User{ID: 111, FirstName: "U"},
				Chat:           Chat{ID: -100200, Type: "supergroup"},
				Text:           text,
				ReplyToMessage: reply,
			},
		}
	}
	// Unaddressed group chatter: ingested but not addressed (Router drops it).
	msg, ok := inboundFromUpdate(base("plain chatter", nil), 999, "my_bot")
	if !ok || msg.AddressedToBot {
		t.Errorf("plain group message must not be addressed: ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	// @-mention: addressed, mention token stripped.
	msg, ok = inboundFromUpdate(base("@my_bot do the thing", nil), 999, "my_bot")
	if !ok || !msg.AddressedToBot || msg.Text != "do the thing" {
		t.Errorf("mention: ok=%v addressed=%v text=%q", ok, msg.AddressedToBot, msg.Text)
	}
	// Reply to the bot's own message: addressed.
	botMsg := &Message{MessageID: 1, From: &User{ID: 999, IsBot: true}}
	msg, ok = inboundFromUpdate(base("follow-up", botMsg), 999, "my_bot")
	if !ok || !msg.AddressedToBot {
		t.Errorf("reply-to-bot must be addressed: ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	if msg.Text != "follow-up" || msg.CommandText != "follow-up" {
		t.Fatalf("reply-to-bot text/command = %q/%q", msg.Text, msg.CommandText)
	}
}

func TestInboundGroupHumanReplyRequiresMentionAndPreservesQuotedContext(t *testing.T) {
	humanReply := &Message{
		MessageID: 9,
		From:      &User{ID: 222, FirstName: "Ada", LastName: "Lovelace"},
		Text:      "/issue historical command",
	}
	update := func(text string) Update {
		return Update{UpdateID: 1, Message: &Message{
			MessageID:      10,
			From:           &User{ID: 111, FirstName: "Grace"},
			Chat:           Chat{ID: -100200, Type: "supergroup"},
			Text:           text,
			ReplyToMessage: humanReply,
		}}
	}

	msg, ok := inboundFromUpdate(update("summarize this"), 999, "my_bot")
	if !ok || msg.AddressedToBot {
		t.Fatalf("human reply without mention must not address bot: ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	if msg.Text != "summarize this" {
		t.Fatalf("unaddressed chatter was enriched: %q", msg.Text)
	}

	msg, ok = inboundFromUpdate(update("@my_bot summarize this"), 999, "my_bot")
	if !ok || !msg.AddressedToBot {
		t.Fatalf("mentioned human reply must address bot: ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	if msg.CommandText != "summarize this" {
		t.Fatalf("CommandText = %q, want sender's instruction only", msg.CommandText)
	}
	for _, want := range []string{"sender=\"Ada Lovelace\"", "/issue historical command", "summarize this"} {
		if !strings.Contains(msg.Text, want) {
			t.Fatalf("enriched Text = %q, missing %q", msg.Text, want)
		}
	}
}

func TestInboundGroupHumanReplyNewCommandPreservesQuotedContext(t *testing.T) {
	quoted := &Message{
		MessageID: 9,
		From:      &User{ID: 222, FirstName: "Ada", LastName: "Lovelace"},
		Text:      "the deployment failed after the schema change",
	}
	msg, ok := inboundFromUpdate(Update{UpdateID: 1, Message: &Message{
		MessageID:      10,
		From:           &User{ID: 111, FirstName: "Grace"},
		Chat:           Chat{ID: -100200, Type: "supergroup"},
		Text:           "@my_bot /clear summarize this",
		ReplyToMessage: quoted,
	}}, 999, "my_bot")
	if !ok || !msg.AddressedToBot {
		t.Fatalf("message was not accepted: ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	if !msg.ForceFresh {
		t.Fatal("/clear human reply must request a fresh session before shared routing")
	}
	if msg.CommandText != "/clear summarize this" {
		t.Fatalf("CommandText = %q, want original cleaned command", msg.CommandText)
	}
	for _, want := range []string{
		"sender=\"Ada Lovelace\"",
		"the deployment failed after the schema change",
		"summarize this",
	} {
		if !strings.Contains(msg.Text, want) {
			t.Fatalf("enriched Text = %q, missing %q", msg.Text, want)
		}
	}
	if strings.Contains(msg.Text, "/clear") {
		t.Fatalf("agent-readable Text still contains /clear: %q", msg.Text)
	}
}

func TestInboundGroupChatCommandUsesSameControlNormalization(t *testing.T) {
	msg, ok := inboundFromUpdate(Update{UpdateID: 1, Message: &Message{
		MessageID: 10,
		From:      &User{ID: 111, FirstName: "Grace"},
		Chat:      Chat{ID: -100200, Type: "supergroup"},
		Text:      "@my_bot /new inspect this",
	}}, 999, "my_bot")
	if !ok || !msg.AddressedToBot {
		t.Fatalf("message was not accepted: ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	if msg.Text != "inspect this" || msg.CommandText != "/new inspect this" {
		t.Fatalf("Text/CommandText = %q/%q", msg.Text, msg.CommandText)
	}
	if msg.ForceFresh {
		t.Fatal("/new must not set /clear's ForceFresh semantic")
	}
}

func TestInboundGroupHumanReplyUsesCaptionAndHandlesNonText(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reply  *Message
		wanted string
	}{
		{
			name: "caption",
			reply: &Message{MessageID: 9, From: &User{ID: 222, Username: "ada"},
				Caption: "diagram caption", Photo: []any{struct{}{}},
			},
			wanted: "diagram caption",
		},
		{
			name: "empty non-text",
			reply: &Message{MessageID: 9, From: &User{ID: 222, Username: "ada"},
				Document: &struct {
					FileName string `json:"file_name"`
				}{FileName: "notes.txt"},
			},
			wanted: "[empty or non-text message]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := inboundFromUpdate(Update{UpdateID: 1, Message: &Message{
				MessageID: 10, From: &User{ID: 111, FirstName: "Grace"},
				Chat: Chat{ID: -100200, Type: "supergroup"}, Text: "@my_bot inspect this",
				ReplyToMessage: tc.reply,
			}}, 999, "my_bot")
			if !ok || !msg.AddressedToBot || msg.CommandText != "inspect this" ||
				!strings.Contains(msg.Text, "sender=\"ada\"") || !strings.Contains(msg.Text, tc.wanted) {
				t.Fatalf("message = %+v", msg)
			}
		})
	}
}

func TestInboundFromUpdateDropsBotsAndChannels(t *testing.T) {
	if _, ok := inboundFromUpdate(Update{Message: &Message{
		From: &User{ID: 5, IsBot: true}, Chat: Chat{ID: 1, Type: "private"}, Text: "x",
	}}, 999, "b"); ok {
		t.Error("bot sender must be dropped")
	}
	if _, ok := inboundFromUpdate(Update{Message: &Message{
		From: &User{ID: 5}, Chat: Chat{ID: 1, Type: "channel"}, Text: "x",
	}}, 999, "b"); ok {
		t.Error("channel post must be dropped")
	}
	if _, ok := inboundFromUpdate(Update{}, 999, "b"); ok {
		t.Error("empty update must be dropped")
	}
}

func TestInboundFreshAliasIsPlainText(t *testing.T) {
	u := Update{UpdateID: 1, Message: &Message{
		MessageID: 2, From: &User{ID: 3, FirstName: "U"},
		Chat: Chat{ID: 4, Type: "private"}, Text: "/fresh start over please",
	}}
	msg, ok := inboundFromUpdate(u, 999, "my_bot")
	if !ok || msg.ForceFresh || msg.Text != "/fresh start over please" || msg.CommandText != msg.Text {
		t.Errorf("fresh: ok=%v force=%v text=%q", ok, msg.ForceFresh, msg.Text)
	}
}

func TestInboundTelegramCommandSuffixes(t *testing.T) {
	for _, tc := range []struct {
		text      string
		wantText  string
		wantFresh bool
	}{
		{text: "/fresh@my_bot continue", wantText: "/fresh continue", wantFresh: false},
		{text: "/clear@my_bot continue", wantText: "continue", wantFresh: true},
		{text: "/issue@my_bot fix login", wantText: "/issue fix login", wantFresh: false},
	} {
		u := Update{UpdateID: 1, Message: &Message{
			MessageID: 2, From: &User{ID: 3, FirstName: "U"},
			Chat: Chat{ID: -4, Type: "supergroup"}, Text: tc.text,
		}}
		msg, ok := inboundFromUpdate(u, 999, "my_bot")
		if !ok || !msg.AddressedToBot || msg.ForceFresh != tc.wantFresh || msg.Text != tc.wantText {
			t.Errorf("%q: ok=%v addressed=%v fresh=%v text=%q", tc.text, ok, msg.AddressedToBot, msg.ForceFresh, msg.Text)
		}
	}
}

func TestInboundUsesExactStructuredTelegramMentions(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		entities  []MessageEntity
		addressed bool
		wantText  string
	}{
		{
			name:      "unicode prefix uses UTF-16 entity offsets",
			text:      "😀 @my_bot hello",
			entities:  []MessageEntity{{Type: "mention", Offset: 3, Length: 7}},
			addressed: true,
			wantText:  "😀  hello",
		},
		{
			name:      "similar username is not the bot",
			text:      "@my_bot_extra hello",
			entities:  []MessageEntity{{Type: "mention", Offset: 0, Length: 13}},
			addressed: false,
			wantText:  "@my_bot_extra hello",
		},
		{
			name:      "bot command suffix targets exact bot",
			text:      "/issue@my_bot fix",
			entities:  []MessageEntity{{Type: "bot_command", Offset: 0, Length: 13}},
			addressed: true,
			wantText:  "/issue fix",
		},
		{
			name:      "bot command for a similar bot is ignored",
			text:      "/issue@my_bot_extra fix",
			entities:  []MessageEntity{{Type: "bot_command", Offset: 0, Length: 19}},
			addressed: false,
			wantText:  "/issue@my_bot_extra fix",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := Update{UpdateID: 1, Message: &Message{
				MessageID: 2, From: &User{ID: 3, FirstName: "U"},
				Chat: Chat{ID: -4, Type: "supergroup"}, Text: tc.text, Entities: tc.entities,
			}}
			msg, ok := inboundFromUpdate(u, 999, "my_bot")
			if !ok || msg.AddressedToBot != tc.addressed || msg.Text != tc.wantText {
				t.Fatalf("ok=%v addressed=%v text=%q", ok, msg.AddressedToBot, msg.Text)
			}
		})
	}
}

func TestDispatchIssueErrorSendsFailureNotice(t *testing.T) {
	notice := make(chan sendMessageParams, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got sendMessageParams
		_ = json.NewDecoder(r.Body).Decode(&got)
		notice <- got
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer srv.Close()

	wantErr := errors.New("database unavailable")
	c := &telegramChannel{
		botID: 999, botUsername: "my_bot",
		api:     newBotAPI(srv.URL, "123:abc", srv.Client()),
		handler: func(context.Context, channel.InboundMessage) error { return wantErr },
		logger:  testLogger(),
	}
	err := c.dispatch(context.Background(), Update{UpdateID: 1, Message: &Message{
		MessageID: 2, From: &User{ID: 3, FirstName: "U"},
		Chat: Chat{ID: 42, Type: "private"}, Text: "/issue broken path",
	}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("dispatch error = %v, want %v", err, wantErr)
	}
	select {
	case got := <-notice:
		if got.Text != issueDispatchFailedText || got.ChatID != 42 ||
			got.ReplyParameters == nil || got.ReplyParameters.MessageID != 2 {
			t.Fatalf("notice = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for issue failure notice")
	}
}

func TestTelegramSessionRouting(t *testing.T) {
	p2p := channel.InboundMessage{Source: channel.Source{
		ChatID: "555", ChatType: channel.ChatTypeP2P,
	}}
	key, cfg, thread := telegramSessionRouting(p2p)
	if key != "555" || thread != "" {
		t.Errorf("p2p: key=%q thread=%q", key, thread)
	}
	var bc telegramBindingConfig
	if err := json.Unmarshal(cfg, &bc); err != nil || bc.ChatID != "555" {
		t.Errorf("binding config = %+v, err %v", bc, err)
	}
	topic := channel.InboundMessage{Source: channel.Source{
		ChatID: "-100", ChatType: channel.ChatTypeGroup, ThreadID: "77",
	}}
	key, _, thread = telegramSessionRouting(topic)
	if key != "-100:77" || thread != "77" {
		t.Errorf("forum topic: key=%q thread=%q", key, thread)
	}
}

func TestGetUpdates409IsErrConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request"}`))
	}))
	defer srv.Close()
	api := newBotAPI(srv.URL, "123:abc", srv.Client())
	if _, err := api.GetUpdates(context.Background(), 0); err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestRetryAfterOn429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7}}`))
	}))
	defer srv.Close()
	api := newBotAPI(srv.URL, "123:abc", srv.Client())
	_, err := api.SendMessage(context.Background(), sendMessageParams{ChatID: 1, Text: "x"})
	wait, ok := retryAfter(err)
	if !ok || wait != 7*time.Second {
		t.Fatalf("retryAfter = %v, %v; want 7s", wait, ok)
	}
}

func TestGetWebhookInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getWebhookInfo") {
			t.Fatalf("path = %q, want getWebhookInfo", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"url":"https://example.test/telegram","pending_update_count":3}}`))
	}))
	defer srv.Close()

	info, err := newBotAPI(srv.URL, "123:secret", srv.Client()).GetWebhookInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != "https://example.test/telegram" || info.PendingUpdateCount != 3 {
		t.Fatalf("webhook info = %+v", info)
	}
}

func TestTransportErrorDoesNotExposeBotToken(t *testing.T) {
	transportErr := errors.New("dial tcp 123:secret@example.test:443: connection refused")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, transportErr
	})}

	_, err := newBotAPI("https://api.example.test", "123:secret", client).GetMe(context.Background())
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "123:secret") {
		t.Fatalf("transport error exposed bot token: %v", err)
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("errors.Is lost transport cause: %v", err)
	}
}

func TestConnectDispatchesAndAdvancesOffset(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(r.URL.Path, "getUpdates") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
			return
		}
		n := calls.Add(1)
		var body getUpdatesParams
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch n {
		case 1:
			if body.Offset != 0 {
				t.Errorf("first poll offset = %d, want 0", body.Offset)
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":10,"message":{"message_id":1,"from":{"id":42,"first_name":"A"},"chat":{"id":42,"type":"private"},"text":"hi"}}]}`))
		case 2:
			if body.Offset != 11 {
				t.Errorf("second poll offset = %d, want 11", body.Offset)
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		default:
			// Park until the test cancels.
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	var received []channel.InboundMessage
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := &telegramChannel{
		botID:       999,
		botUsername: "my_bot",
		api:         newBotAPI(srv.URL, "123:abc", srv.Client()),
		handler: func(ctx context.Context, m channel.InboundMessage) error {
			received = append(received, m)
			close(done)
			return nil
		},
		logger: testLogger(),
	}
	go func() { _ = ch.Connect(ctx) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("message not dispatched")
	}
	cancel()
	if len(received) != 1 || received[0].Text != "hi" {
		t.Fatalf("received = %+v", received)
	}
}

func TestChunkMessagePrefersNewlines(t *testing.T) {
	text := strings.Repeat("a", 60) + "\n" + strings.Repeat("b", 60)
	chunks := chunkMessage(text, 100)
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d: %v", len(chunks), chunks)
	}
	if !strings.HasSuffix(chunks[0], "a") || !strings.HasPrefix(chunks[1], "b") {
		t.Errorf("split should land on the newline: %q | %q", chunks[0], chunks[1])
	}
}

func TestChunkMessageCountsUTF16Units(t *testing.T) {
	chunks := chunkMessage("😀a😀", 3)
	if len(chunks) != 2 || chunks[0] != "😀a" || chunks[1] != "😀" {
		t.Fatalf("chunks = %#v", chunks)
	}
	for _, chunk := range chunks {
		if got := utf16Units(chunk); got > 3 {
			t.Errorf("chunk %q uses %d UTF-16 units", chunk, got)
		}
	}
}

func TestSenderFallsBackOnlyForHTMLParseErrors(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer srv.Close()

	s := newSender(newBotAPI(srv.URL, "123:secret", srv.Client()), testLogger())
	result, err := s.Send(context.Background(), channel.OutboundMessage{ChatID: "42", Text: "literal <tag>"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || result.MessageID != "42:7" {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestSenderUsesCurrentReplyParametersOnlyOnFirstChunk(t *testing.T) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests = append(requests, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":42,"type":"private"}}}`, len(requests))
	}))
	defer srv.Close()

	s := newSender(newBotAPI(srv.URL, "123:secret", srv.Client()), testLogger())
	_, err := s.Send(context.Background(), channel.OutboundMessage{
		ChatID: "42", Text: strings.Repeat("x", maxMessageUnits+1), ReplyTo: "42:17",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if _, exists := requests[0]["reply_to_message_id"]; exists {
		t.Fatal("legacy reply_to_message_id must not be sent")
	}
	reply, ok := requests[0]["reply_parameters"].(map[string]any)
	if !ok || reply["message_id"] != float64(17) || reply["allow_sending_without_reply"] != true {
		t.Fatalf("first reply_parameters = %#v", requests[0]["reply_parameters"])
	}
	if _, exists := requests[1]["reply_parameters"]; exists {
		t.Fatalf("second chunk unexpectedly quotes: %#v", requests[1])
	}
}

func TestDispatchUnsupportedMediaInAddressedGroupPreservesTopicAndReply(t *testing.T) {
	var got []sendMessageParams
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body sendMessageParams
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		got = append(got, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7,"chat":{"id":-42,"type":"supergroup"}}}`))
	}))
	defer srv.Close()

	c := &telegramChannel{
		botID: 999, botUsername: "my_bot", api: newBotAPI(srv.URL, "123:secret", srv.Client()),
		handler: func(context.Context, channel.InboundMessage) error { t.Fatal("media reached handler"); return nil },
		logger:  testLogger(),
	}
	base := Message{
		MessageID: 21, From: &User{ID: 3, FirstName: "U"}, Chat: Chat{ID: -42, Type: "supergroup"},
		Photo: []any{struct{}{}}, IsTopicMessage: true, MessageThreadID: 8,
	}
	if err := c.dispatch(context.Background(), Update{UpdateID: 1, Message: &base}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unaddressed group media produced notice: %+v", got)
	}
	base.Caption = "@my_bot look"
	base.CaptionEntities = []MessageEntity{{Type: "mention", Offset: 0, Length: 7}}
	if err := c.dispatch(context.Background(), Update{UpdateID: 2, Message: &base}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != msgUnsupportedType || got[0].MessageThreadID != 8 ||
		got[0].ReplyParameters == nil || got[0].ReplyParameters.MessageID != 21 {
		t.Fatalf("addressed group notice = %+v", got)
	}
}

func TestSendChatActionPreservesForumTopic(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	if err := newBotAPI(srv.URL, "123:secret", srv.Client()).SendChatAction(context.Background(), -42, 8); err != nil {
		t.Fatal(err)
	}
	if got["chat_id"] != float64(-42) || got["action"] != "typing" || got["message_thread_id"] != float64(8) {
		t.Fatalf("sendChatAction params = %#v", got)
	}
}

func TestSendChatActionOmitsEmptyForumTopic(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	if err := newBotAPI(srv.URL, "123:secret", srv.Client()).SendChatAction(context.Background(), 42, 0); err != nil {
		t.Fatal(err)
	}
	if _, exists := got["message_thread_id"]; exists {
		t.Fatalf("empty message_thread_id must be omitted: %#v", got)
	}
}

func TestFormatHTML(t *testing.T) {
	got := formatHTML("# Title\n**bold** and `code` and [link](https://e.co/a_b)\n```go\nx < 1\n```")
	for _, want := range []string{
		"<b>Title</b>",
		"<b>bold</b>",
		"<code>code</code>",
		`<a href="https://e.co/a_b">link</a>`,
		`<pre><code class="language-go">x &lt; 1</code></pre>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatHTML missing %q in:\n%s", want, got)
		}
	}
	if plain := formatHTML("a < b & c"); !strings.Contains(plain, "a &lt; b &amp; c") {
		t.Errorf("entities must be escaped: %q", plain)
	}
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
