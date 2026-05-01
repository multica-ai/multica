---
document_type: decisions
project_id: SPEC-2026-05-01-001
---

# Forge — Architecture Decision Records

## ADR-001: Self-host on DigitalOcean droplet (vs Multica Cloud)

**Date**: 2026-05-01
**Status**: Accepted
**Deciders**: Shiv

### Context

Multica offers a hosted SaaS at `multica.ai` and a self-host path. Using cloud means all task data, agent outputs, and code context get sent to Multica's servers. We're managing internal Asymbl work (client engagements, proprietary code) — that's a non-starter.

### Decision

Self-host on a DigitalOcean droplet (`s-2vcpu-4gb`, sfo3, $24/mo) following the existing Asymbl pattern (jarvis, casey, ben-* droplets).

### Consequences

**Positive:**
- Full data sovereignty — nothing leaves Asymbl-controlled infrastructure
- Can rebrand without commercial license (internal use is allowed by license)
- Matches existing ops muscle memory (DO + Cloudflare Tunnel + Doppler)

**Negative:**
- We own uptime, backups, security patching
- Manual upgrades when Multica releases new versions

**Neutral:**
- $24/mo cost is negligible

### Alternatives Considered

1. **Multica Cloud**: rejected — privacy, no rebrand
2. **AWS Fargate**: rejected — heavier ops than DO droplet for 5-10 users
3. **Self-host on Mac mini in office**: rejected — no office, no static IP, fragile

---

## ADR-002: Single subdomain via Cloudflare Tunnel ingress regex (no Caddy/nginx)

**Date**: 2026-05-01
**Status**: Accepted

### Context

Forge has two services on different ports (web on 3000, backend on 8080). They need to be exposed at one URL for clean UX. Three options:
1. Two subdomains (`forge.asymbl.app` + `api.forge.asymbl.app`)
2. Caddy/nginx reverse proxy on droplet
3. Cloudflare Tunnel ingress with path regex

### Decision

Cloudflare Tunnel ingress with path regex routing on a single subdomain (`forge.asymbl.app`).

### Consequences

**Positive:**
- One fewer process on the droplet (no Caddy/nginx)
- Native Cloudflare WebSocket support
- TLS termination + cert renewal handled by Cloudflare automatically
- Single subdomain = simpler CORS, simpler cookies

**Negative:**
- Path-based routing rules live in YAML (not version-controlled with the repo unless we add config there)
- If we ever add a third service, refactor needed

### Alternatives Considered

1. **Two subdomains**: rejected — two cert renewals, more CORS gymnastics
2. **Caddy on droplet**: rejected — extra process to manage and secure

---

## ADR-003: Build Docker images locally, publish to GHCR, pull on droplet

**Date**: 2026-05-01
**Status**: Accepted

### Context

We need a reproducible build/deploy pipeline. Options: build on droplet, build locally + scp, build locally + push to registry.

### Decision

Build images locally (or via GitHub Actions in future), publish to private GHCR (`ghcr.io/asymbl/forge-*`), pull from droplet.

### Consequences

**Positive:**
- Droplet stays light (no build deps installed)
- `next build` doesn't risk OOM on 4 GB droplet
- Image SHAs provide audit trail
- GitHub Actions can take over later without changing the deploy flow

**Negative:**
- Requires GHCR access setup (PAT, login on droplet)
- First-time image push is slow

### Alternatives Considered

1. **Build on droplet**: rejected — `next build` + `go build` push memory limit
2. **`docker save | docker load` over SSH**: rejected — works for one-off but breaks CI later

---

## ADR-004: Asymbl light-mode brand applied via Tailwind theme + replaced logo component

**Date**: 2026-05-01
**Status**: Accepted

### Context

Multica's UI supports light + dark mode with a generic "asterisk" logo. We need to apply Asymbl's brand identity (Navy/White/Orange palette, Asymbl logo) and lock to light mode by default.

### Decision

- Replace CSS variable values in `globals.css` with Asymbl colors per brand guide §Application: Website
- Replace `MulticaIcon` clip-path component with `AsymblLogo` `<img>` component referencing PNG from brand assets
- Remove dark mode toggle from UI (Tailwind class still configured but never applied)

### Consequences

**Positive:**
- Theme changes are localized — easy to maintain across upstream syncs
- Logo swap is a single component edit
- Brand consistent across web + email + desktop

**Negative:**
- Multica's modified Apache 2.0 prohibits modifying the logo. **We accept this risk for internal use.** Documented separately.
- Some users may want dark mode — accept that tradeoff for v1

### Alternatives Considered

1. **Keep Multica logo**: rejected — defeats the rebrand purpose
2. **Both modes with Asymbl theme**: rejected — added scope, brand guide is light-mode-first
3. **Custom design system from scratch**: rejected — months of work

---

## ADR-005: Email domain restriction via backend middleware patch

**Date**: 2026-05-01
**Status**: Accepted

### Context

Multica has no built-in email allowlist. We need to restrict signup to `@asymbl.com` to prevent accidental external onboarding.

### Decision

Patch `server/internal/handler/auth.go` `SendCode` handler to check email domain against `ALLOWED_EMAIL_DOMAIN` env var; return 403 on mismatch. If env var is unset, behavior matches upstream (no restriction).

### Consequences

**Positive:**
- Minimal surface change — single env var, single handler
- Reversible (unset env var = upstream behavior)
- Easy to test
- Upstream-compatible (we can keep cherry-picking `auth.go` updates)

**Negative:**
- Patch lives in our fork, not upstream — must re-apply after upstream sync if `auth.go` changes shape

### Alternatives Considered

1. **Cloudflare Worker pre-filter**: rejected — adds dependency on CF compute, harder to debug
2. **Frontend-only check**: rejected — bypassed via curl
3. **OAuth-only with Google Workspace asymbl.com**: rejected — adds complexity for v1

---

## ADR-006: zen + codex CLI review gate on all code changes

**Date**: 2026-05-01
**Status**: Accepted

### Context

Stakeholder requirement: "if you make code changes, have it always reviewed by zen and codex CLI."

### Decision

Every PR to Forge passes through both `zen codereview` and `codex review` before merging. Findings at P0/P1 must be addressed; P2 may be deferred with a note.

### Consequences

**Positive:**
- Independent quality bar beyond manual review
- Catches subtle issues (race conditions, security)
- Consistent standard across all engineers + AI contributors

**Negative:**
- Adds 5-15 min to every PR cycle
- Tooling must be available (currently CLI only — no CI integration)

### Alternatives Considered

1. **Single tool review**: rejected — stakeholder explicitly requested both
2. **Manual review only**: rejected — stakeholder requirement
3. **Add to GitHub Actions**: deferred — Phase 2 if useful

---

## ADR-007: Defer remote agent integration (Ben Corpay, Hermes, OpenClaw) to Phase 2

**Date**: 2026-05-01
**Status**: Accepted (revised based on stakeholder direction)

### Context

Initial plan included installing `forge` daemon on Ben Corpay (147.182.194.102) so its OpenClaw agent could be assigned tasks via Forge UI. Stakeholder directed: "we will add ben etc later — i want this to be standalone."

### Decision

v1 ships as a **standalone deployment**. Remote agent daemons (Ben Corpay, other Ben droplets, Jarvis, Casey) are **Phase 2 work** with their own spec.

### Consequences

**Positive:**
- Smaller v1 scope, faster launch
- No coupling to Asymbl's other infra (those droplets keep their existing patterns)
- Clear v1 success criteria (just "Forge works")

**Negative:**
- v1 utility limited to local-Mac agent execution
- Need a Phase 2 effort to realize the full vision

### Alternatives Considered

1. **Include Ben Corpay in v1**: rejected per stakeholder direction
2. **Skip remote daemons entirely**: rejected — needed for the full vision

---

## ADR-008: Modified Apache 2.0 license — internal-only use, logo replaced

**Date**: 2026-05-01
**Status**: Accepted (with acknowledged risk)

### Context

Multica's license is modified Apache 2.0 with two extra restrictions:
1. No commercial SaaS or embedded redistribution without commercial license
2. No modification of logo or copyright notices in frontend, even for internal use

We are **internal-only** (compliant with restriction 1). We **are modifying the logo** (technically violates restriction 2).

### Decision

Proceed with logo replacement for internal use. Document as known risk.

Mitigations:
- Never expose Forge to non-Asymbl users / clients
- Never offer Forge as a service or SaaS to anyone
- Preserve copyright notice text in source files (only the logo image is swapped)
- If Multica's commercial team objects, we re-apply Multica logo immediately and negotiate

### Consequences

**Positive:**
- Asymbl-branded internal tool
- No Multica brand confusion for our team

**Negative:**
- Technically out of compliance with §1.b of Multica's license
- If discovered + disputed, we'd need to either revert logo or buy commercial license

**Mitigation**: Internal scope means low discovery probability; commercial license is buyable if it ever becomes an issue.

### Alternatives Considered

1. **Keep Multica logo**: rejected — undercuts the rebrand purpose
2. **Buy commercial license preemptively**: deferred — overkill for 5-10 internal users; revisit if expanding scope
3. **Build something custom from scratch**: rejected — months of work

---

## ADR-009: Doppler for secrets (vs .env files in repo)

**Date**: 2026-05-01
**Status**: Accepted

### Context

Multica's selfhost flow uses a local `.env` file. We have an org standard: Doppler for all secrets across projects (talent-intelligence, candidate-portal, etc.).

### Decision

Doppler project `forge`, configs `prd` (production droplet) and optionally `dev` (local). All env vars sourced from Doppler at deploy time. `.env` is gitignored and only generated locally for `docker compose`.

### Consequences

**Positive:**
- Consistent with Asymbl org standard
- Audit trail (Doppler logs secret access)
- Easy rotation (change in Doppler, redeploy)

**Negative:**
- Requires `doppler` CLI on droplet (one extra dep)

---

## ADR-010: GitHub Container Registry private (vs Docker Hub or self-hosted)

**Date**: 2026-05-01
**Status**: Accepted

### Context

Need a private registry for our custom Forge images.

### Decision

GHCR private images at `ghcr.io/<org>/forge-web` and `ghcr.io/<org>/forge-backend`.

### Consequences

**Positive:**
- Free for private repos
- Tight integration with GitHub Actions (when we add CI)
- Same auth as repo access (PAT)

**Negative:**
- Requires PAT on droplet for `docker login`

### Alternatives Considered

1. **Docker Hub**: rejected — extra account, paid for private
2. **DO Container Registry**: rejected — adds platform dep, GHCR is better
3. **Self-hosted registry**: rejected — too much ops for this scale
