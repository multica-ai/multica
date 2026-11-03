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

## Phases 2 to 4: how the remaining work is organised

The first part of this plan (WS0 to WS8) delivered phase 1 and the first slices of phases
2 and 3. The workstreams below implement the rest of the spec: **WS9** completes phase 2,
**WS10** is phase 3, **WS11** is phase 4 and **WS12** is release. The conventions of
"How to read this plan" apply unchanged. Migration numbers: WS9 700 to 729, WS10 730 to
779, WS11 780 to 849.

Two rules added for this stage:

- **A spike before a hot path.** Steps marked *spike* are timeboxed experiments whose
  result is written into the step's commit message before the dependent step starts
  (the budget gate in the claim path, the Telegram notifier). If a spike fails its check,
  the dependent step uses the stated fallback.
- **Every table ships with its writer, its reader and its delete path.** Workspace
  deletion (`workspace_delete_manifest_test.go`) lists each new table; the append-only
  tables are the only ones allowed no delete query inside the application, and are removed
  only by workspace teardown.

### Dependency map for WS9 to WS12

```
WS9  Phase 2 completion
  9.1-9.3 charters ───────────────┬─> 9.4 compliance ─> 9.5 operating review
  9.6-9.7 proposal lifecycle ─────┤                         │
  9.8 inquiries, 9.9 attention    │                         │
  9.10-9.11 chat delivery, 9.12 feedback, 9.13 catch-up     │
                                  v                         v
WS10 Phase 3 ─ 10.1 proposal kinds ─> 10.2-10.5 pipeline ─> 10.15 pipeline UI
  10.6-10.14 knowledge system (parallel with the pipeline) ─┤
  10.16 directory ──────────────────────────────────────────┤
  10.17 operate/learn ─> 10.18 starter company ─> 10.19 secretary updates ─> 10.20
                                                          │
WS11 Phase 4 ─ 11.1-11.3 metrics + goals ─> 11.5 controls ─> 11.6-11.8 budgets
  11.4 governance log (needed by 11.5, 11.8, 11.10, 11.14)
  11.9 autonomy ─> 11.10 standing approvals
  11.11 scorecards ─> 11.12 reviews ─> 11.13 revisions ─> 11.14 org changes ─> 11.15
  11.16 retros, 11.17 scale, 11.18 export, 11.19 health, 11.20 security
                                                          │
WS12 Release ─ docs, company-day scenarios, drills, performance, upgrade notes
```

What can run in parallel: within WS9 the charter track (9.1 to 9.5), the proposal track
(9.6 to 9.7), delivery (9.10 to 9.11) and the small items (9.8, 9.9, 9.12, 9.13) touch
different files. WS10's knowledge system (10.6 to 10.14) is independent of its pipeline
(10.1 to 10.5, 10.15). WS11's goals track and its budget track are independent; the
governance log (11.4) comes first because the others write to it.

## WS9: Phase 2 completion (L)

| Step | Work | Files | Check |
|---|---|---|---|
| 9.1 | Migrations 700 to 704: `squad_charter`, `squad_routine` (primary keys by the concurrent pattern, indexes). Queries and sqlc. Handlers `GET/PUT /api/squads/{id}/charter` with revision conflict, routines list/link/unlink, cadence presets creating autopilots through the autopilot service. Workspace deletion entries. | `server/migrations/*`, `queries/squad_charter.sql`, `handler/squad_charter.go`, router | Go: permissions (admin, creator, member read), revision 409, preset schedules, isolation |
| 9.2 | Core types, zod and queries for charter and routines. System-managed "Your charter" briefing block (and "Your goals" placeholder for 11.2) in `squad_briefing.go`. | `packages/core/office/charter-*.ts`, `handler/squad_briefing.go` | Schema tests; briefing text test; block absent without a charter |
| 9.3 | Squad panel: Charter tab (view/edit, intake, response hours) and Routines list with state chips, Run now, Pause, Add routine. Six locales. | `packages/views/office/components/office-squad-panel*`, new `office-charter-tab.tsx`, `office-routines.tsx` | View tests; locale parity |
| 9.4 | Compliance derivation service (states in spec 2.2, expected output per kind), `charter` section in the digest, `silent` flag, workspace counts; digest schema in core. | `service/routine_compliance.go`, `service/digest.go`, `core/office/digest-schema.ts` | Go: one test per state and output kind; `late` with grace; `silent`; isolation |
| 9.5 | Operating review: on first charter save create the weekly "Operating review" autopilot assigned to the squad with the system-built description; setting to disable; compliance covers it. | `service/operating_review.go`, charter handler | Go: created once, description contains charter and goals, disabling pauses it |
| 9.6 | Proposal lifecycle: migration (states `revising`, `expired`, `revision`, `expires_at`, `snoozed_until`); decision values `revise` and `snooze`; `multica proposal revise`; daily expiry job with one leader wake-up; related-prior-art notes on create (title similarity and decisions-base search at 0.75, never blocking). | `migrations`, `service/proposal.go`, `handler/proposal.go`, `cmd/multica/cmd_proposal.go`, scheduler job | Go: each transition, cap effects (snooze counts, revise does not count as new), expiry frees the cap, notes; CLI tests |
| 9.7 | Decisions dialog and brief card: Ask for changes, Decide later, revision history, related prior decisions. Digest and brief show snoozed items only when due. | `office-decisions-dialog.tsx`, `office-brief-dialog.tsx`, core proposals | View tests |
| 9.8 | `brief_inquiry` migration, `multica brief ask`, daily cap with `inquiry_limit`, answer capture on the leader's reply, digest `inquiries`; Secretary template updated to use the command instead of raw comments. | `migrations`, `handler/brief_inquiry.go`, `cmd_brief.go`, `instruction-templates.ts` | Go: cap, answered detection, isolation; template test |
| 9.9 | Adaptive attention: `squad_attention` migration, nightly recompute job, caps read by the proposal limits, pin by admin, squad panel display with the reason. | `migrations`, `service/attention.go`, `service/proposal.go` | Go: each band, minimum sample, pin, workspace cap fixed |
| 9.10 | **Spike** then build the notifier: `channel.Notifier` interface and the Telegram adapter (`sendMessage` with inline keyboard) using the installation credentials and the member binding; `brief_delivery` table and retry job; preferences API and UI; subscriber on `brief:created`; quiet hours. Spike check: a real message reaches a real Telegram chat from a test bot within ten seconds of publish. Fallback if the binding does not hold a chat id for a member: store `chat_id` on the preference at the first inbound message. | `integrations/channel/notifier.go`, `integrations/telegram/*`, `migrations`, `handler/brief_delivery.go`, `core/office/delivery-*.ts`, settings UI | Go: adapter against recorded responses, retry/backoff, quiet hours, failure never fails the brief; manual: real Telegram |
| 9.11 | Signed action buttons: token format (proposal id, action, expiry, HMAC with the server key), callback endpoint resolving the external user to a member through the binding, owner/admin check, same decision service; `/brief` and `/decisions` commands in the chat integration. Adapters for Slack and Lark behind the interface as 9.11a and 9.11b (own commits; platforms without button support fall back to links). | `integrations/channel/*`, `handler/channel_action.go` | Go: expired, tampered, wrong action, non-owner binding, agent actor, replay |
| 9.12 | Feedback: `brief_feedback` migration, API, dialog controls, `multica brief feedback --recent`, Secretary template reads it first and records standing preferences; `brief.usefulness` figure. | `migrations`, `handler/brief_feedback.go`, `office-brief-dialog.tsx`, template | Go and view tests; template test |
| 9.13 | Catch-up job and the workspace setting (`brief_catchup_enabled`, hour). | `service/brief_catchup.go`, scheduler | Go: fires once, skips when a brief exists, skips with the runtime offline, honours the setting |
| 9.14 | Phase 2 acceptance: scripted scenarios for 2.10 over the API; the Hermes run through Multica's daemon (WS0.2) executed with the boss present; the Telegram run. | `e2e/phase2-*.spec.ts`, runbook in `docs/engineering/runbooks/phase2.md` | Playwright run; manual sign-off recorded in the PR |

PRs: charter track (9.1 to 9.5), lifecycle (9.6 to 9.7), small items (9.8, 9.9, 9.12,
9.13), delivery (9.10 to 9.11), acceptance (9.14).

## WS10: Phase 3, the company operating system (XL)

| Step | Work | Files | Check |
|---|---|---|---|
| 10.1 | Proposal kinds: migration (`kind`, `payload`), the handler registry (`Validate`, `Apply`, `Summarize`), `decision` moved onto it with no behaviour change, `--kind` and kind flags in the CLI, per-kind rendering hooks in views. | `migrations`, `service/proposal_kinds/*.go`, `cmd_proposal.go`, `core/office/proposal-schema.ts` | Go: existing phase 1 tests pass unchanged; unknown kind refused; payload validation per kind |
| 10.2 | Pipeline and initiative schema (migrations 730 to 739), install service for the default pipeline (custom statuses per stage, workflow with squad handlers, board project, stage metadata), idempotent. | `migrations`, `service/pipeline_install.go`, queries | Go: install twice changes nothing; workflow steps hand to squads; statuses created once |
| 10.3 | Initiative service and API: create, artifacts rows, advance, attest, send-back, shelve, resume, close, force; stage events; events published; permissions as 3.1. CLI `multica initiative list/show/attest/advance/shelve`. | `service/initiative.go`, `handler/initiative.go`, `cmd_initiative.go` | Go: every transition and refusal; backing issue status follows; handoff assigns the owner squad |
| 10.4 | Kind handlers `opportunity` (creates the initiative) and `stage_gate` (advances; rejection sends back and wakes the leader); the system files gate proposals when a gate stage is ready; the build stage creates the SDLC build project and kickoff. | `service/proposal_kinds/{opportunity,stage_gate}.go`, `service/initiative_gate.go` | Go: end to end from opportunity to Validate, gate approve and reject, build project created |
| 10.5 | SLA and flags in the digest: `pipeline` section, `stalled`, artifact progress; the optional brief headings and the relaxed validator (spec 3.7 order) with core schema update. | `service/digest.go`, `handler/brief.go`, core | Go: validator accepts the new order and rejects a wrong one; `Watch` optional |
| 10.6 | Knowledge taxonomy: `knowledge_kind` migration and seed, `knowledge_link` columns, CLI flags and `template`, required-heading validation, supersession, tags pushed to WeKnora, search filters by kind. | `migrations`, `service/knowledge_kind.go`, `cmd_knowledge.go`, knowledge client | Go with the Fake and recorded WeKnora responses; reject missing headings; supersede hides the old document |
| 10.7 | `ventures` base and folder-per-initiative: provisioning update, a backfill that adds the base to workspaces already provisioned, resumable. | `service/knowledge_provision.go` | Go: existing workspace gains the base once |
| 10.8 | Publish policies: migration, resolution (kind over base), `auto_after_review` with `multica knowledge review`, L2 check (stubbed to true until 11.9 and then enforced), governance event writer interface (real in 11.4). | `service/knowledge_policy.go`, handler, CLI | Go: each mode; critic from another squad required; audit call made |
| 10.9 | The Librarian: instruction template and `knowledge-curator` role, the daily/weekly/monthly routines as charter routines, jobs for freshness, taxonomy audit and link repair; added to the Executive Office squad on setup and by a backfill. | `core/agents/instruction-templates.ts`, `service/knowledge_audit.go`, setup service | Go: freshness creates review requests, audit finds orphans and repairs links; template test |
| 10.10 | Retrieval eval: `knowledge_eval_case`, runner, hit rate stored and reported in the digest, "add as eval case" action in search results. | `migrations`, `service/knowledge_eval.go`, views | Go: hit-rate arithmetic; a wrong expectation lowers it |
| 10.11 | WeKnora models step: chat/summary, rerank and vision models from `KNOWLEDGE_LLM_*`, connection test with per-model errors, Wiki on three bases at `focused`, per-base off switch; env and SSRF notes in the self-host docs. Live check against the 9router endpoint on the development machine. | `knowledge/` client, `service/knowledge_provision.go`, `SELF_HOSTING.md` | Contract tests; live: a document yields a Wiki page and a summary |
| 10.12 | Upload and sources: multipart upload endpoint and drag-and-drop, URL import, Sources panel reading WeKnora data sources, the optional repo-docs sync routine. | `handler/knowledge_upload.go`, `packages/views/knowledge/*` | Go: size limit, kind required, member publish rule; view tests |
| 10.13 | `knowledge_acl` and per-subject MCP endpoints; agent endpoint assignment recomputed on membership and ACL change; `list_knowledge_bases` hides restricted bases. | `migrations`, `service/knowledge_acl.go` | Go: a restricted base is visible only to listed subjects; a removed member loses access |
| 10.14 | Deprovisioning job with the seven-day hold, and the per-owner WeKnora account provisioning with one-time password reveal. | `service/knowledge_deprovision.go`, handler | Go: not before the hold; recoverable within it |
| 10.15 | Pipeline UI: `/pipeline` route (reserved slug), board, initiative panel (overview, artifacts with Waive, thread, gates, timeline), admin Force, Office room badges, nav and desktop wiring, six locales. | `packages/views/pipeline/*`, `apps/web`, `apps/desktop`, `core/paths` | View tests; route and path consistency tests |
| 10.16 | Directory: endpoint, CLI, briefing text, `squad_request` origin, digest `overdue_requests`. | `handler/squad_directory.go`, migration for the origin check | Go: overdue arithmetic, isolation |
| 10.17 | Operate and Learn mechanics: the weekly `metrics_report` routine, 30/90-day review gates scheduled by the launch event, the three outcomes (continue, scale, sunset) with their handlers. | `service/initiative_operate.go`, kind handler | Go: gates created on launch, scale creates an initiative at Define, sunset triggers a retro (11.16) |
| 10.18 | **Starter company.** 10.18a: the squad card schema, apply service (`POST /api/office/company-template/apply`), resumable report, the wizard (runtime choice, selection, estimate, start options) and the idempotency tests. 10.18b to 10.18e: the squad cards and instruction templates by floor: Research (Opportunity Lab, Competitor Intelligence, Customer Voice, plus the existing Market Research); Product (Discovery, Analysis, Planning); Launch (Release & Ops, Growth & GTM, Support); Governance (Finance & Risk, People & Performance with phase 4 fields stubbed). Shared instruction blocks first. | `packages/core/squads/company-template.ts`, `packages/core/agents/instruction-templates.ts`, `service/company_template.go`, `packages/views/office/components/office-company-wizard.tsx` | Core: card schema and every card validates; Go: apply on empty workspace, twice, and interrupted at each step; view: wizard states |
| 10.19 | Secretary and leader updates: pipeline vocabulary and commands in the Secretary template, Pipeline section guidance, brief validator changes live, leader briefing mentions initiatives owned. | `instruction-templates.ts`, `squad_briefing.go` | Template and briefing tests |
| 10.20 | Phase 3 acceptance: the scenario of 3.8 over the API with scripted leaders, and the UI checks; runbook for the boss's machine (starter company on a clean workspace). | `e2e/phase3-*.spec.ts`, runbook | Playwright; manual sign-off |

PRs: kinds (10.1), pipeline back end (10.2 to 10.5), knowledge system (10.6 to 10.14 in
three PRs), pipeline UI (10.15), directory and operate (10.16, 10.17), starter company
(10.18 in five PRs), secretary updates and acceptance (10.19, 10.20).

## WS11: Phase 4, governance (XL)

| Step | Work | Files | Check |
|---|---|---|---|
| 11.1 | Metrics: migrations 780 to 789 (`metric`, `metric_reading`), built-in metric jobs (nightly) for every metric in 4.1, readings API and CLI, the webhook endpoint (token auth, rate limit, scope check), downsampling job. | `migrations`, `service/metrics*.go`, `handler/metrics.go`, `cmd_metric.go` | Go: each built-in metric against fixtures; webhook auth and limits; downsample keeps daily values |
| 11.2 | Goals and key results: migrations, progress and confidence derivation, API and CLI, planning-cycle job (opens a planning issue per charter squad), proposal kind `goal`, leader briefing "Your goals", operating review reports progress. | `migrations`, `service/goals.go`, `service/proposal_kinds/goal.go`, scheduler | Go: progress and confidence arithmetic including direction and override; planning job idempotent per period |
| 11.3 | Governance route (reserved slug) and the Goals tab: tree, detail, readings, add reading; brief Goals section guidance. | `packages/views/governance/*`, apps wiring | View tests |
| 11.4 | `governance_event` migration and writer used by every later step; Log tab with filters and CSV export; the append-only database test (no update or delete statement exists in the queries for this table). | `migrations`, `service/governance_log.go`, views | Go: writer; test asserting the generated queries expose no update or delete for the table |
| 11.5 | Company controls: `company_state` and quiet-hours settings, enforcement at autopilot dispatch, wakeups, auto-publish and pipeline auto-advance, the held-run state at the claim (shared with 11.7), "pause and stop running work", Office header control, `multica company` commands. | `service/company_state.go`, `service/autopilot.go`, `service/issue_wakeup*.go`, views, CLI | Go: each enforcement point; human and Secretary runs exempt; resume restores; quiet defers |
| 11.6 | **Spike** then budget accounting: `budget`, `budget_period`, `budget_hold` migrations; attribution (task to squad, floor, initiative), cost source unification with Analytics, incremental job with cursor, counters cache. Spike check: admission cost at the claim stays under 2 ms at p99 with 50 budgets and 10,000 queued tasks. | `migrations`, `service/budget_accounting.go`, shared cost function | Go: attribution cases; estimate equals the Analytics figure; benchmark in the commit message |
| 11.7 | The gate: `BudgetGate.Admit` in `claimTask` and the batch claim, run classes from the task's originator and source, reservation from the trailing p90, holds recorded once per task, release paths (extend, raise, period roll, cancel), autopilot dispatch check with reason code `budget_hold`. Fallback if the claim query cannot be gated cheaply: gate at the daemon claim handler before calling the service, with the same interface. | `service/task.go`, `service/budget_gate.go`, `service/autopilot.go` | Go: held vs admitted per class and mode; reservation arithmetic; a human run is never held; release makes the task claimable again |
| 11.8 | Budgets: API, Budgets tab (tree, burn, projected overrun, holds, Extend and Release), `budget` proposal kind, brief Budget section, defaults and `warn` start. | `handler/budget.go`, `service/proposal_kinds/budget.go`, views | Go and view tests; log entries written |
| 11.9 | Autonomy: `squad.autonomy_level`, the enforcement table of 4.3 (L0 routines held, publish policy checks, standing-approval eligibility), Autonomy tab, squad panel and room tag, changes logged. | `migrations`, `service/autonomy.go`, views | Go: each cell of the table; default level 1 for existing squads |
| 11.10 | Standing approvals: `approval_policy`, the engine hooked into proposal filing, forbidden classes in code, weekly cap, revoke, `standing_approval` kind, Policies list, "Auto-approved by policy" in the brief, the weekly review flag for reversed approvals. | `service/approval_policy.go`, hook in `service/proposal.go`, views | Go: matches, each refusal reason, every forbidden class, cap, revoke, system actor recorded; an agent cannot edit a policy |
| 11.11 | Scorecards: `scorecard` migration, nightly job, metric definitions (failure causes split), `insufficient` rule, API for owners and admins and the subject leader. | `service/scorecard.go`, handler | Go: each metric on fixtures; runtime-offline failures not charged to the agent |
| 11.12 | Reviews: `review` migration, the People & Performance squad card and reviewer template, monthly routine, `multica review publish`, API and Reviews tab, brief People section. | `migrations`, template, `handler/review.go`, views | Go: review must cite a scorecard; visibility rules; recommendation count |
| 11.13 | Instruction revisions: `agent_instruction_revision` migration, one hook in the agent update service covering every path (API, CLI, templates, imports), rollback endpoint, diff rendering component shared with charter changes. | `migrations`, `service/agent.go`, `packages/views/common/diff-view.tsx` | Go: each update path writes a revision; rollback restores; views test the diff |
| 11.14 | `org_change` kind with the eight handlers of 4.6 (instruction, skill, charter, autonomy, hire, retire, move, merge), filing permissions, attention accounting, the Decisions rendering per type, governance events. | `service/proposal_kinds/org_change/*.go`, views | Go: each type applies and reverts; hire creates the agent with the squad as home; retire cancels queued tasks and keeps knowledge; merge moves members and routines |
| 11.15 | Probation: the 14-day job, before/after scorecard comparison, outcome stored, rollback proposal when worse. | `service/probation.go` | Go: improved, neutral, worse; insufficient data defers |
| 11.16 | Retrospectives: `retro_trigger`, trigger evaluation job for the five triggers, dedupe window, retro issue templates, the `postmortem` and `retrospective` kinds wired, instruction block "search postmortems first". | `service/retro.go`, templates | Go: each trigger fires once per window; issue assigned to the right leader |
| 11.17 | Scale: `floor_chief`, brief `scope_type`/`scope_id` and the floor heading subset, floor-scoped digest, `digest_mode`, the floor chief template and routine, the pulse endpoint and the Office strip. | `migrations`, `handler/digest.go`, `handler/pulse.go`, template, views | Go: floor digest filters; auto mode switches above 30 squads; pulse cache; view test of the strip |
| 11.18 | Company as code: export and import endpoints, `multica company export/import`, schema version, idempotent import with a dry-run report. | `service/company_export.go`, CLI | Go: round trip into an empty workspace reproduces structure; secrets and runtimes excluded |
| 11.19 | System health tab: job status registry (last success, lag), delivery failures, WeKnora health, gate cache age, risk line in the digest for a stuck job. | `service/job_status.go`, views | Go: registry updates from each job; stuck threshold |
| 11.20 | Security review and retention: tests for the points in 4.9 (signed tokens, policy edit by agents refused, inert Markdown, rate limits), `metric_reading` downsample, review of the service account scope. | tests across packages | Findings recorded in the PR; all fixed or accepted in writing |
| 11.21 | Phase 4 acceptance: the scenarios of 4.10 over the API (budget hold and extend, policy approve and refuse, pause, review to org change to rollback, retro) and UI checks. | `e2e/phase4-*.spec.ts`, runbook | Playwright; manual sign-off |

PRs: log and controls (11.4, 11.5), metrics and goals (11.1 to 11.3), budgets (11.6 to
11.8, with the spike result first), autonomy and policies (11.9, 11.10), reviews and org
changes (11.11 to 11.15), retros and scale (11.16, 11.17), operations (11.18 to 11.20),
acceptance (11.21).

## WS12: Release (M)

| Step | Work | Check |
|---|---|---|
| 12.1 | `apps/docs`: charters and routines, proposals in full, the pipeline and initiatives, knowledge taxonomy and the Librarian, the starter company, goals and metrics, budgets, autonomy and standing approvals, controls and the log, reviews and org changes, delivery to chat apps, company as code. English first, then fr, ja, ko and zh following `conventions.mdx`; update the Secretary and knowledge-base pages. | Link, frontmatter and navigation check; no screenshots invented |
| 12.2 | The six "company day" scenarios of the roadmap as one scripted run (API, with scripted leaders and a fake clock for schedules) plus the manual runbook for the boss's machine: Hermes through the daemon, Telegram, WeKnora with the LLM, the starter company on a clean workspace. | The script passes on CI-class hardware; the runbook is executed once and signed off |
| 12.3 | Backup and restore drill: Postgres tables and WeKnora volumes, restore order, company export/import for the structure. | Drill performed on a copy; time and steps recorded |
| 12.4 | Performance checks: digest with 50 squads and 200 initiatives, claim gate microbenchmark, board with 200 cards, brief length stays within target with the new sections. | Numbers recorded; thresholds in the spec met or the spec updated |
| 12.5 | Release notes and upgrade notes (new tables, env vars, optional components, migration list), version bump per `.github/RELEASING.md`. | Release checklist complete |

## Traceability: requirement to workstream

| Requirement | Workstreams |
|---|---|
| R1 office with many agents and squads | WS1 to WS3, WS10.18 |
| R2 scales to many squads | WS1, WS2, WS11.17 |
| R3 full value chain | WS10 (pipeline, starter company), WS6 (`market_research`), build via SDLC squads |
| R4 boss orchestrates and works with agents | WS3, WS4, WS9.6 to 9.7, WS11.5 |
| R5 proactive agents | WS4, WS9.1 to 9.9, WS10.4 |
| R6 Hermes Secretary, daily report, boss decides | WS5, WS6, WS9.10 to 9.13, WS10.19 |
| R7 company knowledge base | WS7, WS10.6 to 10.14 |
| R8 boss accountable, nothing runs away | WS4 limits, WS9.9, WS11 |

## Delivery order

1. WS0.
2. In parallel: WS1, WS2, WS4 backend (4.1 to 4.4), K1.
3. WS3, then 4.5.
4. WS5, then WS6.
5. K2, K3, K4, then K5 and K6.
6. WS8.

Each PR leaves the product working, and nothing is exposed that cannot be turned off:
the Secretary and the knowledge base are opt-in per workspace.

For the remaining work the order is: WS9 (charters, lifecycle, delivery in parallel),
then WS10 (kinds first, pipeline and knowledge system in parallel, starter company
last), then WS11 (log, controls and goals first; budgets after the spike; autonomy and
policies; reviews and org changes; scale), then WS12. The starter company (10.18) is
built before WS11 but its phase 4 fields (goals scaffold, budget shares, autonomy
defaults) are stubbed and completed in 11.2, 11.8 and 11.9.

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
- **The budget gate (11.6, 11.7)** is the riskiest step of phase 4: it sits in the claim
  path. The spike result decides between the service claim and the daemon handler as the
  gate's home; nothing else in WS11 depends on which.
- **The Telegram notifier (9.10)** depends on the binding holding a chat id. The
  fallback stores it at the first inbound message.
- **Forty instruction templates (10.18)** are the largest content task. Shared blocks
  are written first, floors land one PR at a time, and the weekly reviews and boss
  feedback are the long-run quality loop.
- **Order inside WS10** matters: proposal kinds (10.1) before anything files a gate or an
  opportunity; the Librarian (10.9) needs the Executive Office squad from setup.
- **Hermes on the boss's machine** gates the acceptance of WS9, WS10 and WS11. Scripted
  leaders prove the platform; the runbook proves the Secretary.

## Implementation status

Measured against the code on `main`. "Done" means implemented and covered by tests that
ran; "Open" means specified here and not built. WS9 is built on the phase 2 branch and WS10 on the phase 3 branch; WS11 and WS12 are open.

| Area | Status | Notes |
|---|---|---|
| WS0 Preflight | Partly done | Hermes is installed and answers through 9router; a run through Multica's daemon is open (9.14). |
| WS1 Engine: 6 rooms x 10 desks | Done | Figure is about 37 px at 1280 x 720; the 64 px target was unattainable. |
| WS2 Floors, seats, home squad | Done | |
| WS3 Office on floors | Done | `agent.office_room` removed. |
| WS4 Proposals | Done | Leader briefing lives in `handler/squad_briefing.go`. |
| WS5 Digest and briefs | Done | |
| WS6 Secretary | Done | Setup is an API call from the client; `market_research` is placed on a Research floor by the client. |
| WS7 Knowledge base | Done except upload and sources | K1 to K6; upload, sources panel and freshness reviews are in 10.12 and 10.9. |
| WS8 Docs | Done for phase 1 | en, fr, ja, ko, zh; the new phases are in 12.1. |
| Phase 1 acceptance in a browser | Partly done | `office-first-loop` passes; the full Playwright suite is slow here and unclassified. |
| WS9 Phase 2 completion | Built on `feat/digital-company-office-phase2`, not yet merged | Steps 9.1 to 9.14 are built; Slack and Lark (9.11a, 9.11b) are built, and DingTalk and WeCom are not built. Playwright spec `e2e/phase2-office.spec.ts` passes locally (one test, two runs). Manual sign-off is in `docs/engineering/runbooks/phase2.md`. |
| WS10 Phase 3 | Built on `feat/digital-company-office-phase3`, not yet merged | Steps 10.1 to 10.20 are built and unit-tested, and the initiative controls and issue thread (10.15) are in the pipeline panel. Upload heading checks cover `.md`, `.txt`, `.docx` and `.pdf` (10.12); they agreed with WeKnora's own parse on four live samples. Open or partial: 10.11 models, Wiki and summaries are not run live (needs the model endpoint); 10.12 repo-docs sync has no HTTP endpoint or UI and no live git test; 10.8 brief FYI for automatic publishes; 10.9 freshness routines report and open review issues only. Acceptance: `e2e/phase3-office.spec.ts` and `e2e/phase3-pipeline-ui.spec.ts` pass with WeKnora-dependent steps skipped; runbook `docs/engineering/runbooks/phase3.md` is unsigned. Deviations are in the spec's "Phase 3 deviations". |
| WS11 Phase 4 | Open | 11.1 to 11.21. |
| WS12 Release | Open | 12.1 to 12.5. |

## Deferred follow-ups

Items to pick up once every phase is done. They are recorded here so they are not lost.

- **Quickstart re-run for an existing squad.** If the first quickstart run's charter save fails, a re-run skips the squad that already has the template's name, so it ends up with no charter and no operating review. The fix is to send the revision-0 charter save on the existing path too, and treat the 409 as already chartered. The cost is that a hand-made squad with the same name also gets a charter and an operating review, so it needs a decision on that first.
- **DingTalk and WeCom chat push.** Spec 2.7 lists them with Slack and Lark, but the plan has no step for them. Decided not to build them now; Slack and Lark are the only non-Telegram adapters.
