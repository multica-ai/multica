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
- The script also skips the entire merge attempt when **any** open
  upstream-sync PR is already in flight — both the bot's own rolling-branch
  PR and a human-driven catch-up on `upstream-sync/*`. This prevents the bot
  from racing a manual catch-up and escalating a duplicate conflict report on
  the same batch (the FIR-2197 incident). The `last-run.json` `message` field
  names the in-flight PR URL so the heartbeat can link to it.

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
- Tracking issue for the deploy gate: set `UPSTREAM_SYNC_TRACKING_ISSUE=<id>`
  to change which issue's Tine PASS comment gates auto-deploy. Default is the
  daily-status issue FIR-2196. The sync PR body carries this as a
  `Tracking-Issue:` trailer.

---

# v2 — nightly auto-deploy after Tine approval (FIR-2217)

After the sync pass opens/updates PRs, the bot runs a **deploy pass**
(`scripts/upstream-sync-deploy.sh --pass`) that merges Tine-approved, CI-green
PRs. **Merging `main` IS the prod deploy** (the runner webhook runs
`.deploy/deploy.sh`). The bot never approves its own PRs — Tine's PASS is the
only approval signal.

## The five safety fences

| Fence | Rule | How it is enforced |
|---|---|---|
| a. Tine approval | No PASS ⇒ no merge | Bot reads the tracking issue's comments and requires a comment **authored by Tine** matching `Resultat: PASS` that **references the PR** (`#<num>` or full URL). A stale PASS on an earlier PR cannot green-light a different PR. |
| b. CI-green | Red/pending CI ⇒ skip + blocker | `gh pr checks` must report every check as `pass`/`skipping` and at least one check must exist. |
| c. Auto-rollback | Smoke fail ⇒ revert + incident | After merge the bot polls the public site for 200 (60s grace, then every 30s up to 10 min). On failure it auto-opens **and merges** a `revert/pr-<n>-*` PR (webhook redeploys the previous version) and files a high-priority incident issue pinging Sara. |
| d. Freeze | `deploy-freeze` label ⇒ pause all merges | A one-click label on the daily-status issue (FIR-2196). Present ⇒ the deploy pass exits without merging anything. Chosen over an autopilot metadata key because a label needs no redeploy and no code change to toggle. |
| e. Per-repo opt-in | Only firtal-cerebro | The script lives in and runs against this repo only. |

> The freeze fence **fails safe**: if the `multica` CLI cannot read the label
> (CLI absent / API down), the pass treats the state as frozen and merges
> nothing.

## How a PR becomes deployable

1. A PR is open, non-draft, mergeable, CI-green.
2. Its body carries a `Tracking-Issue: <FIR-key|uuid>` trailer (bot sync PRs
   get this automatically; human PRs add it manually). A `mention://issue/<id>`
   link is also accepted.
3. Tine posts a QA comment on that tracking issue containing `Resultat: PASS`
   and the PR number/URL.

Then the next deploy pass merges it, deploys, and verifies.

## Deploy-pass modes

```bash
bash scripts/upstream-sync-deploy.sh --pass        # full nightly pass
bash scripts/upstream-sync-deploy.sh --scan        # list green/mergeable candidates (no merge)
bash scripts/upstream-sync-deploy.sh --check <PR>  # is this PR Tine-approved? (exit 0/10)
bash scripts/upstream-sync-deploy.sh --smoke       # one smoke check (exit 0/1)
bash scripts/upstream-sync-deploy.sh --report      # print last-deploy.json
```

Result of `--pass` is written to `.upstream-sync/last-deploy.json`:

```json
{ "status": "deployed|rolled-back|noop|frozen",
  "deployed":    [{"pr":575,"sha":"…","deployed_at":"03:42 CET"}],
  "rolled_back": [{"pr":580,"sha":"…","revert":"https://…/pull/581"}],
  "skipped":     [{"pr":579,"reason":"no-tine-approval"}],
  "ran_at": "…Z" }
```

Exit codes: `0` ok/noop · `2` frozen/no-candidates · `4` merge failure ·
`5` smoke failed AND rolled back (a human should look).

## Bot agent instruction delta (apply in Multica UI)

The Upstream Sync Bot agent prompt has no CLI update path, so apply this change
in the agent settings when v2 ships:

- **Remove** the rule *"Du må aldrig merge en PR selv — det er Saras job."*
- **Replace** it with: *"Efter `--nightly` kør `bash scripts/upstream-sync-deploy.sh --pass`. Du merger KUN PRs som scriptet selv har godkendt (Tine PASS + grøn CI + ikke frosset). Læs `.upstream-sync/last-deploy.json` og inkludér deploy-udfald i samme heartbeat på FIR-2196. På `status: rolled-back` mention Sara; scriptet har allerede åbnet incident-issuet."*
- Extend the heartbeat format with a **Deploy** line:
  `- **Deploy:** <deployed #PR @ HH:MM | rolled-back #PR → revert-PR | frozen | ingen>`
