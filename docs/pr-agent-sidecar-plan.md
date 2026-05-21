# pr-agent-sidecar — Implementation Plan

## Context

INV-496 settled the architecture for a PR-review agent. The sidecar is the Go service that sits between GitHub App webhooks and Multica's existing assignment-triggered task flow. It is the only **new** runtime component we need to build — the skill is a separate workstream; everything else is config or ops. This plan implements that service end-to-end.

Architecture recap (from INV-496):

```
GitHub PR → Cloudflare Tunnel → pr-agent-sidecar (Mac mini)
                                  ├─ POST /api/issues → multica.zoop.tools
                                  └─ GET /installation-token?nonce=…
```

The sidecar is fork-local — lives in a new top-level `pr-agent-sidecar/` directory. No upstream Multica code is edited.

## Goal

A Go service exposing two HTTP endpoints:

1. **`POST /webhook/github`** — receives GitHub App webhooks for `pull_request.opened` / `synchronize`, dedups by `X-GitHub-Delivery`, creates a Multica issue assigned to the `pr-reviewer` agent, stashes a one-time nonce mapping to the installation/PR.
2. **`GET /installation-token?nonce=…`** — single-use nonce exchange for a short-lived GitHub App installation token, scoped to the PR's repo, returned to the skill at review time.

## Project layout (new dir, all fork-local)

```
pr-agent-sidecar/
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml          sidecar + cloudflared, joins external `multica-bridge`
├── .env.example
├── README.md
└── cmd/sidecar/
    ├── main.go                 wires config, router, signal handling
    ├── config.go               env var loading + validation
    ├── server.go               chi router + middleware
    ├── webhook.go              POST /webhook/github
    ├── token.go                GET /installation-token
    ├── nonce_store.go          in-memory store with TTL sweep
    ├── delivery_dedup.go       in-memory cache keyed on X-GitHub-Delivery
    ├── multica_client.go       thin client for POST /api/issues
    ├── github_app.go           ghinstallation wrappers
    └── *_test.go               unit tests per file
```

## Dependencies

- `github.com/go-chi/chi/v5` — HTTP router (matches Multica's stack at `server/cmd/server/router.go`)
- `github.com/google/go-github/v60` — `ValidatePayload` + typed event parsing
- `github.com/bradleyfalzon/ghinstallation/v2` — App auth + installation token minting

Standard library otherwise.

## Config (env)

Loaded once at startup; missing required → log and exit.

```
GITHUB_APP_ID
GITHUB_APP_PRIVATE_KEY        PEM string (or path)
GITHUB_WEBHOOK_SECRET         signature validation
MULTICA_PAT                   mul_* token for pr-agent-bot
MULTICA_BASE_URL              https://multica.zoop.tools
MULTICA_WORKSPACE_ID          UUID of dogfood workspace
PR_REVIEWER_AGENT_ID          UUID of pr-reviewer agent
REPO_ALLOWLIST                comma-separated owner/repo
SIDECAR_PUBLIC_URL            https://pr-agent.zoop.tools (embedded in issue body for skill callback)
PORT                          default 9000
```

## Multica API client (request shape confirmed)

Confirmed against `server/internal/handler/issue.go:1045-1063` (`CreateIssueRequest`) and `server/internal/middleware/auth.go:47-91` (PAT auth):

- **Method/path:** `POST {MULTICA_BASE_URL}/api/issues`
- **Headers:** `Authorization: Bearer {MULTICA_PAT}`, `X-Workspace-ID: {MULTICA_WORKSPACE_ID}`, `Content-Type: application/json`
- **Body:** only `title` is required. We send:
  ```json
  {
    "title": "Review PR #N: <pr_title>",
    "description": "<markdown body with PR URL, head SHA, callback URL>",
    "assignee_type": "agent",
    "assignee_id": "<PR_REVIEWER_AGENT_ID>"
  }
  ```
  `assignee_type` and `assignee_id` MUST both be present (server returns 400 otherwise; see `issue.go:1536`). Literal `"agent"` is accepted (see `issue.go:1542-1574`).
- **Success:** `201 Created` with `IssueResponse` body (fields at `issue.go:27-55`). We only need `id` and `identifier` from the response.
- **Failure:** log + return 500 to GitHub (GitHub retries 3x).

## Dedup (no server-side idempotency)

Multica has no `Idempotency-Key` support — confirmed by reading the handler. GitHub retries webhooks up to 3 times on 5xx. To avoid duplicate Multica issues:

- Maintain a small in-memory LRU keyed on `X-GitHub-Delivery` (the unique GUID GitHub sends per delivery; retries share the same GUID).
- 24-hour TTL — generous, since GitHub's retry window is hours.
- Cache holds `delivery_id → multica_issue_id`. Repeat delivery returns 200 with the prior issue ID, **does not** call Multica again.
- For MVP, in-memory is fine. Reboot clears it; worst case is a small number of duplicate issues right after a Mac mini reboot.

## Nonce store

`map[string]record` + `sync.RWMutex`. Record holds `{installationID, repo, prNumber, headSHA, expiresAt}`. Background goroutine sweeps expired entries every 30s. **5-minute TTL.** Single-use — `Consume()` returns the record and deletes atomically.

## Endpoint flows

### `POST /webhook/github`

1. `github.ValidatePayload(r, []byte(webhookSecret))` → raw payload or error (401).
2. Check `X-GitHub-Delivery` against dedup cache; if hit, return 200 with cached issue ID, no further work.
3. `github.ParseWebHook(github.WebHookType(r), payload)` → typed event.
4. Skip anything not `*github.PullRequestEvent` with action `opened` or `synchronize` (return 200, no-op).
5. Reject repos outside `REPO_ALLOWLIST` (return 200, no-op — log for audit).
6. Generate 32-hex nonce. Store with 5-minute TTL.
7. Call Multica client with the request body above.
8. Cache `delivery_id → multica_issue_id` on success.
9. Return 202 with `{ok: true, multica_issue_id, multica_identifier}`.

### `GET /installation-token?nonce=<n>`

1. Look up + consume the nonce. Missing → 404. Single-use guarantee enforced here.
2. Build `ghinstallation.NewFromAppsTransport(appsTransport, installationID).Token(ctx)`.
3. Return `{token, expires_at}` (1-hour TTL from GitHub).
4. Token is **never persisted** anywhere — only returned in the response body.

## GitHub App auth (token minting)

Initialize once at startup:

```go
tr, err := ghinstallation.NewAppsTransport(
    http.DefaultTransport, appID, []byte(pemKey))
```

Per-request, per-installation:

```go
itr := ghinstallation.NewFromAppsTransport(tr, installationID)
token, exp, err := itr.Token(ctx) // ghinstallation handles caching + refresh
```

## Dockerfile (multi-stage, distroless)

```
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/sidecar ./cmd/sidecar

FROM gcr.io/distroless/static
COPY --from=build /out/sidecar /sidecar
EXPOSE 9000
ENTRYPOINT ["/sidecar"]
```

## docker-compose.yml (sidecar's own compose file)

```yaml
services:
  pr-agent-sidecar:
    build: .
    container_name: pr-agent-sidecar
    restart: unless-stopped
    env_file: .env
    networks: [multica-bridge]

  cloudflared:
    image: cloudflare/cloudflared:latest
    container_name: pr-agent-cloudflared
    restart: unless-stopped
    command: tunnel --no-autoupdate run
    environment:
      - TUNNEL_TOKEN=${CLOUDFLARE_TUNNEL_TOKEN}
    depends_on: [pr-agent-sidecar]
    networks: [multica-bridge]

networks:
  multica-bridge:
    external: true
```

Mac mini one-time prereq: `docker network create multica-bridge` (and add the same `multica-bridge` external network to `runtime-workspace-deployment/docker-compose.yml` so skills can resolve `pr-agent-sidecar:9000` — that's a follow-up task, not part of building this service).

## Implementation order

1. `go mod init github.com/zoopone/pr-agent-sidecar`; install deps; hello-world chi server on `:9000` with a `GET /healthz` handler.
2. `config.go` — env loader + tests (positive + missing-required).
3. `nonce_store.go` — store + TTL sweep + concurrent-access tests.
4. `delivery_dedup.go` — LRU + 24-hour TTL + tests.
5. `multica_client.go` — `POST /api/issues` against staging Multica using a hand-crafted call (smoke test outside the webhook handler first).
6. `webhook.go` — signature validation + event parsing (logging-only at first); then wire through to nonce + dedup + Multica client.
7. `github_app.go` + `token.go` — ghinstallation wiring + nonce-driven token mint.
8. `Dockerfile` — build the multi-stage image; verify the binary runs.
9. `docker-compose.yml` — sidecar + cloudflared services + external network.
10. `README.md` — bootstrap walkthrough (GitHub App registration, Cloudflare Tunnel setup, env vars, network create).
11. End-to-end smoke: send a real signed test webhook (e.g. `gh webhook forward` or a fake-but-signed payload); verify a Multica issue is created; verify `/installation-token` returns a working token that can post a PR comment.

## Critical files to read before coding (no edits)

| Path | Why |
|---|---|
| `server/internal/handler/issue.go:1045-1063` | `CreateIssueRequest` struct — exact field names |
| `server/internal/handler/issue.go:1287` | success status (201) |
| `server/internal/handler/issue.go:1536-1574` | assignee validation rules |
| `server/internal/middleware/auth.go:47-91` | PAT validation flow |
| `server/internal/middleware/workspace.go:66-84` | `X-Workspace-ID` header resolution |
| `server/cmd/server/router.go:249` | confirms auth middleware applies to `/api/issues` |

## Reusable patterns

- chi router setup pattern lives at `server/cmd/server/router.go` — mirror its structure (router, middleware chain, route registration in `server.go`).
- Multica's existing skill outbound-HTTP pattern is in `multica-skills/Outline/Tools/Outline.sh` — reference for how the skill will eventually call this sidecar.

## Verification

- **Unit (Go test):** nonce store TTL + concurrency; dedup cache hit + miss + expiry; signature validation pass + fail; Multica client request body shape (against a recorded response).
- **Integration:** signed webhook → real `POST /api/issues` against staging Multica → assert 201 + correct issue title + correct agent assignee. Run with `STAGING_*` env vars.
- **E2E:** open a test PR on `multica` repo → webhook reaches sidecar via tunnel → Multica issue created → skill (separate workstream) fetches token via `/installation-token` → posts review comment. Gated by GitHub App registration, bot user creation, and the skill being ready.

## Out of scope (do NOT add here)

- The `pr-reviewer` skill itself (in `multica-skills/`)
- GitHub App registration (ops in Cloudflare/GitHub dashboards)
- Cloudflare Tunnel creation (ops)
- DNS for `pr-agent.zoop.tools` (ops)
- `gh` CLI install in runtime-workspace Dockerfile (separate workstream)
- Editing `runtime-workspace-deployment/docker-compose.yml` to attach to `multica-bridge` (separate workstream)
