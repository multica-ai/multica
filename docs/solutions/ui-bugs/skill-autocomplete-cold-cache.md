---
title: "Skill autocomplete cold cache — @mention dropdown empty on first use"
date: 2026-07-13
category: ui-bugs
module: editor
problem_type: ui_bug
component: mention-suggestion
symptoms:
  - "@ dropdown shows no skills even when workspace has active skills"
  - "Skills appear only after visiting the Skills settings page"
  - "Bug invisible in dev if developer visits Settings before testing comment composer"
root_cause: missing_cache_prime
resolution_type: code_fix
severity: medium
tags:
  - mention
  - autocomplete
  - react-query
  - cache
  - skill
  - tiptap
related_components:
  - mention-suggestion.tsx
  - workspace/queries.ts
---

# Skill autocomplete cold cache — @mention dropdown empty on first use

## Problem

`buildSyncItems()` in the mention suggestion pipeline reads workspace skills (and agents, squads, members) from the TanStack Query cache via `getQueryData()`, but nothing primes that cache before the user types `@`. For users who haven't visited the Skills settings page, the cache is empty on first use and the `@` dropdown shows no skills.

## Symptoms

- The `@` autocomplete dropdown in the comment composer shows no skills even when the workspace has active skills defined.
- Members, agents, and squads also appear empty on a cold cache, though these are more commonly primed by other pages (agent list, member management).
- After visiting the Skills settings page (which fires `useQuery(skillListOptions(wsId))`), skills appear correctly in the `@` dropdown because the cache is now warm.
- The bug is invisible in development if the developer visits Settings before testing the comment composer.

## What Didn't Work

The Skills settings page was expected to be the canonical fetch point, with the mention suggestion factory simply reading from cache. This assumed the user would have visited Settings first. The agent creation flow and workspace layout do not fetch skills. For many users, the comment composer (`@` mention) is the first touchpoint, so the cache is cold at that moment.

Prior sessions (avatar chip implementation, task-run-comment linkage) read `mention-suggestion.tsx` for rendering analysis but never investigated its data-loading mechanics — the suggestion bar was treated as stable infrastructure whose cache behavior was assumed correct.

## Solution

Added a cache-warming call inside `createMentionSuggestion` (the TipTap suggestion factory), executed once at factory construction time:

```ts
// packages/views/editor/extensions/mention-suggestion.tsx
// Inside createMentionSuggestion(), after pluginKey creation:

const ensureCaches = () => {
  const wsId = getCurrentWsId();
  if (!wsId) return;
  void qc.ensureQueryData(skillListOptions(wsId));
  void qc.ensureQueryData(agentListOptions(wsId));
  void qc.ensureQueryData(squadListOptions(wsId));
  void qc.ensureQueryData(memberListOptions(wsId));
};
// Fire once on factory construction; subsequent mount of the composer
// hits a warm cache.
ensureCaches();
```

This fires for all four entity types (skills, agents, squads, members) even though skills were the primary symptom, because all four share the same cache-only-read pattern in `buildSyncItems`.

The test stub (`fakeQc` in `mention-suggestion.test.tsx`) also needs an `ensureQueryData` no-op so tests that pre-populate the cache directly don't break.

## Why This Works

`queryClient.ensureQueryData(options)` has two behaviors:

1. **Cache warm** — returns the cached data immediately (no network request, effectively a no-op).
2. **Cache cold** — issues the fetch and stores the result, so subsequent `getQueryData` calls find data.

It is idempotent and safe to call repeatedly. By calling it at factory construction (which happens once when the editor mounts), all four entity caches are guaranteed warm by the time the user types `@` and `buildSyncItems` runs. The `void` discard is intentional — we don't need to await the fetch; we just need to trigger it so the cache is populated before the synchronous `getQueryData` reads.

## Prevention

When a component reads from `queryClient.getQueryData()` (cache-only read without a fetch trigger), ensure that one of the following exists somewhere upstream in the render tree or initialization path:

1. A `useQuery(options)` hook that shares the same `queryKey` — both fetches and subscribes.
2. A `queryClient.ensureQueryData(options)` call at an appropriate lifecycle point (mount, factory init, route entry).
3. A `queryClient.prefetchQuery(options)` call in an effect or loader.

If none of these exist, the cache-only read will silently return stale/missing data. **Code review should flag any `getQueryData` call that lacks a corresponding upstream prime for the same query key.**

A useful grep for review:

```bash
# Find getQueryData calls without a matching ensureQueryData or useQuery
rg "getQueryData" --type ts | rg -v "ensureQueryData|useQuery|prefetchQuery"
```

## Related Issues

- `docs/solutions/ui-bugs/mention-hover-card-inconsistency.md` — related to mention system but addresses hover card UI consistency, not data loading
- GitHub #1039 — agent mention renders empty after selection (same mention pipeline, different symptom: rendering phase vs. suggestion fetching phase)

## Related Artifacts

- Ideation: `docs/ideation/2026-07-13-skill-mention-ideation.html`
- Plan: `docs/plans/2026-07-13-001-refactor-mention-type-registry-plan.md`
- Fix commit: PR #5346 (`fix(editor): prefetch mentionable-entity queries when suggestion opens`)
