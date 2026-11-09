Calendar loading verification, 2026-11-09. Captured in Chromium from this
checkout's isolated local Web app, 1440 × 1000, light theme, using E2E fixtures.
The calendar optimization and these screenshots are committed together.

- `calendar-after.png`: issue due-date picker after its calendar has loaded.
- `range-after.png`: created-date range filter after keyboard selection.

Reproduce against a managed local environment:

```sh
make up C=api,web ARGS="--ephemeral"
SHOTS_DIR=.github/pr-evidence/mul-8789 make env-exec ARGS="-- pnpm exec playwright test e2e/lazy-calendar-smoke.spec.ts --project=chromium"
make destroy ARGS="--yes"
```

The same browser check also selects and clears a due date, edits a custom date
property, and selects/applies a date range using the keyboard.
