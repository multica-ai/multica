---
document_type: implementation_plan
project_id: SPEC-2026-05-01-001
version: 1.0.0
last_updated: 2026-05-01T11:30:00Z
status: in-review
estimated_effort: 5-7 working days for v1 (one engineer + Claude)
---

# Forge — Implementation Plan

## Overview

Five sequential phases: **fork & rebrand** → **infrastructure** → **build & deploy** → **email & auth** → **launch & test**. Each phase has a hard exit criterion. Code review gate (`zen` + `codex` CLI) runs before any phase boundary commits.

| Phase | Duration | Key Deliverables | Exit Gate |
|---|---|---|---|
| 1: Fork & Rebrand | 1.5-2 days | Forge repo with all branding swapped | `grep -ri multica src/ \| wc -l` returns 0 user-visible references |
| 2: Infrastructure | 0.5 day | Droplet, tunnel, DNS, Resend domain | `forge.asymbl.app` returns 200 (placeholder OK) |
| 3: Build & Deploy | 1 day | GHCR images live, droplet running stack | Docker compose ps green, login page loads |
| 4: Email & Auth | 0.5 day | Domain restriction works, OTP delivers | Test signup with @asymbl.com works, others rejected |
| 5: Launch & Test | 0.5-1 day | E2E flow validated, team invited | Owner + 1 member assign issue, agent executes |
| **Total** | **4-5 days** | | |

Add 30% buffer for unknowns → **5-7 working days**.

---

## Pre-flight prerequisites (user actions)

Before kicking off Phase 1, the user provides:

- [ ] **GitHub repo**: create private `github.com/asymbl/forge` (or `github.com/shivasymbl/forge`)
- [ ] **GHCR access**: confirm Docker images can publish to `ghcr.io/<org>/forge-*`
- [ ] **Cloudflare account**: confirm `asymbl.app` zone is in account where existing tunnels live
- [ ] **Resend dashboard**: click "Add Domain" for `asymbl.app`, capture the DKIM CNAME values
- [ ] **Doppler workspace**: create project `forge` with config `prd`
- [ ] **zen + codex CLI**: confirm both are installed and working locally

---

## Phase 1: Fork & Rebrand

**Duration**: 1.5-2 days
**Goal**: Produce a fully rebranded Forge codebase in our own private repo. Zero user-visible "Multica" references. Asymbl light-mode brand applied.

### Task 1.1: Initialize Forge repo

- Push current `plan/forge-asymbl-fork` branch to new private repo as `main`
- Set up `.github/CODEOWNERS` (Shiv as default reviewer)
- Add `LICENSE.asymbl` alongside upstream `LICENSE` (note: internal use only, fork attribution preserved)
- Tag commit `v0.0.1-fork`
- **Effort**: 30 min
- **Acceptance**: Repo exists, git history preserves Multica attribution, can clone privately

### Task 1.2: Web frontend — brand replacement

Files to edit (verified during research):

- `apps/web/app/layout.tsx` — page title, metadata
- `apps/web/app/(landing)/*` — landing pages (homepage, about, changelog) — **nuke entirely**, replace with simple "Forge" placeholder or 404
- `apps/web/app/robots.ts`, `sitemap.ts` — block all crawlers (private)
- `apps/web/app/(auth)/login/page.tsx` — login page copy
- `apps/web/app/auth/callback/page.tsx` — OAuth callback (if used)
- `apps/web/app/custom.css` + `apps/web/app/globals.css` — Asymbl color tokens
- `apps/web/app/favicon.ico/route.ts` — serve Asymbl favicon

Find-and-replace strategy:
- `Multica` → `Forge` (case-sensitive)
- `multica.ai` → `asymbl.app`
- `multica` (lowercase, in URLs/handles) → `forge`
- **Skip**: package import paths (`@multica/...`) — those are renamed in Task 1.5

**Effort**: 3-4 hours
**Acceptance**: `grep -rIn "Multica\|multica\.ai" apps/web/app/ packages/views/` returns zero user-visible hits (only allowable: code identifiers we'll rename in 1.5)

### Task 1.3: Logo + favicon swap

- Copy Asymbl assets from `~/Downloads/Asymbl Logo & Favicon/` to `apps/web/public/brand/`
  - `Logo_ Full Color on White.png` → `public/brand/asymbl-logo-color.png`
  - `Favicon_ Full Color on White.png` → `public/brand/favicon.png`
  - Generate ICO + multiple sizes for desktop app and OS chrome
- Replace `packages/ui/components/common/multica-icon.tsx`:
  - Rename file → `asymbl-logo.tsx`
  - Replace clip-path asterisk with `<img>` referencing `/brand/asymbl-logo-color.png`
  - Component name: `AsymblLogo` (or keep export name + alias for minimal diff)
- Update all imports: `MulticaIcon` → `AsymblLogo`

**Effort**: 1-2 hours
**Acceptance**: Login page loads with Asymbl logo; favicon shows Asymbl mark

### Task 1.4: Tailwind theme — Asymbl light-mode colors

Files: `apps/web/app/globals.css`, `tailwind.config.*`

Apply CSS variable mapping per brand guide §Application: Website:

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
  --color-error: #B90E0A;
  --color-info: #385CAE;
  --color-accent: #90D0FE;
}
```

Force light mode by default:
- Remove dark mode toggle from header (or hide it)
- Set `<html className="light">` permanently
- Tailwind config: `darkMode: 'class'` → keep but never apply

Update primary action buttons:
- Multica's black buttons → Orange `#DD7001`
- Border radius: 12-16px (already close)

**Effort**: 2-3 hours (visual tuning iterations)
**Acceptance**: App matches Asymbl brand guide screenshots; no dark mode visible

### Task 1.5: Package & component renames

- `packages/ui/components/common/multica-icon.tsx` → `asymbl-logo.tsx` (done in 1.3)
- `apps/desktop/package.json`: `@multica/desktop` → `@asymbl/forge-desktop`
- Workspace root `package.json`: any `multica`-named workspaces
- Go module path: keep `github.com/multica-ai/multica` to minimize diff with upstream (it's not user-visible)
- Docker compose: rename containers
  - `multica-postgres-1` → `forge-postgres`
  - `multica-backend-1` → `forge-backend`
  - `multica-frontend-1` → `forge-web`
  - Volume `multica_pgdata` → `forge_pgdata`
  - Network → `forge_default`

**Effort**: 1-2 hours
**Acceptance**: `pnpm install && pnpm build` works; `docker compose ps` shows new container names

### Task 1.6: Email template rebrand (server/internal/service/email.go)

Edit `SendVerificationCode` and `SendInvitationEmail` HTML:

- Subject lines:
  - `"Your Multica verification code"` → `"Your Forge verification code"`
  - `"%s invited you to %s on Multica"` → `"%s invited you to %s on Forge"`
- Body:
  - Add Asymbl logo via inline `<img src="https://forge.asymbl.app/brand/asymbl-logo-color.png" />` at top
  - Button color: `#000` → `#DD7001` (Orange)
  - Background: White
  - Heading color: Navy `#032D60`
  - Add Asymbl footer with `forge.asymbl.app` link

**Effort**: 1 hour
**Acceptance**: Send test email; renders correctly in Gmail + Apple Mail

### Task 1.7: Mac desktop app rebrand

`apps/desktop/`:
- `package.json`: name + description + author
- `electron-builder.yml`: appId `com.asymbl.forge`, productName "Forge", icons → Asymbl
- `resources/icon.icns`, `icon.png`, `icon.ico` → regenerate from Asymbl logo (use `electron-icon-builder` or manual)
- `src/`: replace all "Multica" UI strings
- `scripts/bundle-cli.mjs`: bundle the renamed `forge` CLI binary instead of `multica`
- `scripts/brand-dev-electron.mjs`: existing branding script — review and update Asymbl-side

**Effort**: 4-6 hours (icon regeneration is fiddly; codesigning is separate)
**Acceptance**: `pnpm dev` in `apps/desktop` opens Electron window labeled "Forge"; `pnpm package` produces `Forge.dmg`

### Task 1.8: CLI rename (forge from multica)

Multica's CLI is in `cli/` (Go binary). Two approaches:

**Option A (lower effort)**: Symlink `forge` → `multica` post-install. Inside the binary, reference to "multica" remains in help text and config paths.
**Option B (cleaner)**: Build with renamed cobra root: rename `cli/cmd/root.go`'s `cobra.Command{Use: "multica"}` to `Use: "forge"`. Config dir `~/.multica/` → `~/.forge/`. Binary distributed as `forge`.

**Recommended**: Option B for v1 to fully erase Multica from end-user perspective.

Effort: 2-3 hours.
**Acceptance**: `forge --help` works; `forge daemon start` runs; config in `~/.forge/`

### Task 1.9: Phase 1 review gate

Before merging Phase 1:

```bash
# Run zen review
zen codereview --files "$(git diff --name-only main)"

# Run codex CLI review
codex review --diff main..HEAD
```

Address any P0/P1 findings. Then merge `phase-1-rebrand` PR to `main`.

**Acceptance**: Both tools return clean (or addressed); PR approved by Shiv.

### Phase 1 Exit Criteria

- [ ] Zero "Multica" string in any user-visible surface (UI, emails, desktop title bar, page metadata)
- [ ] Asymbl logo appears on login page, sidebar, favicon, OG image
- [ ] Light mode default, Asymbl colors applied
- [ ] Mac desktop app rebuilt as "Forge"
- [ ] CLI renamed to `forge`
- [ ] zen + codex review clean
- [ ] PR merged to Forge `main`

---

## Phase 2: Infrastructure

**Duration**: 0.5 day
**Goal**: Provisioning is done. The droplet, DNS, tunnel, Resend domain, Doppler are all live and ready to receive deployments.

### Task 2.1: DigitalOcean droplet provisioning

```bash
doctl compute droplet create forge \
  --image ubuntu-24-04-x64 \
  --size s-2vcpu-4gb \
  --region sfo3 \
  --ssh-keys <your-key-id> \
  --tag-name forge,asymbl \
  --enable-monitoring \
  --enable-backups
```

Post-provision setup:
- SSH in, create non-root user `forge`, disable root SSH
- Install Docker, Docker Compose plugin, cloudflared, doppler CLI
- UFW: allow 22 only, deny inbound otherwise
- Hostname: `forge`

**Effort**: 30 min
**Acceptance**: `ssh forge@<ip>` works; `docker --version` and `cloudflared --version` succeed

### Task 2.2: Cloudflare Tunnel setup

```bash
# On droplet
cloudflared tunnel login           # browser auth, drops cert.pem
cloudflared tunnel create forge    # writes UUID.json
cloudflared tunnel route dns forge forge.asymbl.app
# Write /etc/cloudflared/config.yml (per ARCHITECTURE.md)
cloudflared service install        # systemd unit
systemctl enable --now cloudflared
```

**Effort**: 20 min
**Acceptance**: `curl https://forge.asymbl.app` returns 502 (no backend yet) — proves DNS + tunnel work

### Task 2.3: Resend domain verification (asymbl.app)

User actions in Resend dashboard:
1. Click "Add Domain" → `asymbl.app`
2. Receive DKIM CNAME values + SPF TXT
3. Add to Cloudflare DNS (DNS Only — grey cloud, NOT proxied):
   - MX `send` → `feedback-smtp.us-east-1.amazonses.com` priority 10
   - TXT `send` → `v=spf1 include:amazonses.com ~all`
   - TXT `resend._domainkey` → (long DKIM value from Resend)
   - TXT `_dmarc` → `v=DMARC1; p=none;`
4. Click "Verify" in Resend, wait for green check

**Effort**: 15 min (mostly waiting on DNS)
**Acceptance**: Resend dashboard shows asymbl.app verified; test API call sending from `forge@asymbl.app` succeeds

### Task 2.4: Doppler project + secrets

```bash
doppler projects create forge
doppler configs create prd --project forge
# Add secrets via dashboard or CLI:
doppler secrets set --project forge --config prd JWT_SECRET="$(openssl rand -hex 32)"
doppler secrets set --project forge --config prd DATABASE_URL="postgres://forge:$(openssl rand -hex 24)@postgres:5432/forge"
doppler secrets set --project forge --config prd RESEND_API_KEY="re_96wmDVdD_4DwMMYAtTG5VdF9J4H7LiYFJ"
doppler secrets set --project forge --config prd RESEND_FROM_EMAIL="forge@asymbl.app"
doppler secrets set --project forge --config prd ALLOWED_EMAIL_DOMAIN="asymbl.com"
doppler secrets set --project forge --config prd APP_ENV="production"
doppler secrets set --project forge --config prd FRONTEND_ORIGIN="https://forge.asymbl.app"
```

**Effort**: 20 min
**Acceptance**: `doppler secrets --project forge --config prd` lists all expected vars

### Phase 2 Exit Criteria

- [ ] Forge droplet up and reachable
- [ ] `forge.asymbl.app` returns 502 via Cloudflare Tunnel (proves routing works)
- [ ] Resend `asymbl.app` domain verified
- [ ] Doppler `forge/prd` populated

---

## Phase 3: Build & Deploy

**Duration**: 1 day
**Goal**: Custom Forge images live in GHCR, droplet pulls + runs them, full stack healthy.

### Task 3.1: Build Forge Docker images locally

```bash
# Web frontend
docker buildx build \
  --platform linux/amd64 \
  --build-arg NEXT_PUBLIC_API_URL=https://forge.asymbl.app \
  --build-arg NEXT_PUBLIC_WS_URL=wss://forge.asymbl.app/ws \
  -f apps/web/Dockerfile \
  -t ghcr.io/asymbl/forge-web:v0.1.0 \
  --push .

# Backend
docker buildx build \
  --platform linux/amd64 \
  -f server/Dockerfile \
  -t ghcr.io/asymbl/forge-backend:v0.1.0 \
  --push .
```

**Effort**: 1-2 hours (first build is slow; iterate)
**Acceptance**: Images visible in GHCR; can `docker pull` from droplet

### Task 3.2: docker-compose.selfhost.yml customization

Update from upstream:
- Image references: `ghcr.io/asymbl/forge-*`
- Container/volume/network names: `forge-*`
- Postgres database/user: `forge`
- Add `env_file: .env` (sourced from Doppler)

**Effort**: 30 min
**Acceptance**: `docker compose config` validates

### Task 3.3: Deploy to droplet

```bash
# On Mac: package compose + .env
doppler secrets download --project forge --config prd --no-file --format docker > .env
scp docker-compose.selfhost.yml forge@<ip>:/home/forge/
scp .env forge@<ip>:/home/forge/

# On droplet
ssh forge@<ip>
echo "$GHCR_PAT" | docker login ghcr.io -u <username> --password-stdin
docker compose -f docker-compose.selfhost.yml --env-file .env pull
docker compose -f docker-compose.selfhost.yml --env-file .env up -d
docker compose ps  # verify all 3 healthy
```

**Effort**: 1 hour
**Acceptance**: `curl https://forge.asymbl.app` returns Forge login page (no 502)

### Task 3.4: Smoke test

- Login page renders with Asymbl branding
- Try login with test email → check backend logs for OTP
- Verify OTP, get to onboarding flow
- Create workspace
- Page navigation works (no 404s, no console errors)

**Effort**: 30 min
**Acceptance**: Owner account created and logged in

### Phase 3 Exit Criteria

- [ ] All 3 containers healthy on droplet
- [ ] `forge.asymbl.app` serves Forge UI with TLS
- [ ] Postgres migrations ran cleanly
- [ ] Owner account created via dev path (or Resend if email verification works)

---

## Phase 4: Email & Auth Hardening

**Duration**: 0.5 day
**Goal**: Production auth working end-to-end. Domain restriction enforced.

### Task 4.1: Email domain restriction patch

Edit `server/internal/handler/auth.go`:

```go
// In SendCode handler
allowedDomain := os.Getenv("ALLOWED_EMAIL_DOMAIN")
if allowedDomain != "" {
    parts := strings.Split(req.Email, "@")
    if len(parts) != 2 || !strings.EqualFold(parts[1], allowedDomain) {
        h.respondError(w, r, http.StatusForbidden, "email domain not allowed")
        return
    }
}
```

Add unit test in `auth_test.go` verifying:
- @asymbl.com email → success
- @gmail.com email → 403
- Unset env var → no restriction (preserves upstream behavior)

**Effort**: 1 hour
**Acceptance**: Tests pass; manual test confirms gmail signups fail with 403

### Task 4.2: Build + deploy v0.1.1 with patch

```bash
git tag v0.1.1
docker buildx build --push -t ghcr.io/asymbl/forge-backend:v0.1.1 -f server/Dockerfile .
# On droplet:
sed -i 's/v0.1.0/v0.1.1/g' .env
docker compose -f docker-compose.selfhost.yml pull backend
docker compose -f docker-compose.selfhost.yml up -d backend
```

**Effort**: 30 min

### Task 4.3: Verify Resend email delivery

- Trigger send-code with real `@asymbl.com` email
- Verify OTP arrives within 30s
- Check email FROM = `forge@asymbl.app`
- Check rendering in Gmail + Apple Mail
- Verify OTP works for login

**Effort**: 30 min
**Acceptance**: Real OTP email lands in inbox, code authenticates user

### Task 4.4: zen + codex review of patches

Run on the `auth.go` and email template diffs.

**Acceptance**: No outstanding findings; merge to `main`

### Phase 4 Exit Criteria

- [ ] `@asymbl.com` signup → OTP → JWT works
- [ ] `@gmail.com` (or other) signup → 403
- [ ] Email template renders cleanly with Asymbl branding
- [ ] Code reviewed by zen + codex

---

## Phase 5: Launch & Test

**Duration**: 0.5-1 day
**Goal**: Stakeholder validation, first agent task executed end-to-end.

### Task 5.1: Owner sets up first workspace

- Sign up as `shiv@asymbl.com`
- Create "Asymbl Internal" workspace
- Create a project called "Forge Bootstrap"

### Task 5.2: Local daemon test

- Install `forge` CLI on developer Mac
- `forge setup self-host --server-url https://forge.asymbl.app`
- Login flow → daemon starts
- `forge runtime list` → shows Claude Code, Codex, Gemini detected

### Task 5.3: First agent execution

- Create issue "Hello from Forge"
- Description: "Print 'hello world' and exit"
- Assign to Claude runtime
- Watch real-time progress
- Verify completion + artifact

### Task 5.4: Invite second user

- From Owner account, invite second `@asymbl.com` user as `member`
- Verify invite email received + accepted
- Verify member CANNOT create agents (gets 403)

### Task 5.5: Backup verification

- Run pg_dump cron manually
- Verify dump appears in DO Spaces
- Test restore on a scratch container

### Task 5.6: Documentation

Write a 1-page `RUNBOOK.md` covering:
- How to deploy a new image version
- How to add a new user (or remove)
- How to rotate secrets
- How to roll back

### Phase 5 Exit Criteria

- [ ] Owner + 1 member onboarded
- [ ] First issue executed end-to-end
- [ ] Backup verified
- [ ] Runbook written
- [ ] Status moved to `completed` in spec README

---

## Dependency Graph

```
Phase 1 (Rebrand) ──┬──► Phase 3 (Deploy)
                    │
Phase 2 (Infra) ────┴──► Phase 3 (Deploy) ──► Phase 4 (Auth) ──► Phase 5 (Launch)
```

Phases 1 and 2 can run in parallel (different work streams). Everything else is sequential.

## Risk Mitigation Tasks

| Risk | Mitigation Task | Phase |
|---|---|---|
| Asymbl logo licensing risk | Document acknowledged risk in DECISIONS.md, never expose to clients | 1 |
| Cloudflare Tunnel WebSocket issues | Test `/ws` connection in Phase 3 smoke test | 3 |
| Resend domain not verified by deployment day | Verify domain in Phase 2 (before Phase 4) | 2 |
| Desktop app codesign issues | Use existing Apple Dev account if available; fallback to manual right-click-open | 1.7 |
| Droplet OOM during build | Build images locally + push (never build on droplet) | 3 |

## Testing Checklist

- [ ] Backend unit tests pass (Multica's + new domain restriction tests)
- [ ] Frontend builds without TS errors
- [ ] Email OTP integration test (real Resend send)
- [ ] Domain restriction integration test (gmail.com → 403)
- [ ] WebSocket connection test
- [ ] Postgres backup + restore cycle
- [ ] zen review pass
- [ ] codex CLI review pass
- [ ] Manual brand audit (no Multica strings visible anywhere)

## Documentation Tasks

- [ ] `docs/spec/active/2026-05-01-forge/` — this spec
- [ ] `RUNBOOK.md` for deploy/rollback (Phase 5)
- [ ] `BRANDING.md` — list of files modified during rebrand (helps with upstream sync)
- [ ] `CHANGELOG.md` — release notes per version

## Launch Checklist

- [ ] All Phase 5 exit criteria met
- [ ] DNS pointing correctly
- [ ] TLS valid
- [ ] Backups configured
- [ ] Monitoring alerts (UptimeRobot ping every 5 min)
- [ ] Team announcement drafted
- [ ] Rollback procedure tested

## Post-Launch

- [ ] Monitor first 48h for crashes
- [ ] Gather feedback from first 3-5 users
- [ ] Plan Phase 2 scope (Ben Corpay, Hermes, OpenClaw)
- [ ] Move spec to `docs/spec/completed/`
