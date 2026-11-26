import { getShortcut, shortcutMatchesEvent, type ShortcutActionId } from "@multica/core/shortcuts";
import { isPageShortcutEvent } from "../../common/page-shortcut";

/**
 * Whether a keydown is `action`'s single-key Inbox shortcut and the Inbox's to
 * take: free for a page shortcut (`isPageShortcutEvent`) and not under a
 * floating Issue Peek.
 */
export function isInboxShortcut(event: KeyboardEvent, action: ShortcutActionId): boolean {
  if (!isPageShortcutEvent(event)) return false;
  // A floating Issue Peek owns J / K and hides the Inbox detail behind it.
  if (typeof document !== "undefined" && document.querySelector("[data-issue-peek]")) return false;
  return shortcutMatchesEvent(getShortcut(action), event);
}
