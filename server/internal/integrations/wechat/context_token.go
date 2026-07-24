package wechat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file implements the core iLink quirk: an outbound sendmessage MUST echo
// back the context_token carried by the inbound message it replies to, or the
// reply is not associated with the conversation. The token changes with every
// inbound message, so it is refreshed on the chat-session binding's config after
// each inbound append and read back by the outbound subscriber.

// wechatBindingConfig is the opaque JSON persisted on
// channel_chat_session_binding.config for a WeChat session. It carries the
// latest context_token (the outbound reply's association key) and the peer user
// id the reply must be addressed to (recovered at outbound time so the
// subscriber does not need to re-derive it from the message history).
type wechatBindingConfig struct {
	// ContextToken MUST be echoed back on sendmessage or the reply drops. Refreshed
	// on every inbound message.
	ContextToken string `json:"context_token,omitempty"`
	// PeerUserID is the WeChat user id ("xxx@im.wechat") the reply is addressed
	// to. For a p2p chat it is the human sender; for a group it is the group id
	// (the bot posts into the group). Captured at inbound time.
	PeerUserID string `json:"peer_user_id,omitempty"`
}

// wechatSessionRouting derives, from one inbound WeChat message, the two things
// the session layer needs:
//   - bindingKey: the session-isolation key (stored as channel_chat_id). WeChat
//     has no thread/topic concept, so a p2p chat is one continuous session per
//     peer and a group is one continuous session per group — the key is the
//     chat id in both cases.
//   - config: the initial binding config (context_token + peer). The shared
//     ChatSession.EnsureSession stores this when the binding is first created;
//     subsequent inbound messages refresh it via updateContextToken.
//
// It is a pure function so the isolation contract is unit-tested without a DB.
func wechatSessionRouting(msg channel.InboundMessage) (bindingKey string, config []byte) {
	peer := msg.Source.SenderID
	if msg.Source.ChatType == channel.ChatTypeGroup {
		// For a group the reply target is the group id (the chat itself), not the
		// individual sender.
		peer = msg.Source.ChatID
	}
	raw := decodeWechatRaw(msg)
	cfg := wechatBindingConfig{
		ContextToken: raw.ContextToken,
		PeerUserID:   peer,
	}
	config, _ = json.Marshal(cfg)
	return msg.Source.ChatID, config
}

// updateContextToken refreshes the context_token (and peer) on an existing
// chat-session binding so the next outbound reply carries the freshest token.
// Called by the sessionBinder after AppendUserMessage. A failure is logged but
// non-fatal: the outbound path degrades to the token from the prior message
// (which may still associate) and ultimately to a "please resend" fallback.
type bindingConfigUpdater interface {
	UpdateChannelChatSessionBindingConfig(ctx context.Context, arg db.UpdateChannelChatSessionBindingConfigParams) error
}

func updateContextToken(ctx context.Context, q bindingConfigUpdater, sessionID pgtype.UUID, msg channel.InboundMessage) error {
	_, config := wechatSessionRouting(msg)
	if err := q.UpdateChannelChatSessionBindingConfig(ctx, db.UpdateChannelChatSessionBindingConfigParams{
		ChatSessionID: sessionID,
		Config:        config,
	}); err != nil {
		return fmt.Errorf("wechat: refresh context_token on binding: %w", err)
	}
	return nil
}

// extractBindingConfig decodes a stored binding config blob. A decode miss
// yields a zero-value config (the outbound path then treats the token as
// missing and falls back).
func extractBindingConfig(raw []byte) wechatBindingConfig {
	var cfg wechatBindingConfig
	_ = json.Unmarshal(raw, &cfg)
	return cfg
}
