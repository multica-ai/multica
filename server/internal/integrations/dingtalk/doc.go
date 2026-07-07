// Package dingtalk contains the Multica ↔ 钉钉 (DingTalk) Bot integration.
//
// It is the third chat channel after Lark and Slack and is built
// entirely on the generic channel_* tables plus the channel-agnostic
// engine (internal/integrations/channel/engine) — no DingTalk-specific
// tables. The package covers:
//
//  1. Registration (registration.go / registration_service.go /
//     registration_verifier.go) — the scan-to-create device flow
//     ("一键创建钉钉应用", oapi.dingtalk.com /app/registration/*): a QR
//     scan mints a per-agent org-internal app in the scanning user's
//     own org. Credentials are exchanged for an app access token
//     BEFORE the installation row commits, so a half-created app
//     surfaces as a clean install error instead of a dead row.
//  2. InstallationService (installation.go) + ChannelStore
//     (channel_store.go) — channel_installation rows with the
//     client_secret sealed via internal/util/secretbox; the only
//     write path to a dingtalk installation goes through here.
//  3. Transport (stream_connector.go / dingtalk_channel.go) — one
//     Stream Mode WebSocket per active installation, supervised by
//     the shared engine.Supervisor (lease, reconnect, backoff). Frames
//     are ACKed before dispatch; the engine's two-phase dedup absorbs
//     redeliveries. No SDK dependency — the frame protocol is decoded
//     locally.
//  4. Inbound normalization (inbound.go / fresh_command.go) — the bot
//     callback becomes a channel.InboundMessage; /new on the first
//     non-empty line strips the directive and forces a fresh agent
//     session for the dispatch.
//  5. ResolverSet (resolvers.go) — the per-platform seams the engine
//     Router runs the shared pipeline through: installation routing by
//     client_id, identity, dedup, session bind/append (shared
//     engine.ChatSession), drop audit, /unbind, replier, typing.
//  6. AutoBinder (auto_binding.go) — an unbound org member is resolved
//     through the corp directory with the installation's own
//     credentials (senderStaffId → topapi/v2/user/get → unionid /
//     profile email) and bound automatically; the explicit
//     "click to bind" prompt is only the fallback.
//  7. BindingTokenService (binding.go) — the fallback bind flow:
//     15-minute single-use tokens, hash at rest, transactional redeem
//     that rejects cross-user rebinds in-DB.
//  8. RobotMessenger (robot_messenger.go) + shared API plumbing
//     (api.go) — the per-installation server API client: robot group /
//     oTo sends for agent replies, the processing-emotion reply/recall
//     pair, and the legacy-oapi directory lookup. App access tokens
//     are cached per client_id.
//  9. OutboundReplier (replier.go) — verdict-driven notices posted
//     through the inbound message's session webhook (no API permission
//     needed): binding prompt, offline / archived / busy notices,
//     /unbind confirmation, /issue confirmation.
//  10. TypingIndicatorManager (typing_indicator.go) — the 🤔思考中 text
//     emotion on ingested messages, recalled on chat-done /
//     task-failed / no-task settle. DingTalk's equivalent of the Lark
//     typing reaction; the emotion asset is the publicly shipped one
//     the official DingTalk OpenClaw connector uses, so no per-org
//     registration is needed.
//  11. Outbound (outbound.go) — the bus subscriber: on chat:done it
//     clears the emotion and delivers the agent's reply (group chats
//     by openConversationId, DMs by the staff id captured on the
//     session binding); on task:failed it clears the emotion and sends
//     a failure notice, because DingTalk has no run card to carry the
//     failure state (interactive cards require a per-app template the
//     scan-to-create flow cannot assume).
//
// Architectural boundaries (mirroring the Lark package):
//
//  1. Issue creation goes through internal/service.IssueService via the
//     shared engine — this package never creates issues directly.
//  2. Inbound ingestion uses the shared engine.ChatSession, not the
//     HTTP chat handlers.
//  3. Unbound users and non-workspace members never reach
//     chat_session/chat_message; they land in the shared drop audit
//     (no message body) with a drop_reason.
//  4. client_secret is encrypted at rest via secretbox; the DB never
//     sees plaintext, and handlers never see the ciphertext.
package dingtalk
