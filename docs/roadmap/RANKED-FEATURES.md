# Multica Feature Roadmap — Agent Platform for Firtal

Multica er Firtals **AI Infrastructure layer** — det sted hvor alle agenter bor, uanset
hvor de bruges fra. Enhver app, Slack bot, eller workflow der skal bruge en agent (CFO,
CX, Code Review) kalder Multica via API/MCP.

Baseline: codebase-analyse 2026-04-11 + Glass/Ramp-artikel + Firtal Platform Architecture (Layer 3).

## Arkitektur-ramme

```
┌─────────────────────────────────────────────────────────┐
│                    Multica (Agent Platform)              │
│                                                         │
│  CFO Agent      CX Agent      Code Review Agent   ...   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │ Skills   │  │ Skills   │  │ Skills   │              │
│  │ Memory   │  │ Memory   │  │ Memory   │              │
│  │ Config   │  │ Config   │  │ Config   │              │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘              │
│       │              │              │                    │
│  ═════╪══════════════╪══════════════╪═══════════════     │
│       │    Agent Chat API / MCP Gateway                  │
│  ═════╪══════════════╪══════════════╪═══════════════     │
│       │              │              │                    │
│  ┌────┴──────────────┴──────────────┴────────────┐      │
│  │  Platform Integration Layer                    │      │
│  │  Central Auth │ Credentials Vault │ Event Bus  │      │
│  │  PII Guard │ Permission Engine │ Registry API  │      │
│  └───────────────────────────────────────────────┘      │
└─────────────────────────────────────────────────────────┘
       │              │              │           │
  ┌────▼────┐   ┌─────▼────┐  ┌─────▼────┐ ┌────▼────┐
  │Multica  │   │  Slack   │  │ Anden   │ │ Cron/  │
  │   UI    │   │   Bot    │  │  App    │ │ Event  │
  └─────────┘   └──────────┘  └─────────┘ └─────────┘
```

## Scoring

Hver feature scores på 3 dimensioner (1-5):

- **Adoption** — hvor mange i virksomheden bruger det dagligt?
- **Stickiness** — hvor svært er det at skifte væk efter 30 dage?
- **Multiplier** — skalerer værdien med antal brugere? (netværkseffekt)

**Impact = Adoption × Stickiness × Multiplier** (max 125)

---

## Tier 0 — Platform Foundation (gør Multica til infrastruktur, ikke bare et tool)

### #0a. Agent Chat API — "Talk to any agent"
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 5 | Enhver app/service i virksomheden kan bruge agenter |
| Stickiness | 5 | Alle apps afhænger af API'et — kan ikke fjernes |
| Multiplier | 5 | Jo flere consumers, jo mere værd er platformen |
| **Impact** | **125** | |

**Har i dag:** Chat i UI (sessions, messages, streaming). REST API eksisterer men kun til Multica's eget UI.
**Scope:**
- `POST /api/v1/agents/{agent_id}/chat` — headless chat endpoint med SSE streaming
- Session management (genoptag samtale, eller ny)
- Context injection (caller kan sende ekstra context med)
- Agent discovery endpoint: `GET /api/v1/agents` — "hvilke agenter findes?"
- Rate limiting + usage tracking per caller/API key
- MCP server der eksponerer agenter som tools (enhver MCP-klient kan kalde CFO-agenten)

**Effort:** 2-3 uger (bygger på eksisterende chat + API infra)
**Dependency:** Cloud Deploy (serveren skal være tilgængelig)

---

### #0b. Platform Integration Layer
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 3 | Usynligt for brugere, men fundamentet for alt |
| Stickiness | 5 | Dybt integreret i platformens services |
| Multiplier | 5 | Enabler sikker, compliant agent-brug |
| **Impact** | **75** | |

**Har i dag:** Egen auth, egen user-tabel, ingen vault, ingen event bus integration.
**Scope:**
- **Central Auth integration** — delegér auth til platformens IdP i stedet for egen Google OAuth. Multica accepterer platform-tokens.
- **Credentials Vault** — integrations-credentials (Slack, GitHub tokens) hentes fra Firtals vault, ikke gemt i Multica DB
- **Event Bus publishing** — agent events (task.started, task.completed, agent.responded) publiceres til platformens Event Bus
- **PII Guard** — al agent output passerer PII-filter inden det returneres til caller. Forhindrer CPR, kreditkort, passwords i output.
- **Permission Engine** — agent-permissions (hvem må kalde hvilken agent, hvilke tools må agenten bruge) hentes fra platformens permission engine

**Effort:** 3-4 uger (afhænger af hvor modne platform services er)
**Dependency:** Platform services skal eksistere (Central Auth, Vault, Event Bus, PII Guard)
**Note:** Kan bygges inkrementelt — start med Event Bus + PII Guard, tilføj auth + vault senere

---

### #0c. Agent Governance
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 3 | Admins konfigurerer, alle beskyttes |
| Stickiness | 5 | Compliance-krav forsvinder ikke |
| Multiplier | 4 | Nødvendig for at agenter kan handle autonomt |
| **Impact** | **60** | |

**Har i dag:** Intet. Agenter kører frit uden guardrails.
**Scope:**
- Policy engine: "CFO-agent må ikke godkende >50.000 DKK uden human approval"
- Action classification: read-only vs. mutating vs. destructive
- Human-in-the-loop approval queue for restricted actions
- Audit trail: hvem bad agenten om hvad, hvad gjorde den, hvad var output
- Budget caps per agent/user/workspace (token + API cost)
- Kill switch: stop en agent mid-execution

**Effort:** 2-3 uger
**Dependency:** Bedre med Permission Engine (#0b), men kan starte standalone

---

## Tier 1 — Kernekapabiliteter (gør agenterne nyttige)

### #1. Persistent Agent Memory
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 5 | Enhver bruger/app mærker forskel med det samme |
| Stickiness | 4 | Memory der kender virksomheden er uerstattelig |
| Multiplier | 4 | Organisatorisk viden akkumuleres |
| **Impact** | **80** | |

**Har i dag:** Nul memory. Agenter starter blank hver gang. Kun issue context (.agent_context/).
**Scope:**
- Per-agent memory (akkumuleret viden fra alle samtaler)
- Per-user memory (præferencer, rolle, team, projekter)
- Per-workspace memory (conventions, terminology, org chart)
- Auto-build fra integrations + platform Registry API / Ontology API
- 24h synthesis pipeline (consolidér, ryd op, opdatér)
- Memory injection i agent context (lag 2-3 i prompt-arkitekturen)
- Memory API: andre services kan skrive til en agents memory

**Effort:** 2-3 uger
**Dependency:** Bedre med integrations, men kan starte uden
**Platform integration:** Trækker fra Registry API + Ontology API for virksomhedsdata

---

### #2. Skill Marketplace + Sharing ("Dojo")
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 4 | Power users skaber, alle forbruger |
| Stickiness | 5 | 50+ custom skills = umuligt at starte forfra |
| Multiplier | 5 | Én persons gennembrud → hele teamets baseline |
| **Impact** | **100** | |

**Har i dag:** Skills eksisterer (SKILL.md filer, agent skill assignment). Ingen sharing, ingen discovery, ingen versioning UI.
**Scope:**
- Workspace skill library (browse, search, install)
- Skill publishing (fra agent → workspace → org-wide)
- Git-backed versioning med diff-view
- Usage stats per skill (hvem bruger hvad, success rate)
- Kategori/tag-system
- Skill API: andre apps kan query "hvilke skills har CFO-agenten?"

**Effort:** 2-3 uger
**Dependency:** Ingen
**Platform integration:** Skills registreres i App Registry for cross-platform discovery

---

### #3. Integration Framework (Slack, GitHub, Google Workspace, Linear, Notion)
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 5 | Agenter der ikke kan læse Slack/GitHub/Calendar er halvblinde |
| Stickiness | 5 | Jo flere integrations, jo sværere at migrere |
| Multiplier | 4 | Hver integration gør platformen mere værdifuld for alle |
| **Impact** | **100** | |

**Har i dag:** Google OAuth (login only). Nul tool-integrations. Agenter kan kun se issues + repo.
**Scope:** OAuth connector framework + 5 launch-integrations:
1. **Slack** — læs/skriv beskeder, post agent-output, Slack commands
2. **GitHub** — PRs, issues, reviews, webhooks
3. **Google Workspace** — Calendar, Drive, Gmail (læs context, ikke send)
4. **Linear** — issue sync (bi-directional)
5. **Notion** — læs docs som kontekst

**Effort:** 4-5 uger (framework 1 uge, per connector 3-5 dage)
**Dependency:** Credentials Vault (#0b) for sikker token-opbevaring
**Platform integration:** Credentials hentes fra Credentials Vault. Connectors registreres som MCP tools via MCP Gateway.

---

### #4. Scheduled Automations + Headless Mode
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 4 | Ops, finance, CX — enhver med gentagne tasks |
| Stickiness | 5 | 20 cron jobs kørende = kan ikke slukke |
| Multiplier | 3 | Primært individuel, men output kan deles |
| **Impact** | **60** | |

**Har i dag:** Daemon kører tasks on-demand. Ingen scheduling, ingen cron, ingen headless approval.
**Scope:**
- Cron-baseret task scheduling i UI (daglig/ugentlig/custom)
- Headless execution med approval queue (godkend fra telefon/Slack)
- Output routing: post til Slack-kanal, email, issue comment, eller Event Bus
- Schedule management UI (list, pause, edit, delete, run history)
- External trigger: andre apps kan trigge en scheduled task via API

**Effort:** 2 uger
**Dependency:** Agent Chat API (#0a) for execution, Event Bus (#0b) for output routing

---

## Tier 2 — Differentiering (gør Multica bedre end alternativer)

### #5. Slack-native Agent Assistants
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 4 | Folk der aldrig åbner Multica UI bruger agenten via Slack |
| Stickiness | 4 | Team-vaner bygget op omkring Slack bot er svære at bryde |
| Multiplier | 4 | Hele kanalen ser agent-output |
| **Impact** | **64** | |

**Har i dag:** Intet.
**Scope:**
- Slack bot der lytter i kanaler (configurable per workspace)
- Brug fuldt agent-setup (skills, memory, integrations)
- Tråd-baserede svar (ikke spam i kanal)
- Slash commands: `/multica ask [agent]`, `/multica run [skill]`

**Effort:** 1-2 uger
**Dependency:** Integration Framework (#3) for Slack, Agent Chat API (#0a)

---

### #6. Evals + Anti-hallucination
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 3 | Usynligt for brugere, men øger trust |
| Stickiness | 4 | Eval-data akkumuleres, baseline etableres |
| Multiplier | 4 | Platform-wide kvalitetsforbedring |
| **Impact** | **48** | |

**Har i dag:** Intet. Ingen output-validering, ingen eval framework.
**Scope:**
- **Anti-hallucination:** Fakta-check mod kendte data (Registry API, Ontology API). Flag usikre udsagn.
- **Eval Layer 1:** Automatisk — format, completeness, tool-use korrekthed
- **Eval Layer 2:** Regelbaseret — business rules, compliance checks
- **Eval Layer 3:** LLM-as-judge — kvalitet, relevans, tone
- **Eval Hooks:** Triggers på eval-failure (alert, retry, escalate til human)
- Eval dashboard med trends over tid

**Effort:** 3-4 uger
**Dependency:** Agent Chat API (#0a), Registry API fra platform
**Platform integration:** Maps direkte til Evals (3 layers), Eval Hooks, Anti-hallucination i platform-arkitekturen

---

### #7. Knowledge Engine + Document Intelligence
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 4 | Agenter der kan læse virksomhedens docs er dramatisk bedre |
| Stickiness | 3 | Knowledge base vokser over tid |
| Multiplier | 4 | Alle agenter deler videnbase |
| **Impact** | **48** | |

**Har i dag:** Intet. Ingen RAG, ingen doc parsing.
**Scope:**
- Document ingestion pipeline (PDF, DOCX, Notion exports, Confluence)
- Chunking + embedding + vector search (pgvector allerede i DB)
- Knowledge base per workspace (shared videnbase alle agenter kan søge i)
- Auto-index fra integrations (Notion pages, Google Drive docs, GitHub repos)
- Context injection: relevant docs hentes automatisk baseret på brugerens spørgsmål

**Effort:** 3-4 uger
**Dependency:** Integration Framework (#3) for auto-indexing
**Platform integration:** Maps til Document Intelligence + Knowledge Engine. Bruger Unstructured Ingestion for doc parsing.

---

### #8. Onboarding Wizard + Skill Recommender ("Sensei")
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 5 | Reducerer time-to-value fra dage til minutter |
| Stickiness | 2 | Engangs-oplevelse |
| Multiplier | 3 | Bedre onboarding = højere org-adoption |
| **Impact** | **30** | |

**Har i dag:** Intet. Ny bruger ser tom workspace.
**Scope:**
- Guided setup: connect integrations → pick role → install recommended skills
- Rolle-baseret skill recommendations (sælger vs. ingeniør vs. CX)
- "Get started" tasks der viser agent-capabilities
- Progressbar: "Du har sat 3/7 ting op"

**Effort:** 1-2 uger
**Dependency:** Integration Framework (#3), Skill Marketplace (#2)

---

### #9. Usage Analytics + Agent ROI Dashboard
| Dimension | Score | Begrundelse |
|-----------|-------|-------------|
| Adoption | 3 | Primært managers og champions |
| Stickiness | 3 | Data akkumuleres over tid |
| Multiplier | 3 | Synlighed → budget → mere adoption |
| **Impact** | **27** | |

**Har i dag:** Grundlæggende usage tracking (tokens, tasks). Ingen dashboards, ingen ROI.
**Scope:**
- Dashboard: tasks kørt, tokens brugt, tid sparet (estimate)
- Per-team og per-agent breakdown
- Skill usage stats (mest populære, højest success rate)
- Export til CSV/PDF for ledelsesrapporter
- "Time saved" estimat baseret på task complexity
- LLM Gateway metrics: cost per agent, cost per team, model breakdown

**Effort:** 1-2 uger
**Dependency:** Ingen
**Platform integration:** Feeds data til platformens Observability + Monitoring

---

## Tier 3 — Polish

### #10. Granular RBAC + Audit Log
| Impact | **24** | Adoption 2 × Stickiness 4 × Multiplier 3 |

**Scope:** Per-project roller, agent-permissions, audit log med compliance-export.
**Effort:** 2-3 uger
**Platform integration:** Delegér til Permission Engine. Audit log feeds til platformens Observability.

### #11. Split-pane Workspace + Inline Rendering
| Impact | **24** | Adoption 3 × Stickiness 4 × Multiplier 2 |

**Scope:** Horizontal/vertical split, drag tabs, inline rendering (MD, HTML, CSV, code).
**Effort:** 2-3 uger

### #12. Global Cross-entity Search
| Impact | **18** | Adoption 3 × Stickiness 3 × Multiplier 2 |

**Scope:** Søg på tværs af issues, comments, chat, skills, knowledge base. Full-text + semantic.
**Effort:** 1-2 uger

### #13. Self-healing Integrations
| Impact | **16** | Adoption 4 × Stickiness 2 × Multiplier 2 |

**Scope:** Health monitoring per integration. Auto-reconnect. Fejl-notification.
**Effort:** 1 uge (efter #3)

### #14. Mobile PWA / Approval App
| Impact | **15** | Adoption 3 × Stickiness 3 × Multiplier ~2 |

**Scope:** Godkend headless tasks fra telefon. Se notifikationer. Lightweight chat.
**Effort:** 1-2 uger

### #15. Agent Templates + Org-wide Agent Config
| Impact | **12** | Adoption 2 × Stickiness 3 × Multiplier 2 |

**Scope:** Præ-konfigurerede agent-profiler. Admin deployer til hele org.
**Effort:** 1 uge

### #16. Billing & Usage Limits
| Impact | **10** | Adoption 2 × Stickiness 5 × Multiplier 1 |

**Scope:** Per-workspace usage caps, per-user budgets, cost alerts.
**Effort:** 2-3 uger

---

## Eksisterende Roadmap (allerede planlagt)

| Feature | Effort | Status i platform-kontekst |
|---------|--------|---------------------------|
| Cloud Deploy (Fly.io) | 3-5 dage | Forudsætning for Agent Chat API. Kør først. |
| Public API + MCP Server | 2-3 uger | **Erstattet af #0a (Agent Chat API)** — bredere scope |
| Cloud Agent Runtimes | 2-3 uger | Uændret. Maps til Agent Runtime i platform-arkitekturen. |

Auth Hardening (domain-lock): <1 dag task, kør i Wave 1.

---

## Anbefalet Execution Order

```
Wave 1 (uge 1-3):    Cloud Deploy → Agent Chat API (#0a) → Auth Hardening
Wave 2 (uge 3-7):    Skill Marketplace (#2) → Integration Framework (#3)
Wave 3 (uge 7-10):   Persistent Memory (#1) → Agent Governance (#0c) → Cloud Runtimes
Wave 4 (uge 10-13):  Scheduled Automations (#4) → Slack Bot (#5) → Platform Integration (#0b)
Wave 5 (uge 13-17):  Evals (#6) → Knowledge Engine (#7) → Onboarding (#8)
Wave 6 (uge 17-20):  Dashboard (#9) → RBAC (#10) → Search (#12) → Self-healing (#13)
```

Kritisk path: Cloud Deploy → Agent Chat API → alt andet.

Platform Integration (#0b) er placeret i Wave 4 fordi den afhænger af at platform services
(Central Auth, Vault, Event Bus) er modne nok. Kan rykkes frem hvis de er klar.

---

## Multica vs. Platform Architecture (Layer 3) — Coverage Map

| Platform Service (AI Infrastructure) | Multica Feature | Wave |
|--------------------------------------|-----------------|------|
| LLM Gateway | Delvist via Cloud Runtimes (model routing) | 3 |
| Agent Runtime | ✅ Eksisterer (daemon + 5 backends) | — |
| Agent Profiler | Usage Analytics (#9) + Eval metrics | 6 |
| Skills Loader | ✅ Eksisterer + Skill Marketplace (#2) | 2 |
| MCP Gateway | Agent Chat API (#0a) eksponerer agenter som MCP tools | 1 |
| Anti-hallucination | Evals + Anti-hallucination (#6) | 5 |
| Evals (3 layers) | Evals + Anti-hallucination (#6) | 5 |
| Eval Hooks | Evals + Anti-hallucination (#6) | 5 |
| Document Intelligence | Knowledge Engine (#7) | 5 |
| Agent Memory | Persistent Agent Memory (#1) | 3 |
| Knowledge Engine | Knowledge Engine (#7) | 5 |
| Agent Governance | Agent Governance (#0c) | 3 |

**Platform-integration med andre kolonner:**

| Platform Service (andre kolonner) | Multica integration | Wave |
|-----------------------------------|---------------------|------|
| Central Auth | Platform Integration Layer (#0b) | 4 |
| Permission Engine | Platform Integration Layer (#0b) + RBAC (#10) | 4/6 |
| Credentials Vault | Platform Integration Layer (#0b) | 4 |
| PII Guard | Platform Integration Layer (#0b) | 4 |
| Event Bus | Platform Integration Layer (#0b) | 4 |
| Registry API | Memory (#1) + Knowledge Engine (#7) | 3/5 |
| Ontology API | Memory (#1) + Evals (#6) | 3/5 |
| Task API | Scheduled Automations (#4) | 4 |
| Observability | Usage Analytics (#9) | 6 |
| Unstructured Ingestion | Knowledge Engine (#7) | 5 |

---

## Ledelsesresumé

Multica skifter fra "AI task management tool" til **Firtals centrale agent-platform** — det
sted hvor alle virksomhedens AI-agenter bor, uanset om de bruges fra Multica's UI, Slack,
eller en anden app via API.

**Hvad vi bygger:** 16 features i 6 bølger over ~20 uger. Den vigtigste nye feature er
**Agent Chat API** — et endpoint der lader enhver app "tale med en agent." Når det er på
plads, er CFO-agenten tilgængelig overalt: i Multica UI, via Slack bot, fra et dashboard,
eller fra en kollegas app. Derudover bygger vi skill sharing (andres gennembrud bliver
dine), persistent memory (agenten husker), og governance (agenten overholder reglerne).

**Hvorfor:** Multica dækker i dag 2.5 ud af 12 AI Infrastructure services i Firtals
platform-arkitektur. Roadmappen lukker alle 12 huller og integrerer med 10 platform
services fra andre kolonner (auth, vault, event bus, PII guard, registry, ontology,
observability, task API, permission engine, unstructured ingestion).

**Hvad det koster:** ~20 uger udvikling (~1 senior dev full-time). Infrastruktur: Fly.io
~$50/md, S3 ~$10/md, API-integrationer = gratis. Total runtime: <$100/md.

**Forventet outcome:** Én platform der ejer alle AI-agenter i virksomheden. Andre apps
kalder agenter via API i stedet for at bygge egne. Skills deles på tværs af teams.
Memory akkumulerer organisatorisk viden. Governance sikrer compliance. Lock-in er total:
50+ skills, memory, integrations, cron jobs, og afhængigheder fra andre apps.
