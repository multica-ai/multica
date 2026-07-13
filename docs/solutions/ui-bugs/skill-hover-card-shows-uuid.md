---
title: "Skill hover card shows UUID instead of skill name"
date: 2026-07-13
category: ui-bugs
module: "packages/views/editor"
problem_type: ui_bug
component: skill-profile-card
symptoms:
  - "Hover card header shows raw UUID instead of human-readable skill name"
  - "Bug is purely visual — no error thrown, no functional breakage"
root_cause: missing_api_name_resolution
resolution_type: code_fix
severity: medium
tags:
  - skill
  - hover-card
  - mention
  - uuid
  - name-resolution
related_components:
  - skill-profile-card.tsx
  - mention-hover-card.tsx
---

# Skill hover card shows UUID instead of skill name

## Problem

When a user hovers over a skill mention in the editor, the hover card displays the raw UUID identifier instead of the human-readable skill name. `MentionHoverCard` passes `skillName={label ?? id}` to `SkillProfileCard` — when the markdown label is empty (common for autocomplete-generated mentions where the label is the UUID itself), the UUID leaks into the rendered header.

## Symptoms

- Hover card header shows a raw UUID string instead of "my-skill-name".
- Description and frontmatter sections render correctly once the detail query resolves, but the name field is visually broken during loading and sometimes persists if the prop is the UUID.
- No error is thrown; the bug is purely a data-identity issue at the rendering boundary.

## What Didn't Work

1. **Relying solely on the `skillName` prop.** The prop is the markdown label from the `@skill(...)` syntax. In autocomplete-generated mentions, the label is the skill's slug or UUID — not its display name. The prop alone cannot guarantee a meaningful name.

2. **Empty-string guard on the prop (`skillName || "Untitled"`).** This masks the UUID but produces a generic fallback that doesn't improve UX. It doesn't fix the root cause: the canonical name is available from the API but isn't being fetched.

## Solution

`SkillProfileCard` now fetches the skill detail from the API and uses the response as the authoritative name source.

**Key changes in `packages/views/editor/skill-profile-card.tsx`:**

1. **Added `useQuery(skillDetailOptions(wsId, skillId))`** to fetch the full skill detail (name, description, content).

2. **API name supersedes the prop:**
   ```ts
   const resolvedName = skillDetail?.name ?? skillName;
   ```

3. **Skeleton fallback when both sources are empty:**
   ```tsx
   {resolvedName ? (
     <p className="truncate text-sm font-semibold">{resolvedName}</p>
   ) : (
     <Skeleton className="mb-1 h-4 w-3/4" />
   )}
   ```
   Instead of rendering the UUID as text, the component shows a loading skeleton.

4. **Description follows the same pattern:**
   ```ts
   const resolvedDescription = skillDetail?.description ?? skillDescription;
   ```

**The `MentionHoverCard` caller (`mention-hover-card.tsx`)** continues to pass `label ?? id` as `skillName`. This is now safe because `SkillProfileCard` no longer treats the prop as the source of truth — it's a hint superseded by the API response. Any future caller gets the same protection automatically.

## Why This Works

- The `skillDetailOptions` query is already cached and prefetched in nearby flows.
- The skeleton placeholder provides good UX during the brief loading window without showing misleading data.
- The fix is backward-compatible: if the detail query fails or is slow, the prop still serves as a graceful fallback.
- The defense is at the consumer (`SkillProfileCard`), not the producer, so all callers benefit.

## Prevention

- **Rule:** Never render a UUID or slug as visible text in a profile/preview card without first attempting to resolve the human-readable name from the API. If the API is unavailable, show a skeleton or placeholder, not the raw identifier.
- **Pattern:** Components that display entity names should own their data resolution internally (via `useQuery`), treating incoming name props as hints superseded once the canonical source resolves. This prevents data-identity bugs at rendering boundaries.
- **Test:** `skill-profile-card.test.tsx` now includes a case where `skillId` is set but `skillName` is undefined, asserting the skeleton renders and the UUID never appears in the DOM.

## Related Issues

- `docs/solutions/ui-bugs/skill-autocomplete-cold-cache.md` — sibling bug in the same mention pipeline (different symptom: missing data in dropdown vs. wrong data in hover card)
- `docs/solutions/ui-bugs/mention-hover-card-inconsistency.md` — related to mention hover card rendering patterns

## Related Artifacts

- Fix commit: PR #5346 (`fix(skills): resolve skill name via detail query + render frontmatter`)
- Plan: `docs/plans/2026-07-13-001-refactor-mention-type-registry-plan.md`
