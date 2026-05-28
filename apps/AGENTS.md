# Apps Agent Guidelines

This file gives app-level guidance for agents working under `apps/`.
The root `../CLAUDE.md` remains the source of truth for architecture,
commands, coding rules, and package boundaries.

## Scope

- `web/` is the Next.js App Router client and owns Next-specific routes,
  platform adapters, and server actions.
- `desktop/` is the Electron client and owns Electron process code,
  packaging scripts, and React Router platform wiring.
- `docs/` is the documentation site.
- `mobile/` is the Expo / React Native client and has separate rules in
  `mobile/CLAUDE.md`.

## Shared App Rules

- Keep shared business logic out of app directories. Put cross-web/desktop
  logic in `packages/core/`, `packages/ui/`, or `packages/views/` according
  to the package boundary rules in `../CLAUDE.md`.
- App directories should contain platform integration: routing, process
  wiring, environment handling, and build or packaging entrypoints.
- Do not import `next/*` or `react-router-dom` from shared packages.
  Framework-specific navigation belongs in each app's platform layer.
- Use the package-specific scripts through the root workspace commands unless
  `../CLAUDE.md` documents a narrower command for the exact task.

## Before Editing Mobile

Read `mobile/CLAUDE.md` before changing anything under `mobile/`. Mobile has
its own React Native stack, release cadence, UI component rules, and parity
requirements with web and desktop.
