# Issue Status Confirm Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a confirmation dialog before existing issues are moved to `cancelled` or `archive`.

**Architecture:** Reuse the existing modal registry and `AlertDialog` pattern from delete issue. Keep mutation ownership in the current callers; the new modal only gates confirmation and then invokes the original update callback.

**Tech Stack:** React, TypeScript, Zustand modal store, TanStack Query mutations, Vitest, Testing Library, existing `@multica/ui` AlertDialog primitives.

---

## File Structure

- Modify: `packages/core/modals/store.ts`
  - Add `issue-status-confirm` to the modal type union.
- Create: `packages/views/issues/actions/status-confirmation.ts`
  - Export `requiresIssueStatusConfirmation(status)` and modal payload helpers.
- Create: `packages/views/modals/issue-status-confirm.tsx`
  - Render the confirmation dialog and call the provided confirm callback.
- Modify: `packages/views/modals/registry.tsx`
  - Register `IssueStatusConfirmModal`.
- Modify: `packages/views/locales/en/modals.json`
  - Add English strings for the status confirmation modal.
- Modify: `packages/views/locales/zh-Hans/modals.json`
  - Add Simplified Chinese strings for the status confirmation modal.
- Modify: `packages/views/issues/actions/use-issue-actions.ts`
  - Intercept single-issue status updates to `cancelled` and `archive`.
- Modify: `packages/views/issues/components/pickers/status-picker.tsx`
  - Add optional confirmation hook for reusable picker flows.
- Modify: `packages/views/issues/components/issue-detail.tsx`
  - Pass confirmation behavior to `StatusPicker`.
- Modify: `packages/views/issues/components/batch-action-toolbar.tsx`
  - Confirm destructive batch status updates.
- Modify tests:
  - `packages/views/issues/actions/__tests__/use-issue-actions.test.tsx`
  - `packages/views/issues/actions/__tests__/issue-actions-menu.test.tsx`
  - Add or update picker/batch toolbar tests near the touched components.

## Task 1: Add Shared Status Confirmation Helpers

- [ ] **Step 1: Create the helper file**

Create `packages/views/issues/actions/status-confirmation.ts`:

```ts
import type { IssueStatus } from "@multica/core/types";

export const CONFIRMABLE_ISSUE_STATUSES = ["cancelled", "archive"] as const satisfies readonly IssueStatus[];

export type ConfirmableIssueStatus = (typeof CONFIRMABLE_ISSUE_STATUSES)[number];

export function requiresIssueStatusConfirmation(
  status: IssueStatus | undefined | null,
): status is ConfirmableIssueStatus {
  return status === "cancelled" || status === "archive";
}
```

- [ ] **Step 2: Run typecheck for the helper**

Run: `pnpm --filter @multica/views exec tsc --noEmit`

Expected: If the package has no standalone TypeScript script, use the repository typecheck command in Task 7. No runtime behavior changes yet.

## Task 2: Add Modal Store and Registry Support

- [ ] **Step 1: Extend the modal type union**

Modify `packages/core/modals/store.ts`:

```ts
type ModalType =
  | "create-workspace"
  | "create-issue"
  | "quick-create-issue"
  | "create-project"
  | "feedback"
  | "issue-set-parent"
  | "issue-add-child"
  | "issue-delete-confirm"
  | "issue-status-confirm"
  | "issue-backlog-agent-hint"
  | null;
```

- [ ] **Step 2: Create the modal component**

Create `packages/views/modals/issue-status-confirm.tsx`:

```tsx
"use client";

import { useState } from "react";
import type { IssueStatus } from "@multica/core/types";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useT } from "../i18n";

type ConfirmCallback = () => void | Promise<void>;

function isIssueStatus(value: unknown): value is IssueStatus {
  return value === "cancelled" || value === "archive";
}

export function IssueStatusConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const status = data?.status;
  const count = typeof data?.count === "number" ? data.count : 1;
  const onConfirm = data?.onConfirm;
  const [confirming, setConfirming] = useState(false);

  if (!isIssueStatus(status) || typeof onConfirm !== "function") {
    onClose();
    return null;
  }

  const handleConfirm = async () => {
    setConfirming(true);
    try {
      await (onConfirm as ConfirmCallback)();
      onClose();
    } catch {
      setConfirming(false);
    }
  };

  const descriptionKey =
    count > 1
      ? status === "cancelled"
        ? "description_cancelled_batch"
        : "description_archive_batch"
      : status === "cancelled"
        ? "description_cancelled"
        : "description_archive";

  return (
    <AlertDialog open onOpenChange={(v) => { if (!v && !confirming) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.issue_status_confirm.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.issue_status_confirm[descriptionKey], { count })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={confirming}>
            {t(($) => $.issue_status_confirm.cancel)}
          </AlertDialogCancel>
          <AlertDialogAction onClick={handleConfirm} disabled={confirming}>
            {confirming
              ? t(($) => $.issue_status_confirm.confirming)
              : t(($) => $.issue_status_confirm.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
```

- [ ] **Step 3: Register the modal**

Modify `packages/views/modals/registry.tsx`:

```tsx
import { IssueStatusConfirmModal } from "./issue-status-confirm";
```

Add the switch case:

```tsx
case "issue-status-confirm":
  return <IssueStatusConfirmModal onClose={close} data={data} />;
```

## Task 3: Add Localized Copy

- [ ] **Step 1: Add English strings**

In `packages/views/locales/en/modals.json`, add this sibling object near `delete_issue`:

```json
"issue_status_confirm": {
  "title": "Change issue status?",
  "description_cancelled": "This issue will be marked as Cancelled. You can cancel now to keep the current status.",
  "description_archive": "This issue will be moved to Archive. You can cancel now to keep the current status.",
  "description_cancelled_batch": "{{count}} selected issues will be marked as Cancelled. You can cancel now to keep their current statuses.",
  "description_archive_batch": "{{count}} selected issues will be moved to Archive. You can cancel now to keep their current statuses.",
  "cancel": "Cancel",
  "confirm": "Confirm",
  "confirming": "Confirming..."
}
```

- [ ] **Step 2: Add Simplified Chinese strings**

In `packages/views/locales/zh-Hans/modals.json`, add this sibling object near `delete_issue`:

```json
"issue_status_confirm": {
  "title": "确认变更 issue 状态？",
  "description_cancelled": "该 issue 将被标记为已取消。取消操作可保留当前状态。",
  "description_archive": "该 issue 将被移动到已归档。取消操作可保留当前状态。",
  "description_cancelled_batch": "{{count}} 个已选 issue 将被标记为已取消。取消操作可保留当前状态。",
  "description_archive_batch": "{{count}} 个已选 issue 将被移动到已归档。取消操作可保留当前状态。",
  "cancel": "取消",
  "confirm": "确认",
  "confirming": "确认中..."
}
```

## Task 4: Gate Single-Issue Action Menu Updates

- [ ] **Step 1: Write the failing hook test**

Update `packages/views/issues/actions/__tests__/use-issue-actions.test.tsx` with:

```tsx
it("opens a confirmation modal before updating status to cancelled", () => {
  const { result } = renderHook(() => useIssueActions(mockIssue), { wrapper });

  act(() => {
    result.current.updateField({ status: "cancelled" });
  });

  expect(mockUpdateMutate).not.toHaveBeenCalled();
  expect(mockOpenModal).toHaveBeenCalledWith("issue-status-confirm", {
    status: "cancelled",
    count: 1,
    onConfirm: expect.any(Function),
  });

  const payload = mockOpenModal.mock.calls.at(-1)?.[1] as { onConfirm: () => void };
  act(() => {
    payload.onConfirm();
  });

  expect(mockUpdateMutate).toHaveBeenCalledWith(
    { id: "issue-1", status: "cancelled" },
    expect.any(Object),
  );
});

it("opens a confirmation modal before updating status to archive", () => {
  const { result } = renderHook(() => useIssueActions(mockIssue), { wrapper });

  act(() => {
    result.current.updateField({ status: "archive" });
  });

  expect(mockUpdateMutate).not.toHaveBeenCalled();
  expect(mockOpenModal).toHaveBeenCalledWith("issue-status-confirm", {
    status: "archive",
    count: 1,
    onConfirm: expect.any(Function),
  });
});
```

- [ ] **Step 2: Run the failing test**

Run: `pnpm --filter @multica/views exec vitest run packages/views/issues/actions/__tests__/use-issue-actions.test.tsx`

Expected: FAIL because `updateField` still mutates immediately.

- [ ] **Step 3: Implement the gate**

Modify `packages/views/issues/actions/use-issue-actions.ts`:

```ts
import { requiresIssueStatusConfirmation } from "./status-confirmation";
```

Inside `updateField`, before the mutation:

```ts
const runUpdate = () => {
  updateIssue.mutate(
    { id: issueId, ...updates },
    { onError: () => toast.error(t(($) => $.detail.update_failed)) },
  );
};

if (requiresIssueStatusConfirmation(updates.status)) {
  openModal("issue-status-confirm", {
    status: updates.status,
    count: 1,
    onConfirm: runUpdate,
  });
  return;
}

runUpdate();
```

Keep the existing backlog-agent hint block after `runUpdate()` for assignment updates.

- [ ] **Step 4: Run the hook tests**

Run: `pnpm --filter @multica/views exec vitest run packages/views/issues/actions/__tests__/use-issue-actions.test.tsx`

Expected: PASS.

## Task 5: Gate StatusPicker-Based Single-Issue Updates

- [ ] **Step 1: Extend StatusPicker props**

Modify `packages/views/issues/components/pickers/status-picker.tsx`:

```tsx
confirmStatusChange?: (status: IssueStatus, runUpdate: () => void) => void;
```

Update the `PickerItem` click handler:

```tsx
onClick={() => {
  const runUpdate = () => onUpdate({ status: s });
  if (confirmStatusChange) {
    confirmStatusChange(s, runUpdate);
  } else {
    runUpdate();
  }
  setOpen(false);
}}
```

- [ ] **Step 2: Wire issue detail**

Modify `packages/views/issues/components/issue-detail.tsx`:

```tsx
import { useModalStore } from "@multica/core/modals";
import { requiresIssueStatusConfirmation } from "../actions/status-confirmation";
```

Inside `IssueDetail`, add:

```tsx
const openModal = useModalStore((s) => s.open);
const confirmStatusChange = useCallback(
  (status: IssueStatus, runUpdate: () => void) => {
    if (!requiresIssueStatusConfirmation(status)) {
      runUpdate();
      return;
    }
    openModal("issue-status-confirm", {
      status,
      count: 1,
      onConfirm: runUpdate,
    });
  },
  [openModal],
);
```

Pass it to the sidebar picker:

```tsx
<StatusPicker
  status={issue.status}
  onUpdate={handleUpdateField}
  confirmStatusChange={confirmStatusChange}
  align="start"
/>
```

Do not pass this prop in `packages/views/modals/create-issue.tsx`.

## Task 6: Gate Batch Status Updates

- [ ] **Step 1: Wire modal store and helper**

Modify `packages/views/issues/components/batch-action-toolbar.tsx`:

```tsx
import { useModalStore } from "@multica/core/modals";
import { requiresIssueStatusConfirmation } from "../actions/status-confirmation";
```

Inside `BatchActionToolbar`, add:

```tsx
const openModal = useModalStore((s) => s.open);
```

- [ ] **Step 2: Split confirmed status handling from generic batch updates**

Add:

```tsx
const handleBatchStatusUpdate = (updates: Partial<UpdateIssueRequest>) => {
  if (!requiresIssueStatusConfirmation(updates.status)) {
    void handleBatchUpdate(updates);
    return;
  }

  openModal("issue-status-confirm", {
    status: updates.status,
    count,
    onConfirm: () => handleBatchUpdate(updates),
  });
};
```

Pass it to the status picker:

```tsx
<StatusPicker
  status="todo"
  onUpdate={handleBatchStatusUpdate}
  open={statusOpen}
  onOpenChange={setStatusOpen}
  triggerRender={<Button variant="ghost" size="sm" disabled={loading} />}
  trigger={t(($) => $.batch.status)}
  align="center"
/>
```

## Task 7: Verification

- [ ] **Step 1: Run focused tests**

Run:

```bash
pnpm --filter @multica/views exec vitest run packages/views/issues/actions/__tests__/use-issue-actions.test.tsx packages/views/issues/actions/__tests__/issue-actions-menu.test.tsx
```

Expected: PASS.

- [ ] **Step 2: Run package tests if available**

Run:

```bash
pnpm --filter @multica/views test
```

Expected: PASS.

- [ ] **Step 3: Run typecheck**

Run:

```bash
pnpm typecheck
```

Expected: PASS.

- [ ] **Step 4: Inspect git diff**

Run:

```bash
git diff -- packages/core/modals/store.ts packages/views/modals/registry.tsx packages/views/modals/issue-status-confirm.tsx packages/views/issues/actions/use-issue-actions.ts packages/views/issues/actions/status-confirmation.ts packages/views/issues/components/pickers/status-picker.tsx packages/views/issues/components/issue-detail.tsx packages/views/issues/components/batch-action-toolbar.tsx packages/views/locales/en/modals.json packages/views/locales/zh-Hans/modals.json
```

Expected: Diff is limited to the confirmation flow, localization, and tests.

## Self-Review

- Spec coverage: The plan covers all confirmed entry points: actions menu, issue detail status picker, and batch toolbar status picker.
- Scope guard: Create issue status selection is intentionally excluded because it creates a new issue rather than changing an existing issue status.
- Type consistency: The implementation uses the existing `IssueStatus` union and the repository's actual archive status value, `archive`.
- Placeholder scan: No task depends on unspecified behavior.
