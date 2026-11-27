import { useModalStore } from "@multica/core/modals";
import { isEditableShortcutTarget, isPortalLayerShortcutTarget } from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

/**
 * Whether a keydown is free for a single-key page shortcut: not handled
 * already, not a held key or an IME composition, not typed into a field, and
 * no menu, dialog or modal on top.
 */
export function isPageShortcutEvent(event: KeyboardEvent): boolean {
  if (event.defaultPrevented || event.repeat || isImeComposing(event)) return false;
  if (isEditableShortcutTarget(event.target)) return false;
  if (isPortalLayerShortcutTarget(event.target)) return false;
  return !useModalStore.getState().modal;
}
