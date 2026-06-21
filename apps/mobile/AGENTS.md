# Mobile App Rules (apps/mobile/) — agent pointer

This file provides guidance to AI agents working in `apps/mobile/`.

> **Single source of truth:** This is a concise pointer document. The authoritative
> mobile rules — the locked tech-stack baseline, the `packages/` import boundaries, and
> the mandatory pre-flight before writing any mobile code — live in
> **`apps/mobile/CLAUDE.md`**. Read that file first.
>
> Repo-wide architecture, conventions, and commands live in the root **`CLAUDE.md`**
> (mirrored by the root `AGENTS.md`). The mobile-specific rules in `apps/mobile/CLAUDE.md`
> take precedence inside this directory.

## Why this file exists

Codex-based agents auto-load `AGENTS.md`, not `CLAUDE.md`. Without this pointer, an agent
working in `apps/mobile/` would never be told that `apps/mobile/CLAUDE.md` is the real
rulebook for this app. This file keeps the CLAUDE.md ↔ AGENTS.md pairing intact for the
mobile subtree, the same way the root `AGENTS.md` does for the repo root.

Keep this pointer in sync with `apps/mobile/CLAUDE.md`: if the mobile rules move or are
renamed, update this file in the same change.
