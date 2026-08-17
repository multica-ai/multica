"use client";

import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Owns the floating window's composer-focus nonce.
 *
 * `focusRequest` is handed to ChatInput, which pulls keyboard focus into the
 * editor every time the number changes; `0` is inert. Callers bump it for the
 * moments that mean "you are about to type something new" — a fresh chat, an
 * agent switch, a project-context change.
 *
 * Opening the window is one of those moments (MUL-5522): the whole point of the
 * toggle shortcut is reaching chat without a mouse, so landing in a window you
 * still have to click into would defeat it — and the same courtesy applies to
 * opening it from the FAB.
 *
 * The ref is seeded with the mount-time value so only a real closed → open
 * transition focuses. ChatWindow stays mounted while closed and `isOpen` is
 * restored from storage, so treating mount as an open event would let a
 * persisted "open" preference steal focus from whatever page the user loaded.
 */
export function useChatInputFocus(isOpen: boolean): {
  focusRequest: number;
  requestInputFocus: () => void;
} {
  const [focusRequest, setFocusRequest] = useState(0);
  const requestInputFocus = useCallback(() => setFocusRequest((n) => n + 1), []);

  const wasOpenRef = useRef(isOpen);
  useEffect(() => {
    const wasOpen = wasOpenRef.current;
    wasOpenRef.current = isOpen;
    if (isOpen && !wasOpen) requestInputFocus();
  }, [isOpen, requestInputFocus]);

  return { focusRequest, requestInputFocus };
}
