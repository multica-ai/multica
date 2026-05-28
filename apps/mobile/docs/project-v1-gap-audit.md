# Project surface — v1 gap audit (apps/mobile/)

**Status**: Pre-implementation. Pairs with `project-v1-plan.md` (the execution plan).
**Date**: 2026-05-20

## Context

Mobile is preparing the first user-facing release (not demo). Core use cases are **操作 issue / 派活 / 聊天**. This document inventories every gap between mobile's current Project surface and the web reference — both for the items addressed in this PR and the items consciously deferred to later PRs.

The gap inventory is the work product. The plan to close the in-scope gaps lives in `project-v1-plan.md`.

## Methodology

Compared mobile files in:
- `apps/mobile/app/(app)/[workspace]/more/projects.tsx`
- `apps/mobile/app/(app)/[workspace]/project/{new,[id],[id]/edit,[id]/add-resource,[id]/picker/lead}.tsx`
- `apps/mobile/components/project/*`

…against web reference in:
- `packages/views/projects/components/{projects-page,project-detail,project-resources-section,project-picker}.tsx`
- `packages/views/projects/components/modals/create-project.tsx`
- `packages/core/projects/` (queries, mutations, ws-updaters, schemas)

For each affordance: cite web file:line + mobile file:line + classify severity per the rules in `apps/mobile/CLAUDE.md` "Behavioral parity":

- 🔴 **Important** — parity violation; product semantics diverge between clients
- 🟡 **UX gap** — feature exists on web, useful on mobile, not critical for release semantics
- ⚪ **Intentional divergence** — small-screen incompatible, deliberately omitted on mobile
- 🟢 **Quality sweep** — hardcoded colors, hand-drawn icons; baseline alignment with the just-landed inbox / my-issues / Issues UI

## In-scope for v1 PR (🔴 + 🟢)

### 1. Tier B picker migration not finished

Two project pickers stayed on the legacy `<Modal transparent>` shell when `feat(mobile): migrate sheet modals to route-level pageSheet (cc61ae3a)` landed:

| File | Issue |
|---|---|
| `components/project/pickers/project-status-picker-sheet.tsx` | Still uses `<Modal transparent fade>` centered card |
| `components/project/pickers/project-priority-picker-sheet.tsx` | Same |

**Why this matters**: project detail's attribute row now has **mixed gestures** — tap *lead* gives native iOS pageSheet drag-to-dismiss; tap *status / priority* gives the old transparent-Modal centered card with backdrop tap dismiss. Mobile CLAUDE.md Lesson 6 "picker-row consistency" explicitly forbids mixed-gesture rows. `project/[id].tsx:69` already has a TODO comment acknowledging this.

### 2. Progress bar missing on detail

Web `project-detail.tsx:596-620` renders a horizontal progress bar with `done_count / issue_count` and percentage. Mobile detail page has **zero progress visibility** — the project schema (`packages/core/types/project.ts:17-18`) has `issue_count` and `done_count` fields, but mobile never reads them.

Users on mobile have no answer to "how done is this project?". Real behavioral parity violation.

### 3. View mode switcher + Board view on detail's issues

Web `project-detail.tsx:211-237, 311` exposes Board / List / Gantt selector for the issues section. Mobile `components/project/project-related-issues.tsx` hardcodes a **two-bucket "Open / Done" rollup** that doesn't exist on web — web groups by full `BOARD_STATUSES` (backlog/todo/in_progress/in_review/done/blocked).

This is *silent semantic divergence*: a user expects "Open" on mobile and "Backlog + Todo + In Progress + In Review + Blocked" on web to be the same five categories. They are not.

Gantt stays deferred (small-screen incompatible — see ⚪ section).

### 4. Create form has no draft persistence

Web `create-project.tsx:120-150` uses `useProjectDraftStore` to survive backgrounding — type title, get a call, come back, title still there. Mobile `project/new.tsx` has no draft store. Per mobile CLAUDE.md "cellular edge hazard": on flaky network or interrupted use, users lose work mid-form.

The pattern is already present in mobile via `new-issue-draft-store.ts` (landed in the Tier B sheet migration PR). New `new-project-draft-store.ts` should mirror that exact shape.

### 5. Hardcoded hex sweep (🟢 quality)

The just-landed UI baseline scrubbed `#71717a` / `#a1a1aa` style hex from inbox / my-issues / more-issues. Project domain is the last hold-out.

| File:line | Hex | Notes |
|---|---|---|
| `project-properties-section.tsx:135` | `#a1a1aa` | Chevron stroke |
| `project-resources-section.tsx:112` | `#71717a` | GitHub icon color |
| `project-related-issues.tsx:184` | `#71717a` | Chevron stroke |
| `project/[id].tsx:156` | `#71717a` | Android header menu fallback |
| `more/projects.tsx:108-127` | `#0a84ff` ×2 | Hand-drawn PlusButton SVG stroke |

All become `THEME[scheme].mutedForeground` (Ionicons color prop) or NativeWind semantic tokens. Dark-mode breakage is the failure mode they cause.

### 6. Hand-drawn SVG icons (🟢 quality, Principle 1)

| File:line | Replacement |
|---|---|
| `more/projects.tsx:108-127` PlusButton | `<IconButton name="add" />` |
| `project-properties-section.tsx:130-142` chevron | `<Ionicons name="chevron-forward" />` |
| `project-related-issues.tsx:168-191` chevron | `<Ionicons name="chevron-forward" />` |

Existing `IconButton` + `Ionicons` already used everywhere else in mobile. These three are pre-baseline legacy.

## Out of scope for this PR (recorded, not done)

### 🟡 UX gaps — defer to v1.1+

| Affordance | Reason for deferring |
|---|---|
| Pin / unpin project | Mobile has **no pin API methods and no "Pinned" consumer surface** (no pinned-items list anywhere). Building only the pin toggle button without a list to show pinned items = dead UI. Needs a dedicated PR that does pin endpoints + storage + a pinned-items surface end-to-end. |
| Inline title edit on detail header | Mobile routes to `/[id]/edit` for title changes — extra tap but functional. Inline edit on phones has keyboard-management issues; deferring acceptable. |
| Icon emoji picker on create + edit | Currently a 4-char TextInput. Needs a third-party emoji picker library or a built-in grid component. UX divergence but no functional gap. |
| Issue filter UI on Project detail | Consistent with my-issues / Issues — fine-grained filters (assignee / creator / label) are not in v1 anywhere on mobile. |
| Resources attachment at create time | Web allows selecting repos before project exists. Mobile creates first, then adds resources. Adds a step but not a parity violation. |
| Section collapse (Properties / Description / Resources) | Web pattern (MUL-2275). Small-screen sidebars don't need collapse — content is already short. |
| Description visibility on Detail | Currently only editable on `/edit`. Read-only display on detail screen could be added but lower priority. |
| Copy link menu item | Mobile has "Open on web" which serves a similar purpose. iOS native `Share.share` would also work. |

### ⚪ Intentional divergence — never

| Affordance | Why |
|---|---|
| Gantt view on Project detail | Web added scheduled-only Gantt (MUL-1881, 54368fd8). Phone screens are too narrow for meaningful Gantt rendering — Linear iOS / Asana iOS don't ship Gantt either. Permanent mobile divergence; document inline at the call site once the issues section is touched. |
| Breadcrumb in header | Web shows "Workspace > Project". Mobile uses native iOS Stack title bar (single line). Platform-correct divergence. |

## What's already aligned (no work needed)

- **Data layer hygiene**: `projectKeys` 3-segment factory, `queryFn { signal }` forwarding, `parseWithFallback` on every typed response
- **Mutations**: optimistic three-step (snapshot → patch → rollback) + settle invalidate for create / update / delete / resource add / resource remove
- **Realtime**: `useProjectRealtime` per-record, `useProjectsRealtime` listing-level, both via `useWSSubscriptions` + typed `ws.on<E>()`, scoped reconnect invalidate
- **Error surfacing**: matches MUL-2317 (`Alert.alert(err.message)` on mutation failures) — equivalent to web's toast pattern
- **Status / Priority / Lead enum coverage**: all values present (status: planned / in_progress / paused / completed / cancelled; priority: urgent / high / medium / low / none; lead: member / agent)
- **Resource CRUD**: add via route modal, remove via long-press + Alert (acceptable platform divergence from web's hover-delete)
- **List page**: web itself has no filter / sort / scope tabs → mobile correctly mirrors the minimal surface

## Out of scope at the *release* level (record only, separate PRs)

These are blockers for mobile v1 release but unrelated to Project:

- **Push notifications (APNs registration + tap-to-issue routing)** — blocks the 派活 use-case loop
- **Chat surface full audit** — wasn't depth-reviewed yet
- **Error boundaries + Sentry crash reporting** — release telemetry baseline
- **App Store metadata** (icon, splash, launch storyboard, privacy manifest, version, About / privacy-policy links)
- **First-launch onboarding** (welcome → login → workspace pick/create → optional tour)
- **i18n (Chinese)** — v1 ships English-only by default; can change if product disagrees

Each gets its own PR / planning doc later. They are NOT addressed in the Project v1 PR.

## Severity summary

| Tier | Count | In this PR |
|---|---|---|
| 🔴 Important (parity) | 4 | Yes — all four |
| 🟢 Quality sweep | 2 | Yes — all (hex + SVG) |
| 🟡 UX gap | 7 | No — deferred to v1.1+ |
| ⚪ Intentional divergence | 2 | No — permanent |

PR scope = **6 items** (Tier B picker collapse counts as one).
