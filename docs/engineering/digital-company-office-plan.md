# Digital company office: development plan

Implements [digital-company-office.md](digital-company-office.md). Read the spec first;
this plan says in what order to build it, what each step touches, and how to know it
is done. Nothing here is implemented yet.

## How to read this plan

- A **workstream** (WS) is a theme with its own acceptance test. A **step** is one
  atomic conventional commit that leaves the app working. Several steps make one PR.
- **Size**: S is under half a day, M is one to two days, L is three to five days, for
  one engineer who knows the repo.
- **Check** lines say what must pass before the step merges. Every step also keeps
  `pnpm typecheck`, `pnpm lint`, `pnpm test` and `make test` green, runs `make sqlc`
  after SQL changes, and reports skipped checks (repo rule).
- Repo rules that apply to every step: no foreign keys; every migration index is
  `CONCURRENTLY` in its own file; UI-consumed JSON goes through zod and
  `parseWithFallback` with malformed-response tests; UUID handling per the backend UUID
  rules; copy in all six locales; shared UI in `packages/views`, logic in
  `packages/core`; no stores in `views`.

## Dependency map

```
WS0 Preflight ──┬──> WS1 Engine (6 rooms x 10 desks) ─────────┐
                ├──> WS2 Floors, seats, home squad ───────────┼─> WS3 Office on floors
                │                                              │
                │            WS4 Proposals ────────────────────┤
                │                                              │
                ├──> WS5 Digest + Briefs ──> WS6 Secretary ────┤
                │                                              │
                └──> WS7 Knowledge (K1..K6) ───────────────────┴─> WS8 Docs + rollout
```

What can run in parallel (they touch different files): WS1, WS2 backend, WS7-K1, and
WS4 backend. What must wait: WS3 needs WS1 and WS2; WS5 needs WS2 and WS4; WS6 needs WS5
and WS7-K2; WS7-K4 needs WS7-K1..K3.

## WS0: Preflight (S)

| Step | Work | Check |
|---|---|---|
| 0.1 | Branch from `origin/main`; confirm `make up` works and the office renders. | `make status` healthy |
| 0.2 | Hermes: with the daemon running, `multica daemon status` lists a `hermes` runtime online; a trivial agent run on it completes. Needs a model provider configured in Hermes (`hermes model`), which only the boss can supply. | One run completes, or the blocker is recorded |
| 0.3 | WeKnora: the evaluation already ran it (about 650 MB idle, ingestion and search verified; see the spec's hands-on evaluation). Re-run `curl localhost:8090/health` on the target machine. | Health endpoint ok |

## WS1: Engine, 6 rooms of 10 desks (L, parallel)

Touches only `packages/views/office/lib/pixtuoid/` and the office constants.

| Step | Work | Files | Check |
|---|---|---|---|
| 1.1 | Check out pixtuoid at the pinned commit (`b74dfb6`), apply the existing patch cleanly, and reproduce the committed wasm. Add `wasm32-unknown-unknown` with rustup. | scratch checkout | Rebuilt wasm matches behaviour of the committed one |
| 1.2 | Change the studio to 6 rooms x 10 desks: `OFFICE_ROOMS`, `OFFICE_ROOM_DESKS`, `OFFICE_DESKS`, `STUDIO_DESKS`, room and pod sizes, `studio_rooms` grid 3 x 2, `studio_desks` 5 x 2 pod, `STUDIO_MIN_W/H`. Keep rooms and desks snapped to the coarse grid. | patch, `office-protocol.ts` (`MIN_UNITS_*`) | Rust tests in the checkout pass |
| 1.3 | Screenshot a full floor at 1280 x 720 and 1920 x 1080. Acceptance (corrected: figure is 12 units, about 37 px at 1280 x 720; see the spec's Engine section): figure at least 37 px tall, tags and room names legible, every worker reaches a desk. If not met, switch the pod to 4 x 2 (8 desks) and repeat. | scratch | Screenshots reviewed |
| 1.4 | Commit the rebuilt wasm, the regenerated patch and the README, with `OFFICE_ROOMS` in core widened to 6 and `OFFICE_ROOM_CAPACITY` to the final desk count. Office tests updated. | `pixtuoid-web-issues.patch`, `README.md`, `wasm/*`, `office-protocol.ts`, `office-canvas.test.tsx` | `pnpm test` office suites |

## WS2: Floors, seats, home squad (L)

| Step | Work | Files | Check |
|---|---|---|---|
| 2.1 | Migrations: `office_floor`; `squad.floor_id`, `squad.office_slot`; partial unique index on the seat; `agent.home_squad_id`; drop `agent.office_room`. sqlc queries and `make sqlc`. | `server/migrations/*`, `server/pkg/db/queries/squad.sql`, new `office_floor.sql`, `agent.sql` | Migration up and down apply |
| 2.2 | Service and handlers: floors CRUD (empty floors only), atomic `PUT /api/office/seats` with 409 on conflict, auto-seat on squad create, `home_squad_id` rules (member of the squad; cleared on removal and archive; set on first membership), 16-floor cap, workspace isolation. Remove `agentOfficeRooms`. | `server/internal/handler/office.go`, `squad.go`, `agent.go`, router | Go tests listed in the spec |
| 2.3 | Core: `packages/core/office/` types, zod schemas, queries, mutations; `layoutFloor`; remove `office-room.ts` and `use-update-agent-office-room.ts`. | `packages/core/office/*`, `packages/core/agents/*`, `types/squad.ts`, `types/agent.ts` | `layoutFloor` matrix, schema malformed tests |

PR: "feat(office): floors, seats and home squads" (2.1 to 2.3).

## WS3: Office on floors (L)

Needs WS1 and WS2.

| Step | Work | Files | Check |
|---|---|---|---|
| 3.1 | Rebuild `OfficeScene` on `layoutFloor`: sessions per agent by home squad, room names from squads, empty slots with "Create squad". Seating no longer depends on the issue list; the list feeds tags. | `office-view.tsx`, `office-canvas.tsx` | View tests |
| 3.2 | Floor tabs: rename in place, reorder, add, delete; remember the last floor per workspace as a client preference; first-run card. | new `office-floor-tabs.tsx`, `office-first-run.tsx` | View tests |
| 3.3 | Unseated tray and the "Seat all" banner for owners and admins. | `office-unseated-tray.tsx` | View tests |
| 3.4 | Squad panel; agent panel loses the room tab, gains "Home squad". Remove `office-room-menu.tsx`. | `office-squad-panel.tsx`, `office-agent-panel.tsx` | View tests |
| 3.5 | Locale strings in all six languages under `office.*`. | `packages/views/locales/*/issues.json` | Locale parity test |

PR: "feat(office): squads on the floor".

## WS4: Proposals (L, backend parallel with WS2)

| Step | Work | Files | Check |
|---|---|---|---|
| 4.1 | Migration: `proposal` table and indexes; extend `issue_origin_type_check` with `proposal`. sqlc. | `server/migrations/*`, `proposal.sql` | up and down |
| 4.2 | Service: create (issue unassigned, `in_review`; urgent assigns the oldest owner), limits (3 pending per squad, 3 per week per squad, 10 pending per workspace, 1 urgent per squad per week), decide (approve with optional handoff project and kickoff issue through the issue service; reject with a mention comment). Agent actors refused on decide. Events after commit. | `server/internal/service/proposal.go`, `handler/proposal.go`, router | Go tests: limits, approve with and without handoff, reject wakes the leader, non-owner refused |
| 4.3 | CLI `multica proposal create|list`; leader briefing block "Proposing to the boss" for squads with an active autopilot. | `server/cmd/multica/cmd_proposal.go`, `execenv/runtime_config_sections.go` | CLI tests, briefing test |
| 4.4 | Core queries, mutations, schemas. | `packages/core/office/proposals.ts` | Schema tests |
| 4.5 | Decisions dialog, room badge, `Needs you` tag. | `office-decisions-dialog.tsx`, `office-view.tsx` | View tests |

PR: "feat(office): proposals and decisions". 4.5 lands after WS3.

## WS5: Digest and briefs (L)

Needs WS2 and WS4.

| Step | Work | Files | Check |
|---|---|---|---|
| 5.1 | Digest queries and `GET /api/office/digest`: per-squad done, in progress, blocked, stale (3 days), failed runs, no-action count, pending proposals, five most recent completed; workspace runs and cost, offline runtimes; quiet-squad collapse; caps. `multica brief digest`. | `server/pkg/db/queries/digest.sql`, `service/digest.go`, `handler/digest.go`, `cmd_brief.go` | Caps, ordering, stale and blocked rules, workspace isolation |
| 5.2 | Migration: `brief` table. Endpoints `POST|GET /api/briefs`; publish allowed for the Secretary or an admin only; heading and size validation; `brief_ready` inbox type for each owner; notification-preference mapping. `multica brief publish`. | `server/migrations/*`, `handler/brief.go`, `notification_listeners.go`, `types/inbox.ts` | Permission, validation, inbox tests |
| 5.3 | Brief dialog: latest brief rendered, its proposals with Approve and Reject, Ask prefill, unread dot, history, empty state with Generate now (a manual autopilot run). | `office-brief-dialog.tsx`, `office-header.tsx` | View tests |

## WS6: The Secretary (M)

Needs WS5. The Hermes check from 0.2 must have passed before 6.3.

| Step | Work | Files | Check |
|---|---|---|---|
| 6.1 | `workspace.secretary_agent_id` (or the existing settings field); publish permission uses it. | migration, `workspace.sql`, handler | Tests |
| 6.2 | `secretary` instruction template: role, pull model, digest as the only source of facts, brief structure, language rule, inquiry cap, authority table, memory discipline. | `packages/core/agents/instruction-templates.ts` | Template test |
| 6.3 | First-run setup flow: check for an online Hermes runtime, create the Secretary (private, concurrency 1), the `Executive` floor and `Executive Office` squad, set the setting, pin in Chat, create the daily autopilot (08:00 local, `run_only`) and the paused weekly one. | `packages/core/squads/templates.ts`, `office-first-run.tsx`, quickstart | Core and view tests; manual run on Hermes |
| 6.4 | `market_research` squad template and its weekly routine, seated on a `Research` floor. | `templates.ts`, instruction templates | Tests |
| 6.5 | E2E: first loop from a created research squad to an approved proposal that starts a project, then a brief that lists a pending proposal; autopilot triggered manually. | `e2e/` with `TestApiClient` | Playwright |

## WS7: Knowledge base (L, K1 parallel)

The spec's knowledge section defines each piece. WeKnora runs as a pinned sidecar.

| Step | Work | Files | Check |
|---|---|---|---|
| K1.1 | `knowledge` compose profile with pinned images; env `KNOWLEDGE_URL`, `KNOWLEDGE_SERVICE_EMAIL` and `KNOWLEDGE_SERVICE_PASSWORD`; the Ollama address in `SSRF_WHITELIST_EXTRA`; WeKnora ports remapped off 8080; docs for the deployment and backups. | `docker-compose.selfhost*.yml`, `SELF_HOSTING.md` | Stack boots, health ok |
| K1.2 | `server/internal/knowledge/` client behind a `Knowledge` interface: service-account login, tenant create, workspace key create (use the `token` field, not `api_key`), KB create, document create (manual, URL, file), publish, delete, search, read, MCP endpoint create. Contract tests against recorded responses; a fake for other tests. | `server/internal/knowledge/*` | Contract tests |
| K1.3 | Migrations: `workspace_knowledge`, `knowledge_base`, `knowledge_link`. Provisioning service: enable for a workspace (tenant, key, default KBs, `company-read` MCP endpoint, registration in the workspace MCP library and assignment to agents), idempotent and resumable. | `server/migrations/*`, `service/knowledge.go`, `handler/knowledge.go` | Provisioning tests with the fake |
| K2.1 | "Company knowledge" instruction block, included only when enabled. | `execenv/runtime_config_sections.go` | Briefing test |
| K2.2 | Read verification with a real agent: a run searches and cites a document through MCP (gated real-agent smoke test; only with authorization). | gated test | Manual |
| K3.1 | Write path: `multica knowledge add|list|publish|discard`, draft-then-publish, idempotency keys, admin-only delete. | `cmd_knowledge.go`, service | Tests |
| K3.2 | System writers: decision records and brief archive into `decisions`. | proposal and brief services | Idempotency tests |
| K4.1 | Core: Knowledge API schemas, queries, mutations (search, list, documents, drafts, upload, URL import, publish, discard). | `packages/core/knowledge/*` | Schema tests |
| K4.2 | Knowledge page: KB list, browser, search with sources, upload and URL import, Drafts, "Save to knowledge" on issues and briefs, Open in WeKnora, offline state. Route, nav entry, desktop wiring. | `packages/views/knowledge/*`, `apps/web`, `apps/desktop` | View tests |
| K5.1 | Automatic context on issue runs (setting, timeout, failures ignored); Secretary searches the KB first; brief notes on drafts. | `service/task.go`, templates | Tests |
| K6.1 | Freshness list in the weekly brief; data-source guidance in docs. | digest, docs | Tests |

PRs: K1 (infra, client, provisioning), K2 and K3 (agent read and write), K4 (page),
K5 and K6.

## WS8: Docs and rollout (M)

| Step | Work |
|---|---|
| 8.1 | `apps/docs`: Office (floors, squads, decisions), Secretary and briefs, Knowledge base, deployment notes. English first, then the translations the docs convention requires; read `conventions.mdx` before writing copy. |
| 8.2 | Release note and upgrade notes: `office_room` removed, new env vars, optional sidecar. |
| 8.3 | Manual verification script on the boss's machine: Hermes online, daily brief generated, a proposal approved from a brief, a document saved, drafted, published and found by an agent. |

## Delivery order

1. WS0.
2. In parallel: WS1, WS2, WS4 backend (4.1 to 4.4), K1.
3. WS3, then 4.5.
4. WS5, then WS6.
5. K2, K3, K4, then K5 and K6.
6. WS8.

Each PR leaves the product working, and nothing is exposed that cannot be turned off:
the Secretary and the knowledge base are opt-in per workspace.

## Risks that change the plan

- **Engine rebuild (WS1)** is the largest unknown. If the 3 x 2 grid cannot meet the
  size acceptance, drop to 8 desks before touching the grid; if that also fails, ship
  WS2 and WS3 on four rooms (the page already supports it) and finish WS1 after.
- **Hermes (WS0.2)** needs a model provider only the boss can authorize. WS6 can be
  built and tested without it; its manual verification waits.
- **WeKnora API churn** is contained by the adapter and the pinned version. A version
  bump is its own step with the contract tests re-recorded.
- **Embedding model choice** is fixed per knowledge base at creation; changing it later
  means re-ingesting. Decide before K1.3 creates any KB.

## Implementation status

Status at the completion round, measured against the code on `main`. "Done" means
implemented and covered by tests that ran; "Partial" and "Open" are listed with the
reason.

| Area | Status | Notes |
|---|---|---|
| WS1 Engine: 6 rooms x 10 desks | Done | Figure is about 37 px at 1280 x 720; the spec's 64 px target was unattainable (corrected in the spec). |
| WS2 Floors, seats, home squad | Done | Backend, core `layoutFloor`, migrations 601–607. |
| WS3 Office on floors | Done | Floor tabs, first-run card, unseated tray, squad panel. `agent.office_room` removed (608). |
| WS4 Proposals | Done | Backend, limits, decisions, CLI, leader briefing (in `handler/squad_briefing.go`, not `execenv`). |
| WS4 4.5 Decisions dialog, room badge | Done | |
| WS5 Digest and briefs | Done | Backend, CLI, inbox type. |
| WS5 5.3 Brief dialog, Generate now | Completion round | See the completion PR. |
| WS6 6.1–6.2 Setting, secretary template | Done | |
| WS6 6.3 Setup | Done backend, completion round for UI | `GET/POST /api/office/setup` idempotent; UI and Chat pin in the completion round. |
| WS6 6.4 market_research | Done | Placement on a named Research floor completed in the completion round. |
| WS6 6.5 First-loop e2e | Completion round | Spec written; not run against a live stack on this machine. |
| WS7 K1 Sidecar, client, provisioning | Done | Live-tested against WeKnora v0.9.0. |
| WS7 K2–K3 Instruction block, read and write paths | Done | |
| WS7 K4.1 Core | Done | |
| WS7 K4.2 Knowledge page | Completion round for Save to knowledge and Ask the Secretary | Page, search, drafts, enable are done. |
| WS7 K5.1 Automatic context | Done | Gated on readiness, score ≥ 0.5, top three, 2 s. |
| WS7 K6.1 Freshness and pending drafts | Completion round | |
| WS8 8.1 Docs | Done for en, fr, ja, ko, zh | No zh-Hant docs locale exists in the site; translations not reviewed by native speakers. |
| WS8 8.2 Upgrade notes | Done | In `SELF_HOSTING.md`. |
| WS8 8.3 Manual verification on the boss's machine | Open | Needs a Hermes model provider configured by the boss and a live knowledge profile. |
| WS0.2 Hermes runtime online and a trivial run | Open | Hermes v0.22.5 is installed; a model provider is not configured. |
| Full repo checks (`make check`, `pnpm test`, `make test`) | Completion round | Run before the completion PR. |
