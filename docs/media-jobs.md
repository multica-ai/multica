# Media jobs

The shared scheduler, transfer path and CLI use native adapters on the host
daemon. `daemon.Config.MediaExecutor(provider, profileID)` can supply an embedded
adapter; otherwise normal daemon configuration enables native discovery.
Installing an agent CLI alone does not establish image support.

## Native Codex images

The daemon probes the resolved built-in Codex executable during registration
and periodic CLI inspection. It requires a file-backed ChatGPT login, the
provider's `imageGeneration` capability, and an enabled `image_generation`
feature. API-key authentication, unavailable features, and custom runtime
profiles do not advertise `media-image-codex-v1`. Capability changes refresh
registration even when the CLI version has not changed. Failed registrations
retain the previous acknowledgement and are retried on the next inspection.

Each job starts one fresh Codex App Server turn in its private temporary
directory. Its private `CODEX_HOME` links only the host's authentication file;
the CLI owns authentication and refresh. Native images, session state and
generated configuration stay in that directory and are removed after the
transfer. No host credentials enter a job request, receipt, or upload.
File-backed authentication and local symbolic-link support are required.

The adapter accepts only the current thread's completed `imageGeneration`
receipt and its `savedPath`. Assistant text is not an artifact manifest.
Duplicate item receipts are ignored; multiple distinct images fail closed.
The daemon enforces its existing artifact/symlink boundary before uploading.
With no completed image, a structured image `usageLimitExceeded` failure or a
terminal turn/error `usageLimitExceeded` or `rateLimitExceeded` signal permits
quota failover. Retrying error notifications and ambiguous process/transport
failures do not replay the model. Cancellation terminates the owned subprocess
tree before temporary files are removed. The job disables shell, plugins, apps,
local image-reading tools and subagents; editing uses the supplied `localImage`
inputs.

The native image tool selects its image model. It does not expose a model
selector through the App Server protocol used here, so `--model` is rejected
instead of being passed as a reasoning-model identifier. `--size` and `--aspect`
are supplied as image instructions; they are not exact pixel guarantees.
This integration does not certify a particular GPT Image model version.
The protocol was verified with `codex-cli 0.160.1`.

An explicitly authorized host smoke test generates an image and edits it:

```sh
cd server
MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./pkg/agent \
  -run '^TestCodexNativeImageLive$' -count=1 -v
```

Ordinary tests use test-owned CLI processes and synthetic credentials. They
cover native receipts, login/capability gating, cancellation, explicit quota
failures, registration refresh/retry, and three HTTP upload attempts with one
native generation. The live test verifies the native CLI; the cross-container
acceptance additionally requires deploying the daemon and running the commands
below from a separate caller without provider credentials.

## Native Antigravity images

The built-in `antigravity` runtime advertises `media-image-agy-v1` only when its
host has a readable `.gemini/antigravity-cli/antigravity-oauth-token` under
`HOME`, containing a nonempty `token.access_token` and either absent or
`consumer` `auth_method`, and `agy models` exits successfully with a nonempty
catalog within 15 seconds. The file may be a symlink to a regular file. Custom
profiles do not inherit this capability. Login or catalog changes refresh the
runtime registration even when the CLI version stays the same.

Each assignment starts one `agy --output-format stream-json` invocation with
`HOME=<job-directory>/agy-home`. Only the host authentication file is linked
into its private `.gemini/antigravity-cli` directory; credentials are not copied
into requests, inputs, receipts or uploads. The normal child environment filter
also removes inherited Labrastro task/account variables. Sessions, transcripts,
logs and images remain under the job directory and are removed after transfer.
`ANTIGRAVITY_APP_DATA_DIR` is insufficient for this isolation in `agy 1.4.12`.
Local symbolic-link support is required. The CLI owns token refresh.

The prompt invokes the built-in `image-generator` subagent and asks it to accept
the first successful image without refinement. This is a normal print-mode
conversation, not `--agent image-generator`. References are supplied through
native `generate_image` `ImagePaths`. The adapter associates the stream's
conversation and image-generator child with their native `transcript.jsonl`
records. It accepts only the `GENERIC` result immediately following a
`generate_image` call, using its image media URI or native saved-path line.
An assistant response or a `view_file` image is not a generation receipt.
If the child retries internally, the last successful native receipt is used.
The artifact must resolve to a nonempty regular file inside the **parent**
conversation's `brain/<conversation-id>/` directory. Foreign conversations,
ambiguous producers and escaping paths fail closed.

A valid image takes precedence over a trailing quota error, so it is uploaded
without another generation. Without an image, explicit quota/rate-limit errors
in the terminal result, native generation transcript or native error log map
to `media.RateLimitError` and the existing cooldown/reassignment path. Other
failures do not replay generation. Cancellation kills the owned process tree.
Like ordinary native Antigravity tasks, execution uses
`--dangerously-skip-permissions`; private `HOME` isolates session state, not OS
access. It does not provide the Codex adapter's protocol-level tool allowlist.

`image-generate` and `image-edit` use Antigravity's native image model;
`--model` and `--duration` are rejected. `--size` and `--aspect` are prompt
instructions, not exact pixel guarantees. Ordinary tests replay the captured
`agy 1.4.12` generate/edit contracts and use test-owned subprocesses with
synthetic credentials. The DB-backed `TestMediaAntigravityCredentialSeparatedCLI`
runs an actual caller CLI in an empty home against an `httptest` Server and a
separate host Executor. It covers `agy`/`auto`, generation/editing, idempotent
upload retry, quota cooldown/reassignment and image attachment receipts without
provider credentials on the caller. This verifies the cross-container protocol
boundary without deploying a production daemon.

An explicitly authorized host smoke test exercises the installed native CLI:

```sh
cd server
MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./pkg/agent \
  -run '^TestAntigravityNativeImageLive$' -count=1 -v
```

Set `MULTICA_MEDIA_SMOKE_OUTPUT_DIR` to an empty directory to retain the
generated and edited images for inspection. Existing files are not overwritten.

## Request and receipt

```sh
multica media generate --capability image-generate --provider auto \
  --prompt 'A red lighthouse at dusk' --output json
multica media generate --capability image-edit --provider codex \
  --prompt-file ./prompt.txt --input subject=./photo.png --out ./result.png
multica media generate --capability video-generate --provider flow \
  --prompt 'Clouds over a mountain' --duration 8 --aspect 16:9 --async
multica media job get <job-id> --output json
multica media job wait <job-id> --out ./result.mp4
multica media job cancel <job-id>
```

`--input` and `--input-attachment` are repeatable; optional roles are `input`,
`subject`, `scene`, and `style`. `--model`, `--size`, `--aspect` and `--duration`
are passed to the adapter, which validates native combinations. `--issue`
binds a member-created job to an issue. Inside a task, the server derives and
locks the producing task, issue, chat session and actor from the task token.
A task cannot override this binding.

The default command polls until terminal. Without `--out`, **no output binary
is downloaded**: the response contains `attachment_id`, durable `markdown_url`
and ready-to-paste `markdown`. `--out` streams to a temporary sibling file and
renames after a complete download. `--async` returns the job immediately and
cannot be combined with `--out`.

## Scheduler and adapter contract

There is one `media_job` table. Migrations are `9035_labrastro_media_job`,
`9036_labrastro_media_job_identity_index`, `9037_labrastro_media_job_queue_index`
and `9038_labrastro_media_job_running_index`. No executor, attempt, upload-part,
profile-lock or cache tables are introduced. `attempt` is a single current
assignment UUID on the job, used to reject stale receipts.

States are `queued → running → completed/failed/cancelled`; a definite provider
429 returns the same job to `queued`. Each runtime has at most one running
media job. Claims lock the runtime and queued job and use the existing runtime
owner/public/shared-with rules. Workspace membership is required. Eligible
runtimes must be online, carry a matching per-runtime capability and not be
cooling. Header-level `media-*` capabilities are discarded.

| Runtime host | Tag / media source | Operations |
| --- | --- | --- |
| codex | media-image-codex-v1 | image-generate, image-edit |
| antigravity (`--provider agy`) | media-image-agy-v1 | image-generate, image-edit |
| Any verified existing runtime (e.g. antigravity) | media-video-flow-v1 / flow | video-generate |
| Same Flow host | media-image-whisk-v1 / whisk | image-remix |

Media source comes from the advertised tag, independently of the host's runtime
type. Codex/agy tags retain their native host restriction; Flow and Whisk can
share an existing antigravity runtime with a verified browser executor. The
matched `capability_tag` is stored per assignment and supplies the cooldown key,
even if registration changes its tags before the result arrives.

Rank uses the minimum remaining percentage across applicable active quota
windows. Exhausted windows are excluded; agy uses the Gemini pool, not the
Claude/GPT pool. Unknown quota ranks below measured capacity. Expired windows
are ignored. `auto` can cross providers with the same operation. A typed
`media.RateLimitError` cools that runtime/tag in `agent_runtime.metadata` until
Retry-After, the next relevant quota reset, or a one-minute fallback. Registration
preserves these server-owned cooldowns. A job permits at most 32 explicit quota
retries and has a 30-minute deadline, enforced on claims, reads and transfers.

`media.Executor.Submit` receives downloaded inputs and a private job directory;
it writes its artifact inside that directory and honors context cancellation.
Submit is called once per assignment. After generation, only HTTP upload of the
same file is retried, up to three total attempts. The upload is idempotent per
job/attempt, including a lost response after storage commit. Local files are
removed on return. Cancellation is observed within the worker's three-second
status interval and prevents later output/receipts from reviving the job.

Claims and receipts use negotiated `rpc-v1` (`media.claim`, `media.result`), with
HTTP fallback only when a WS request was not sent. Parameters, input attachment
IDs and a scoped, expiring `mjt_` token travel over RPC. Binary data never does.
Each runtime has one serial worker. Its next claim settles any prior running
assignment: an already persisted attachment completes the job; otherwise it
fails with `assignment lost`. This releases the runtime even after a lost claim
or receipt. Shutdown sends the receipt with an independent ten-second timeout.
No model execution is replayed. If the runtime never returns, the job deadline
still applies. This stage has no durable native execution recovery or resumable
upload ledger.

## HTTP and storage

- User/task auth: `POST /api/media/jobs`, `GET /api/media/jobs/{id}`,
  `POST /api/media/jobs/{id}/cancel`. Read/cancel is restricted to the caller;
  task credentials additionally require the same originating task.
- Daemon auth: `POST /api/daemon/media/claim`, `POST /api/daemon/media/result`.
  WS calls must name a runtime in the authenticated connection's runtime set.
- Job token: `GET /api/media/jobs/{id}/inputs/{attachmentID}`,
  `GET /api/media/jobs/{id}/status`, `POST /api/media/jobs/{id}/output?filename=...`.
  Tokens authorize only declared inputs and one output for the current attempt,
  not ordinary account APIs. Revoked runtime consent/ownership stops transfers.

Output is a fixed-length raw HTTP body, sniffed as image/video, up to 1 GiB.
Local and S3 use their existing `UploadStream` implementations. The ordinary
multipart `/api/upload-file` endpoint now streams from a bounded-memory multipart
spool; `?media=1` opts into the 1 GiB file limit for media inputs. Existing uploads
retain the 100 MiB limit. Deployment proxies must permit the desired request size.
External-channel task credentials retain their existing default-deny route
policy for the new media commands.

## Verification

The integration fixture uses PostgreSQL, a real WebSocket hub, a fake Executor
and Local storage. It streams a 110 MiB output, injects an upload failure and a
lost committed-upload response, checks one Submit / three uploads / one output
attachment, observes heartbeat acknowledgements during transfer, and verifies
receipt-only versus explicit streamed download behavior. Separate tests cover
quota selection, Flow/Whisk registration and routing on an existing antigravity
runtime, lost dispatch/receipt settlement, shutdown receipts, permission
revocation, input scope, private-chat isolation, artifact path/symlink boundaries,
content-type and retry limits, stale receipts, cancellation, task/issue binding,
CLI validation, migrations/rollback and S3's streaming contract.
No real provider login or generation is needed for these checks.
