---
project_id: SPEC-2026-05-01-001
project_name: "Forge — Asymbl AI Agent Management Platform"
slug: forge
status: approved
created: 2026-05-01T11:30:00Z
approved: 2026-05-01T08:57:17Z
approved_by: "shivasymbl <sdevinarayanan@asymbl.com>"
started: null
completed: null
expires: 2026-08-01T11:30:00Z
superseded_by: null
tags: [agents, infrastructure, internal-tool, asymbl-fork, multica-fork]
stakeholders: [shivnath@asymbl.com]
worktree:
  branch: plan/forge-asymbl-fork
  base_branch: main
  upstream: github.com/multica-ai/multica
---

# Forge

**Asymbl's internal AI agent management platform — a self-hosted, rebranded fork of Multica.**

Forge gives Asymbl team members a Kanban-style workspace to assign tasks to AI coding agents (Claude Code, Codex, Gemini, OpenClaw, Hermes), watch them execute, and compound reusable skills over time. It's deployed on our own DigitalOcean infrastructure, accessible at `forge.asymbl.app`, and restricted to `@asymbl.com` email sign-ups.

## At a glance

| | |
|---|---|
| **Deployment** | DigitalOcean droplet `s-2vcpu-4gb` ($24/mo, sfo3) |
| **URL** | `https://forge.asymbl.app` (single subdomain, Cloudflare Tunnel) |
| **Auth** | Email OTP via Resend, restricted to `@asymbl.com` |
| **Email FROM** | `forge@asymbl.app` (asymbl.app verified on Resend) |
| **Branding** | Asymbl light-mode (Navy `#032D60`, White, Orange `#DD7001` CTAs) |
| **License posture** | Modified Apache 2.0 — internal use, logo replaced (acknowledged risk) |
| **v1 scope** | Standalone deployment, local-machine daemons only |
| **v2 scope** | Remote agent integration (Ben Corpay, other Ben droplets, Jarvis, Casey) |

## Documents

- [REQUIREMENTS.md](REQUIREMENTS.md) — what we're building and why
- [ARCHITECTURE.md](ARCHITECTURE.md) — how it's wired
- [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) — phased task breakdown
- [DECISIONS.md](DECISIONS.md) — architectural decision records
- [RESEARCH_NOTES.md](RESEARCH_NOTES.md) — verified findings from spike work
- [CHANGELOG.md](CHANGELOG.md) — plan evolution

## Open items requiring user input

1. **GitHub repo for Forge** — need a private repo (e.g. `github.com/asymbl/forge`). I'll fork from `multica-ai/multica` once created.
2. **GHCR org access** — confirm `ghcr.io/asymbl` exists, or fall back to `ghcr.io/shivasymbl`.
3. **Cloudflare account** — confirm `asymbl.app` zone is in the same CF account as existing tunnels.
4. **Resend "Add Domain" for asymbl.app** — user action in Resend dashboard, then we receive DKIM CNAME values.
5. **Code review tooling** — confirm `zen` and `codex` CLI are available locally; will run them on every Forge code change.

## Next step

Run `/claude-spec:approve forge` to lock the plan, then `/claude-spec:implement forge` to begin Phase 1.
