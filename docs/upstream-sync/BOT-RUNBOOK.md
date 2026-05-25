# Upstream sync bot — runbook

This runbook describes how the **Upstream Sync Bot** agent processes a nightly
issue created by the upstream-sync autopilot.

## Trigger

- Autopilot fires at 03:00 CET daily (cron in Multica).
- Autopilot mode: `create_issue` → opens an issue titled
  `Upstream sync — YYYY-MM-DD` assigned to the Upstream Sync Bot agent.

## What the bot does

1. Check out `firtal-cerebro` on the rolling branch.
2. Run `bash scripts/upstream-sync.sh --nightly` from the repo root.
3. Read `.upstream-sync/last-run.json`.
4. Act on the `status` field:

| status                | bot action                                                                                          |
|-----------------------|-----------------------------------------------------------------------------------------------------|
| `noop`                | Comment "Upstream HEAD matches fork main — nothing to sync." then close the issue (status=done).    |
| `clean`               | Comment "Clean merge — PR opened: <pr_url>." Leave issue open until PR is merged.                   |
| `clean-after-resolve` | Comment "Auto-resolve cleared all conflicts — PR opened: <pr_url>." Leave issue open until merged.  |
| `conflict`            | Read `.upstream-sync/conflicts-*.md`, paste into the issue, `@mention` Sara (CTO). Leave open.      |
| `push-failed` / `pr-failed` | Comment with the error message from `last-run.json`. `@mention` Sara.                         |

5. When a `clean` / `clean-after-resolve` PR is merged into `main`, the bot
   updates the issue with the deploy outcome and closes it.

## Idempotency

- Running the script twice in one day is safe: if the rolling branch already
  contains the current `upstream/main` HEAD, the script exits with `status=noop`
  and the bot reports "already shipped this batch".

## Recovery

- If the script exits with code `4` (push/PR failure), the local rolling branch
  is in good shape — re-running the script after fixing connectivity issues
  will retry the push.
- If the script exits with code `2` (conflict), the in-progress merge is
  aborted before the bot reads the report. The next invocation starts cleanly.

## Manual run

```bash
bash scripts/upstream-sync.sh --dry-run     # preview without changes
bash scripts/upstream-sync.sh --nightly     # full run
bash scripts/upstream-sync.sh --report      # print latest status JSON
```

## Configuration

- Reviewer override: set `UPSTREAM_SYNC_REVIEWER=<github-handle>` before
  running. Default is `tinetestsen` (Tine).
- Rolling branch name is hardcoded to `chore/upstream-rolling`; do not rename
  without also updating the bot agent's prompt and the PR-detection logic.
