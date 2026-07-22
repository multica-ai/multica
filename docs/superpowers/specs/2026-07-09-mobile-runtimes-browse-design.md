# Mobile Runtimes Browse (More page) — Design

Date: 2026-07-09
Status: Approved

## Context

This is round 2 of a 4-round effort to bring suitable desktop-only sidebar
sections onto mobile's More page (round 1, Skills, already shipped —
`docs/superpowers/specs/2026-07-08-mobile-skills-browse-design.md`). Rounds
3 and 4 (Usage, Agents) are separate future specs.

Desktop/web's Runtimes section (`packages/views/runtimes/`) is a full
management surface: connect a new runtime (cloud/local/remote), delete a
runtime, configure custom runtime profiles and pricing, trigger a CLI
update and poll its status, and view per-runtime usage charts. None of that
authoring/action surface belongs on a phone.

What's valuable on mobile is checking **whether a runtime is online**,
**which agents are configured to use it**, and **whether it needs a CLI
update** — while away from a desktop.

Mobile already has a head start here: `apps/mobile/data/queries/runtimes.ts`
already fetches the full runtime list (it feeds the presence-dot system
today), and desktop itself has no separate detail endpoint — its detail
page finds the runtime by id in the already-fetched list. Round 2 is
mostly a UI layer on data mobile already has.

## Goal

Add a read-only Runtimes browse experience to the mobile More page: list →
detail. No creation, deletion, profile/pricing configuration, update
triggering, or usage charts — all of that stays desktop-only.

## Non-goals

- Connect a new runtime (cloud/local/remote).
- Delete a runtime or configure custom runtime profiles/pricing.
- Trigger a CLI update or poll an update job's status — the detail screen
  only ever shows a static "update available" signal, never a button.
- Per-runtime usage/cost charts (round 3's territory).
- Tap-through from the "agents on this runtime" list to an agent detail
  screen — round 4 (Agents) hasn't shipped yet, so this list is static
  name/avatar rows, not navigable.

## Changes

### 1. Nav entry

Add a "Runtimes" `NavRow` to the More page's existing "Workspace"
`SectionGroup` (`apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`), after
Skills. Pushes `more/runtimes`.

### 2. Data layer

**Reused as-is:** `runtimeListOptions(wsId)` in
`apps/mobile/data/queries/runtimes.ts` already exists (built for the
presence-dot system) and already returns `RuntimeDevice[]` via the existing
`api.listRuntimes` method. Both the list and detail screens use it
directly — no new endpoint, no schema changes.

**New — `latestCliVersionOptions()`** added to the same
`data/queries/runtimes.ts` file. Mirrors desktop's
`packages/core/runtimes/queries.ts` version: a plain `fetch` against
GitHub's public releases API (`https://api.github.com/repos/multica-ai/multica/releases/latest`),
no backend call, 10-minute-ish staleness is acceptable so this is a
`queryOptions` with no special invalidation.

**New — `apps/mobile/lib/runtime-health.ts`.** `deriveRuntimeHealth` and
the `RuntimeHealth` type themselves are pure functions/types in
`packages/core/runtimes` and are directly importable (mobile's whitelist
covers `@multica/core` pure functions). What's NOT importable is the
presentational mapping (health → dot color, health → label) — that lives
in `packages/views/runtimes/components/shared.tsx`, out of mobile's
whitelist. Mirror only that small mapping table into this new file;
`deriveRuntimeHealth` itself is imported, not copied.

**New — `apps/mobile/lib/runtime-update-check.ts`.** Desktop's
"does this runtime need an update" comparison (`isNewer` / `stripV` /
`runtimeNeedsUpdate`) lives in `packages/core/runtimes/hooks.ts` but is
NOT exported from that module — only the two hooks built on top of it are
(`useMyRuntimesNeedUpdate`, `useUpdatableRuntimeIds`), and both call
`packages/core`'s own `runtimeListOptions`/`latestCliVersionOptions`
internally, binding to a different key-factory instance than mobile owns
(the same hazard the mobile CLAUDE.md's "Mobile-owned updaters" section
documents for realtime WS updaters). Mirror the three small pure functions
into this new file instead of trying to partially reuse the hooks.

### 3. List screen — `more/runtimes.tsx` (new)

Pushed Stack route, registered in `[workspace]/_layout.tsx` with a native
header. `FlatList` + `RefreshControl`, following the same shape as
`more/skills.tsx`. Sort: online first, then `last_seen_at` desc (surfaces
live runtimes at the top).

Each row: name, provider (plain text label — no `ProviderLogo` image asset
mirroring, that's desktop-only polish), a health dot (color from the
mirrored mapping in `lib/runtime-health.ts`), a visibility icon
(private/public), last-seen relative time (reusing `apps/mobile/lib/time-ago.ts`).

### 4. Detail screen — `more/runtimes/[id].tsx` (new)

Pushed Stack route. Found by filtering the already-fetched
`runtimeListOptions(wsId)` list by id — no separate query, matching
desktop's own approach. Shows:

- Name, provider, mode (`local`/`cloud`), visibility
- Owner (resolved via the existing `useActorLookup` hook, same as Skills'
  creator resolution — `getName("member", runtime.owner_id)`)
- Health badge (dot + label) via `lib/runtime-health.ts`
- Device info (`runtime.device_info`, displayed as a single raw string —
  no splitting into hostname/version halves; that's a desktop nicety not
  worth the added parsing surface for a read-only view)
- Created / updated timestamps (`timeAgo()`)
- CLI version (from `runtime.metadata.cli_version`, matching desktop's
  `readRuntimeCliVersion` field-read — mirrored inline, it's a one-line
  read) and, only when desktop's exact gating conditions hold
  (`runtime_mode === "local"`, `runtime.owner_id === current user id`,
  `runtime.metadata.launched_by !== "desktop"`, and
  `isNewer(latestVersion, cliVersion)` from the mirrored check), a static
  "update available" badge — never a button, never a polled status.
- "Agents on this runtime" section: agents from the existing
  `agentListOptions(wsId)` query filtered by `agent.runtime_id === runtime.id`,
  rendered as static name + `ActorAvatar` rows (no navigation — mirrors
  Skills' "used by" section structurally, but this list shows configured
  association, not live task-execution state, and there is no agent
  detail screen to link to yet).

### 5. i18n

New `apps/mobile/locales/{en,zh-Hans}/runtimes.json` namespace, following
the established pattern. "Runtime" is not one of the mixed-rule trio
(`issue`/`skill`/`task`) — it's a fully-translated concept like
`project`/`workspace`. Desktop's own precedent (confirmed in
`packages/views/locales/zh-Hans/layout.json`'s sidebar entry) uses "运行时"
for the nav label; mobile's `more_page.nav.runtimes` key and every other
zh-Hans string in the new namespace use "运行时" throughout, mirroring that
existing product term rather than inventing a new one.

## Testing

No new test files — matches the precedent from Skills-browse (list/detail
screens verified via typecheck + lint + manual pass, not new unit tests).

## Verification

1. `pnpm --filter @multica/mobile typecheck`
2. `pnpm --filter @multica/mobile lint`
3. `pnpm --filter @multica/mobile test` (locale parity)
4. Manual: tap Runtimes row on More page → list renders with correct
   health dots/visibility icons/last-seen times → tap a runtime → detail
   shows owner/health/device info/timestamps → confirm the CLI-update
   badge only appears for a runtime that is local, owned by the signed-in
   user, not desktop-launched, and genuinely behind the latest GitHub
   release (test against whatever the workspace's actual runtimes report —
   if none qualify, confirm the badge correctly does NOT appear rather than
   forcing a scenario) → confirm "Agents on this runtime" lists the right
   agents (cross-check against desktop's runtime detail page for the same
   runtime) → confirm both languages render correctly, including the
   zh-Hans "运行时" nav label.
