## What does this PR do?

<!-- Describe the change clearly. What problem does it solve? Why is this approach the right one? -->



## Related Issue

<!-- Link the issue this PR addresses. If no issue exists, consider creating one first. -->

Closes #

## Production gate

<!--
  Pin EXACTLY ONE GitHub label on this PR before merging to `main`.
  See docs/agents/production-gate.md for the full rule.

  - `prod-ready`   → ready to be promoted to Multica.firtal.com (raises a deploy_review)
  - `staging-only` → for Sara.firtal.com only (WIP / experiment / probe; no deploy_review)

  If neither label is set the scanner defaults to skip — same effect as
  `staging-only`, but the intent is unclear. Tick the box that matches the
  label you set so reviewers can sanity-check at a glance.
-->

- [ ] `prod-ready` — promote to `Multica.firtal.com`
- [ ] `staging-only` — `Sara.firtal.com` only (no production review)

## Type of Change

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Refactor / code improvement (no behavior change)
- [ ] Documentation update
- [ ] Tests (adding or improving test coverage)
- [ ] CI / infrastructure

## Changes Made

<!-- List the specific changes. Include file paths for code changes. -->

-

## How to Test

<!-- Steps to verify this change works. For bugs: reproduction steps + proof that the fix works. -->

1.
2.
3.

## Checklist

- [ ] I have included a thinking path that traces from project context to this change
- [ ] I have run tests locally and they pass
- [ ] I have added or updated tests where applicable
- [ ] If this change affects the UI, I have included before/after screenshots
- [ ] I have updated relevant documentation to reflect my changes
- [ ] If I added a new runtime / coding tool / UI tab, I synced the change to **landing copy** (`apps/web/features/landing/i18n/`) and **relevant docs** (`apps/docs/content/docs/`)
- [ ] If this PR touches Chinese product copy, I checked it against `apps/docs/content/docs/developers/conventions.zh.mdx` (terminology, mixed-rule for `task` / `issue` / `skill`)
- [ ] I have considered and documented any risks above
- [ ] I will address all reviewer comments before requesting merge

## AI Disclosure

<!-- Most PRs involve AI coding tools — that's totally fine! We're curious about your process. -->

**AI tool used:** <!-- e.g. Claude Code, Cursor, GitHub Copilot, Multica Agent, N/A -->

**Prompt / approach:**
<!-- How did you use AI to produce this code? Share your prompt, conversation link, or describe your approach. This helps the team learn from each other's AI workflows. -->


## Screenshots (optional)

<!-- If applicable, add screenshots showing the change in action. -->
