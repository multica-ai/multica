---
document_type: requirements
project_id: SPEC-2026-05-01-001
version: 1.0.0
last_updated: 2026-05-01T11:30:00Z
status: in-review
---

# Forge — Product Requirements Document

## Executive Summary

Asymbl needs an internal platform to coordinate AI coding agents as teammates — assigning issues, tracking progress, and accumulating reusable skills across engagements. Multica (open-source, github.com/multica-ai/multica) provides exactly this functionality but only as a cloud-hosted SaaS or self-hosted deployment of *their* brand. Forge is a self-hosted, rebranded fork deployed on Asymbl's own infrastructure, restricted to Asymbl team members, and themed to Asymbl's brand identity.

The v1 deliverable is a **standalone deployment** of Forge on a DigitalOcean droplet at `forge.asymbl.app`. Asymbl team members install the local CLI daemon on their own laptops, which auto-detects available coding agents (Claude Code, Codex, Gemini), and can assign work via the Forge web UI. Remote agent integration (Ben Corpay, Jarvis, Casey droplets) is explicitly deferred to Phase 2.

## Problem Statement

### The Problem

Asymbl team members work with multiple AI coding agents (Claude Code, Codex, OpenClaw, Hermes) across many projects, but have no shared coordination layer. Tasks are assigned via Slack threads, terminal sessions, or memory; agent skills don't compound across people; and there's no visibility into what agents are working on.

### Impact

- **Coordination overhead**: Every Asymbl engineer reinvents how to delegate to agents
- **Skill loss**: Agents learn within a session, then forget
- **No audit trail**: We can't show clients what AI did vs what humans did
- **Tool fragmentation**: 7+ existing OpenClaw/Hermes droplets but no unified board

### Current State

- Slack `#ai-ops` channels for ad-hoc agent coordination
- Hindsight Cloud + Mempalace for memory persistence (per-agent, not org-wide)
- Paperclip Mindcat as a reference architecture (proprietary, complex, $291/day burn)
- No shared "issue board" for agent work

## Goals and Success Criteria

### Primary Goal

Stand up a branded, self-hosted Forge instance that any `@asymbl.com` user can sign up to, create a workspace, and assign issues to a locally-detected AI agent — with the entire experience visually indistinguishable from a native Asymbl product.

### Success Metrics (v1)

| Metric | Target | Measurement |
|---|---|---|
| `forge.asymbl.app` reachable with valid TLS | 100% uptime over launch week | Cloudflare analytics |
| No "Multica" string visible in UI/emails/desktop app | Zero references | Manual QA + grep audit |
| Sign-up restricted to `@asymbl.com` | Reject all non-asymbl.com attempts | Test 5 non-asymbl emails |
| First Asymbl user creates workspace + assigns first issue | < 10 min from sign-up | Manual user test |
| Local daemon detects ≥3 agent providers | Claude Code, Codex, Gemini all online | `multica runtime list` |
| Email OTP delivered via Resend | < 30s p95 | Resend dashboard |

### Non-Goals (Explicit Exclusions for v1)

- Remote agent daemons on Ben Corpay or other Asymbl droplets — **Phase 2**
- Google OAuth — **Phase 2**
- Hermes-specific integration patterns — **Phase 2**
- Client-facing use (offering Forge to non-Asymbl users)
- White-labeling per-client (each client gets their own instance) — **Future**
- Custom email templates beyond brand polish (e.g., per-workspace templates)
- Mobile apps (web responsive is sufficient)

## User Analysis

### Primary Users

**Asymbl Engineers** (5-10 users in v1)

- **Who**: Internal Asymbl team, all on `@asymbl.com` email
- **Needs**: Assign coding tasks to AI agents, track progress without Slack overhead, see what agents have completed
- **Context**: Daily, during active engagements

### User Stories

1. As an Asymbl engineer, I want to sign in with my `@asymbl.com` email so I can use Forge without a separate password.
2. As an Asymbl engineer, I want to create a workspace for a client engagement so issues are scoped per project.
3. As an Asymbl engineer, I want to detect my local AI agents automatically so I don't manually configure runtimes.
4. As an Asymbl engineer, I want to assign an issue to a specific agent so it executes autonomously.
5. As an Asymbl engineer, I want to watch real-time progress so I can intervene when an agent gets stuck.
6. As an Asymbl admin, I want to manage workspace members so only my team can see our work.
7. As Shiv (owner), I want non-`@asymbl.com` emails rejected at sign-up so we don't accidentally onboard outsiders.

## Functional Requirements

### Must Have (P0) — v1 Launch

| ID | Requirement | Acceptance Criteria |
|---|---|---|
| FR-001 | Deploy Forge backend, frontend, and Postgres on a single DigitalOcean droplet | `docker compose ps` shows all 3 services healthy |
| FR-002 | Expose Forge at `forge.asymbl.app` via Cloudflare Tunnel | `curl https://forge.asymbl.app` returns 200 with valid TLS |
| FR-003 | Single-subdomain reverse proxy: `/api/*`, `/auth/*`, `/uploads/*`, `/ws` → backend (8080); everything else → frontend (3000) | Tunnel ingress rules verified, WebSocket connection works |
| FR-004 | Sign-up restricted to `@asymbl.com` email domain | Server rejects `/auth/send-code` for non-asymbl.com with 403 |
| FR-005 | Email OTP delivered via Resend, FROM `forge@asymbl.app` | Test email arrives within 30s, From header correct |
| FR-006 | Replace all "Multica" branding with "Forge" / Asymbl branding | Zero references in user-visible strings (UI, emails, desktop app, OG meta) |
| FR-007 | Use Asymbl light-mode color palette (Navy `#032D60` primary, White bg, Orange `#DD7001` CTAs) | Visual QA against brand guide |
| FR-008 | Replace Multica logo with Asymbl logo across web app | Login page, sidebar, favicon, OG image, email headers |
| FR-009 | Local CLI daemon (`forge` CLI, renamed from `multica`) auto-detects Claude Code, Codex, Gemini on developer's machine | `forge runtime list` shows all 3 |
| FR-010 | Workspace + issue + agent assignment + execution flow works end-to-end | One-shot test: sign up → workspace → issue → assign → execute → done |
| FR-011 | Mac desktop app rebuilt with Asymbl branding | App name "Forge", bundle ID `com.asymbl.forge`, Asymbl icon |
| FR-012 | Owner/admin/member roles enforced (members can't create agents/runtimes) | Test with non-admin user: agent creation fails with 403 |

### Should Have (P1) — v1.1 Polish

| ID | Requirement | Acceptance Criteria |
|---|---|---|
| FR-101 | Postgres daily backup to DO Spaces, 30-day retention | Cron runs nightly, restore tested |
| FR-102 | Cloudflare Tunnel running as systemd service for restart-on-crash | `systemctl status cloudflared` enabled |
| FR-103 | Email templates polished with Asymbl brand (logo, button color, typography hints) | Render in Litmus or screenshot QA |
| FR-104 | `.dmg` installer for Mac desktop app, signed and distributable internally | Drag-to-Applications works |
| FR-105 | Sentry or PostHog error tracking wired up (post-deployment) | Test error appears in dashboard |

### Nice to Have (P2) — Phase 2

| ID | Requirement | Acceptance Criteria |
|---|---|---|
| FR-201 | Remote daemon installation on Ben Corpay droplet (147.182.194.102) | Daemon visible as remote runtime in Forge UI |
| FR-202 | Google OAuth as alternative to email OTP | "Sign in with Google" button works |
| FR-203 | OpenClaw + Hermes detected as runtimes on remote droplets | Visible in runtime list with `provider: openclaw` |
| FR-204 | GitHub Actions CI: build + push image to GHCR on tag | Tag triggers build, droplet pulls + restarts |

## Non-Functional Requirements

### Performance

- Page load < 2s on first visit (cold cache)
- Issue creation latency < 500ms
- WebSocket reconnect < 5s after network blip
- Daemon registration heartbeat every 15s (Multica default)

### Security

- TLS 1.3 enforced via Cloudflare Tunnel (CF edge → origin localhost over private network)
- JWT secret 32+ chars, generated once via `openssl rand -hex 32`, stored in `.env`
- `APP_ENV=production` set to disable dev master code (`888888`)
- Database accessible only from droplet localhost (no public Postgres port)
- Resend API key in `.env`, not in source
- All secrets managed via Doppler (project: `forge`, configs: `prd`)
- No secrets committed to repo; pre-commit hook runs gitleaks

### Compliance / Licensing

- Multica's modified Apache 2.0 prohibits modifying the logo. **We are doing so anyway** for internal use only. Risk acknowledged by stakeholder. Mitigation: never offer Forge as a service to clients (would require commercial license).
- All Asymbl-owned code in the fork is licensed internally; external contributions not accepted in v1.

### Scalability

- Designed for 5-10 users, ~50 issues/day. Vertical scale path: bump droplet to `s-4vcpu-8gb` ($48/mo) if needed.
- Horizontal scale (multi-droplet, managed Postgres) is **out of scope** for v1.

### Reliability

- Target uptime: 99% (single-droplet, no HA in v1) — acceptable for internal tool
- Recovery: `docker compose up -d` rebuilds the stack from images; Postgres restored from backup if needed
- Rollback: previous Docker image tag via `MULTICA_IMAGE_TAG` env var

### Maintainability

- Single source repo (forked from multica-ai/multica)
- Asymbl-specific changes isolated to clearly-named files (e.g., `*-asymbl.go`) where possible
- Brand strings centralized in a config module (TypeScript constants + Go config)
- Upstream sync strategy: cherry-pick security fixes monthly; rebrand patches kept in `branding/` folder
- **Code review gate**: all changes pass `zen` and `codex` CLI review before merge

## Technical Constraints

- Must use Multica's existing Postgres 17 + pgvector schema (don't fork the data model)
- Must keep Multica's CLI/daemon protocol intact (so we can sync upstream daemon updates)
- DigitalOcean droplet (existing infra pattern across Asymbl)
- Cloudflare Tunnel (existing pattern across 7 other Asymbl tunnels)
- Doppler for secrets (existing org standard)
- GitHub for source (existing org standard)

## Dependencies

### Internal Dependencies

- DigitalOcean account (`Asymbl Intelligence` team, sdevinarayanan@asymbl.com authenticated)
- Cloudflare account containing `asymbl.app` zone
- Resend account with API key `re_96wmDVdD_4DwMMYAtTG5VdF9J4H7LiYFJ`
- Doppler workspace for secrets
- GitHub org or user account for repo + GHCR

### External Dependencies

- `github.com/multica-ai/multica` upstream (we fork, don't push back)
- `pgvector/pgvector:pg17` Docker image
- Multica daemon protocol (we maintain compatibility)

## Risks and Mitigations

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Multica license enforcement (logo modification) | Low | Medium | Internal-only; no client-facing use; lawyer review if expanding scope |
| Upstream Multica breaks our patches | Medium | Medium | Cherry-pick selectively; keep brand patches in dedicated folder |
| Resend domain verification fails | Low | High (no email = no auth) | Verify DNS records ahead of cutover; fallback: dev master code in private mode |
| Cloudflare tunnel mis-routes WebSocket | Medium | High (real-time UI breaks) | Test `/ws` route first; fall back to nginx if needed |
| Desktop app codesigning blocks distribution | Medium | Medium (Mac warns "unidentified developer") | Use existing Asymbl Apple Developer account; or document right-click-open workaround |
| Asymbl team rejects branding/UX | Low | Low | Quick iteration; brand guide is authoritative |
| Droplet OOM during Next.js build | Low | Medium | Build images locally + push to GHCR; droplet only pulls (no in-place builds) |
| GHCR private repo access misconfig | Low | Low | Test pull from droplet during Phase 1 |

## Open Questions

- [ ] Which GitHub org owns the Forge repo? (Asymbl org or shivasymbl personal)
- [ ] Is there an existing Asymbl Apple Developer account for desktop app signing?
- [ ] Who else gets `owner` role at launch? (Just Shiv, or also Justin/team leads?)
- [ ] Should we auto-create a default "Asymbl Internal" workspace on first deploy?
- [ ] Doppler project name: `forge` or `asymbl-forge`?

## Appendix

### Glossary

| Term | Definition |
|---|---|
| **Forge** | Asymbl's rebranded fork of Multica |
| **Multica** | Upstream open-source platform we forked |
| **Runtime** | A compute environment that executes agent tasks (local Mac, remote droplet) |
| **Daemon** | The `forge` CLI process running on a runtime, polling for tasks |
| **Provider** | An agent type (claude, codex, gemini, openclaw, hermes) |
| **Workspace** | Tenancy boundary in Forge — each Asymbl engagement gets a workspace |

### References

- Asymbl Brand Style Guide: `/Users/sdevinarayanan/Downloads/Asymbl Logo & Favicon/Asymbl Brand Style Guide (1).md`
- Multica self-hosting docs: `SELF_HOSTING.md`, `SELF_HOSTING_ADVANCED.md`
- Multica license: `LICENSE` (modified Apache 2.0)
- Resend Cloudflare DNS guide: https://resend.com/docs/dashboard/domains/cloudflare
