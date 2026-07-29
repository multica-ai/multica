# AGENTS.md — The documentation site

`apps/docs/` is the published product documentation: Fumadocs on Next.js, serving `content/docs/`. It is product-facing. Contributor procedures, internal decision history, and repository-only detail belong to the tiers in [`docs/AGENTS.md`](../../docs/AGENTS.md), not here.

The [decision record](../../docs/decisions/implemented/process/2026-04-28-docs-site-written-from-source.md) owns the rationale for how this site is written.

## Every claim comes from the source

Each factual statement on a page must be locatable in the code. Do not write from the product's own descriptions, from the interface, from memory, or from an earlier version of a page — that is how documentation acquires claims the implementation does not support.

**A capability that is not implemented is not documented.** A control in the UI, a column in the schema, or a handler accepting a field is not evidence that a feature works; look for the service-layer behavior. When a boundary is genuinely unclear, ask rather than guessing — a page that documents a half-wired feature is worse than one that omits it, because a reader will try to use it.

A change that alters documented behavior updates the affected page in the same pull request.

## Concepts before tasks

The vocabulary is the hard part. Workspace, Agent, Runtime, Daemon, Skill, Autopilot, Trigger, session resumption — none of it is self-evident, and a task guide that mentions a Runtime is unusable to a reader who does not know what one is. Explain the concept, then the task.

Explain the distributed execution model early and repeat the shape of it where it matters: the server does not run agents. Agents run on a daemon on the user's own machine. Nearly every "my agent isn't working" question resolves to that fact.

## Write for the reader tier the page serves

| Tier | Reader | What they want |
|---|---|---|
| P0 | New user, evaluator | What is this, and how do I have it running in five minutes |
| P0 | Self-host operator | How do I deploy it, and how do I diagnose it |
| P1 | Workspace owner or admin | How do I configure agents, permissions, and automation |
| P1 | CLI and developer user | The full command surface and the architecture behind it |
| P2 | A coding agent, pointed at one page by a human | Complete, independently runnable steps |

A P0 page does not need schema fields. A P1 page can carry an architecture diagram. The P2 reader sets a hard constraint on every procedure: **each command must be complete and runnable on its own.** Never write "replace the value in the command above" — that reader executes literally and has no earlier command in context.

## One home per fact, here too

A fact shared across pages is written once and linked from the others — the provider matrix is written in one place, not restated in every page that mentions providers. Duplicated prose drifts; a link does not.

## Language

English is the source. Other locales are translations of it, in sibling files (`page.mdx`, `page.zh.mdx`, `page.ja.mdx`, `page.ko.mdx`).

Editing a page obligates its translations in the same change, or the site ships a page that contradicts itself depending on the reader's language. Chinese copy follows the naming and voice rules in `content/docs/developers/conventions.zh.mdx`, which is also the source of truth for product terminology repo-wide.

A page is registered in `meta.json` and in each locale's `meta.<locale>.json`, or it does not appear in navigation.
