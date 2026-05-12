---
title: "feat: Prominent issue identifier badge with copy action"
type: feat
status: active
date: 2026-05-12
origin: docs/brainstorms/2026-05-12-issue-identifier-badge.md
---

# Prominent Issue Identifier Badge on Detail Page

## Summary

Render the issue identifier as a visually distinct, clickable badge in the PageHeader breadcrumb on the issue detail page. Clicking the badge copies the identifier to clipboard with toast confirmation. Add a `copyIdentifier` handler to `useIssueActions`.

## Requirements

- R1. The issue identifier is visually distinct from breadcrumb navigation text
- R2. Clicking the identifier copies it to clipboard
- R3. Copy success/failure shows toast feedback
- R4. The change works on both web and desktop (shared `packages/views/` code)
- R5. Mobile layout is not broken

## Scope Boundaries

- Does not add keyboard shortcuts
- Does not change document/tab title
- Does not modify list view, board view, or other surfaces
- Does not extract a shared `IssueIdentityBar` component (can follow up later)
- Does not fuse identifier into the title or change `TitleEditor`

## Context & Research

### Relevant Code and Patterns

- `packages/views/issues/components/issue-detail.tsx` — PageHeader renders identifier at lines 708-710 as `text-muted-foreground tabular-nums`
- `packages/views/issues/actions/use-issue-actions.ts` — has `copyLink()` using `navigator.clipboard.writeText()` + `toast.success()` / `toast.error()`
- `packages/views/search/search-command.tsx` — already copies issue identifier with `toast.success(t(($) => $.toast.copied_identifier, { identifier }))`
- `packages/views/issues/components/issue-chip.tsx` — `IssueChip` shows `StatusIcon` + `identifier` + `title` in a bordered, rounded chip
- `packages/ui/components/ui/badge.tsx` — existing `Badge` component with variants
- `packages/views/locales/en/issues.json` and `packages/views/locales/zh-Hans/issues.json` — i18n strings for the `issues` namespace
- `packages/views/locales/en/search.json` — already has `"copied_identifier": "Copied {{identifier}}"` and `"copy_identifier": "Copy identifier"`

### Institutional Learnings

- No relevant `docs/solutions/` entries for this pattern.

## Key Technical Decisions

- **Badge over plain styled text:** A badge (subtle background + border) creates clearer visual separation from navigation elements than typography changes alone.
- **Status icon included:** Consistent with `IssueChip` and gives immediate state context. Uses existing `StatusIcon` component.
- **Click-to-copy on the badge:** The identifier is the natural copy target. No separate button needed.
- **Add `copyIdentifier` to `useIssueActions`:** Keeps clipboard logic centralized alongside `copyLink`, following the existing hook pattern.

## Implementation Units

### U1. Add `copyIdentifier` to `useIssueActions` hook

**Goal:** Expose a `copyIdentifier` handler from `useIssueActions` that copies the issue identifier to clipboard with toast feedback.

**Requirements:** R2, R3

**Dependencies:** None

**Files:**
- Modify: `packages/views/issues/actions/use-issue-actions.ts`
- Test: `packages/views/issues/actions/__tests__/use-issue-actions.test.tsx`

**Approach:**
- Add `copyIdentifier: () => Promise<void>` to `UseIssueActionsResult` interface
- Implement `copyIdentifier` using `navigator.clipboard.writeText(issueIdentifier)` with success/error toast
- Reuse the same toast pattern as `copyLink`

**Patterns to follow:**
- `copyLink` in same file (lines 121-130)
- `search-command.tsx` identifier copy pattern

**Test scenarios:**
- Happy path: `copyIdentifier` writes `issue.identifier` to clipboard and shows success toast
- Error path: when `navigator.clipboard.writeText` rejects, shows error toast
- Edge case: when `issue` is null, `copyIdentifier` is a no-op

**Verification:**
- `useIssueActions` returns `copyIdentifier` when passed a valid issue
- Calling `copyIdentifier` copies the identifier and shows "Copied MUL-123" toast

### U2. Create `IssueIdentifierBadge` component

**Goal:** Build a small, reusable badge component that displays the issue status icon + identifier with click-to-copy behavior.

**Requirements:** R1, R2, R3

**Dependencies:** U1

**Files:**
- Create: `packages/views/issues/components/issue-identifier-badge.tsx`
- Test: `packages/views/issues/components/issue-identifier-badge.test.tsx`

**Approach:**
- Accept `issue: Issue` and `onCopy: () => void` props
- Render a `Badge` (or custom styled span) with `StatusIcon` + identifier text
- Style with subtle background to distinguish from breadcrumb text
- Add `cursor-pointer` and hover state to signal interactivity
- Call `onCopy` on click

**Patterns to follow:**
- `IssueChip` for status icon + identifier layout pattern
- `Badge` component from `packages/ui/components/ui/badge.tsx`

**Test scenarios:**
- Happy path: renders status icon and identifier text
- Happy path: calls `onCopy` prop when clicked
- Edge case: renders correctly with different issue statuses

**Verification:**
- Component renders status icon and identifier
- Clicking the component triggers the copy handler

### U3. Integrate badge into issue detail header

**Goal:** Replace the plain-text identifier in `IssueDetail` PageHeader with the new `IssueIdentifierBadge`, wired to `useIssueActions.copyIdentifier`.

**Requirements:** R1, R2, R3, R4, R5

**Dependencies:** U1, U2

**Files:**
- Modify: `packages/views/issues/components/issue-detail.tsx`
- Modify: `packages/views/issues/components/index.ts` (export new component)
- Modify: `packages/views/locales/en/issues.json`
- Modify: `packages/views/locales/zh-Hans/issues.json`
- Test: `packages/views/issues/components/issue-detail.test.tsx`

**Approach:**
- Import `IssueIdentifierBadge` in `issue-detail.tsx`
- Replace the plain `<span>{issue.identifier}</span>` in PageHeader (line 708-710) with `<IssueIdentifierBadge issue={issue} onCopy={actions.copyIdentifier} />`
- Keep the badge in the same breadcrumb position — no layout changes
- Add `identifier_copied` and `identifier_copy_failed` i18n strings to `issues.json` in both EN and ZH
- Wire the new toast messages through `useT("issues")`

**Patterns to follow:**
- Existing breadcrumb structure in `issue-detail.tsx:684-714`
- Existing i18n patterns in `packages/views/locales/en/issues.json`

**Test scenarios:**
- Happy path: badge renders in the header with correct identifier
- Happy path: clicking the badge triggers copy action
- Edge case: mobile view — badge does not break header layout or cause overflow
- Integration: copyIdentifier is wired correctly from `useIssueActions`

**Verification:**
- Opening an issue detail page shows the identifier as a distinct badge
- Clicking the badge copies the identifier and shows toast
- Header layout is unchanged on mobile and desktop

## Risks & Dependencies

| Risk | Mitigation |
|------|-----------|
| Badge styling conflicts with header flex layout | Keep badge inline in the breadcrumb flex container; test on mobile |
| i18n parity drift | Update both EN and ZH locale files in the same PR |

## Sources & References

- **Origin document:** [docs/brainstorms/2026-05-12-issue-identifier-badge.md](../brainstorms/2026-05-12-issue-identifier-badge.md)
- Related code: `packages/views/issues/components/issue-detail.tsx`, `packages/views/issues/actions/use-issue-actions.ts`
- Related component: `packages/views/issues/components/issue-chip.tsx`
