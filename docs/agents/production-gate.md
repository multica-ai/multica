# Production gate — `prod-ready` / `staging-only` PR labels

Authoritative, agent-facing reference for the label every PR to `main` on this
repo must carry. Read this before opening a PR.

## What you need to do

Before you mark a PR ready and merge it to `main`, pin **exactly one** label on
the PR:

- **`prod-ready`** — this change is ready to be promoted to production
  (`Multica.firtal.com`). The deploy scanner in `firtal-data-registry` will
  raise (or extend) a `deploy_review` wave when the commit lands on `main`.
- **`staging-only`** — this change is for staging only (`Sara.firtal.com`).
  Common reasons: work-in-progress, an experiment, a one-off probe, anything
  you do NOT want a reviewer to consider promoting today. No `deploy_review`
  is raised.

If neither label is set, the scanner defaults to **skip** — same effect as
`staging-only`, but the intent is unclear. Add the label so future agents and
human reviewers can tell at a glance what you meant.

## Why this exists

`main` is staging (`Sara.firtal.com`). Anything that lands there goes live
in staging immediately — that is the design and the reason agents are free to
merge to `main` without prior human approval. `production` (`Multica.firtal.com`)
is a separate gated promotion, behind a human-reviewed `deploy_review`.

Before this label flow, the scanner raised a `deploy_review` for **every** new
commit on `main`. With agents using `main` as a staging playground, the review
queue filled with noise from experiments that were never meant to be promoted.
The label is the explicit signal that separates "ready for production" from
"just testing in staging".

## When in doubt

- Quick refactor that you have tested in your worktree and want a human to
  promote → `prod-ready`.
- Probing something / spike / WIP / "let me see if this even builds" →
  `staging-only`.
- Reverting or rolling back a previous promotion → `prod-ready` (the revert
  needs to reach production to actually undo the bad change).
- Documentation-only change touching `apps/docs/` → `prod-ready` (docs ship
  with prod).

## Mechanics (what the scanner actually does)

The deploy scanner lives in `firtal-data-registry/lib/deploy-scan/`. For every
new commit on `main` of a gated app (`deploy_model = gate` in the apps
registry — `firtal-cerebro` today), it calls GitHub's `commits/{sha}/pulls`
endpoint to find the merge PR and reads the labels:

| PR labels on the merge | Scanner action | Reason recorded |
|---|---|---|
| `prod-ready` | Raise or extend a `deploy_review` wave | `prod_ready_label` |
| `staging-only` | Skip; commit still recorded for the Changes UI | `staging_only_label` |
| neither | Skip (default-safe) | `no_label` |
| no PR found (direct push to `main`) | Skip | `no_pr_found` |
| GitHub lookup failed | Skip — never raise on partial data | `lookup_failed` |

Non-gated apps (everything that deploys directly from `main` with no separate
production branch) are not affected — they keep their existing "raise on every
new commit" behavior.

## Future tightening (not live yet)

These checks are on the roadmap on top of the label, in this order:

1. **CI must be green** in `firtal-cerebro` before the review is raised.
2. **Staging soak** — the change must have lived in staging for ≥ 2 hours
   without a new regression report.
3. **Fan-back to the source FIR issue** when CI fails or the review finds
   problems, so the agent that built it gets pinged.
4. **`hotfix` label** that bypasses the staging soak window for incidents.
5. **48-hour auto-escalation** for waves that sit open.
6. **One-click rollback** after a bad promotion.

None of these change the rule above — set the label on every PR.
