<!-- ╔════════════════════════════════════════════════════════════════════════╗ -->
<!-- ║  FORK NOTICE — READ BEFORE CHANGING ANYTHING                             ║ -->
<!-- ╚════════════════════════════════════════════════════════════════════════╝ -->

> ## ⚠️ This is a personal fork (Hira), not upstream Multica
>
> This repo (`saucevn/multica`) is a private fork that **Vietnamizes + rebrands**
> Multica as **"Hira"** (deployed at app.hira.vn). Upstream is `multica-ai/multica`.
> The fork is engineered to keep `git merge upstream/main` low-conflict so we can
> keep pulling upstream features. **You must protect that property.**
>
> **Before any change, read [`BRANDING.md`](BRANDING.md)** — it holds the 3-layer
> strategy, the Touch-point Registry (every upstream-owned file the fork edits +
> how to resolve each on merge), and the forbidden list.
>
> ### Golden rules (violating these breaks upstream sync — the whole point of the fork)
>
> 1. **Never rename technical identifiers.** Keep `@multica/*` packages, the
>    `multica` CLI, the Go module path, env vars (`MULTICA_*`), DB names, and the
>    `multica-locale` cookie. This is a *surface* rebrand only (UI text, brand
>    colors, logo, emails).
> 2. **Never edit `packages/ui/styles/tokens.css` or `base.css`.** All brand color
>    overrides live in fork-owned `packages/ui/styles/brand.css` (loaded after tokens).
> 3. **Never edit the `en` / `zh-Hans` / `ko` / `ja` translation strings.** The fork
>    only *adds* the `vi` locale (`packages/views/locales/vi/`). Keep it at parity
>    with `en` — `parity.test.ts` enforces this.
> 4. **Keep `DEFAULT_LOCALE = "en"`.** Vietnamese users auto-match `vi` via
>    `Accept-Language`; changing the default would fight upstream's tests.
> 5. **Don't rewrite upstream pages/components just to restyle** — override via
>    design tokens / CSS first.
> 6. **Every time you edit an upstream-owned file, add a row to the Touch-point
>    Registry in `BRANDING.md` in the same commit.** No silent divergence.
>
> ### Common fork tasks
>
> - **Add/fix a Vietnamese string** → edit the matching `packages/views/locales/vi/*.json`
>   (mirror the `en` key shape; keep `{{placeholders}}`). Run the parity test.
> - **Tweak brand color/look** → `packages/ui/styles/brand.css` only.
> - **Pull upstream features** → `scripts/sync-upstream.sh` (safe, isolated, runs the
>   safety nets). See [`BRANDING.md`](BRANDING.md) → *Upstream sync playbook*.
>
> Everything below this banner is **upstream Multica's** architecture guidance and
> still applies. The fork rules above take precedence where they overlap.

<!-- ── end fork notice; upstream content follows ──────────────────────────── -->

# Repository Guidelines

This file provides guidance to AI agents when working with code in this repository.

> **Single source of truth:** This file is a concise pointer document.
> All authoritative architecture, coding rules, commands, and conventions
> live in **CLAUDE.md** at the project root. Read that file first.

## Quick Reference

### Architecture

Go backend + monorepo frontend (pnpm workspaces + Turborepo) with shared packages.

- `server/` — Go backend (Chi router, sqlc, gorilla/websocket)
- `apps/web/` — Next.js frontend (App Router)
- `apps/desktop/` — Electron desktop app
- `packages/core/` — Headless business logic (Zustand stores, React Query hooks, API client)
- `packages/ui/` — Atomic UI components (shadcn/Base UI, zero business logic)
- `packages/views/` — Shared business pages/components
- `packages/tsconfig/` — Shared TypeScript config

### State Management (critical)

- **React Query** owns all server state (issues, members, agents, inbox, workspace list)
- **Zustand** owns all client state (current workspace selection, view filters, drafts, modals)
- All Zustand stores live in `packages/core/` — never in `packages/views/` or app directories
- WS events invalidate React Query — never write directly to stores

### Package Boundaries (hard rules)

- `packages/core/` — zero react-dom, zero localStorage, zero process.env
- `packages/ui/` — zero `@multica/core` imports
- `packages/views/` — zero `next/*`, zero `react-router-dom`, use `NavigationAdapter` for routing
- `apps/web/platform/` — only place for Next.js APIs

### Commands

```bash
make dev              # Auto-setup + start everything
pnpm typecheck        # TypeScript check
pnpm test             # TS unit tests (Vitest)
make test             # Go tests
make check            # Full verification pipeline
```

See CLAUDE.md for the complete command reference.
