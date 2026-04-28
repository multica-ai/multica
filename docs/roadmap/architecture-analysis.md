# Multica Architecture Analysis

Analyse udført 2026-04-11. Baseline for roadmap-beslutninger.

## Prompt Architecture (Agent Execution)

Multica styrer agent-adfærd via 6 lag. Ingen af dem er egentlig system prompt — Multica
rider ovenpå Claude Code's native mekanismer.

| # | Lag | Kilde | Hvad det er |
|---|-----|-------|-------------|
| 0 | System prompt | Claude Code binær (Anthropic) | Hardcoded, uændret, ikke kontrolleret af Multica |
| 1 | User prompt | `server/internal/daemon/prompt.go` | 3 linjer: rolle + issue ID + "kør CLI". Context-aware (chat vs ticket) |
| 2 | CLAUDE.md | `server/internal/daemon/execenv/runtime_config.go` | Injiceret fil i workdir. CLI-reference, workflow (3 varianter), agent identity, skills, mentions |
| 3 | Issue context | `server/internal/daemon/execenv/context.go` | `.agent_context/issue_context.md` — issue ID, trigger type, skills-liste |
| 4 | Skills | `server/internal/daemon/execenv/context.go` | `.claude/skills/*/SKILL.md` — native skill discovery |
| 5 | Runtime context | Agenten selv via `multica` CLI | Pull-baseret: `multica issue get`, `multica repo checkout`, etc. |
| 6 | Repo CLAUDE.md | Repoet der checkes ud | Claude Code's native adfærd — ikke Multica-styret |

### Key Insight

Lag 5 er reelt en "fattig mands MCP" — agenten kalder CLI via Bash tool, parser JSON stdout.
Hvert kald er et fuldt Bash tool_use roundtrip. En MCP-server ville spare tokens og øge reliability.

### SystemPrompt-feltet bruges ikke

`agent.ExecOptions.SystemPrompt` sendes som tom string i `daemon.go:979`.
Al instruktion sker via CLAUDE.md (fil) og user prompt (3 linjer).

## Platform Architecture

```
Monorepo (pnpm workspaces + Turborepo)
├── server/                    Go backend
│   ├── cmd/server/            HTTP server (Chi router, 173 routes)
│   ├── cmd/multica/           CLI binary
│   ├── cmd/migrate/           DB migrations
│   ├── internal/
│   │   ├── daemon/            Task polling, agent execution, prompt building
│   │   ├── handler/           REST handlers
│   │   ├── realtime/          WebSocket hub
│   │   ├── middleware/        Auth, workspace, CORS
│   │   └── service/           Email (Resend)
│   └── pkg/
│       ├── agent/             Backend interface + 5 implementations
│       └── db/                sqlc generated queries
├── apps/
│   ├── web/                   Next.js 16 (App Router, Turbopack)
│   └── desktop/               Electron (electron-vite)
├── packages/
│   ├── core/                  Headless logic (stores, API client, hooks)
│   ├── ui/                    Atomic components (shadcn/Base UI)
│   └── views/                 Shared pages (zero framework imports)
└── e2e/                       Playwright tests
```

## What's Missing (Roadmap Items)

| Missing | Impact | Addressed by |
|---------|--------|-------------|
| Public API / docs | No third-party integrations possible | Feature 1 |
| MCP server | Agents waste tokens on CLI→Bash→JSON roundtrips | Feature 1 |
| Cloud agent runtimes | Agents only run when local machine is on | Feature 2 |
| Model-agnostic execution | Locked to whatever CLIs are installed locally | Feature 2 |
| Cloud deploy | Self-host only, no managed option | Feature 3 |

## Telemetry

Ingen. Ingen analytics, tracking, phone-home, Sentry. Self-hosted version er fuldt isoleret.
Eneste eksterne kald: `clawhub.ai` (skill marketplace, opt-in) og `copilothub.ai` (CDN for uploads).

## Auth

- Email + verification code (master code `888888` i dev)
- Google OAuth (kræver konfiguration)
- Personal Access Tokens (`mul_*`) for CLI/daemon
- JWT sessions (30 dage)
