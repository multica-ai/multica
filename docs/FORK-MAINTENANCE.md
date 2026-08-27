# Fork maintenance (scotthawes/multica)

This fork is **isolated by design**. We do not send PRs upstream. All custom
work lives on `my-fixes`; occasionally we pull changes from `origin`
(multica-ai/multica) and re-apply ours on top.

The `my-fixes` autonomy / model-control series has been merged to `main`, which
now tracks `mine/main` (deploy from `main`). `my-fixes` remains the development
branch for the next patch series.

## Remotes

| remote | points at | role |
|---|---|---|
| `origin` | multica-ai/multica | upstream source, fetch only |
| `mine` | scotthawes/multica | our published backup |

## Ongoing work

Everything goes through the `my-fixes` branch:

```bash
git checkout my-fixes
# ... change, build (cd server && go build ./... && go test ./internal/daemon/ ./internal/service/), commit
git push mine my-fixes
```

Issue tracker for this fork's backlog: GitHub issues on scotthawes/multica
(GAP-1..30 filed there; see issues #5–#30).

## Syncing from upstream

When upstream releases something we want (check
https://github.com/multica-ai/multica/releases):

```bash
git fetch origin
git checkout my-fixes
git rebase origin/main        # or merge, if history sharing matters more than linearity
# resolve conflicts, then:
cd server && go build ./... && go test ./internal/daemon/ ./internal/service/ ./internal/handler/
git push --force-with-lease mine my-fixes   # rebase rewrote history
```

Rules learned so far:
- Upstream moves fast (~5 patch releases in 2 days during Aug 2026). Rebase
  sooner rather than later; conflicts compound.
- Hot conflict zones: `service/task.go` (retry machinery evolves often),
  `internal/daemon/daemon.go` (large file, many authors). Expect manual merges.
- After any rebase touching `pkg/db/queries/*.sql`, run `make sqlc` before
  building.
- Keep `docker-compose.selfhost.yml` edits uncommitted or in a stash — they are
  machine-specific and not part of the patch series.

## Current patch series (my-fixes)

Rebased onto upstream main 2026-08-24 (includes their runtime_offline
health-gated retry + hasRunnableSuccessor slot guard).

1. `a6c9ef8`→ fix(execenv): atomic metadata writes + gc-meta write retry
2. `193724e`→ feat(daemon): durable terminal-report outbox (+ tests)
3. `2b41ead`→ feat(service): auto-retry webhook-triggered autopilot runs
4. `7f0f85e`→ feat: jitter retry schedules against thundering herd
5. `2c5aa8a`→ perf(db): bound claim-candidate scan with LIMIT
6. `4b165ae` feat(retry): prior-attempt failure digest into retry children (GAP-23)
7. `fc8f4e3` feat(verify): opt-in verifier agent runs after branch delivery (GAP-24, migration 403)
8. feat(service): hollow-completion flag — agent comment when an issue task completes with no branch (GAP-29, issue #24)
9. feat(service): dead-letter case file comment on retry exhaustion (GAP-27, issue #22)
10. fix(daemon): GC spares agent branches whose taskKey task dir still exists under WorkspacesRoot — crash between finalize and report no longer loses committed work (GAP-16, issue #21)
11. fix(daemon): retry RecoverOrphans 3x with capped backoff at workspace registration — transient DB error no longer leaves prior incarnation's running rows stuck until restart (GAP-18, issue #28)
12. feat(agent): destructive-command gate on ACP permission requests — rm -rf /, force-push to main/master, DROP DATABASE/TABLE, TRUNCATE, fork bombs are hard-denied before auto-grant (reject_once or protocol error) instead of silently approved; v1 is a static blocklist with warn log, no approval UI yet (GAP-30, issue #25)
13. fix(daemonws): soft-drop before slow-client eviction — full send buffer drops the frame and counts consecutive drops (`soft_drops_total` metric + one warn log); client evicted only after 5 consecutive drops or ping timeout, so one busy tick no longer kills an otherwise healthy daemon connection (GAP-22, issue #26)
14. feat(daemon): optional per-provider concurrency ceiling — `MULTICA_PROVIDER_CEILING="codex:2,claude:3"` caps in-flight tasks per provider; unset providers fall back to `MULTICA_DAEMON_MAX_CONCURRENT_TASKS`, env unset = no change. Enforced at claim time (capped providers get their own headroom-bounded claim batch; at-ceiling providers are skipped that cycle), in-memory only, no DB column (GAP-21, issue #29)
15. fix(daemon): writer-liveness marker gates workdir reuse — `.writer_alive` written in the managed workdir at task start, removed only on clean completion; a leftover marker makes `execenv.Reuse` decline so the next task on the issue gets a fresh checkout instead of a dir a crashed writer left half-mutated. Marker-existence check only, managed (non-local/worktree) workdirs only (GAP-15, issue #18)
16. feat(daemon): disk-pressure telemetry on daemon heartbeat — daemon reports `disk_free_percent` (Statfs on WorkspacesRoot filesystem) in every HTTP + WS heartbeat; server logs a warn below 10% free, no DB column, additive optional wire field so old peers ignore it; Windows reports unknown (GAP-8, issue #13)
17. feat(scheduler): opt-in retention sweep job for append-only tables — third `JobSpec` (`retention_sweep`, daily) on the existing lease/catch-up infra; batched deletes (1000/stmt, ≤50 loops/table) of terminal `sys_cron_executions` (SUCCESS/FAILED by finished_at), delivered `webhook_delivery` (non-queued, raw bodies are the bulk), and read `inbox_item` rows past the age threshold. Off by default: `MULTICA_RETENTION_DAYS` unset/0 = inert; per-table overrides `MULTICA_RETENTION_CRON_EXECUTIONS_DAYS` / `MULTICA_RETENTION_WEBHOOK_DELIVERY_DAYS` / `MULTICA_RETENTION_INBOX_ITEM_DAYS` (0 disables that table). No migration, no FKs, live-row semantics unchanged (GAP-9, issue #11)
18. feat(daemon): opt-in additional repo checkouts per task — repo entry (workspace repos JSON or github_repo resource_ref) gains `"additional_checkout": true`; flagged repos get a sibling worktree at `<envRoot>/extra/<repo-name>` created before the agent starts (sync-on-miss into the existing bare-repo cache, then `CreateWorktree`, which also refreshes a checkout left by a reused env), and the brief's Repositories section names the path so the agent works there directly instead of running `multica repo checkout`. Opt-in only: flag unset = byte-identical default path (no extra dir, no cache sync); failure fails the task before StartTask like any prepare error; GC reclaims `extra/` with the env root wholesale. No new tables, no migration. Known ceiling: linked-worktree gitdirs live under the shared cache — if a provider sandbox ever refuses writes to these out-of-workdir siblings, switch them to `IsolatedGitMetadata` (GAP-11, issue #12)
19. feat(handler): `event` autopilot trigger kind — third kind alongside schedule/webhook; create/update accept it, event_filters required (the filter set IS the subscription contract), timezone rejected (no next_run_at), filters round-trip in trigger responses, falls through to the generic INSERT with empty cron so the cron scheduler skips it (`kind != "schedule"` guard untouched). No dispatch wiring yet: bus events don't fire these triggers — kind is validated + persisted for future routing. Downstream source whitelists widened additively: quota admission accepts run source `event`, retry eligibility treats event runs like webhook runs (nothing re-fires them → auto-retry on transient failure), metrics label whitelist, daemon brief passthrough comment. Existing schedule/webhook behavior byte-identical; CLI can't create them yet (no --event-filters flag) (GAP-7, issue #10)
20. feat(service): outbound notification sinks — `MULTICA_NOTIFY_SINKS="https://hooks.example.com/a,https://hooks.example.com/b"` comma list (env-optional, unset = disabled). Every terminal task (`NotifyTaskFinished` + batch `notifyTasksFinished`) fire-and-forget POSTs `{task_id,status,reason}` per sink via 2s-timeout Client in a goroutine so slow sink never blocks request; warns on delivery fail/reject (4xx/5xx). No retry/HMAC/queue, no new table; add webhook_delivery worker + signing when guarantee needed (GAP-6, issue #9)
21. feat(scheduler): issue dependency edges gate task dispatch — `ClaimAgentTask` candidate SELECT skips queued issue tasks whose issue has an `issue_dependency` row (`issue_id` = the task's issue, `type='blocked_by'`) whose blocker is not terminal (`done`/`cancelled`); task stays queued and is retried on every later poll, so closing/unblocking the blocker releases it with no extra wiring. Opt-in: no dependency rows → NOT EXISTS trivially true, dispatch byte-identical. Uses existing table from migration 001 (no migration, no FK changes). Known ceiling: raw status check covers built-ins only — a custom status in the done/cancelled category does not unblock (safe direction); no API writes dependency edges yet, so edges must be inserted directly until that lands (GAP-13, issue #8)

22. docs(cli): webhook autopilot triggers are CLI-exposed + documented — `multica autopilot trigger-add <id> --kind webhook` (upstream MUL-5421 code, docs lagged); CLI_AND_DAEMON.md no longer claims webhook/api unimplemented; documents event_filters scoping via POST /api/autopilots/<id>/triggers and trigger-rotate-url signing-secret rotation; api kind still server-less (GAP-12, issue #7)

23. feat(security): encrypt agent custom_env at rest (GAP-10, Phase 1, Closes #6) — AES-256-GCM envelope `{"enc":"v1","n":...,"c":...}` in `agent.custom_env` JSONB; `MULTICA_ENV_ENC_KEY` (32-byte hex/base64) set on the server, passed to the daemon which decrypts on claim. Key unset → plaintext degrade (one warning, no migration). Supersedes the earlier "deferred" note.

24. fix(daemon): per-task opencode data-dir isolation — execenv prepares `<envRoot>/opencode-data` (mkdir 0700) for provider=opencode on fresh prepare and reuse, daemon exports it as `XDG_DATA_HOME` before agent start (before custom_env so a user-set XDG_DATA_HOME still wins); mkdir failure falls back to the shared `~/.local/share/opencode` default instead of blocking dispatch; dir is GC'd with the env root. Kills the SQLite lock-collision failure class when concurrent opencode tasks share one db (GAP-1, issue #5)

25. feat(daemon): opt-in provider failover chain — `MULTICA_PROVIDER_FAILOVER="codex:claude,kimi-k2.6;claude:qwen3.7-plus"` (semicolon between primaries, comma between fallbacks; bad/self/duplicate fallbacks warn+skipped). handleTask runs the primary then walks the chain only when an attempt dies on a transient transport error (`agent_error.provider_network` or 429/529 via taskfailure.Classify); success, any other failure class, or runCtx cancellation breaks immediately and reportTerminalTask keeps the last attempt's reason. Usage from every attempt accumulates into the reported result so billing stays complete. In-memory, no DB/migration/RPC; unset env = byte-identical pre-failover path. ProviderCeilings still enforced per attempt. Known ceiling: server's 3-attempt provider_network retry budget is shared across the whole chain, not per provider (GAP-5, issue #4)

26. feat(daemon): official agent-wrapper hook — `MULTICA_AGENT_WRAPPER` env on the daemon (unset = byte-identical direct spawn). Non-empty value is whitespace-split (`strings.Fields`); task launch runs `<wrapper...> <agent-binary> <agent args...>` instead of the bare agent by rewriting the launch boundary at `agent.ResolveBackend` in runTask (ExecutablePath ← wrapper[0], wrapper remainder + agent path prepend LaunchPrefix). Covers all providers incl. custom-profile launches; model-selection probe goes through the wrapper too. Contract: wrapper must be exec-transparent (stdin/stdout passthrough, propagate exit codes). No DB/migration/config field (GAP-3, issue #2)

27. feat(daemon): prior-run digest handoff on session resume — when a task resumes a provider session (`PriorSessionResumed`, i.e. the claim carried a prior_session_id), writeContextFiles collects the reused workdir's git state via best-effort `git -C <workdir>` (current branch, commits ahead of main/master, last 3 commit subjects, capped at 1KB) and renders it as a "## Prior Run Digest" section in `.agent_context/issue_context.md`. Kills the bare-issue-ID cold rediscovery tax on resumed runs (GAP-2, issue #1). Sidecar-only: never reaches CLAUDE.md/AGENTS.md runtime brief, which stays byte-identical across runs (MUL-5377). No DB/migration/persisted digest; any git failure (fresh clone, detached HEAD, non-repo workdir) omits the section silently. Known ceiling: ahead-count probes only main/master default branches; tool-result digests would need session-file parsing (GAP-2 stretch goal)

28. feat(agent): per-step model attribution for opencode runs — opencode backend accumulates `step_finish` token usage into a per-model map instead of one bucket: each step is keyed by the model its own stream events name (`part.modelID`/`part.providerID` flat or nested message-style `model:{providerID,modelID}` block, rendered `provider/model`); a step naming no model falls back to the configured agent model (or "unknown"). Result JSONB already serializes `Usage` keyed by model, so multi-model cost analytics split without DB migration. Read-only parse of events already flowing; today's opencode run stream puts no model fields on parts, so behavior is byte-identical until a version starts naming them (GAP-4, issue #3)

29. feat(daemon): per-task cost/token/wall-clock budget caps — `MULTICA_BUDGET_MAX_COST_USD` / `MULTICA_BUDGET_MAX_TOKENS` / `MULTICA_BUDGET_MAX_WALL_CLOCK` (e.g. `90m`), per-agent override via agent CustomEnv (same key names), all zero = off so default behavior byte-identical. Wall clock wraps run context under existing cancel watcher; cost/token checked on every attempt boundary against accumulated GAP-4 usage (chain stops on overrun — failover would only spend more); overrun fails task with new taxonomy reason `agent_error.budget_exceeded`, off retry allowlist, so FailTask posts failure comment + GAP-27 dead-letter case file auto. Cost uses provider-reported price ticks — backends reporting no cost leave that cap inert (token + wall clock still bound). Known ceiling: checks per attempt, not live-per-step; runner usage callback upgrade path (GAP-28, issue #23)

30. feat(service): event trigger dispatch full (GAP-31, issue #31) — `CompleteTask` now calls `maybeEnqueueEventTriggers` after verifier: queries `ListEventAutopilotTriggersForTask` with filter `[{"event":"task.completed"}]` for completing agent's workspace, then for each trigger creates `AutopilotRun` (source `event`, status `running`, payload `{"event":"task.completed","task_id":...}`) + `CreateAutopilotTask` (agent/squad-leader resolved minimal dispatchRunOnly pattern), updates run via `UpdateAutopilotRunRunning`, and `NotifyTaskEnqueued`. ~30 lines additive, no new table/migration; log-only stub upgraded to enqueue.

Daemon-side pieces take effect when you rebuild + swap `~/.local/bin/multica`
AND the desktop-bundled binary (see below). Server-side pieces require
redeploying the self-host server from this branch.

### Autonomy enablement (2026-08-25, workspace PSP-latro)

Full-autonomy caps set for workspace `55e95948-ebca-4d05-97fe-913db33d969d`:
budget $5 / 500k tokens / 60m wall clock, provider ceilings
`opencode:2,deepseek-v4-flash:2`, failover
`opencode:deepseek-v4-flash;deepseek-v4-flash:opencode`, notify sinks empty.
Values live in `.env` + compose passthrough (`docker-compose.selfhost.yml`,
pool tweak `pool_max_conns=8&pool_min_conns=2` preserved) and — because tasks
for this workspace run under the **host** launchd daemon, not a container —
in `~/Library/LaunchAgents/com.multica.daemon.plist`. Plist change applies on
next daemon restart:
`launchctl kickstart -k gui/$UID/com.multica.daemon` (do NOT run mid-task).
Verifier chain set via direct SQL (no API token in `.env`): Gameplay
Programmer → QA Tester → Reviewer 2 (`verify_agent_id`).

### Desktop app binary swap

Desktop spawns `<Multica.app>/Contents/Resources/app.asar.unpacked/resources/bin/multica`
— NOT ~/.local/bin. After each Multica app update:

```bash
cp ~/.local/bin/multica /Applications/Multica.app/Contents/Resources/app.asar.unpacked/resources/bin/multica
codesign --force --sign - /Applications/Multica.app/Contents/Resources/app.asar.unpacked/resources/bin/multica
# then quit + reopen the app; rollback backup: ~/multica.bak-desktop-0.4.32
```

Requires App Management (or Full Disk Access) permission for your terminal.

### Verifier agents (GAP-24)

Set via API: `PATCH /api/agents/{id}` with `"verify_agent_id": "<uuid>"`
(null clears). The named agent then runs a fresh-session task on the same
issue whenever this agent completes work that produced a branch; its handoff
note names the branch to check out and asks for a PASS/FAIL verdict comment.
Verifier failures are non-retryable by taxonomy.

### Autonomous development setup (multica-dev workspace) — 2026-08-25 (live)

Dogfooding: run Multica on itself for `my-fixes` development.

1. `multica workspace create --name multica-dev --slug multica-dev --issue-prefix MDEV` → workspace `dc85f04e-b671-457f-808f-b9a666ac6063`, runtime `ffb65ef2-346f-4e46-a588-eba82ba712b1` (Opencode MacBook-Air-2.local), `WorkspacesRoot` `~/multica_workspaces_multica-dev`.
2. Agents per role (same runtime): `Builder` `bb15ecfd-2b44-444e-9e59-e36cf115e230` (`muse-spark` fixer) → `QA` `5dd45ae7-d0c1-4a9a-9f49-8b55b871fb27` (`deepseek` tester) → `Reviewer` `c9601323-8f1b-4691-b157-c48f830b38b9` (`minimax` oracle) `verify_agent_id` chain; `Docs` `6209fd9f-27dd-4477-be54-92cf297aef9d` (`qwen` librarian).
3. Autopilots: `Handoff` `48cd5804-b8b3-4d1e-bc38-ef738bb6f0fb` (`event` trigger `a21c48ec-88f3-4e7c-953a-05b303c08bea` `[{"event":"task.completed"}]`) → `GAP-31` `maybeEnqueueEventTriggers`; `Retention Sweep` `ce954b98-d0d0-4642-bce6-46f4f5dd95bf` (`schedule` `658c3771-595f-4fbf-959d-7d2f2040099a` `0 2 * * *` nightly, `GAP-9`).
4. Enable `MULTICA_AGENT_WRAPPER` sandbox + `custom_env` encryption `GAP-10` when secrets move.
5. Wire `.env` `MULTICA_BUDGET_MAX_*` `5/500k/60m` + `PROVIDER_CEILING/FAILOVER` already `42249` live — reuse for `multica-dev` tasks.

### Model fallback / retry / pricing / encrypt / help-signal (2026-08-27, main @ 035f46887)

Merged from `my-fixes` to `main` (which now tracks `mine/main`) via PRs
#39, #42, #44, #45, #46, #47. Migrations **432–435** and **437** (there is no
436 — `model_pricing` lives in 434):

- **432** `model_tier_map` — global + per-workspace tier→concrete map.
- **433** `agent_task_queue.concrete_model` — the resolver-chosen concrete model.
- **434** `model_tier_map.fallback_concrete` (ordered chain) + new `model_health`
  and `model_pricing` tables.
- **435** `agent_task_queue.auto_rerun_count` — one auto-rerun on retry exhaustion
  for transient provider errors (gated by `isModelFailureReason`).
- **437** `agent_task_queue.help_signal` JSONB + `agent_requested_help` reason +
  `agent_help_requested` inbox item (GAP-25); excluded from auto-retry/auto-rerun.

Behavior:
- Resolver `resolveConcreteModel` picks a known-healthy candidate (primary →
  fallback) up front; `FailTask` on a model failure marks `model_health`
  unhealthy, `CompleteTask` marks it healthy; `model_pricing_watcher` (15m poll)
  flips a price-breach model `unhealthy`/`pricing` with a **sticky downgrade**
  (`last_failure_at` pushed 365d ahead) until the price recovers.
- Auto-rerun: after retry exhaustion on a transient model failure, exactly one
  auto-rerun fires instead of dead-ending.
- Help signal: agents emit `blocked_reason` / `needs` / `confidence` on `/fail` or
  `/complete`; the platform routes it to a human inbox item, never the retry loop.

CLI/API surface: `multica model-map get|set|get-fallback|set-fallback`,
`multica model-health get|set`, `GET|PATCH /api/model-map[/fallback]`,
`GET|PUT /api/model-health`. See `docs/model-availability-fallback-design.md`
and `docs/AUTONOMY-RUNBOOK.md`.

Notes:
- `MULTICA_ENV_ENC_KEY` (GAP-10, Phase 1) is wired in `docker-compose.selfhost.yml`
  and read from `.env`, which is **gitignored** — never commit the key.
- Daily host disk hygiene: a launchd job `com.scotthawes.docker-cleanup` runs
  `docker image prune` + `docker container prune` at 04:00 daily. It is host-side,
  not part of the compose stack.

### Missing AI tools (next gaps)

- **Reviewer verifier**: exists `GAP-24` but not wired to all delivery agents → wire `Reviewer 2` as verifier for delivery agents + `Test Harness` `go test` gate.
- **Cost observability**: `GAP-4` `usageByModel` + `GAP-28` caps logged but no dashboard → add `GAP-6` `MULTICA_NOTIFY_SINKS` webhook to `PostHog`/`Grafana` for `token/cost` per model.
- **Semantic search**: no vector index for codebase → add `repoCache` + `pgvector` `issue`/`code` embeddings for `PriorRunDigest` relevance.
- **Prompt optimizer**: no `prompt-optimizer` skill linked → add `foundry prompt-optimizer` for `handoffNote`/`issue_context` templates.
- **External dispatch**: no `MCP` tools for `gh`, `docker`, `cloudflared` → add `mcp: github-gh`, `docker-mcp` to `agent` `Workflow` for autonomous PR/migration.
- **Blocked deps API**: `issue_dependency` `blocked_by` edges have no API — insert via SQL until handler lands.

### Cross-workspace model control (proposed 404, pricing shifts)

**Goal**: 1 row flip controls 40 agents across 4 workspaces — price jump needs no per-agent `UPDATE`.

**Design**: tier indirection, not concrete pin. `agent.model` stores tier `cheap/balanced/premium` or concrete escape. `model_tier_map` holds concrete.

```
model_tier_map(workspace_id uuid NULL, tier text, concrete text, PRIMARY KEY(workspace_id,tier))
NULL workspace_id → global default (all workspaces follow)
workspace_id set → override that workspace only
resolver: concrete = workspace_map[tier] ?? global_map[tier] ?? tier
```

**Migration 404**: `CREATE TABLE model_tier_map (...)` 3 global rows `cheap/balanced/premium` initially `mimo/muse-spark/qwen`. `ALTER` not needed, new table additive, no FK besides `workspace_id` cascade.

**Handler**:
- `PATCH /api/model-map` `{global:{cheap:"...",balanced:"...",premium:"..."}}` → `UPSERT` `NULL` rows
- `PATCH /api/workspaces/{id}/settings {model_map:{cheap:"..."}}` → `UPSERT` `workspace_id` row
- `GET /api/model-map` returns `global` + `overrides` + resolved concrete per agent + `cost` last 24h via `GAP-4` `usageByModel`

**CLI**:
- `multica model-map set --global --cheap mimo --balanced muse-spark --premium qwen` → global 3 rows
- `multica model-map set --workspace dc85f04e --cheap other` → override only `multica-dev`

**Resolver**: `server/internal/service/task.go:enqueueIssueTask` before `CreateAgentTask`:
```go
concrete := tier
if m, ok := workspaceMap[tier]; ok { concrete = m } else if g, ok := globalMap[tier]; ok { concrete = g }
// ponytail: 3-tier map, per-agent concrete escape when tier fails, add 4th tier when sweeper volume proves
```
`model NULL` → `balanced` fallback, per-agent `model="opencode/qwen..."` bypasses map (experiment).

**Ops**: `costUSDTicks` alert → `PATCH /api/model-map` 1 call → next `ClaimAgentTask` resolves new concrete, no restart. `soft_drops_total` + `disk_free_percent` watch for capacity.

**Status**: not yet migrated — `agent.model` still concrete (`mimo/x-preview/big-pickle` 33 pinned, 7 `NULL`). Until 404 lands, control via direct `UPDATE agent SET model='tier'` (40 rows) or `docker exec psql UPDATE model_tier_map` stub via `.env` `MULTICA_MODEL_MAP_CHEAP` fallback (needs `launchctl kickstart`).
