package wechat

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TestInboundFromIlink_P2P covers the p2p path: a 1:1 message becomes a P2P
// chat keyed by the sender, always addressed to the bot, with the context_token
// and bot id stashed in Raw for the resolvers.
func TestInboundFromIlink_P2P(t *testing.T) {
	m := iLinkMessage{
		MsgID:        "msg-1",
		FromUserID:   "alice@im.wechat",
		ToUserID:     "bot-01@im.bot",
		MsgType:      "text",
		Content:      "hello bot",
		ContextToken: "ctx-abc",
	}
	got := inboundFromIlink(m)

	if got.EventID != "msg-1" || got.MessageID != "msg-1" {
		t.Errorf("EventID/MessageID = %q/%q, want msg-1", got.EventID, got.MessageID)
	}
	if got.Source.ChannelType != TypeWechat {
		t.Errorf("Source.ChannelType = %q, want %q", got.Source.ChannelType, TypeWechat)
	}
	if got.Source.ChatType != channel.ChatTypeP2P {
		t.Errorf("Source.ChatType = %q, want p2p", got.Source.ChatType)
	}
	if got.Source.ChatID != "alice@im.wechat" {
		t.Errorf("Source.ChatID = %q, want alice@im.wechat", got.Source.ChatID)
	}
	if got.Source.SenderID != "alice@im.wechat" {
		t.Errorf("Source.SenderID = %q, want alice@im.wechat", got.Source.SenderID)
	}
	if !got.AddressedToBot {
		t.Error("p2p message should always be addressed to bot")
	}
	if got.Type != channel.MsgTypeText {
		t.Errorf("Type = %q, want text", got.Type)
	}
	if got.Text != "hello bot" {
		t.Errorf("Text = %q, want hello bot", got.Text)
	}
	raw := decodeWechatRaw(got)
	if raw.IlinkBotID != "bot-01@im.bot" {
		t.Errorf("Raw.IlinkBotID = %q, want bot-01@im.bot", raw.IlinkBotID)
	}
	if raw.ContextToken != "ctx-abc" {
		t.Errorf("Raw.ContextToken = %q, want ctx-abc", raw.ContextToken)
	}
}

// TestInboundFromIlink_Group covers the group path: a group message is keyed by
// the group id (not the sender), and MVP treats all group messages as addressed.
func TestInboundFromIlink_Group(t *testing.T) {
	m := iLinkMessage{
		MsgID:        "msg-2",
		FromUserID:   "bob@im.wechat",
		ToUserID:     "bot-01@im.bot",
		GroupID:      "group-99",
		MsgType:      "text",
		Content:      "hi everyone",
		ContextToken: "ctx-xyz",
	}
	got := inboundFromIlink(m)

	if got.Source.ChatType != channel.ChatTypeGroup {
		t.Errorf("Source.ChatType = %q, want group", got.Source.ChatType)
	}
	if got.Source.ChatID != "group-99" {
		t.Errorf("Source.ChatID = %q, want group-99", got.Source.ChatID)
	}
	if got.Source.SenderID != "bob@im.wechat" {
		t.Errorf("Source.SenderID = %q, want bob@im.wechat (sender preserved)", got.Source.SenderID)
	}
	if !got.AddressedToBot {
		t.Error("MVP group message should be addressed to bot")
	}
}

// TestInboundFromIlink_NonText maps a non-text type to MsgTypeUnknown and keeps
// the raw type in Raw for diagnostics.
func TestInboundFromIlink_NonText(t *testing.T) {
	got := inboundFromIlink(iLinkMessage{
		MsgID:   "msg-3",
		MsgType: "image",
		Content: "",
	})
	if got.Type != channel.MsgTypeUnknown {
		t.Errorf("Type = %q, want unknown", got.Type)
	}
	raw := decodeWechatRaw(got)
	if raw.MsgType != "image" {
		t.Errorf("Raw.MsgType = %q, want image", raw.MsgType)
	}
}

// TestWechatSessionRouting_P2P verifies the binding key is the chat id for p2p
// (one continuous session per peer) and the context_token + peer land in config.
func TestWechatSessionRouting_P2P(t *testing.T) {
	raw, _ := json.Marshal(wechatRawEvent{IlinkBotID: "b@im.bot", ContextToken: "tok"})
	msg := channel.InboundMessage{
		MessageID: "m1",
		Source: channel.Source{
			ChannelType: TypeWechat,
			ChatID:      "alice@im.wechat",
			ChatType:    channel.ChatTypeP2P,
			SenderID:    "alice@im.wechat",
		},
		Raw: raw,
	}
	bindingKey, config := wechatSessionRouting(msg)
	if bindingKey != "alice@im.wechat" {
		t.Errorf("bindingKey = %q, want alice@im.wechat", bindingKey)
	}
	cfg := extractBindingConfig(config)
	if cfg.ContextToken != "tok" {
		t.Errorf("config.ContextToken = %q, want tok", cfg.ContextToken)
	}
	if cfg.PeerUserID != "alice@im.wechat" {
		t.Errorf("config.PeerUserID = %q, want alice@im.wechat", cfg.PeerUserID)
	}
}

// TestWechatSessionRouting_Group verifies the binding key is the group id and
// the peer is the group (the reply target for a group message is the group).
func TestWechatSessionRouting_Group(t *testing.T) {
	raw, _ := json.Marshal(wechatRawEvent{IlinkBotID: "b@im.bot", ContextToken: "tok", GroupID: "g1"})
	msg := channel.InboundMessage{
		Source: channel.Source{
			ChannelType: TypeWechat,
			ChatID:      "g1",
			ChatType:    channel.ChatTypeGroup,
			SenderID:    "bob@im.wechat",
		},
		Raw: raw,
	}
	bindingKey, config := wechatSessionRouting(msg)
	if bindingKey != "g1" {
		t.Errorf("bindingKey = %q, want g1", bindingKey)
	}
	cfg := extractBindingConfig(config)
	if cfg.PeerUserID != "g1" {
		t.Errorf("config.PeerUserID = %q, want g1 (group is the reply target)", cfg.PeerUserID)
	}
}

// TestDecodeCredentials_Encrypted verifies decrypt + base64 round-trip and that
// base_url flows through.
func TestDecodeCredentials_Encrypted(t *testing.T) {
	// identity decrypter returns the ciphertext bytes as-is; combined with
	// base64-encoded "plaintext" this gives a deterministic round-trip.
	enc := encodeBase64([]byte("secret-bot-token"))
	cfg := installConfig{
		AppID:             "bot@im.bot",
		BotTokenEncrypted: enc,
		BaseURL:           "https://ilink.example",
	}
	raw, _ := json.Marshal(cfg)
	creds, err := decodeCredentials(raw, func(c []byte) ([]byte, error) { return c, nil })
	if err != nil {
		t.Fatalf("decodeCredentials: %v", err)
	}
	if creds.BotToken != "secret-bot-token" {
		t.Errorf("BotToken = %q, want secret-bot-token", creds.BotToken)
	}
	if creds.BaseURL != "https://ilink.example" {
		t.Errorf("BaseURL = %q, want https://ilink.example", creds.BaseURL)
	}
	if creds.AppID != "bot@im.bot" {
		t.Errorf("AppID = %q, want bot@im.bot", creds.AppID)
	}
}

// TestDecodeCredentials_Empty errors on an empty config blob.
func TestDecodeCredentials_Empty(t *testing.T) {
	_, err := decodeCredentials(nil, nil)
	if err == nil {
		t.Error("expected error on empty config")
	}
}

// TestEncodeDecodeSendTarget verifies the "<user>\t<contextToken>" packing the
// Channel.Send path uses to carry both through OutboundMessage.ChatID.
func TestEncodeDecodeSendTarget(t *testing.T) {
	user, token := decodeSendTarget(encodeSendTarget("alice@im.wechat", "ctx-1"))
	if user != "alice@im.wechat" {
		t.Errorf("user = %q, want alice@im.wechat", user)
	}
	if token != "ctx-1" {
		t.Errorf("token = %q, want ctx-1", token)
	}
	// A bare id with no tab yields the id and an empty token.
	u, tok := decodeSendTarget("bare-id")
	if u != "bare-id" || tok != "" {
		t.Errorf("decode bare id: user=%q token=%q, want bare-id/empty", u, tok)
	}
}

// TestChunkMessage verifies long bodies split on rune boundaries under the cap.
func TestChunkMessage(t *testing.T) {
	chunks := chunkMessage("hello", 100)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Errorf("short text: got %v, want [hello]", chunks)
	}
	chunks = chunkMessage("abcdef", 2)
	if len(chunks) != 3 {
		t.Fatalf("6 chars / cap 2: got %d chunks, want 3", len(chunks))
	}
	if want := "ab"; chunks[0] != want {
		t.Errorf("chunk[0] = %q, want %q", chunks[0], want)
	}
}

// TestRandomUIN_NonEmpty sanity-checks the X-WECHAT-UIN nonce generator
// produces a base64 value and varies across calls (best-effort).
func TestRandomUIN_NonEmpty(t *testing.T) {
	a := randomUIN()
	b := randomUIN()
	if a == "" {
		t.Error("randomUIN returned empty string")
	}
	if a == b {
		t.Logf("note: two randomUIN calls collided (acceptable but unlikely): %q", a)
	}
}

// TestOriginWechatChatConstant guards the origin_type label against an
// accidental rename: it MUST match the CHECK constraint migration 162 adds.
func TestOriginWechatChatConstant(t *testing.T) {
	if originWechatChat != "wechat_chat" {
		t.Errorf("originWechatChat = %q, want wechat_chat", originWechatChat)
	}
}
