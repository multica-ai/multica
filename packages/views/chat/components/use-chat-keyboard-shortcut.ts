"use client";

import { useEffect } from "react";
import { useChatStore } from "@multica/core/chat";

/**
 * Global keyboard shortcut for the floating chat window.
 * Cmd/Ctrl+K toggles the chat window open/closed.
 *
 * This hook should be mounted once in the dashboard layout alongside
 * FloatingChat. It respects the floatingChatEnabled preference — when the
 * user has turned off the floating window, the shortcut does nothing.
 */
export function useChatKeyboardShortcut() {
  const floatingChatEnabled = useChatStore((s) => s.floatingChatEnabled);
  const toggle = useChatStore((s) => s.toggle);

  useEffect(() => {
    if (!floatingChatEnabled) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      // Cmd+K (Mac) or Ctrl+K (Windows/Linux)
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        // Don't capture if the user is typing in an input/textarea
        const target = e.target as HTMLElement;
        const isEditable =
          target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.isContentEditable;

        if (isEditable) return;

        e.preventDefault();
        toggle();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [floatingChatEnabled, toggle]);
}
