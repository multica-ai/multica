# Repository Guidelines

**This file is a pointer. It holds no rules of its own.**

All authoritative architecture, coding rules, commands and conventions for this
repository live in **[CLAUDE.md](CLAUDE.md)** at the project root. Read that
file first — including "Investigate before you code" (the committed code-map).

Two rules there are load-bearing enough to name here so you cannot miss them:

- **Cerebro Extension Discipline** — never silently edit an upstream-zone file
  (`server/**`, `packages/core/**`, `packages/ui/**`, `packages/views/**`).
  CLAUDE.md defines the three protection paths.
- **`docs/agents/permission-system.md`** — read before touching anything that
  grants, denies, gates or approves an agent action.

Verification lives in **[VERIFY.md](VERIFY.md)**. Deploy lives in
**[DEPLOY.md](DEPLOY.md)**.

A rule written only here is invisible to Claude agents, which read CLAUDE.md and
not this file. Add rules there, never here.
