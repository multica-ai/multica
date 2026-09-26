import { useEffect, type RefObject } from "react";
import {
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  shortcutMatchesEvent,
  type ShortcutChord,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

export type IssuePropertyShortcut = "status" | "priority";

export function issuePropertyShortcutFromEvent(
  event: KeyboardEvent,
  statusShortcut: ShortcutChord | null,
  priorityShortcut: ShortcutChord | null,
): IssuePropertyShortcut | null {
  if (event.defaultPrevented || event.repeat || isImeComposing(event)) return null;
  if (isEditableShortcutTarget(event.target) || isPortalLayerShortcutTarget(event.target)) return null;
  if (shortcutMatchesEvent(statusShortcut, event)) return "status";
  if (shortcutMatchesEvent(priorityShortcut, event)) return "priority";
  return null;
}

/** Configured shortcuts open property pickers only on the visible issue detail. */
export function useIssuePropertyShortcuts(
  targetRef: RefObject<HTMLElement | null>,
  enabled: boolean,
  statusShortcut: ShortcutChord | null,
  priorityShortcut: ShortcutChord | null,
  onOpen: (property: IssuePropertyShortcut) => void,
) {
  useEffect(() => {
    if (!enabled) return;
    const onKeyDown = (event: KeyboardEvent) => {
      const property = issuePropertyShortcutFromEvent(event, statusShortcut, priorityShortcut);
      if (!property || !targetRef.current?.getClientRects().length) return;
      event.preventDefault();
      onOpen(property);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [enabled, onOpen, priorityShortcut, statusShortcut, targetRef]);
}
