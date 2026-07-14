---
title: "AccessPicker draft resets on every render when passed unstable invocationTargets={[]}"
date: 2026-07-14
category: ui-bugs
module: "packages/views/agents"
problem_type: logic_error
component: tooling
severity: high
symptoms:
  - "Radio buttons in the bulk 'Set access scope' dialog appear unresponsive — clicking Workspace or Specific people has no visible effect"
  - "Checkboxes inside the 'Specific people' section don't respond to clicks"
  - "Apply button stays disabled even after selecting a scope"
root_cause: logic_error
resolution_type: code_fix
tags: [react-hooks, useEffect, useRef, unstable-reference, access-picker, bulk-dialog, agents-list]
---

# AccessPicker draft resets on every render when passed unstable invocationTargets={[]}

## Problem

The bulk "Set access scope" dialog on the agents list page embeds an `AccessPicker` component in `hideFooter` mode, delegating the apply action to a parent dialog button. Three interrelated React hooks bugs rendered the dialog completely non-functional: radio buttons snapped back to the default on every click, the Apply button stayed disabled, and even when manually enabled, Apply did nothing.

All three bugs are rooted in how `AccessPicker` manages draft-state synchronization with its props and how it communicates readiness and payload to its parent.

## Symptoms

From the user's perspective in the bulk "Set access scope" dialog:

- Clicking any radio option ("Owner only", "Workspace", "Specific people") appeared to have no effect — the radio flickered but snapped back.
- "Specific people" was unreachable: the member checkboxes did not respond to clicks.
- Apply stayed greyed out regardless of which scope was selected.
- If the user somehow enabled Apply (by selecting a non-default scope), clicking it performed no action — no API call, no toast, no dialog close.

## What Didn't Work

**Reverting the forwardRef refactor.** The initial implementation wrapped `AccessPicker` in `forwardRef` to expose `commit()`. Reverting to a plain function component had zero effect. The root cause was in the hooks logic, not in the ref forwarding. This consumed a full production-rebuild + user-test cycle.

**Checking CSS / disabled attributes in the Radix Dialog portal.** Radio elements rendered correctly, were properly named, and received click/change events. The problem was purely in React state management.

**Debugging `onChange` (Save button) vs `onReadyChange`.** `onChange` fires only from the internal Save button (hidden in bulk mode). The bulk dialog used `onReadyChange` — a completely separate code path. Debugging `onChange` was a red herring.

**Production-build friction.** PM2 ran `next start` but `.next` was stale. Every fix iteration required `pnpm build` + PM2 restart, slowing the debug cycle from seconds to minutes.

## Solution

Four targeted fixes, each addressing one aspect of the layered bugs.

**Fix 1 — `useRef` value comparison for the `useEffect`** (fixes radio/checkbox reset):

```tsx
// BEFORE — useEffect fires on every render when invocationTargets={[]}
// creates a new array reference each render cycle
const persistedMembers = useMemo(
  () => selectedTargetIds(invocationTargets, "member"),
  [invocationTargets],       // ← new [] every render → useMemo recomputes
);
useEffect(() => {
  setDraftScope(persistedScope);
  setDraftMembers(persistedMembers);
}, [persistedScope, persistedMembers]);  // ← fires every render → draft reset

// AFTER — useRef stores previous values; effect fires only on real changes
const prevPersistedScopeRef = useRef(persistedScope);
const prevPersistedMembersRef = useRef(persistedMembers);
useEffect(() => {
  const scopeChanged = persistedScope !== prevPersistedScopeRef.current;
  const membersChanged =
    persistedMembers.length !== prevPersistedMembersRef.current.length ||
    persistedMembers.some(
      (id, i) => id !== prevPersistedMembersRef.current[i],
    );
  if (scopeChanged || membersChanged) {
    setDraftScope(persistedScope);
    setDraftMembers(persistedMembers);
    prevPersistedScopeRef.current = persistedScope;
    prevPersistedMembersRef.current = persistedMembers;
  }
}, [persistedScope, persistedMembers]);
```

**Fix 2 — pass `invocationTargets={undefined}` instead of `{[]}` in the bulk dialog** (defense-in-depth):

```tsx
// BEFORE — new array reference each render, regardless of Fix 1
<AccessPicker invocationTargets={[]} />

// AFTER — undefined is a stable reference; AccessPicker handles via ??
<AccessPicker invocationTargets={undefined} />
```

**Fix 3 — `hasInteracted` state + extended `onReadyChange`** (fixes Apply disabled on default + Apply does nothing):

```tsx
// BEFORE
const ready = dirty && (draftScope !== "members" || hasMemberTarget);
useEffect(() => {
  onReadyChange?.(ready);   // ← only passes boolean, no AccessChange payload
}, [ready, onReadyChange]);

// AFTER
const [hasInteracted, setHasInteracted] = useState(false);
const selectDraftScope = (scope: AccessScope) => {
  setHasInteracted(true);
  setDraftScope(scope);
};
const ready = hasInteracted && (draftScope !== "members" || hasMemberTarget);

useEffect(() => {
  // passes both the flag AND the actual change
  onReadyChange?.(ready, ready ? buildChange() ?? undefined : undefined);
  return () => onReadyChange?.(false);
}, [ready, onReadyChange, buildChange]);
```

**Fix 4 — extract `buildChange()` helper** from `save()` for reuse by both flows:

```tsx
const buildChange = (): AccessChange | null => {
  if (draftScope === "private")
    return { permission_mode: "private", invocation_targets: [] };
  const targets: AgentInvocationTargetInput[] = [];
  if (draftScope === "workspace") targets.push({ target_type: "workspace" });
  if (draftScope === "members") {
    if (draftMembers.length === 0) return null;
    for (const id of draftMembers) targets.push({ target_type: "member", target_id: id });
    for (const id of teamIds) targets.push({ target_type: "team", target_id: id });
  }
  return { permission_mode: "public_to", invocation_targets: targets };
};
```

## Why This Works

**Fix 1 + 2** (radio/checkbox): The `useRef`-based comparison ensures `useEffect` only resets draft state when the persisted values actually change by content. Passing `undefined` from the bulk dialog (instead of `[]`) avoids the unstable-reference trap entirely. User selections now persist across renders.

**Fix 3** (`hasInteracted`): Decouples "user explicitly chose" from "draft differs from persisted." Selecting the default "Owner only" scope in a bulk dialog is a valid action — applying the default to a mixed selection is intentional. The dirty-tracking pattern (`draft ≠ persisted`) correctly prevents Save in the single-agent inspector but incorrectly blocks Confirm in a confirmation dialog.

**Fix 3 + 4** (onReadyChange + buildChange): Passing `AccessChange` alongside the `ready` boolean means the parent dialog has the actual scope to apply, without requiring `forwardRef` + imperative `commit()`.

**The three bugs formed a classic layered debugging scenario:** Bug #1 (unstable reference) masked #2 and #3 — no interaction persisted long enough to hit the Apply-button bugs. Bug #2 (ready-gate semantics) was only discoverable after #1 was fixed. Bug #3 (value-passing gap) was the final layer. Each fix revealed the next. Reverting the `forwardRef` refactor was a dead end because all three bugs would have existed regardless of how the component exposed `commit()` — the root cause was in hooks usage, not in the ref API.

## Prevention

**Never pass inline `{[]}` or `{}` as JSX props to components with `useEffect`/`useMemo` dependencies on those props.** Use `undefined`, `useMemo`, or module-level constants to maintain referential stability. The second an effect depends on an array prop, every render creates a new reference that resets the component.

**For "select and apply" patterns (vs "edit and save"), use `hasInteracted` instead of dirty comparison.** The dirty flag correctly guards "Save" in the inspector (no-change = disabled), but incorrectly guards "Confirm" in a bulk dialog where applying the default is intentional. A `hasInteracted` boolean captures user intent, not state divergence.

**When a component passes data to its parent via callback, include the data alongside the state signal.** A boolean-only callback (`onReadyChange(ready)`) forces the parent to independently reconstruct the payload — duplicating logic or requiring an imperative handle. `onReadyChange(ready, change)` keeps the child self-contained.

## Related

- [Adaptive skill import dialog with cmdk Command primitive](../design-patterns/adaptive-skill-import-dialog.md) — shares the "avoid useEffect for synchronization" philosophy; the specific manifestation and fix differ (list branching vs. unstable reference resetting draft state).
- [PR #5393](https://github.com/multica-ai/multica/pull/5393) — feat(agents): add access-scope column, filter, and bulk edit to agents list (the feature that surfaced these bugs).
- [docs/plans/2026-07-14-001-feat-agent-list-access-scope-plan.md](../../plans/2026-07-14-001-feat-agent-list-access-scope-plan.md) — the implementation plan with full unit structure and test scenarios.
