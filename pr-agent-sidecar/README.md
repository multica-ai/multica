# pr-agent-sidecar

Go service that bridges a GitHub App webhook to a Multica issue, and brokers
short-lived GitHub App installation tokens to the `pr-reviewer` skill on
demand. Fork-local: lives entirely outside upstream `multica-ai/multica`.

See `docs/pr-agent-sidecar-plan.md` (in the parent repo) for the full design.

## What it does

```
GitHub PR  →  Cloudflare Tunnel  →  pr-agent-sidecar (Mac mini)
                                       │
                                       ├─ POST /api/issues → multica.zoop.tools
                                       │     (creates Multica issue, assigns
                                       │      pr-reviewer agent)
                                       │
                                       └─ GET /installation-token?nonce=…
                                             (single-use exchange for a fresh
                                              GitHub App installation token)
```

The skill in the runtime-workspace container reaches the sidecar **internally**
at `http://pr-agent-sidecar:9000` over the shared `multica-bridge` Docker
network — no public hop for that call.

## Endpoints

- `GET /healthz` — liveness.
- `POST /webhook/github` — GitHub App webhook receiver. Validates HMAC
  signature, dedupes by `X-GitHub-Delivery`, creates a Multica issue.
- `GET /installation-token?nonce=…` — single-use nonce exchange for a 1-hour
  installation token scoped to the PR's repo.

## Bootstrap (one-time, on the Mac mini)

### 1. Register a GitHub App on the `zoopone` org

Settings → Developer settings → GitHub Apps → New GitHub App.

- **Permissions:** Pull requests (read), Contents (read), Issues (write).
- **Subscribe to events:** `pull_request`.
- **Webhook URL:** `https://pr-agent.zoop.tools/webhook/github` (the tunnel
  hostname from step 2 below).
- Generate a webhook secret; save it.
- Generate the App private key (`.pem`); save it.
- Install the App on the `multica` repo only.

### 2. Create the Cloudflare Tunnel

In the Cloudflare dashboard:

- Zero Trust → Networks → Tunnels → Create a tunnel.
- Pick a name (e.g. `pr-agent`), copy the **tunnel token** (used as
  `CLOUDFLARE_TUNNEL_TOKEN` below).
- Add a public hostname: `pr-agent.zoop.tools` → Service:
  `http://pr-agent-sidecar:9000`.

### 3. Multica bot identity

Create a user account `pr-agent-bot@zoop.one` in Multica. Invite it to the
dogfood workspace. Mint a Personal Access Token (`mul_…`). Save it.

Create a `pr-reviewer` agent in the same workspace. Copy its UUID:

```
multica agent list -o json | jq -r '.[] | select(.name=="pr-reviewer") | .id'
```

### 4. Configure secrets

Create `.env` in this directory (it is gitignored) with the values below.
Mount the App private key either inline (multi-line `GITHUB_APP_PRIVATE_KEY`
env) or as a file path (`/run/secrets/github-app.pem`, mount via
docker-compose).

```dotenv
# --- GitHub App ---
GITHUB_APP_ID=12345
GITHUB_APP_PRIVATE_KEY=/run/secrets/github-app.pem
GITHUB_WEBHOOK_SECRET=replace-me

# --- Multica API (called from sidecar) ---
MULTICA_PAT=mul_replace-me
MULTICA_BASE_URL=https://multica.zoop.tools
MULTICA_WORKSPACE_ID=00000000-0000-0000-0000-000000000000
PR_REVIEWER_AGENT_ID=00000000-0000-0000-0000-000000000000

# --- Sidecar self-identity ---
SIDECAR_PUBLIC_URL=https://pr-agent.zoop.tools
REPO_ALLOWLIST=zoopone/multica
PORT=9000

# --- Cloudflare Tunnel (used by docker-compose only) ---
CLOUDFLARE_TUNNEL_TOKEN=replace-me
```

### 5. Create the shared Docker network

```bash
docker network create multica-bridge
```

This is the network shared with `runtime-workspace-deployment/`. The runtime
compose file must also attach its services to `multica-bridge` (one-time
edit there).

### 6. Up

```bash
docker compose up -d --build
```

### 7. Smoke

```bash
curl -fsS http://localhost:9000/healthz   # → {"ok":true}
# From inside a runtime-workspace container on the same Mac mini:
curl -fsS http://pr-agent-sidecar:9000/healthz
```

Then open a test PR on `zoopone/multica` and verify a Multica issue gets
created within ~1 second.

## Local development

```bash
# Requires Go 1.26+ (brew install go)
cd cmd/sidecar
go test ./...
go run .   # reads env from your shell
```

## Layout

```
pr-agent-sidecar/
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── README.md  (this file — includes the env-vars template)
└── cmd/sidecar/
    ├── main.go            entrypoint
    ├── config.go          env loader
    ├── server.go          chi router + Server struct
    ├── webhook.go         POST /webhook/github
    ├── token.go           GET /installation-token
    ├── nonce_store.go     in-memory single-use nonce store
    ├── delivery_dedup.go  in-memory dedup of GitHub webhook retries
    ├── multica_client.go  POST /api/issues
    ├── github_app.go      ghinstallation wrappers
    └── *_test.go
```
