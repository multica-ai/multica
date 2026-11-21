import { toast } from "sonner";
import { useModalStore } from "@multica/core/modals";
import {
  getShortcut,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  shortcutMatchesEvent,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

/** Long enough to read the toast and reach for its undo. */
export const INBOX_UNDO_TOAST_DURATION = 10_000;

let pending: { id: string | number; undo: () => void } | null = null;

const clear = (id: string | number) => {
  if (pending?.id !== id) return;
  pending = null;
  document.removeEventListener("keydown", onKeyDown);
};

function onKeyDown(event: KeyboardEvent) {
  if (event.defaultPrevented || event.repeat || isImeComposing(event)) return;
  if (isEditableShortcutTarget(event.target)) return;
  if (isPortalLayerShortcutTarget(event.target)) return;
  if (useModalStore.getState().modal) return;
  if (!shortcutMatchesEvent(getShortcut("undoInboxAction"), event)) return;
  const undo = takeInboxUndo();
  if (!undo) return;
  event.preventDefault();
  undo();
}

/**
 * Confirm an Inbox action with a toast that can undo it, from its button or
 * the undo shortcut (Z) for as long as the toast shows. Only the latest
 * action's toast answers the shortcut.
 */
export function toastInboxUndo(
  message: string,
  { label, undo, description }: { label: string; undo: () => void; description?: string },
) {
  if (pending) clear(pending.id);
  const id = toast.success(message, {
    description,
    duration: INBOX_UNDO_TOAST_DURATION,
    action: {
      label,
      onClick: () => {
        clear(id);
        undo();
      },
    },
    onDismiss: () => clear(id),
    onAutoClose: () => clear(id),
  });
  pending = { id, undo };
  document.addEventListener("keydown", onKeyDown);
}

/** Whether an undo toast is showing, so the undo shortcut is its. */
export function hasInboxUndo() {
  return pending !== null;
}

/** Take the showing toast's undo and close the toast. */
export function takeInboxUndo(): (() => void) | null {
  if (!pending) return null;
  const { id, undo } = pending;
  clear(id);
  toast.dismiss(id);
  return undo;
}
