// Package weixin contains the protocol foundation for personal WeChat direct
// messages over Tencent's iLink bot API.
//
// Personal WeChat is deliberately a distinct channel type from WeCom
// (Enterprise WeChat). The two products use unrelated authentication,
// transport, identity, and reply-context protocols.
//
// This package currently owns only the protocol client, inbound normalization,
// and cursor-safe receiver. Installation persistence, QR-session HTTP handlers,
// channel-engine registration, and the EventChatDone outbound subscriber are
// added in later slices so the security- and reliability-sensitive wire layer
// can be reviewed independently first.
package weixin

import "github.com/multica-ai/multica/server/internal/integrations/channel"

// TypeWeixin is the durable channel discriminator for personal WeChat iLink.
// Do not reuse "wecom": that value already identifies Enterprise WeChat.
const TypeWeixin channel.Type = "weixin"
