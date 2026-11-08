package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestInboundDirectMessageIsAlwaysAddressed(t *testing.T) {
	msg, ok := inboundFromGatewayMessage("bot-1", gatewayMessage{
		ID:        "m1",
		ChannelID: "dm-1",
		Content:   "hello",
		Author:    gatewayAuthor{ID: "u1", Username: "ada"},
	})
	if !ok {
		t.Fatal("direct message dropped")
	}
	if msg.Source.ChatType != channel.ChatTypeP2P {
		t.Fatalf("chat type = %q, want p2p", msg.Source.ChatType)
	}
	if !msg.AddressedToBot {
		t.Fatal("direct message must be addressed")
	}
	if msg.Text != "hello" || msg.CommandText != "hello" {
		t.Fatalf("text = %q / command = %q", msg.Text, msg.CommandText)
	}
	var raw discordRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		t.Fatalf("raw: %v", err)
	}
	if raw.BotID != "bot-1" || raw.EventType != "message" {
		t.Fatalf("raw = %+v", raw)
	}
}

func TestInboundGuildMentionStripsBotMention(t *testing.T) {
	msg, ok := inboundFromGatewayMessage("bot-1", gatewayMessage{
		ID:        "m2",
		ChannelID: "ch-1",
		GuildID:   "g1",
		Content:   "<@!bot-1> /issue Fix the build",
		Author:    gatewayAuthor{ID: "u1"},
		Mentions:  []gatewayMention{{ID: "bot-1"}},
	})
	if !ok {
		t.Fatal("mention dropped")
	}
	if msg.Source.ChatType != channel.ChatTypeGroup || !msg.AddressedToBot {
		t.Fatalf("chat type = %q addressed = %v", msg.Source.ChatType, msg.AddressedToBot)
	}
	if msg.Text != "/issue Fix the build" {
		t.Fatalf("text = %q", msg.Text)
	}
	if msg.CommandText != "/issue Fix the build" {
		t.Fatalf("command text = %q", msg.CommandText)
	}
}

func TestInboundGuildUnaddressedIsNotAddressed(t *testing.T) {
	msg, ok := inboundFromGatewayMessage("bot-1", gatewayMessage{
		ID:        "m3",
		ChannelID: "ch-1",
		GuildID:   "g1",
		Content:   "chatter",
		Author:    gatewayAuthor{ID: "u1"},
	})
	if !ok {
		t.Fatal("message dropped before the router could filter it")
	}
	if msg.AddressedToBot {
		t.Fatal("plain guild chatter must not be addressed")
	}
}

func TestInboundDropsBotsAndSelf(t *testing.T) {
	for name, m := range map[string]gatewayMessage{
		"self":  {ID: "m4", Content: "hi", Author: gatewayAuthor{ID: "bot-1"}},
		"other": {ID: "m5", Content: "hi", Author: gatewayAuthor{ID: "b2", Bot: true}},
	} {
		if _, ok := inboundFromGatewayMessage("bot-1", m); ok {
			t.Fatalf("%s: message ingested", name)
		}
	}
}

func TestInboundAttachmentOnlyMessageGetsPlaceholder(t *testing.T) {
	msg, ok := inboundFromGatewayMessage("bot-1", gatewayMessage{
		ID:        "m6",
		ChannelID: "dm-1",
		Author:    gatewayAuthor{ID: "u1"},
		Attachments: []gatewayAttachment{
			{Filename: "report.pdf"},
			{},
		},
	})
	if !ok {
		t.Fatal("attachment-only message dropped")
	}
	if msg.Text != "[File: report.pdf]\n[File: attachment]" {
		t.Fatalf("text = %q", msg.Text)
	}
}

func TestChunkMessageRespectsRuneBoundary(t *testing.T) {
	chunks := chunkMessage("你好世界", 2)
	if len(chunks) != 2 || chunks[0] != "你好" || chunks[1] != "世界" {
		t.Fatalf("chunks = %q", chunks)
	}
	if got := chunkMessage("short", 10); len(got) != 1 || got[0] != "short" {
		t.Fatalf("short chunks = %q", got)
	}
}

func TestSendTextChunksAndQuotesFirstOnly(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/ch-1/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bot tok" {
			t.Errorf("authorization = %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "sent-" + r.URL.Path})
	}))
	defer srv.Close()

	api := newAPIClient(srv.URL, "tok", srv.Client())
	_, err := sendText(context.Background(), api, "ch-1", strings.Repeat("a", maxMessageRunes+5), "m0")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("sent %d messages, want 2", len(bodies))
	}
	if _, ok := bodies[0]["message_reference"]; !ok {
		t.Fatal("first chunk must quote the triggering message")
	}
	if _, ok := bodies[1]["message_reference"]; ok {
		t.Fatal("later chunks must not repeat the quote")
	}
}

func TestAPIClientReportsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized","code":0}`))
	}))
	defer srv.Close()

	_, err := newAPIClient(srv.URL, "bad", srv.Client()).GetCurrentUser(context.Background())
	if !isUnauthorized(err) {
		t.Fatalf("err = %v, want unauthorized", err)
	}
	if classifyCredentialVerificationError(err) == ErrCredentialsUnverifiable {
		t.Fatal("an explicit rejection must not be reported as unverifiable")
	}
}
