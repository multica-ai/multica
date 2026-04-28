# Feature 3: Cloud Deploy

## Problem

Multica kan kun køres lokalt via `docker-compose.selfhost.yml` eller `make dev`.
Der er ingen one-click cloud deploy, ingen managed hosting guide, og CLI'ens
default fallback til `api.multica.ai` er den eneste cloud-option.

## What Exists Today

- `docker-compose.selfhost.yml` — 3 services: postgres, backend (Go), frontend (Next.js)
- `Dockerfile` — multi-stage Go build for backend
- `Dockerfile.web` — Next.js standalone build for frontend
- Ingen state ud over PostgreSQL + S3 (ingen Redis, queues, workers)
- Alle env vars allerede parameteriseret i `.env.example`
- CLI understøtter allerede remote server via `multica config set server_url <url>`

## Scope

### Phase 1: Fly.io Deploy (1-2 dage)

Fly.io er det mest naturlige fit — Go + Next.js + Postgres, alt managed.

**Filer:**
- `fly.toml` for backend (Go server, port 8080)
- `fly.toml` for frontend (Next.js, port 3000) — eller combine i én app med reverse proxy
- Fly Postgres (managed, automatic backups)
- Secrets via `fly secrets set`

**Kommandoer:**
```bash
fly apps create multica-api
fly postgres create --name multica-db
fly deploy -c fly.backend.toml
fly deploy -c fly.frontend.toml
```

**S3 alternative:** Tigris (Fly's built-in S3-kompatibel storage) for file uploads.

### Phase 2: Railway / Render Alternative (1 dag ekstra)

- `railway.toml` eller `render.yaml` — service definitions
- Begge platforms autodetecter Dockerfile
- Managed Postgres included

### Phase 3: One-Click Deploy Button (1 dag)

- "Deploy to Fly" / "Deploy to Railway" button i README
- Pre-configured template med env vars
- Docs i `SELF_HOSTING.md` (eksisterer allerede, udvid med cloud sections)

## Architecture Notes

### Hvad der virker out of the box:
- Backend er stateless — horizontal scaling muligt
- Alle env vars allerede parameteriseret
- CORS allerede konfigurerbart via `CORS_ALLOWED_ORIGINS`
- Email via Resend (cloud-native, allerede integreret)
- CLI peger allerede mod remote server

### Hvad der kræver opmærksomhed:
- **WebSocket** — Fly.io/Railway håndterer WS, men load balancer sticky sessions kan kræves
- **File uploads** — kræver S3/Tigris bucket (ikke lokal disk)
- **Migrations** — backend kører auto-migration ved start (allerede implementeret)
- **DNS + TLS** — Fly/Railway giver gratis TLS, custom domain via CNAME

### Daemon + Cloud Server

CLI/daemon-flowet virker allerede med remote server:
```bash
multica config set server_url https://multica-api.fly.dev
multica login
multica daemon start
```

Daemon kører lokalt, taler med cloud server. Ingen ændringer nødvendige.

## Estimate

| Task | Effort |
|------|--------|
| Fly.io config + deploy | 1 dag |
| Test + fix WebSocket/CORS | 0.5 dag |
| S3/Tigris setup for uploads | 0.5 dag |
| Docs + deploy button | 0.5 dag |
| Railway/Render alternative | 1 dag |
| **Total** | **3-5 dage** |

## Files to Create

- Ny: `fly.backend.toml`
- Ny: `fly.frontend.toml` (eller combined)
- Modify: `SELF_HOSTING.md` — cloud deploy sections
- Modify: `README.md` — deploy buttons
