---
document_type: architecture
project_id: SPEC-2026-05-01-001
version: 1.0.0
last_updated: 2026-05-01T11:30:00Z
status: in-review
---

# Forge — Technical Architecture

## System Overview

Forge is a 3-container Docker Compose stack on a single DigitalOcean droplet, fronted by a Cloudflare Tunnel terminating at `forge.asymbl.app`. Email auth is delegated to Resend. Source is forked from `github.com/multica-ai/multica`, rebranded, and built into custom Docker images published to GitHub Container Registry.

### High-level diagram

```
                     ┌─────────────────────────────────────────────┐
                     │          forge.asymbl.app (TLS)             │
                     │      Cloudflare edge                        │
                     └─────────────────┬───────────────────────────┘
                                       │ Tunnel (outbound only)
                                       ▼
   ┌──────────────────────────────────────────────────────────────────┐
   │  DigitalOcean droplet — s-2vcpu-4gb / sfo3 / Ubuntu 24.04        │
   │                                                                   │
   │  ┌──────────────┐  ingress:                                      │
   │  │ cloudflared  │   /api,/auth,/uploads,/ws  → :8080             │
   │  │ (systemd)    │   /*                       → :3000             │
   │  └──────┬───────┘                                                │
   │         │                                                         │
   │         ├─────────────────► Docker network: forge_default        │
   │         │                                                         │
   │  ┌──────▼─────────┐  ┌──────────────┐  ┌────────────────────┐  │
   │  │ web :3000      │  │ backend :8080│  │ postgres :5432     │  │
   │  │ Next.js 16 SSR │◄─┤ Go + Chi     │◄─┤ pg17 + pgvector    │  │
   │  └────────────────┘  └──────┬───────┘  └────────────────────┘  │
   │                             │                                    │
   │                             ▼                                    │
   │                       ┌────────────┐                             │
   │                       │ Resend API │                             │
   │                       │ (SMTP/API) │                             │
   │                       └────────────┘                             │
   │                                                                   │
   │  /etc/cron.daily: pg_dump → DO Spaces (asymbl-backups)           │
   └──────────────────────────────────────────────────────────────────┘

                                       ▲
                                       │ HTTPS + WSS
                                       │
                          ┌────────────┴────────────┐
                          │  Asymbl developer Macs  │
                          │  forge daemon detects   │
                          │  Claude Code, Codex,    │
                          │  Gemini on PATH         │
                          └─────────────────────────┘
```

## Key Design Decisions

See [DECISIONS.md](DECISIONS.md) for the full ADR set. Headlines:

- **ADR-001**: Self-host vs cloud — self-host on DO droplet for data sovereignty
- **ADR-002**: Single subdomain via Cloudflare Tunnel ingress regex (no Caddy/nginx)
- **ADR-003**: GHCR for image distribution (build locally, pull on droplet)
- **ADR-004**: Asymbl light-mode brand applied via Tailwind theme + replaced logo component
- **ADR-005**: Email domain restriction via backend middleware patch on `/auth/send-code`
- **ADR-006**: zen + codex CLI review gate on all code changes

## Component Design

### 1. Cloudflare Tunnel (`cloudflared` on droplet)

- **Purpose**: Terminate TLS, route requests by path to backend or frontend, hide droplet origin IP
- **Ingress rules** (in `/etc/cloudflared/config.yml`):
  ```yaml
  tunnel: <UUID>
  credentials-file: /etc/cloudflared/<UUID>.json
  ingress:
    - hostname: forge.asymbl.app
      path: ^/(api|auth|uploads|ws)(/.*)?$
      service: http://localhost:8080
      originRequest:
        noTLSVerify: true
    - hostname: forge.asymbl.app
      service: http://localhost:3000
    - service: http_status:404
  ```
- **Why path-regex over Caddy**: One fewer process; Cloudflare-native; matches existing Asymbl tunnel pattern (jarvis, casey, ben-*)
- **WebSocket support**: built-in, transparent
- **Rollback**: edit config.yml, `systemctl restart cloudflared`

### 2. Web frontend (`forge-web`, Next.js 16)

- **Purpose**: Server-rendered React UI, talks to backend via same-origin `/api`, `/auth`, `/uploads`
- **Image**: `ghcr.io/asymbl/forge-web:vX.Y.Z` (custom-built from forked source with brand changes)
- **Port**: 3000 (internal)
- **Build-time env vars** (baked into image):
  - `NEXT_PUBLIC_API_URL=https://forge.asymbl.app`
  - `NEXT_PUBLIC_WS_URL=wss://forge.asymbl.app/ws`
- **Branding overrides applied during build**:
  - Replace `MulticaIcon` component → `AsymblLogo` component using PNG from brand assets
  - Tailwind theme: Asymbl colors mapped to existing CSS variable names
  - All "Multica" strings → "Forge" in TSX literals
  - Favicon → Asymbl favicon
  - Email template HTML in Go server (subject + body) → Asymbl-branded

### 3. Backend (`forge-backend`, Go + Chi router)

- **Purpose**: API server, WebSocket hub, agent task orchestration, Resend integration
- **Image**: `ghcr.io/asymbl/forge-backend:vX.Y.Z`
- **Port**: 8080 (internal)
- **New code** (Asymbl-specific patches):
  - **Email domain middleware**: `server/internal/handler/auth.go` — patch `SendCode` handler to reject non-`@asymbl.com` emails when env var `ALLOWED_EMAIL_DOMAIN=asymbl.com` is set
  - **Email template branding**: `server/internal/service/email.go` — replace inline HTML with Asymbl-branded version (Navy header, Orange button, Asymbl logo via inline base64 or hosted URL)
- **Migrations**: auto-run on startup (Multica behavior, unchanged)
- **Env vars** (selected):
  - `JWT_SECRET` (32-char random, generated once)
  - `DATABASE_URL=postgres://forge:***@postgres:5432/forge`
  - `RESEND_API_KEY=re_96wmDVdD_...`
  - `RESEND_FROM_EMAIL=forge@asymbl.app`
  - `ALLOWED_EMAIL_DOMAIN=asymbl.com` ← **NEW**
  - `APP_ENV=production`
  - `FRONTEND_ORIGIN=https://forge.asymbl.app`

### 4. Database (`postgres`, pgvector/pgvector:pg17)

- **Purpose**: Primary data store, pgvector for skill semantic search
- **Image**: `pgvector/pgvector:pg17` (no custom build — official upstream)
- **Port**: 5432 (internal only, not exposed to host)
- **Volume**: `forge_pgdata` (persistent)
- **Database name**: `forge` (renamed from `multica`)
- **User**: `forge` with strong password from Doppler
- **Backup**: nightly `pg_dump -Fc` to DO Spaces

### 5. Mac desktop app (`@asymbl/forge-desktop`)

- **Purpose**: Native menubar/dock app for engineers to start/stop daemon, view tasks
- **Forked from**: `apps/desktop/` in Multica repo (Electron + Vite + React)
- **Rebrand changes**:
  - `package.json` name: `@multica/desktop` → `@asymbl/forge-desktop`
  - `electron-builder.yml` appId: `ai.multica.desktop` → `com.asymbl.forge`
  - App display name: "Multica" → "Forge"
  - Icons: `resources/icon.png`, `icon.ico`, `icon.icns` → Asymbl logo (regenerated for all platforms)
  - Server URL hardcoded to `https://forge.asymbl.app` (override via config)
  - Bundled CLI: `forge` (renamed from `multica`)
- **Distribution**: signed `.dmg` shared via Asymbl Drive or internal S3 (no public release)

### 6. CLI daemon (`forge`, renamed from `multica`)

- **Purpose**: Runs on developer's Mac, polls Forge backend for tasks, executes via local agent CLIs
- **Source**: Multica CLI fork
- **Rename strategy**: Symlink `forge` → `multica` binary OR rebuild Go binary with renamed cobra root command
- **Auto-detected providers**: claude, codex, gemini (whichever is on PATH)
- **Config**: `~/.forge/config.json` (renamed from `~/.multica/`)
- **Install**: `brew tap asymbl/forge && brew install forge` (private tap on private GitHub repo)

## Data Design

Uses Multica's existing schema unchanged. Tables include (Multica's docs):

- `users`, `workspaces`, `workspace_members`
- `agents`, `runtimes`, `daemons`
- `issues`, `issue_comments`, `issue_status_history`
- `projects`, `labels`
- `skills` (pgvector embedding column)
- `verification_codes`, `attempts` (auth)

No schema changes required for v1. The email-domain restriction is enforced at the handler layer, not the data layer.

## API Design

API surface inherited from Multica unchanged. Examples:

- `POST /auth/send-code` — request OTP (we patch this for domain restriction)
- `POST /auth/verify-code` — exchange OTP for JWT
- `GET /api/me`
- `GET /api/workspaces`
- `POST /api/agents` (admin/owner only)
- `GET /api/runtimes`
- WebSocket `/ws` — real-time updates

Authentication: `Authorization: Bearer <jwt>` + `X-Workspace-ID: <id>` for scoped endpoints.

## Integration Points

### Internal

| System | Type | Purpose |
|---|---|---|
| Doppler | Secrets fetch | All env vars sourced from Doppler at deploy time |
| GitHub | Source + GHCR | Repo + private Docker image registry |

### External

| Service | Type | Purpose |
|---|---|---|
| Resend | REST API | Send OTP + invite emails |
| DigitalOcean Spaces | S3-compatible | Postgres backups |
| Cloudflare | DNS + Tunnel | Routing + TLS |

## Security Design

### Authentication

- Email OTP (passwordless) — only flow in v1
- 6-digit numeric codes, 10-min expiry, max 5 attempts
- JWT issued on verification, 30-day expiry (configurable)
- Domain-restricted: `@asymbl.com` only

### Authorization

- Three roles per workspace: `owner`, `admin`, `member`
- Members cannot create agents, runtimes, or invite (admin/owner only)
- Default: first user to sign up becomes owner of their workspace

### Data Protection

- TLS 1.3 enforced via Cloudflare
- JWT secret in Doppler, never in repo
- Postgres password in Doppler
- Resend API key in Doppler
- No PII beyond email + name; no client data stored unless team uploads it as issue attachments

### Threat Model

| Threat | Mitigation |
|---|---|
| External actor signs up | `@asymbl.com` domain restriction at handler |
| Stolen JWT | 30-day expiry; revoke via DB on user offboard |
| Compromised Resend key | Rotate via Doppler; redeploy |
| Droplet root compromise | Cloudflare Tunnel hides IP; UFW restricts inbound to SSH only |
| Image supply chain | Pin GHCR image SHA in compose; never use `:latest` in production |
| Logo license enforcement | Internal-only deployment; no client exposure |

## Performance Considerations

### Expected load (v1)

- 5-10 concurrent users
- ~50 issues/day
- ~10 agent task executions/day
- ~100 WebSocket events/min peak

### Targets

| Metric | Target |
|---|---|
| Page load (first visit) | < 2s |
| API p95 latency | < 200ms |
| WebSocket reconnect | < 5s |
| OTP email delivery | < 30s p95 |

### Optimization

- pgvector HNSW index on skills (Multica default)
- Next.js standalone output for smaller image + faster cold start
- Cloudflare cache for static assets (fonts, logo)

## Reliability & Operations

### Availability

- Target: 99% (single-droplet, no HA)
- MTTR target: 30 min
- Recovery: `docker compose up -d` after droplet reboot; auto-start via systemd if needed

### Failure Modes

| Failure | Impact | Recovery |
|---|---|---|
| Droplet reboot | 1-2 min downtime | Docker auto-restart on boot |
| Postgres corruption | Full outage | Restore from latest pg_dump backup |
| Cloudflared dies | Site offline | systemd auto-restart (configured) |
| Resend outage | New users can't sign up | Existing JWTs still work; wait for Resend |
| GHCR outage | Can't pull new images | Old image keeps running |

### Monitoring (Phase 2)

- PostHog for product analytics + error tracking
- Cloudflare analytics for traffic + tunnel health
- DO monitoring for droplet CPU/RAM/disk
- UptimeRobot pinging `forge.asymbl.app/api/health`

### Backup & Recovery

- Nightly `pg_dump -Fc` → DO Spaces (`asymbl-backups/forge/YYYYMMDD.dump`)
- 30-day retention (Spaces lifecycle rule)
- DO droplet snapshot weekly ($4/mo for 80 GB)
- Restore tested manually before going live

## Testing Strategy

### Unit testing

- Backend: `go test ./...` (Multica's existing tests retained, plus new tests for `ALLOWED_EMAIL_DOMAIN` middleware)
- Frontend: `pnpm test` (Vitest, existing Multica tests)

### Integration testing

- One end-to-end test pre-launch: sign up → workspace → assign issue → execute → verify
- Resend domain verification test (send to a test inbox)
- Email domain rejection test (try gmail.com signup, expect 403)

### Manual QA

- Visual brand audit against Asymbl style guide
- Dark mode disabled / forced light mode check
- Mobile responsive sanity check (web only)

## Deployment Considerations

### Environment requirements

- Ubuntu 24.04 LTS on DO droplet
- Docker 24+ with Compose plugin
- cloudflared (latest)
- doppler CLI (for secret injection at deploy time)

### Configuration management

- `docker-compose.selfhost.yml` (forked, image names parameterized)
- `.env` populated from Doppler at deploy time, never committed
- `cloudflared` config in `/etc/cloudflared/config.yml`

### Rollout strategy

1. Bring up droplet
2. Provision DNS + tunnel
3. Verify Resend
4. Pull v0.1.0 images
5. `docker compose up -d`
6. Smoke test from owner account
7. Invite first 2-3 team members
8. Monitor 48h, then open to wider team

### Rollback plan

- Image rollback: change `MULTICA_IMAGE_TAG` in `.env`, `docker compose up -d`
- DNS rollback: cloudflared route is a single subdomain — point elsewhere or remove
- DB rollback: restore from latest pg_dump (last resort)

## Future Considerations

- Multi-instance deployment (per-client) — would require lifting the logo restriction or buying commercial license
- Hermes / OpenClaw runtime support on remote droplets (Phase 2)
- SSO via Google Workspace (Phase 2)
- Custom skill packs published to a Forge skill registry
- Integration with Asymbl's existing tools (Salesforce, Hindsight Cloud)
