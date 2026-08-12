# Private recruitment inbox processor

This binary is intentionally separate from Multica's general Feishu channel.
It listens to official `im.message.receive_v1` long-connection events for one
secret-configured chat, replies as the Feishu bot, and never writes the source
message into Multica issues, chats, task transcripts, object storage, or logs.

## Runtime contract

- Run `./recruitment_inbox` as a single always-on service replica. Feishu's
  official long connection supplies event authenticity; the server-provided
  WebSocket endpoint is bootstrapped with the app credentials and every data
  frame is ACKed using the official binary protocol.
- The allowlisted chat identifier, Feishu credentials, OpenAI credential, and
  HMAC key come only from the deployment secret manager. Do not put their
  values in source, CI logs, Multica metadata, or issue comments.
- A message is durably claimed before ACK. OCR/transcription runs off the ACK
  path. Terminal transitions clear the temporary source message ID.
- The Feishu send `uuid` is a stable HMAC-derived idempotency key. Connector
  redelivery and ambiguous retries therefore cannot create a second reply.
- Image/audio bytes are held in memory only for the bounded extraction call
  and the response body is closed immediately. No temporary media file or
  object-store copy is created.
- Every reply states that the service only records/analyzes information.
  Consequential instructions ask for `确认生效`; this processor contains no
  executor for rejection, publishing, contact, scheduling, offers, forwarding,
  candidate-state changes, or budget/rule activation.

## Persistent schema

`recruitment_inbox_event` contains only:

- HMAC message key (and the acceptance-approved source message ID only while
  `processing`, required for crash recovery and cleared at terminal state)
- boolean/count structured summary; no extracted text or values
- role-version pointer
- processing state, timestamps, error code
- HMAC sent-message key and send status

It has no chat-ID, raw-message, content, resource-key, filename, transcript,
resume, contact, salary, or evaluation column.

## Secret/config key names

Required: `DATABASE_URL`, `RECRUITMENT_INBOX_FEISHU_APP_ID`,
`RECRUITMENT_INBOX_FEISHU_APP_SECRET`,
`RECRUITMENT_INBOX_ALLOWED_CHAT_ID`, `RECRUITMENT_INBOX_HASH_KEY`, and either
`RECRUITMENT_INBOX_OPENAI_API_KEY` or
`RECRUITMENT_INBOX_OPENAI_BASE_URL`.

Recommended: `RECRUITMENT_INBOX_BOT_OPEN_ID`,
`RECRUITMENT_INBOX_ROLE_VERSION`, `RECRUITMENT_INBOX_MODEL`,
`RECRUITMENT_INBOX_IMAGE_MODEL`, and `RECRUITMENT_INBOX_AUDIO_MODEL`.

The Feishu app needs the official message receive event plus least-privilege bot
scopes for reading message resources and sending messages. The bot must be a
member of the designated private group.

## Health, alerting, and rollback

- Liveness: `GET /health`
- Readiness: `GET /readyz` (requires PostgreSQL and an active Feishu socket)
- Alert on readiness failure, restart loop, and log `error_code` counts. Logs
  carry only error categories and HMAC message keys.
- Disable immediately by scaling the service to zero or revoking the Feishu app
  event subscription. Roll back the image to the prior revision; the table is
  backward-compatible and may remain. Delete its migration only after the
  service is stopped and retention requirements are confirmed.

Do not test with production candidate material until a deployed revision is
healthy and an explicitly designated synthetic test flow is approved.

For the bundled Helm chart, set `recruitmentInbox.enabled=true` after adding
the required secret keys to `existingSecret`. Disable/rollback with
`--set recruitmentInbox.enabled=false`; the Deployment is then removed while
the audit-minimal table remains available for the agreed retention window.
