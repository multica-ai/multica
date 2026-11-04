# Self-Hosting Guide

Deploy Multica on your own infrastructure in minutes.

## Architecture

| Component | Description | Technology |
|-----------|-------------|------------|
| **Backend** | REST API + WebSocket server | Go (single binary) |
| **Frontend** | Web application | Next.js 16 |
| **Database** | Primary data store | PostgreSQL 17 (`pgcrypto` + `pg_trgm`) |

Each user who runs AI agents locally also installs the **`multica` CLI** and runs the **agent daemon** on their own machine.

## Anonymous Deployment Telemetry

The self-hosted API server sends one first-party, deployment-level anonymous snapshot per UTC day to the fixed Multica endpoint `https://telemetry.multica.ai/v1/telemetry/events`. This helps maintainers understand which release versions are in use, deployment-size ranges, aggregate run volume, and how many members actively use a deployment. The endpoint is compiled into the server and cannot be redirected through configuration.

The snapshot contains only the release version; bucketed counts of workspaces, distinct human members, active agents, and daemons seen in the previous 24 hours; aggregate counts of runs started, completed, failed, and cancelled in that window; and bucketed counts of members who opened the signed-in web or desktop app, and of members who personally started an agent run (assign, mention, comment, chat, manual run, or rerun — not autopilot or agent-to-agent delegation), in the previous 1, 7, and 28 complete UTC days. Members are counted per deployment and are never identified. It does not include names, email addresses or domains, IP addresses, business IDs, host/device details, repository or organization details, model/plugin data, prompts or output, comments or chats, paths, token/cost data, credentials, errors, stacks, or logs. This telemetry is not used for billing, licensing, authentication, or security decisions.

Telemetry is enabled by default. To stop both collection and network delivery, set `DO_NOT_TRACK=1` or `DO_NOT_TRACK=true` on the API server and recreate/restart the backend. The deployment identity remains in PostgreSQL, so enabling it again does not create a new identity. `ANALYTICS_DISABLED` controls the separate PostHog integration and does not disable this first-party telemetry.

When cloning a production database for staging or testing, clear the clone's `instance_telemetry_state` table before starting its API server (or set `DO_NOT_TRACK=true`); otherwise both deployments share one identity and their same-day snapshots can silently deduplicate each other.

## Quick Install (Recommended)

Two commands to set up everything — server, CLI, and configuration.

<details open>
<summary><b>macOS / Linux</b></summary>

<br/>

```bash
# 1. Install CLI + provision the self-host server
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server

# 2. Configure CLI, authenticate, and start the daemon
multica setup self-host
```
</details>
<details>
<summary><b>Windows (PowerShell)</b></summary>

<br/>

```powershell
# 1. Install CLI + provision the self-host server
$env:MULTICA_MODE="with-server"; irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex

# 2. Configure CLI, authenticate, and start the daemon
multica setup self-host
```
</details>

This installs the `multica` CLI, checks out the latest self-host assets, pulls the official Multica images from GHCR, and configures everything for localhost.

Open http://localhost:3000. To log in, configure `RESEND_API_KEY` in `.env` for email-based codes (recommended), or leave Resend unset and copy the generated code from the backend logs. See [Step 2 — Log In](#step-2--log-in) for details.

> **Prerequisites:** Docker and Docker Compose must be installed. The script checks for this and provides install links if missing.
>
> **CLI only?** If the self-host server is already running and you only need the CLI on a macOS/Linux machine, install it with Homebrew:
>
> ```bash
> brew install multica-ai/tap/multica
> ```

---

## Step-by-Step Setup (Alternative)

If you prefer to run each step manually:

### Step 1 — Start the Server

**Prerequisites:** Docker and Docker Compose.

```bash
git clone https://github.com/multica-ai/multica.git
cd multica
make selfhost
```

`make selfhost` automatically creates `.env` from the example, generates a random `JWT_SECRET`, and starts all services via Docker Compose.

By default it pulls the latest stable release images from GHCR. To build the backend/web from your current checkout instead, run `make selfhost-build`.
If the selected GHCR tag has not been published yet, `make selfhost` now tells you to fall back to `make selfhost-build`.
`make selfhost-build` uses local `multica-backend:dev` / `multica-web:dev` tags, so it does not overwrite the pulled `:latest` images.

Once ready:

- **Frontend:** http://localhost:3000
- **Backend API:** http://localhost:8080

> **Note:** If you prefer to run the Docker Compose steps manually, see [Manual Docker Compose Setup](#manual-docker-compose-setup) below.

### Step 2 — Log In

Open http://localhost:3000 in your browser. The Docker self-host stack defaults to `APP_ENV=production` (set in `docker-compose.selfhost.yml`), and there is no fixed verification code by default. Pick one of the following to log in:

- **Recommended (production):** configure `RESEND_API_KEY` in `.env`, then restart the backend. Real verification codes will be sent to the email address you enter. See [Advanced Configuration → Email](SELF_HOSTING_ADVANCED.md#email-required-for-authentication).
- **Without email configured:** the verification code is generated server-side and printed to the backend container logs (look for `[DEV] Verification code for ...:`). Useful for one-off testing on a single machine.
- **Deterministic local/private testing:** set `APP_ENV=development` and `MULTICA_DEV_VERIFICATION_CODE=888888` in `.env`, then restart the backend. This fixed code is ignored when `APP_ENV=production`.

Changes to `ALLOW_SIGNUP`, `DISABLE_WORKSPACE_CREATION`, `WORKSPACE_CREATION_ALLOWED_EMAILS`, and `GOOGLE_CLIENT_ID` also take effect after restarting the backend / compose stack. The web UI reads `ALLOW_SIGNUP`, `DISABLE_WORKSPACE_CREATION`, and `GOOGLE_CLIENT_ID` from `/api/config` at runtime, so no web rebuild is needed. `WORKSPACE_CREATION_ALLOWED_EMAILS` is read only by the backend and is not exposed through `/api/config`. See [Advanced Configuration → Signup Controls](SELF_HOSTING_ADVANCED.md#signup-controls-optional) for the recommended sequence to lock down workspace creation, or to limit it to selected users.

> **Warning:** do **not** set `MULTICA_DEV_VERIFICATION_CODE` on a publicly reachable instance — anyone who knows an email address can then log in with that fixed code.

### Step 3 — Install CLI & Start Daemon

The daemon runs on your local machine (not inside Docker). It detects installed AI agent CLIs, registers them with the server, and executes tasks when agents are assigned work.

Each team member who wants to run AI agents locally needs to:

### a) Install the CLI and an AI agent

```bash
brew install multica-ai/tap/multica
```

You also need at least one AI agent CLI installed:
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (`claude` on PATH)
- [Antigravity CLI](https://antigravity.google/docs/cli-install) (`agy` on PATH)
- [CodeBuddy Code](https://www.codebuddy.ai/docs/cli/quickstart) (`codebuddy` on PATH)
- [DevEco Code](https://gitcode.com/openharmony-sig/deveco-code) (`deveco` on PATH)
- [Codex](https://github.com/openai/codex) (`codex` on PATH)
- [GitHub Copilot CLI](https://docs.github.com/en/copilot) (`copilot` on PATH)
- [OpenClaw](https://github.com/openclaw/openclaw) (`openclaw` on PATH)
- [OpenCode](https://github.com/anomalyco/opencode) (`opencode` on PATH)
- [Huawei Cloud CodeArts](https://support.huaweicloud.com/qs-codeartssnap/codeartsagent_qs_0004.html) (`codearts` on PATH)
- [Hermes](https://github.com/NousResearch/hermes) (`hermes` on PATH)
- [Pi](https://pi.dev/) (`pi` on PATH)
- [Cursor Agent](https://cursor.com/) (`cursor-agent` on PATH)
- Kimi (`kimi` on PATH)
- [Reasonix](https://github.com/esengine/DeepSeek-Reasonix) (`reasonix` on PATH; run `reasonix setup` first)
- Dim (`dim` on PATH)
- Kiro CLI (`kiro-cli` on PATH)
- Qoder CLI (`qodercli` on PATH)
- Qoder CN CLI (`qoderclicn` on PATH)
- Trae CLI (`traecli` on PATH)
- [Grok Build CLI](https://docs.x.ai/) (`grok` on PATH)
- Qwen Code (`qwen` on PATH)
- [QwenPaw](https://github.com/agentscope-ai/QwenPaw) (`qwenpaw` on PATH; pick its model in QwenPaw's own configuration)
- [MiniMax Code CLI](https://www.npmjs.com/package/@minimax-ai/code) (`mcode` 0.1.2+ on PATH). Install a supported Node.js release (`>=22.19 <23` or `>=24 <27`), run `npm install --global @minimax-ai/code@latest`, then `mcode login`.
- [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) (`dsh` on PATH with the Multica runtime profile installed; set `DEEPSEEK_API_KEY`)

### b) One-command setup

```bash
multica setup self-host
```

This automatically:
1. Configures the CLI to connect to `localhost` (ports 8080/3000)
2. Opens your browser for authentication
3. Discovers your workspaces
4. Starts the daemon in the background

For on-premise deployments with custom domains:

```bash
multica setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

To verify the daemon is running:

```bash
multica daemon status
```

> **Alternative:** If you prefer manual steps, see [Manual CLI Configuration](#manual-cli-configuration) below.

### Step 4 — Verify & Start Using

1. Open your workspace in the web app at http://localhost:3000
2. Navigate to **Settings → Runtimes** — you should see your machine listed
3. Go to **Settings → Agents** and create a new agent
4. Create an issue and assign it to your agent — it will pick up the task automatically

---

## Kubernetes Deployment (Alternative)

If you already run a Kubernetes cluster, you can deploy Multica there instead of Docker Compose using the released OCI Helm chart at `oci://ghcr.io/multica-ai/charts/multica` or the source chart at [`deploy/helm/multica/`](deploy/helm/multica/). It targets a typical k3s / k8s setup with an Ingress controller and a default `ReadWriteOnce` StorageClass — authored against k3s + Traefik + `local-path`, and should work on any cluster with minor tweaks.

The chart creates the following resources in the target namespace:

- `multica-postgres` — `pgvector/pgvector:pg17` backed by a 10Gi PVC
- `multica-backend` — Go API/WS server. Backed by a 5Gi `ReadWriteOnce` uploads PVC by default; set `backend.uploads.persistence.enabled=false` when you have configured S3 (`backend.config.s3Bucket`) and don't want the chart to declare the PVC at all.
- `multica-frontend` — Next.js standalone server
- Two `Ingress` resources: one for the web host, one for the backend host
- `multica-config` ConfigMap (rendered from `values.yaml`)

The `multica-secrets` Secret is **not** managed by the chart — you create it once with `kubectl` so real values never need to land in git.

> **Runtime frontend upstreams:** current `multica-web` images read `REMOTE_API_URL` and `DOCS_URL` when the Next.js server runs, so API/docs upstream changes do not require a web rebuild. The chart defaults `REMOTE_API_URL` to this release's backend Service. `frontend.compatibility.backendAlias` exists only for legacy images that still baked `REMOTE_API_URL=http://backend:8080` at build time.

> **Prerequisites:** `kubectl` and `helm` (v3.13+ for `--take-ownership`, or v4+) configured for the target cluster, an Ingress controller (Traefik / NGINX), and a default StorageClass.

### Step 1 — Point hostnames at the cluster

The chart defaults to `multica.dev.lan` (web) and `api.multica.dev.lan` (backend). Pick one of:

- **`/etc/hosts`** on every machine that needs access (developer laptops + the machine running the daemon):

  ```text
  192.168.1.206  multica.dev.lan api.multica.dev.lan
  ```

  Replace `192.168.1.206` with any node IP where your Ingress controller's Service is reachable.

- **Local DNS** (Pi-hole, Unbound, etc.): add A records for both hostnames pointing at the cluster Ingress IP.

To use different hostnames, override the matching values at install time (see [Step 4](#step-4--install-the-chart)) — `ingress.frontend.host`, `ingress.backend.host`, plus `backend.config.appUrl`, `backend.config.frontendOrigin`, `backend.config.localUploadBaseUrl`, and `backend.config.googleRedirectUri`.

### Step 2 — Create the namespace

```bash
kubectl create namespace multica
```

### Step 3 — Create the `multica-secrets` Secret

The chart references this Secret by name. Create it once with random values:

```bash
kubectl -n multica create secret generic multica-secrets \
  --from-literal=JWT_SECRET="$(openssl rand -hex 32)" \
  --from-literal=POSTGRES_PASSWORD="$(openssl rand -hex 16)" \
  --from-literal=RESEND_API_KEY="" \
  --from-literal=GOOGLE_CLIENT_SECRET="" \
  --from-literal=CLOUDFRONT_PRIVATE_KEY="" \
  --from-literal=MULTICA_DEV_VERIFICATION_CODE=""
```

Leave optional values empty for now — you can fill them in later (see [Step 5 — Log In](#step-5--log-in)).

### Step 4 — Install the chart

```bash
helm install multica oci://ghcr.io/multica-ai/charts/multica \
  --version <chart-version> \
  -n multica
```

Released chart versions strip the leading `v` from the Git tag. For example, release tag `v0.3.5` publishes chart version `0.3.5`; the chart defaults the backend and frontend image tags to `v0.3.5`.

To override defaults, export the chart values, edit them, and pass them with `-f`:

```bash
helm show values oci://ghcr.io/multica-ai/charts/multica \
  --version <chart-version> > my-values.yaml
# edit my-values.yaml — e.g. change ingress hosts, image tags, resource limits
helm install multica oci://ghcr.io/multica-ai/charts/multica \
  --version <chart-version> \
  -n multica \
  -f my-values.yaml
```

When developing from a checkout, use the local chart path instead:

```bash
helm install multica deploy/helm/multica -n multica
```

If the backend has to reach services whose certificates come from an internal CA, such as a self-hosted Gitea, set `backend.extraCACerts.configMap`. See [Advanced Configuration → Private CA Certificates](SELF_HOSTING_ADVANCED.md#private-ca-certificates-optional).

Watch the pods come up:

```bash
kubectl -n multica get pods -w
```

On a cold cluster the backend can sit `Running` but not `Ready` for a few minutes while it waits on PostgreSQL and runs migrations — a startupProbe absorbs this, so the pod should not restart. Once the backend reports `Ready`, migrations have completed and `/healthz` returns OK:

```bash
curl -H "Host: api.multica.dev.lan" http://<ingress-ip>/healthz
# {"status":"ok","checks":{"db":"ok","migrations":"ok"}}
```

Then open http://multica.dev.lan in your browser.

### Step 5 — Log In

The chart defaults to `APP_ENV=production` (set in `values.yaml` under `backend.config.appEnv`), and there is no fixed verification code by default. Pick one of the following to log in — the same three options as the Docker setup:

- **Recommended (production):** patch the Secret with a real Resend key, then restart the backend:

  ```bash
  kubectl -n multica patch secret multica-secrets --type=merge \
    -p '{"stringData":{"RESEND_API_KEY":"re_xxx"}}'
  kubectl -n multica rollout restart deploy/multica-backend
  ```

  Real verification codes will be sent to the email address you enter. See [Advanced Configuration → Email](SELF_HOSTING_ADVANCED.md#email-required-for-authentication).

- **Without email configured:** the verification code is generated server-side and printed to the backend pod logs (look for `[DEV] Verification code for ...:`). Useful for one-off testing.

  ```bash
  kubectl -n multica logs -f deploy/multica-backend | grep "Verification code"
  ```

- **Deterministic local/private testing:** set `backend.config.appEnv: development` in your values file and `MULTICA_DEV_VERIFICATION_CODE=888888` in the Secret, then `helm upgrade` and restart. This fixed code is ignored when `APP_ENV=production`.

  ```bash
  helm upgrade multica oci://ghcr.io/multica-ai/charts/multica \
    --version <chart-version> \
    -n multica \
    -f my-values.yaml --set backend.config.appEnv=development
  kubectl -n multica patch secret multica-secrets --type=merge \
    -p '{"stringData":{"MULTICA_DEV_VERIFICATION_CODE":"888888"}}'
  kubectl -n multica rollout restart deploy/multica-backend
  ```

`ALLOW_SIGNUP`, `DISABLE_WORKSPACE_CREATION`, `WORKSPACE_CREATION_ALLOWED_EMAILS`, and `GOOGLE_CLIENT_ID` likewise live under `backend.config.*` in `values.yaml` (as `allowSignup`, `disableWorkspaceCreation`, `workspaceCreationAllowedEmails`, and `googleClientId`). After `helm upgrade`, the backend pod will roll automatically because the ConfigMap hash changes; the web UI reads `allowSignup`, `disableWorkspaceCreation`, and `googleClientId` from `/api/config` at runtime, so no web rebuild is needed. `workspaceCreationAllowedEmails` is read only by the backend and is not exposed through `/api/config`.

> **Warning:** do **not** set `MULTICA_DEV_VERIFICATION_CODE` on a publicly reachable instance — anyone who knows an email address can then log in with that fixed code.

### Step 6 — Install CLI & Start Daemon

The daemon runs on your local machine, not in the cluster. Install the CLI and an AI agent as in [Step 3](#step-3--install-cli--start-daemon) above, then point the CLI at your Ingress hostnames:

```bash
multica setup self-host \
  --server-url http://api.multica.dev.lan \
  --app-url http://multica.dev.lan
```

Make sure the machine running the daemon has the same `/etc/hosts` (or DNS) entries from [Step 1](#step-1--point-hostnames-at-the-cluster).

### Updating

To pull the latest images without changing the chart version when your values still use the mutable `latest` image tag:

```bash
kubectl -n multica rollout restart deploy/multica-backend deploy/multica-frontend
```

To upgrade to a specific Multica release, upgrade to the matching chart version. The released chart defaults its app images to the matching Git tag:

```bash
helm upgrade multica oci://ghcr.io/multica-ai/charts/multica \
  --version <chart-version> \
  -n multica \
  -f my-values.yaml
```

If you need to override the app images independently from the chart version, set the image tags in your values file:

```yaml
images:
  backend:
    tag: v0.2.4
  frontend:
    tag: v0.2.4
```

Then run the same upgrade command with `-f my-values.yaml`:

```bash
helm upgrade multica oci://ghcr.io/multica-ai/charts/multica \
  --version <chart-version> \
  -n multica \
  -f my-values.yaml
```

To roll back if an upgrade goes sideways:

```bash
helm -n multica rollback multica
```

> **Upgrading from `v0.3.4` to `v0.3.5+` fails with `refusing to drop legacy daily rollups: ...`?** As of MUL-2957 the `migrate up` command runs an idempotent monthly-slice backfill automatically before applying migration `103`, so a clean upgrade is a single `helm upgrade` + backend rollout. If you are still on a pre-MUL-2957 binary or the auto-hook fails, run the standalone backfill against the same database the chart is using (`kubectl -n multica exec deploy/multica-backend -- ./backfill_task_usage_hourly --sleep-between-slices=2s`), then restart the backend deployment to re-apply migrations. See [Advanced Configuration → Usage Dashboard Rollup](SELF_HOSTING_ADVANCED.md#usage-dashboard-rollup) for the full recovery flow.

### Tearing down

```bash
# Remove the workloads but keep the PVCs and the Secret
helm -n multica uninstall multica

# Wipe everything, including PostgreSQL data and uploads
kubectl delete namespace multica
```

---

## Usage Dashboard Rollup

The Usage / Runtime dashboards read from a derived `task_usage_hourly` table populated by `rollup_task_usage_hourly()`. As of MUL-2957 the backend runs this rollup **in-process** on every replica via a DB-backed scheduler (`sys_cron_executions`); a fresh self-host install needs no operator action and the bundled `pgvector/pgvector:pg17` image works without changes — you do **not** need to swap it for an image that ships `pg_cron`, register an external cron job, set up a systemd timer, or run a Kubernetes `CronJob`.

Multiple backend replicas are safe: each replica ticks every 30 seconds and tries to claim the current 5-minute UTC plan, but the unique key `(job_name, scope_kind, scope_id, plan_time)` means only one wins each plan. Inspect steady-state operation:

> **WeCom (企业微信) smart bot and replica count.** Unlike Slack and Lark, whose outbound is stateless HTTP that any replica can perform, the WeCom smart bot's only outbound path is an in-process WebSocket long connection, held by the replica that owns that bot's lease. What happens to a reply produced on a *different* replica depends on the realtime relay:
>
> - **Sharded or dual relay mode (`REDIS_URL` set, the default with Redis):** the reply or inbox push is forwarded to the lease holder over the relay and delivered. Multi-replica WeCom is supported in this mode.
> - **Legacy relay mode, or no Redis:** the reply is dropped and the WeCom user sees nothing. Run the WeCom-enabled backend as a single replica in this configuration.
>
> In **every** mode there is one residual window: a reply produced while *no* replica holds a live connection to that bot — all of them mid-reconnect — is not delivered. It is **counted**: the replica that routed it checks afterwards whether any replica ever claimed the delivery, and increments `multica_wecom_outbound_dropped_total{reason="no_live_connection"}` when none did, so the window can be measured on a deployment rather than guessed at. If a rare lost reply during reconnects is unacceptable, a single replica remains the most conservative deployment. Everything else (including the rollup scheduler above) is multi-replica safe.

```sql
SELECT plan_time, status, attempt, runner_id,
       error_code, error_msg, started_at, finished_at
  FROM sys_cron_executions
 WHERE job_name = 'rollup_task_usage_hourly'
 ORDER BY plan_time DESC
 LIMIT 20;
```

Full reference (audit table semantics, advisory lock 4246, the standalone backfill command, flag descriptions, the `v0.3.4 → v0.3.5+` migration auto-hook) lives in [Advanced Configuration → Usage Dashboard Rollup](SELF_HOSTING_ADVANCED.md#usage-dashboard-rollup).

> **Upgrading from `v0.3.4` to `v0.3.5+`?** As of MUL-2957 the `migrate up` command runs an idempotent monthly-slice backfill automatically right before applying migration `103`, so the upgrade completes in a single invocation — no operator step required. If you are still on a pre-MUL-2957 binary or the auto-hook fails for an environmental reason, run `backfill_task_usage_hourly` against the same database and re-run the upgrade. See [Advanced Configuration → Usage Dashboard Rollup](SELF_HOSTING_ADVANCED.md#usage-dashboard-rollup) for the recovery flow.

### Compatibility paths (existing deployments only)

External schedulers — **`pg_cron` registered on the database, an external cron job, a systemd timer, or a Kubernetes `CronJob`** — that call `SELECT rollup_task_usage_hourly()` directly were the only option before MUL-2957 and remain a supported compatibility path. They are no longer the recommended setup; new deployments should rely on the in-process scheduler instead. The SQL function holds advisory lock 4246 internally, so the in-process scheduler and any pre-existing external schedule can coexist without ever double-writing the rollup.

If you already have a `pg_cron` job in production, the safe sequence to retire it is:

1. Confirm the in-process scheduler is healthy on at least one backend replica — recent SUCCESS rows should be landing in `sys_cron_executions` for `rollup_task_usage_hourly`:

   ```sql
   SELECT plan_time, status, runner_id, finished_at
     FROM sys_cron_executions
    WHERE job_name = 'rollup_task_usage_hourly'
      AND status = 'SUCCESS'
    ORDER BY plan_time DESC
    LIMIT 5;
   ```

2. Once SUCCESS rows are arriving on schedule, unschedule the redundant `pg_cron` entry:

   ```sql
   SELECT cron.unschedule('rollup_task_usage_hourly')
     FROM cron.job WHERE jobname = 'rollup_task_usage_hourly';
   ```

3. Leave the `pg_cron` extension itself installed unless you are sure no other workload depends on it. The bundled `pgvector/pgvector:pg17` image does **not** ship `pg_cron`, so nothing in Multica's default install needs it; uninstalling `pg_cron` from a custom image that other workloads still use is a separate decision.

External cron / systemd timer / Kubernetes `CronJob` setups that call `SELECT rollup_task_usage_hourly()` directly can be retired the same way — once `sys_cron_executions` shows steady SUCCESS rows from the in-process scheduler, the external job is redundant and can be removed.

## Company Knowledge Base (Optional)

Multica can run a pinned [WeKnora](https://github.com/Tencent/WeKnora) sidecar that stores and retrieves company documents. Agents read it through a read-only MCP server; people publish into it. It is opt-in, and a workspace that never enables it behaves exactly as before. If the sidecar is down, runs continue without it and the Knowledge page shows "offline".

The stack adds five containers behind the `knowledge` compose profile: `knowledge-app`, `knowledge-ui`, `knowledge-docreader`, `knowledge-postgres` (ParadeDB) and `knowledge-redis`, with their own volumes. Only `knowledge-app` joins Multica's network. Together they idle at roughly 650 MB of RAM, plus an embedding model.

### 1. Choose an embedding model

The embedding model is fixed per knowledge base when it is created, and changing it later means re-ingesting everything, so decide first. The default is `bge-m3` (multilingual) served by a local [Ollama](https://ollama.com):

```bash
ollama pull bge-m3
```

Ollama must be reachable from containers without being exposed to the network. On Linux, bind it to the Docker bridge address only (for example `OLLAMA_HOST=172.17.0.1:11434`), not `0.0.0.0`. The compose file maps `host.docker.internal` to the Docker host and allow-lists it. To use an OpenAI-compatible endpoint instead, set `KNOWLEDGE_EMBEDDING_PROVIDER=generic`, `KNOWLEDGE_EMBEDDING_URL`, `KNOWLEDGE_EMBEDDING_MODEL`, `KNOWLEDGE_EMBEDDING_DIMENSION` and `KNOWLEDGE_EMBEDDING_API_KEY`.

> WeKnora refuses model URLs that resolve to a private address (its SSRF guard) unless the host is listed in `KNOWLEDGE_SSRF_WHITELIST_EXTRA`. Setting that variable **replaces** WeKnora's own default list, so list every internal host it must reach.

### 2. Configure and start

In `.env`:

```bash
KNOWLEDGE_SYSTEM_AES_KEY=$(openssl rand -hex 16)   # required, keep it stable
KNOWLEDGE_JWT_SECRET=$(openssl rand -hex 32)
MULTICA_KNOWLEDGE_SECRET_KEY=$(openssl rand -base64 32)
KNOWLEDGE_URL=http://knowledge-app:8080
KNOWLEDGE_SERVICE_EMAIL=multica-service@example.com
KNOWLEDGE_SERVICE_PASSWORD=<8-32 characters, letters and digits>
```

Then start the profile and register the service user once. Multica signs in as this user to create one WeKnora workspace per Multica workspace:

```bash
docker compose -f docker-compose.selfhost.yml --profile knowledge up -d

curl -X POST http://localhost:8090/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"multica-service","email":"multica-service@example.com","password":"<the password above>"}'

docker compose -f docker-compose.selfhost.yml up -d backend   # pick up the new .env values
```

Optionally set `KNOWLEDGE_DISABLE_REGISTRATION=true` afterwards to close WeKnora self-registration. In Multica, a workspace owner or admin then enables knowledge for the workspace; Multica creates the workspace, five default knowledge bases and a read-only `company-read` MCP server that every agent receives.

### Models, Wiki and summaries (optional)

Summaries, Wiki pages and reranking need a language model. Point Multica at an OpenAI-compatible endpoint and it registers the models in WeKnora when knowledge is enabled:

| Variable | Purpose |
|---|---|
| `KNOWLEDGE_LLM_BASE_URL` | The endpoint, for example `https://llm.example.com/v1`. Unset means no model is registered. |
| `KNOWLEDGE_LLM_API_KEY` | The endpoint's key. Stored by WeKnora, never shown back. |
| `KNOWLEDGE_LLM_MODEL` | The chat model. Summaries and Wiki need it. |
| `KNOWLEDGE_RERANK_MODEL` | The reranking model, optional. |
| `KNOWLEDGE_VISION_MODEL` | The vision model, optional. It is registered but not tested at setup. |

When a chat model exists, Wiki is turned on for `company`, `ventures` and `decisions` at WeKnora's `focused` setting, and every base gets summaries. Owners and admins change either per base on the Knowledge page, under Setup. Wiki is limited to three bases. Multica does not meter the model calls WeKnora makes; the cost is bounded by the focused setting, the three-base limit and the summaries switch, so turn summaries off on a base that is high-volume. Setup > Models runs each configured model once and names any that failed and why.

If the endpoint is on a private address, add its host to `KNOWLEDGE_SSRF_WHITELIST_EXTRA` as described above. A model that fails to register does not stop knowledge from becoming ready; the failure is shown by the models test.

`KNOWLEDGE_UPLOAD_MAX_MB` caps one uploaded file. The default is 50, WeKnora's own default; set it no higher than WeKnora's `MAX_FILE_SIZE_MB`.

The repository docs sync (Knowledge, Sources, Repository docs) clones a repository's `docs` folder over https with the server's `git`, which the image includes. It is off until a workspace owner turns it on, and the server must be able to reach the repository's https address. Private repositories are not supported yet: the address is stored as typed and read back to owners, so it must not carry a credential.

Each owner who enables knowledge also gets a WeKnora login, which is an admin of the workspace's WeKnora tenant, so they can use WeKnora's own interface. WeKnora creates those logins only for a system administrator, so the service user (`KNOWLEDGE_SERVICE_EMAIL`) must be one; bootstrap it with `WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL`. The owner's password is shown once from Setup and cannot be shown again.

When a workspace is deleted, its WeKnora tenant is kept for seven days and then removed. The removal is retried if WeKnora is down. To keep a tenant, delete its `knowledge_tenant_deprovision` row while it is still held, by a direct database command run by an operator.

### 3. Make it reachable by agents

Agents run on your daemons, usually outside Docker, and talk to WeKnora's MCP endpoint (`<KNOWLEDGE_PUBLIC_URL>/mcp/<id>`) with a bearer token. Set `KNOWLEDGE_PUBLIC_URL` to an address those machines can open and put a TLS reverse proxy in front of the loopback port (`KNOWLEDGE_API_PORT`, default `8090`). If you skip it, the MCP URL falls back to `KNOWLEDGE_URL`, which works only for daemons on the compose network. Set `KNOWLEDGE_UI_URL` to the proxied WeKnora UI (`KNOWLEDGE_UI_PORT`, default `8085`) for the "Open in WeKnora" link.

### Ports

WeKnora normally listens on 80 and 8080; 8080 is Multica's. The profile publishes the WeKnora API on `127.0.0.1:${KNOWLEDGE_API_PORT:-8090}` and its UI on `127.0.0.1:${KNOWLEDGE_UI_PORT:-8085}`.

### Backups

Back up these named volumes together, plus `KNOWLEDGE_SYSTEM_AES_KEY` and `MULTICA_KNOWLEDGE_SECRET_KEY`:

| Volume | Holds |
| --- | --- |
| `knowledge-postgres-data` | Documents, chunks, embeddings, workspaces and API keys |
| `knowledge-data-files` | Uploaded original files |

Losing `KNOWLEDGE_SYSTEM_AES_KEY` makes WeKnora's encrypted credentials unrecoverable; losing `MULTICA_KNOWLEDGE_SECRET_KEY` means each workspace's stored key can no longer be read, and knowledge must be re-provisioned. `knowledge-docreader-tmp` is scratch space and does not need a backup. `knowledge-redis-data` holds Redis stream state, not documents, so it is optional in a backup. For a consistent copy, stop the profile (`docker compose -f docker-compose.selfhost.yml --profile knowledge stop`) before copying the volumes, or use `pg_dump` against `knowledge-postgres`.

### Upgrading WeKnora

The three WeKnora images are pinned to `v0.9.0` through `KNOWLEDGE_VERSION`; never use `latest`. Multica's adapter is tested against recorded v0.9.0 responses, so a version bump is a deliberate change that re-records them.

See [Advanced Configuration](SELF_HOSTING_ADVANCED.md#company-knowledge-base) for every variable.

## Stopping Services

If you installed via the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --stop
```

If you cloned the repo manually:

```bash
# Stop the Docker Compose services (backend, frontend, database)
make selfhost-stop

# Stop the local daemon
multica daemon stop
```

## Switching to Multica Cloud

If you've been self-hosting and want to switch your CLI to [Multica Cloud](https://multica.ai):

```bash
multica setup
```

This reconfigures the CLI for multica.ai, re-authenticates, and restarts the daemon. You will be prompted before overwriting the existing configuration.

> Your local Docker services are unaffected. Stop them separately if you no longer need them.

## Upgrading

```bash
docker compose -f docker-compose.selfhost.yml pull
docker compose -f docker-compose.selfhost.yml up -d
```

Pin `MULTICA_IMAGE_TAG` in `.env` to an exact version like `v0.2.4` if you want to stay on a specific release. Migrations run automatically on backend startup.
If the selected GHCR tag has not been published yet, fall back to `make selfhost-build` or `docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build`.

> **Upgrading from `v0.3.4` to `v0.3.5+` fails with `refusing to drop legacy daily rollups: ...`?** That's migration `103`'s fail-closed guard: it requires `task_usage_hourly` to be seeded before the legacy daily rollups are dropped. As of MUL-2957 `migrate up` runs that backfill automatically right before applying `103`, so the upgrade completes in a single invocation. If you are still on a pre-MUL-2957 binary or the auto-hook fails, run `backfill_task_usage_hourly` manually first, then re-run the upgrade. Full instructions in [Advanced Configuration → Usage Dashboard Rollup](SELF_HOSTING_ADVANCED.md#usage-dashboard-rollup).

---

## Upgrade notes: digital company office

This release adds floors and squad seats to the office, proposals, the Secretary's daily briefs, and an optional knowledge base. Migrations 600 to 650 run automatically on backend startup.

What changes for operators:

- **`agent.office_room` is removed.** Migration 608 drops the column. Its per-agent room labels had no link to a squad, so nothing is carried over. Agents are drawn at the desks of their home squad.
- **Existing squads start unseated.** No migration places them on a floor. An owner or admin sees a **Seat all** banner in the office, and nothing moves until they press it.
- **The knowledge base is an optional sidecar.** WeKnora runs only when you start the `knowledge` compose profile and set `KNOWLEDGE_URL`. Without it, nothing changes. See [Company Knowledge Base (Optional)](#company-knowledge-base-optional) for the steps, the variables, and the one service account you register by hand.
- **Secretary setup has no button yet.** Setup runs through the API. It needs an online Hermes runtime, and an owner's or admin's token:

```bash
curl -X POST "$MULTICA_API_URL/api/company/setup" \
  -H "Authorization: Bearer $MULTICA_TOKEN" \
  -H "X-Workspace-ID: $MULTICA_WORKSPACE_ID" \
  -H 'Content-Type: application/json' \
  -d '{"secretary_instructions": "<the text of the secretary template in packages/core/agents/instruction-templates.ts>", "timezone": "Europe/Berlin"}'
```

Setup is idempotent. It creates the Secretary, the Executive floor and squad, the daily brief autopilot, and the paused weekly review, and it returns the readiness checklist. A second call changes nothing that already exists.

After upgrading, check the company: seat the squads you want on a floor, and see the [Company](https://multica.ai/docs/company) and [Secretary and briefs](https://multica.ai/docs/secretary) pages for what members will see.

---

## Manual Docker Compose Setup

If you prefer running Docker Compose steps manually instead of `make selfhost`:

```bash
git clone https://github.com/multica-ai/multica.git
cd multica
cp .env.example .env
```

Edit `.env` — set `JWT_SECRET` (required): docker compose refuses to start without
it, and a production backend refuses to boot on the dev default or any known
placeholder.

```bash
JWT_SECRET=$(openssl rand -hex 32)
```

Then start everything:

```bash
docker compose -f docker-compose.selfhost.yml pull
docker compose -f docker-compose.selfhost.yml up -d
```

## Manual CLI Configuration

If you prefer configuring the CLI step by step instead of `multica setup`:

```bash
# Point CLI to your local server
multica config set server_url http://localhost:8080
multica config set app_url http://localhost:3000

# Login (opens browser)
multica login

# Start the daemon
multica daemon start
```

For production deployments with TLS:

```bash
multica config set app_url https://app.example.com
multica config set server_url https://api.example.com
multica login
multica daemon start
```

## Advanced Configuration

For environment variables, manual setup (without Docker), reverse proxy configuration, database setup, and more, see the [Advanced Configuration Guide](SELF_HOSTING_ADVANCED.md).
