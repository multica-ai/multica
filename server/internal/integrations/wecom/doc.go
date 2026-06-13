// Package wecom contains the Multica ↔ 企业微信 intelligent robot integration.
//
// MVP uses the long-connection API mode (wss://openws.work.weixin.qq.com):
//   - InstallationService (encrypted bot + corp secrets)
//   - BindingTokenService (member identity binding)
//   - UserIDResolver (open_userid → plaintext userid via self-built app)
//   - ChatSessionService (channel-aware chat_session)
//   - Dispatcher (inbound pipeline)
//   - WSConnector (JSON aibot_* protocol)
//   - Hub (WS lease + per-installation supervisors)
//   - Patcher (subscribes to EventChatDone / EventTaskFailed; completes
//     the outbound stream with the agent reply)
//
// Architectural boundaries (mirror Lark integration):
//   1. Issue creation goes through internal/service.IssueService.Create.
//   2. Inbound ingestion uses ChatSessionService, not SendChatMessage handler.
//   3. Unbound users never reach chat_session; they land in wecom_inbound_audit.
//   4. Secrets are encrypted at rest via internal/util/secretbox.
package wecom
