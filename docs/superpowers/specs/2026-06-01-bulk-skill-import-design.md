# Bulk Skill Import — Design

**Date:** 2026-06-01
**Status:** Approved (design), pending implementation plan
**Branch:** `feat/bulk-skill-import`

## Context

Today a workspace can only add skills one at a time: create manually, import a
single skill by URL (ClawHub / Skills.sh / GitHub), or copy from a connected
runtime. A self-hoster who has a folder of skills on disk, or a GitHub repo that
holds many skills, has no way to load them in bulk — they must repeat the
single-skill flow N times.

This adds two bulk-ingestion front doors that share one engine:

1. **From a GitHub repo/folder URL** — paste a repo (or a subfolder of it) and
   import every skill found inside.
2. **From a local folder** — drop/select a folder in the browser and import
   every skill found inside.

Both reduce to the same core: **discover candidate skills in a tree → show a
checklist → import the selected ones concurrently → show a success/skip/fail
summary.** This mirrors the existing runtime bulk-import panel, which is the UX
and engineering precedent we copy.

## Goals

- Import many skills in one action from a GitHub repo/folder URL.
- Import many skills in one action from a locally-selected folder.
- Reuse existing import endpoints and validation; add the minimum new surface.
- Partial-success semantics: one bad skill never blocks the rest.

## Non-goals

- No new storage model (skills + files stay in Postgres TEXT as today).
- No arbitrary git/zip/tarball sources (GitHub only for the URL path).
- No overwrite/merge of existing skills — name conflicts are **skipped**.
- No translation of skill *content* — we only read frontmatter for name/desc.

## Skill-discovery convention

A skill is delimited **recursively by `SKILL.md`**: every `SKILL.md` found
anywhere in the tree is one skill. Its files are the sibling files in the same
directory and its subdirectories, up to the next `SKILL.md` boundary. This
works for flat (`skills/foo/SKILL.md`) and nested
(`.claude/skills/foo/SKILL.md`) layouts without special-casing.

Name/description come from the `SKILL.md` YAML frontmatter (reusing the existing
`parseSkillFrontmatter` logic on the server; a small equivalent on the client
for the folder path). If frontmatter lacks a name, fall back to the skill's
directory name.

## UX flow

Entry point: the existing method chooser in
[create-skill-dialog.tsx](packages/views/skills/components/create-skill-dialog.tsx)
gains **one new card, "Import a set"** (`bulk`). Selecting it opens a panel with
a **source toggle: GitHub repo | Local folder**. Everything after the source
step is shared:

1. **Source input**
   - GitHub: a URL field (`github.com/owner/repo` or
     `…/tree/<branch>/<subdir>`), "Discover" button.
   - Folder: a `webkitdirectory` file input + drag-drop target.
2. **Candidate checklist** — each row: name, description, file count, and an
   **"already exists"** badge when the name collides with a skill already in the
   workspace (computed against the loaded `listSkills`). Checkboxes; "select
   all"; rows that already exist are unchecked by default. Mirrors the runtime
   panel's list UI.
3. **Import** — concurrent (pool of 10), live progress.
4. **Summary** — counts and per-item status: imported / skipped / failed, with
   error detail. Dialog stays open on partial failure for retry.

The dialog widens for the `bulk` method (same as it does for `runtime`).

## Architecture

### Component A — Backend: GitHub discovery endpoint (NEW)

`POST /api/skills/discover` — body `{ "url": "<github repo/folder url>" }`,
returns the candidate list **without importing**:

```jsonc
{
  "candidates": [
    {
      "name": "code-reviewer",      // from frontmatter, fallback dir name
      "description": "…",
      "path": "skills/code-reviewer", // dir containing SKILL.md
      "import_url": "https://github.com/owner/repo/tree/<ref>/skills/code-reviewer"
    }
  ],
  "truncated": false   // true if more than the discovery cap were found
}
```

Implementation:
- Reuse the existing GitHub URL parsing / ref resolution
  (`detectImportSource`, `resolveGitHubRefAndPath`) from
  [skill.go](server/internal/handler/skill.go).
- Fetch the tree in **one** call via the GitHub Git Trees API
  (`GET /repos/{o}/{r}/git/trees/{sha}?recursive=1`), filter entries ending in
  `/SKILL.md` (or root `SKILL.md`), scoped to the subfolder if the URL pointed
  at one.
- For each, fetch only that `SKILL.md` to parse frontmatter (name/description).
  Supporting files are NOT fetched here — discovery stays cheap.
- Build a per-candidate `import_url` that the existing single-skill importer can
  consume verbatim.
- Workspace-scoped + auth via the same middleware as other skill routes. If the
  workspace has a connected GitHub App, use its token (as the single-skill
  importer does) for higher rate limits / private repos; otherwise anonymous
  (public repos only).
- Response schema added the same PR (zod on the client per API Response
  Compatibility), with a malformed-response test.

**Import of selected GitHub candidates reuses the existing**
`POST /api/skills/import { url }` — no new import endpoint. The client loops it
over the selected `import_url`s through the shared engine (Component C).

### Component B — Frontend: local folder discovery (client-only, NEW)

- A folder reader using `<input webkitdirectory>` + drag-drop (reuse the dedup
  pattern from
  [editor/extensions/file-upload.ts](packages/views/editor/extensions/file-upload.ts)).
- Recursively locate every `SKILL.md`; group sibling/descendant files (until the
  next `SKILL.md`) as that skill's files.
- A small TS frontmatter parser (the one intentional duplication of the Go
  parser) extracts name/description.
- Client-side filtering before send: skip binary files (extension list mirrored
  from the server), drop empties, enforce the same caps (1 MiB/file,
  8 MiB/skill, 128 files/skill) and surface a clear error if exceeded.
- Produces `CreateSkillRequest` payloads.

**Import of folder candidates reuses the existing** `POST /api/skills`
(`createSkill`) — server-side validation/caps apply as normal.

### Component C — Shared bulk import engine (NEW, small)

A `useBulkSkillImport` hook driving a unified task type:

```ts
type BulkImportTask =
  | { kind: "url"; url: string; name: string }       // GitHub path → importSkill
  | { kind: "payload"; data: CreateSkillRequest };   // folder path → createSkill
```

- Concurrency pool of 10 (copy the Promise-race pool from
  [runtime-local-skill-import-panel.tsx](packages/views/skills/components/runtime-local-skill-import-panel.tsx)).
- Per-item try/catch → result `{ name, status: "imported"|"skipped"|"failed", error?, skill? }`.
  A 409 name conflict maps to `skipped` (reuse `isNameConflictError` from
  [skills/lib/utils.ts](packages/views/skills/lib/utils.ts)).
- On completion: invalidate `workspaceKeys.skills(wsId)` and
  `workspaceKeys.agents(wsId)` **once**, then `setQueryData` to seed each
  imported skill's detail cache (same as the runtime panel).

The bulk panel is source-agnostic: GitHub and folder paths both produce a
candidate list and a `BulkImportTask[]`, then hand off to this engine + the
shared checklist/progress/summary views.

## Limits & edge cases

- **Batch cap:** at most 100 skills per bulk operation. If discovery finds more,
  show the count and that the list is capped — never silently truncate
  (`truncated: true` from the endpoint; explicit banner on the client).
- **Folder memory:** the browser reads the folder into memory; enforce a soft
  total-size ceiling with a clear error rather than hanging on huge trees.
- **Empty discovery:** "No SKILL.md found" with a one-line hint about the
  expected layout.
- **GitHub rate limit / private repos:** documented limitation — anonymous
  discovery is subject to GitHub's unauthenticated limit; connect a GitHub App
  for private repos and higher limits.
- **All-skipped / all-failed:** summary still shown; dialog stays open.

## Reuse map

| Concern | Reused from |
|---|---|
| Checklist + progress + summary UI | `runtime-local-skill-import-panel.tsx` |
| Concurrency pool | same |
| Single-skill GitHub import | `POST /api/skills/import` |
| Single-skill create | `POST /api/skills` (`createSkill`) |
| GitHub URL/ref parsing, frontmatter, caps, binary-skip, sanitize | `skill.go` |
| Conflict detection | `isNameConflictError` (`skills/lib/utils.ts`) |
| Folder drag-drop / dedup | `editor/extensions/file-upload.ts` |
| Query invalidation/seed | `workspace/queries.ts` keys |

**Net new:** one backend discovery endpoint + its schema/test; a client folder
reader + TS frontmatter parser; the `useBulkSkillImport` hook; the bulk panel
with the source toggle and the new chooser card.

## Testing

- **Backend (`go test`):** `POST /api/skills/discover` — flat layout, nested
  layout, subfolder-scoped URL, repo with zero SKILL.md, malformed/oversized
  tree, and a frontmatter-less skill (dir-name fallback). Plus a malformed
  GitHub-API-response test (fail-closed).
- **Client schema:** malformed discovery response → `parseWithFallback` returns
  the empty fallback (no crash).
- **Folder reader (`vitest`):** recursive SKILL.md grouping, binary skip,
  cap enforcement, frontmatter parse + fallback.
- **Engine (`vitest`):** concurrency cap respected, 409→skipped, partial
  failure produces the right summary, invalidation called once.
- **E2E (optional):** drop a fixture folder of 2 skills, import, assert both
  appear in the list and a duplicate is skipped.

## Verification

1. `make check` (typecheck, unit, Go, e2e).
2. Manual via the self-host docker stack: rebuild frontend, open Skills →
   New skill → Import a set → (a) paste a public GitHub repo with several
   skills, confirm discovery + import + summary; (b) select a local folder of
   skills, confirm the same. Confirm a name-collision shows "already exists" and
   ends up `skipped`.
