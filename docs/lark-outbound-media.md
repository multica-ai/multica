# Lark outbound images and files

The Lark integration delivers attachments bound to the assistant message in a
completed chat turn. The server reads their bytes from its configured object
store and uploads them with the conversation's bot credentials. Agent runtimes
bind their output files through the existing attachment upload API.

## Delivery

Text and supported images share one schema-2.0 message card. Text is sent first;
uploaded images update the same message at their Markdown positions. Images
without a reference are appended to the card. A reply containing only images
can use image messages. Other attachments are sent separately to the same
conversation and reply target.

Image upload uses an explicit format allowlist and a 10,000,000-byte limit.
Unsupported formats, larger images and images explicitly refused by the image
endpoint fall back to file upload, with a 30,000,000-byte limit. Empty files and
larger files are rejected before upload. MP4 and Opus uploads use their matching
video and audio message types; their existing file keys are sufficient, without
requiring a generated thumbnail or duration.

The integration requires object storage, bot capability and the application's
`im:resource:upload` or `im:resource` permission. Uploads use the resource HTTP
client and its longer timeout. Multipart bodies reuse the file buffer.

## Rendering and ownership

Goldmark identifies image references and their source spans, including reference
syntax, escapes and entities. Only attachments bound to the current assistant
message can supply an uploaded image key. Other image destinations remain
ordinary links. No remote URL in model output is fetched to render an image.

Initial cards and full replacement updates use `config.update_multi=true`.
Updates retain images whose previous update was accepted or remains uncertain.
Retries target the same message and content.

The existing durable reply ledger fences the whole reply across duplicate
completion events and replicas. Admission happens before spawning a worker or
looking up attachments. Pending batches and active transfers are bounded, and
waiting for a transfer slot respects the delivery budget. Each external write
rechecks the installation, active conversation, frozen destination and lease.

Attachment sends reuse a UUID derived from the installation, assistant message
and attachment. Lost responses and the provider's 230049 pending result remain
uncertain, with bounded retries. Definite failure and uncertainty have distinct
notices, each with a separate deadline after the transfer budget expires.
Attachments remain available in Multica. Interrupted batches are not replayed
without per-file delivery receipts.

## Verification

Run the Lark package tests and static checks from `server/`:

```sh
go test -race ./internal/integrations/lark -count=1
go vet ./internal/integrations/lark
```

Set `DATABASE_URL` to an isolated, migrated test database to include the reply
ownership and attachment-to-HTTP pipeline tests. These tests skip when it is
unset. The suite covers multipart transport, token refresh, upload/message type
pairing, reply routing, idempotency, size/format fallback, pending sends,
authorization changes and same-message Markdown updates.

With an authorized test bot, verify an image-only reply, a downloadable file,
text before and after an image in one message, and an SVG or oversized image
delivered as a file. Download the image and file to compare their bytes with the
platform attachments. Capture the rendered chat as well as the message payload;
message GET requires `card_msg_content_type=user_card_content` for the card body.

See the official [upload image][image], [upload file][file], [message content][content]
and [update card][patch] contracts.

[image]: https://open.feishu.cn/document/server-docs/im-v1/image/create
[file]: https://open.feishu.cn/document/server-docs/im-v1/file/create
[content]: https://open.feishu.cn/document/server-docs/im-v1/message-content-description/create_json
[patch]: https://open.feishu.cn/document/server-docs/im-v1/message-card/patch
