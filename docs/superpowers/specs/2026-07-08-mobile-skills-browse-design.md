# Mobile Skills Browse (More page) — Design

Date: 2026-07-08
Status: Approved

## Context

Desktop/web exposes a top-level "Skills" section (`packages/views/skills/`)
that mobile has never had. This is round 1 of a 4-round effort to bring
suitable desktop-only sidebar sections onto mobile's More page (the other
three, in order: Runtimes, Usage, Agents — each gets its own spec/plan).
Desktop's Skills page is a full CRUD workspace-management surface: create
dialog, a `ListGrid` table with checkboxes/sorting/batch actions, and a
detail page with an in-browser file-tree editor (`FileTree` + `FileViewer`)
that edits `SKILL.md` and any attached files directly.

None of that authoring surface belongs on a phone. What's valuable on
mobile is being able to **look up what a skill does** — its description,
which agents use it, and its file contents — while away from a desktop.

## Goal

Add a read-only Skills browse experience to the mobile More page: list →
detail → file content viewer. No creation, editing, deletion, or
skill-to-agent assignment changes — all of that stays desktop-only.

## Non-goals

- Create / edit / delete a skill.
- Edit `SKILL.md` or any attached file content.
- Change which agents a skill is assigned to.
- Import a skill from a local runtime, ClawHub, Skills.sh, or GitHub.
- Search, column sorting, or batch actions (desktop `ListGrid` affordances
  that don't map to a phone-sized flat list).

## Changes

### 1. Nav entry

Add a "Skills" `NavRow` to the More page's existing "Workspace"
`SectionGroup` (`apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`), after
Projects. Pushes `more/skills`.

### 2. Data layer — `apps/mobile/data/queries/skills.ts` (new)

Mirrors `packages/core/workspace/queries.ts`'s shape, same 3-segment key
factory convention as every other mobile feature:

```ts
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const skillKeys = {
  all: (wsId: string | null) => ["skills", wsId] as const,
  list: (wsId: string | null) => [...skillKeys.all(wsId), "list"] as const,
  detail: (wsId: string | null, id: string) =>
    [...skillKeys.all(wsId), "detail", id] as const,
};

export const skillListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: skillKeys.list(wsId),
    queryFn: ({ signal }) => api.listSkills({ signal }),
    enabled: !!wsId,
  });

export const skillDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: skillKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getSkill(id, { signal }),
    enabled: !!wsId,
  });
```

`apps/mobile/data/api.ts` gains `listSkills` / `getSkill` methods using the
`fetchValidated` helper, hitting the same `GET /api/skills` /
`GET /api/skills/:id` endpoints desktop uses — no backend changes. Response
schemas: reuse `SkillSummary`/`Skill` types from `@multica/core/types`
(`import type` only, per the mobile sharing whitelist); define Zod schemas
and fallbacks in `apps/mobile/data/schemas.ts` alongside the existing ones.

"Used by N agents" reuses `selectSkillAssignments` from
`packages/core/workspace/queries.ts` directly — it's a pure function over
`Agent[]`, on the mobile-safe whitelist (types + pure functions). The list
and detail screens each also query mobile's existing
`agentListOptions(wsId)` (`apps/mobile/data/queries/agents.ts`, already
used elsewhere) and pass the result through `selectSkillAssignments` to get
per-skill counts — no new endpoint needed.

Origin badge: mobile's import whitelist is `@multica/core` only (types +
pure functions) — `packages/views/skills/lib/origin.ts` doesn't qualify
even though `readOrigin()`/`totalFileCount()` are pure, since they live in
`packages/views`, not `packages/core`. Mirror, don't import: copy the same
tiny logic into a new `apps/mobile/lib/skill-origin.ts`, documenting at the
top that it mirrors `packages/views/skills/lib/origin.ts` (same pattern
mobile already uses for realtime WS updaters — see mobile CLAUDE.md
"Mobile-owned updaters").

### 3. List screen — `more/skills.tsx` (new)

Pushed Stack route, registered in `[workspace]/_layout.tsx` with a native
header (title from a new `skills.json` namespace), following the same
shape as `more/projects.tsx`: `FlatList` + `RefreshControl`, client-side
sort by `updated_at` desc, loading skeleton, error state.

Each row: skill name, description (1 line, truncated), "used by N agents"
count, origin badge icon (manual / imported — reusing the same iconography
desktop uses for at-a-glance recognition, not a new icon set). No search
bar, no create button, no checkboxes.

### 4. Detail screen — `more/skills/[id].tsx` (new)

Pushed Stack route. Shows:

- Name, description
- Creator (`ActorAvatar`, already used elsewhere on mobile) + created/updated timestamps via `timeAgo()` (`apps/mobile/lib/time-ago.ts`, already used by `project-row.tsx`/`comment-card.tsx`/etc.)
- Origin badge
- "Used by" list: agent name/avatar chips for each agent in
  `selectSkillAssignments(agents).get(skill.id)`, or an empty state if none
- Flat file list: "SKILL.md" (synthesized — not a real `SkillFile` row) plus
  every entry in `skill.files`, each showing its `path`

Tapping a file pushes the file viewer.

### 5. File viewer — `more/skills/[id]/file/[...path].tsx` (new)

Read-only content view, one Stack push per file. File paths can be nested
(e.g. `scripts/helper.py`), so this must be an Expo Router catch-all
segment (`[...path]`), not a single `[path]` segment — a single segment
can't carry a slash. `SKILL.md` is pushed with the literal one-element
path `["SKILL.md"]`, which is safe to treat as a real path segment since it
never collides with an entry in `skill.files` (those are separate,
attached files — `content` and `files` are distinct fields on `Skill`).
Content source: for `SKILL.md`, `skill.content`; otherwise the matching
`SkillFile.content` found by joining the catch-all segments back into a
path and matching against `skill.files[].path`. No new endpoint — `Skill`
from the detail response already carries full file bodies.

Rendering: `.md` files (including the `SKILL.md` sentinel) render through
the existing `<Markdown>` wrapper (`@/lib/markdown`, already used for issue
descriptions/comments). Every other extension renders as plain monospaced
text inside a `ScrollView`. No editing, no save, no syntax highlighting
beyond what `<Markdown>` already does for `.md`.

### 6. i18n

New `apps/mobile/locales/{en,zh-Hans}/skills.json` namespace, following the
established 9-namespace pattern (list screen title/empty-state, detail
labels, origin badge labels, file-viewer title). "Skill" itself has no
settled Chinese translation (per `conventions.mdx`'s entity glossary) —
UI strings keep it lowercase English, matching how `issue`/`task` are
handled today.

## Testing

No new test files — matches how prior mobile list/detail screens in this
project were verified (typecheck + lint + manual pass), consistent with
`more/projects.tsx`/`more/pins.tsx` having no dedicated test files either.

## Verification

1. `pnpm --filter @multica/mobile typecheck`
2. `pnpm --filter @multica/mobile lint`
3. `pnpm --filter @multica/mobile test` (locale parity)
4. Manual: tap Skills row on More page → list renders with correct counts
   and origin badges → tap a skill → detail shows creator/timestamps/used-by
   → tap SKILL.md → markdown renders → back → tap an attached file (if any
   skill in the test workspace has one) → plain text renders. Confirm both
   languages render correctly. Confirm empty state (a skill used by zero
   agents, and a workspace with zero skills) renders sensibly.
