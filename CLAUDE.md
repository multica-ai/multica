# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Super Multica is a distributed AI agent framework with a monorepo architecture. It includes an agent engine with multi-provider LLM support, an Electron desktop app with embedded Hub, a WebSocket gateway for remote access, and a Next.js web app.

## Monorepo Structure

```
super-multica/
├── apps/
│   ├── cli/           ← Command-line interface (`@multica/cli`)
│   ├── desktop/       ← Electron + Vite + React (`@multica/desktop`) — primary target
│   ├── gateway/       ← NestJS WebSocket gateway (`@multica/gateway`)
│   ├── server/        ← NestJS REST API server (`@multica/server`)
│   ├── web/           ← Next.js 16 web app (`@multica/web`, port 3001)
│   └── mobile/        ← React Native mobile app (`@multica/mobile`)
│
├── packages/
│   ├── core/          ← Core agent engine, hub, channels (`@multica/core`)
│   ├── sdk/           ← Gateway client SDK (`@multica/sdk`, Socket.io)
│   ├── ui/            ← Shared UI components (`@multica/ui`, Shadcn/Tailwind v4)
│   ├── store/         ← Zustand state management (`@multica/store`)
│   ├── hooks/         ← React hooks (`@multica/hooks`)
│   ├── types/         ← Shared TypeScript types (`@multica/types`)
│   └── utils/         ← Utility functions (`@multica/utils`)
│
└── skills/            ← Bundled agent skills
```

## Common Commands

```bash
# Install dependencies
pnpm install

# Multica CLI (unified entry point)
pnpm multica                   # Interactive mode (default)
pnpm multica run "<prompt>"    # Run a single prompt
pnpm multica chat              # Interactive REPL mode
pnpm multica session list      # List sessions
pnpm multica profile list      # List profiles
pnpm multica skills list       # List skills
pnpm multica tools list        # List tools
pnpm multica credentials init  # Initialize credentials
pnpm multica help              # Show help

# Development servers
pnpm dev                       # Desktop app (default, recommended)
pnpm dev:desktop               # Desktop app
pnpm dev:gateway               # WebSocket gateway only
pnpm dev:web                   # Next.js web app
pnpm dev:all                   # Gateway + web app

# Build
pnpm build                     # Build all (turbo-orchestrated)
pnpm --filter @multica/desktop build
pnpm --filter @multica/core build

# Type checking
pnpm typecheck

# Testing (vitest)
pnpm test                      # Single run
pnpm test:watch                # Watch mode
pnpm test:coverage             # With v8 coverage
```

## Architecture

```
Desktop App (standalone, recommended)
  └─ Hub (embedded)
     └─ Agent Engine (LLM runner, sessions, skills, tools)
        └─ (Optional) Gateway connection for remote access

Web App (requires Gateway)
  → @multica/sdk (GatewayClient, Socket.io)
    → Gateway (NestJS, WebSocket, port 3000)
      → Hub + Agent Engine
```

**Agent Engine** (`packages/core/src/agent/`): Orchestrates LLM interactions with multi-provider support (OpenAI, Anthropic, DeepSeek, Kimi, Groq, Mistral, Google, Together). Features session management (JSONL-based, UUIDv7 IDs), profile system (`~/.super-multica/agent-profiles/`), modular skills with hot-reload, and token-aware context window guards.

**Hub** (`packages/core/src/hub/`): Manages agents and communication channels. Embedded in desktop app, or runs standalone for web clients.

**Gateway** (`apps/gateway/`): NestJS WebSocket server with Socket.io for remote client access, message routing, and device verification.

**CLI** (`apps/cli/`): Command-line interface. Entry point: `apps/cli/src/index.ts`.

## Tech Stack & Config

- **Package manager**: pnpm 10 with workspaces (`pnpm-workspace.yaml`)
- **Build orchestration**: Turborepo (`turbo.json`)
- **TypeScript**: ESNext target, NodeNext modules, strict mode
- **Testing**: Vitest with globals enabled
- **Frontend**: React 19, Next.js 16, Tailwind CSS v4, Shadcn/UI
- **Backend**: NestJS 11, Socket.io, Pino logging
- **Desktop**: Electron 33+, electron-vite, electron-builder

## pnpm Configuration

**Required `.npmrc` for Electron packaging:**

```ini
shamefully-hoist=true
```

After adding/changing `.npmrc`:

```bash
rm -rf node_modules apps/*/node_modules packages/*/node_modules
rm pnpm-lock.yaml
pnpm install
```

See `docs/package-management.md` for detailed package management guide.

## Code Style

- **Comments**: Always write code comments in English, regardless of the conversation language.

## Design System

The UI follows a **restrained, professional** design language. This is a work tool, not a consumer app.

### Core Principles

1. **Restraint over decoration** — No flashy colors, minimal animations
2. **Clarity over cleverness** — Obvious > subtle, explicit > implicit
3. **Consistency over novelty** — Use Shadcn/UI patterns, don't reinvent
4. **Density over sprawl** — Respect screen real estate

### Typography

| Font | CSS Variable | Usage |
|------|--------------|-------|
| Geist Sans | `font-sans` | Primary UI text |
| Geist Mono | `font-mono` | Code, technical values |
| Playfair Display | `font-brand` | Brand name "Multica" ONLY |

Fonts are loaded via `@fontsource` packages (not Google Fonts) for cross-platform consistency.

### Colors

- **No brand color** — Purple/blue "AI colors" feel generic. We use neutral grays.
- **Color is for state** — Running (blue), success (green), error (red)
- **Dark mode is true dark** — Not gray, actual near-black

### Component Library

- **Base**: Shadcn/UI (Radix primitives + Tailwind)
- **Styling**: Tailwind CSS v4 with OKLCH colors
- **Config**: `packages/ui/src/styles/globals.css`

### When Building UI

- Prefer existing Shadcn components over custom implementations
- Use semantic color variables (`--muted`, `--destructive`), not raw colors
- Keep animations subtle and purposeful (no gratuitous motion)
- Test in both light and dark modes

## Credentials Setup

```bash
pnpm multica credentials init
```

Creates:
- `~/.super-multica/credentials.json5` (LLM providers + built-in tools)
- `~/.super-multica/skills.env.json5` (skills / plugins / integrations)

## Atomic Commits

After completing any task that modifies code, create atomic commits:

1. Run `git status` and `git diff` to see all modifications
2. Skip if no changes exist
3. Group changes by logical purpose (feature, fix, refactor, docs, test, chore)
4. Stage and commit each group separately

**Format**: `<type>(<scope>): <description>`

Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`

### Examples

```bash
git add packages/core/src/agent/runner.ts packages/core/src/agent/runner.test.ts
git commit -m "feat(agent): add streaming support"

git add packages/utils/src/format.ts
git commit -m "refactor(utils): simplify date formatting"

git add README.md
git commit -m "docs: update API documentation"
```

## Pre-push Checks

Before pushing, always run:

```bash
pnpm typecheck          # Type check all packages
pnpm test               # Run tests
```

This ensures CI will pass. For a clean check (no cache):

```bash
pnpm turbo typecheck --force
```
