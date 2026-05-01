---
document_type: research
project_id: SPEC-2026-05-01-001
last_updated: 2026-05-01T11:30:00Z
---

# Forge — Research Notes

Verified findings from spike work on 2026-05-01.

## Tooling availability (verified)

| Tool | Status | Version |
|---|---|---|
| `doctl` | Authenticated as sdevinarayanan@asymbl.com, Asymbl Intelligence team, 25 droplet limit | 1.142.0 |
| `cloudflared` | Installed locally | v2026.3.0 |
| `wrangler` | Installed locally | latest |
| Docker Desktop | Running | 29.4.0 |
| Docker Compose | Available | v5.1.2 |
| Homebrew | Available | 5.1.8 |
| `multica` CLI (upstream) | Installed via `brew tap multica-ai/tap` | 0.2.22 |

## DigitalOcean infrastructure

### Droplet sizing (confirmed)

For 3-container stack (Postgres + Go backend + Next.js SSR), 5-10 internal users, ~50 issues/day, no agent execution on this server:

- **Recommended**: `s-2vcpu-4gb` ($24/mo)
- Postgres 17 + pgvector: ~150-300 MB
- Go backend: ~80-150 MB
- Next.js 16 SSR: ~250-450 MB
- OS + Docker: ~400-600 MB
- cloudflared: ~17 MB
- **Total steady state**: ~1.0-1.5 GB; 4 GB leaves comfortable headroom for `next build` and Postgres autovacuum spikes
- 2 GB option (`s-1vcpu-2gb` @ $12/mo) risks OOM during build

### Region choice

`sfo3` (San Francisco 3) — colocates with `casey`, `ben-*`, `jarvis` droplets per existing pattern. Available regions: nyc1/nyc2/nyc3, sfo2/sfo3, ams3, fra1, lon1, sgp1, blr1, syd1, atl1, ric1, tor1.

### Existing Asymbl droplets (for reference)

| Name | IP | Role |
|---|---|---|
| `jarvis-openclaw` | 64.23.198.51 | Hermes / OpenClaw |
| `casey` | 167.71.27.87 | Marketing assistant |
| `ba-ben-highspring` | 147.182.244.89 | Ben - Highspring |
| `ben-asymbl` | 137.184.40.183 | Ben - Asymbl |
| `ben-judge-group` | 146.190.49.230 | Ben - Judge Group |
| `ben-corpay` | 147.182.194.102 | Ben - Corpay (Phase 2 target) |
| `Asymbl-MCP-Server-1` | 167.71.94.38 | MCP server |

## Cloudflare configuration

### Existing tunnels (reference pattern)

| Name | Created | Purpose |
|---|---|---|
| `ba-ben-highspring` | 2026-02-05 | OpenClaw |
| `ben-asymbl` | 2026-03-20 | OpenClaw |
| `ben-corpay` | 2026-04-30 | OpenClaw |
| `ben-judge-group` | 2026-03-23 | OpenClaw |
| `casey` | 2026-04-12 | Hermes |
| `foresight` | 2026-03-29 | (other) |
| `jarvis` | 2026-02-01 | Hermes |

All use locally-managed tunnels with cloudflared as systemd service. We follow the same pattern.

### Cloudflare Tunnel ingress for Forge

Confirmed via Cloudflare docs: ingress rules accept Go-syntax `path` regex on the same hostname. This eliminates the need for nginx/Caddy.

Config to deploy at `/etc/cloudflared/config.yml`:

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

WebSocket support via `cloudflared` is built-in. **Important**: Next.js rewrites do NOT forward WebSocket Upgrade headers, so `/ws` MUST be a tunnel-level rule routing directly to `:8080`.

## Resend configuration

### API key validity

`re_96wmDVdD_4DwMMYAtTG5VdF9J4H7LiYFJ` — verified valid via test API call.

### Domain status

`asymbl.com` is **not yet verified** on Resend. User direction: verify `asymbl.app` instead, since FROM email will be `forge@asymbl.app`. This separates user identity (`@asymbl.com`) from system mail (`asymbl.app`).

### Required DNS records (for asymbl.app on Cloudflare)

Per Resend's Cloudflare guide (`https://resend.com/docs/dashboard/domains/cloudflare`):

| Type | Name | Value | Proxy |
|---|---|---|---|
| MX | `send` | `feedback-smtp.<region>.amazonses.com` (priority 10) | DNS Only |
| TXT | `send` | `v=spf1 include:amazonses.com ~all` | DNS Only |
| TXT | `resend._domainkey` | (DKIM key from Resend dashboard) | DNS Only |
| TXT | `_dmarc` | `v=DMARC1; p=none;` | DNS Only |

**Critical**: All Resend DNS records must be **DNS Only (grey cloud)** in Cloudflare. Proxy mode breaks SPF/DKIM/MX.

## Multica daemon — self-hosted compatibility (confirmed)

Verified via source at `server/internal/daemon/config.go:366` (`NormalizeServerBaseURL`):

- Daemon accepts `https://`, `http://`, `ws://`, `wss://` — `https://forge.asymbl.app` normalizes correctly
- HTTP base + WSS upgrade for `/ws`

Setup commands (after rebrand, daemon will be `forge` instead of `multica`):

```bash
forge config set server_url https://forge.asymbl.app
forge config set app_url https://forge.asymbl.app
forge login --token   # paste PAT from Forge UI
forge daemon start
```

systemd unit pattern (for any remote droplet, e.g. Phase 2):

```ini
[Unit]
Description=Forge agent daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=forge
Environment=HOME=/home/forge
ExecStart=/usr/local/bin/forge daemon start --foreground
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

## Branding scope (from grep)

333 total `Multica`/`multica` references across the codebase. Breakdown to be done in Phase 1 Task 1.2.

Key user-visible files identified:
- `apps/web/app/layout.tsx` — page title, metadata
- `apps/web/app/(landing)/*` — entire landing site
- `apps/web/app/(auth)/login/page.tsx` — login UI
- `packages/views/auth/login-page.tsx` — shared login component
- `packages/views/layout/app-sidebar.tsx` — sidebar branding
- `packages/views/runtimes/components/connect-remote-dialog.tsx` — hardcodes `multica.ai` URLs
- `packages/ui/components/common/multica-icon.tsx` — clip-path SVG logo (replace entirely)
- `server/internal/service/email.go` — email subject + body
- `apps/desktop/package.json` — `@multica/desktop` package name
- `apps/desktop/electron-builder.yml` — appId, productName, icons
- `docker-compose.selfhost.yml` — image names, container names, volumes

## Multica license (verified)

Modified Apache 2.0 with two extra clauses (file `LICENSE`):

1. **Commercial use restriction (§1.a)**: Cannot offer Multica as a hosted service or embedded component to third parties without commercial license. **Internal use within a single organization is explicitly allowed.** Forge is internal-only — compliant.

2. **Logo restriction (§1.b)**: Cannot remove/modify the Multica LOGO or copyright info in the frontend (`apps/web/`). **We violate this for the rebrand.** Risk acknowledged — see ADR-008.

License text states copyright as `© 2025 Multica, Inc.`

## Asymbl Brand Style Guide (key extracts)

Source: `/Users/sdevinarayanan/Downloads/Asymbl Logo & Favicon/Asymbl Brand Style Guide (1).md`

### Colors (light mode primary)

```css
:root {
  --color-primary: #032D60;       /* Navy */
  --color-primary-light: #385CAE;
  --color-bg: #FFFFFF;
  --color-bg-alt: #E8F4FC;        /* Light Blue */
  --color-text: #032D60;
  --color-text-muted: #595959;
  --color-cta: #DD7001;           /* Orange */
  --color-cta-hover: #C46300;
  --color-success: #70BF75;
  --color-warning: #FBBE01;
  --color-error: #B90E0A;
  --color-info: #385CAE;
  --color-accent: #90D0FE;
  --color-purple: #888DD0;
}
```

### 60-30-10 ratio
- 60% Navy and White
- 30% Light Blue and Gray
- 10% Orange and accent colors (CTAs, highlights)

### Logo files available
- `Logo_ Full Color on White.png` — primary for light backgrounds
- `Logo_ Full Color on Blue 90.png` — for navy backgrounds
- `Logo_ Reverse on Black.png` — high contrast
- `Favicon_ Full Color on White.png`
- `AsymblLogo.ai`, `.eps` (vector source)

### Card spec (will use across UI)
- Border radius: 12-16px
- Shadow: `0 2px 8px rgba(3, 45, 96, 0.1)`
- Background: White
- Optional colored header bar (Orange / Yellow / Green / Purple / Blue per item type)

### Buttons

| Type | Background | Text | Border |
|---|---|---|---|
| Primary | Orange `#DD7001` | White | None |
| Primary Hover | `#C46300` | White | None |
| Secondary | Transparent | Navy | Navy 2px |
| Secondary Hover | Light Blue `#E8F4FC` | Navy | Navy 2px |
| Ghost | Transparent | Orange | None |

## Open research items (deferred)

- Apple Developer account status for desktop app codesigning
- GHCR org `asymbl` existence (vs personal `shivasymbl`)
- Cloudflare account holding `asymbl.app` zone (one of two visible Account Tags)
- Doppler workspace name conventions (project: `forge` vs `asymbl-forge`)
- Whether `zen` and `codex` CLI tools are installed locally (referenced by user, not yet verified)
