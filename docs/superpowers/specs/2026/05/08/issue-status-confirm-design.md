# Issue Status Confirm Design

## Goal

When a user changes an issue status to `cancelled` or `archive`, Multica must show a confirmation dialog before the mutation runs. The confirmed scope is all user-visible issue status change entry points: the issue actions menu, the issue detail sidebar status picker, and the batch action toolbar status picker.

## Current Context

Issue status changes currently flow through two UI paths:

- `packages/views/issues/actions/use-issue-actions.ts` exposes `updateField`, used by the issue actions dropdown and context menu.
- `packages/views/issues/components/pickers/status-picker.tsx` is reused by the issue detail sidebar, create issue modal, and batch action toolbar.

The delete issue confirmation flow already uses an `AlertDialog` in `packages/views/modals/delete-issue-confirm.tsx`, registered through `packages/views/modals/registry.tsx` and opened through `useModalStore`. The new confirmation should follow this pattern.

## User Experience

Selecting any non-destructive status continues to update immediately.

Selecting `cancelled` or `archive` closes the picker/menu and opens a confirmation dialog. The dialog explains that the issue will be moved to the selected terminal state and that the user can cancel without changing anything. The primary action confirms the status change; the cancel action closes the dialog and does not call the update mutation.

For batch status changes, the dialog copy must make it clear that multiple selected issues will be updated. The same confirmation component can support both single-issue and batch flows by accepting a `count` and an `onConfirm` callback through modal data.

## Architecture

Add a reusable issue status confirmation modal in `packages/views/modals/issue-status-confirm.tsx`. Register it as `issue-status-confirm` in `packages/core/modals/store.ts` and `packages/views/modals/registry.tsx`.

Add small status-confirm helpers near existing issue UI code:

- A predicate that treats `cancelled` and `archive` as confirmation-required target statuses.
- A helper hook or callback wrapper that opens `issue-status-confirm` when confirmation is required, otherwise runs the original update path.

The existing mutation behavior stays in place. The modal should call the original update callback only after the user confirms. It should not duplicate React Query mutation logic.

## Entry Points

Issue actions menu:

- `useIssueActions.updateField` should intercept `updates.status` when the target status is `cancelled` or `archive`.
- On confirmation, run the same mutation payload that would have run immediately.
- Keep the existing backlog-agent hint logic for assignee updates unchanged.

Issue detail sidebar:

- `StatusPicker` should support an optional confirmation wrapper or `confirmBeforeUpdate` behavior.
- `IssueDetail` should pass enough context for single-issue confirmation.

Batch action toolbar:

- `BatchActionToolbar` should intercept target statuses `cancelled` and `archive` before calling `handleBatchUpdate`.
- The confirmation dialog should include the selected count and call the existing batch update callback after confirmation.

Create issue modal:

- Do not add confirmation here. Creating a new issue directly in `cancelled` or `archive` is not a status change from an existing issue, and the confirmed requirement is about setting an issue status.

## Localization

Add English and Simplified Chinese strings under `packages/views/locales/*/modals.json` for:

- title
- single-issue descriptions for cancelled and archive
- batch descriptions for cancelled and archive
- cancel
- confirm
- confirming label if needed
- failure toast is not needed in the modal because existing mutation callbacks already own failure toasts

Use the existing issue status glossary: Chinese UI copy should use “已取消” and “已归档”.

## Error Handling

The modal should not swallow errors from the original mutation path. Existing mutation handlers already show failure toasts. The modal can close immediately after invoking `onConfirm`, matching the lightweight confirmation behavior, or remain open while awaiting a promise if the callback returns one. The implementation should prefer supporting async callbacks so batch updates can avoid closing before the mutation settles if needed.

If modal data is missing a valid target status or confirm callback, the modal should render nothing and close defensively.

## Testing

Add focused Vitest coverage:

- `useIssueActions.updateField({ status: "cancelled" })` opens `issue-status-confirm` instead of mutating immediately.
- Confirming the modal invokes the original update callback.
- Non-destructive statuses still update immediately.
- `StatusPicker` calls the confirmation path for `cancelled` and `archive`.
- `BatchActionToolbar` opens the confirmation modal with the selected issue count for destructive target statuses.

Existing issue actions menu tests should be updated if text or modal behavior changes.
