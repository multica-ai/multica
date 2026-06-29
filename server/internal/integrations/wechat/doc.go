// Package wechat contains the Multica ↔ 企业微信 (WeChat Work) intelligent
// bot integration via the long-connection WebSocket protocol.
//
// Architecture mirrors the lark package:
//
//  1. DB schema + sqlc wrappers (migration 119_wechat_integration.up.sql)
//  2. InstallationService (encrypted secret, workspace-scoped lookups)
//  3. ChatSessionService (channel-aware chat_session ensure / append)
//  4. Dispatcher (inbound pipeline: installation route → dedup → group
//     filter → identity check → session → append → enqueue chat task)
//  5. AuditLogger (wechat_inbound_audit; no body column)
//  6. Hub (WS lease + per-installation supervisor goroutines with
//     exponential backoff + jitter; same lease CAS as lark)
//  7. WSConnector (WeCom long-connection protocol: JSON text frames,
//     aibot_subscribe + aibot_msg_callback + aibot_respond_msg + ping)
//  8. Outbound (subscribes to EventChatDone/EventTaskFailed; sends
//     stream replies via the WebSocket using callback req_id)
//
// Key differences from the Lark integration:
//   - WeCom uses JSON text frames, not binary protobuf
//   - Reply uses aibot_respond_msg with the callback's req_id (not a
//     new request); stream reply format with content accumulation
//   - No device-flow registration (bot_id + secret configured directly)
//   - User identity is WeCom userid (corp-scoped), not per-app open_id
//   - No outbound card patching; uses stream reply messages instead
package wechat
