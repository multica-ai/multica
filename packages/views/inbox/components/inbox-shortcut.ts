import { useModalStore } from "@multica/core/modals";
import {
  getShortcut,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  shortcutMatchesEvent,
  type ShortcutActionId,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

/**
 * Whether a keydown is `action`'s single-key Inbox shortcut and the Inbox's to
 * take: not handled already, not a held key or an IME composition, not typed
 * into a field, and no menu, dialog or modal on top.
 */
export function isInboxShortcut(event: KeyboardEvent, action: ShortcutActionId): boolean {
  if (event.defaultPrevented || event.repeat || isImeComposing(event)) return false;
  if (isEditableShortcutTarget(event.target)) return false;
  if (isPortalLayerShortcutTarget(event.target)) return false;
  if (useModalStore.getState().modal) return false;
  // A floating Issue Peek owns J / K and hides the Inbox detail behind it.
  if (typeof document !== "undefined" && document.querySelector("[data-issue-peek]")) return false;
  return shortcutMatchesEvent(getShortcut(action), event);
}
