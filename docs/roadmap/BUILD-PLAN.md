# Multica Agent Platform — Autonomous Build Plan

Dato: 2026-04-11
Start: Mandag 2026-04-14

## Oversigt

Byg Multica fra task management tool til Firtals centrale agent platform.
~30 builds, kørende autonomt via `/build-plan` → `/build` → `/build-eval` → `make check` → Playwright.

## Jespers touchpoints (~2.5 timer total)

### Touchpoint 1: Inden build starter (~2 timer)
1. **Godkend specs** — læs fase 0 output, godkend eller korrigér (30-60 min)
2. **Sæt OAuth credentials op i `.env`:**
   - Slack app + bot token (https://api.slack.com/apps)
   - GitHub OAuth app (https://github.com/settings/developers)
   - Google Cloud OAuth client (https://console.cloud.google.com/apis/credentials)
   - Linear API key (https://linear.app/settings/api)
   - Notion integration token (https://www.notion.so/my-integrations)
   - Tid: 30-60 min
3. **10 eval golden cases** — beskriv 10 agent-interaktioner med forventet output (15 min)
4. **Opret Slack test workspace** (5 min)

### Touchpoint 2: Når alt er bygget (~30 min)
- Review færdigt produkt
- Kør app, test flows, giv feedback

### Mellem builds
- Intet — medmindre et build fejler 3 gange i træk, så flagger Claude dig

---

## Fase 0: Forberedelse (Claude kører autonomt)

### 0a. Deep Codebase Read
Læs de 5 kritiske filer for at forstå reel arkitektur (ikke bare docs):
1. `server/internal/daemon/daemon.go` — task execution flow
2. `server/internal/handler/chat.go` — chat arkitektur
3. `server/internal/daemon/prompt.go` + `execenv/` — prompt builder, context injection
4. `server/internal/realtime/` — WebSocket hub, event model
5. `server/migrations/` — komplet DB schema, alle relationer

### 0b. Architecture Contract
Skriv `ARCHITECTURE-CONTRACT.md` med hard checklist baseret på reel kodeforståelse:
- State management regler (TanStack Query vs Zustand)
- Package boundary regler (core/views/ui)
- Go handler patterns (writeJSON, writeError, router registration)
- WS event requirements
- Optimistic mutation krav
- Migration nummering
- sqlc regenerering

### 0c. Unambiguous Specs (30 builds)
Per build:
- Præcise DB schemas (tabeller, kolonner, typer, foreign keys)
- API endpoints (metode, path, request body, response shape)
- Trade-off beslutninger (ikke "embedding model" men "text-embedding-3-small")
- Reference til eksisterende handler/hook som template
- WS events der skal emettes
- Testbare assertions ("POST /api/v1/agents/X/chat returnerer SSE stream")

### 0d. Cross-feature E2E Tests
10 Playwright tests der alle fejler fra start:
1. Agent uses memory from previous session
2. Skill created in UI is available to agent at execution
3. Scheduled automation triggers and posts output
4. Governance blocks restricted action
5. Agent chat API returns same quality as UI chat
6. Real-time sync across tabs for all new entities
7. Workspace switch clears all new feature state
8. Knowledge base retrieval injects relevant context
9. Eval flags bad agent output
10. Full flow: agent + skills + memory + schedule + governance

---

## Builds (sekventiel)

### Build 0: Direct LLM Runtime (Knowledge Agents)
```
0a: directLLMBackend — ny agent.Backend implementation der kalder LLM direkte (ingen CLI, ingen daemon, ingen git repo).
    Prompt + skills + memory + MCP tools → LLM API → streamed response.
    Bruges til knowledge agents (CFO, CX, Sales, Ops).
    Eksisterende code agents (Claude Code, Codex) er uændrede.

0b: Agent type i DB + UI — "Code Agent" vs "Knowledge Agent".
    Code Agent: spawner CLI i repo (eksisterende flow).
    Knowledge Agent: kalder LLM direkte med tools (nyt flow).
    UI: agent config viser runtime-valg.
```

> CRITICAL: Dette er Build 0 fordi HELE platformen afhænger af det.
> Uden dette kan Multica kun køre code agents i git repos.
> Med dette kan enhver app kalde en CFO/CX/Sales-agent via API.

### Build 1-2: Agent Chat API
```
1a: Chat API endpoint + SSE streaming + session management (bruger BEGGE runtime types)
1b: MCP server (agenter som MCP tools) + agent discovery endpoint
```

### Build 3-4: Skill Marketplace
```
2a: Skill CRUD API + DB schema + workspace skill library
2b: Skill publishing UI + versioning + usage stats
```

### Build 5-7: Persistent Memory
```
3a: Memory DB schema + CRUD API + per-agent/user/workspace memory
3b: Memory injection i agent context (prompt builder integration)
3c: Memory synthesis pipeline (24h consolidering)
```

### Build 8-9: Agent Governance
```
4a: Policy engine + DB schema + action classification
4b: Approval queue UI + kill switch + audit trail
```

### Build 10-11: Scheduled Automations
```
5a: Schedule DB schema + cron trigger + task execution
5b: Schedule management UI + output routing + run history
```

### Build 12-13: Knowledge Engine
```
6a: Document ingestion API + DB schema + chunking + embedding
6b: Vector search + context injection + auto-index
```

### Build 14-16: Evals
```
7a: Eval Layer 1 — format + completeness checks (deterministisk)
7b: Eval Layer 2 — business rules + compliance checks
7c: Eval Layer 3 — LLM-as-judge + eval hooks + dashboard
```

### Build 17-20: MCP Integration Framework
```
8a: MCP Server Registry + DB schema + connection manager (start/stop/health check MCP servers)
8b: MCP credential injection (env vars fra vault/config) + per-agent MCP config (hvilke servers en agent kan bruge)
8c: MCP Gateway UI (settings side: add server → configure credentials → test connection → assign to agents)
8d: Pre-configured MCP templates (one-click setup for Slack, GitHub, Google, Linear, Notion + enhver anden MCP server)
```

> NOTE: Bygger IKKE custom connectors. Bruger eksisterende MCP servers fra community/official.
> Multica er MCP client (bruger andres servers) OG MCP server (andre apps kalder Multica agenter).
> Credentials: API tokens, ikke OAuth apps — enklere setup.

### Build 21-22: Slack Bot
```
9a: Slack bot (listen in channels, thread replies)
9b: Slash commands + agent routing
```

### Build 23-27: Tier 3 Polish + Glass-inspired UX
```
10a: Zero-config auto-setup (login → rolle fra Google profile → skills auto-installed → MCP servers auto-connected. Ingen wizard, bare virker.)
10b: Auto-open artifacts (agent output → åbn som tab i desktop app automatisk)
10c: Usage analytics dashboard + agent ROI
10d: Contextual skill nudges (under arbejde, ikke kun onboarding: "du arbejder med X — vil du bruge Y skill?")
10e: Auth hardening (domain lock) + RBAC + global search
```

---

## Per-build pipeline

```
/build-plan   → verified task breakdown med quality gates
/build        → autonomous execution med smoke tests + placeholder detection
/build-eval   → uafhængig kvalitetsvurdering (stoler ikke på agentens selv-rapport)
make check    → typecheck + unit tests + go tests
playwright    → cross-feature E2E suite (alle 10 tests)

Grøn → commit → næste build
Rød  → fix loop (max 3 iterationer) → flag Jesper hvis stadig rød
```

---

## Estimater

| Metric | Estimat |
|--------|---------|
| Antal builds | ~27 |
| Tokens per build | 2-4M |
| Total tokens | ~55-70M |
| Jespers tid | ~2.5 timer |
| Bygge-tid (wall clock) | Afhænger af token throughput |
| Forventede flags til Jesper | 0-3 (kun ved 3x failure) |

---

## Forudsætninger

- [ ] `make dev` virker (DB + server + frontend kører)
- [ ] `make check` er grøn inden start
- [ ] Playwright E2E suite kører (`pnpm exec playwright test`)
- [ ] OAuth credentials sat op i `.env` (inden build 17)
- [ ] Slack test workspace oprettet (inden build 26)
- [ ] 10 eval golden cases defineret (inden build 14)

---

## Filer der produceres i fase 0

```
docs/roadmap/BUILD-PLAN.md          ← denne fil
docs/roadmap/ARCHITECTURE-CONTRACT.md  ← hard checklist fra kode-read
docs/roadmap/builds/
  build-01a-chat-api.md             ← spec med DB + API + assertions
  build-01b-mcp-server.md
  build-02a-skill-crud.md
  ...
  build-10c-auth-rbac-search.md
e2e/tests/platform-integration.spec.ts  ← 10 cross-feature tests
```

---

## Ledelsesresumé

Vi bygger Multica om fra task management tool til Firtals centrale AI-agent platform.
30 builds kører autonomt med quality gates. Jesper bruger ~2.5 timer total: godkend
specs og credentials i starten, review produktet til sidst. Resten kører uden ham.
Total cost: ~60-80M tokens. Output: en platform hvor enhver app i Firtal kan kalde
enhver agent via API, med skills, memory, governance, og integrations.
