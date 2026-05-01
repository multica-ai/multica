# Changelog

## [1.1.0] - 2026-05-01

### Approved

- Spec approved by shivasymbl <sdevinarayanan@asymbl.com> at 2026-05-01T08:57:17Z
- Status: in-review → approved
- Implementation in progress (Phase 1 Task 1.2 underway)
- PR #1 open: https://github.com/shivasymbl/forge/pull/1

## [1.0.0] - 2026-05-01

### Added
- Initial Forge spec created in `docs/spec/active/2026-05-01-forge/`
- README.md, REQUIREMENTS.md, ARCHITECTURE.md, IMPLEMENTATION_PLAN.md, DECISIONS.md, RESEARCH_NOTES.md
- Project ID: SPEC-2026-05-01-001
- Status: in-review

### Research conducted
- doctl, cloudflared, Docker availability verified
- DigitalOcean droplet sizing analysis (recommend `s-2vcpu-4gb` sfo3)
- Existing Asymbl tunnel pattern reviewed (jarvis, casey, ben-* — all use locally-managed cloudflared as systemd)
- Cloudflare Tunnel ingress regex confirmed for single-subdomain routing
- Resend API key validity tested (valid; asymbl.com not yet verified)
- Resend DNS records for asymbl.app captured (DNS Only — grey cloud required)
- Multica self-hosted daemon URL handling confirmed compatible with HTTPS endpoints
- Branding scope counted (333 references across source)
- Multica license terms verified (modified Apache 2.0; logo restriction acknowledged as risk)
- Asymbl Brand Style Guide read and color tokens extracted
- Available Asymbl logo PNG and AI/EPS files inventoried

### Decisions captured
- ADR-001: Self-host on DigitalOcean
- ADR-002: Single subdomain via Cloudflare Tunnel ingress regex
- ADR-003: Build images locally, publish to GHCR
- ADR-004: Asymbl light-mode brand via Tailwind theme + replaced logo component
- ADR-005: Email domain restriction via backend middleware patch
- ADR-006: zen + codex CLI review gate on all code changes
- ADR-007: Defer remote agent integration to Phase 2
- ADR-008: Logo modification — internal-only, acknowledged license risk
- ADR-009: Doppler for secrets
- ADR-010: GitHub Container Registry private

### Stakeholder direction recorded
- Project name: **Forge** (URL: `forge.asymbl.app`)
- URL pattern: single subdomain, path-based routing
- Desktop app: full rebrand
- FROM email: `forge@asymbl.app` (asymbl.app domain on Resend, not asymbl.com)
- Light mode default per Asymbl brand guide
- Standalone deployment for v1 (no Ben Corpay)
- All code changes reviewed by zen + codex CLI before merge

### Protocol deviations
- `/claude-spec:plan` worktree creation skipped (planned in-place on `plan/forge-asymbl-fork` branch in current dir) — user was actively iterating; new-terminal spawn would have broken flow

### Open items requiring user input
- GitHub repo for Forge (suggested: `github.com/asymbl/forge`)
- GHCR org access
- Cloudflare account confirmation for `asymbl.app` zone
- Resend "Add Domain" for `asymbl.app` (need DKIM CNAME values back)
- Confirmation that `zen` and `codex` CLI are installed locally
