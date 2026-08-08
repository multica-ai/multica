# DEPLOY — firtal-cerebro

**This file is the single source of truth for how this repository reaches
staging and production.** CLAUDE.md points here and does not restate it.

It covers the mechanics and the approval routing for this repository. The
cross-cutting process that is not repository-specific — how to wait for a
deploy, escalation, rollback decision-making — lives in the `deploy` skill,
which defers to this file whenever the two describe the same mechanic.

## Environments

| Environment | URL | Branch | Platform |
|---|---|---|---|
| Local dev | `http://localhost:3000` | your branch | — |
| Staging | `https://cerebro.firtal.com` | `main` | Sliplane (Docker containers) |
| Production | `https://Multica.firtal.com` | `production` | Sliplane (Docker containers) |

- **`main`** deploys continuously to staging. Sliplane rebuilds on every push.
- **`production`** deploys to production. Sliplane rebuilds on every push to
  that branch, and the merge into it requires an approved release-issue.

## What triggers a deploy

**Merging to `main` deploys to staging, not to production.** Production only
moves when `main` is merged into `production`, and that merge requires an
`approve` comment on a release-issue in the Multica "Deployments" project.

Standing rule: merge to `main` as soon as CI is green — do not wait. Staging
updates itself, and production still requires the separate release-issue step.

## Deploy flow — step by step

1. A PR merges into `main` (continuously, all day). Staging
   (`cerebro.firtal.com`) rebuilds automatically.
2. The `auto-deploy-trigger` autopilot fires from a GitHub webhook, with a
   scheduled fallback that polls `main` against `production`. It looks the app
   up in `registry.firtal.com`, runs the `release-review` checklist over the
   `main..production` diff, and creates **one** release-issue in the
   Deployments project (`ecb4fb83-0995-48a5-97d2-3adce73aa800`) per pending
   release wave. Later merges update the same issue (idempotent on repository
   plus main-head-sha), so five PRs in ten minutes become one approval, not
   five.
3. The app owner comments `approve` on the release-issue and tags the agent.
   The agent merges the standing `main → production` PR through the GitHub API.
   Approval happens as that comment in Multica — never as a button or a direct
   merge in the GitHub UI.
4. The push to `origin/production` makes Sliplane rebuild and roll out the
   production containers behind `Multica.firtal.com`.

## Default path vs selective release

**Default path (preferred):** merge `main` into `production` via the release-issue
above. That is a normal three-way merge. Files that exist only on `production`
stay on `production`; only changes that landed on `main` since the branches
diverged move forward. Do **not** reset `production` to the `main` tree, and do
**not** treat `git diff production..main` as the merge result — a plain diff
lists production-only files as deletions even though a real merge keeps them.

**Selective path (allowed):** ship a subset of `main` by opening a `release/*`
branch from `production`, cherry-picking the chosen commits, and merging that
branch into `production` through the same release-issue approval. Use this when
staging holds work that must not go live yet.

### Branch hygiene after a selective release

Cherry-picks create commits that live on `production` but not on `main` (hotfixes,
renumbered migrations, incident docs, diagnostics). That is fine short-term.
Before the next full `main → production` wave, those production-only commits
must already be on `main` — merge `production` back into `main` (or port the
files) so both branches carry the same production-critical content.

Check before every full release wave:

```bash
git fetch origin main production
# Content only on production (must be empty, or explained):
git diff --name-status origin/main..origin/production --diff-filter=A
# Migration files must match by path on both branches when already applied live:
git diff --name-status origin/main origin/production -- server/migrations/
```

Migration version strings are the full filename stem (for example
`9161_cerebro_drop_persona_sandbox`), not the numeric prefix alone. A migration
already applied in production must exist on `main` under that same stem so a
fresh database and production stay aligned. Idempotent SQL (`IF EXISTS` /
`IF NOT EXISTS`) is required when the same change also exists under an older
stem.

## Who approves

| Change | Reviewer | Approves the merge |
|---|---|---|
| Small (< 3 files, no user-facing impact) | — | Yourself |
| Medium (feature, UI, API) | Tine (QA) | You, after Tine has approved |
| High risk (auth, production data, payment, breaking) | Tine (QA) | Sara — wait for an explicit go |

**Never deploy on a Friday** without Sara's explicit approval.

**CLI release is not a production deploy.** Cutting a `v0.x.y` tag publishes
binaries to GitHub Releases and Homebrew only. Production always runs
`origin/production` regardless of the newest CLI tag. See CLAUDE.md, "CLI
Release (binary distribution)".

## PR labels: `staging-only` vs `prod-ready`

Every PR to `main` carries exactly ONE of `staging-only` or `prod-ready`. The
label decides whether a merged PR raises a `deploy_review` — that is, whether
the change moves on toward production. See `docs/agents/production-gate.md`
for the technical detail.

Any PR author, human or agent, may set `prod-ready` on their own PR once they
judge the change ready, with green CI as the floor. Green CI is the minimum,
not the trigger — waiting longer is allowed, flipping earlier is not.

- **Open with `staging-only`** while CI is still running, or if any check fails.
- **Flip to `prod-ready`** when CI is fully green and you judge the change
  ready for production. A trivial one-line fix qualifies the moment CI goes
  green; for a larger change, re-reading your own diff, soaking on staging, or
  waiting for a second pair of eyes first is all reasonable.

`staging-only` does not mean work-in-progress — it means "not yet cleared for
production".

## Verifying a deploy

Staging (`cerebro.firtal.com`): open the URL, confirm the app loads, and check
the Sliplane deploy log for the service.

Production (`Multica.firtal.com`): open the URL, confirm the app loads, check
the Sliplane deploy log for errors, and compare the serving commit with the
release merge commit. The server exposes `/version` (commit SHA) for
programmatic checks. The full production proof is
`multica agent-browser internal-verify --app multica` — see VERIFY.md, §3.

Every push to the Sliplane deploy branches also starts
`.github/workflows/cerebro-post-deploy-live-runtime-tools.yml`. It waits until
the environment's `/version` reports the pushed commit, then fails if the live
workspace has no verifiable online Runtime or any online Runtime has an empty
`capabilities` report. The `staging` and `production` GitHub environments must
provide the authenticated URLs and token documented in
`docs/agents/task-mandate-rollout.md`. A failed live Runtime tool gate means the
deployment is not accepted; inspect the workflow and Sliplane logs, keep the
affected feature flag off, and follow the rollback order below.

## Rollback

Use this order when a change fails after deploy:

1. Turn off the affected feature flag, if the change is behind one.
2. Fix forward for a small, isolated fault you can fix and verify immediately.
3. Otherwise revert the change on the `production` branch and let Sliplane
   deploy the previous version again.
4. If the new container will not start, select the last working deployment in
   the Sliplane dashboard, then verify `https://Multica.firtal.com` and the
   Sliplane log.

## Sliplane services

Staging (Sliplane project "Multica Staging"), running `main`:

- `multica-staging-web` — Next.js web app (`Dockerfile.web`)
- `multica-staging-backend` — Go API server (`Dockerfile`)
- `multica-staging-postgres` — staging database
- `multica-staging-cloudflared` — Cloudflare tunnel

Web and backend have autoDeploy on `main`: a push builds and rolls out by
itself.

**Manual fallback.** If an automatic build does not fire, trigger a new deploy
from the Sliplane dashboard on that service.

## Local runtime host

`.deploy/` holds the launchd scripts that run the stack on a single Mac for the
local agent runtime. It is cerebro-specific and separate from the Sliplane
deploy above. See `.deploy/README.md`.

## Changelog

The deploy logbook for this repository lives in **Multica**, not in this file:
**Changelogs → firtal-cerebro**.

Never record a deploy as a commit in `DEPLOY.md` — a docs-only commit creates
another deploy-review (FIR-1773). Write the entry in the Multica document.
