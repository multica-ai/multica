# Lark inbound context media

Lark/Feishu chat turns retain attachments from the context already selected for
that turn: explicit quotes, direct children of merged forwards, and the existing
recent group window. Images, files, audio and video use the same resource
download, upload-intent ledger, attachment binding and authorization as direct
message attachments. This does not add document parsing, transcription or video
frame extraction.

## Selection and attribution

- Existing topic boundaries, recent-window limits, deleted-message handling and
  forward-child limits still apply. Nested forwards are not expanded.
- A resource is collected while its message is rendered, so omitted messages do
  not cause downloads. Forwarded resources use the containing forward's message
  ID, as required by the Lark download API.
- Repeated appearances of the same resource and source message share one
  attachment. Each source position receives an attachment ID in its marker.
- Markers are chosen to avoid collisions with the rendered original text,
  including rich-text spans, sender names, filenames and source wrappers.
- Failed downloads retain an explicit unavailable marker at their source
  position. A successful neighboring attachment cannot replace that marker.
- `/issue` keeps successfully bound historical media in an attributed,
  code-formatted source transcript. It does not present those resources as the
  reporter's own embedded uploads. Direct uploads retain their existing format,
  and concurrent human edits to the description are preserved.

## Preparation, deadlines and recovery

The production WebSocket connector prepares context without remote I/O. It emits
a fallback body and a deferred-context descriptor. The router durably appends
that body with the existing media-pending deadline before acknowledging the
event. Context reads and downloads then run behind that fence, outside the
connector's ACK budget.

Independent recent, quoted and forwarded metadata reads start together, share a
bounded request context, and are joined before rendering. Name lookup does not
start after that context has expired. The enclosing media deadline bounds both
metadata and resource work; cancellation propagates to the reads. A resolved
text-only body is also persisted before the media fence is released.

On timeout or shutdown, incomplete results cannot overwrite the durable fallback
or bind partial references. Existing upload intents retain cleanup responsibility
for unbound objects. If the process stops before finalization, the existing
deferred-task scheduler resumes the accepted task at its stored deadline with
the saved fallback; it does not repeat the lost context job or create a second
task.

A bare `/clear` only records fresh-session intent for the next turn. Automatic
recent context must not turn it into a model task. Explicitly quoted context,
direct media, or a prompt after `/clear` still follows the ordinary media path.
`/new` continues to exclude automatic recent context while accepting explicit
quotes and forwards.

## Regression coverage

Run against a disposable, migrated database:

```sh
cd server
go test -race -p 2 -parallel 2 \
  ./internal/integrations/lark ./internal/integrations/channel/engine
```

The suites cover all three context sources and four media kinds; source ownership
and deduplication; omitted and failed media; literal marker collisions in chat
and `/issue`; edited issue descriptions; slow reads after ACK; cancellation and
joining; resolved text persistence; bare control commands; and fallback recovery
without duplicate tasks. Redis-backed tests additionally require `REDIS_TEST_URL`.
