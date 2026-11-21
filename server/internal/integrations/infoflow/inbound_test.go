package infoflow

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestInboundFromEvent_PrivateText(t *testing.T) {
	ev := eventFromPayload(t, `{"MsgType":"text","MsgId":"msg-1","FromUserId":"user-1","text":{"content":"  hello bot  "}}`)
	msg, ok := inboundFromEvent("appkey-A", ev)
	if !ok {
		t.Fatal("expected a private text message to be ingested")
	}
	if msg.Source.ChatType != channel.ChatTypeP2P {
		t.Errorf("chat type = %q, want p2p", msg.Source.ChatType)
	}
	if !msg.AddressedToBot {
		t.Error("a 1:1 message is always addressed to the bot")
	}
	if msg.Text != "hello bot" || msg.CommandText != msg.Text {
		t.Errorf("text/command = %q/%q, want hello bot", msg.Text, msg.CommandText)
	}
	if msg.Source.ChatID != "u:user-1" {
		t.Errorf("chat id = %q, want u:user-1", msg.Source.ChatID)
	}
	raw, err := decodeRawEvent(msg)
	if err != nil {
		t.Fatalf("decodeRawEvent: %v", err)
	}
	if raw.AppID != "appkey-A" || raw.MessageID != "msg-1" || raw.SenderID != "user-1" {
		t.Errorf("raw envelope = %+v", raw)
	}
}

func TestInboundFromEvent_GroupText(t *testing.T) {
	ev := eventFromPayload(t, `{
		"groupid": 123,
		"eventtype": "MESSAGE_RECEIVE",
		"message": {
			"header": {"msgtype": "text", "messageid": "gmsg-1", "fromuserid": "member-1"},
			"body": [{"type": "TEXT", "content": "  hi group  "}]
		}
	}`)
	msg, ok := inboundFromEvent("appkey-B", ev)
	if !ok {
		t.Fatal("expected a group text message to be ingested")
	}
	if msg.Source.ChatType != channel.ChatTypeGroup {
		t.Errorf("chat type = %q, want group", msg.Source.ChatType)
	}
	if msg.Source.ChatID != "g:123" {
		t.Errorf("chat id = %q, want g:123", msg.Source.ChatID)
	}
	if msg.Source.SenderID != "member-1" || msg.MessageID != "gmsg-1" {
		t.Errorf("sender/message = %q/%q", msg.Source.SenderID, msg.MessageID)
	}
	if msg.Text != "hi group" {
		t.Errorf("text = %q, want hi group", msg.Text)
	}
}

func TestInboundFromEvent_Drops(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "missing sender", raw: `{"MsgType":"text","MsgId":"m1","text":{"content":"hi"}}`},
		{name: "missing message id", raw: `{"MsgType":"text","FromUserId":"u1","text":{"content":"hi"}}`},
		{name: "empty text", raw: `{"MsgType":"text","MsgId":"m1","FromUserId":"u1"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := eventFromPayload(t, tc.raw)
			if _, ok := inboundFromEvent("app", ev); ok {
				t.Fatalf("expected drop, got ingest for %s", tc.name)
			}
		})
	}
}

func TestExtractText_GroupBodyAndFallback(t *testing.T) {
	group := map[string]any{
		"message": map[string]any{
			"body": []any{
				map[string]any{"type": "TEXT", "content": "one"},
				map[string]any{"type": "IMAGE", "content": "ignored"},
				map[string]any{"type": "MD", "content": "two"},
			},
		},
	}
	if got := extractText(group, true); got != "one\ntwo" {
		t.Errorf("group body text = %q, want one\\ntwo", got)
	}
	fallback := map[string]any{"content": "  plain  "}
	if got := extractText(fallback, true); got != "plain" {
		t.Errorf("group content fallback = %q", got)
	}
	if got := extractText(map[string]any{"md": map[string]any{"content": "  md  "}}, false); got != "md" {
		t.Errorf("md text = %q", got)
	}
}

func TestNormalizeMsgType(t *testing.T) {
	tests := map[string]channel.MsgType{
		"text":     channel.MsgTypeText,
		"MD":       channel.MsgTypeText,
		"":         channel.MsgTypeText,
		"image":    channel.MsgTypeImage,
		"file":     channel.MsgTypeFile,
		"voice":    channel.MsgTypeAudio,
		"video":    channel.MsgTypeVideo,
		"whatever": channel.MsgTypeUnknown,
	}
	for in, want := range tests {
		if got := normalizeMsgType(in); got != want {
			t.Errorf("normalizeMsgType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChatIDRoundTrip(t *testing.T) {
	tests := []struct {
		chatID   string
		wantKind channel.ChatType
		wantRaw  string
	}{
		{chatID: formatChatID(channel.ChatTypeP2P, "u1"), wantKind: channel.ChatTypeP2P, wantRaw: "u1"},
		{chatID: formatChatID(channel.ChatTypeGroup, "42"), wantKind: channel.ChatTypeGroup, wantRaw: "42"},
		{chatID: "unprefixed", wantKind: channel.ChatTypeP2P, wantRaw: "unprefixed"},
	}
	for _, tc := range tests {
		kind, raw := splitChatID(tc.chatID)
		if kind != tc.wantKind || raw != tc.wantRaw {
			t.Errorf("splitChatID(%q) = %q/%q, want %q/%q", tc.chatID, kind, raw, tc.wantKind, tc.wantRaw)
		}
		target := targetFromChatID(tc.chatID)
		if target.ChatType != tc.wantKind || target.ChatID != tc.wantRaw {
			t.Errorf("targetFromChatID(%q) = %+v", tc.chatID, target)
		}
	}
}

func TestGroupIDInt64(t *testing.T) {
	if id, err := groupIDInt64("123"); err != nil || id != 123 {
		t.Errorf("groupIDInt64(123) = %d, %v", id, err)
	}
	for _, bad := range []string{"", "abc", "0", "-1"} {
		if _, err := groupIDInt64(bad); err == nil {
			t.Errorf("groupIDInt64(%q) expected error", bad)
		}
	}
}
