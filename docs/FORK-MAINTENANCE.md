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
