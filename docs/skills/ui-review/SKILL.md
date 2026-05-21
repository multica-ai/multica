---
name: ui-review
description:
  Run a comprehensive UI quality review on changed components before commit or
  PR. Covers accessibility, responsiveness, dark mode, performance, copy clarity,
  design system compliance, error handling, and animation. Use when the user says
  "review UI", "check the UI", "UI review", or before committing UI changes.
---

# UI Review

Run this skill after every UI change, before committing or opening a PR.
It consolidates the full quality pipeline into a single pass.

## Scope

Identify which files changed:

```bash
git --no-pager diff --name-only HEAD | grep -E '\.(tsx|css)$'
```

If no unstaged changes, diff against the base branch:

```bash
git --no-pager diff --name-only origin/main...HEAD | grep -E '\.(tsx|css)$'
```

Read every changed `.tsx` file completely before starting.

## Pipeline

Run each check below **in order**. For each check, report findings as a
markdown table: `| Finding | Severity | Line | Recommendation |`. Severity
levels: 🔴 Critical, 🟠 High, 🟡 Medium, 🟢 Low.

After all checks, produce a **summary scorecard** and a list of **fixes to
apply**.

Then **implement all fixes** that are Critical or High severity. Ask the user
before implementing Medium or Low fixes if there are more than 5.

After fixing, run `pnpm typecheck && pnpm test` to verify nothing broke.

---

### 1. Accessibility audit

For every interactive element (button, link, input, slider, switch, select,
tab) in the changed components:

- Does it have an accessible name? (`aria-label`, visible text, `htmlFor`/`id`)
- Does it meet 44×44px minimum touch target? Check `h-*`, `w-*`, `size` prop.
  The design system sizes: `sm` = 32px, `default` = 40px, `lg` = 48px,
  `icon` = 40px, `icon-sm` = 32px, `icon-lg` = 48px.
- Does it have focus-visible styles? (Inherited from shadcn/ui base components
  is OK.)
- Do all images have `alt` text?
- Do decorative SVGs have `aria-hidden="true"`?
- Does animated content respect `prefers-reduced-motion`? Check for
  `motion-reduce:` Tailwind prefix or JS media query check.

### 2. Responsiveness

Trace the layout at three viewport widths:

- **320px** (small phone): Does content overflow? Do flex-wrap containers wrap gracefully?
- **768px** (tablet): Do `sm:` breakpoint classes activate correctly?
- **1024px+** (desktop): Do `lg:` breakpoint classes work?

Check for mobile landscape (viewport height ~320px):

- Does `max-h-[90vh]` leave room for all controls?
- Are there any `min-h-[Npx]` values that exceed available space?

### 3. Dark mode and theming

- Are there any hard-coded color classes (`text-slate-*`, `bg-gray-*`,
  `text-white`, `bg-black`, etc.) that don't use design tokens?
- Design tokens to use: `text-foreground`, `text-muted-foreground`,
  `bg-background`, `bg-muted`, `border-border`, `text-primary`,
  `text-destructive`, `text-success`, etc.
- If a hard-coded color is intentional (e.g., dark canvas for image editing),
  add `dark:` variant.
- No `dark:` variant needed if using CSS variable tokens (they auto-switch).

### 4. Design system compliance

Compare against established patterns in the codebase:

- **Dialog structure**: `DialogHeader` → `DialogTitle` + `DialogDescription` →
  content with `space-y-4 py-2` → `DialogFooter` with `variant="outline"`
  Cancel + `variant="default"` primary.
- **Loading buttons**: `{isPending && <Loader2 className="h-4 w-4 animate-spin mr-2" />}` +
  always show text (don't replace text with "Loading...").
- **Dialog max-height**: `max-h-[90vh]`.
- **Button sizes**: Use design system sizes (`sm`, `default`, `lg`, `icon`).
  Don't override with custom `h-*` unless matching an existing system size.
- **Icon sizes**: `h-4 w-4` for standard buttons, `h-3.5 w-3.5` for tab
  triggers and compact link buttons.
- **Toast titles**: Sentence case. No Title Case.
- **Toast structure**: `{ title, description, variant }`. Use
  `variant: 'destructive'` for errors.
- **Named exports** for components (not default).
- **Typography**: `font-display` for headings, `text-muted-foreground` for
  secondary text.
- **Package boundaries**: `packages/ui/` must not import `@multica/core`.
  `packages/views/` must not import `next/*` or `react-router-dom`.
  `packages/core/` must not import `react-dom` or `localStorage`.

### 5. Performance

- Are `useCallback`/`useMemo` used for:
  - Handlers passed as props to memoized child components?
  - Expensive computations called on every render?
- Are there inline arrow functions in JSX that defeat `React.memo`?
- Are child components that receive frequently-changing props wrapped in
  `React.memo`?
- Is `loading="lazy"` used for images loaded from CDN URLs?

### 6. Copy and microcopy clarity

- Are all user-facing strings in sentence case?
- Are error messages actionable? Do they tell the user what to do next?
- Are status messages consistent in tone?
- Do keyboard shortcut tooltips show platform-aware keys? (`⌘` on Mac,
  `Ctrl` on Windows/Linux.)
- Are button labels unambiguous?

### 7. Error handling and resilience

- Do images have `onError` handlers?
- Do async operations have loading, error, and success states?
- Are there double-submit guards on form buttons?
- Are blob URLs properly revoked?
- Are `requestAnimationFrame` / `setTimeout` callbacks safe if the component
  unmounts?

### 8. Animation and motion

- Do transitions use GPU-accelerated properties only? (`transform`, `opacity`,
  `filter`.)
- Are durations under 300ms for feedback, under 500ms for transitions?
- Do all animations have `motion-reduce:transition-none` or check
  `prefers-reduced-motion`?

### 9. Polish

- Are icon sizes consistent within the same hierarchy level?
- Are spacing classes from a consistent scale? (`gap-2`, `gap-3`, not
  `gap-1.5` unless intentional.)
- Is there dead code, commented-out code, or `console.log` statements?
- Are all exports used? Any unused imports?

---

## Output format

After running all checks, produce:

```markdown
## UI Review Summary

| Check | Status | Issues |
|-------|--------|--------|
| Accessibility | ✅/⚠️/❌ | N issues |
| Responsiveness | ✅/⚠️/❌ | N issues |
| Dark mode | ✅/⚠️/❌ | N issues |
| Design system | ✅/⚠️/❌ | N issues |
| Performance | ✅/⚠️/❌ | N issues |
| Copy clarity | ✅/⚠️/❌ | N issues |
| Error handling | ✅/⚠️/❌ | N issues |
| Animation | ✅/⚠️/❌ | N issues |
| Polish | ✅/⚠️/❌ | N issues |

**Score: N/10**

### Fixes applied
- ...

### Remaining (user decision needed)
- ...
```

Then implement the Critical and High fixes, run `pnpm typecheck && pnpm test`,
and report results.
