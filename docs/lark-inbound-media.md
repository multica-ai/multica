# Lark inbound context media

Lark/Feishu chat turns retain attachments from the trigger message, explicit
quotes, and direct children of merged forwards. Images, files, audio and video
reuse direct-message resource downloads, upload intents, attachment binding and
authorization. This does not add parsing, transcription or video frame extraction.

## Selection and attribution

- Automatic recent group context keeps its existing text and media placeholders.
  It never collects, downloads, stores or binds historical media. Repeated group
  mentions do not reupload that history.
- Existing topic boundaries, recent-window limits, deleted-message handling and
  forward-child limits still apply. Nested forwards are not expanded.
- Resources are collected only while explicitly selected messages are rendered.
  Forward children use the containing forward's message ID for resource reads.
- Repeated appearances of one resource and source message share one attachment;
  each selected position receives the attachment ID. Different owners stay distinct.
- Markers avoid collisions with the full rendered original text, including
  rich-text spans, sender names, filenames and source wrappers.
- Failed reads keep unavailable markers at their original positions.
- `/issue` follows the existing WeCom behavior: selected media becomes issue
  attachments and its ordinary attachment blocks are appended to the description.
  Quoted/forwarded text stays out of the description, just as for text-only quotes.
  Existing marked-block merging preserves late attachments through stale or batch
  saves and respects explicit deletion after an editor adopts them.

## Deadlines and recovery

Context metadata enrichment stays inline under the existing connector
`EnrichTimeout` (two seconds by default). Independent recent, quoted and forwarded
reads start together, share that deadline, and are joined before rendering.
Cancellation reaches every read; name lookup does not start after expiration.
Timeouts retain explicit unavailable context. This PR does not extend that budget
or introduce asynchronous metadata preparation.

Only actual downloadable resources enter the existing durable media-pending
workflow. Downloads, uploads, cancellation, upload-intent cleanup and task recovery
retain their existing limits and lifecycle; text-only context does not acquire a
media fence. Attachment bytes and ordinary CLI output limits are unchanged.

A bare `/clear` records fresh-session intent without automatic context creating a
model turn. Explicit quotes, current media or a prompt still follow the ordinary
path. `/new` skips automatic recent context while allowing explicit selections.

## Verification

With an explicit disposable, migrated `DATABASE_URL`:

```sh
cd server
go test -race -p 2 -parallel 2 \
  ./internal/integrations/lark ./internal/integrations/channel/engine
```

Regression coverage includes explicit quotes/forwards and all four media kinds;
automatic recent exclusion across repeated turns; resource ownership/deduplication;
failed/omitted resources; literal marker collisions; appended issue attachments
without quoted transcripts; preserved reporter edits; existing inline issue media
and stale/batch-save handling; concurrent reads and cancellation. Redis-backed
tests additionally require `REDIS_TEST_URL`.
