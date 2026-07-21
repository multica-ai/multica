# Animation improvement plans

These plans were written against commit `002ea0d87` after a read-only motion-opportunity review. They are implementation specifications; source code has not been changed.

| # | Plan | Severity | Status | Depends on |
| --- | --- | --- | --- | --- |
| 001 | [Animate the attachment preview entrance and exit](001-animate-attachment-preview.md) | MEDIUM | DONE | — |
| 002 | [Transition remote runtime setup into success](002-transition-runtime-connect-success.md) | MEDIUM | DONE | 001 |
| 003 | [Transition runtime skill import phases](003-transition-runtime-skill-import-phases.md) | MEDIUM | DONE | 001 |
| 004 | [Give agent creation screens a directional transition](004-transition-agent-creation-studio.md) | MEDIUM | DONE | 001 |
| 005 | [Add restrained feedback to batch toolbars](005-animate-batch-toolbars.md) | LOW | DONE | 001 |

## Completed execution order

1. Completed `001-animate-attachment-preview.md` first, adding the shared `@multica/ui/lib/motion` constants used by every later plan.
2. Completed plans 002, 003, and 004 on top of those shared constants.
3. Completed plan 005 with the shorter timing reserved for its higher-frequency interaction.

## Dependency notes

- Plans 002–005 must import the exact constants introduced by plan 001. They must not duplicate easing arrays or durations locally.
- If plan 001 is intentionally skipped, revise the dependent plan before execution so it establishes an equivalent shared module; do not improvise hand-written values in each feature file.
- Each executor must compare its target file with commit `002ea0d87`. If the cited structure has drifted materially, stop and request a plan reconciliation.

## Status values

- `TODO`: not implemented.
- `IN PROGRESS`: an executor is actively applying the plan.
- `DONE`: implementation and verification are complete.
- `RETIRED`: the finding no longer applies or was deliberately declined.
