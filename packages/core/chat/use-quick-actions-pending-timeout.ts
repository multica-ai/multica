import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { chatKeys, QUICK_ACTIONS_PENDING_TIMEOUT_MS } from "./queries";
import type { ChatQuickActionsPendingState } from "../types/chat";

/**
 * Clears a session's `quickActionsPending` marker from the query cache when no
 * chat:quick_actions supplement resolves it before the marker's own deadline
 * (MUL-5149). Unlike a component-local timer, this drops the REAL cache state,
 * so the pill spinner / skeleton stops AND a later refresh — even after closing
 * and reopening chat — starts clean instead of re-reading a stuck marker.
 *
 * The deadline is `marker.expires_at` (absolute), NOT a fresh window per mount:
 * switching between the floating chat and the chat tab unmounts one host and
 * mounts the other, and a per-mount timer would restart the full wait each time,
 * letting a lost event strand the marker indefinitely. Reading the stored
 * deadline means a remount waits only the remaining time. The final clear is
 * message-scoped so it never wipes a newer turn's marker.
 *
 * Mount once per surface that reads the marker (chat page / floating window).
 */
export function useQuickActionsPendingTimeout(
  sessionId: string | null,
  pending: ChatQuickActionsPendingState | null,
): void {
  const qc = useQueryClient();
  const messageId = pending?.message_id ?? null;
  const expiresAt = pending?.expires_at ?? null;
  useEffect(() => {
    if (!sessionId || !messageId) return;
    const clear = () =>
      qc.setQueryData<ChatQuickActionsPendingState | null>(
        chatKeys.quickActionsPending(sessionId),
        (current) =>
          current && current.message_id === messageId ? null : current,
      );
    // Remaining time to this marker's absolute deadline; an old marker without
    // one falls back to a full window. Already-past deadlines clear next tick.
    const remaining =
      expiresAt != null
        ? expiresAt - Date.now()
        : QUICK_ACTIONS_PENDING_TIMEOUT_MS;
    const timer = setTimeout(clear, Math.max(0, remaining));
    return () => clearTimeout(timer);
  }, [qc, sessionId, messageId, expiresAt]);
}
