# Decision: Mobile mirrors web's client-side shaping before rendering a list

Status: implemented

## Problem

Web showed "Inbox 1" while mobile showed three unread dots — same workspace, same user, same moment.

`GET /api/inbox` returns raw rows. They include archived items, and they include one row per notification rather than one per issue, so a comment, a status change, and an assignment on the same issue produce three rows. Web and desktop run those rows through `deduplicateInboxItems` in `packages/core/inbox/queries.ts` before rendering *and* before counting unread: drop archived, group by `issue_id` keeping the newest, sort by `created_at` descending. Mobile's first implementation rendered the raw list.

The bug is not really about the inbox. It came from assuming an API list response is what should be displayed, when the endpoint returns the raw cache shape and the client is responsible for shaping it. The same assumption is available to make again with timeline coalescing, comment thread flattening, and every future list endpoint.

## Decision

Mobile mirrors `deduplicateInboxItems` in `apps/mobile/lib/inbox-display.ts` and runs the inbox tab through it before rendering and before any counting.

The general rule this establishes is in [`apps/mobile/CLAUDE.md`](../../../../apps/mobile/CLAUDE.md): before rendering any API list response, look for the preprocessing web and desktop run between `useQuery` and their JSX — `dedupe*`, `coalesce*`, `filter*`, `*-display.ts`, a `useMemo` transform — and mirror all of it.

This is an instance of the behavioral-parity contract in the same file. Counts and visibility must agree across clients: the same filter yields the same N. Mobile may differ in interaction, never in what the user believes exists.

## Alternatives considered

**Deduplicate on the server so every client gets display-ready rows.** Rejected. Web and desktop already depend on the raw rows for behavior other than the inbox list, and the shaping is a presentation decision that varies by surface. Changing the endpoint's contract to fix one client's omission would push the divergence somewhere else.

**Reimplement the shaping from the described behavior rather than mirroring the source.** Rejected — it is what produced the bug. The rule points at the specific web-side function so a reader can diff the two.

## Consequences

Mobile carries a parallel copy of shaping logic that must follow the `packages/core` original when it changes. That copy is deliberate: mobile only imports types and pure functions from `@multica/core`, so the alternative is not sharing but silently diverging.

Any mobile screen consuming a list endpoint now has a required step before it is done, and the parity hazard is visible rather than discovered by comparing two screens side by side.
