# Corpus Transfer P0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a browser-independent, bounded-memory corpus package and transfer path with canonical manifests, Zip64-capable archives, server readback SHA-256 verification, and durable sink ACKs.

**Architecture:** A shared Go package stages and hashes source files, emits a canonical manifest, builds or inspects ZIP archives, and safely extracts verified packages. A new workspace-scoped API records transfer intent before streaming a raw fixed-length body into existing storage backends, verifies stored bytes outside a database transaction, atomically confirms the ledger row, and separately records an idempotent sink ACK. Cobra commands expose `pack`, `send`, `status`, and `receive` so an existing 1.55 GB ZIP can be delivered without browser automation.

**Tech Stack:** Go 1.26, Cobra, Chi, PostgreSQL/sqlc, `archive/zip`, `crypto/sha256`, existing local/S3 storage interfaces.

---

### Task 1: Canonical manifest, staging, and ZIP package core

**Files:**
- Create: `server/internal/corpustransfer/manifest.go`
- Create: `server/internal/corpustransfer/package.go`
- Create: `server/internal/corpustransfer/extract.go`
- Test: `server/internal/corpustransfer/manifest_test.go`
- Test: `server/internal/corpustransfer/package_test.go`
- Test: `server/internal/corpustransfer/extract_test.go`

- [ ] **Step 1: Write failing manifest tests**

Cover sorted canonical JSON, content SHA-256, mtime cutoff, byte-exact duplicate provenance via `replica_of`, rejection of symlinks and traversal, and no semantic filtering. Exercise this API:

```go
type SourceRoot struct { Type, Name, Path string }
type Entry struct {
    Path, SourceType, SHA256 string
    SizeBytes int64
    Mtime time.Time
    ReplicaOf string `json:"replica_of,omitempty"`
}
func StageAndBuildManifest(ctx context.Context, roots []SourceRoot, cutoff time.Time, stagingDir string) (Manifest, error)
func (m Manifest) CanonicalJSON() ([]byte, error)
```

- [ ] **Step 2: Run the manifest tests and observe the missing-package failure**

Run: `cd server && go test ./internal/corpustransfer -run 'Test(Stage|Canonical)' -count=1`

Expected: FAIL because `internal/corpustransfer` does not exist.

- [ ] **Step 3: Implement the minimal manifest/staging core**

Walk each explicit source root without following symlinks. Copy accepted regular files to a mode-0700 staging tree while hashing, compare pre/post file metadata to reject changing inputs, normalize archive paths with `path.Clean`, sort entries, and set `replica_of` to the first path carrying the same SHA-256 without deleting either entry.

- [ ] **Step 4: Run the manifest tests green**

Run: `cd server && go test ./internal/corpustransfer -run 'Test(Stage|Canonical)' -count=1`

Expected: PASS.

- [ ] **Step 5: Write failing ZIP and extraction tests**

Cover archive creation with an embedded `manifest.json`, sidecar manifest inspection for an existing ZIP, archive SHA/size computation after close, safe extraction, per-entry hash validation, duplicate/path traversal/symlink rejection, expansion limits, and macOS Unicode/case-fold collisions. Exercise:

```go
func BuildZIP(stagingDir, destination string, manifest Manifest) (ArchiveEnvelope, error)
func InspectZIP(archivePath string, source SourceInfo) (Manifest, ArchiveEnvelope, error)
func ExtractVerified(archivePath, destination string, manifest Manifest) error
```

- [ ] **Step 6: Run ZIP tests red, implement, then run green**

Run before and after: `cd server && go test ./internal/corpustransfer -count=1`

Expected before: FAIL on undefined functions. Expected after: PASS. Use streaming readers/writers only; never `io.ReadAll` archive contents.

- [ ] **Step 7: Commit the package core**

```bash
git add server/internal/corpustransfer
git commit -m "feat(corpus): add canonical package core"
```

### Task 2: Durable transfer and ACK schema

**Files:**
- Create: `server/migrations/265_corpus_transfer.up.sql`
- Create: `server/migrations/265_corpus_transfer.down.sql`
- Create: `server/migrations/266_corpus_transfer_primary_index.up.sql`
- Create: `server/migrations/266_corpus_transfer_primary_index.down.sql`
- Create: `server/migrations/267_corpus_transfer_primary_key.up.sql`
- Create: `server/migrations/267_corpus_transfer_primary_key.down.sql`
- Create: `server/migrations/268_corpus_transfer_idempotency_index.up.sql`
- Create: `server/migrations/268_corpus_transfer_idempotency_index.down.sql`
- Create: `server/migrations/269_corpus_transfer_ack.up.sql`
- Create: `server/migrations/269_corpus_transfer_ack.down.sql`
- Create: `server/migrations/270_corpus_transfer_ack_primary_index.up.sql`
- Create: `server/migrations/270_corpus_transfer_ack_primary_index.down.sql`
- Create: `server/migrations/271_corpus_transfer_ack_primary_key.up.sql`
- Create: `server/migrations/271_corpus_transfer_ack_primary_key.down.sql`
- Create: `server/pkg/db/queries/corpus_transfer.sql`
- Generated: `server/pkg/db/generated/corpus_transfer.sql.go`
- Generated: `server/pkg/db/generated/models.go`

- [ ] **Step 1: Add migration contract tests before schema changes**

Extend the repository migration test surface, if present, with assertions that the new tables have no foreign keys, all state values are checked, and every new index is created concurrently in a single-statement migration.

- [ ] **Step 2: Run migration checks red**

Run: `cd server && go test ./migrations/... -count=1` when the package exists; otherwise run `make lint-migrations` if exposed by the Makefile and record the expected missing migration failure.

- [ ] **Step 3: Add the transfer and ACK tables**

`corpus_transfer` stores server IDs/actor/workspace/object key, canonical manifest JSON/hash, expected and verified archive size/SHA, state, expiry/verification lease, bounded failure code, and lifecycle timestamps. `corpus_transfer_ack` stores the `(workspace_id, transfer_id, sink_id)` identity, confirmed SHA, acknowledging actor, and timestamp. Do not add foreign keys. Build unique indexes with `CREATE UNIQUE INDEX CONCURRENTLY` in their own files, then attach them as primary keys in the following migrations.

- [ ] **Step 4: Add compare-and-set sqlc queries**

Define queries for create-or-read-by-idempotency, workspace-scoped get, `created -> uploading`, `uploading -> uploaded`, `uploaded -> verifying`, verified confirmation/failure, confirmed content lookup, idempotent ACK insert/read, and `confirmed -> acked`. Every query includes `workspace_id`; no query authorizes by transfer ID alone.

- [ ] **Step 5: Generate sqlc and verify schema**

Run: `make sqlc && cd server && go test ./pkg/db/... -count=1`

Expected: generation succeeds and DB package tests pass.

- [ ] **Step 6: Commit schema and queries**

```bash
git add server/migrations server/pkg/db/queries/corpus_transfer.sql server/pkg/db/generated
git commit -m "feat(corpus): add transfer and ack ledger"
```

### Task 3: Workspace-scoped streaming API

**Files:**
- Create: `server/internal/handler/corpus_transfer.go`
- Test: `server/internal/handler/corpus_transfer_test.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write failing handler tests**

Test create validation/idempotency, 2 GiB maximum size, exact `Content-Length`, chunked/truncated/duplicate PUT rejection, bounded streaming, server readback SHA verification, failure on altered storage bytes, idempotent completion, confirmed-only download, workspace isolation, ACK replay, and conflicting ACK digest. Use a fake implementing:

```go
type transferStorage interface {
    UploadStream(context.Context, string, io.Reader, int64, string, string) (string, error)
    GetReader(context.Context, string) (io.ReadCloser, error)
    DeleteObject(context.Context, string) error
}
```

- [ ] **Step 2: Run handler tests red**

Run: `cd server && go test ./internal/handler -run CorpusTransfer -count=1`

Expected: FAIL because the handlers/routes do not exist.

- [ ] **Step 3: Implement create and upload**

Add member-only routes under `/api/workspaces/{id}/corpus-transfers`. Create validates canonical manifest and envelope; derives actor/workspace server-side; mints `workspaces/{workspaceID}/corpus-transfers/{transferID}/archive.zip`; and records intent first. Upload claims one immutable attempt, requires a positive exact length no greater than 2 GiB, wraps the request in `io.LimitReader(size+1)`, passes it to `UploadStream`, and records `uploaded` only when exactly `size` bytes were consumed.

- [ ] **Step 4: Implement completion, status/download, and ACK**

Completion claims verification, hashes/counts `GetReader` bytes outside a transaction, then compare-and-sets confirmation or a bounded failure code. Download serves confirmed/acked objects only. ACK validates the confirmed digest and stable sink ID and performs immutable insert plus state advancement in one DB transaction; an identical replay returns the same receipt.

- [ ] **Step 5: Run focused API tests green**

Run: `cd server && go test ./internal/handler -run CorpusTransfer -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the API**

```bash
git add server/internal/handler/corpus_transfer.go server/internal/handler/corpus_transfer_test.go server/cmd/server/router.go
git commit -m "feat(corpus): stream and verify transfer objects"
```

### Task 4: Streaming CLI client primitives

**Files:**
- Modify: `server/internal/cli/client.go`
- Test: `server/internal/cli/client_test.go`

- [ ] **Step 1: Write failing client tests**

Use `httptest` and a reader that fails if eagerly buffered. Pin this API:

```go
func (c *Client) PutStream(ctx context.Context, path string, body io.Reader, size int64, out any) error
func (c *Client) DownloadStream(ctx context.Context, path string, dst io.Writer) error
```

Assert auth/workspace headers are retained, `Content-Length` is exact, redirects do not leak authorization to another host, and downloads do not buffer.

- [ ] **Step 2: Run client tests red**

Run: `cd server && go test ./internal/cli -run 'TestClient(PutStream|DownloadStream)' -count=1`

Expected: FAIL on undefined methods.

- [ ] **Step 3: Implement minimal streaming methods and run green**

Use `http.NewRequestWithContext`, assign `req.ContentLength`, reuse the client's authenticated request setup/error decoder, and `io.Copy` successful downloads into the caller's writer.

Run: `cd server && go test ./internal/cli -run 'TestClient(PutStream|DownloadStream)' -count=1`

Expected: PASS.

- [ ] **Step 4: Commit client primitives**

```bash
git add server/internal/cli/client.go server/internal/cli/client_test.go
git commit -m "feat(cli): add bounded streaming HTTP methods"
```

### Task 5: Corpus CLI pack/send/status/receive flow

**Files:**
- Create: `server/cmd/multica/cmd_corpus.go`
- Test: `server/cmd/multica/cmd_corpus_test.go`
- Modify: `server/cmd/multica/main.go`
- Modify: `scripts/agent-cli-command-names.txt`

- [ ] **Step 1: Write failing command tests**

Test `pack` with repeated `--source type=name:path`, a cutoff, and deterministic manifest sidecar; `send` using either a sidecar or on-the-fly inspection of an existing ZIP; `status`; and `receive` streaming to a mode-0600 temporary file, verifying archive and entries, atomically installing a package directory, then ACKing. Include the copy-ready existing-export shape:

```powershell
multica corpus send "C:\Users\EDY\Desktop\codex-export-皮皮-30d.zip" --source-type codex-export
```

- [ ] **Step 2: Run command tests red**

Run: `cd server && go test ./cmd/multica -run Corpus -count=1`

Expected: FAIL because `corpus` is not registered.

- [ ] **Step 3: Implement `pack` and `send`**

`pack` creates private staging/temp paths, invokes the shared package core, writes `<archive>.manifest.json`, and prints archive/manifest SHA, byte size, entry count, and missing roots as JSON. `send` opens the archive file (never `os.ReadFile`), creates the intent, raw-streams it, completes it, and prints the server receipt with `last_success_at`, `late`, `missing=false`, and `retry_reason` fields.

- [ ] **Step 4: Implement `status` and `receive`**

`status` prints the durable state/failure reason. `receive` downloads confirmed bytes to a private temporary file, verifies the server receipt and manifest, extracts into a private sibling staging directory, renames it to `<output>/<package_id>`, and ACKs only after successful install. Failed extraction never modifies the destination or sends ACK.

- [ ] **Step 5: Run command tests green and verify help**

Run:

```bash
cd server && go test ./cmd/multica -run Corpus -count=1
go run ./cmd/multica corpus --help
```

Expected: tests pass and help lists `pack`, `send`, `status`, and `receive`.

- [ ] **Step 6: Commit CLI flow**

```bash
git add server/cmd/multica/cmd_corpus.go server/cmd/multica/cmd_corpus_test.go server/cmd/multica/main.go scripts/agent-cli-command-names.txt
git commit -m "feat(corpus): add package transfer CLI"
```

### Task 6: Operator documentation and local end-to-end proof

**Files:**
- Create: `apps/docs/content/docs/corpus-transfer.mdx`
- Create: `apps/docs/content/docs/corpus-transfer.zh.mdx`
- Modify: documentation navigation files only if required by the docs app
- Test: existing docs build/typecheck

- [ ] **Step 1: Write the user-facing contract**

Document privacy/retention boundaries, exact macOS pack/send/receive commands, the one-line Windows existing-ZIP send command, the 2 GiB/no-resume P0 limit, ACK semantics, `missing != zero collaboration`, and why the P0 uses authenticated streaming rather than Tailscale raw pull, Feishu files, browser automation, attachment upload, or presigned URLs.

- [ ] **Step 2: Run a local end-to-end integration test**

Start the repository's test server/local storage through the existing test harness, create a fixture containing duplicate and non-ASCII paths, run pack -> send -> complete -> receive -> ACK, then compare manifest/archive hashes and inspect the final ledger state. Add the flow as an automated Go integration test when the existing handler harness supports it.

- [ ] **Step 3: Run focused and broad verification**

```bash
gofmt -w server/internal/corpustransfer server/internal/handler/corpus_transfer*.go server/cmd/multica/cmd_corpus*.go
cd server && go test ./internal/corpustransfer ./internal/cli ./cmd/multica ./internal/handler -count=1
cd .. && make test
git diff --check
```

Expected: all commands exit 0 with no test failures or whitespace errors.

- [ ] **Step 4: Inspect acceptance coverage and commit docs/test proof**

Confirm the issue requirements map to code/tests: shared scanner, manifest, ZIP/SHA, server readback verification, atomic confirmation, ACK ledger, macOS demonstration, and browser-independent existing Windows ZIP command. State any environment-only acceptance evidence honestly.

```bash
git add apps/docs server
git commit -m "docs(corpus): document transfer operations"
```

### Task 7: Branch completion and delivery

**Files:**
- Review all changed files and commits

- [ ] **Step 1: Run fresh final verification**

Run the full commands from Task 6 again in the final tree and inspect their exit codes and full summaries.

- [ ] **Step 2: Review the diff and repository state**

Run:

```bash
git status --short --branch
git diff origin/main...HEAD --stat
git diff origin/main...HEAD --check
git log --oneline origin/main..HEAD
```

Expected: only WS-3061 files are changed, no temporary workflow/reference files are tracked, and all commits are coherent.

- [ ] **Step 3: Push and open a linked PR**

Push the dedicated branch and create a PR whose title includes `WS-3061` and whose body includes `Closes WS-3061`, verification evidence, security/retention decisions, the existing Windows ZIP command, and declared P0 non-goals.

- [ ] **Step 4: Complete Multica delivery**

Create the required Allen closing follow-up child with assignee ID `b285f936-4b6a-43fe-83f8-b7154ebc693a`, parent WS-3061, and a request for an HTML report plus Feishu notification to the issue creator. Then post exactly one concise WS-3061 result comment with the PR URL, tests, path choice/failure mode, and Pipi command, and move WS-3061 to `in_review`.
