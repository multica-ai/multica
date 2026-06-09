// Package lark contains the Multica ↔ 飞书 (Lark) Bot integration.
//
// MVP scope is tracked in MUL-2671. After the migration / service
// boundary PRs landed, this package now covers:
//
//  1. DB schema + sqlc wrappers (migration 109_lark_integration.up.sql)
//  2. InstallationService (encrypted app_secret, workspace-scoped lookups)
//  3. BindingTokenService (15-minute single-use, transactional redeem
//     that rejects cross-user rebinds in-DB)
//  4. ChatSessionService (channel-aware chat_session ensure / append
//     with /issue command parsing)
//  5. Dispatcher (inbound pipeline: installation route → top-level
//     message_id dedup → group filter → identity check → ensure
//     session → append → /issue → enqueue chat task; typed outcomes
//     for offline / archived; emit returns DispatchResult + error so
//     the connector can post the matching Lark-side reply card)
//  6. AuditLogger (lark_inbound_audit; deliberately no body column)
//  7. APIClient interface + http_client.go (real Lark Open Platform
//     transport for IM v1 send/patch + binding prompt + bot info;
//     stubAPIClient refuses calls when no production client is wired)
//  8. Hub (WS lease + per-installation supervisor goroutines with
//     exponential backoff + jitter; renewer cancels the connector's
//     run ctx on lease loss to keep §4.4 ownership safe across
//     replicas; EventConnector interface is the seam for the real
//     wire protocol)
//  9. WSLongConnConnector (real long-conn over gorilla/websocket; the
//     wire protocol is the binary Frame envelope from the official Go
//     SDK — bootstrap via POST /callback/ws/endpoint, app-layer
//     ping/pong, ACK responses on every data frame, ctx cancel breaks
//     blocking ReadMessage via a watchdog goroutine for §4.4)
// 10. Patcher (subscribes to task / chat-done events; keeps the
//     per-task Lark interactive card in sync; throttled patches +
//     final/error bypass)
// 11. OutcomeReplier (outbound side of the EventEmitter contract:
//     NeedsBinding mints a token + sends the binding prompt;
//     AgentOffline / AgentArchived push status notice cards into the
//     chat; Ingested is owned by the Patcher; Dropped is silent)
// 12. RegistrationService (RFC 8628 device-flow scan-to-install: opens
//     a session against accounts.feishu.cn, polls in the background,
//     and on success writes through InstallationService + auto-binds
//     the installer via InstallerBinder so §2.1 "scan to bind, you're
//     done" holds end-to-end)
//
// Architectural boundaries (frozen from Elon's 二审, MUL-2671 §4.8):
//
//  1. Issue creation goes through internal/service.IssueService.Create —
//     this package never calls qtx.CreateIssue directly.
//  2. Inbound message ingestion uses ChatSessionService here, NOT the
//     HTTP `SendChatMessage` handler. Group chat_sessions have multi-
//     member creator semantics that the HTTP handler's single-creator
//     guard rejects on purpose.
//  3. Outbound card-message mapping lives in `lark_outbound_card_message`
//     (per task/message), never on `chat_session.metadata`.
//  4. Unbound users and non-workspace members never reach
//     chat_session/chat_message. They land in `lark_inbound_audit` (no
//     body) with a drop_reason and nothing else.
//  5. `app_secret` is encrypted at rest via internal/util/secretbox.
//     The DB never sees plaintext.
package lark
