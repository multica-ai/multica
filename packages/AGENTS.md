# Package Guidelines

This directory contains shared TypeScript workspaces used by the web and desktop apps.

> **Single source of truth:** Root `../CLAUDE.md` defines the authoritative package
> boundaries, commands, and coding rules. Read it before changing package code.

## Package Map

- `core/` - headless business logic, API clients, React Query hooks, platform adapters, and shared Zustand stores.
- `ui/` - atomic UI components and shared styles only; no product data or business logic.
- `views/` - reusable business views and page-level components shared across web and desktop.
- `tsconfig/` - shared TypeScript configuration.
- `eslint-config/` - shared lint configuration.

## Boundary Rules

- Keep `core/` free of `react-dom`, browser globals such as `localStorage`, and `process.env`.
- Keep `ui/` free of `@multica/core` imports.
- Keep `views/` free of `next/*`, `react-router-dom`, and Zustand stores; route through the navigation adapter.
- Put web/desktop-shared behavior in `core/`, `ui/`, or `views/` instead of duplicating it in both apps.
- Declare every directly imported external package in the workspace's own `package.json`.

## Verification

Use the root `CLAUDE.md` command list for checks. For package-only TypeScript changes,
the relevant checks are normally the Turborepo `pnpm typecheck`, `pnpm lint`, and
`pnpm test` flows, or a narrower package-filtered Vitest command when appropriate.
