# Corpus Transfer P0 Design

## Decision

Build a dedicated, workspace-scoped corpus transfer protocol around an
authenticated raw streaming PUT. Do not raise the attachment endpoint's 100 MiB
limit: that path buffers both the CLI multipart body and the server payload and
cannot safely carry the existing 1.55 GB export.

The P0 flow is:

```text
SourceAdapter -> canonical Manifest -> ZipSink -> archive + SHA-256
    -> create intent -> authenticated streaming PUT to a temporary object
    -> server readback SHA-256 verification -> atomic confirmation
    -> DownloadSink verification/application -> durable ACK
```

This selects API streaming over two alternatives:

- S3 presigned PUT is efficient but adds a second bearer-capability auth model,
  provider-specific signing/checksum behavior, and a local-storage exception.
  It belongs with the later cross-tenant/direct-upload hardening.
- A local drop directory is useful as a test or future sink but is not a remote
  delivery path and cannot provide server-side verification or a central ACK.

## Shared collection core

`SourceAdapter` discovers files and emits stable logical entries. P0 ships a
path adapter plus macOS defaults for Codex sessions, Multica task transcripts,
and explicitly configured Automation roots. Discovery does not delete or
semantically filter source material. The manifest records provenance and uses
content hashes to make exact duplicates visible without destroying evidence.

The canonical manifest contains:

- `schema_version`, `package_id`, and `created_at`
- source adapter name/version and requested time window
- hostname/device identity supplied as informational provenance
- sorted entries with normalized relative path, source type, size, mtime, and
  lowercase SHA-256
- `entry_count` and `total_uncompressed_bytes`

The archive embeds `manifest.json` but not the final archive digest, avoiding a
self-hash cycle. The CLI writes through Go's `archive/zip` to a mode-0600
temporary file. `archive/zip` emits Zip64 structures when sizes, offsets, or
entry counts require them. The archive is hashed from disk after close so the
declared size and digest exactly match the uploaded bytes.

## Transfer protocol

Routes live under `/api/workspaces/{id}/corpus-transfers` and therefore inherit
workspace membership and task-token workspace pinning.

1. `POST /corpus-transfers` validates the manifest/envelope, creates a durable
   intent, and returns a server-generated transfer ID.
2. `PUT /corpus-transfers/{transferID}/content` requires the exact declared
   `Content-Length`, accepts only one upload attempt, and streams a bounded body
   into a server-generated temporary object key through `UploadStream`.
3. `POST /corpus-transfers/{transferID}/complete` claims verification, reads the
   immutable stored object through `GetReader`, computes SHA-256 and byte count,
   then atomically records confirmation in PostgreSQL.
4. `GET /corpus-transfers/{transferID}` returns the durable state/receipt.
5. `GET /corpus-transfers/{transferID}/content` streams confirmed bytes only.
6. `POST /corpus-transfers/{transferID}/acks` records that a stable sink applied
   the confirmed archive. Identical replays return the existing ACK; a different
   digest for the same sink conflicts.

Every query constrains both transfer and workspace IDs. Workspace, actor, and
object-key identity are server-owned; manifest values never authorize access.

## State and atomicity

The transfer state machine is:

```text
created -> uploading -> uploaded -> verifying -> confirmed -> acked
                          |            |
                          +----------> failed
created/uploading -------------------> expired
```

The server never holds a database transaction while streaming or hashing a
large object. It uses short compare-and-set transitions before and after each
storage operation. Atomic confirmation means database visibility changes in one
transaction; it does not pretend PostgreSQL and S3 share a transaction. The
temporary object becomes the confirmed backing object without an S3 rename.

An ACK is distinct from confirmation: confirmation proves the server stored the
declared bytes, while ACK proves a named sink verified and applied them. The ACK
transaction checks workspace, transfer, confirmed digest, and sink identity,
then inserts the immutable ACK and advances the transfer state.

## Limits and failure behavior

- P0 accepts archives up to 2 GiB, covering the 1,555,394,559-byte Windows
  export while remaining below S3's 5 GiB single-PUT limit.
- A missing, chunked, truncated, over-limit, duplicate, or post-upload PUT is
  rejected. Interrupted P0 uploads restart with a new transfer/key.
- SHA or size mismatch records a bounded failure code and never exposes the
  object as confirmed content.
- ZIP consumers reject absolute/traversing paths, NULs, symlinks and special
  entries, duplicates, expansion beyond the manifest, and macOS Unicode/case
  collisions before installing from a private staging directory.
- `missing`, `late`, and `retry_reason` are transfer/ACK delivery state, never a
  proxy for employee collaboration. A missing package means missing evidence.

## P0 non-goals

- Browser UI or browser automation
- Presigned/direct S3 PUT, multipart upload, resume, or offline queue
- Windows-specific source discovery and locked-file retry hardening
- Cross-tenant sharing or long-term corpus retention
- Analyzer filters, business-verb classification, or CPA body backfill

## Verification

Unit tests cover canonical manifests, duplicate hashing/provenance, safe ZIP
creation/extraction, exact-length streaming, storage readback verification,
workspace isolation, compare-and-set transitions, idempotent completion/ACK,
and failure states. A local-storage integration test exercises the full
pack-upload-complete-download-apply-ACK path without a browser or in-memory file
buffer. A sparse/synthetic large-file smoke test demonstrates bounded-memory
streaming; the existing 615,078,022-byte sample remains the external acceptance
anchor when available.
