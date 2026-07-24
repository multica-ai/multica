// Package wechat is the WeChat ClawBot (iLink) integration for the
// channel-agnostic engine. The WeChat ClawBot is a 2026 official WeChat
// capability ("微信 ClawBot 插件" / iLink Bot protocol) that lets a bot
// receive and reply to messages on a personal WeChat account after the account
// owner scans a login QR code.
//
// Unlike Feishu (an enterprise-wide app install serving all members) or Slack
// (BYO app tokens), a WeChat installation is bound to ONE personal WeChat
// account that the installer scans at install time. The account's bot_token +
// baseurl (returned by the QR-login status poll) are the per-installation
// credentials. Multiple distinct WeChat users can still message that bot and
// are distinguished by their per-installation user id ("xxx@im.wechat"), so the
// shared channel_user_binding + chat_session machinery applies unchanged.
//
// Transport: WeChat iLink has NO inbound webhook/callback. Messages are pulled
// via long-polling (POST /ilink/bot/getupdates, server holds ~35s). That makes
// deployment EASIER than Feishu/Slack (no public ingress) and maps cleanly onto
// channel.Channel.Connect's "block on the receive loop" contract.
//
// The iLink protocol quirk that most shapes this adapter: an outbound
// /ilink/bot/sendmessage MUST echo back the context_token carried by the
// inbound message it replies to, or the reply is not associated with the
// conversation. We persist that token in channel_chat_session_binding.config at
// inbound time and read it back at outbound time (context_token.go).
//
// The adapter is structured to mirror slack/ (the cleanest second-channel
// reference) for resolvers/binding/outbound, and lark/ for the QR-scan
// device-flow install UX. The channel engine, the generalized channel_* tables,
// and the shared sqlc queries are reused WITHOUT modification — "adding WeChat"
// is this adapter plus a one-line CHECK constraint migration, exactly as Slack
// proved.
package wechat
