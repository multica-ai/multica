---
title: "Adaptive skill import dialog with cmdk Command primitive"
date: "2026-07-09"
module: "packages/views"
problem_type: "design_pattern"
component: "frontend_stimulus"
severity: "medium"
tags:
  - react
  - cmdk
  - command-palette
  - adaptive-ui
  - skill-import
  - react-hooks
  - i18n
---

## Context

The skill import dialog needed to handle three very different list shapes: an empty workspace with no skills to import, a small handful (1–2) that fit comfortably on a summary card, and a larger set (3+) that benefits from search and grouping. The naive approach — a single scrolling list of checkboxes regardless of count — produced poor UX in both extremes (empty state felt broken, 30+ items were unnavigable). The dialog also had to interoperate with `cmdk` / shadcn's `Command` primitives, which carry their own selection semantics and keyboard handlers.

## Guidance

**Derive the branch from data, not state.** Compute the UI branch directly from the source data (`runtimeSkills.length`) in render, never via a state variable synchronized by `useEffect`. This keeps the branch in lockstep with the list and avoids a transient state where `runtimeSkills` has updated but `branch` still reflects the previous length — a class of React bugs that shows up in tests as "the component rendered the wrong branch for one tick."

```tsx
const branch =
  runtimeSkills.length === 0
    ? "empty"
    : runtimeSkills.length <= 2
      ? "summary"
      : "search";
```

**Prefer `CommandPrimitive.Item` over shadcn `CommandItem` for custom selection UI.** shadcn's `CommandItem` is a thin wrapper around `cmdk`'s primitive that renders a built-in `CheckIcon` when the item is "selected." When you layer your own `Checkbox` inside the item (as `SkillItem` does to track which skills the user has opted to import), both the wrapper's check indicator and your checkbox fire, producing a double-toggle: `onSelect` from cmdk and `onToggle` from the inner checkbox both dispatch, leaving the item in the wrong state. Using `CommandPrimitive.Item` directly gives you the keyboard navigation and focus ring without the competing visual affordance:

```tsx
import { Command as CommandPrimitive } from "cmdk";

<CommandPrimitive.Item
  value={skill.id}
  onSelect={() => toggleSkill(skill.id)}
  className="..."
>
  <SkillItem skill={skill} />
</CommandPrimitive.Item>;
```

In the search branch, drop `SkillItem`'s own `onToggle` prop entirely — let `onSelect` be the single toggle entry point. In the summary branch, where the item is not rendered inside a `Command` tree, `SkillItem`'s `onToggle` remains the sole handler.

**Let the panel own scrolling.** Inside a `Command` palette, `CommandList` adds its own max-height scroll region. When the palette is already embedded in a dialog panel with its own scroll container, you get nested scrollers and broken arrow-key navigation past the viewport. Set `max-h-none` on `CommandList` (or pass it through `listProps`) so the panel's single scroll region is the only one:

```tsx
<CommandList className="max-h-none" />
```

**Hoist `useMemo` to the top level of the component.** If you're tempted to memoize a derived value inside an IIFE that returns early based on a branch, stop — React's rules of hooks require every hook call to execute unconditionally on every render. Move the `useMemo` to the component body above any conditional returns, and branch on its result afterward:

```tsx
// Wrong: useMemo inside a conditional block
const grouped = (() => {
  if (branch === "search") {
    return useMemo(() => groupByRoot(runtimeSkills), [runtimeSkills]);
  }
  return null;
})();

// Right: useMemo unconditionally at the top
const grouped = useMemo(() => groupByRoot(runtimeSkills), [runtimeSkills]);
if (branch !== "search") {
  return <SummaryCard skills={runtimeSkills} />;
}
return <SearchPalette grouped={grouped} />;
```

**Group "Other" for skills without a `root`.** Older daemon runtimes omit the `root` field on skill records. When grouping by root for the search palette, bucket `root === undefined` into an "Other" group rather than dropping them — otherwise those skills silently disappear in the most important view. Treat undefined as a first-class group key:

```ts
const grouped = useMemo(() => {
  const map = new Map<string, RuntimeSkill[]>();
  for (const s of runtimeSkills) {
    const key = s.root ?? "other"; // "Other" bucket for missing root
    (map.get(key) ?? map.set(key, []).get(key)!).push(s);
  }
  return map;
}, [runtimeSkills]);
```

**i18n: keep "skill" in English in zh-Hans.** Per the project conventions (`apps/docs/content/docs/developers/conventions.zh.mdx`), product nouns like "skill", "agent", "issue" are not translated in Chinese UI copy — write "skill" verbatim, use straight quotes (`"`, not `""`), and pair with a Chinese measure word when the noun is countable ("一个 skill", not "一个技能"). This keeps glossary alignment with the English UI and avoids the translation drift that surfaces when a term has both a Chinese rendering and an English rendering in circulation.

## Why This Matters

- **Single source of truth for branch state** eliminates an entire class of off-by-one render bugs where state and data disagree, and removes the `useEffect` that would otherwise need careful dependency tracking.
- **Using `CommandPrimitive.Item` directly** sidesteps a subtle but repeatable conflict between cmdk's selection model and custom selection UI — a conflict that surfaces as a double-toggle that only shows up under keyboard navigation, making it easy to miss in manual testing.
- **Single scroll region** makes arrow-key navigation behave predictably across the full list and eliminates the "scroll stops halfway" UX where the inner scroller hits its max-height while the outer panel is still scrollable.
- **Top-level `useMemo`** satisfies React's rules of hooks, which means the component passes `eslint-plugin-react-hooks` and doesn't break if React's reconciler changes call ordering assumptions.
- **"Other" bucket** preserves discoverability of skills from older daemons — a backward-compatibility concern that would otherwise be silent data loss.
- **Consistent i18n** reduces glossary drift and keeps the Chinese UI aligned with the English product terminology documented in the conventions source of truth.

## When to Apply

- Building a dialog, palette, or list whose shape changes materially with the count of items (empty / small / many).
- Composing cmdk / `Command` with a custom item component that already owns its own selection visual (checkbox, radio, toggle).
- Any `CommandList` rendered inside a container that already provides its own scroll region.
- Any component that derives multiple dependent values from a single data source — prefer computing them at the top level and branching on results, over state + effect synchronization.
- Grouping data by a field that older backends or daemons may omit — always provide an "Other"/"Unknown" fallback bucket.
- Writing zh-Hans UI copy that references product nouns defined in the conventions glossary.

## Examples

Skill import dialog branching (abbreviated):

```tsx
export function SkillImportDialog({ runtimeSkills }: Props) {
  const branch =
    runtimeSkills.length === 0
      ? "empty"
      : runtimeSkills.length <= 2
        ? "summary"
        : "search";

  // Grouped view computed unconditionally — only consumed in the search branch.
  const grouped = useMemo(() => {
    const map = new Map<string, RuntimeSkill[]>();
    for (const s of runtimeSkills) {
      const key = s.root ?? "other";
      const arr = map.get(key);
      if (arr) arr.push(s);
      else map.set(key, [s]);
    }
    return map;
  }, [runtimeSkills]);

  if (branch === "empty") return <EmptyState />;
  if (branch === "summary") return <SummaryCard skills={runtimeSkills} />;

  return (
    <Command>
      <CommandInput placeholder="Search skills..." />
      <CommandList className="max-h-none">
        <CommandEmpty>No skill found.</CommandEmpty>
        {Array.from(grouped.entries()).map(([root, skills]) => (
          <CommandGroup key={root} heading={root === "other" ? "Other" : root}>
            {skills.map((skill) => (
              <CommandPrimitive.Item
                key={skill.id}
                value={skill.id}
                onSelect={() => toggleSkill(skill.id)}
              >
                <SkillItem skill={skill} />
              </CommandPrimitive.Item>
            ))}
          </CommandGroup>
        ))}
      </CommandList>
    </Command>
  );
}
```

Note that in the search branch, `SkillItem` does **not** receive an `onToggle` prop — `onSelect` on the `CommandPrimitive.Item` is the single source of toggle intent. In the summary branch, `SkillItem` handles its own checkbox click via `onToggle` because it is not wrapped in a `Command` item.

## Related

- PR #5160: https://github.com/multica-ai/multica/pull/5160
- Existing patterns referenced during implementation:
  - `packages/views/issues/components/issue-detail.tsx:128-178` (subscriber picker)
  - `packages/views/issues/components/pickers/property-picker.tsx:111-144` (keyboard nav with `isImeComposing`)
  - `packages/views/search/search-command.tsx:500` (CommandPrimitive.Item usage)
  - `packages/views/editor/extensions/pinyin-match.ts` (CJK input matching)
- Conventions source of truth: `apps/docs/content/docs/developers/conventions.zh.mdx`
