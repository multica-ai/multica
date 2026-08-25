# Fork maintenance (scotthawes/multica)

This fork is **isolated by design**. We do not send PRs upstream. All custom
work lives on `my-fixes`; occasionally we pull changes from `origin`
(multica-ai/multica) and re-apply ours on top.

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

23. skip GAP-10 encrypt agent custom_env at rest — deferred (secretbox wiring bigger than ponytail slot, opt-in default plaintext unchanged). Retry when provider stable.
24. fix(daemon): per-task opencode data-dir isolation — execenv prepares `<envRoot>/opencode-data` (mkdir 0700) for provider=opencode on fresh prepare and reuse, daemon exports it as `XDG_DATA_HOME` before agent start (before custom_env so a user-set XDG_DATA_HOME still wins); mkdir failure falls back to the shared `~/.local/share/opencode` default instead of blocking dispatch; dir is GC'd with the env root. Kills the SQLite lock-collision failure class when concurrent opencode tasks share one db (GAP-1, issue #5)

25. feat(daemon): opt-in provider failover chain — `MULTICA_PROVIDER_FAILOVER="codex:claude,kimi-k2.6;claude:qwen3.7-plus"` (semicolon between primaries, comma between fallbacks; bad/self/duplicate fallbacks warn+skipped). handleTask runs the primary then walks the chain only when an attempt dies on a transient transport error (`agent_error.provider_network` or 429/529 via taskfailure.Classify); success, any other failure class, or runCtx cancellation breaks immediately and reportTerminalTask keeps the last attempt's reason. Usage from every attempt accumulates into the reported result so billing stays complete. In-memory, no DB/migration/RPC; unset env = byte-identical pre-failover path. ProviderCeilings still enforced per attempt. Known ceiling: server's 3-attempt provider_network retry budget is shared across the whole chain, not per provider (GAP-5, issue #4)

Daemon-side pieces take effect when you rebuild + swap `~/.local/bin/multica`
AND the desktop-bundled binary (see below). Server-side pieces require
redeploying the self-host server from this branch.

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
