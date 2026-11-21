"use client";

import { useEffect } from "react";
import { useChatStore } from "@multica/core/chat";
import { getShortcut, shortcutMatchesEvent } from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

/**
 * The expand / restore shortcut for the floating chat window. It runs the
 * header button's own `toggleExpand`, so the two never disagree about a window
 * the user dragged to full size. The window is mounted only where floating
 * chat is available, which keeps the chord free everywhere else; a closed
 * window opens already expanded.
 */
export function useChatExpandShortcut(enabled: boolean, toggleExpand: () => void) {
  useEffect(() => {
    if (!enabled) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || isImeComposing(event)) return;
      if (!shortcutMatchesEvent(getShortcut("toggleChatExpand"), event)) return;
      event.preventDefault();
      const chat = useChatStore.getState();
      if (chat.isOpen) {
        toggleExpand();
        return;
      }
      chat.setExpanded(true);
      chat.setOpen(true);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [enabled, toggleExpand]);
}
